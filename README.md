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

## Status: pre-v1.0

Lore is pre-v1.0. The exported surface is stable in shape but may change in
detail between minor versions. Pin to a version, read release notes before
upgrading, and expect occasional breakage on `main`.

## What lore is not

- Not a CLI binary. Not an MCP server. Not an HTTP server. Not a UI.
- Not a hosted service. Not multi-tenant. Not an LLM client.
- Not a replacement for a full retrieval-augmented-generation framework.

Lore is the substrate. Everything above is a consumer's choice.

## Attribution

Lore extracts and generalizes the storage, embedding, and retrieval primitives
originally built inside [`mathomhaus/guild`](https://github.com/mathomhaus/guild).
Guild remains the opinionated agent-coordination platform that adds
quest, oath, and brief on top of these primitives.

## Spec

The architectural rationale and product positioning that informs this library
lives in the maintainer's notes at
`~/Library/CloudStorage/SynologyDrive-Obsidian/Personal/01 Projects/Agent Guild/Positioning/lore-product-mvp-2026-04-27.md`.

## License

Apache License 2.0. See [LICENSE](./LICENSE).
