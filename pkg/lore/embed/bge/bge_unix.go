// Unix-specific BGE embedder implementation. Uses shota3506/onnxruntime-purego
// to run the int8 model via the platform's ONNX Runtime shared library.
// On first use, the embedded model and vocabulary bytes are extracted to the
// OS temporary directory so ONNX Runtime can load them from disk.

//go:build unix

package bge

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/shota3506/onnxruntime-purego/onnxruntime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore/embed"
)

// ortAPIVersion is the ORT C API version this package targets. Ties us to
// ORT 1.23.x; bumping requires an explicit decision.
const ortAPIVersion = 23

// maxSeqLen is the token sequence cap (inclusive of [CLS] and [SEP]).
// Matches bge-small's position-embedding table size.
const maxSeqLen = 512

// bgeEmbedder is the unix production path: BAAI/bge-small-en-v1.5-int8
// run through shota3506/onnxruntime-purego with CLS-token pooling and L2
// normalization.
//
// Safe for concurrent use: Embed holds mu across the ORT Run call because
// the CPU execution provider is not documented as concurrent-safe.
type bgeEmbedder struct {
	rt        *ortRuntime
	tokenizer *wordPieceTokenizer
	dim       int
	logger    *slog.Logger
	tracer    trace.Tracer
	mu        sync.Mutex
	closed    bool
}

// ortRuntime groups the three ORT handles whose lifetimes are tied together.
type ortRuntime struct {
	rt      *ort.Runtime
	env     *ort.Env
	session *ort.Session
}

func (r *ortRuntime) close() {
	if r == nil {
		return
	}
	if r.session != nil {
		r.session.Close()
		r.session = nil
	}
	if r.env != nil {
		r.env.Close()
		r.env = nil
	}
	if r.rt != nil {
		_ = r.rt.Close()
		r.rt = nil
	}
}

// newEmbedder is the platform-specific constructor called from bge.go.
func newEmbedder(cfg *config) (embed.Embedder, error) {
	// Extract embedded assets to a temporary directory so ORT can load them
	// from disk. We use os.TempDir() for simplicity; repeated New() calls
	// reuse an existing valid file.
	dir, err := prepareAssets()
	if err != nil {
		cfg.logger.Error("bge: failed to prepare model assets", "err", err)
		return nil, fmt.Errorf("bge: prepare assets: %w", err)
	}

	libPath, err := probeLibrary(cfg.logger)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", embed.ErrUnsupported, err)
	}

	rt, err := openRuntime(libPath, filepath.Join(dir, "model.onnx"))
	if err != nil {
		return nil, fmt.Errorf("bge: open runtime: %w", err)
	}

	vocabPath := filepath.Join(dir, "vocab.txt")
	vocab, err := loadVocab(vocabPath)
	if err != nil {
		rt.close()
		return nil, fmt.Errorf("bge: load vocab: %w", err)
	}

	tk := newWordPieceTokenizer(vocab)
	if err := tk.assertVocabHasSpecials(); err != nil {
		rt.close()
		return nil, err
	}

	return &bgeEmbedder{
		rt:        rt,
		tokenizer: tk,
		dim:       embed.Dim,
		logger:    cfg.logger,
		tracer:    cfg.tracer,
	}, nil
}

// Dimensions returns the embedding dimension.
func (e *bgeEmbedder) Dimensions() int { return e.dim }

// Close releases the underlying ORT session, env, and runtime. Idempotent.
func (e *bgeEmbedder) Close(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	e.rt.close()
	e.rt = nil
	return nil
}

// Embed tokenizes each text, runs one forward pass per text through the
// ONNX model, CLS-pools the last hidden state, and L2-normalizes. Returns
// one []float32 vector of length Dimensions() per input string.
//
// Returns ErrInvalidArgument if texts is empty or any element is empty.
// Returns ErrClosed if Close has already been called. Context cancellation
// is honored between tokenization steps; ORT Run itself is uncancellable.
func (e *bgeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: texts must not be empty", embed.ErrInvalidArgument)
	}
	for i, t := range texts {
		if t == "" {
			return nil, fmt.Errorf("%w: texts[%d] is empty", embed.ErrInvalidArgument, i)
		}
	}

	ctx, span := e.tracer.Start(ctx, "lore.embed.encode",
		trace.WithAttributes(
			attribute.Int("texts.count", len(texts)),
			attribute.Int("dimensions", e.dim),
		),
	)
	defer span.End()

	results := make([][]float32, len(texts))
	for i, text := range texts {
		if err := ctx.Err(); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, err
		}
		vec, err := e.embedOne(ctx, text)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return nil, fmt.Errorf("bge: embed[%d]: %w", i, err)
		}
		results[i] = vec
	}
	return results, nil
}

