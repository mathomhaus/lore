package lore

import "fmt"

// Kind classifies a lore entry. The canonical taxonomy has eight values that
// together cover both document-derived knowledge (procedure, reference,
// explanation) and agent-derived knowledge (decision, principle, observation,
// research, idea). Diátaxis prior art motivates the document-derived three;
// the rest carry classifications agents naturally produce while reasoning.
//
// Callers should treat Kind as an opaque typed string and use the constants
// declared below. New kinds may be added in future versions; consumers that
// switch on Kind should always handle an unknown value gracefully.
type Kind string

// Canonical kinds. Lore validates these on every write path and rejects any
// other value with ErrInvalidKind.
const (
	// KindDecision records a choice made with rationale. ADRs, "we chose X
	// because Y", policy decisions.
	KindDecision Kind = "decision"

	// KindPrinciple records a durable rule or invariant. "Always X", "never Y",
	// coding standards.
	KindPrinciple Kind = "principle"

	// KindProcedure records a step-by-step how-to. Runbooks, deploy guides,
	// incident response playbooks.
	KindProcedure Kind = "procedure"

	// KindReference records a fact to look up. Service catalogs, API specs,
	// configuration tables, glossaries.
	KindReference Kind = "reference"

	// KindExplanation records a concept or mental model. Architecture
	// overviews, "how auth works", design walkthroughs.
	KindExplanation Kind = "explanation"

	// KindObservation records an empirical finding. "We measured X", "we saw
	// Y", postmortems.
	KindObservation Kind = "observation"

	// KindResearch records the result of an investigation. Spike outputs,
	// "we explored X, here's what we found".
	KindResearch Kind = "research"

	// KindIdea records a proposal not yet decided. Design sketches, open
	// questions, "what if we did X".
	KindIdea Kind = "idea"
)

// AllKinds returns the canonical kinds in display order. The order is stable
// and may be relied upon by UIs, tests, and migration tooling.
func AllKinds() []Kind {
	return []Kind{
		KindDecision,
		KindPrinciple,
		KindProcedure,
		KindReference,
		KindExplanation,
		KindObservation,
		KindResearch,
		KindIdea,
	}
}

// Validate reports whether k is one of the canonical kinds. It returns
// ErrInvalidKind wrapped with the offending value when validation fails so
// callers can inspect both the sentinel and the bad input.
func (k Kind) Validate() error {
	switch k {
	case KindDecision,
		KindPrinciple,
		KindProcedure,
		KindReference,
		KindExplanation,
		KindObservation,
		KindResearch,
		KindIdea:
		return nil
	default:
		return fmt.Errorf("kind %q: %w", string(k), ErrInvalidKind)
	}
}

// String returns the wire-format string for k. It is the identity function on
// the underlying string and is provided for symmetry with fmt.Stringer
// expectations.
func (k Kind) String() string {
	return string(k)
}
