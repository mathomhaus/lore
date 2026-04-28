package lore

import "testing"

// Compile-time sanity: zero values of the public structs are usable.
func TestEntry_ZeroValue(t *testing.T) {
	var e Entry
	if e.ID != 0 || e.Title != "" || e.Tags != nil || e.Metadata != nil {
		t.Fatalf("Entry zero value has unexpected non-zero fields: %+v", e)
	}
}

func TestEdge_ZeroValue(t *testing.T) {
	var ed Edge
	if ed.FromID != 0 || ed.ToID != 0 || ed.Relation != "" || ed.Weight != 0 {
		t.Fatalf("Edge zero value has unexpected non-zero fields: %+v", ed)
	}
}

func TestSearchHit_ZeroValue(t *testing.T) {
	var h SearchHit
	if h.Score != 0 || h.Highlights != nil {
		t.Fatalf("SearchHit zero value has unexpected non-zero fields: %+v", h)
	}
}

func TestListOpts_ZeroValue(t *testing.T) {
	var o ListOpts
	if o.Project != "" || o.Kind != "" || o.Tag != "" || o.Limit != 0 || o.Offset != 0 {
		t.Fatalf("ListOpts zero value has unexpected non-zero fields: %+v", o)
	}
}

func TestSearchOpts_ZeroValue(t *testing.T) {
	var o SearchOpts
	if o.Project != "" || o.Kinds != nil || o.Tags != nil || o.Limit != 0 {
		t.Fatalf("SearchOpts zero value has unexpected non-zero fields: %+v", o)
	}
}
