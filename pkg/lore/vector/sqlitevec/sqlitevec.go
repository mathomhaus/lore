// Package sqlitevec is a SQLite-backed reference implementation of
// vector.VectorStore. Vectors are stored as BLOB columns (little-endian
// float32 bytes) and similarity search runs entirely in Go using cosine
// similarity over a full table scan.
//
// # Implementation choice: pure-Go Path A
//
// Guild's codebase confirmed that sqlite-vec extension loading is non-trivial
// under modernc.org/sqlite (pure-Go) and risks CGO leakage. This package
// therefore uses Path A: raw BLOB storage + Go-side cosine similarity. At
// v0.1.1 corpus sizes (up to ~100K vectors with 384 dimensions) a full linear
// scan completes in well under 50ms on modern hardware. The guild embed/index
// benchmarks (index_bench_test.go) measured ~22ms at 100K on Apple M3 Pro
// with int8-quantized vectors; float32 scans are ~5x slower, placing 100K at
// ~100ms, which remains acceptable. Document the 100K scale limit in Search.
//
// The package name "sqlitevec" refers to SQLite-backed vector storage, not to
// the sqlite-vec extension. A future contributor can add a true sqlite-vec
// extension impl as a separate package (e.g., pkg/lore/vector/sqlitevecext)
// once modernc.org/sqlite cleanly supports extension loading.
//
// # Thread safety
//
// Store is safe for concurrent use. The caller-owned *sql.DB handles its own
// connection pooling; this package does not add additional locking.
package sqlitevec

import (
	"container/heap"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore/vector"
)

const defaultSearchLimit = 10

// Store is the SQLite-backed VectorStore. Construct via New; do not use
// struct literal initialization.
type Store struct {
	db         *sql.DB
	dimensions int
	log        *slog.Logger
	tracer     trace.Tracer

	closedMu sync.Mutex
	closed   bool
}

// Option is a functional option for New.
type Option func(*Store)

// WithLogger attaches a custom slog.Logger. Without this the store uses
// slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(s *Store) {
		if l != nil {
			s.log = l
		}
	}
}

// WithTracer attaches an OpenTelemetry Tracer. Without this the store uses
// trace.NewNoopTracerProvider().Tracer("").
func WithTracer(t trace.Tracer) Option {
	return func(s *Store) {
		if t != nil {
			s.tracer = t
		}
	}
}

// New returns a VectorStore backed by db. The caller owns db and is
// responsible for closing it; New does not take ownership.
//
// dimensions binds the store to a fixed vector length. Every Upsert and Search
// call must supply vectors of exactly this length; mismatches return
// vector.ErrInvalidArgument.
//
// New runs schema migrations (creates the vectors table if absent) and returns
// an error if the DB is unreachable or the migration fails.
func New(db *sql.DB, dimensions int, opts ...Option) (vector.VectorStore, error) {
	if db == nil {
		return nil, fmt.Errorf("sqlitevec: New: nil *sql.DB")
	}
	if dimensions <= 0 {
		return nil, fmt.Errorf("sqlitevec: New: dimensions must be positive, got %d: %w", dimensions, vector.ErrInvalidArgument)
	}

	s := &Store{
		db:         db,
		dimensions: dimensions,
		log:        slog.Default(),
		tracer:     trace.NewNoopTracerProvider().Tracer(""),
	}
	for _, o := range opts {
		o(s)
	}

	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("sqlitevec: migrate: %w", err)
	}
	return s, nil
}

// migrate creates the vectors table if it does not already exist.
func (s *Store) migrate() error {
	_, err := s.db.Exec(schemaSQL) //nolint:sqlcheck // schemaSQL is a compile-time constant DDL statement
	if err != nil {
		return fmt.Errorf("create vectors table: %w", err)
	}
	return nil
}

// Dimensions returns the vector length this store was configured for.
func (s *Store) Dimensions() int {
	return s.dimensions
}

