// Package lore is a structured knowledge primitive for AI agents.
//
// Lore stores classified knowledge entries (decisions, principles, procedures,
// references, explanations, observations, research, ideas) and the edges that
// connect them, then serves them back to retrieval pipelines that combine
// lexical and semantic ranking. It ships as a Go library, not a service:
// callers compose it into their own MCP servers, HTTP services, ingestion
// pipelines, or CLI tools.
//
// # Scope
//
// Lore v0.1.1 is library-only. It does not ship a CLI binary, an MCP server,
// an HTTP server, or a UI. It does not embed an LLM or talk to a remote
// service. It is the substrate; consumers compose the rest.
//
// # The three pluggable interfaces
//
// Lore is built around three swap points so users can mix in-process reference
// implementations with their own backends without rewriting retrieval logic:
//
//   - Store persists entries and edges. The reference implementation lives in
//     pkg/lore/store/sqlite and uses modernc.org/sqlite (pure Go) plus FTS5 for
//     lexical search. Replace it with Postgres, MySQL, or any other engine by
//     implementing the interface in pkg/lore/store.
//
//   - Embedder turns text into vectors. The reference implementation lives in
//     pkg/lore/embed/bge and runs an int8-quantized BGE model in process.
//     Replace it with a remote embedding API or a different local model by
//     implementing the interface in pkg/lore/embed.
//
//   - VectorStore persists and queries vectors. The reference implementation
//     lives in pkg/lore/vector/sqlitevec and uses the sqlite-vec extension.
//     Replace it with pgvector, Qdrant, Weaviate, or any other engine by
//     implementing the interface in pkg/lore/vector.
//
// On top of these three, lore composes a Retriever that runs lexical and
// vector queries in parallel and fuses results with reciprocal-rank fusion,
// and an optional Ingester that walks document trees and classifies chunks
// into entries on Path B (document ingestion). Path A (agent inscribe) goes
// straight through Store and Embedder without an ingest pipeline.
//
// # Caller-owned dependencies
//
// Constructors in this library accept already-initialized resources:
// *sql.DB, *http.Client, *slog.Logger, OpenTelemetry providers, and so on.
// The library does not open database connections, parse URLs, or read
// environment variables on the caller's behalf. This keeps the library
// stateless beyond its injected dependencies and safe to deploy across
// multiple replicas.
//
// # Stability
//
// Lore is pre-v1.0. The exported surface is stable in shape but may change in
// detail between minor versions. Pin to a version and read release notes
// before upgrading.
package lore
