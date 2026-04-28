// Package bge provides an in-process embedder backed by an int8-quantized
// BAAI/bge-small-en-v1.5 model. The model and vocabulary files are bundled
// via go:embed so no network access, no external binaries, and no cgo are
// required at runtime.
//
// Platform support mirrors guild's embed package: unix (darwin + linux,
// amd64 + arm64). On other platforms New returns ErrUnsupported so callers
// can fall through to lexical-only retrieval.
//
// Usage:
//
//	emb, err := bge.New()
//	if err != nil {
//	    // handle ErrUnsupported or init failure
//	}
//	defer emb.Close(ctx)
//
//	vecs, err := emb.Embed(ctx, []string{"hello world"})
package bge

import (
	"log/slog"

	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore/embed"
)

// Option configures a BGE embedder at construction time.
type Option func(*config)

// WithLogger sets the structured logger used for initialization messages
// and runtime warnings. If not provided, slog.Default() is used.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		c.logger = l
	}
}

// WithTracer sets the OpenTelemetry tracer used to instrument Embed calls.
// If not provided, the no-op tracer is used.
func WithTracer(t trace.Tracer) Option {
	return func(c *config) {
		c.tracer = t
	}
}

type config struct {
	logger *slog.Logger
	tracer trace.Tracer
}

// New returns an Embedder backed by the bundled int8-quantized BGE-small
// model. The model is loaded from go:embed assets at first call; no network
// access is performed. Returns ErrUnsupported on platforms where the ONNX
// runtime is not available.
//
// Callers must call Close when done to release the loaded model.
func New(opts ...Option) (embed.Embedder, error) {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}
	if cfg.tracer == nil {
		cfg.tracer = trace.NewNoopTracerProvider().Tracer("lore.embed.bge")
	}
	return newEmbedder(cfg)
}

// newEmbedder is the platform-dispatched constructor. Implemented in
// bge_unix.go on supported platforms; the stub in bge_unsupported.go
// returns ErrUnsupported everywhere else.
