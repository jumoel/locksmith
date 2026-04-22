package npm

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseWorkspaceGlobs_ArrayForm(t *testing.T) {
	data := []byte(`{
		"name": "my-monorepo",
		"workspaces": ["packages/*"]
	}`)

	globs, err := ParseWorkspaceGlobs(data)
	if err != nil {
		t.Fatalf("ParseWorkspaceGlobs() error: %v", err)
	}
	if len(globs) != 1 {
		t.Fatalf("got %d globs, want 1", len(globs))
	}
	if globs[0] != "packages/*" {
		t.Errorf("globs[0] = %q, want %q", globs[0], "packages/*")
	}
}

func TestParseWorkspaceGlobs_ArrayFormMultiple(t *testing.T) {
	data := []byte(`{
		"name": "my-monorepo",
		"workspaces": ["packages/*", "apps/*", "tools/linter"]
	}`)

	globs, err := ParseWorkspaceGlobs(data)
	if err != nil {
		t.Fatalf("ParseWorkspaceGlobs() error: %v", err)
	}
	expected := []string{"packages/*", "apps/*", "tools/linter"}
	if len(globs) != len(expected) {
		t.Fatalf("got %d globs, want %d", len(globs), len(expected))
	}
	for i, want := range expected {
		if globs[i] != want {
			t.Errorf("globs[%d] = %q, want %q", i, globs[i], want)
		}
	}
}

func TestParseWorkspaceGlobs_ObjectForm(t *testing.T) {
	data := []byte(`{
		"name": "my-monorepo",
		"workspaces": {
			"packages": ["packages/*", "apps/*"]
		}
	}`)

	globs, err := ParseWorkspaceGlobs(data)
	if err != nil {
		t.Fatalf("ParseWorkspaceGlobs() error: %v", err)
	}
	expected := []string{"packages/*", "apps/*"}
	if len(globs) != len(expected) {
		t.Fatalf("got %d globs, want %d", len(globs), len(expected))
	}
	for i, want := range expected {
		if globs[i] != want {
			t.Errorf("globs[%d] = %q, want %q", i, globs[i], want)
		}
	}
}

func TestParseWorkspaceGlobs_NoWorkspaces(t *testing.T) {
	data := []byte(`{
		"name": "regular-package",
		"version": "1.0.0",
		"dependencies": {"foo": "^1.0.0"}
	}`)

	globs, err := ParseWorkspaceGlobs(data)
	if err != nil {
		t.Fatalf("ParseWorkspaceGlobs() error: %v", err)
	}
	if globs != nil {
		t.Errorf("got %v, want nil for package without workspaces", globs)
	}
}

func TestParseWorkspaceGlobs_InvalidJSON(t *testing.T) {
	data := []byte(`{not valid json}`)

	_, err := ParseWorkspaceGlobs(data)
	if err == nil {
		t.Fatal("ParseWorkspaceGlobs() expected error for invalid JSON, got nil")
	}
}

