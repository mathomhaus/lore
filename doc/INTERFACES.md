# lore: Interface Reference

This document is a condensed reference for every exported interface and its
key methods. For full godoc, run `go doc github.com/mathomhaus/lore/pkg/lore/...`.

## Store (`pkg/lore/store`)

`Store` persists lore entries and edges. All methods accept `ctx context.Context`
as their first argument and propagate cancellation to the underlying driver.
Close is idempotent; after Close all methods return `lore.ErrClosed`.

```go
type Store interface {
    Inscribe(ctx context.Context, e lore.Entry) (id int64, err error)
    Update(ctx context.Context, e lore.Entry) error
    Get(ctx context.Context, id int64) (lore.Entry, error)
    DeleteBySource(ctx context.Context, source string) (deleted int, err error)
    ListByTag(ctx context.Context, tag string, opts lore.ListOpts) ([]lore.Entry, error)
    ListByKind(ctx context.Context, kind lore.Kind, opts lore.ListOpts) ([]lore.Entry, error)
    SearchText(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error)
    AddEdge(ctx context.Context, edge lore.Edge) error
    ListEdges(ctx context.Context, fromID int64) ([]lore.Edge, error)
    Close(ctx context.Context) error
}
```

| Method | Description |
|---|---|
| `Inscribe` | Persist a new entry; return storage-assigned ID. |
| `Update` | Replace all mutable fields of an existing entry (full replacement, not patch). |
| `Get` | Fetch a single entry by ID. Returns `ErrNotFound` when absent. |
| `DeleteBySource` | Remove all entries with the given Source; non-matching source returns 0, nil. |
| `ListByTag` | Entries carrying the exact tag, ordered newest-first. |
| `ListByKind` | Entries of the given kind, ordered newest-first. |
| `SearchText` | BM25 full-text search over Title and Body. Higher Score is better. |
| `AddEdge` | Persist a directed edge. Re-adding the same triple is a no-op. |
| `ListEdges` | All edges from a given entry ID, ordered by created_at ascending. |
| `Close` | Release resources. Idempotent. |

Sentinel errors (from `pkg/lore`): `ErrNotFound`, `ErrDuplicate`,
`ErrInvalidKind`, `ErrInvalidArgument`, `ErrConflict`, `ErrUnsupported`,
`ErrClosed`.

Reference implementation: `pkg/lore/store/sqlite`. Constructor: `sqlite.New(db *sql.DB, opts ...Option) (Store, error)`.

---

## Embedder (`pkg/lore/embed`)

`Embedder` turns text into dense float32 vectors. Batch-oriented: callers
wrap a single string with `[]string{s}` when only one is needed. Safe for
concurrent use.

```go
type Embedder interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    Close(ctx context.Context) error
}
```

| Method | Description |
|---|---|
| `Embed` | Produce one vector per input string. Returns `ErrInvalidArgument` for empty slice or empty element. |
| `Dimensions` | Vector length emitted by Embed. Stable for the lifetime of the Embedder. |
| `Close` | Release loaded model and tokenizer. Idempotent. |

Sentinel errors: `embed.ErrInvalidArgument`, `embed.ErrUnsupported`,
`embed.ErrClosed`.

`ErrUnsupported` indicates the platform has no working ONNX Runtime. Callers
should fall back to lexical-only retrieval when they receive this from `New`
or `Embed`.

Reference implementation: `pkg/lore/embed/bge`. Constructor:
`bge.New(opts ...Option) (embed.Embedder, error)`. Options: `bge.WithLogger`,
`bge.WithTracer`.

---

## VectorStore (`pkg/lore/vector`)

`VectorStore` persists float32 vectors keyed by entry ID and answers
nearest-neighbor queries. Dimension-bound at construction.

```go
type VectorStore interface {
    Upsert(ctx context.Context, id int64, vector []float32) error
    Delete(ctx context.Context, id int64) error
    Search(ctx context.Context, query []float32, opts SearchOpts) ([]Hit, error)
    Dimensions() int
    Close(ctx context.Context) error
}
```

