package bge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mathomhaus/lore/pkg/lore/embed"
	"github.com/mathomhaus/lore/pkg/lore/embed/bge"
)

// newOrSkip constructs a BGE embedder; skips the test if the platform is
// unsupported or the ONNX runtime library is not installed.
func newOrSkip(t *testing.T) embed.Embedder {
	t.Helper()
	emb, err := bge.New()
	if err != nil {
		if errors.Is(err, embed.ErrUnsupported) {
			t.Skipf("skipping: %v", err)
		}
		t.Fatalf("bge.New: %v", err)
	}
	t.Cleanup(func() {
		if err := emb.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return emb
}

// TestEmbed_Dimensions verifies that every output vector has length Dimensions().
func TestEmbed_Dimensions(t *testing.T) {
	emb := newOrSkip(t)
	ctx := context.Background()

	texts := []string{"hello world", "retrieval augmented generation", "agent memory"}
	vecs, err := emb.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("got %d vectors, want %d", len(vecs), len(texts))
	}
	for i, v := range vecs {
		if len(v) != emb.Dimensions() {
			t.Errorf("vecs[%d] has length %d, want %d", i, len(v), emb.Dimensions())
		}
	}
}

// TestEmbed_Determinism verifies that the same input produces identical
// output on two consecutive calls.
func TestEmbed_Determinism(t *testing.T) {
	emb := newOrSkip(t)
	ctx := context.Background()
	text := "deterministic embedding test"

	v1, err := emb.Embed(ctx, []string{text})
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	v2, err := emb.Embed(ctx, []string{text})
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}

	if len(v1[0]) != len(v2[0]) {
		t.Fatalf("vector length mismatch: %d vs %d", len(v1[0]), len(v2[0]))
	}
	for i := range v1[0] {
		if v1[0][i] != v2[0][i] {
			t.Errorf("v1[0][%d]=%v != v2[0][%d]=%v", i, v1[0][i], i, v2[0][i])
		}
	}
}

// TestEmbed_BatchSizes verifies that batches of size 1, 5, and 100 all succeed.
func TestEmbed_BatchSizes(t *testing.T) {
	emb := newOrSkip(t)
	ctx := context.Background()

	for _, n := range []int{1, 5, 100} {
		texts := make([]string, n)
		for i := range texts {
			texts[i] = "batch test item"
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			t.Errorf("Embed(batch=%d): %v", n, err)
			continue
		}
		if len(vecs) != n {
			t.Errorf("Embed(batch=%d): got %d vectors, want %d", n, len(vecs), n)
		}
	}
}

// TestEmbed_EmptyInput verifies that an empty texts slice returns ErrInvalidArgument.
func TestEmbed_EmptyInput(t *testing.T) {
	emb := newOrSkip(t)
	_, err := emb.Embed(context.Background(), []string{})
	if !errors.Is(err, embed.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestEmbed_EmptyString verifies that a slice containing an empty string returns ErrInvalidArgument.
func TestEmbed_EmptyString(t *testing.T) {
	emb := newOrSkip(t)
	_, err := emb.Embed(context.Background(), []string{"valid", "", "also valid"})
	if !errors.Is(err, embed.ErrInvalidArgument) {
		t.Errorf("expected ErrInvalidArgument, got %v", err)
	}
}

// TestClose_Idempotent verifies that calling Close multiple times does not error.
func TestClose_Idempotent(t *testing.T) {
	emb := newOrSkip(t)
	ctx := context.Background()

	if err := emb.Close(ctx); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := emb.Close(ctx); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestEmbed_DimensionsConst verifies that Dimensions() returns the package-
// level Dim constant.
func TestEmbed_DimensionsConst(t *testing.T) {
	emb := newOrSkip(t)
	if emb.Dimensions() != embed.Dim {
		t.Errorf("Dimensions()=%d, want %d", emb.Dimensions(), embed.Dim)
	}
}
