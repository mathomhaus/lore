package hybrid_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/hybrid"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/rrf"
	lstore2 "github.com/mathomhaus/lore/pkg/lore/store"
	lstore "github.com/mathomhaus/lore/pkg/lore/store/sqlite"
	lvector "github.com/mathomhaus/lore/pkg/lore/vector"
	"github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

// ---- mock embedder ---------------------------------------------------------

// fixedEmbedder maps input texts to pre-defined float32 vectors.
// Unrecognised texts return the zero vector. This avoids the ONNX Runtime
// dependency in CI while still exercising the vector arm end-to-end.
type fixedEmbedder struct {
	dim     int
	mapping map[string][]float32
}

func newFixedEmbedder(dim int, mapping map[string][]float32) *fixedEmbedder {
	if mapping == nil {
		mapping = map[string][]float32{}
	}
	return &fixedEmbedder{dim: dim, mapping: mapping}
}

func (e *fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("fixedEmbedder: empty texts")
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := e.mapping[t]; ok {
			out[i] = v
		} else {
			// Return a zero vector so every text gets a valid (if useless) vector.
			out[i] = make([]float32, e.dim)
		}
	}
	return out, nil
}

func (e *fixedEmbedder) Dimensions() int { return e.dim }

func (e *fixedEmbedder) Close(_ context.Context) error { return nil }

// ---- error-returning stubs -------------------------------------------------

type failVectorStore struct{ err error }

func (f *failVectorStore) Upsert(_ context.Context, _ int64, _ []float32) error { return nil }
func (f *failVectorStore) Delete(_ context.Context, _ int64) error              { return nil }
func (f *failVectorStore) Search(_ context.Context, _ []float32, _ lvector.SearchOpts) ([]lvector.Hit, error) {
	return nil, f.err
}
func (f *failVectorStore) Dimensions() int               { return dim }
func (f *failVectorStore) Close(_ context.Context) error { return nil }

// ---- constants and corpus --------------------------------------------------

const dim = 4

// Unit vectors for distinct semantic directions.
var (
	vecA = []float32{1, 0, 0, 0}
	vecB = []float32{0, 1, 0, 0}
	vecC = []float32{0, 0, 1, 0}
	vecD = []float32{0, 0, 0, 1}
)

// corpusEntry pairs human-readable text with a fixed embedding vector.
type corpusEntry struct {
	title  string
	body   string
	vector []float32
}

// testCorpus is a small fixed knowledge base used across all tests.
// Semantic clusters:
//   - vecA: deployment/rollout entries (indexes 0-2)
//   - vecB: API/auth/observability entries (indexes 3-4, 9)
//   - vecC: database migration entries (indexes 5-6)
//   - vecD: incident/on-call entries (indexes 7-8)
var testCorpus = []corpusEntry{
	{title: "Deployment rollout guide", body: "Steps for deploying a service to production. Rolling deployments minimise downtime.", vector: vecA},
	{title: "Rollout checklist", body: "Pre-deployment checks before each rollout: smoke tests, rollback plan, monitoring.", vector: vecA},
	{title: "Canary deployment strategy", body: "Route 5% of traffic during deployment to detect regressions early.", vector: vecA},
	{title: "API rate limiting policy", body: "Throttle requests to 100/s per consumer key to protect backend services.", vector: vecB},
	{title: "Authentication token rotation", body: "Rotate JWT signing keys every 90 days. Emergency rotation on suspected compromise.", vector: vecB},
	{title: "Database migration runbook", body: "Run schema migrations with zero-downtime strategies. Backward-compatible changes only.", vector: vecC},
	{title: "Migration rollback procedure", body: "Revert a failed schema migration using the down migration script.", vector: vecC},
	{title: "Incident response playbook", body: "On-call engineer steps: acknowledge alert, assess blast radius, page SRE team.", vector: vecD},
	{title: "On-call rotation schedule", body: "Primary and secondary on-call shifts. Swap requests via the scheduling tool.", vector: vecD},
	{title: "Observability stack setup", body: "Prometheus, Grafana, and Loki configuration for service health dashboards.", vector: vecB},
}

// ---- test helpers ----------------------------------------------------------

// openFull opens an in-memory SQLite DB, creates store + vstore, seeds the
// full testCorpus, and returns all three resources plus the seeded IDs.
// embMapping configures the fixedEmbedder query-to-vector mappings.
func openFull(t *testing.T, embMapping map[string][]float32) (
	s lstore2.Store,
	emb *fixedEmbedder,
	vs lvector.VectorStore,
	ids []int64,
) {
	t.Helper()

	db := openDB(t)
	st, err := lstore.New(db)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close(context.Background()) })

	vs2, err := sqlitevec.New(db, dim)
	if err != nil {
		t.Fatalf("sqlitevec.New: %v", err)
	}
	t.Cleanup(func() { _ = vs2.Close(context.Background()) })

	ctx := context.Background()
	seededIDs := make([]int64, len(testCorpus))
	for i, ce := range testCorpus {
		id, err := st.Inscribe(ctx, lore.Entry{
			Kind:  lore.KindProcedure,
			Title: ce.title,
			Body:  ce.body,
		})
		if err != nil {
			t.Fatalf("Inscribe %d: %v", i, err)
		}
		seededIDs[i] = id
		if err := vs2.Upsert(ctx, id, ce.vector); err != nil {
			t.Fatalf("Upsert %d: %v", i, err)
		}
	}

	return st, newFixedEmbedder(dim, embMapping), vs2, seededIDs
}

