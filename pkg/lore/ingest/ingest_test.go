package ingest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/ingest"
	"github.com/mathomhaus/lore/pkg/lore/ingest/heuristic"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	// __file__ is relative to the package dir; testdata is a sibling.
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("testdata abs path: %v", err)
	}
	return dir
}

// newIngester builds a default heuristic ingester for tests.
func newIngester() ingest.Ingester {
	return heuristic.NewIngester()
}

// findEntry returns the first entry in result whose Title contains substr.
func findEntry(entries []lore.Entry, substr string) (lore.Entry, bool) {
	for _, e := range entries {
		if containsStr(e.Title, substr) {
			return e, true
		}
	}
	return lore.Entry{}, false
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// hasTag reports whether e has the given tag.
func hasTag(e lore.Entry, tag string) bool {
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// TestProcess_Frontmatter verifies that explicit kind/tags in YAML front
// matter take precedence over path rules and heading patterns.
func TestProcess_Frontmatter(t *testing.T) {
	root := testdataDir(t)
	ing := newIngester()

	result, err := ing.Process(context.Background(), root)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// The ADR file has frontmatter kind=decision, tags=[adr, language].
	e, ok := findEntry(result.Entries, "Use Go for the implementation")
	if !ok {
		t.Fatalf("expected entry with title 'Use Go for the implementation'; entries: %v", titles(result.Entries))
	}
	if e.Kind != lore.KindDecision {
		t.Errorf("kind: got %q, want %q", e.Kind, lore.KindDecision)
	}
	if !hasTag(e, "adr") {
		t.Errorf("expected tag 'adr'; tags: %v", e.Tags)
	}
	if !hasTag(e, "language") {
		t.Errorf("expected tag 'language'; tags: %v", e.Tags)
	}
}

// TestProcess_PathRules verifies that path-based rules classify files
// correctly even without explicit front matter.
func TestProcess_PathRules(t *testing.T) {
	root := testdataDir(t)
	ing := newIngester()

	result, err := ing.Process(context.Background(), root)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// docs/runbooks/deploy.md should be kind=procedure + tag=runbook.
	e, ok := findEntry(result.Entries, "Deploy to production")
	if !ok {
		t.Fatalf("expected entry 'Deploy to production'; entries: %v", titles(result.Entries))
	}
	if e.Kind != lore.KindProcedure {
		t.Errorf("deploy kind: got %q, want %q", e.Kind, lore.KindProcedure)
	}
	if !hasTag(e, "runbook") {
		t.Errorf("deploy: expected tag 'runbook'; tags: %v", e.Tags)
	}

	// CLAUDE.md should be kind=reference + tag=agent-config.
	c, ok := findEntry(result.Entries, "lore agent bootstrap")
	if !ok {
		t.Fatalf("expected entry 'lore agent bootstrap'; entries: %v", titles(result.Entries))
	}
	if c.Kind != lore.KindReference {
		t.Errorf("CLAUDE.md kind: got %q, want %q", c.Kind, lore.KindReference)
	}
	if !hasTag(c, "agent-config") {
		t.Errorf("CLAUDE.md: expected tag 'agent-config'; tags: %v", c.Tags)
	}
}

// TestProcess_HeadingPatterns verifies that files without explicit front
// matter or matching path rules are classified by heading keywords.
func TestProcess_HeadingPatterns(t *testing.T) {
	root := testdataDir(t)
	ing := newIngester()

	result, err := ing.Process(context.Background(), root)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// docs/concepts/auth.md has "## What is auth" which signals explanation.
	e, ok := findEntry(result.Entries, "What is auth")
	if !ok {
		t.Fatalf("expected entry 'What is auth'; entries: %v", titles(result.Entries))
	}
	if e.Kind != lore.KindExplanation {
		t.Errorf("auth concept kind: got %q, want %q", e.Kind, lore.KindExplanation)
	}
}

// TestProcess_Fallback verifies that files not matched by any rule or
// heading pattern fall back to kind=research.
func TestProcess_Fallback(t *testing.T) {
	root := testdataDir(t)
	ing := newIngester()

	result, err := ing.Process(context.Background(), root)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// notes/random.md has no frontmatter, no matching path rule, no
	// classification headings. It should fall back to kind=research.
	e, ok := findEntry(result.Entries, "Meeting notes")
	if !ok {
		t.Fatalf("expected entry 'Meeting notes'; entries: %v", titles(result.Entries))
	}
	if e.Kind != lore.KindResearch {
		t.Errorf("random notes kind: got %q, want %q", e.Kind, lore.KindResearch)
	}
}

// TestProcess_FileErrors verifies that an unreadable file produces a
// FileError in the result but does not abort the rest of the walk.
func TestProcess_FileErrors(t *testing.T) {
	// Build a temp tree with one valid file and one unreadable file.
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.md")
	badPath := filepath.Join(dir, "bad.md")

	if err := os.WriteFile(goodPath, []byte("# Good\n\nContent.\n"), 0o644); err != nil {
		t.Fatalf("write good.md: %v", err)
	}
	if err := os.WriteFile(badPath, []byte("# Bad\n\nContent.\n"), 0o000); err != nil {
		t.Fatalf("write bad.md: %v", err)
	}

	ing := newIngester()
	result, err := ing.Process(context.Background(), dir)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	// good.md should produce one entry.
	if len(result.Entries) == 0 {
		t.Error("expected at least one entry from good.md")
	}

	// bad.md should produce one FileError.
	if len(result.Errors) == 0 {
		t.Error("expected at least one FileError for unreadable bad.md")
	}

	found := false
	for _, fe := range result.Errors {
		if fe.Path == badPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected FileError for %s; got: %v", badPath, result.Errors)
	}
}

// TestWalker_SkipsHidden verifies that the walker does not descend into
// .git, node_modules, vendor, or other hidden directories.
func TestWalker_SkipsHidden(t *testing.T) {
	dir := t.TempDir()

	// Create a file in a skipped directory.
	for _, skip := range []string{".git", "node_modules", "vendor", ".hidden"} {
		skipDir := filepath.Join(dir, skip)
		if err := os.MkdirAll(skipDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", skipDir, err)
		}
		f := filepath.Join(skipDir, "doc.md")
		if err := os.WriteFile(f, []byte("# Should be skipped\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	// Add one reachable file.
	good := filepath.Join(dir, "readme.md")
	if err := os.WriteFile(good, []byte("# Readme\n"), 0o644); err != nil {
		t.Fatalf("write readme.md: %v", err)
	}

	wr := ingest.WalkDir(dir, ingest.WalkerConfig{})
	if len(wr.Errors) != 0 {
		t.Errorf("unexpected walk errors: %v", wr.Errors)
	}
	if len(wr.Paths) != 1 {
		t.Errorf("expected exactly 1 path; got %d: %v", len(wr.Paths), wr.Paths)
	}
	if len(wr.Paths) == 1 && wr.Paths[0] != good {
		t.Errorf("path: got %q, want %q", wr.Paths[0], good)
	}
}

// TestChunker_Headings verifies that H1/H2/H3 headings produce separate
// chunks, each with the correct title and source line number.
func TestChunker_Headings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")

	content := `# Title one

First paragraph.

## Section two

Second paragraph.

### Subsection three

Third paragraph.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write doc.md: %v", err)
	}

	chunks, err := ingest.ChunkFile(path, dir)
	if err != nil {
		t.Fatalf("ChunkFile: %v", err)
	}

	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %v", len(chunks), chunkTitles(chunks))
	}

	wantTitles := []string{"Title one", "Section two", "Subsection three"}
	for i, c := range chunks {
		if c.Title != wantTitles[i] {
			t.Errorf("chunk[%d].Title: got %q, want %q", i, c.Title, wantTitles[i])
		}
	}

	// First chunk starts at line 1 (no frontmatter).
	if chunks[0].Source != "doc.md:1" {
		t.Errorf("chunk[0].Source: got %q, want %q", chunks[0].Source, "doc.md:1")
	}
}

// TestChunker_Frontmatter verifies that YAML front matter is parsed
// and attached to all chunks from the file.
func TestChunker_Frontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fm.md")

	content := `---
kind: decision
tags: [adr, test]
---

# My decision

Body text.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fm.md: %v", err)
	}

	chunks, err := ingest.ChunkFile(path, dir)
	if err != nil {
		t.Fatalf("ChunkFile: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}

	c := chunks[0]
	if c.FM.Kind != "decision" {
		t.Errorf("FM.Kind: got %q, want %q", c.FM.Kind, "decision")
	}
	if len(c.FM.Tags) != 2 || c.FM.Tags[0] != "adr" || c.FM.Tags[1] != "test" {
		t.Errorf("FM.Tags: got %v, want [adr test]", c.FM.Tags)
	}
}

// TestChunker_NoHeadings verifies that a file with no headings returns a
// single chunk with the filename stem as the title.
func TestChunker_NoHeadings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.md")

	if err := os.WriteFile(path, []byte("Just some plain text.\n"), 0o644); err != nil {
		t.Fatalf("write plain.md: %v", err)
	}

	chunks, err := ingest.ChunkFile(path, dir)
	if err != nil {
		t.Fatalf("ChunkFile: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Title != "plain" {
		t.Errorf("Title: got %q, want %q", chunks[0].Title, "plain")
	}
}

// titles returns a slice of entry titles for diagnostic messages.
func titles(entries []lore.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Title
	}
	return out
}

// chunkTitles returns a slice of chunk titles for diagnostic messages.
func chunkTitles(chunks []ingest.Chunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Title
	}
	return out
}
