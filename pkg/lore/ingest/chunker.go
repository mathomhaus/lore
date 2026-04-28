package ingest

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter holds the fields we parse from a YAML front matter block.
// All fields are optional; unrecognized keys are silently ignored.
type Frontmatter struct {
	Kind string   `yaml:"kind"`
	Tags []string `yaml:"tags"`
}

// Chunk is an intermediate representation of one heading-bounded section of
// a Markdown file. It is enriched into a lore.Entry by the classifier.
type Chunk struct {
	// Title is the text of the heading that opened this chunk, or the file
	// stem when the file has no headings.
	Title string

	// Body is the raw Markdown for this chunk, including the opening heading
	// line.
	Body string

	// Source is "<repo-relative-path>:<line-number>" of the heading.
	Source string

	// FM is the parsed front matter from the file. It is identical for
	// every chunk derived from the same file; per-chunk overrides are not
	// supported in v0.1.1.
	FM Frontmatter

	// FilePath is the absolute path of the source file.
	FilePath string
}

// ChunkFile reads the Markdown file at path, extracts any YAML front matter,
// and splits the remainder at H1/H2/H3 boundaries. Each boundary produces one
// Chunk. If the file has no headings, the whole body is returned as a single
// Chunk whose title is the filename stem.
//
// The Source field on each chunk is set to "<relPath>:<lineNumber>" where
// relPath is path relative to root. If root is empty or path is not under
// root, the base name is used instead.
func ChunkFile(path, root string) ([]Chunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("ingest: chunker: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	relPath := RelativeOrBase(path, root)

	scanner := bufio.NewScanner(f)

	var fm Frontmatter
	var bodyLines []string

	lineNum := 0
	inFrontmatter := false
	var fmLines []string
	fmDone := false
	bodyLineOffset := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if lineNum == 1 && line == "---" {
			inFrontmatter = true
			continue
		}

		if inFrontmatter {
			if line == "---" || line == "..." {
				inFrontmatter = false
				fmDone = true
				bodyLineOffset = lineNum
				continue
			}
			fmLines = append(fmLines, line)
			continue
		}

		if !fmDone {
			bodyLineOffset = lineNum - 1
			fmDone = true
		}

		bodyLines = append(bodyLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ingest: chunker: scan %s: %w", path, err)
	}

	if len(fmLines) > 0 {
		_ = yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm)
	}

	// Absolute line number of the first body line.
	firstBodyLine := bodyLineOffset + 1

	// Find all H1/H2/H3 boundary positions.
	type boundary struct {
		lineIdx int
		title   string
	}

	var boundaries []boundary
	for i, line := range bodyLines {
		if title, ok := ExtractHeading(line); ok {
			boundaries = append(boundaries, boundary{lineIdx: i, title: title})
		}
	}

	fileTitle := stemName(filepath.Base(path))

	// No headings: whole body is one chunk.
	if len(boundaries) == 0 {
		body := strings.Join(bodyLines, "\n")
		src := fmt.Sprintf("%s:%d", relPath, firstBodyLine)
		if strings.TrimSpace(body) == "" {
			return nil, nil
		}
		return []Chunk{{
			Title:    fileTitle,
			Body:     body,
			Source:   src,
			FM:       fm,
			FilePath: path,
		}}, nil
	}

	chunks := make([]Chunk, 0, len(boundaries))
	for idx, b := range boundaries {
		var endIdx int
		if idx+1 < len(boundaries) {
			endIdx = boundaries[idx+1].lineIdx
		} else {
			endIdx = len(bodyLines)
		}

		chunkBodyLines := bodyLines[b.lineIdx:endIdx]
		body := strings.Join(chunkBodyLines, "\n")
		absLineNum := firstBodyLine + b.lineIdx
		src := fmt.Sprintf("%s:%d", relPath, absLineNum)

		chunks = append(chunks, Chunk{
			Title:    b.title,
			Body:     body,
			Source:   src,
			FM:       fm,
			FilePath: path,
		})
	}

	return chunks, nil
}

// ExtractHeading returns the heading text and true if line is an ATX-style
// H1, H2, or H3 Markdown heading (one, two, or three leading # characters
// followed by a space and non-empty text). Returns ("", false) for anything
// else.
func ExtractHeading(line string) (string, bool) {
	if len(line) < 3 || line[0] != '#' {
		return "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 3 || level >= len(line) || line[level] != ' ' {
		return "", false
	}
	title := strings.TrimSpace(line[level+1:])
	if title == "" {
		return "", false
	}
	return title, true
}

// stemName returns the filename without its extension.
func stemName(base string) string {
	ext := filepath.Ext(base)
	if ext == "" {
		return base
	}
	return base[:len(base)-len(ext)]
}

// RelativeOrBase returns path relative to root. If the path is not under root
// or Rel returns an error, it falls back to filepath.Base(path).
func RelativeOrBase(path, root string) string {
	if root == "" {
		return filepath.Base(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Base(path)
	}
	return rel
}