| Method | Description |
|---|---|
| `Upsert` | Store or replace the vector for entry ID. Vector length must equal Dimensions(). |
| `Delete` | Remove the vector for entry ID. Returns `ErrNotFound` when absent. |
| `Search` | Return top-Limit vectors by cosine similarity. Query length must equal Dimensions(). |
| `Dimensions` | Fixed vector length for this store. |
| `Close` | Release resources beyond the caller-owned DB. Idempotent. |

`SearchOpts.Kinds` and `SearchOpts.Tags` are advisory hints. Reference
implementations do not apply them; the Retriever layer post-filters via
`Store.Get`.

Sentinel errors: `vector.ErrNotFound`, `vector.ErrInvalidArgument`,
`vector.ErrClosed`.

Reference implementation: `pkg/lore/vector/sqlitevec`. Constructor:
`sqlitevec.New(db *sql.DB, dimensions int, opts ...Option) (vector.VectorStore, error)`.
Options: `sqlitevec.WithLogger`, `sqlitevec.WithTracer`.

---

## Retriever (`pkg/lore/retrieve`)

`Retriever` runs a search and returns ranked results. Implementations compose
`Store`, `Embedder`, and `VectorStore`; callers do not need to interact with
the underlying interfaces directly for search.

```go
type Retriever interface {
    Search(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error)
}
```

| Method | Description |
|---|---|
| `Search` | Execute retrieval for the given query; return results ranked by descending score. Returns `ErrInvalidArgument` for empty query or negative limit. |

Implementations:

- `hybrid.New(store, embedder, vstore, opts...)` - Fuses BM25 + vector via RRF.
  Options: `hybrid.WithRRFK(k int)`, `hybrid.WithCandidatePoolSize(n int)`,
  `hybrid.WithLogger`, `hybrid.WithTracer`.
- `bm25.New(store, opts...)` - Lexical-only. Options: `bm25.WithLogger`,
  `bm25.WithTracer`.
- `vector.New(store, embedder, vstore, opts...)` - Semantic-only. Options:
  `vector.WithLogger`, `vector.WithTracer`.

The hybrid retriever degrades gracefully: if one arm fails, results from the
surviving arm are returned. Both arms failing returns an error.

---

## Ingester (`pkg/lore/ingest`)

`Ingester` walks a document tree and produces classified lore entries. Pure
functional transform: `Process` does not write to any store.

```go
type Ingester interface {
    Process(ctx context.Context, root string) (Result, error)
}
```

| Method | Description |
|---|---|
| `Process` | Walk root, chunk recognized files, classify chunks, return entries. Non-nil error signals a fatal failure (root does not exist, etc.). Per-file failures are collected in Result.Errors. |

`Result` carries `Entries []lore.Entry` and `Errors []FileError`. Callers
decide how to handle `Result.Errors`: strict mode may discard entries; lenient
mode logs them and continues.

Reference implementation: `pkg/lore/ingest/heuristic`. Constructor:
`heuristic.NewIngester(opts ...Option) ingest.Ingester`. Options:
`heuristic.WithRules([]Rule)`, `heuristic.WithLogger`, `heuristic.WithTracer`,
`heuristic.WithMaxFileSize(n int64)`.

Classification priority (first match wins):

1. YAML front matter `kind:` field (validated against canonical kinds).
2. Path rules: `filepath.Match` against repo-relative path and base name.
3. Heading keywords: `Decision`, `Procedure`, `Explanation`, `Principle`, etc.
4. Fallback: `KindResearch`.

---

## RRF utility (`pkg/lore/retrieve/rrf`)

```go
func Fuse(rankings [][]int64, k int) []ScoredID
```

Combines multiple ranked lists (each a slice of entry IDs, best first) into a
single fused list using Reciprocal Rank Fusion. Pass `rrf.DefaultK` (60) when
in doubt. Returns `[]ScoredID` sorted by descending score; ties break by
ascending ID for determinism.

Useful for callers that want to run their own ranked lists through RRF without
the hybrid retriever.
