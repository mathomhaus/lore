# lore

A structured knowledge primitive for AI agents. Apache 2.0 OSS Go library.

Lore stores classified knowledge entries (decisions, principles, procedures,
references, explanations, observations, research, ideas) and the typed edges
that connect them, then serves them back to retrieval pipelines that combine
lexical and semantic ranking. It ships as a Go library, not a service: callers
compose it into their own MCP servers, HTTP services, ingestion pipelines, or
CLI tools.

The library is built around three pluggable interfaces (`Store`, `Embedder`,
`VectorStore`) plus a composing `Retriever` and an optional `Ingester`. Each
interface ships with an in-process reference implementation (modernc.org/sqlite,
BGE int8, sqlite-vec) so a single binary can run against a local SQLite file
out of the box. Swap any of the three for Postgres, a remote embedding API,
pgvector, or anything else by implementing the interface.

## Install

```
go get github.com/mathomhaus/lore@latest
```

Requires Go 1.23 or newer.

## Usage

### Store: persist and retrieve entries

The `store.Store` interface is the primary write and read surface. Open a
`*sql.DB` with the `"sqlite"` driver (registered by `modernc.org/sqlite`),
pass it to `sqlite.New`, and the constructor runs schema migrations
automatically.

```go
import (
    "context"
    "database/sql"
    "fmt"

    _ "modernc.org/sqlite"

    "github.com/mathomhaus/lore/pkg/lore"
    "github.com/mathomhaus/lore/pkg/lore/store/sqlite"
)

func main() {
    dsn := "lore.db" +
        "?_pragma=journal_mode(WAL)" +
        "&_pragma=busy_timeout(5000)" +
        "&_pragma=synchronous(NORMAL)" +
        "&_pragma=foreign_keys(ON)"

    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        panic(err)
    }
    defer db.Close()

    st, err := sqlite.New(db)
    if err != nil {
        panic(err)
    }
    defer st.Close(context.Background())

    // Persist a decision.
    id, err := st.Inscribe(context.Background(), lore.Entry{
        Project: "myproject",
        Kind:    lore.KindDecision,
        Title:   "Use SQLite for local persistence",
        Body:    "Chosen for zero-dependency deployment and strong ACID guarantees.",
        Tags:    []string{"adr", "storage"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("inscribed", id)

    // Retrieve it.
    entry, err := st.Get(context.Background(), id)
    if err != nil {
        panic(err)
    }
    fmt.Println(entry.Title)

    // Full-text search.
    hits, err := st.SearchText(context.Background(), "SQLite persistence", lore.SearchOpts{Limit: 5})
    if err != nil {
        panic(err)
    }
    for _, h := range hits {
        fmt.Printf("%.3f  %s\n", h.Score, h.Entry.Title)
    }
}
```

The `store.Store` interface is backend-agnostic. Swap `sqlite.New` for any
implementation that satisfies the interface to use a different storage engine
without changing callers.

## Status: pre-v1.0

Lore is pre-v1.0. The exported surface is stable in shape but may change in
detail between minor versions. Pin to a version, read release notes before
upgrading, and expect occasional breakage on `main`.

## What lore is not

- Not a CLI binary. Not an MCP server. Not an HTTP server. Not a UI.
- Not a hosted service. Not multi-tenant. Not an LLM client.
- Not a replacement for a full retrieval-augmented-generation framework.

Lore is the substrate. Everything above is a consumer's choice.

## VectorStore

`pkg/lore/vector` defines the `VectorStore` interface. The reference
implementation in `pkg/lore/vector/sqlitevec` stores vectors as BLOB columns
inside your existing `*sql.DB` and runs cosine similarity entirely in Go
(no CGO, no extensions).

```go
import (
    "context"
    "database/sql"

    _ "modernc.org/sqlite"

    "github.com/mathomhaus/lore/pkg/lore/vector"
    "github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

db, _ := sql.Open("sqlite", "lore.db")

// Bind to a 384-dimension space (BGE-small-en-v1.5).
store, err := sqlitevec.New(db, 384)
if err != nil {
    // handle
}
defer store.Close(context.Background())

ctx := context.Background()

// Store a vector.
vec := make([]float32, 384) // fill from your Embedder
_ = store.Upsert(ctx, entryID, vec)

// Search: returns top-5 hits in descending cosine similarity order.
hits, err := store.Search(ctx, queryVec, vector.SearchOpts{Limit: 5})
for _, h := range hits {
    fmt.Printf("entry %d score %.4f\n", h.ID, h.Score)
}
```

Kind and tag filters in `SearchOpts` are advisory. The sqlitevec reference
implementation does not apply them (a full-table-scan store has no efficient
join). Post-filter results via your `Store.Get` call or swap in a
VectorStore that understands your schema.

Scale: the reference impl performs a full linear scan. Acceptable for up to
roughly 100K vectors of 384 dimensions (benchmark: ~100ms on Apple M3 Pro).
Beyond that, implement `VectorStore` with pgvector, Qdrant, or a native
sqlite-vec extension backend.

## Attribution

Lore extracts and generalizes the storage, embedding, and retrieval primitives
originally built inside [`mathomhaus/guild`](https://github.com/mathomhaus/guild).
Guild remains the opinionated agent-coordination platform that adds
quest, oath, and brief on top of these primitives.


## License

Apache License 2.0. See [LICENSE](./LICENSE).