func TestParseWithWorkspaces_Simple(t *testing.T) {
	rootData := []byte(`{
		"name": "my-monorepo",
		"version": "1.0.0",
		"workspaces": ["packages/*"],
		"dependencies": {"is-odd": "^3.0.0"}
	}`)

	memberData := map[string][]byte{
		"packages/lib-a": []byte(`{
			"name": "@workspace/lib-a",
			"version": "1.0.0",
			"dependencies": {"ms": "^2.1.0"}
		}`),
		"packages/lib-b": []byte(`{
			"name": "@workspace/lib-b",
			"version": "1.0.0",
			"dependencies": {"is-number": "^7.0.0"}
		}`),
	}

	parser := NewSpecParser()
	spec, err := parser.ParseWithWorkspaces(rootData, memberData)
	if err != nil {
		t.Fatalf("ParseWithWorkspaces() error: %v", err)
	}

	// Root spec fields.
	if spec.Name != "my-monorepo" {
		t.Errorf("Name = %q, want %q", spec.Name, "my-monorepo")
	}
	if spec.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", spec.Version, "1.0.0")
	}

	// Root dependencies should include is-odd.
	found := false
	for _, dep := range spec.Dependencies {
		if dep.Name == "is-odd" {
			found = true
			if dep.Constraint != "^3.0.0" {
				t.Errorf("is-odd constraint = %q, want %q", dep.Constraint, "^3.0.0")
			}
		}
	}
	if !found {
		t.Error("root dependencies missing is-odd")
	}

	// Workspaces.
	if len(spec.Workspaces) != 2 {
		t.Fatalf("Workspaces count = %d, want 2", len(spec.Workspaces))
	}

	// Members should be sorted by path.
	if spec.Workspaces[0].RelPath != "packages/lib-a" {
		t.Errorf("Workspaces[0].RelPath = %q, want %q", spec.Workspaces[0].RelPath, "packages/lib-a")
	}
	if spec.Workspaces[1].RelPath != "packages/lib-b" {
		t.Errorf("Workspaces[1].RelPath = %q, want %q", spec.Workspaces[1].RelPath, "packages/lib-b")
	}

	// Member spec names.
	if spec.Workspaces[0].Spec.Name != "@workspace/lib-a" {
		t.Errorf("Workspaces[0].Spec.Name = %q, want %q", spec.Workspaces[0].Spec.Name, "@workspace/lib-a")
	}
	if spec.Workspaces[1].Spec.Name != "@workspace/lib-b" {
		t.Errorf("Workspaces[1].Spec.Name = %q, want %q", spec.Workspaces[1].Spec.Name, "@workspace/lib-b")
	}

	// Member dependencies are parsed.
	libADeps := spec.Workspaces[0].Spec.Dependencies
	foundMS := false
	for _, dep := range libADeps {
		if dep.Name == "ms" {
			foundMS = true
		}
	}
	if !foundMS {
		t.Error("lib-a dependencies missing ms")
	}

	libBDeps := spec.Workspaces[1].Spec.Dependencies
	foundIsNumber := false
	for _, dep := range libBDeps {
		if dep.Name == "is-number" {
			foundIsNumber = true
		}
	}
	if !foundIsNumber {
		t.Error("lib-b dependencies missing is-number")
	}
}

func TestParseWithWorkspaces_NoMembers(t *testing.T) {
	rootData := []byte(`{
		"name": "solo-package",
		"version": "1.0.0",
		"dependencies": {"ms": "^2.1.0"}
	}`)

	parser := NewSpecParser()
	spec, err := parser.ParseWithWorkspaces(rootData, nil)
	if err != nil {
		t.Fatalf("ParseWithWorkspaces() error: %v", err)
	}

	if spec.Name != "solo-package" {
		t.Errorf("Name = %q, want %q", spec.Name, "solo-package")
	}
	if spec.Workspaces != nil {
		t.Errorf("Workspaces = %v, want nil for no members", spec.Workspaces)
	}
}

func TestParseWithWorkspaces_EmptyMemberMap(t *testing.T) {
	rootData := []byte(`{
		"name": "solo-package",
		"version": "1.0.0"
	}`)

	parser := NewSpecParser()
	spec, err := parser.ParseWithWorkspaces(rootData, map[string][]byte{})
	if err != nil {
		t.Fatalf("ParseWithWorkspaces() error: %v", err)
	}

	if spec.Workspaces != nil {
		t.Errorf("Workspaces = %v, want nil for empty member map", spec.Workspaces)
	}
}

func TestParseWithWorkspaces_MemberParseFails(t *testing.T) {
	rootData := []byte(`{
		"name": "my-monorepo",
		"version": "1.0.0"
	}`)

	memberData := map[string][]byte{
		"packages/broken": []byte(`{not valid json`),
	}

	parser := NewSpecParser()
	_, err := parser.ParseWithWorkspaces(rootData, memberData)
	if err == nil {
		t.Fatal("ParseWithWorkspaces() expected error for invalid member JSON, got nil")
	}
}

func TestParseWithWorkspaces_RootParseFails(t *testing.T) {
	parser := NewSpecParser()
	_, err := parser.ParseWithWorkspaces([]byte(`{broken`), nil)
	if err == nil {
		t.Fatal("ParseWithWorkspaces() expected error for invalid root JSON, got nil")
	}
}

// workspaceFixturePath returns the absolute path to the workspace fixture directory.
func workspaceFixturePath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "testharness", "fixtures", "workspace-simple")
}

