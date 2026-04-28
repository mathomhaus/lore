package lore_test

import (
	"fmt"

	"github.com/mathomhaus/lore/pkg/lore"
)

// ExampleKind_Validate demonstrates how to validate a Kind value before
// writing an entry to a Store. Unknown kinds are rejected at write time
// by all standard implementations.
func ExampleKind_Validate() {
	good := lore.KindDecision
	if err := good.Validate(); err != nil {
		fmt.Println("unexpected error:", err)
	} else {
		fmt.Println("valid:", good)
	}

	bad := lore.Kind("unknown")
	if err := bad.Validate(); err != nil {
		fmt.Println("rejected unknown kind")
	}
	// Output:
	// valid: decision
	// rejected unknown kind
}

// ExampleAllKinds prints the canonical kind taxonomy in display order.
func ExampleAllKinds() {
	for _, k := range lore.AllKinds() {
		fmt.Println(k)
	}
	// Output:
	// decision
	// principle
	// procedure
	// reference
	// explanation
	// observation
	// research
	// idea
}

// ExampleKind_String shows that Kind satisfies fmt.Stringer and can be used
// directly in format strings.
func ExampleKind_String() {
	k := lore.KindProcedure
	fmt.Println(k.String())
	// Output:
	// procedure
}

// ExampleEntry_zero shows the zero value of Entry: all fields empty or nil,
// ready for population before passing to Store.Inscribe.
func ExampleEntry_zero() {
	var e lore.Entry
	fmt.Printf("id=%d kind=%q title=%q\n", e.ID, e.Kind, e.Title)
	// Output:
	// id=0 kind="" title=""
}

// ExampleSearchOpts shows how to construct a SearchOpts that restricts
// results to a project and a pair of kinds.
func ExampleSearchOpts() {
	opts := lore.SearchOpts{
		Project: "runbookCorpus",
		Kinds:   []lore.Kind{lore.KindProcedure, lore.KindReference},
		Limit:   10,
	}
	fmt.Printf("project=%s kinds=%d limit=%d\n", opts.Project, len(opts.Kinds), opts.Limit)
	// Output:
	// project=runbookCorpus kinds=2 limit=10
}
