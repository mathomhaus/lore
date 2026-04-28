package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/store"
	"github.com/mathomhaus/lore/pkg/lore/store/sqlite"
)

// openMemoryStore opens a fresh :memory: SQLite database and returns a Store
// backed by it. The test owns both the Store and the *sql.DB; it must call
// st.Close then db.Close when done.
func openMemoryStore(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	st, err := sqlite.New(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("sqlite.New: %v", err)
	}
	return st, db
}

func closeStore(t *testing.T, st store.Store, db *sql.DB) {
	t.Helper()
	if err := st.Close(context.Background()); err != nil {
		t.Errorf("Store.Close: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("db.Close: %v", err)
	}
}

// sampleEntry returns a valid lore.Entry suitable for test inserts.
func sampleEntry(overrides ...func(*lore.Entry)) lore.Entry {
	e := lore.Entry{
		Project: "testproject",
		Kind:    lore.KindDecision,
		Title:   "Use SQLite for local storage",
		Body:    "We chose SQLite because it is embeddable and requires no separate process.",
		Source:  "https://example.com/adr-001",
		Tags:    []string{"adr", "storage"},
	}
	for _, fn := range overrides {
		fn(&e)
	}
	return e
}

// TestInscribe_Roundtrip verifies that inscribing an entry and fetching it by
// ID returns an entry with matching fields.
func TestInscribe_Roundtrip(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	in := sampleEntry()

	id, err := st.Inscribe(ctx, in)
	if err != nil {
		t.Fatalf("Inscribe: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != id {
		t.Errorf("ID: got %d, want %d", got.ID, id)
	}
	if got.Project != in.Project {
		t.Errorf("Project: got %q, want %q", got.Project, in.Project)
	}
	if got.Kind != in.Kind {
		t.Errorf("Kind: got %q, want %q", got.Kind, in.Kind)
	}
	if got.Title != in.Title {
		t.Errorf("Title: got %q, want %q", got.Title, in.Title)
	}
	if got.Body != in.Body {
		t.Errorf("Body: got %q, want %q", got.Body, in.Body)
	}
	if got.Source != in.Source {
		t.Errorf("Source: got %q, want %q", got.Source, in.Source)
	}
	if len(got.Tags) != len(in.Tags) {
		t.Errorf("Tags length: got %d, want %d", len(got.Tags), len(in.Tags))
	} else {
		for i := range in.Tags {
			if got.Tags[i] != in.Tags[i] {
				t.Errorf("Tags[%d]: got %q, want %q", i, got.Tags[i], in.Tags[i])
			}
		}
	}
}

// TestInscribe_RejectsInvalidKind verifies that an entry with an unknown kind
// returns ErrInvalidKind and no row is written.
func TestInscribe_RejectsInvalidKind(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	e := sampleEntry(func(e *lore.Entry) {
		e.Kind = "boguskind"
	})

	_, err := st.Inscribe(ctx, e)
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}
	if !errors.Is(err, lore.ErrInvalidKind) {
		t.Errorf("expected ErrInvalidKind, got %v", err)
	}
}

// TestInscribe_RejectsEmptyTitle verifies that an entry with an empty title
// returns ErrInvalidArgument.
func TestInscribe_RejectsEmptyTitle(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	e := sampleEntry(func(e *lore.Entry) { e.Title = "  " })

	_, err := st.Inscribe(ctx, e)
	if err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestUpdate_RoundTrip verifies that an update replaces entry fields.
func TestUpdate_RoundTrip(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	id, err := st.Inscribe(ctx, sampleEntry())
	if err != nil {
		t.Fatalf("Inscribe: %v", err)
	}

	updated := sampleEntry(func(e *lore.Entry) {
		e.ID = id
		e.Title = "Updated title"
		e.Body = "Updated body"
		e.Kind = lore.KindPrinciple
	})
	if err := st.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got.Title != "Updated title" {
		t.Errorf("Title: got %q, want %q", got.Title, "Updated title")
	}
	if got.Kind != lore.KindPrinciple {
		t.Errorf("Kind: got %q, want %q", got.Kind, lore.KindPrinciple)
	}
}

// TestUpdate_NotFound verifies that updating a non-existent ID returns
// ErrNotFound.
func TestUpdate_NotFound(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	e := sampleEntry(func(e *lore.Entry) { e.ID = 99999 })

	err := st.Update(ctx, e)
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, lore.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestGet_NotFound verifies that fetching a non-existent ID returns ErrNotFound.
func TestGet_NotFound(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	_, err := st.Get(ctx, 99999)
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, lore.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestDeleteBySource_Multiple verifies that all entries with a given source are
// deleted and the correct count is returned.
func TestDeleteBySource_Multiple(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	src := "https://example.com/shared-source"

	for i := 0; i < 3; i++ {
		e := sampleEntry(func(e *lore.Entry) { e.Source = src })
		if _, err := st.Inscribe(ctx, e); err != nil {
			t.Fatalf("Inscribe[%d]: %v", i, err)
		}
	}
	// One extra with a different source.
	other := sampleEntry(func(e *lore.Entry) { e.Source = "https://example.com/other" })
	otherID, err := st.Inscribe(ctx, other)
	if err != nil {
		t.Fatalf("Inscribe other: %v", err)
	}

	deleted, err := st.DeleteBySource(ctx, src)
	if err != nil {
		t.Fatalf("DeleteBySource: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted count: got %d, want 3", deleted)
	}

	// The other entry should still be accessible.
	if _, err := st.Get(ctx, otherID); err != nil {
		t.Errorf("Get other entry after delete: %v", err)
	}
}

// TestDeleteBySource_Empty verifies that an empty source returns
// ErrInvalidArgument.
func TestDeleteBySource_Empty(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	_, err := st.DeleteBySource(context.Background(), "")
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestListByTag_Filter verifies that only entries carrying the queried tag are
// returned and entries without the tag are excluded.
func TestListByTag_Filter(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()

	e1 := sampleEntry(func(e *lore.Entry) {
		e.Title = "Entry with adr tag"
		e.Tags = []string{"adr", "architecture"}
	})
	e2 := sampleEntry(func(e *lore.Entry) {
		e.Title = "Entry with only architecture tag"
		e.Tags = []string{"architecture"}
	})
	e3 := sampleEntry(func(e *lore.Entry) {
		e.Title = "Entry with no tags"
		e.Tags = nil
	})

	for _, e := range []lore.Entry{e1, e2, e3} {
		if _, err := st.Inscribe(ctx, e); err != nil {
			t.Fatalf("Inscribe: %v", err)
		}
	}

	hits, err := st.ListByTag(ctx, "adr", lore.ListOpts{})
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 result, got %d", len(hits))
	}
	if hits[0].Title != e1.Title {
		t.Errorf("Title: got %q, want %q", hits[0].Title, e1.Title)
	}
}

// TestListByKind_Filter verifies that only entries of the queried kind are
// returned.
func TestListByKind_Filter(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()

	decisions := []lore.Entry{
		sampleEntry(func(e *lore.Entry) { e.Kind = lore.KindDecision; e.Title = "Decision A" }),
		sampleEntry(func(e *lore.Entry) { e.Kind = lore.KindDecision; e.Title = "Decision B" }),
	}
	principles := []lore.Entry{
		sampleEntry(func(e *lore.Entry) { e.Kind = lore.KindPrinciple; e.Title = "Principle A" }),
	}

	for _, e := range append(decisions, principles...) {
		if _, err := st.Inscribe(ctx, e); err != nil {
			t.Fatalf("Inscribe: %v", err)
		}
	}

	got, err := st.ListByKind(ctx, lore.KindDecision, lore.ListOpts{})
	if err != nil {
		t.Fatalf("ListByKind: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(got))
	}
	for _, e := range got {
		if e.Kind != lore.KindDecision {
			t.Errorf("unexpected kind %q in results", e.Kind)
		}
	}
}

// TestListByKind_InvalidKind verifies that an invalid kind returns ErrInvalidKind.
func TestListByKind_InvalidKind(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	_, err := st.ListByKind(context.Background(), "notakind", lore.ListOpts{})
	if !errors.Is(err, lore.ErrInvalidKind) {
		t.Errorf("expected ErrInvalidKind, got %v", err)
	}
}

// TestSearchText_BM25Ranking verifies that a known-relevant document ranks
// above an unrelated document.
func TestSearchText_BM25Ranking(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()

	relevant := sampleEntry(func(e *lore.Entry) {
		e.Title = "SQLite full text search with BM25"
		e.Body = "BM25 ranking in SQLite FTS5 provides excellent relevance scoring for text retrieval."
	})
	unrelated := sampleEntry(func(e *lore.Entry) {
		e.Title = "Kubernetes deployment strategy"
		e.Body = "Rolling updates minimize downtime during deployments in kubernetes clusters."
	})

	for _, e := range []lore.Entry{relevant, unrelated} {
		if _, err := st.Inscribe(ctx, e); err != nil {
			t.Fatalf("Inscribe: %v", err)
		}
	}

	hits, err := st.SearchText(ctx, "BM25 SQLite", lore.SearchOpts{Limit: 10})
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit, got zero")
	}

	// The first (highest-scoring) hit should be the relevant document.
	if hits[0].Entry.Title != relevant.Title {
		t.Errorf("top hit title: got %q, want %q", hits[0].Entry.Title, relevant.Title)
	}

	// Scores must be positive (we negate bm25() which returns negative values).
	for i, h := range hits {
		if h.Score <= 0 {
			t.Errorf("hit[%d] score %f is not positive", i, h.Score)
		}
	}
}

// TestSearchText_EmptyQuery verifies that an empty query returns
// ErrInvalidArgument.
func TestSearchText_EmptyQuery(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	_, err := st.SearchText(context.Background(), "  ", lore.SearchOpts{})
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestAddEdge_RoundTrip verifies that an added edge is returned by ListEdges.
func TestAddEdge_RoundTrip(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	id1, err := st.Inscribe(ctx, sampleEntry(func(e *lore.Entry) { e.Title = "Entry One" }))
	if err != nil {
		t.Fatalf("Inscribe id1: %v", err)
	}
	id2, err := st.Inscribe(ctx, sampleEntry(func(e *lore.Entry) { e.Title = "Entry Two" }))
	if err != nil {
		t.Fatalf("Inscribe id2: %v", err)
	}

	edge := lore.Edge{
		FromID:   id1,
		ToID:     id2,
		Relation: "informs",
		Weight:   1.0,
	}
	if err := st.AddEdge(ctx, edge); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	edges, err := st.ListEdges(ctx, id1)
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].FromID != id1 {
		t.Errorf("FromID: got %d, want %d", edges[0].FromID, id1)
	}
	if edges[0].ToID != id2 {
		t.Errorf("ToID: got %d, want %d", edges[0].ToID, id2)
	}
	if edges[0].Relation != "informs" {
		t.Errorf("Relation: got %q, want %q", edges[0].Relation, "informs")
	}
}

// TestAddEdge_Idempotent verifies that adding the same edge twice is a no-op.
func TestAddEdge_Idempotent(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	id1, _ := st.Inscribe(ctx, sampleEntry(func(e *lore.Entry) { e.Title = "A" }))
	id2, _ := st.Inscribe(ctx, sampleEntry(func(e *lore.Entry) { e.Title = "B" }))

	edge := lore.Edge{FromID: id1, ToID: id2, Relation: "informs"}
	if err := st.AddEdge(ctx, edge); err != nil {
		t.Fatalf("first AddEdge: %v", err)
	}
	if err := st.AddEdge(ctx, edge); err != nil {
		t.Fatalf("second AddEdge (should be idempotent): %v", err)
	}

	edges, err := st.ListEdges(ctx, id1)
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Errorf("expected 1 edge after two identical adds, got %d", len(edges))
	}
}

// TestAddEdge_NotFound verifies that adding an edge with a non-existent entry
// returns ErrNotFound.
func TestAddEdge_NotFound(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	id1, _ := st.Inscribe(ctx, sampleEntry())

	err := st.AddEdge(ctx, lore.Edge{FromID: id1, ToID: 99999, Relation: "informs"})
	if !errors.Is(err, lore.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// TestListEdges_FromID verifies that edges are scoped to the given FromID.
func TestListEdges_FromID(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	ids := make([]int64, 3)
	for i := range ids {
		id, err := st.Inscribe(ctx, sampleEntry(func(e *lore.Entry) {
			e.Title = "Entry"
		}))
		if err != nil {
			t.Fatalf("Inscribe[%d]: %v", i, err)
		}
		ids[i] = id
	}

	// Add edges from ids[0] to ids[1] and ids[2].
	for _, toID := range ids[1:] {
		if err := st.AddEdge(ctx, lore.Edge{FromID: ids[0], ToID: toID, Relation: "informs"}); err != nil {
			t.Fatalf("AddEdge: %v", err)
		}
	}
	// Add edge from ids[1] to ids[2] — should NOT appear in ids[0] list.
	if err := st.AddEdge(ctx, lore.Edge{FromID: ids[1], ToID: ids[2], Relation: "informs"}); err != nil {
		t.Fatalf("AddEdge ids[1]->ids[2]: %v", err)
	}

	edges, err := st.ListEdges(ctx, ids[0])
	if err != nil {
		t.Fatalf("ListEdges: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("expected 2 edges from ids[0], got %d", len(edges))
	}
	for _, e := range edges {
		if e.FromID != ids[0] {
			t.Errorf("unexpected FromID %d in results", e.FromID)
		}
	}
}

// TestListEdges_InvalidFromID verifies that fromID <= 0 returns ErrInvalidArgument.
func TestListEdges_InvalidFromID(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	_, err := st.ListEdges(context.Background(), 0)
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument for fromID=0, got %v", err)
	}
}

// TestClose_Idempotent verifies that calling Close multiple times returns nil.
func TestClose_Idempotent(t *testing.T) {
	st, db := openMemoryStore(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := st.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := st.Close(ctx); err != nil {
		t.Errorf("second Close (should be idempotent): %v", err)
	}
}

// TestAfterClose_ReturnsErrClosed verifies that method calls after Close return
// ErrClosed.
func TestAfterClose_ReturnsErrClosed(t *testing.T) {
	st, db := openMemoryStore(t)
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := st.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := st.Inscribe(ctx, sampleEntry())
	if !errors.Is(err, lore.ErrClosed) {
		t.Errorf("Inscribe after Close: expected ErrClosed, got %v", err)
	}

	_, err = st.Get(ctx, 1)
	if !errors.Is(err, lore.ErrClosed) {
		t.Errorf("Get after Close: expected ErrClosed, got %v", err)
	}
}

// TestListByTag_EmptyTag verifies that an empty tag returns ErrInvalidArgument.
func TestListByTag_EmptyTag(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	_, err := st.ListByTag(context.Background(), "", lore.ListOpts{})
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestMetadata_RoundTrip verifies that metadata key-value pairs survive a
// write-read cycle.
func TestMetadata_RoundTrip(t *testing.T) {
	st, db := openMemoryStore(t)
	defer closeStore(t, st, db)

	ctx := context.Background()
	in := sampleEntry(func(e *lore.Entry) {
		e.Metadata = map[string]string{
			"component": "auth",
			"owner":     "platform-team",
		}
	})

	id, err := st.Inscribe(ctx, in)
	if err != nil {
		t.Fatalf("Inscribe: %v", err)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Metadata["component"] != "auth" {
		t.Errorf("metadata component: got %q, want %q", got.Metadata["component"], "auth")
	}
	if got.Metadata["owner"] != "platform-team" {
		t.Errorf("metadata owner: got %q, want %q", got.Metadata["owner"], "platform-team")
	}
}
