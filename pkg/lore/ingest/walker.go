package ingest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultMaxFileSize is the default upper bound on file size. Files
	// larger than this are skipped with a FileError. Override via
	// WalkerConfig.MaxFileSize.
	DefaultMaxFileSize int64 = 10 * 1024 * 1024 // 10 MB
)

// WalkerConfig holds the knobs that govern the filesystem walk.
type WalkerConfig struct {
	// MaxFileSize is the size threshold above which a file is skipped.
	// Zero or negative means "use DefaultMaxFileSize".
	MaxFileSize int64
}

// WalkResult is the output of a single WalkDir call.
type WalkResult struct {
	// Paths contains the absolute paths of accepted files in walk order.
	Paths []string

	// Errors collects per-entry failures: unreadable directories, oversized
	// files, and similar conditions that do not abort the whole walk.
	Errors []FileError
}

// skipDirs is the set of directory basenames the walker never descends into.
var skipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
}

// markdownExts is the set of file extensions the walker accepts.
// Comparison is case-insensitive so ".MD" is accepted too.
var markdownExts = map[string]struct{}{
	".md":       {},
	".markdown": {},
}

// WalkDir walks root with filepath.WalkDir and returns the paths of Markdown
// files that pass the filter criteria.
//
// Walk semantics for v0.1.1:
//   - One-shot, non-incremental.
//   - Symlinks are NOT followed.
//   - .gitignore patterns are NOT honored (v0.2 followup).
//
// Skipped unconditionally:
//   - Directories named in skipDirs (.git, node_modules, vendor).
//   - Hidden directories (basename starts with ".").
//   - Non-Markdown files; only .md and .markdown extensions are accepted.
//   - Files larger than cfg.MaxFileSize (default 10 MB).
func WalkDir(root string, cfg WalkerConfig) WalkResult {
	maxSize := cfg.MaxFileSize
	if maxSize <= 0 {
		maxSize = DefaultMaxFileSize
	}

	var res WalkResult

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entry: record and continue.
			res.Errors = append(res.Errors, FileError{Path: path, Err: err})
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		base := d.Name()

		if d.IsDir() {
			if path == root {
				return nil
			}
			if _, skip := skipDirs[base]; skip {
				return filepath.SkipDir
			}
			if strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only regular files; skip symlinks, pipes, devices, etc.
		if d.Type() != 0 {
			return nil
		}

		// Extension filter (case-insensitive).
		ext := strings.ToLower(filepath.Ext(base))
		if _, ok := markdownExts[ext]; !ok {
			return nil
		}

		// Size check.
		info, err := os.Stat(path)
		if err != nil {
			res.Errors = append(res.Errors, FileError{Path: path, Err: err})
			return nil
		}
		if info.Size() > maxSize {
			res.Errors = append(res.Errors, FileError{
				Path: path,
				Err:  newFileTooLargeError(path, info.Size(), maxSize),
			})
			return nil
		}

		res.Paths = append(res.Paths, path)
		return nil
	})

	if err != nil {
		res.Errors = append(res.Errors, FileError{Path: root, Err: err})
	}

	return res
}

func newFileTooLargeError(path string, size, limit int64) error {
	return &fileTooLargeError{path: path, size: size, limit: limit}
}

type fileTooLargeError struct {
	path  string
	size  int64
	limit int64
}

func (e *fileTooLargeError) Error() string {
	return fmt.Sprintf("ingest: walker: file too large: %s (%s > %s)",
		filepath.Base(e.path), formatBytes(e.size), formatBytes(e.limit))
}

func formatBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
	)
	switch {
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
