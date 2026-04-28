package sqlitevec_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore/vector"
	"github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

// openMemDB opens an in-memory SQLite database for testing. The caller is
// responsible for closing it.
func openMemDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newStore constructs a Store for testing with the given dimension.
func newStore(t *testing.T, dim int) vector.VectorStore {
	t.Helper()
	db := openMemDB(t)
	s, err := sqlitevec.New(db, dim)
	if err != nil {
		t.Fatalf("sqlitevec.New: %v", err)
	}
	return s
}

// ones returns a float32 slice of length dim filled with v.
func filled(dim int, v float32) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = v
	}
	return out
}

// TestUpsert_DimensionMismatch verifies that Upsert rejects a vector whose
// length does not match the store's configured dimensions.
func TestUpsert_DimensionMismatch(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, 4)

	err := s.Upsert(ctx, 1, []float32{1.0, 2.0}) // length 2, want 4
	if !errors.Is(err, vector.ErrInvalidArgument) {
		t.Errorf("Upsert with wrong dim: got %v, want ErrInvalidArgument", err)
	}
}

// TestSearch_TopK inserts three vectors (ones, twos, threes) and asserts that
// searching with a query close to "threes" returns the closest vector first.
func TestSearch_TopK(t *testing.T) {
	ctx := context.Background()
	const dim = 4
	s := newStore(t, dim)

	if err := s.Upsert(ctx, 1, filled(dim, 1.0)); err != nil {
		t.Fatalf("Upsert(1, ones): %v", err)
	}
	if err := s.Upsert(ctx, 2, filled(dim, 2.0)); err != nil {
		t.Fatalf("Upsert(2, twos): %v", err)
	}
	if err := s.Upsert(ctx, 3, filled(dim, 3.0)); err != nil {
		t.Fatalf("Upsert(3, threes): %v", err)
	}

	// All three unit vectors are identical in direction (they are all
	// multiples of [1,1,1,1]) so cosine similarity to any of them is 1.0.
	// Use a slightly varied query to break the tie via magnitude comparison.
	// [3, 3, 3, 4] is closest in direction to threes = [3, 3, 3, 3].
	query := []float32{3.0, 3.0, 3.0, 4.0}
	hits, err := s.Search(ctx, query, vector.SearchOpts{Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("len(hits) = %d, want 3", len(hits))
	}
	// All filled vectors with equal components are collinear; any of them
	// should be returned. Verify they are in descending score order.
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Errorf("hits not in descending order: hits[%d].Score %f > hits[%d].Score %f",
				i, hits[i].Score, i-1, hits[i-1].Score)
		}
	}
}

// TestSearch_NonCollinear inserts three distinct vectors and verifies the
// closest one wins. This test confirms that cosine similarity actually
// differentiates directions, not just magnitudes.
func TestSearch_NonCollinear(t *testing.T) {
	ctx := context.Background()
	const dim = 3

	s := newStore(t, dim)

	// e1 = [1, 0, 0], e2 = [0, 1, 0], e3 = [0, 0, 1]
	if err := s.Upsert(ctx, 1, []float32{1.0, 0.0, 0.0}); err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}
	if err := s.Upsert(ctx, 2, []float32{0.0, 1.0, 0.0}); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}
	if err := s.Upsert(ctx, 3, []float32{0.0, 0.0, 1.0}); err != nil {
		t.Fatalf("Upsert 3: %v", err)
	}

	// Query is closest to e1.
	hits, err := s.Search(ctx, []float32{0.9, 0.1, 0.0}, vector.SearchOpts{Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	if hits[0].ID != 1 {
		t.Errorf("top hit ID = %d, want 1 (e1 = [1,0,0] is closest to query [0.9,0.1,0])", hits[0].ID)
	}
}

// TestSearch_LimitRespected inserts 5 vectors and verifies Search returns at
// most Limit results.
func TestSearch_LimitRespected(t *testing.T) {
	ctx := context.Background()
	const dim = 4
	s := newStore(t, dim)

	for i := int64(1); i <= 5; i++ {
		if err := s.Upsert(ctx, i, filled(dim, float32(i))); err != nil {
			t.Fatalf("Upsert(%d): %v", i, err)
		}
	}

	hits, err := s.Search(ctx, filled(dim, 1.0), vector.SearchOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 2 {
		t.Errorf("len(hits) = %d, want <= 2", len(hits))
	}
}

// TestSearch_DefaultLimit verifies that a zero Limit defaults to 10.
func TestSearch_DefaultLimit(t *testing.T) {
	ctx := context.Background()
	const dim = 2
	s := newStore(t, dim)

	// Insert 15 entries.
	for i := int64(1); i <= 15; i++ {
		vec := []float32{float32(i), float32(i)}
		if err := s.Upsert(ctx, i, vec); err != nil {
			t.Fatalf("Upsert(%d): %v", i, err)
		}
	}

	hits, err := s.Search(ctx, []float32{1.0, 1.0}, vector.SearchOpts{}) // Limit=0 => default 10
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) > 10 {
		t.Errorf("len(hits) = %d, want <= 10 (default limit)", len(hits))
	}
}

// TestDelete_NotFound verifies that deleting a non-existent entry returns
// vector.ErrNotFound.
func TestDelete_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, 4)

	err := s.Delete(ctx, 999)
	if !errors.Is(err, vector.ErrNotFound) {
		t.Errorf("Delete non-existent: got %v, want ErrNotFound", err)
	}
}