// Close marks the store as closed. It does not close the caller-owned *sql.DB.
// Idempotent.
func (s *Store) Close(_ context.Context) error {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	s.closed = true
	return nil
}

func (s *Store) isClosed() bool {
	s.closedMu.Lock()
	defer s.closedMu.Unlock()
	return s.closed
}

// Upsert stores the vector for the given entry ID, replacing any existing
// vector. The vector must have length equal to Dimensions(); otherwise
// vector.ErrInvalidArgument is returned.
//
// The vector is encoded as little-endian float32 bytes: each element occupies
// 4 bytes (IEEE 754 binary32). Total BLOB size is len(vec)*4 bytes.
func (s *Store) Upsert(ctx context.Context, id int64, vec []float32) error {
	ctx, span := s.tracer.Start(ctx, "lore.vector.upsert",
		trace.WithAttributes(
			attribute.Int64("lore.id", id),
			attribute.Int("lore.dim", len(vec)),
		),
	)
	defer span.End()

	if s.isClosed() {
		return vector.ErrClosed
	}
	if len(vec) != s.dimensions {
		s.log.WarnContext(ctx, "sqlitevec: Upsert dimension mismatch",
			"entry_id", id,
			"got", len(vec),
			"want", s.dimensions,
		)
		return fmt.Errorf("sqlitevec: Upsert: got %d dimensions, want %d: %w",
			len(vec), s.dimensions, vector.ErrInvalidArgument)
	}

	blob, err := encodeVec(vec)
	if err != nil {
		s.log.ErrorContext(ctx, "sqlitevec: Upsert encode failed", "entry_id", id, "err", err)
		return fmt.Errorf("sqlitevec: Upsert: encode: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO vectors (entry_id, dim, vec, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(entry_id) DO UPDATE SET
		     dim        = excluded.dim,
		     vec        = excluded.vec,
		     updated_at = excluded.updated_at`, //nolint:sqlcheck // all values are parameterized
		id, s.dimensions, blob, now,
	)
	if err != nil {
		return fmt.Errorf("sqlitevec: Upsert: exec: %w", err)
	}
	return nil
}

// Delete removes the vector for the given entry ID. Returns vector.ErrNotFound
// when no vector exists for that ID.
func (s *Store) Delete(ctx context.Context, id int64) error {
	ctx, span := s.tracer.Start(ctx, "lore.vector.delete",
		trace.WithAttributes(attribute.Int64("lore.id", id)),
	)
	defer span.End()

	if s.isClosed() {
		return vector.ErrClosed
	}

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM vectors WHERE entry_id = ?`, //nolint:sqlcheck // parameterized
		id,
	)
	if err != nil {
		return fmt.Errorf("sqlitevec: Delete: exec: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlitevec: Delete: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("sqlitevec: Delete: entry_id %d: %w", id, vector.ErrNotFound)
	}
	return nil
}

// Search returns the top-Limit vectors most similar to the query vector, in
// descending score order. Score is cosine similarity in [-1.0, 1.0]; higher
// is more similar.
//
// The query vector must have length equal to Dimensions(); otherwise
// vector.ErrInvalidArgument is returned.
//
// Kind and Tag filters in SearchOpts are not applied by this implementation:
// they are advisory hints for the Retriever layer, which post-filters results
// via Store.Get. This keeps VectorStore dependency-free with respect to the
// entry schema.
//
// Scale limit: Search performs a full table scan, loading all vector BLOBs and
// computing cosine similarity in Go. At ~100K vectors of 384 dimensions this
// costs roughly 100ms on a modern laptop. Above that threshold consider a
// purpose-built ANN index (pgvector, qdrant, or a true sqlite-vec extension
// implementation).
func (s *Store) Search(ctx context.Context, query []float32, opts vector.SearchOpts) ([]Hit, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit < 1 {
		limit = 1
	}

	ctx, span := s.tracer.Start(ctx, "lore.vector.search",
		trace.WithAttributes(
			attribute.Int("lore.dim", len(query)),
			attribute.Int("lore.limit", limit),
		),
	)
	defer span.End()

	if s.isClosed() {
		return nil, vector.ErrClosed
	}
	if len(query) != s.dimensions {
		s.log.WarnContext(ctx, "sqlitevec: Search dimension mismatch",
			"got", len(query),
			"want", s.dimensions,
		)
		return nil, fmt.Errorf("sqlitevec: Search: got %d dimensions, want %d: %w",
			len(query), s.dimensions, vector.ErrInvalidArgument)
	}

	// Full scan: load all entry_id + vec rows. Acceptable at v0.1.1 scale.
	rows, err := s.db.QueryContext(ctx,
		`SELECT entry_id, vec FROM vectors`, //nolint:sqlcheck // no user input; full scan by design
	)
	if err != nil {
		return nil, fmt.Errorf("sqlitevec: Search: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	h := &hitHeap{}
	heap.Init(h)

	var candidates int
	for rows.Next() {
		var (
			id   int64
			blob []byte
		)
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("sqlitevec: Search: scan: %w", err)
		}

		vec, err := decodeVec(blob, s.dimensions)
		if err != nil {
			s.log.WarnContext(ctx, "sqlitevec: Search: skipping malformed BLOB",
				"entry_id", id,
				"blob_len", len(blob),
				"want_bytes", s.dimensions*4,
				"err", err,
			)
			continue
		}
		candidates++

		score := cosine(query, vec)
		item := Hit{ID: id, Score: score}

		if h.Len() < limit {
			heap.Push(h, item)
		} else if score > (*h)[0].Score {
			heap.Pop(h)
			heap.Push(h, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlitevec: Search: iterate: %w", err)
	}

	span.SetAttributes(attribute.Int("lore.candidates", candidates))

	// Drain heap in descending order.
	result := make([]Hit, h.Len())
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = heap.Pop(h).(Hit)
	}
	return result, nil
}

// encodeVec serializes a []float32 to little-endian IEEE 754 bytes.
// Each element occupies 4 bytes; total length is len(v)*4.
func encodeVec(v []float32) ([]byte, error) {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf, nil
}

// decodeVec deserializes a little-endian IEEE 754 BLOB to []float32.
// Returns an error if the BLOB length is not exactly dim*4.
func decodeVec(buf []byte, dim int) ([]float32, error) {
	want := dim * 4
	if len(buf) != want {
		return nil, fmt.Errorf("decode: got %d bytes, want %d (dim=%d)", len(buf), want, dim)
	}
	v := make([]float32, dim)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
	}
	return v, nil
}

// cosine returns the cosine similarity between a and b in [-1.0, 1.0].
// Both slices must be the same length; caller guarantees this (Search does).
// Returns 0 when either vector has zero magnitude to avoid NaN propagation.
func cosine(a, b []float32) float64 {
	var dot, magA, magB float64
	for i := range a {
		ai := float64(a[i])
		bi := float64(b[i])
		dot += ai * bi
		magA += ai * ai
		magB += bi * bi
	}
	if magA == 0 || magB == 0 {
		return 0
	}
	return dot / (math.Sqrt(magA) * math.Sqrt(magB))
}

// Hit re-exports vector.Hit so the heap operates on the concrete type
// without an import cycle. The public Search method returns []vector.Hit.
type Hit = vector.Hit

// hitHeap is a min-heap of Hits keyed by Score. It is used by Search to
// maintain the running top-K without allocating O(n) result memory.
// container/heap requires a slice-backed heap; we implement the five methods.
type hitHeap []Hit

func (h hitHeap) Len() int           { return len(h) }
func (h hitHeap) Less(i, j int) bool { return h[i].Score < h[j].Score } // min on top
func (h hitHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *hitHeap) Push(x any) {
	*h = append(*h, x.(Hit))
}

func (h *hitHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
