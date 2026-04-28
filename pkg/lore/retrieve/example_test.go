package retrieve_test

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/bm25"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/rrf"
	"github.com/mathomhaus/lore/pkg/lore/store/sqlite"
)

// Example_bm25Search demonstrates lexical-only search using the BM25 ranker.
// This path requires no embedder and works on every platform.
func Example_bm25Search() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
	if err != nil {
		fmt.Println("open db:", err)
		return
	}
	defer db.Close()

	st, err := sqlite.New(db)
	if err != nil {
		fmt.Println("sqlite.New:", err)
		return
	}
	defer st.Close(ctx)

	entries := []lore.Entry{
		{Kind: lore.KindProcedure, Title: "Rollout runbook", Body: "Step-by-step deployment procedure for production services."},
		{Kind: lore.KindDecision, Title: "Deployment strategy decision", Body: "Rationale for choosing canary deployment over blue-green."},
		{Kind: lore.KindReference, Title: "Kubernetes resource limits", Body: "CPU and memory limit recommendations per service tier."},
	}
	for _, e := range entries {
		if _, err := st.Inscribe(ctx, e); err != nil {
			fmt.Println("inscribe:", err)
			return
		}
	}

	r := bm25.New(st)
	hits, err := r.Search(ctx, "deployment", lore.SearchOpts{Limit: 5})
	if err != nil {
		fmt.Println("search:", err)
		return
	}

	fmt.Printf("found %d hit(s) for 'deployment'\n", len(hits))
	// Output:
	// found 2 hit(s) for 'deployment'
}

// ExampleFuse demonstrates the rrf.Fuse function directly. Two separate rankers
// produce independent ranked lists; Fuse combines them into a single order.
// Documents appearing in both lists accumulate higher scores than those
// appearing in only one.
func ExampleFuse() {
	// Ranker A (BM25): top result is entry 100, then 200.
	rankA := []int64{100, 200}
	// Ranker B (vector): top result is entry 200, then 300.
	rankB := []int64{200, 300}

	fused := rrf.Fuse([][]int64{rankA, rankB}, rrf.DefaultK)

	// Entry 200 appears in both lists so it accumulates the highest fused score.
	for _, s := range fused {
		fmt.Printf("id=%d\n", s.ID)
	}
	// Output:
	// id=200
	// id=100
	// id=300
}
