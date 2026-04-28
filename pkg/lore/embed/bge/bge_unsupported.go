// Fallback for platforms where shota3506/onnxruntime-purego is not
// available (e.g. Windows). New returns ErrUnsupported so callers can
// deterministically fall through to lexical-only retrieval.

//go:build !unix

package bge

import (
	"github.com/mathomhaus/lore/pkg/lore/embed"
)

func newEmbedder(_ *config) (embed.Embedder, error) {
	return nil, embed.ErrUnsupported
}
