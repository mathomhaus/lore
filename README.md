# lore

Lore is a structured knowledge library for AI agents. It stores classified
entries (decisions, principles, procedures, references, explanations,
observations, research, ideas) and the edges between them, then serves
them to retrieval pipelines that combine lexical and semantic ranking.

Lore ships as a Go library, not a service. Callers compose it into their own
MCP servers, HTTP services, ingestion pipelines, or CLI tools. Three pluggable
interfaces (`Store`, `Embedder`, `VectorStore`) each have an in-process
reference implementation that runs against a local SQLite file out of the box.
Swap any of the three for Postgres, a remote embedding API, or a purpose-built
vector database by satisfying the interface.

## Install

```
go get github.com/mathomhaus/lore@v0.1.1
```

Requires Go 1.23 or newer.

## Quickstart

The example below wires the three reference implementations together, inscribes
an entry, embeds it into the vector store, and runs a hybrid search.

```go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	_ "modernc.org/sqlite"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/embed"
	"github.com/mathomhaus/lore/pkg/lore/embed/bge"
	"github.com/mathomhaus/lore/pkg/lore/retrieve/hybrid"
	"github.com/mathomhaus/lore/pkg/lore/store/sqlite"
	"github.com/mathomhaus/lore/pkg/lore/vector/sqlitevec"
)

func main() {
	ctx := context.Background()

	// Open a single SQLite file. All three backends share the same DB.
	dsn := "knowledge.db" +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(ON)"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Store: handles entry persistence and BM25 full-text search.
	st, err := sqlite.New(db)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close(ctx)

	// VectorStore: stores and queries float32 vectors (384-dim for BGE-small).
	vs, err := sqlitevec.New(db, 384)
	if err != nil {
		log.Fatal(err)
	}
	defer vs.Close(ctx)

	// Embedder: in-process BGE int8 model. Falls back gracefully on platforms
	// without ONNX Runtime.
	var emb embed.Embedder
	emb, err = bge.New()
	if err != nil {
		if !errors.Is(err, embed.ErrUnsupported) {
			log.Fatal(err)
		}
		log.Print("embedder unavailable; using BM25-only retrieval")
		emb = nil
	}
	if emb != nil {
		defer emb.Close(ctx)
	}

	// Inscribe a decision entry.
	id, err := st.Inscribe(ctx, lore.Entry{
		Project: "decisionLog",
		Kind:    lore.KindDecision,
		Title:   "Use SQLite for local persistence",
		Body:    "Chosen for zero-dependency deployment and strong ACID guarantees under single-writer workloads.",
		Tags:    []string{"adr", "storage"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("inscribed entry id=%d\n", id)

	// Embed and store the vector for the new entry if an embedder is available.
	if emb != nil {
		entry, _ := st.Get(ctx, id)
		vecs, err := emb.Embed(ctx, []string{entry.Title + " " + entry.Body})
		if err == nil {
			_ = vs.Upsert(ctx, id, vecs[0])
		}
	}

	// Build a hybrid retriever that fuses BM25 + vector via RRF.
	r := hybrid.New(st, emb, vs,
		hybrid.WithRRFK(60),
		hybrid.WithCandidatePoolSize(50),
	)

	// Search.
	hits, err := r.Search(ctx, "SQLite persistence decision", lore.SearchOpts{
		Project: "decisionLog",
		Limit:   5,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, h := range hits {
		fmt.Printf("%.4f  %s\n", h.Score, h.Entry.Title)
	}
}
```

## What lore is

Lore exposes three pluggable interfaces:

**Store** (`pkg/lore/store`) persists entries and edges. The reference
implementation in `pkg/lore/store/sqlite` uses `modernc.org/sqlite` (pure Go)
with FTS5 for BM25 full-text search. Replace it with Postgres, MySQL, or any
other engine by satisfying the interface.

**Embedder** (`pkg/lore/embed`) turns text into dense vectors. The reference
implementation in `pkg/lore/embed/bge` runs an int8-quantized BGE-small-en-v1.5
model in process via ONNX Runtime. Replace it with a remote embedding API or a
different local model without changing retrieval logic.

**VectorStore** (`pkg/lore/vector`) stores and queries float32 vectors. The
reference implementation in `pkg/lore/vector/sqlitevec` stores vectors as BLOB
columns in the same SQLite file and does cosine similarity in Go (no CGO, no
extensions). Replace it with pgvector, Qdrant, or a native extension backend.

On top of these three, lore composes a **Retriever** (`pkg/lore/retrieve`) that
fuses BM25 lexical and vector semantic rankings via Reciprocal Rank Fusion (RRF,
k=60). An **Ingester** (`pkg/lore/ingest`) optionally walks document trees and
classifies chunks into entries (Path B).

