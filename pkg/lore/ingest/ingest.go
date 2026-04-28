// Package ingest provides Path B document ingestion for the lore library.
//
// The Ingester interface walks a filesystem directory tree, chunks recognized
// document files (Markdown for v0.1.1), classifies each chunk into a lore
// entry, and returns the entries to the caller. Ingester is a pure functional
// transform: Process returns [Result] and the caller decides how to write the
// entries to a [lore.Store] and [lore.VectorStore].
//
// # Composing with a store
//
// The caller owns the write side so it can apply its own transactional logic,
// deduplication, or batching:
//
//	result, err := ing.Process(ctx, "/workspace/docs")
//	if err != nil {
//	    return err
//	}
//	for _, e := range result.Entries {
//	    if _, err := store.Put(ctx, e); err != nil {
//	        log.Error("put entry", "err", err, "title", e.Title)
//	    }
//	}
//
// # Versioning notes
//
// v0.1.1: Markdown only (.md, .markdown). Filesystem walker only (no git
// index, no incremental). .gitignore patterns NOT honored (v0.2 followup).
// Symlinks are NOT followed.
package ingest

import (
	"context"
	"fmt"

	"github.com/mathomhaus/lore/pkg/lore"
)

// Ingester walks document trees and produces classified entries.
// Implementations are stateless functional transforms: Process returns the
// entries it computed and the caller is responsible for writing them to a
// Store and VectorStore.
type Ingester interface {
	// Process walks root (a filesystem directory), chunks recognized files,
	// classifies each chunk, and returns the entries. It does not write to
	// any store. Returns a Result with successful entries plus any per-file
	// errors that did not abort the whole walk.
	//
	// A non-nil error from Process signals a fatal failure (e.g. root does
	// not exist or is not a directory). Per-file failures that do not abort
	// the walk are collected in Result.Errors instead.
	Process(ctx context.Context, root string) (Result, error)
}

// Result carries the output of a single [Ingester.Process] call.
type Result struct {
	// Entries are the successfully classified chunks from all walked files.
	// Ordered by discovery: file walk order, then chunk order within each file.
	Entries []lore.Entry

	// Errors collects per-file failures that did not abort the whole walk.
	// A caller that needs strict mode should inspect this slice and decide
	// whether to discard Entries or surface the errors.
	Errors []FileError
}

// FileError records a per-file failure during a walk. The walk continues
// after recording the error.
type FileError struct {
	// Path is the absolute path of the file that caused the failure.
	Path string

	// Err is the underlying error.
	Err error
}

// Error implements the error interface so FileError values can be passed to
// structured loggers that accept error.
func (e FileError) Error() string {
	return fmt.Sprintf("ingest: %s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying error for errors.Is and errors.As chains.
func (e FileError) Unwrap() error {
	return e.Err
}
