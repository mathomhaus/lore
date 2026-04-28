// Package hybrid provides a Retriever that fuses BM25 lexical search and
// vector semantic search via Reciprocal Rank Fusion (RRF). It composes:
//
//   - store.Store        for BM25 (store.SearchText)
//   - embed.Embedder     for query vectorisation
//   - vector.VectorStore for nearest-neighbour search
//
// The three dependencies are caller-owned; hybrid.New does not call Close
// on any of them.
//
// OTel instrumentation:
//
//	lore.retrieve.search        (top-level span)
//	  lore.retrieve.bm25        (sub-span, attr bm25.count)
//	  lore.retrieve.vector      (sub-span, attr vector.count)
//	  lore.retrieve.fuse        (sub-span, attr fused.count)
package hybrid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/embed"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/rrf"
	lstore "github.com/mathomhaus/lore/pkg/lore/store"
	lvector "github.com/mathomhaus/lore/pkg/lore/vector"
)

const tracerName = "lore.retrieve.hybrid"

const (
	defaultLimit         = 10
	defaultRRFK          = rrf.DefaultK
	defaultCandidatePool = 50
)

// Retriever is the hybrid BM25+vector retriever. Construct with New.
// Safe for concurrent use.
type Retriever struct {
	store    lstore.Store
	emb      embed.Embedder
	vstore   lvector.VectorStore
	logger   *slog.Logger
	tracer   trace.Tracer
	rrfK     int
	poolSize int
}

// Option configures a Retriever.
type Option func(*Retriever)

// WithLogger sets the structured logger. Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(r *Retriever) { r.logger = l }
}

// WithTracer sets the OpenTelemetry tracer.
// Defaults to the global tracer provider.
func WithTracer(t trace.Tracer) Option {
	return func(r *Retriever) { r.tracer = t }
}

// WithRRFK sets the RRF smoothing constant k. Default 60.
// Values <= 0 are ignored and the default is kept.
func WithRRFK(k int) Option {
	return func(r *Retriever) {
		if k > 0 {
			r.rrfK = k
		}
	}
}

// WithCandidatePoolSize sets the number of candidates fetched from each
// ranker before fusion. Default 50. Values <= 0 are ignored.
func WithCandidatePoolSize(n int) Option {
	return func(r *Retriever) {
		if n > 0 {
			r.poolSize = n
		}
	}
}

// New returns a hybrid Retriever that fuses BM25 + vector ranking via RRF.
//
// Caller-owned dependencies: s, emb, and vstore are all consumer-managed.
// The Retriever does not call Close on any of them.
func New(s lstore.Store, emb embed.Embedder, vstore lvector.VectorStore, opts ...Option) *Retriever {
	r := &Retriever{
		store:    s,
		emb:      emb,
		vstore:   vstore,
		rrfK:     defaultRRFK,
		poolSize: defaultCandidatePool,
	}
	for _, o := range opts {
		o(r)
	}
	if r.logger == nil {
		r.logger = slog.Default()
	}
	if r.tracer == nil {
		r.tracer = otel.GetTracerProvider().Tracer(tracerName)
	}
	return r
}

