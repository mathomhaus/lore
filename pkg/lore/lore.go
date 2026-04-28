package lore

import "time"

// Entry is the canonical record stored by lore. One entry corresponds to one
// classified piece of knowledge. The shape is deliberately small: the eight
// canonical kinds plus open-ended tags and a free-form metadata map cover
// both document-derived ingestion and agent-mediated inscribe paths without
// forcing schema churn for new use cases.
//
// Implementations of Store decide how to persist nullable and slice fields
// (typically: empty string for unset Source, empty slice for no Tags, nil
// map for no Metadata). The library treats nil and empty as equivalent.
type Entry struct {
	// ID is the storage-assigned identifier. Zero on entries that have not
	// yet been persisted.
	ID int64

	// Project scopes an entry to a logical workspace. Implementations may
	// use this for filtering, partitioning, or access control. Empty string
	// is permitted and represents the default project.
	Project string

	// Kind is one of the canonical eight values declared in kind.go.
	Kind Kind

	// Title is a short distinctive label, suitable for display in lists and
	// for boosting in lexical retrieval. Required on write.
	Title string

	// Body is the full prose content of the entry. May be markdown, plain
	// text, or any other UTF-8 payload the caller wishes to store.
	Body string

	// Source records where the entry came from. Free-form by design:
	// implementations and consumers may agree on conventions like
	// "github://owner/repo/path.md#anchor" or "agent://session/inscribe-id".
	Source string

	// Tags are open-ended classifiers. They complement Kind for finer
	// specificity (a decision tagged "adr,architecture,microservices") and
	// are searchable by lexical retrievers.
	Tags []string

	// Metadata carries arbitrary string key/value pairs for extensibility.
	// Reserved keys may be defined by future versions; consumers should
	// namespace their own keys to avoid collisions.
	Metadata map[string]string

	// CreatedAt is the wall-clock time the entry was first persisted.
	CreatedAt time.Time

	// UpdatedAt is the wall-clock time the entry was last modified.
	UpdatedAt time.Time
}

// Edge is a typed directed link between two entries. Edges enable provenance
// tracing ("entry A informs entry B"), supersession chains, and arbitrary
// caller-defined relations. Lore does not enforce a closed vocabulary on
// Relation: implementations and consumers pick their own conventions.
type Edge struct {
	// FromID and ToID are the storage IDs of the linked entries.
	FromID int64
	ToID   int64

	// Relation labels the edge. Common values include "informs",
	// "supersedes", "contradicts", "depends-on", and "describes". The
	// library does not validate the value; consumers may.
	Relation string

	// Weight optionally orders edges of the same relation between the same
	// pair. Implementations may use it for ranking; zero is a valid neutral.
	Weight float64

	// CreatedAt is the wall-clock time the edge was first persisted.
	CreatedAt time.Time
}

// SearchHit is one ranked result from a retrieval call. The Score field is
// implementation-defined: lexical retrievers typically return BM25 or TF-IDF
// scores, vector retrievers return similarity (often cosine), and fused
// retrievers return reciprocal-rank-fusion scores. Higher is always better.
type SearchHit struct {
	// Entry is the matched record.
	Entry Entry

	// Score is the retrieval score for this hit. Comparable across hits
	// from the same query call; not necessarily comparable across queries
	// or across retriever implementations.
	Score float64

	// Highlights optionally carries snippets from the matched body, with
	// implementation-defined markers around the matching terms. May be nil.
	Highlights []string
}

// ListOpts narrows a list query. Zero values mean "no filter".
type ListOpts struct {
	// Project, when non-empty, restricts results to a single project.
	Project string

	// Kind, when non-empty, restricts results to a single kind.
	Kind Kind

	// Tag, when non-empty, restricts results to entries that carry this tag.
	Tag string

	// Limit caps the number of returned entries. Zero means
	// implementation-default (typically a small page size); negative is
	// invalid and returns ErrInvalidArgument.
	Limit int

	// Offset skips the first N results. Zero means "from the start".
	// Negative is invalid and returns ErrInvalidArgument.
	Offset int
}

// SearchOpts configures a retrieval call. Zero values mean
// "implementation-default".
type SearchOpts struct {
	// Project, when non-empty, restricts results to a single project.
	Project string

	// Kinds, when non-empty, restricts results to entries whose Kind is
	// in the slice.
	Kinds []Kind

	// Tags, when non-empty, restricts results to entries that carry every
	// listed tag (intersection, not union).
	Tags []string

	// Limit caps the number of returned hits. Zero means
	// implementation-default; negative is invalid and returns
	// ErrInvalidArgument.
	Limit int
}
