package store_test

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/store/sqlite"
)

// openMemoryDB opens an in-memory SQLite database suitable for examples.
// The caller must close both the Store and the *sql.DB when done.
func openMemoryDB() (*sql.DB, error) {
	return sql.Open("sqlite", ":memory:?_pragma=foreign_keys(ON)")
}

// ExampleNew_inscribeAndGet demonstrates the primary Store write-then-read
// cycle: inscribe a decision entry and retrieve it by ID.
func ExampleNew_inscribeAndGet() {
	ctx := context.Background()
	db, err := openMemoryDB()
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

	id, err := st.Inscribe(ctx, lore.Entry{
		Project: "decisionLog",
		Kind:    lore.KindDecision,
		Title:   "Adopt WAL mode for SQLite",
		Body:    "WAL mode allows concurrent readers while a single writer commits.",
		Tags:    []string{"adr", "storage"},
	})
	if err != nil {
		fmt.Println("inscribe:", err)
		return
	}

	entry, err := st.Get(ctx, id)
	if err != nil {
		fmt.Println("get:", err)
		return
	}

	fmt.Printf("kind=%s title=%q tags=%v\n", entry.Kind, entry.Title, entry.Tags)
	// Output:
	// kind=decision title="Adopt WAL mode for SQLite" tags=[adr storage]
}

// ExampleNew_searchText demonstrates full-text search over inscribed entries.
func ExampleNew_searchText() {
	ctx := context.Background()
	db, err := openMemoryDB()
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
		{
			Project: "runbookCorpus",
			Kind:    lore.KindProcedure,
			Title:   "Database failover runbook",
			Body:    "Steps to promote a replica when the primary is unavailable.",
			Tags:    []string{"runbook", "database"},
		},
		{
			Project: "runbookCorpus",
			Kind:    lore.KindProcedure,
			Title:   "Cache flush runbook",
			Body:    "Steps to safely flush and reload the Redis cache layer.",
			Tags:    []string{"runbook", "cache"},
		},
	}
	for _, e := range entries {
		if _, err := st.Inscribe(ctx, e); err != nil {
			fmt.Println("inscribe:", err)
			return
		}
	}

	hits, err := st.SearchText(ctx, "runbook", lore.SearchOpts{Limit: 5})
	if err != nil {
		fmt.Println("search:", err)
		return
	}

	fmt.Printf("found %d hit(s)\n", len(hits))
	// Output:
	// found 2 hit(s)
}

// ExampleNew_addEdge demonstrates persisting a typed edge between two entries
// and then listing it back.
func ExampleNew_addEdge() {
	ctx := context.Background()
	db, err := openMemoryDB()
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

	fromID, _ := st.Inscribe(ctx, lore.Entry{
		Kind:  lore.KindDecision,
		Title: "Use mTLS for service-to-service auth",
		Body:  "Mutual TLS prevents lateral movement inside the cluster.",
	})
	toID, _ := st.Inscribe(ctx, lore.Entry{
		Kind:  lore.KindProcedure,
		Title: "Rotate mTLS certificates",
		Body:  "Steps to generate and distribute new service certificates.",
	})

	if err := st.AddEdge(ctx, lore.Edge{
		FromID:   fromID,
		ToID:     toID,
		Relation: "informs",
	}); err != nil {
		fmt.Println("add edge:", err)
		return
	}

	edges, err := st.ListEdges(ctx, fromID)
	if err != nil {
		fmt.Println("list edges:", err)
		return
	}

	fmt.Printf("edges from %d: count=%d relation=%s\n", fromID, len(edges), edges[0].Relation)
	// Output:
	// edges from 1: count=1 relation=informs
}