// Search executes the hybrid retrieval pipeline:
//  1. BM25 arm: store.SearchText with candidatePoolSize limit.
//  2. Vector arm: embed query, then vstore.Search with candidatePoolSize limit.
//  3. RRF fusion of both ranked lists.
//  4. Hydrate full entries for fused IDs via store.Get.
//  5. Return SearchHits in fused order.
//
// Partial failures: if one arm fails, Search logs a warning and falls through
// to the surviving arm. If both arms fail, Search returns an error.
//
// Returns lore.ErrInvalidArgument when query is empty or opts.Limit is negative.
func (r *Retriever) Search(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("hybrid: search: %w", lore.ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("hybrid: search: negative limit: %w", lore.ErrInvalidArgument)
	}

	limit := opts.Limit
	if limit == 0 {
		limit = defaultLimit
	}

	ctx, span := r.tracer.Start(ctx, "lore.retrieve.search")
	defer span.End()

	// BM25 arm.
	bm25IDs, bm25Err := r.runBM25(ctx, query, opts)
	if bm25Err != nil {
		r.logger.WarnContext(ctx, "hybrid: bm25 arm failed; will use vector only", "err", bm25Err)
	}

	// Vector arm.
	vecIDs, vecErr := r.runVector(ctx, query, opts)
	if vecErr != nil {
		r.logger.WarnContext(ctx, "hybrid: vector arm failed; will use bm25 only", "err", vecErr)
	}

	// Both arms failed: propagate the BM25 error (first failure) as the
	// primary; wrap vector error in message for diagnostic context.
	if bm25Err != nil && vecErr != nil {
		r.logger.ErrorContext(ctx, "hybrid: both arms failed", "bm25_err", bm25Err, "vec_err", vecErr)
		return nil, fmt.Errorf("hybrid: both rankers failed (bm25: %w; vector: %v)", bm25Err, vecErr)
	}

	// Fuse.
	ctx, fuseSpan := r.tracer.Start(ctx, "lore.retrieve.fuse")

	var rankings [][]int64
	if len(bm25IDs) > 0 {
		rankings = append(rankings, bm25IDs)
	}
	if len(vecIDs) > 0 {
		rankings = append(rankings, vecIDs)
	}

	fused := rrf.Fuse(rankings, r.rrfK)

	// Truncate fused list to limit before hydration to avoid fetching
	// entries we will discard.
	if len(fused) > limit {
		fused = fused[:limit]
	}

	fuseSpan.SetAttributes(attribute.Int("fused.count", len(fused)))
	fuseSpan.End()

	if len(fused) == 0 {
		return nil, nil
	}

	// Hydrate entries. Fetch each entry individually; the number is bounded
	// by limit (typically <= 50).
	results := make([]lore.SearchHit, 0, len(fused))
	for _, scored := range fused {
		entry, err := r.store.Get(ctx, scored.ID)
		if err != nil {
			if errors.Is(err, lore.ErrNotFound) {
				// Index or FTS ahead of store; skip stale reference.
				r.logger.WarnContext(ctx, "hybrid: entry not found; skipping", "id", scored.ID)
				continue
			}
			return nil, fmt.Errorf("hybrid: hydrate entry %d: %w", scored.ID, err)
		}
		// Post-filter by Project, Kinds, and Tags. The vector arm does not
		// honor these filters natively (its SearchOpts hints are advisory),
		// so the Retriever applies them after hydration to satisfy the
		// caller's contract. The BM25 arm's results may already be filtered;
		// re-applying here is a no-op for those.
		if opts.Project != "" && entry.Project != opts.Project {
			continue
		}
		if len(opts.Kinds) > 0 && !containsKind(opts.Kinds, entry.Kind) {
			continue
		}
		if len(opts.Tags) > 0 && !containsAllTags(entry.Tags, opts.Tags) {
			continue
		}
		results = append(results, lore.SearchHit{Entry: entry, Score: scored.Score})
	}

	return results, nil
}

// containsKind reports whether want contains kind.
func containsKind(want []lore.Kind, kind lore.Kind) bool {
	for _, k := range want {
		if k == kind {
			return true
		}
	}
	return false
}

// containsAllTags reports whether entryTags contains every tag in required.
// Membership is intersection: required={"a","b"} demands the entry have BOTH.
func containsAllTags(entryTags, required []string) bool {
	have := make(map[string]struct{}, len(entryTags))
	for _, t := range entryTags {
		have[t] = struct{}{}
	}
	for _, r := range required {
		if _, ok := have[r]; !ok {
			return false
		}
	}
	return true
}

// runBM25 executes the BM25 arm and returns a slice of entry IDs in rank order.
func (r *Retriever) runBM25(ctx context.Context, query string, opts lore.SearchOpts) ([]int64, error) {
	ctx, span := r.tracer.Start(ctx, "lore.retrieve.bm25")
	defer span.End()

	bm25Opts := lore.SearchOpts{
		Project: opts.Project,
		Kinds:   opts.Kinds,
		Tags:    opts.Tags,
		Limit:   r.poolSize,
	}
	hits, err := r.store.SearchText(ctx, query, bm25Opts)
	if err != nil {
		return nil, err
	}

	span.SetAttributes(attribute.Int("bm25.count", len(hits)))

	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.Entry.ID
	}
	return ids, nil
}

// runVector executes the vector arm and returns a slice of entry IDs in rank order.
func (r *Retriever) runVector(ctx context.Context, query string, opts lore.SearchOpts) ([]int64, error) {
	ctx, span := r.tracer.Start(ctx, "lore.retrieve.vector")
	defer span.End()

	vecs, err := r.emb.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embed returned empty vector")
	}

	vsOpts := lvector.SearchOpts{
		Limit: r.poolSize,
		Kinds: opts.Kinds,
		Tags:  opts.Tags,
	}
	hits, err := r.vstore.Search(ctx, vecs[0], vsOpts)
	if err != nil {
		return nil, fmt.Errorf("vstore search: %w", err)
	}

	span.SetAttributes(attribute.Int("vector.count", len(hits)))

	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return ids, nil
}
