package lore

import (
	"errors"
	"testing"
)

func TestKindValidate_Canonical(t *testing.T) {
	for _, k := range AllKinds() {
		t.Run(string(k), func(t *testing.T) {
			if err := k.Validate(); err != nil {
				t.Fatalf("canonical kind %q rejected: %v", k, err)
			}
		})
	}
}

func TestKindValidate_Invalid(t *testing.T) {
	cases := []Kind{
		"",
		"unknown",
		"Decision",  // case-sensitive
		"decisions", // plural
		" decision",
	}
	for _, k := range cases {
		t.Run(string(k), func(t *testing.T) {
			err := k.Validate()
			if err == nil {
				t.Fatalf("expected error for %q, got nil", k)
			}
			if !errors.Is(err, ErrInvalidKind) {
				t.Fatalf("expected ErrInvalidKind, got %v", err)
			}
		})
	}
}

func TestAllKinds_Count(t *testing.T) {
	got := AllKinds()
	const want = 8
	if len(got) != want {
		t.Fatalf("AllKinds returned %d kinds, want %d", len(got), want)
	}
}

func TestAllKinds_Membership(t *testing.T) {
	want := map[Kind]bool{
		KindDecision:    true,
		KindPrinciple:   true,
		KindProcedure:   true,
		KindReference:   true,
		KindExplanation: true,
		KindObservation: true,
		KindResearch:    true,
		KindIdea:        true,
	}
	got := AllKinds()
	if len(got) != len(want) {
		t.Fatalf("AllKinds size mismatch: got %d, want %d", len(got), len(want))
	}
	seen := make(map[Kind]bool, len(got))
	for _, k := range got {
		if !want[k] {
			t.Errorf("AllKinds returned unexpected kind %q", k)
		}
		if seen[k] {
			t.Errorf("AllKinds returned duplicate kind %q", k)
		}
		seen[k] = true
	}
}

func TestKindString(t *testing.T) {
	if got := KindDecision.String(); got != "decision" {
		t.Fatalf("KindDecision.String() = %q, want %q", got, "decision")
	}
}
