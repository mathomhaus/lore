---
kind: decision
tags: [adr, language]
---

# Use Go for the implementation

## Status

Accepted

## Context

We needed a language for the lore library that compiles to a single binary,
has strong typing, good concurrency primitives, and a mature standard library.
Python and Node were considered but ruled out due to deployment complexity and
runtime overhead.

## Decision

Use Go 1.23+ as the implementation language for the lore library.

## Consequences

- Callers must use a Go toolchain to consume the library.
- Cross-language bindings (Python, JS) require a separate wrapper layer.
- The library ships as a Go module with no CGO requirements (pure-Go SQLite
  via modernc.org/sqlite).
