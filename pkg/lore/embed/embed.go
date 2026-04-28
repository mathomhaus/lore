// Package embed defines the Embedder interface used by lore's retrieval
// pipeline to turn text into dense vectors. The interface is deliberately
// minimal and batch-oriented: one Embed call per batch keeps allocation and
// round-trip overhead low for both in-process models and remote APIs.
//
// The reference implementation lives in the bge sub-package: an int8-
// quantized BGE-small model loaded from go:embed assets, running pure CPU
// inference with no network calls and no cgo.
//
// Consumers that want to swap in a remote embedding API, a different model,
// or a test stub only need to satisfy this interface.
package embed

import (
	"context"
	"errors"
)

// Dim is the embedding dimension for BAAI/bge-small-en-v1.5 and its
// quantized variants. Every concrete Embedder in this package emits vectors
// of exactly this length (or returns an error).
const Dim = 384

// Embedder turns text into vectors. Batch-only by design: callers wrap a
// single string with []string{s} when they only have one. Implementations
// must be safe for concurrent calls.
type Embedder interface {
	// Embed produces one vector per input string. Returns ErrInvalidArgument
	// if texts is empty or any element is empty. Vectors all have length
	// Dimensions().
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the vector length emitted by Embed. Stable for the
	// lifetime of the Embedder; consumers verify this matches their
	// VectorStore.
	Dimensions() int

	// Close releases any held resources (loaded models, tokenizers).
	// Idempotent: subsequent calls return nil.
	Close(ctx context.Context) error
}

// Typed errors. Production code should compare with errors.Is, not string
// matching.
var (
	// ErrInvalidArgument is returned when Embed receives an empty texts
	// slice or a slice that contains an empty string. Callers should
	// validate inputs before calling Embed.
	ErrInvalidArgument = errors.New("embed: invalid argument")

	// ErrUnsupported signals that this platform does not have a working
	// embedder (unsupported OS/arch, dylib probe failed, etc.). Callers
	// may fall through to lexical-only retrieval.
	ErrUnsupported = errors.New("embed: unsupported platform")

	// ErrClosed signals that Embed was called after Close. Callers should
	// construct a new Embedder rather than retrying the closed instance.
	ErrClosed = errors.New("embed: embedder closed")
)