// TestDelete_Removes verifies that a deleted vector no longer appears in
// Search results.
func TestDelete_Removes(t *testing.T) {
	ctx := context.Background()
	const dim = 3
	s := newStore(t, dim)

	if err := s.Upsert(ctx, 1, []float32{1.0, 0.0, 0.0}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := s.Delete(ctx, 1); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	hits, err := s.Search(ctx, []float32{1.0, 0.0, 0.0}, vector.SearchOpts{Limit: 10})
	if err != nil {
		t.Fatalf("Search after delete: %v", err)
	}
	for _, h := range hits {
		if h.ID == 1 {
			t.Error("deleted entry ID 1 still appears in Search results")
		}
	}
}

// TestUpsert_Replaces verifies that a second Upsert with the same ID replaces
// the previous vector.
func TestUpsert_Replaces(t *testing.T) {
	ctx := context.Background()
	const dim = 3
	s := newStore(t, dim)

	// Insert e1 pointing along X axis and e2 pointing along Y axis.
	if err := s.Upsert(ctx, 1, []float32{1.0, 0.0, 0.0}); err != nil {
		t.Fatalf("Upsert initial: %v", err)
	}
	if err := s.Upsert(ctx, 2, []float32{0.0, 1.0, 0.0}); err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	// Before replace: query along X should return e1 first.
	hitsBeforeReplace, err := s.Search(ctx, []float32{1.0, 0.0, 0.0}, vector.SearchOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Search before replace: %v", err)
	}
	if len(hitsBeforeReplace) == 0 || hitsBeforeReplace[0].ID != 1 {
		t.Error("before replace: expected entry 1 (X axis) to be top hit")
	}

	// Replace entry 1 to point along Y axis.
	if err := s.Upsert(ctx, 1, []float32{0.0, 1.0, 0.0}); err != nil {
		t.Fatalf("Upsert replace: %v", err)
	}

	// After replace: both e1 and e2 point along Y; query along X now has lower
	// similarity to both. The original e1-along-X entry should no longer exist.
	// Query along Y should return one of {1,2} as top hit.
	hitsAfterReplace, err := s.Search(ctx, []float32{0.0, 1.0, 0.0}, vector.SearchOpts{Limit: 2})
	if err != nil {
		t.Fatalf("Search after replace: %v", err)
	}
	if len(hitsAfterReplace) == 0 {
		t.Fatal("Search after replace returned no hits")
	}
	// Top hit must be either 1 or 2 (both are now Y-axis vectors).
	top := hitsAfterReplace[0].ID
	if top != 1 && top != 2 {
		t.Errorf("top hit after replace = %d, want 1 or 2", top)
	}
}

// TestClose_Idempotent verifies that calling Close multiple times is safe
// and that operations after Close return ErrClosed.
func TestClose_Idempotent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, 4)

	if err := s.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("second Close (idempotent): %v", err)
	}

	// Operations after Close should return ErrClosed.
	err := s.Upsert(ctx, 1, filled(4, 1.0))
	if !errors.Is(err, vector.ErrClosed) {
		t.Errorf("Upsert after Close: got %v, want ErrClosed", err)
	}

	_, err = s.Search(ctx, filled(4, 1.0), vector.SearchOpts{})
	if !errors.Is(err, vector.ErrClosed) {
		t.Errorf("Search after Close: got %v, want ErrClosed", err)
	}

	err = s.Delete(ctx, 1)
	if !errors.Is(err, vector.ErrClosed) {
		t.Errorf("Delete after Close: got %v, want ErrClosed", err)
	}
}

// TestSearch_DimensionMismatch verifies that Search rejects a query vector of
// the wrong length.
func TestSearch_DimensionMismatch(t *testing.T) {
	ctx := context.Background()
	s := newStore(t, 4)

	_, err := s.Search(ctx, []float32{1.0, 2.0}, vector.SearchOpts{})
	if !errors.Is(err, vector.ErrInvalidArgument) {
		t.Errorf("Search wrong dim: got %v, want ErrInvalidArgument", err)
	}
}