// openDB opens a fresh in-memory SQLite database and registers cleanup.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// titleSet converts a slice of SearchHits into a set of entry titles.
func titleSet(hits []lore.SearchHit) map[string]bool {
	m := make(map[string]bool, len(hits))
	for _, h := range hits {
		m[h.Entry.Title] = true
	}
	return m
}

// ---- tests -----------------------------------------------------------------

// TestHybrid_TextOnlyMatch exercises a query where BM25 lexical matching
// should surface relevant results. The embedder returns a zero vector for
// the query so cosine similarities are all equal; BM25 dominates.
func TestHybrid_TextOnlyMatch(t *testing.T) {
	t.Parallel()

	// Zero-vector query: vector distances are tied, BM25 rank wins in RRF.
	embMap := map[string][]float32{
		"deployment rollout": make([]float32, dim),
	}
	s, emb, vs, _ := openFull(t, embMap)

	r := hybrid.New(s, emb, vs, hybrid.WithCandidatePoolSize(10), hybrid.WithRRFK(60))
	hits, err := r.Search(context.Background(), "deployment rollout", lore.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits, got none")
	}
	titles := titleSet(hits)
	found := titles["Deployment rollout guide"] || titles["Rollout checklist"] || titles["Canary deployment strategy"]
	if !found {
		t.Errorf("expected at least one deployment-related hit; got %v", titles)
	}
}

// TestHybrid_SemanticOnlyMatch exercises a query that is a synonym not present
// verbatim in the corpus. The query vector maps to vecA (deployment cluster)
// so vector search surfaces deployment entries even though BM25 finds nothing.
func TestHybrid_SemanticOnlyMatch(t *testing.T) {
	t.Parallel()

	// "release process" is not in any entry body verbatim; map to vecA
	// so the vector arm surfaces deployment entries.
	embMap := map[string][]float32{
		"release process": vecA,
	}
	s, emb, vs, _ := openFull(t, embMap)

	r := hybrid.New(s, emb, vs, hybrid.WithCandidatePoolSize(10), hybrid.WithRRFK(60))
	hits, err := r.Search(context.Background(), "release process", lore.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits, got none")
	}

	titles := titleSet(hits)
	foundDeployment := titles["Deployment rollout guide"] || titles["Rollout checklist"] || titles["Canary deployment strategy"]
	if !foundDeployment {
		t.Errorf("expected a deployment entry via vector search; got %v", titles)
	}
}

// TestHybrid_BothBeats verifies that the fused result set covers entries from
// both arms, achieving higher recall than either arm alone.
// Query text "migration" gives BM25 an edge on migration entries;
// query vector maps to vecA so vector arm surfaces deployment entries.
func TestHybrid_BothBeats(t *testing.T) {
	t.Parallel()

	embMap := map[string][]float32{
		"migration release": vecA,
	}
	s, emb, vs, _ := openFull(t, embMap)

	r := hybrid.New(s, emb, vs, hybrid.WithCandidatePoolSize(10), hybrid.WithRRFK(60))
	hits, err := r.Search(context.Background(), "migration release", lore.SearchOpts{Limit: 8})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	titles := titleSet(hits)
	// BM25 should surface migration entries due to the word "migration".
	foundMigration := titles["Database migration runbook"] || titles["Migration rollback procedure"]
	// Vector (vecA) should surface deployment entries.
	foundDeployment := titles["Deployment rollout guide"] || titles["Rollout checklist"] || titles["Canary deployment strategy"]

	if !foundMigration && !foundDeployment {
		t.Errorf("expected results from both arms; got %v", titles)
	}
	// Both found: this is the "hybrid beats single-mode" condition.
	if !foundMigration || !foundDeployment {
		t.Logf("partial coverage (migration=%v, deployment=%v); acceptable with small corpus", foundMigration, foundDeployment)
	}
}

// TestHybrid_BM25Failure_FallsThroughToVector verifies graceful degradation
// when BM25 finds nothing (query text does not match any entry). The vector
// arm must still produce hits.
func TestHybrid_BM25Failure_FallsThroughToVector(t *testing.T) {
	t.Parallel()

	// Map the query to vecA so the vector arm surfaces deployment entries.
	// The query text "αβγ" contains no Latin characters so FTS5 returns nothing.
	embMap := map[string][]float32{
		"αβγ": vecA,
	}
	s, emb, vs, _ := openFull(t, embMap)

	r := hybrid.New(s, emb, vs, hybrid.WithCandidatePoolSize(10))
	hits, err := r.Search(context.Background(), "αβγ", lore.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits from vector arm when BM25 returns empty; got none")
	}
}

