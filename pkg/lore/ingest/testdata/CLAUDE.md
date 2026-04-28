# lore agent bootstrap

This file is loaded by MCP harnesses that auto-consume `.mcpb` configuration.
It carries repo-specific context for contributing agents.

## First action

Run `guild_session_start(project="lore")` before doing anything else.

## Repo facts

- Module: `github.com/mathomhaus/lore`
- Language: Go 1.23+
- Test gate: `go test -race ./...`
