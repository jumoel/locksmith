package ecosystem

import "testing"

func TestOverrideSet_FindOverride_GlobalMatch(t *testing.T) {
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "is-number", Version: "6.0.0"},
		},
	}
	v, ok := os.FindOverride("is-number", nil)
	if !ok {
		t.Fatal("expected match")
	}
	if v != "6.0.0" {
		t.Errorf("got %q, want %q", v, "6.0.0")
	}
}

func TestOverrideSet_FindOverride_NoMatch(t *testing.T) {
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "is-number", Version: "6.0.0"},
		},
	}
	_, ok := os.FindOverride("is-odd", nil)
	if ok {
		t.Fatal("expected no match for wrong package name")
	}
}

func TestOverrideSet_FindOverride_ParentMatch(t *testing.T) {
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "is-number", Version: "6.0.0", Parent: "is-odd"},
		},
	}
	v, ok := os.FindOverride("is-number", []string{"is-odd"})
	if !ok {
		t.Fatal("expected match when parent is in chain")
	}
	if v != "6.0.0" {
		t.Errorf("got %q, want %q", v, "6.0.0")
	}
}

func TestOverrideSet_FindOverride_ParentNoMatch(t *testing.T) {
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "is-number", Version: "6.0.0", Parent: "is-odd"},
		},
	}
	_, ok := os.FindOverride("is-number", []string{"other-pkg"})
	if ok {
		t.Fatal("expected no match when parent not in chain")
	}
}

func TestOverrideSet_FindOverride_NestedChildren(t *testing.T) {
	// npm-style: override is-number only when it's a dep of is-odd
	os := &OverrideSet{
		Rules: []OverrideRule{
			{
				Package: "is-odd",
				Children: []OverrideRule{
					{Package: "is-number", Version: "6.0.0"},
				},
			},
		},
	}

	// Should match when is-odd is in the parent chain.
	v, ok := os.FindOverride("is-number", []string{"is-odd"})
	if !ok {
		t.Fatal("expected match with nested children and is-odd in parents")
	}
	if v != "6.0.0" {
		t.Errorf("got %q, want %q", v, "6.0.0")
	}

	// Should NOT match when is-odd is not in parents.
	_, ok = os.FindOverride("is-number", []string{"other-pkg"})
	if ok {
		t.Fatal("expected no match when is-odd not in parent chain")
	}
}

func TestOverrideSet_FindOverride_NilSet(t *testing.T) {
	var os *OverrideSet
	_, ok := os.FindOverride("anything", nil)
	if ok {
		t.Fatal("expected no match on nil OverrideSet")
	}
}

func TestOverrideSet_FindOverride_EmptyRules(t *testing.T) {
	os := &OverrideSet{Rules: []OverrideRule{}}
	_, ok := os.FindOverride("anything", nil)
	if ok {
		t.Fatal("expected no match on empty rules")
	}
}

func TestOverrideSet_FindOverride_MultipleRules(t *testing.T) {
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "foo", Version: "1.0.0"},
			{Package: "bar", Version: "2.0.0"},
		},
	}
	v, ok := os.FindOverride("bar", nil)
	if !ok {
		t.Fatal("expected match for bar")
	}
	if v != "2.0.0" {
		t.Errorf("got %q, want %q", v, "2.0.0")
	}
}

func TestOverrideSet_FindOverride_FirstMatchWins(t *testing.T) {
	// When multiple rules match, first one wins.
	os := &OverrideSet{
		Rules: []OverrideRule{
			{Package: "foo", Version: "1.0.0"},
			{Package: "foo", Version: "2.0.0"},
		},
	}
	v, ok := os.FindOverride("foo", nil)
	if !ok {
		t.Fatal("expected match")
	}
	if v != "1.0.0" {
		t.Errorf("got %q, want %q (first match should win)", v, "1.0.0")
	}
}

func TestOverrideSet_FindOverride_DeeplyNestedChildren(t *testing.T) {
	// npm supports deeply nested: {"A": {"B": {"C": "1.0.0"}}}
	// meaning C is overridden only when both A and B are in the parent chain.
	os := &OverrideSet{
		Rules: []OverrideRule{
			{
				Package: "A",
				Children: []OverrideRule{
					{
						Package: "B",
						Children: []OverrideRule{
							{Package: "C", Version: "1.0.0"},
						},
					},
				},
			},
		},
	}

	// Both A and B in parents - should match.
	v, ok := os.FindOverride("C", []string{"A", "B"})
	if !ok {
		t.Fatal("expected match with A and B both in parent chain")
	}
	if v != "1.0.0" {
		t.Errorf("got %q, want %q", v, "1.0.0")
	}

	// Only A in parents - should not match.
	_, ok = os.FindOverride("C", []string{"A"})
	if ok {
		t.Fatal("expected no match with only A in parent chain")
	}

	// Only B in parents - should not match (A must also be present).
	_, ok = os.FindOverride("C", []string{"B"})
	if ok {
		t.Fatal("expected no match with only B in parent chain")
	}
}