// embedOne runs one inference pass for a single text.
func (e *bgeEmbedder) embedOne(_ context.Context, text string) ([]float32, error) {
	ids, mask, typeIDs := e.tokenizer.encode(text, maxSeqLen)
	seqLen := int64(len(ids))

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed || e.rt == nil || e.rt.session == nil {
		return nil, embed.ErrClosed
	}

	idsT, err := ort.NewTensorValue(e.rt.rt, ids, []int64{1, seqLen})
	if err != nil {
		return nil, fmt.Errorf("input_ids tensor: %w", err)
	}
	defer idsT.Close()

	maskT, err := ort.NewTensorValue(e.rt.rt, mask, []int64{1, seqLen})
	if err != nil {
		return nil, fmt.Errorf("attention_mask tensor: %w", err)
	}
	defer maskT.Close()

	typeT, err := ort.NewTensorValue(e.rt.rt, typeIDs, []int64{1, seqLen})
	if err != nil {
		return nil, fmt.Errorf("token_type_ids tensor: %w", err)
	}
	defer typeT.Close()

	inputs := map[string]*ort.Value{
		"input_ids":      idsT,
		"attention_mask": maskT,
		"token_type_ids": typeT,
	}
	outputs, err := e.rt.session.Run(context.Background(), inputs,
		ort.WithOutputNames("last_hidden_state"))
	if err != nil {
		return nil, fmt.Errorf("ort Run: %w", err)
	}
	defer func() {
		for _, v := range outputs {
			v.Close()
		}
	}()

	lhs := outputs["last_hidden_state"]
	data, shape, err := ort.GetTensorData[float32](lhs)
	if err != nil {
		return nil, fmt.Errorf("GetTensorData: %w", err)
	}
	if len(shape) != 3 || shape[0] != 1 || shape[2] != int64(e.dim) {
		return nil, fmt.Errorf("unexpected output shape %v", shape)
	}

	// CLS token is position 0 in the sequence dimension.
	cls := make([]float32, e.dim)
	copy(cls, data[:e.dim])
	l2Normalize(cls)
	return cls, nil
}

// openRuntime loads libonnxruntime, opens an ORT env + inference session,
// and returns the triple. On failure every partially-constructed resource
// is released.
func openRuntime(libPath, modelPath string) (*ortRuntime, error) {
	rt, err := ort.NewRuntime(libPath, ortAPIVersion)
	if err != nil {
		return nil, fmt.Errorf("ort NewRuntime(%q): %w", libPath, err)
	}
	env, err := rt.NewEnv("lore-embed", ort.LoggingLevelWarning)
	if err != nil {
		_ = rt.Close()
		return nil, fmt.Errorf("ort NewEnv: %w", err)
	}
	opts := &ort.SessionOptions{}
	sess, err := rt.NewSession(env, modelPath, opts)
	if err != nil {
		env.Close()
		_ = rt.Close()
		return nil, fmt.Errorf("ort NewSession(%q): %w", modelPath, err)
	}
	return &ortRuntime{rt: rt, env: env, session: sess}, nil
}

// prepareAssets extracts the embedded model and vocab to a stable path under
// os.TempDir() so ONNX Runtime can load them from disk. Already-extracted
// files are reused without re-writing.
func prepareAssets() (string, error) {
	dir := filepath.Join(os.TempDir(), "lore-bge-model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := writeIfMissing(filepath.Join(dir, "model.onnx"), modelBytes); err != nil {
		return "", fmt.Errorf("extract model.onnx: %w", err)
	}
	if err := writeIfMissing(filepath.Join(dir, "vocab.txt"), vocabBytes); err != nil {
		return "", fmt.Errorf("extract vocab.txt: %w", err)
	}
	return dir, nil
}

// writeIfMissing writes data to path only if the file does not already exist.
func writeIfMissing(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil // already exists
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// l2Normalize scales v to unit L2 norm in place. A zero-norm input is left
// untouched. Uses float64 accumulation to reduce precision loss.
func l2Normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1.0 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}

// loadVocab reads a vocab.txt (one token per line, zero-indexed) into a map.
func loadVocab(path string) (map[string]int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer f.Close()
	vocab := make(map[string]int, 32000)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	i := 0
	for scanner.Scan() {
		vocab[scanner.Text()] = i
		i++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return vocab, nil
}