func TestParseWithWorkspaces_RealFixture(t *testing.T) {
	fixtureDir := workspaceFixturePath()

	rootData, err := os.ReadFile(filepath.Join(fixtureDir, "package.json"))
	if err != nil {
		t.Fatalf("reading root package.json: %v", err)
	}

	// Read member package.json files.
	memberData := map[string][]byte{}
	memberPaths := []struct {
		relPath string
		fsPath  string
	}{
		{"packages/lib-a", filepath.Join(fixtureDir, "packages", "lib-a", "package.json")},
		{"packages/lib-b", filepath.Join(fixtureDir, "packages", "lib-b", "package.json")},
	}
	for _, m := range memberPaths {
		data, err := os.ReadFile(m.fsPath)
		if err != nil {
			t.Fatalf("reading %s: %v", m.fsPath, err)
		}
		memberData[m.relPath] = data
	}

	parser := NewSpecParser()
	spec, err := parser.ParseWithWorkspaces(rootData, memberData)
	if err != nil {
		t.Fatalf("ParseWithWorkspaces() error: %v", err)
	}

	// Root name from the fixture.
	if spec.Name != "workspace-simple" {
		t.Errorf("Name = %q, want %q", spec.Name, "workspace-simple")
	}

	// Root has is-odd dependency.
	foundIsOdd := false
	for _, dep := range spec.Dependencies {
		if dep.Name == "is-odd" {
			foundIsOdd = true
		}
	}
	if !foundIsOdd {
		t.Error("root missing is-odd dependency")
	}

	// Two workspace members.
	if len(spec.Workspaces) != 2 {
		t.Fatalf("Workspaces count = %d, want 2", len(spec.Workspaces))
	}

	// Sorted by path: lib-a before lib-b.
	if spec.Workspaces[0].RelPath != "packages/lib-a" {
		t.Errorf("Workspaces[0].RelPath = %q, want packages/lib-a", spec.Workspaces[0].RelPath)
	}
	if spec.Workspaces[0].Spec.Name != "@workspace/lib-a" {
		t.Errorf("Workspaces[0].Spec.Name = %q, want @workspace/lib-a", spec.Workspaces[0].Spec.Name)
	}

	if spec.Workspaces[1].RelPath != "packages/lib-b" {
		t.Errorf("Workspaces[1].RelPath = %q, want packages/lib-b", spec.Workspaces[1].RelPath)
	}
	if spec.Workspaces[1].Spec.Name != "@workspace/lib-b" {
		t.Errorf("Workspaces[1].Spec.Name = %q, want @workspace/lib-b", spec.Workspaces[1].Spec.Name)
	}

	// lib-a has ms dependency.
	libAHasMS := false
	for _, dep := range spec.Workspaces[0].Spec.Dependencies {
		if dep.Name == "ms" {
			libAHasMS = true
		}
	}
	if !libAHasMS {
		t.Error("lib-a missing ms dependency")
	}

	// lib-b has workspace:* dep on lib-a and is-number.
	libBHasWorkspaceDep := false
	libBHasIsNumber := false
	for _, dep := range spec.Workspaces[1].Spec.Dependencies {
		if dep.Name == "@workspace/lib-a" && dep.Constraint == "workspace:*" {
			libBHasWorkspaceDep = true
		}
		if dep.Name == "is-number" {
			libBHasIsNumber = true
		}
	}
	if !libBHasWorkspaceDep {
		t.Error("lib-b missing @workspace/lib-a workspace:* dependency")
	}
	if !libBHasIsNumber {
		t.Error("lib-b missing is-number dependency")
	}
}

func TestParseWithWorkspaces_MembersSortedByPath(t *testing.T) {
	rootData := []byte(`{"name": "root"}`)

	// Provide member data in reverse alphabetical order to verify sorting.
	memberData := map[string][]byte{
		"packages/zulu":  []byte(`{"name": "zulu"}`),
		"apps/alpha":     []byte(`{"name": "alpha"}`),
		"packages/mike":  []byte(`{"name": "mike"}`),
		"apps/bravo":     []byte(`{"name": "bravo"}`),
	}

	parser := NewSpecParser()
	spec, err := parser.ParseWithWorkspaces(rootData, memberData)
	if err != nil {
		t.Fatalf("ParseWithWorkspaces() error: %v", err)
	}

	if len(spec.Workspaces) != 4 {
		t.Fatalf("Workspaces count = %d, want 4", len(spec.Workspaces))
	}

	expectedPaths := []string{"apps/alpha", "apps/bravo", "packages/mike", "packages/zulu"}
	for i, want := range expectedPaths {
		if spec.Workspaces[i].RelPath != want {
			t.Errorf("Workspaces[%d].RelPath = %q, want %q", i, spec.Workspaces[i].RelPath, want)
		}
	}
}
