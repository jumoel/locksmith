package npm

import (
	"encoding/json"
	"testing"
)

func TestParsePackageExtensions_Flat(t *testing.T) {
	raw := json.RawMessage(`{"some-pkg": {"dependencies": {"foo": "^1.0.0"}}}`)
	pes, err := ParsePackageExtensions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes == nil {
		t.Fatal("expected non-nil PackageExtensionSet")
	}
	if len(pes.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(pes.Extensions))
	}
	ext := pes.Extensions[0]
	if ext.Name != "some-pkg" {
		t.Errorf("Name = %q, want %q", ext.Name, "some-pkg")
	}
	if ext.VersionRange != "" {
		t.Errorf("VersionRange = %q, want empty", ext.VersionRange)
	}
	if len(ext.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(ext.Dependencies))
	}
	if ext.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("Dependencies[foo] = %q, want %q", ext.Dependencies["foo"], "^1.0.0")
	}
	if len(ext.PeerDeps) != 0 {
		t.Errorf("expected 0 peer deps, got %d", len(ext.PeerDeps))
	}
}

func TestParsePackageExtensions_Versioned(t *testing.T) {
	raw := json.RawMessage(`{"some-pkg@1": {"dependencies": {"foo": "^1.0.0"}}}`)
	pes, err := ParsePackageExtensions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes == nil {
		t.Fatal("expected non-nil PackageExtensionSet")
	}
	if len(pes.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(pes.Extensions))
	}
	ext := pes.Extensions[0]
	if ext.Name != "some-pkg" {
		t.Errorf("Name = %q, want %q", ext.Name, "some-pkg")
	}
	if ext.VersionRange != "1" {
		t.Errorf("VersionRange = %q, want %q", ext.VersionRange, "1")
	}
	if ext.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("Dependencies[foo] = %q, want %q", ext.Dependencies["foo"], "^1.0.0")
	}
}

func TestParsePackageExtensions_PeerDeps(t *testing.T) {
	raw := json.RawMessage(`{"pkg": {"peerDependencies": {"react": "*"}}}`)
	pes, err := ParsePackageExtensions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes == nil {
		t.Fatal("expected non-nil PackageExtensionSet")
	}
	if len(pes.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(pes.Extensions))
	}
	ext := pes.Extensions[0]
	if ext.Name != "pkg" {
		t.Errorf("Name = %q, want %q", ext.Name, "pkg")
	}
	if len(ext.Dependencies) != 0 {
		t.Errorf("expected 0 deps, got %d", len(ext.Dependencies))
	}
	if len(ext.PeerDeps) != 1 {
		t.Fatalf("expected 1 peer dep, got %d", len(ext.PeerDeps))
	}
	if ext.PeerDeps["react"] != "*" {
		t.Errorf("PeerDeps[react] = %q, want %q", ext.PeerDeps["react"], "*")
	}
}

func TestParsePackageExtensions_Multiple(t *testing.T) {
	raw := json.RawMessage(`{
		"pkg-a": {"dependencies": {"foo": "^1.0.0"}},
		"pkg-b@^2": {"peerDependencies": {"bar": ">=3.0.0"}, "dependencies": {"baz": "~1.0.0"}}
	}`)
	pes, err := ParsePackageExtensions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes == nil {
		t.Fatal("expected non-nil PackageExtensionSet")
	}
	if len(pes.Extensions) != 2 {
		t.Fatalf("expected 2 extensions, got %d", len(pes.Extensions))
	}

	// Build a map for easier lookup since JSON map iteration order is not guaranteed.
	byName := make(map[string]int)
	for i, ext := range pes.Extensions {
		byName[ext.Name] = i
	}

	aIdx, ok := byName["pkg-a"]
	if !ok {
		t.Fatal("missing extension for pkg-a")
	}
	a := pes.Extensions[aIdx]
	if a.VersionRange != "" {
		t.Errorf("pkg-a VersionRange = %q, want empty", a.VersionRange)
	}
	if a.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("pkg-a Dependencies[foo] = %q, want %q", a.Dependencies["foo"], "^1.0.0")
	}

	bIdx, ok := byName["pkg-b"]
	if !ok {
		t.Fatal("missing extension for pkg-b")
	}
	b := pes.Extensions[bIdx]
	if b.VersionRange != "^2" {
		t.Errorf("pkg-b VersionRange = %q, want %q", b.VersionRange, "^2")
	}
	if b.Dependencies["baz"] != "~1.0.0" {
		t.Errorf("pkg-b Dependencies[baz] = %q, want %q", b.Dependencies["baz"], "~1.0.0")
	}
	if b.PeerDeps["bar"] != ">=3.0.0" {
		t.Errorf("pkg-b PeerDeps[bar] = %q, want %q", b.PeerDeps["bar"], ">=3.0.0")
	}
}

func TestParsePackageExtensions_Empty(t *testing.T) {
	// nil raw
	pes, err := ParsePackageExtensions(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes != nil {
		t.Error("expected nil PackageExtensionSet for nil input")
	}

	// empty object
	pes, err = ParsePackageExtensions(json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes != nil {
		t.Error("expected nil PackageExtensionSet for empty object")
	}

	// "null" literal
	pes, err = ParsePackageExtensions(json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes != nil {
		t.Error("expected nil PackageExtensionSet for null")
	}
}

func TestParsePackageExtensions_ScopedPackage(t *testing.T) {
	raw := json.RawMessage(`{"@scope/pkg@^2": {"dependencies": {"foo": "^1.0.0"}, "peerDependencies": {"bar": "*"}}}`)
	pes, err := ParsePackageExtensions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pes == nil {
		t.Fatal("expected non-nil PackageExtensionSet")
	}
	if len(pes.Extensions) != 1 {
		t.Fatalf("expected 1 extension, got %d", len(pes.Extensions))
	}
	ext := pes.Extensions[0]
	if ext.Name != "@scope/pkg" {
		t.Errorf("Name = %q, want %q", ext.Name, "@scope/pkg")
	}
	if ext.VersionRange != "^2" {
		t.Errorf("VersionRange = %q, want %q", ext.VersionRange, "^2")
	}
	if ext.Dependencies["foo"] != "^1.0.0" {
		t.Errorf("Dependencies[foo] = %q, want %q", ext.Dependencies["foo"], "^1.0.0")
	}
	if ext.PeerDeps["bar"] != "*" {
		t.Errorf("PeerDeps[bar] = %q, want %q", ext.PeerDeps["bar"], "*")
	}
}
