package ecosystem

import (
	"sort"
	"testing"
)

func TestWorkspaceIndex_Resolve(t *testing.T) {
	members := []*WorkspaceMember{
		{
			RelPath: "packages/foo",
			Spec:    &ProjectSpec{Name: "@scope/foo", Version: "1.0.0"},
		},
		{
			RelPath: "packages/bar",
			Spec:    &ProjectSpec{Name: "@scope/bar", Version: "2.0.0"},
		},
	}

	idx := NewWorkspaceIndex(members)

	got := idx.Resolve("@scope/foo")
	if got == nil {
		t.Fatal("Resolve(@scope/foo) returned nil, want non-nil")
	}
	if got.RelPath != "packages/foo" {
		t.Errorf("RelPath = %q, want %q", got.RelPath, "packages/foo")
	}
	if got.Spec.Name != "@scope/foo" {
		t.Errorf("Spec.Name = %q, want %q", got.Spec.Name, "@scope/foo")
	}
	if got.Spec.Version != "1.0.0" {
		t.Errorf("Spec.Version = %q, want %q", got.Spec.Version, "1.0.0")
	}

	got = idx.Resolve("@scope/bar")
	if got == nil {
		t.Fatal("Resolve(@scope/bar) returned nil, want non-nil")
	}
	if got.RelPath != "packages/bar" {
		t.Errorf("RelPath = %q, want %q", got.RelPath, "packages/bar")
	}
}

func TestWorkspaceIndex_Resolve_NotFound(t *testing.T) {
	members := []*WorkspaceMember{
		{
			RelPath: "packages/foo",
			Spec:    &ProjectSpec{Name: "@scope/foo", Version: "1.0.0"},
		},
	}

	idx := NewWorkspaceIndex(members)

	got := idx.Resolve("nonexistent-package")
	if got != nil {
		t.Errorf("Resolve(nonexistent-package) = %v, want nil", got)
	}
}

func TestWorkspaceIndex_Resolve_Nil(t *testing.T) {
	var idx *WorkspaceIndex
	got := idx.Resolve("anything")
	if got != nil {
		t.Errorf("nil WorkspaceIndex.Resolve() = %v, want nil", got)
	}
}

func TestWorkspaceIndex_Names(t *testing.T) {
	members := []*WorkspaceMember{
		{
			RelPath: "packages/alpha",
			Spec:    &ProjectSpec{Name: "alpha", Version: "1.0.0"},
		},
		{
			RelPath: "packages/beta",
			Spec:    &ProjectSpec{Name: "beta", Version: "2.0.0"},
		},
		{
			RelPath: "apps/gamma",
			Spec:    &ProjectSpec{Name: "@org/gamma", Version: "0.1.0"},
		},
	}

	idx := NewWorkspaceIndex(members)
	names := idx.Names()

	if len(names) != 3 {
		t.Fatalf("Names() returned %d entries, want 3", len(names))
	}

	// Names() order is non-deterministic (map iteration), so sort for comparison.
	sort.Strings(names)
	expected := []string{"@org/gamma", "alpha", "beta"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("Names()[%d] = %q, want %q", i, names[i], want)
		}
	}
}

func TestWorkspaceIndex_EmptyMembers(t *testing.T) {
	idx := NewWorkspaceIndex([]*WorkspaceMember{})

	names := idx.Names()
	if len(names) != 0 {
		t.Errorf("Names() returned %d entries for empty index, want 0", len(names))
	}

	got := idx.Resolve("anything")
	if got != nil {
		t.Errorf("Resolve() on empty index = %v, want nil", got)
	}
}

func TestWorkspaceIndex_SkipsMembersWithNilSpec(t *testing.T) {
	members := []*WorkspaceMember{
		{RelPath: "packages/valid", Spec: &ProjectSpec{Name: "valid", Version: "1.0.0"}},
		{RelPath: "packages/no-spec", Spec: nil},
		{RelPath: "packages/empty-name", Spec: &ProjectSpec{Name: "", Version: "1.0.0"}},
	}

	idx := NewWorkspaceIndex(members)

	names := idx.Names()
	if len(names) != 1 {
		t.Fatalf("Names() = %v, want exactly [valid]", names)
	}
	if names[0] != "valid" {
		t.Errorf("Names()[0] = %q, want %q", names[0], "valid")
	}
}
