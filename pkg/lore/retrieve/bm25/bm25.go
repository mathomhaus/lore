// Package bm25 provides a lexical-only Retriever that delegates to
// Store.SearchText. It satisfies retrieve.Retriever and is composable
// as the BM25 arm of hybrid.New.
//
// The BM25 ranker does not run an embedding model; it is safe to use on
// platforms where ONNX Runtime is unavailable.
package bm25

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/store"
)

const tracerName = "lore.retrieve.bm25"

// Ranker is a Retriever backed by full-text (BM25) search only.
// Construct with New. Safe for concurrent use.
type Ranker struct {
	store  store.Store
	logger *slog.Logger
	tracer trace.Tracer
}

// Option configures a Ranker.
type Option func(*Ranker)

// WithLogger sets the structured logger for the Ranker.
// Defaults to slog.Default() when not provided.
func WithLogger(l *slog.Logger) Option {
	return func(r *Ranker) { r.logger = l }
}

// WithTracer sets the OpenTelemetry tracer.
// Defaults to the global tracer provider when not provided.
func WithTracer(t trace.Tracer) Option {
	return func(r *Ranker) { r.tracer = t }
}

// New returns a Ranker that delegates to store.SearchText.
// The caller owns store and must not call store.Close before Ranker is done.
func New(s store.Store, opts ...Option) *Ranker {
	r := &Ranker{store: s}
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

// Search runs a BM25 full-text search and returns ranked hits.
// Returns lore.ErrInvalidArgument when query is empty or opts.Limit is negative.
func (r *Ranker) Search(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error) {
	if query == "" {
		return nil, fmt.Errorf("bm25: search: %w", lore.ErrInvalidArgument)
	}
	if opts.Limit < 0 {
		return nil, fmt.Errorf("bm25: search: negative limit: %w", lore.ErrInvalidArgument)
	}

	ctx, span := r.tracer.Start(ctx, "lore.retrieve.bm25")
	defer span.End()

	hits, err := r.store.SearchText(ctx, query, opts)
	if err != nil {
		if !errors.Is(err, lore.ErrInvalidArgument) {
			r.logger.ErrorContext(ctx, "bm25: store.SearchText failed", "err", err)
		}
		return nil, fmt.Errorf("bm25: search: %w", err)
	}

	span.SetAttributes(attribute.Int("bm25.count", len(hits)))
	return hits, nil
}
