// Package store defines the Store interface that all lore persistence backends
// must satisfy. The interface is small and deliberate: every method maps to a
// distinct access pattern (point read, scan, text search, edge traversal) so
// callers can reason about cost without understanding implementation internals.
//
// Error contract:
//   - Every method returns a non-nil error on failure.
//   - Callers may test returned errors with errors.Is against the sentinel
//     values in the parent lore package: ErrNotFound, ErrDuplicate,
//     ErrInvalidKind, ErrInvalidArgument, ErrConflict, ErrUnsupported,
//     ErrClosed.
//   - Implementations wrap sentinels with fmt.Errorf("operation: %w", sentinel)
//     so the full operation context is available via err.Error() while
//     errors.Is still resolves the sentinel.
//
// Lifecycle contract:
//   - Callers own the underlying *sql.DB (or equivalent resource). They
//     configure pool sizing, set pragmas, and call Close on the DB after they
//     have called Store.Close.
//   - Store.Close is idempotent: calling it more than once returns nil.
//   - After Close, all other methods return ErrClosed.
package store

import (
	"context"

	"github.com/mathomhaus/lore/pkg/lore"
)

// Store persists lore entries and edges. Implementations are caller-managed:
// constructors accept a caller-owned *sql.DB (or equivalent); consumers manage
// pool configuration and lifecycle. New runs pending migrations automatically.
//
// All methods accept a context.Context as their first argument. Cancellation
// or deadline expiry propagates to the underlying database driver and returns
// the context error wrapped with operation context.
type Store interface {
	// Inscribe persists a new entry and returns its storage-assigned ID.
	//
	// Errors:
	//   - ErrInvalidKind   when e.Kind is outside the canonical taxonomy.
	//   - ErrInvalidArgument when e.Title or e.Body is empty.
	//   - ErrDuplicate     when the implementation enforces a uniqueness
	//                      constraint and it would be violated.
	//   - ErrClosed        when the store has been closed.
	Inscribe(ctx context.Context, e lore.Entry) (id int64, err error)

	// Update replaces all mutable fields of the entry identified by e.ID.
	// The caller must supply a fully-populated Entry (not a partial patch).
	//
	// Errors:
	//   - ErrNotFound      when e.ID does not match any persisted entry.
	//   - ErrInvalidKind   when e.Kind is outside the canonical taxonomy.
	//   - ErrInvalidArgument when e.Title or e.Body is empty.
	//   - ErrClosed        when the store has been closed.
	Update(ctx context.Context, e lore.Entry) error

	// Get returns the entry with the given ID.
	//
	// Errors:
	//   - ErrNotFound      when id does not match any persisted entry.
	//   - ErrClosed        when the store has been closed.
	Get(ctx context.Context, id int64) (lore.Entry, error)

	// DeleteBySource removes all entries whose Source field exactly matches
	// source and returns the count of deleted rows. Deleting a non-existent
	// source is not an error: deleted returns 0 and err is nil.
	//
	// Errors:
	//   - ErrInvalidArgument when source is empty.
	//   - ErrClosed          when the store has been closed.
	DeleteBySource(ctx context.Context, source string) (deleted int, err error)

	// ListByTag returns all entries that carry the given tag, subject to opts.
	// The tag match is exact: "adr" does not match "adr-2024". Results are
	// ordered by created_at descending (newest first).
	//
	// Errors:
	//   - ErrInvalidArgument when tag is empty or opts.Limit is negative.
	//   - ErrClosed          when the store has been closed.
	ListByTag(ctx context.Context, tag string, opts lore.ListOpts) ([]lore.Entry, error)

	// ListByKind returns all entries of the given kind, subject to opts.
	// Results are ordered by created_at descending (newest first).
	//
	// Errors:
	//   - ErrInvalidKind     when kind is outside the canonical taxonomy.
	//   - ErrInvalidArgument when opts.Limit is negative.
	//   - ErrClosed          when the store has been closed.
	ListByKind(ctx context.Context, kind lore.Kind, opts lore.ListOpts) ([]lore.Entry, error)

	// SearchText runs a full-text query against Title and Body and returns
	// ranked hits. The query string is passed to the FTS engine as-is; callers
	// are responsible for any tokenization pre-processing. Higher Score values
	// are better; scores are not comparable across queries or implementations.
	//
	// Errors:
	//   - ErrInvalidArgument when query is empty or opts.Limit is negative.
	//   - ErrClosed          when the store has been closed.
	SearchText(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error)

	// AddEdge persists a directed edge from edge.FromID to edge.ToID labeled
	// edge.Relation. Re-adding an identical (FromID, ToID, Relation) triple is
	// a no-op and returns nil (idempotent).
	//
	// Errors:
	//   - ErrNotFound      when FromID or ToID does not match any persisted entry.
	//   - ErrInvalidArgument when Relation is empty.
	//   - ErrClosed        when the store has been closed.
	AddEdge(ctx context.Context, edge lore.Edge) error

	// ListEdges returns all edges whose FromID equals fromID. The result may
	// be empty when no edges have been added from fromID.
	//
	// Errors:
	//   - ErrInvalidArgument when fromID is zero or negative.
	//   - ErrClosed          when the store has been closed.
	ListEdges(ctx context.Context, fromID int64) ([]lore.Edge, error)

	// Close releases any resources held by the store. After Close returns, all
	// subsequent calls to store methods return ErrClosed. Close is idempotent:
	// calling it more than once returns nil.
	Close(ctx context.Context) error
}