Two write paths:

- **Path A (agent inscribe):** an agent calls `Store.Inscribe` directly, then
  `Embedder.Embed` + `VectorStore.Upsert`. No document parsing, no LLM cost.
  High-frequency path suitable for session-level knowledge capture.
- **Path B (document ingestion):** `Ingester.Process` walks a directory, chunks
  Markdown files, classifies each chunk (YAML front matter, then path rules,
  then heading patterns, then fallback to `research`), and returns entries. The
  caller writes them to a Store. Suitable for bulk ingestion of existing docs.

## What lore is not

- Not a CLI binary.
- Not an MCP server.
- Not an HTTP server.
- Not a UI.
- Not a hosted service.
- Not multi-tenant (all isolation is caller-provided via the `Project` field).
- Not an LLM client.
- Not a replacement for a full retrieval-augmented-generation framework.

Lore is the substrate. Everything above is a consumer's responsibility.

## Production deployment patterns

Because lore accepts caller-owned `*sql.DB` instances rather than connection
strings, it maps cleanly to multi-replica Kubernetes deployments. A typical
consumer service structure uses three stateless Deployments:

**Ingester worker** reads source documents from a queue (Pub/Sub, SQS, or a
database queue table), calls `Ingester.Process`, and writes the returned entries
to a shared `Store`. One or more replicas; only requires write access to the
database.

**Query API service** receives search queries over HTTP or gRPC. It opens a
read-optimized `*sql.DB` connection pool (WAL mode allows concurrent readers),
constructs a `Store` + `Embedder` + `VectorStore`, wires them into a `Retriever`,
and returns ranked hits. Scales horizontally; each replica is stateless.

**MCP gateway** exposes lore to AI agent harnesses via the Model Context
Protocol. It wraps the same `Store` + `Retriever` in tool handlers for
`inscribe`, `search`, and `list`. The library provides the knowledge primitives;
the MCP surface is the consumer's thin adaptation layer.

All three Deployments can share a single underlying SQLite file (via a network
volume or single-writer proxy) or migrate to Postgres by swapping the `Store`
and `VectorStore` implementations. No code changes are required in the consumer
services when backends are swapped.

## Reference implementations

| Package | Role | Backend | Scale guidance |
|---|---|---|---|
| `pkg/lore/store/sqlite` | Store | modernc.org/sqlite + FTS5 | Suitable for most single-service workloads |
| `pkg/lore/embed/bge` | Embedder | BGE-small-en-v1.5 int8 via ONNX Runtime | Requires ONNX Runtime dylib; pure CPU |
| `pkg/lore/vector/sqlitevec` | VectorStore | SQLite BLOB + Go cosine scan | Good to ~100K vectors of 384 dim (~100ms scan on modern hardware) |
| `pkg/lore/retrieve/hybrid` | Retriever | BM25 + vector via RRF | Inherits limits of Store + VectorStore |
| `pkg/lore/retrieve/bm25` | Retriever (lexical only) | Store.SearchText | No embedder required |
| `pkg/lore/ingest/heuristic` | Ingester | Rule-based heuristic classifier | Pure Go; no LLM cost |

All reference implementations are pure Go with no CGO requirement. The BGE
embedder uses `purego` for ONNX Runtime binding rather than CGO.

## Configuration

**Logger.** Pass `WithLogger(*slog.Logger)` to any constructor. Defaults to
`slog.Default()`.

**Tracer.** Pass `WithTracer(trace.Tracer)` to enable OpenTelemetry spans.
Defaults to the global tracer provider (`otel.GetTracerProvider()`). Wire an
exporter in your service bootstrap to send traces to your backend of choice.

Span names follow the pattern `lore.<package>.<operation>` (for example
`lore.store.inscribe`, `lore.vector.search`, `lore.retrieve.search`).

**BGE embedder.** Set `LORE_ONNXRUNTIME_LIB` to override the default shared
library search path. On macOS: `brew install onnxruntime` puts the dylib where
the probe expects it. When the library is absent, `bge.New` returns
`embed.ErrUnsupported` and callers should fall back to lexical-only retrieval.

## Stability

Lore is pre-v1.0. The exported surface is stable in shape but may change in
detail between minor versions. Pin to a version, read release notes before
upgrading, and expect occasional breaking changes on minor version bumps.

## Attribution

Lore extracts and generalizes the storage, embedding, and retrieval primitives
originally built inside [`mathomhaus/guild`](https://github.com/mathomhaus/guild).
Guild remains the opinionated agent-coordination platform that adds quest, oath,
and brief on top of these primitives.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
