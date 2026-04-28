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

### Path B: document ingestion

Path B ingests existing Markdown document trees into lore entries. The
ingester is a pure functional transform: it returns entries and the caller
writes them to a Store.

```go
import (
    "context"
    "log"

    "github.com/mathomhaus/lore/pkg/lore/ingest/heuristic"
)

func main() {
    ing := heuristic.NewIngester()

    result, err := ing.Process(context.Background(), "/workspace/docs")
    if err != nil {
        log.Fatal(err)
    }

    for _, fe := range result.Errors {
        log.Printf("warn: %v", fe)
    }

    for _, e := range result.Entries {
        log.Printf("entry kind=%s title=%q source=%s", e.Kind, e.Title, e.Source)
        // write e to your store here
    }
}
```

#### Classification priority

The heuristic ingester classifies each chunk using this priority order (first
match wins):

1. YAML front matter with an explicit `kind:` field.
2. Path rules: `docs/adr/*.md` maps to decision+adr; `docs/runbooks/*.md`
   maps to procedure+runbook; `CLAUDE.md`/`agents.md`/`skills.md` map to
   reference+agent-config; and so on. See `heuristic.DefaultRules()`.
3. Heading keywords: `## What is` maps to explanation; `## Decision` /
   `## Context` / `## Consequences` maps to decision; `## Procedure` /
   `## Steps` maps to procedure; etc.
4. Fallback: kind=research (catch-all).

#### Customizing rules

```go
import "github.com/mathomhaus/lore/pkg/lore/ingest/heuristic"

rules := heuristic.DefaultRules()
rules = append(rules, heuristic.Rule{
    PathGlob: "docs/specs/*.md",
    Kind:     lore.KindDecision,
    Tags:     []string{"spec"},
})

ing := heuristic.NewIngester(
    heuristic.WithRules(rules),
    heuristic.WithLogger(slog.Default()),
)
```

#### Walker behavior (v0.1.1)

- Only `.md` and `.markdown` files are processed.
- `.git/`, `node_modules/`, `vendor/`, and any hidden directory (name
  starting with `.`) are skipped unconditionally.
- Files larger than 10 MB are skipped with a FileError.
- Symlinks are not followed.
- `.gitignore` patterns are not honored (planned for v0.2).

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

## Embedder

The `Embedder` interface turns text into dense vectors for semantic retrieval:

```go
import (
    "context"
    "errors"

    "github.com/mathomhaus/lore/pkg/lore/embed"
    "github.com/mathomhaus/lore/pkg/lore/embed/bge"
)

func embedTexts(ctx context.Context, texts []string) ([][]float32, error) {
    emb, err := bge.New()
    if err != nil {
        if errors.Is(err, embed.ErrUnsupported) {
            // Platform has no ONNX Runtime; fall through to lexical-only retrieval.
            return nil, err
        }
        return nil, err
    }
    defer emb.Close(ctx)

    vecs, err := emb.Embed(ctx, texts)
    if err != nil {
        return nil, err
    }
    // Each vecs[i] is a float32 slice of length emb.Dimensions() (384 for BGE-small).
    return vecs, nil
}
```

`bge.New` options:

- `bge.WithLogger(*slog.Logger)` for a structured logger covering init and runtime warnings.
- `bge.WithTracer(trace.Tracer)` for an OTel tracer; spans named `lore.embed.encode`.

The BGE reference implementation requires the ONNX Runtime shared library on the
host (e.g. `brew install onnxruntime` on macOS). Set `LORE_ONNXRUNTIME_LIB` to
override the default search path. When the library is absent, `bge.New` returns
`embed.ErrUnsupported` and callers should fall back to lexical retrieval.

Implement the `embed.Embedder` interface to swap in a remote embedding API or a
different model without changing any retrieval code.

## Retriever: hybrid BM25 + vector search

`pkg/lore/retrieve` defines the `Retriever` interface. The reference
implementation in `pkg/lore/retrieve/hybrid` fuses BM25 lexical search
(via `Store.SearchText`) and vector nearest-neighbour search (via
`Embedder.Embed` + `VectorStore.Search`) using Reciprocal Rank Fusion
(RRF, k=60). This approach avoids tuning score scales across rankers:
only ordinal rank positions matter.

```go
import (
    "context"
    "database/sql"
    "fmt"
    "log"

    _ "modernc.org/sqlite"

    "github.com/mathomhaus/lore/pkg/lore"
    "github.com/mathomhaus/lore/pkg/lore/embed/bge"
    "github.com/mathomhaus/lore/pkg/lore/retrieve/hybrid"
    "github.com/mathomhaus/lore/pkg/lore/store/sqlite"
    "github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

func search(db *sql.DB, query string) ([]lore.SearchHit, error) {
    // Store handles BM25.
    st, err := sqlite.New(db)
    if err != nil {
        return nil, err
    }
    defer st.Close(context.Background())

    // Embedder handles query vectorisation.
    emb, err := bge.New()
    if err != nil {
        // ErrUnsupported on platforms without ONNX Runtime: use BM25-only.
        log.Printf("warn: embedder unavailable, using BM25 only: %v", err)
        emb = nil
    }
    if emb != nil {
        defer emb.Close(context.Background())
    }

    // VectorStore handles nearest-neighbour lookup.
    vs, err := sqlitevec.New(db, 384)
    if err != nil {
        return nil, err
    }
    defer vs.Close(context.Background())

    r := hybrid.New(st, emb, vs,
        hybrid.WithRRFK(60),
        hybrid.WithCandidatePoolSize(50),
    )

    return r.Search(context.Background(), query, lore.SearchOpts{Limit: 10})
}
```

The hybrid retriever tolerates partial failures gracefully:

- If `Embedder.Embed` returns an error (e.g. `embed.ErrUnsupported`), the vector
  arm is skipped and BM25 results are returned alone.
- If `VectorStore.Search` returns an error, the BM25 arm continues independently.
- Only when both arms fail does `Search` return an error.

When the embedder is nil, pass a no-op stub or use `bm25.New(store)` directly:

```go
import "github.com/mathomhaus/lore/pkg/lore/retrieve/bm25"

r := bm25.New(st)
hits, err := r.Search(ctx, "deployment rollout", lore.SearchOpts{Limit: 10})
```

### RRF algorithm

`pkg/lore/retrieve/rrf` exposes `Fuse(rankings [][]int64, k int) []ScoredID`
for callers that want to run their own ranked lists through RRF without the
hybrid retriever:

```go
import "github.com/mathomhaus/lore/pkg/lore/retrieve/rrf"

bm25IDs := []int64{10, 20, 30}
vecIDs  := []int64{20, 10, 40}

fused := rrf.Fuse([][]int64{bm25IDs, vecIDs}, rrf.DefaultK)
for _, s := range fused {
    fmt.Printf("id=%d score=%.4f\n", s.ID, s.Score)
}
```

Output is sorted by descending score; ties break by ascending ID for
determinism.

## Attribution

Lore extracts and generalizes the storage, embedding, and retrieval primitives
originally built inside [`mathomhaus/guild`](https://github.com/mathomhaus/guild).
Guild remains the opinionated agent-coordination platform that adds
quest, oath, and brief on top of these primitives.


## License

Apache License 2.0. See [LICENSE](./LICENSE).
