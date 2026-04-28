// Package retrieve defines the Retriever interface and shared result types
// for lore's hybrid lexical+semantic search layer. Concrete implementations
// live in the sub-packages: bm25, vector, and hybrid.
//
// Architecture summary:
//
//	hybrid.New(store, embedder, vstore) → Retriever
//
// The hybrid retriever composes a BM25 lexical arm (via store.SearchText),
// a vector semantic arm (via embedder.Embed + vstore.Search), and fuses the
// two ranked lists with Reciprocal Rank Fusion (rrf.Fuse) into a single
// ordered result list.
//
// Callers that only want one arm can use bm25.New(store) or vector.New(embedder, vstore)
// directly; both satisfy the same Retriever interface.
package retrieve

import (
	"context"

	"github.com/mathomhaus/lore/pkg/lore"
)

// Retriever runs a hybrid lexical + semantic search and returns ranked results.
// Implementations compose Store (for BM25 over title/body), Embedder (for
// query to vector), and VectorStore (for vector search), and fuse the two
// rankings into a single ordered list.
//
// All implementations must be safe for concurrent use by multiple goroutines.
type Retriever interface {
	// Search executes the retrieval pipeline for the given query string and
	// returns results ranked by descending score. The query must be non-empty;
	// Search returns ErrInvalidArgument otherwise.
	//
	// opts.Limit caps the result count. Zero means implementation default
	// (typically 10). Negative is invalid and returns ErrInvalidArgument.
	//
	// opts.Project, when non-empty, restricts results to a single project.
	Search(ctx context.Context, query string, opts lore.SearchOpts) ([]lore.SearchHit, error)
}
