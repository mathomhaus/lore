package lore

import "errors"

// Sentinel errors are the canonical machine-readable failure modes. Callers
// match them with errors.Is. Implementations that wrap these with
// fmt.Errorf("...: %w", err) preserve the chain.
var (
	// ErrNotFound indicates a requested entry, edge, or row does not exist.
	ErrNotFound = errors.New("lore: not found")

	// ErrDuplicate indicates a write would create a duplicate of an existing
	// row keyed by a uniqueness constraint (for example same-title same-kind
	// inside a single project, depending on the Store implementation).
	ErrDuplicate = errors.New("lore: duplicate")

	// ErrInvalidKind indicates a Kind value is outside the canonical
	// taxonomy. Returned by Kind.Validate and by write paths that classify
	// entries.
	ErrInvalidKind = errors.New("lore: invalid kind")

	// ErrInvalidArgument indicates a caller-supplied input failed validation
	// (empty title, malformed source, negative limit, and so on). Callers
	// should fix the input rather than retry.
	ErrInvalidArgument = errors.New("lore: invalid argument")

	// ErrConflict indicates an optimistic-concurrency check failed: the
	// underlying row changed between read and write. Callers may retry after
	// re-reading current state.
	ErrConflict = errors.New("lore: conflict")

	// ErrUnsupported indicates the requested operation is not implemented by
	// the active backend (for example a vector query against a Store that
	// only implements lexical retrieval).
	ErrUnsupported = errors.New("lore: unsupported")

	// ErrClosed indicates the component has been closed and rejects further
	// I/O. Callers must construct a new instance to continue.
	ErrClosed = errors.New("lore: closed")
)
