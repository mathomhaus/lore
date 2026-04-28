# lore: Architecture

This document describes the design of the lore library for contributors and
consumers who want to understand how the pieces fit together.

## Goal

Lore is a structured knowledge primitive for AI agents. It solves one problem:
store classified knowledge entries, link them with typed edges, and retrieve
them quickly using a combination of lexical and semantic ranking. The library
does not provide a CLI, an HTTP server, an MCP server, or a UI. It is
deliberately library-only so it can be composed into any of those without
duplication or coupling.

## Three pluggable interfaces

The library is built around exactly three swap points. Each swap point is a Go
interface; the reference implementation for each is in a sub-package.

```
Store         pkg/lore/store       persistence + BM25 full-text
Embedder      pkg/lore/embed       text-to-vector
VectorStore   pkg/lore/vector      vector nearest-neighbor
```

Why three and not more or fewer?

Fewer would mean coupling persistence to retrieval or retrieval to embedding,
forcing consumers to take all three or none. More would fragment the interface
without adding capability: for example, splitting `Store` into a separate
`EdgeStore` and `EntryStore` would add complexity without enabling any
deployment pattern that the single interface cannot already serve.

Three also corresponds directly to the three backend choices a production
deployment typically makes: relational store (SQLite, Postgres), embedding
model (local, remote API), and vector index (linear scan, pgvector, Qdrant).
Each swap is independent of the others.

## Composition layer

On top of the three interfaces, the library provides two composing layers:

**Retriever** (`pkg/lore/retrieve`) composes `Store`, `Embedder`, and
`VectorStore` into a unified search surface. The `hybrid` implementation runs
the BM25 arm (`Store.SearchText`) and the vector arm (`Embedder.Embed` +
`VectorStore.Search`) concurrently, then fuses the ranked lists via Reciprocal
Rank Fusion (RRF). Callers that only want one arm use `bm25.New(store)` or
`vector.New(store, embedder, vstore)` directly; both satisfy `Retriever`.

**Ingester** (`pkg/lore/ingest`) is a pure functional transform that walks a
directory tree, chunks recognized files (Markdown in v0.1.1), classifies each
chunk into a lore entry, and returns the entries to the caller. The caller
writes them to a `Store`. The ingester does not hold state between calls.

## Path A vs Path B

**Path A (agent inscribe):** an AI agent session calls `Store.Inscribe`
directly with a fully-formed entry it has already classified. It then calls
`Embedder.Embed` on the entry text and `VectorStore.Upsert` on the resulting
vector. This is the high-frequency path: no file parsing, no LLM inference cost,
no heuristics. An agent that produces 100 inscriptions per session uses Path A
for all of them.

**Path B (document ingestion):** a one-time or periodic pipeline reads
existing document trees (runbooks, ADRs, wikis) and imports them in bulk.
`Ingester.Process` walks the tree, the heuristic classifier assigns kinds and
tags, and the caller writes the results to the same `Store`. Path B is optional:
a service that only needs agent-produced knowledge never instantiates an
`Ingester`.

The two paths share the same `Store` and `VectorStore`; entries produced by
either path are indistinguishable at retrieval time.

## Reference implementations and their scale limits

**`store/sqlite`** uses `modernc.org/sqlite` (pure Go) and FTS5 for full-text
search. It is single-writer by design (SQLite WAL allows concurrent readers).
Suitable for single-service workloads without a shared database requirement.
Swap for a Postgres implementation when multiple writer replicas are needed.

**`embed/bge`** runs the BAAI/bge-small-en-v1.5 int8 model in process using
`purego` bindings to the ONNX Runtime shared library. No CGO, no network calls.
Throughput is bounded by CPU; a single core embeds roughly 200-500 short texts
per second on modern hardware depending on batch size. Swap for a remote
embedding API (OpenAI, Cohere, Vertex AI) by implementing `embed.Embedder`.

**`vector/sqlitevec`** stores float32 vectors as BLOB columns and computes
cosine similarity over a full table scan in Go. Practical limit is roughly
100K vectors of 384 dimensions (~100ms per query on modern laptop hardware).
Above that threshold, swap for a purpose-built ANN index: pgvector,
Qdrant, Weaviate, or a true sqlite-vec extension implementation.

## Hybrid retrieval via RRF

The hybrid retriever fuses two ranked lists using Reciprocal Rank Fusion:

```
score(d) = sum over rankers r: 1 / (k + rank_r(d))
```

where k=60 is the standard smoothing constant (from Cormack, Clarke, Buettcher
2009). RRF requires only the rank position of each document, not the score
magnitude. This makes it robust to score scale differences between BM25 (which
returns large positive floats) and cosine similarity (which returns values in
[-1, 1]). No tuning is needed when switching between BM25 implementations or
embedding models.

The retriever fetches a candidate pool (default: top 50) from each arm, fuses
the two ranked lists via RRF, truncates to the requested limit, and then
hydrates the full entry from the `Store` for each result. Partial arm failures
are tolerated: if the vector arm fails (for example `ErrUnsupported` on a
platform without ONNX Runtime), the BM25 arm continues independently and vice
versa.

## Caller-owned dependencies

Every constructor in the library accepts already-initialized resources. `sqlite.New`
takes a `*sql.DB`. `bge.New` accepts optional `*slog.Logger` and
`trace.Tracer`. `sqlitevec.New` takes a `*sql.DB` and the vector dimension.

The library never opens database connections, reads environment variables for
connection strings, or manages connection pool lifecycle. This design has two
consequences:

1. A service can pass the same `*sql.DB` to `sqlite.New` and `sqlitevec.New`,
   sharing one connection pool and one SQLite file between the store and the
   vector index. Schema migrations for both live in the same database.

2. Multiple replicas of the same service can each open their own `*sql.DB`
   against a shared database (for example Postgres via `database/sql` and a
   Postgres-backed `Store` implementation). The library has no global state
   and is safe to construct multiple times in the same process.

In a Kubernetes deployment this means the ingester worker, query API service,
and MCP gateway each construct their own `Store` and `Retriever` from the same
connection string, without sharing in-process objects or requiring a singleton.
