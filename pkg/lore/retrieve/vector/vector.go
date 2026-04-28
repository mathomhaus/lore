// Package vector provides a semantic-only Retriever that embeds the query
// via an Embedder and searches a VectorStore for nearest neighbours.
// It satisfies retrieve.Retriever and is composable as the vector arm of
// hybrid.New.
package vector

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
	lstore "github.com/mathomhaus/lore/pkg/lore/store"
	lvector "github.com/mathomhaus/lore/pkg/lore/vector"
)

const tracerName = "lore.retrieve.vector"

// Searcher is a Retriever backed by vector (semantic) search only.
// Construct with New. Safe for concurrent use.
type Searcher struct {
	store  lstore.Store
	emb    embed.Embedder
	vstore lvector.VectorStore
	logger *slog.Logger
	tracer trace.Tracer
}

// Option configures a Searcher.
type Option func(*Searcher)

// WithLogger sets the structured logger.
// Defaults to slog.Default() when not provided.
func WithLogger(l *slog.Logger) Option {
	return func(s *Searcher) { s.logger = l }
}

// WithTracer sets the OpenTelemetry tracer.
// Defaults to the global tracer provider when not provided.
func WithTracer(t trace.Tracer) Option {
	return func(s *Searcher) { s.tracer = t }
}

// New returns a Searcher that embeds the query via emb and searches vstore,
// then hydrates full entries from store. The caller owns all three resources
// and must not close them before Searcher is done.
func New(s lstore.Store, emb embed.Embedder, vstore lvector.VectorStore, opts ...Option) *Searcher {
	sr := &Searcher{store: s, emb: emb, vstore: vstore}
	for _, o := range opts {
		o(sr)
	}
	if sr.logger == nil {
		sr.logger = slog.Default()
	}
	if sr.tracer == nil {
		sr.tracer = otel.GetTracerProvider().Tracer(tracerName)
	}
	return sr
}

// Search embeds query, finds nearest-neighbour entries, and returns them as
// SearchHits. Returns lore.ErrInvalidArgument when query is empty or opts.Limit
// is negative.
func (s *Searcher) Search(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("vector: search: %w", lore.ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("vector: search: negative limit: %w", lore.ErrInvalidArgument)
	}

	ctx, span := s.tracer.Start(ctx, "lore.retrieve.vector")
	defer span.End()

	vecs, err := s.emb.Embed(ctx, []string{query})
	if err != nil {
		if !errors.Is(err, embed.ErrUnsupported) {
			s.logger.ErrorContext(ctx, "vector: embed failed", "err", err)
		}
		return nil, fmt.Errorf("vector: embed query: %w", err)
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("vector: embed returned empty vector")
	}
	qvec := vecs[0]

	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}
	vsOpts := lvector.SearchOpts{
		Limit: limit,
		Kinds: opts.Kinds,
		Tags:  opts.Tags,
	}
	hits, err := s.vstore.Search(ctx, qvec, vsOpts)
	if err != nil {
		s.logger.ErrorContext(ctx, "vector: vstore.Search failed", "err", err)
		return nil, fmt.Errorf("vector: vstore search: %w", err)
	}

	span.SetAttributes(attribute.Int("vector.count", len(hits)))

	results := make([]lore.SearchHit, 0, len(hits))
	for _, h := range hits {
		entry, err := s.store.Get(ctx, h.ID)
		if err != nil {
			if errors.Is(err, lore.ErrNotFound) {
				// Vector index slightly ahead of store; skip stale reference.
				s.logger.WarnContext(ctx, "vector: entry not found in store; skipping", "id", h.ID)
				continue
			}
			return nil, fmt.Errorf("vector: hydrate entry %d: %w", h.ID, err)
		}
		// Filter by project when specified.
		if opts.Project != "" && entry.Project != opts.Project {
			continue
		}
		results = append(results, lore.SearchHit{Entry: entry, Score: h.Score})
	}

	return results, nil
}
