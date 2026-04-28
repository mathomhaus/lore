// Package heuristic provides the default Path B heuristic classifier for lore
// ingestion. It implements [ingest.Ingester] using a rule-priority stack:
//
//  1. YAML front matter with explicit kind and tags fields: use as-is.
//  2. Path rules: filepath.Match-style globs against the file path.
//  3. Heading patterns: keywords in H1/H2/H3 heading text.
//  4. Fallback: kind=research (catch-all).
//
// The classifier is configurable via functional options. The zero-value rule
// set (from [DefaultRules]) covers the most common documentation patterns; use
// [WithRules] to replace it entirely.
package heuristic

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/mathomhaus/lore/pkg/lore"
	"github.com/mathomhaus/lore/pkg/lore/ingest"
)

const tracerName = "lore/ingest/heuristic"

// ingester is the concrete implementation of [ingest.Ingester].
type ingester struct {
	rules  []Rule
	log    *slog.Logger
	tracer trace.Tracer
	walker ingest.WalkerConfig
}

// Option is a functional option for [NewIngester].
type Option func(*ingester)

// WithRules replaces the default classification rule table with the supplied
// slice. Rules are tested in order; the first match wins.
func WithRules(rules []Rule) Option {
	return func(i *ingester) {
		i.rules = rules
	}
}

// WithLogger sets the structured logger. When nil, logging is discarded.
func WithLogger(l *slog.Logger) Option {
	return func(i *ingester) {
		if l != nil {
			i.log = l
		}
	}
}

// WithTracer overrides the OTel tracer used for spans. When nil, the global
// tracer provider is used via [otel.Tracer].
func WithTracer(t trace.Tracer) Option {
	return func(i *ingester) {
		if t != nil {
			i.tracer = t
		}
	}
}

// WithMaxFileSize sets the per-file size limit above which a file is skipped.
// Zero or negative resets to the default (10 MB).
func WithMaxFileSize(n int64) Option {
	return func(i *ingester) {
		i.walker.MaxFileSize = n
	}
}

// NewIngester returns a heuristic [ingest.Ingester] configured by opts.
// When no [WithRules] option is supplied, [DefaultRules] is used.
func NewIngester(opts ...Option) ingest.Ingester {
	ing := &ingester{
		rules:  DefaultRules(),
		log:    slog.Default(),
		tracer: otel.Tracer(tracerName),
	}
	for _, o := range opts {
		o(ing)
	}
	return ing
}

// Process implements [ingest.Ingester]. It walks root, chunks each Markdown
// file, classifies each chunk, and returns the results. It does not write to
// any store.
func (ing *ingester) Process(ctx context.Context, root string) (ingest.Result, error) {
	ctx, span := ing.tracer.Start(ctx, "lore.ingest.process",
		trace.WithAttributes(attribute.String("root", root)),
	)
	defer span.End()

	wr := ingest.WalkDir(root, ing.walker)

	ing.log.InfoContext(ctx, "ingest: walk complete",
		"root", root,
		"files", len(wr.Paths),
		"walk_errors", len(wr.Errors),
	)

	span.SetAttributes(attribute.Int("path.count", len(wr.Paths)))

	var result ingest.Result
	result.Errors = append(result.Errors, wr.Errors...)

	for _, path := range wr.Paths {
		entries, ferr := ing.processFile(ctx, path, root)
		if ferr != nil {
			ing.log.WarnContext(ctx, "ingest: file error", "path", path, "err", ferr)
			result.Errors = append(result.Errors, ingest.FileError{Path: path, Err: ferr})
			continue
		}
		result.Entries = append(result.Entries, entries...)
	}

	span.SetAttributes(attribute.Int("entries.count", len(result.Entries)))
	ing.log.InfoContext(ctx, "ingest: process complete",
		"entries", len(result.Entries),
		"file_errors", len(result.Errors),
	)

	return result, nil
}

// processFile chunks and classifies a single file.
func (ing *ingester) processFile(ctx context.Context, path, root string) ([]lore.Entry, error) {
	_, span := ing.tracer.Start(ctx, "lore.ingest.classify",
		trace.WithAttributes(attribute.String("file", path)),
	)
	defer span.End()

	chunks, err := ingest.ChunkFile(path, root)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	entries := make([]lore.Entry, 0, len(chunks))
	for _, c := range chunks {
		kind, tags := ing.classify(c, path, root)
		entries = append(entries, lore.Entry{
			Kind:      kind,
			Title:     c.Title,
			Body:      c.Body,
			Source:    c.Source,
			Tags:      tags,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	return entries, nil
}

// classify determines the Kind and Tags for a chunk using the four-level
// priority stack described in the package doc.
func (ing *ingester) classify(c ingest.Chunk, absPath, root string) (lore.Kind, []string) {
	// 1. YAML front matter with explicit kind.
	if c.FM.Kind != "" {
		k := lore.Kind(c.FM.Kind)
		if err := k.Validate(); err == nil {
			return k, dedupeStrings(c.FM.Tags)
		}
		// Invalid kind in frontmatter: fall through to path rules.
	}

	// 2. Path rules. Test both the repo-relative path and the base name.
	relPath := ingest.RelativeOrBase(absPath, root)
	base := filepath.Base(absPath)

	for _, rule := range ing.rules {
		if matched, _ := filepath.Match(rule.PathGlob, relPath); matched {
			return rule.Kind, dedupeStrings(rule.Tags)
		}
		if matched, _ := filepath.Match(rule.PathGlob, base); matched {
			return rule.Kind, dedupeStrings(rule.Tags)
		}
	}

	// 3. Heading patterns.
	if kind, ok := classifyByHeadings(c.Body); ok {
		return kind, nil
	}

	// 4. Fallback.
	return lore.KindResearch, nil
}

// classifyByHeadings scans the headings in body for classification keywords.
// Returns (kind, true) on the first match.
func classifyByHeadings(body string) (lore.Kind, bool) {
	for _, line := range strings.Split(body, "\n") {
		title, ok := ingest.ExtractHeading(line)
		if !ok {
			continue
		}
		lower := strings.ToLower(title)

		switch {
		case containsAny(lower, "procedure", "how to", "how-to", "steps", "runbook", "playbook"):
			return lore.KindProcedure, true
		case containsAny(lower, "decision", "context", "consequences", "status", "adr"):
			return lore.KindDecision, true
		case containsAny(lower, "what is", "what are", "overview", "introduction", "concept", "explanation"):
			return lore.KindExplanation, true
		case containsAny(lower, "principle", "rule", "guideline", "standard"):
			return lore.KindPrinciple, true
		case containsAny(lower, "observation", "finding", "measurement", "postmortem"):
			return lore.KindObservation, true
		case containsAny(lower, "research", "spike", "investigation", "exploration"):
			return lore.KindResearch, true
		}
	}
	return "", false
}

// containsAny reports whether s contains any of the supplied substrings.
func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// dedupeStrings returns a deduplicated copy of ss preserving order.
func dedupeStrings(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
