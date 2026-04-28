package heuristic

import "github.com/mathomhaus/lore/pkg/lore"

// Rule is a single path-based classification rule. When a walked file's
// path matches PathGlob (tested against both the repo-relative path and the
// base name), the file is assigned Kind and Tags without further inspection.
// Rules are tested in the order returned by [DefaultRules]; the first match
// wins.
type Rule struct {
	// PathGlob is a filepath.Match-style pattern. It is tested against the
	// repo-relative path and, as a convenience, also against the file's base
	// name alone so patterns like "CLAUDE.md" work regardless of nesting.
	PathGlob string

	// Kind is the lore kind assigned to chunks from matching files.
	Kind lore.Kind

	// Tags is the list of tags assigned to chunks from matching files. The
	// classifier deduplicates the slice before setting it on the entry.
	Tags []string
}

// DefaultRules returns the default rule table. Rules are tested in the order
// returned; the first match wins. Callers may pass a modified copy to
// [WithRules] to extend or replace these defaults.
//
// Default coverage:
//   - docs/adr/**  ADRs                 decision + adr
//   - docs/runbooks/**  Runbooks         procedure + runbook
//   - docs/playbooks/** Playbooks        procedure + playbook
//   - docs/concepts/**  Concept pages    explanation + concept
//   - docs/reference/** Reference pages  reference
//   - agents.md, skills.md, CLAUDE.md    reference + agent-config
//   - CONTRIBUTING.md                    procedure + contributing
//   - CHANGELOG.md                       observation + changelog
//   - *.md in project root               reference (catch-wide-root)
func DefaultRules() []Rule {
	return []Rule{
		// ADRs (architectural decision records).
		{PathGlob: "docs/adr/*.md", Kind: lore.KindDecision, Tags: []string{"adr"}},
		{PathGlob: "docs/adr/*.markdown", Kind: lore.KindDecision, Tags: []string{"adr"}},
		{PathGlob: "docs/decisions/*.md", Kind: lore.KindDecision, Tags: []string{"adr"}},
		{PathGlob: "docs/decisions/*.markdown", Kind: lore.KindDecision, Tags: []string{"adr"}},

		// Runbooks.
		{PathGlob: "docs/runbooks/*.md", Kind: lore.KindProcedure, Tags: []string{"runbook"}},
		{PathGlob: "docs/runbooks/*.markdown", Kind: lore.KindProcedure, Tags: []string{"runbook"}},

		// Playbooks.
		{PathGlob: "docs/playbooks/*.md", Kind: lore.KindProcedure, Tags: []string{"playbook"}},
		{PathGlob: "docs/playbooks/*.markdown", Kind: lore.KindProcedure, Tags: []string{"playbook"}},

		// Concept and explanation pages.
		{PathGlob: "docs/concepts/*.md", Kind: lore.KindExplanation, Tags: []string{"concept"}},
		{PathGlob: "docs/concepts/*.markdown", Kind: lore.KindExplanation, Tags: []string{"concept"}},

		// Reference pages.
		{PathGlob: "docs/reference/*.md", Kind: lore.KindReference, Tags: []string{"reference"}},
		{PathGlob: "docs/reference/*.markdown", Kind: lore.KindReference, Tags: []string{"reference"}},

		// Agent configuration files (base-name matching).
		{PathGlob: "agents.md", Kind: lore.KindReference, Tags: []string{"agent-config"}},
		{PathGlob: "agents.markdown", Kind: lore.KindReference, Tags: []string{"agent-config"}},
		{PathGlob: "skills.md", Kind: lore.KindReference, Tags: []string{"agent-config"}},
		{PathGlob: "skills.markdown", Kind: lore.KindReference, Tags: []string{"agent-config"}},
		{PathGlob: "CLAUDE.md", Kind: lore.KindReference, Tags: []string{"agent-config"}},
		{PathGlob: "AGENTS.md", Kind: lore.KindReference, Tags: []string{"agent-config"}},

		// Contributing guide.
		{PathGlob: "CONTRIBUTING.md", Kind: lore.KindProcedure, Tags: []string{"contributing"}},
		{PathGlob: "CONTRIBUTING.markdown", Kind: lore.KindProcedure, Tags: []string{"contributing"}},

		// Changelog: records of what changed.
		{PathGlob: "CHANGELOG.md", Kind: lore.KindObservation, Tags: []string{"changelog"}},
		{PathGlob: "CHANGELOG.markdown", Kind: lore.KindObservation, Tags: []string{"changelog"}},
	}
}
