package ingest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mathomhaus/lore/pkg/lore/ingest/heuristic"
)

// ExampleIngester_process demonstrates running the heuristic ingester against
// a small directory of Markdown files and inspecting the classified entries.
func ExampleIngester_process() {
	// Build a minimal doc tree in a temp directory.
	root, err := os.MkdirTemp("", "lore-ingest-example-*")
	if err != nil {
		fmt.Println("mktemp:", err)
		return
	}
	defer os.RemoveAll(root)

	adrDir := filepath.Join(root, "docs", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		fmt.Println("mkdir:", err)
		return
	}

	files := map[string]string{
		filepath.Join(adrDir, "001-use-sqlite.md"): "# ADR-001: Use SQLite\n\nWe chose SQLite for zero-dependency deployment.",
		filepath.Join(adrDir, "002-use-wal.md"):    "# ADR-002: Enable WAL mode\n\nWAL allows concurrent readers with a single writer.",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			fmt.Println("write:", err)
			return
		}
	}

	ing := heuristic.NewIngester()
	result, err := ing.Process(context.Background(), root)
	if err != nil {
		fmt.Println("process:", err)
		return
	}

	fmt.Printf("entries=%d errors=%d\n", len(result.Entries), len(result.Errors))
	// Output:
	// entries=2 errors=0
}
