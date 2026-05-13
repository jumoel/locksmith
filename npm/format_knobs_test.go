package npm

import (
	"strings"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
)

// makeMinimalResult builds the smallest ResolveResult that exercises one
// non-root package with a registry tarball URL. Used by the format-knob
// tests below.
func makeMinimalResult() (*ResolveResult, *ecosystem.ProjectSpec) {
	rootNode := &ecosystem.Node{Name: "root", Version: "1.0.0"}
	depNode := &ecosystem.Node{
		Name:       "lodash",
		Version:    "4.17.21",
		Integrity:  "sha512-fakeIntegrity==",
		TarballURL: "https://registry.npmjs.org/lodash/-/lodash-4.17.21.tgz",
	}
	graph := &ecosystem.Graph{
		Root:  rootNode,
		Nodes: map[string]*ecosystem.Node{"lodash@4.17.21": depNode},
	}
	rootPlaced := &PlacedNode{
		Path: "", Node: rootNode,
		Children: map[string]*PlacedNode{},
	}
	depPlaced := &PlacedNode{
		Path: "node_modules/lodash", Node: depNode,
		Children: map[string]*PlacedNode{},
	}
	rootPlaced.Children["lodash"] = depPlaced
	result := &ResolveResult{
		Graph: graph,
		Root:  rootPlaced,
		PlacedNodes: map[string]*PlacedNode{
			"":                    rootPlaced,
			"node_modules/lodash": depPlaced,
		},
	}
	project := &ecosystem.ProjectSpec{
		Name:    "root",
		Version: "1.0.0",
	}
	return result, project
}

// TestFormat_OmitLockfileRegistryResolved verifies that when the formatter
// is configured with OmitLockfileRegistryResolved, registry tarball URLs are
// stripped from the `resolved` field on non-root entries. Per ticket #25,
// this matches npm's `omit-lockfile-registry-resolved=true` semantics.
func TestFormat_OmitLockfileRegistryResolved(t *testing.T) {
	result, project := makeMinimalResult()

	formatter := NewPackageLockV3Formatter()
	formatter.OmitLockfileRegistryResolved = true

	out, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult: %v", err)
	}
	str := string(out)

	// The registry URL must not appear anywhere in the output.
	if strings.Contains(str, "https://registry.npmjs.org/lodash") {
		t.Errorf("expected registry URL to be stripped; got:\n%s", str)
	}
	// Sanity: the rest of the entry is still there (npm key includes the
	// node_modules/ prefix).
	if !strings.Contains(str, "\"node_modules/lodash\"") || !strings.Contains(str, "sha512-fakeIntegrity==") {
		t.Errorf("expected entry to still be present (just without resolved); got:\n%s", str)
	}
}

// TestFormat_OmitLockfileRegistryResolved_Default verifies the default
// behavior is unchanged (resolved URL stays).
func TestFormat_OmitLockfileRegistryResolved_Default(t *testing.T) {
	result, project := makeMinimalResult()

	formatter := NewPackageLockV3Formatter()
	out, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult: %v", err)
	}
	if !strings.Contains(string(out), "https://registry.npmjs.org/lodash") {
		t.Errorf("expected registry URL by default; got:\n%s", out)
	}
}

// TestFormat_MinifyPackageLock verifies that MinifyPackageLock=true produces
// compact (single-line) JSON instead of pretty-printed. Per ticket #25 this
// matches npm's `format-package-lock=false`.
func TestFormat_MinifyPackageLock(t *testing.T) {
	result, project := makeMinimalResult()

	formatter := NewPackageLockV3Formatter()
	formatter.MinifyPackageLock = true

	out, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult: %v", err)
	}
	str := string(out)
	// Compact output has no indented multi-line structure: count newlines.
	// `json.NewEncoder(...).Encode` adds exactly one trailing newline; a
	// pretty-printed object with 5+ keys would have many newlines.
	newlines := strings.Count(str, "\n")
	if newlines > 1 {
		t.Errorf("MinifyPackageLock=true should produce single-line JSON (got %d newlines):\n%s", newlines, str)
	}
}

// TestFormat_MinifyPackageLock_Default verifies pretty-print stays the default.
func TestFormat_MinifyPackageLock_Default(t *testing.T) {
	result, project := makeMinimalResult()

	formatter := NewPackageLockV3Formatter()
	out, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult: %v", err)
	}
	if strings.Count(string(out), "\n") < 5 {
		t.Errorf("default should be pretty-printed (multiple newlines); got:\n%s", out)
	}
}
