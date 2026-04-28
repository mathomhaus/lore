# Changelog

All notable changes to this project will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 2026-04-27

### Added

- **`pkg/lore`** - Core types: `Entry`, `Edge`, `SearchHit`, `ListOpts`,
  `SearchOpts`. Eight canonical `Kind` values: `decision`, `principle`,
  `procedure`, `reference`, `explanation`, `observation`, `research`, `idea`.
  Sentinel errors: `ErrNotFound`, `ErrDuplicate`, `ErrInvalidKind`,
  `ErrInvalidArgument`, `ErrConflict`, `ErrUnsupported`, `ErrClosed`.

- **`pkg/lore/store`** - `Store` interface: `Inscribe`, `Update`, `Get`,
  `DeleteBySource`, `ListByTag`, `ListByKind`, `SearchText`, `AddEdge`,
  `ListEdges`, `Close`. Full error contract and lifecycle contract documented.

- **`pkg/lore/store/sqlite`** - SQLite reference implementation of `Store`.
  Uses `modernc.org/sqlite` (pure Go, no CGO). FTS5 full-text index for BM25
  retrieval. Schema migrations via an embedded migration table. OTel spans on
  every I/O method. `slog` for warnings and errors.

- **`pkg/lore/embed`** - `Embedder` interface: `Embed`, `Dimensions`, `Close`.
  Sentinel errors: `ErrInvalidArgument`, `ErrUnsupported`, `ErrClosed`.

- **`pkg/lore/embed/bge`** - BGE-small-en-v1.5 int8 reference implementation
  of `Embedder`. Runs in-process via `github.com/shota3506/onnxruntime-purego`
  (no CGO). Model and tokenizer assets embedded in the binary at build time.
  Returns `embed.ErrUnsupported` on platforms where ONNX Runtime is absent.
  OTel spans named `lore.embed.encode`. Requires ONNX Runtime shared library
  (`brew install onnxruntime` on macOS; `apt install libonnxruntime` on
  Debian-derived Linux).

- **`pkg/lore/vector`** - `VectorStore` interface: `Upsert`, `Delete`,
  `Search`, `Dimensions`, `Close`. `Hit` result type. `SearchOpts` with
  advisory `Kinds`/`Tags` filters. Sentinel errors: `ErrNotFound`,
  `ErrInvalidArgument`, `ErrClosed`.

- **`pkg/lore/vector/sqlitevec`** - SQLite-backed reference implementation
  of `VectorStore`. Vectors stored as little-endian float32 BLOBs. Cosine
  similarity computed in Go via a full table scan. Suitable for up to ~100K
  vectors of 384 dimensions. No CGO, no sqlite-vec extension required.
  OTel spans named `lore.vector.upsert`, `lore.vector.delete`,
  `lore.vector.search`.

- **`pkg/lore/retrieve`** - `Retriever` interface: `Search`. Shared result
  types re-use `lore.SearchHit`.

- **`pkg/lore/retrieve/bm25`** - `Ranker`: lexical-only `Retriever` backed
  by `Store.SearchText`. OTel span `lore.retrieve.bm25`.

- **`pkg/lore/retrieve/vector`** - `Searcher`: semantic-only `Retriever`
  backed by `Embedder` + `VectorStore`. OTel span `lore.retrieve.vector`.

- **`pkg/lore/retrieve/rrf`** - `Fuse`: Reciprocal Rank Fusion over arbitrary
  ranked lists. `DefaultK = 60`. Deterministic tie-breaking by ascending ID.

- **`pkg/lore/retrieve/hybrid`** - `Retriever` that fuses BM25 and vector
  rankings via RRF. Degrades gracefully: if one arm fails, the other continues.
  OTel spans: `lore.retrieve.search`, `lore.retrieve.bm25`,
  `lore.retrieve.vector`, `lore.retrieve.fuse`.

- **`pkg/lore/ingest`** - `Ingester` interface: `Process`. `Result`, `FileError`
  types. `WalkerConfig` for tuning the filesystem walk. Pure functional transform:
  returns entries; caller owns writes.

- **`pkg/lore/ingest/heuristic`** - Heuristic `Ingester` implementation.
  Four-level classification priority: YAML front matter, path rules
  (`DefaultRules`), heading keyword patterns, fallback `research`. Configurable
  via `WithRules`, `WithLogger`, `WithTracer`, `WithMaxFileSize`. OTel spans
  `lore.ingest.process`, `lore.ingest.classify`.

- `doc/ARCHITECTURE.md` - Architecture overview.
- `doc/INTERFACES.md` - Interface reference.
- `CHANGELOG.md` - This file.

### Notes

- Tag `v0.1.1` is the initial public release. Previous commits established the
  package structure iteratively; `v0.1.1` is the first tagged, stable release.
- All reference implementations are pure Go or use `purego` bindings; no CGO
  is required to build the module.
- Pre-v1.0: the exported surface is stable in shape but may change in detail
  between minor versions.

[0.1.1]: https://github.com/mathomhaus/lore/releases/tag/v0.1.1
