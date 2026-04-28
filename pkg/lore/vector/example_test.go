package vector_test

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore/vector"
	"github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

// ExampleNew_upsertAndSearch demonstrates the core VectorStore cycle:
// store two vectors and then query for the nearest neighbor.
func ExampleNew_upsertAndSearch() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Println("open db:", err)
		return
	}
	defer db.Close()

	const dim = 4
	vs, err := sqlitevec.New(db, dim)
	if err != nil {
		fmt.Println("sqlitevec.New:", err)
		return
	}
	defer vs.Close(ctx)

	// Two unit vectors pointing in different directions.
	vecA := []float32{1, 0, 0, 0}
	vecB := []float32{0, 1, 0, 0}

	if err := vs.Upsert(ctx, 1, vecA); err != nil {
		fmt.Println("upsert 1:", err)
		return
	}
	if err := vs.Upsert(ctx, 2, vecB); err != nil {
		fmt.Println("upsert 2:", err)
		return
	}

	// Query with vecA: entry 1 should be the top hit (cosine sim = 1.0).
	hits, err := vs.Search(ctx, vecA, vector.SearchOpts{Limit: 2})
	if err != nil {
		fmt.Println("search:", err)
		return
	}

	fmt.Printf("top hit id=%d\n", hits[0].ID)
	// Output:
	// top hit id=1
}

// ExampleNew_delete shows that deleting a vector removes it from
// future search results.
func ExampleNew_delete() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		fmt.Println("open db:", err)
		return
	}
	defer db.Close()

	vs, err := sqlitevec.New(db, 2)
	if err != nil {
		fmt.Println("sqlitevec.New:", err)
		return
	}
	defer vs.Close(ctx)

	_ = vs.Upsert(ctx, 10, []float32{1, 0})

	// Delete the vector, then verify it is gone.
	if err := vs.Delete(ctx, 10); err != nil {
		fmt.Println("delete:", err)
		return
	}

	hits, _ := vs.Search(ctx, []float32{1, 0}, vector.SearchOpts{Limit: 5})
	fmt.Printf("hits after delete: %d\n", len(hits))
	// Output:
	// hits after delete: 0
}
