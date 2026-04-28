// Package vector defines the VectorStore interface and the types it uses.
// Implementations are dimension-bound at construction and accept a
// caller-managed *sql.DB (or equivalent). Callers own the database lifecycle;
// the VectorStore only owns the schema objects it creates inside that DB.
//
// The package ships one reference implementation: sqlitevec, a pure-Go
// SQLite-backed store that keeps vectors as BLOB columns and does cosine
// similarity in Go. That choice is documented in detail in
// pkg/lore/vector/sqlitevec/sqlitevec.go.
package vector

import (
	"context"
	"errors"

	"github.com/mathomhaus/lore/pkg/lore"
)

// VectorStore persists vectors keyed by entry ID and answers nearest-neighbor
// queries. Implementations are dimension-bound at construction. Caller-managed
// resources (consumer-owned *sql.DB or equivalent).
//
// SearchOpts carries Kind and Tag filter hints. VectorStore implementations
// are not required to honor them: the Retriever layer can post-filter results
// through Store.Get after Search returns. Reference implementations document
// whether they push filters into the query or post-filter.
type VectorStore interface {
	// Upsert stores the vector for the given entry ID. Replaces any existing
	// vector for that ID. The vector length must equal Dimensions();
	// ErrInvalidArgument is returned for a mismatch. The operation is
	// idempotent: calling Upsert twice with the same ID and vector is safe.
	Upsert(ctx context.Context, id int64, vector []float32) error

	// Delete removes the vector for the given entry ID. Returns ErrNotFound
	// when no vector is stored for that ID.
	Delete(ctx context.Context, id int64) error

	// Search returns the top-Limit vectors most similar to the query vector,
	// in descending score order. Higher Score means more similar. The query
	// vector length must equal Dimensions(); ErrInvalidArgument otherwise.
	//
	// Kind and Tag filters on SearchOpts are advisory hints. The reference
	// implementation ignores them and returns top-K by similarity alone;
	// callers that need filtered results should post-filter via Store.Get.
	Search(ctx context.Context, query []float32, opts SearchOpts) ([]Hit, error)

	// Dimensions returns the vector length this store was constructed for.
	// All Upsert and Search calls must supply vectors of exactly this length.
	Dimensions() int

	// Close releases any resources held by the store beyond the caller-owned
	// DB. Idempotent: calling Close more than once returns nil.
	Close(ctx context.Context) error
}

// Hit is one result from a Search call. ID is the entry's storage identifier
// as passed to Upsert. Score is the similarity score; higher is more similar.
// The scale is implementation-defined (cosine similarity in [-1, 1] for the
// reference impl) but always higher-is-better within a single query result.
type Hit struct {
	ID    int64
	Score float64
}

// SearchOpts configures a Search call.
type SearchOpts struct {
	// Limit is the maximum number of hits to return. Default 10 when zero.
	// Negative values are clamped to 1.
	Limit int

	// Kinds, when non-empty, hints that callers want only entries of these
	// kinds. VectorStore implementations are not required to apply this
	// filter; the Retriever layer post-filters via Store.Get when needed.
	Kinds []lore.Kind

	// Tags, when non-empty, hints that callers want only entries carrying all
	// of these tags. Same advisory semantics as Kinds.
	Tags []string
}

// Typed sentinel errors for caller-side branching. All error values returned
// by VectorStore implementations should wrap one of these so callers can use
// errors.Is without matching text.
var (
	// ErrNotFound is returned by Delete when the given entry ID has no stored
	// vector. Implementations wrap this via fmt.Errorf("...: %w", ErrNotFound).
	ErrNotFound = errors.New("vector: not found")

	// ErrInvalidArgument is returned when a caller-supplied argument fails
	// validation (dimension mismatch, negative limit, nil vector, etc.).
	ErrInvalidArgument = errors.New("vector: invalid argument")

	// ErrClosed is returned when an operation is attempted after Close.
	ErrClosed = errors.New("vector: closed")
)