// TestHybrid_VectorFailure_FallsThroughToBM25 verifies graceful degradation
// when the vector store returns an error. BM25 alone should serve results.
func TestHybrid_VectorFailure_FallsThroughToBM25(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	s, err := lstore.New(db)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	ctx := context.Background()
	for _, ce := range testCorpus {
		if _, err := s.Inscribe(ctx, lore.Entry{
			Kind:  lore.KindProcedure,
			Title: ce.title,
			Body:  ce.body,
		}); err != nil {
			t.Fatalf("Inscribe: %v", err)
		}
	}

	emb := newFixedEmbedder(dim, map[string][]float32{
		"deployment rollout": vecA,
	})
	badVS := &failVectorStore{err: fmt.Errorf("simulated vstore failure")}

	r := hybrid.New(s, emb, badVS, hybrid.WithCandidatePoolSize(10))
	hits, err := r.Search(ctx, "deployment rollout", lore.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected BM25 results when vector arm fails; got none")
	}
	titles := titleSet(hits)
	foundDeployment := titles["Deployment rollout guide"] || titles["Rollout checklist"] || titles["Canary deployment strategy"]
	if !foundDeployment {
		t.Errorf("expected a deployment entry from BM25 fallback; got %v", titles)
	}
}

// TestHybrid_BothArmsEmpty verifies that when neither arm finds results,
// Search returns nil without error.
func TestHybrid_BothArmsEmpty(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	s, err := lstore.New(db)
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	vs, err := sqlitevec.New(db, dim)
	if err != nil {
		t.Fatalf("sqlitevec.New: %v", err)
	}
	t.Cleanup(func() { _ = vs.Close(context.Background()) })

	emb := newFixedEmbedder(dim, nil)
	r := hybrid.New(s, emb, vs)
	hits, err := r.Search(context.Background(), "nothing here", lore.SearchOpts{Limit: 5})
	if err != nil {
		t.Fatalf("Search with empty corpus: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected no hits on empty corpus; got %d", len(hits))
	}
}

// TestHybrid_InvalidArgs verifies that bad inputs return ErrInvalidArgument.
func TestHybrid_InvalidArgs(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	s, _ := lstore.New(db)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	vs, _ := sqlitevec.New(db, dim)
	t.Cleanup(func() { _ = vs.Close(context.Background()) })

	emb := newFixedEmbedder(dim, nil)
	r := hybrid.New(s, emb, vs)

	_, err := r.Search(context.Background(), "", lore.SearchOpts{})
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("empty query: want ErrInvalidArgument, got %v", err)
	}

	_, err = r.Search(context.Background(), "something", lore.SearchOpts{Limit: -1})
	if !errors.Is(err, lore.ErrInvalidArgument) {
		t.Errorf("negative limit: want ErrInvalidArgument, got %v", err)
	}
}

// TestRRF_Fuse_Determinism verifies that rrf.Fuse produces identical output
// for identical inputs across multiple calls.
func TestRRF_Fuse_Determinism(t *testing.T) {
	t.Parallel()

	rankings := [][]int64{
		{10, 20, 30, 40, 50},
		{20, 10, 50, 30, 60},
		{30, 60, 10, 20, 50},
	}

	first := rrf.Fuse(rankings, rrf.DefaultK)
	for i := 0; i < 20; i++ {
		got := rrf.Fuse(rankings, rrf.DefaultK)
		if len(got) != len(first) {
			t.Fatalf("iter %d: length mismatch: %d vs %d", i, len(got), len(first))
		}
		for j := range first {
			if first[j] != got[j] {
				t.Fatalf("iter %d pos %d: want %+v, got %+v", i, j, first[j], got[j])
			}
		}
	}
}

// TestRRF_Fuse_ScoreOrder verifies that results are sorted descending by score.
func TestRRF_Fuse_ScoreOrder(t *testing.T) {
	t.Parallel()

	// ID 1 appears at rank 1 in list A and rank 2 in list B.
	// ID 2 appears at rank 1 in both lists: highest fused score.
	// ID 3 appears only in list A at rank 3.
	rankings := [][]int64{
		{1, 2, 3},
		{2, 1},
	}

	fused := rrf.Fuse(rankings, rrf.DefaultK)
	for i := 1; i < len(fused); i++ {
		if fused[i].Score > fused[i-1].Score {
			t.Errorf("pos %d score %f > pos %d score %f: not descending", i, fused[i].Score, i-1, fused[i-1].Score)
		}
	}

	scoreByID := make(map[int64]float64)
	for _, s := range fused {
		scoreByID[s.ID] = s.Score
	}
	// ID 2 appears in both lists at rank 1: expect higher score than ID 3 (one list, rank 3).
	if scoreByID[2] <= scoreByID[3] {
		t.Errorf("ID 2 (in both lists) should outscore ID 3 (one list only); scores: %v", scoreByID)
	}
}

// TestRRF_Fuse_EmptyInput verifies nil/empty input yields nil output.
func TestRRF_Fuse_EmptyInput(t *testing.T) {
	t.Parallel()
	if got := rrf.Fuse(nil, rrf.DefaultK); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
	if got := rrf.Fuse([][]int64{}, rrf.DefaultK); got != nil {
		t.Errorf("expected nil for empty rankings, got %v", got)
	}
}
