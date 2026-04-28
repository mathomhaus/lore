package lore

import (
	"errors"
	"fmt"
	"testing"
)

// Sentinel errors must remain distinct so callers can pattern-match on them.
func TestSentinelErrors_Distinct(t *testing.T) {
	all := []error{
		ErrNotFound,
		ErrDuplicate,
		ErrInvalidKind,
		ErrInvalidArgument,
		ErrConflict,
		ErrUnsupported,
		ErrClosed,
	}
	for i, a := range all {
		for j, b := range all {
			if i == j {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("sentinels %d and %d collide: %v == %v", i, j, a, b)
			}
		}
	}
}

// Wrapping with fmt.Errorf("%w") must preserve errors.Is matching.
func TestSentinelErrors_Wrap(t *testing.T) {
	wrapped := fmt.Errorf("read entry 42: %w", ErrNotFound)
	if !errors.Is(wrapped, ErrNotFound) {
		t.Fatalf("wrapped ErrNotFound did not match via errors.Is")
	}
	if errors.Is(wrapped, ErrDuplicate) {
		t.Fatalf("wrapped ErrNotFound matched unrelated sentinel ErrDuplicate")
	}
}
