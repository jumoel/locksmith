package bun

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/npm"
)

// resolveForFormatTest is a helper that runs the bun resolver with a mock
// registry and returns the result for formatting tests.
func resolveForFormatTest(t *testing.T, reg *mockRegistry, project *ecosystem.ProjectSpec) *ResolveResult {
	t.Helper()
	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	return result
}

// stripTrailingCommas removes trailing commas from JSONC to produce valid JSON.
// Handles commas followed by arbitrary whitespace before } or ].
var trailingCommaRe = regexp.MustCompile(`,(\s*[}\]])`)

func stripTrailingCommas(data []byte) []byte {
	return trailingCommaRe.ReplaceAll(data, []byte("$1"))
}

func TestBunLockFormatter_SimpleProject(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	result := resolveForFormatTest(t, reg, project)
	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	// Strip trailing commas to get valid JSON for parsing.
	cleaned := stripTrailingCommas(output)

	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("output is not valid JSON after stripping commas: %v\n%s", err, string(output))
	}

	// Verify top-level keys.
	for _, key := range []string{"lockfileVersion", "configVersion", "workspaces", "packages"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}

	if v, ok := parsed["lockfileVersion"].(float64); !ok || v != 1 {
		t.Errorf("lockfileVersion = %v, want 1", parsed["lockfileVersion"])
	}
	if v, ok := parsed["configVersion"].(float64); !ok || v != 1 {
		t.Errorf("configVersion = %v, want 1", parsed["configVersion"])
	}
}

func TestBunLockFormatter_DevAndOptionalDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("lodash", "4.17.0", baseTime, nil)
	reg.addVersion("jest", "29.0.0", baseTime, nil)
	reg.addVersion("fsevents", "2.3.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "lodash", Constraint: "^4.17.0", Type: ecosystem.DepRegular},
			{Name: "jest", Constraint: "^29.0.0", Type: ecosystem.DepDev},
			{Name: "fsevents", Constraint: "^2.3.0", Type: ecosystem.DepOptional},
		},
	}

	result := resolveForFormatTest(t, reg, project)
	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	content := string(output)

	// The workspace entry should have all three sections.
	if !strings.Contains(content, `"dependencies"`) {
		t.Error("output missing 'dependencies' section")
	}
	if !strings.Contains(content, `"devDependencies"`) {
		t.Error("output missing 'devDependencies' section")
	}
	if !strings.Contains(content, `"optionalDependencies"`) {
		t.Error("output missing 'optionalDependencies' section")
	}

	// Verify the workspace section references the right constraints.
	if !strings.Contains(content, `"lodash": "^4.17.0"`) {
		t.Error("dependencies should contain lodash@^4.17.0")
	}
	if !strings.Contains(content, `"jest": "^29.0.0"`) {
		t.Error("devDependencies should contain jest@^29.0.0")
	}
	if !strings.Contains(content, `"fsevents": "^2.3.0"`) {
		t.Error("optionalDependencies should contain fsevents@^2.3.0")
	}
}

func TestBunLockFormatter_PackageEntryStructure(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionMeta("A", "1.0.0", baseTime, &ecosystem.VersionMetadata{
		Dependencies: map[string]string{"B": "^1.0.0"},
	})
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	result := resolveForFormatTest(t, reg, project)
	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	// Parse the JSONC output.
	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	packages, ok := parsed["packages"].(map[string]interface{})
	if !ok {
		t.Fatal("packages is not an object")
	}

	// Each package entry should be a 4-element array: [resolvedSpec, "", metadata, integrity].
	for key, val := range packages {
		arr, ok := val.([]interface{})
		if !ok {
			t.Errorf("packages[%s] is not an array", key)
			continue
		}
		if len(arr) != 4 {
			t.Errorf("packages[%s] has %d elements, want 4", key, len(arr))
			continue
		}

		// Element 0: resolved spec string (e.g., "A@1.0.0").
		if _, ok := arr[0].(string); !ok {
			t.Errorf("packages[%s][0] is not a string", key)
		}

		// Element 1: empty string.
		if s, ok := arr[1].(string); !ok || s != "" {
			t.Errorf("packages[%s][1] = %v, want empty string", key, arr[1])
		}

		// Element 2: metadata object.
		if _, ok := arr[2].(map[string]interface{}); !ok {
			t.Errorf("packages[%s][2] is not an object", key)
		}

		// Element 3: integrity string.
		if _, ok := arr[3].(string); !ok {
			t.Errorf("packages[%s][3] is not a string", key)
		}
	}
}

func TestBunLockFormatter_TrailingCommas(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple object",
			input: `{"a": 1}`,
			want:  `{"a": 1,}`,
		},
		{
			name:  "simple array",
			input: `[1, 2]`,
			want:  `[1, 2,]`,
		},
		{
			name:  "empty object stays empty",
			input: `{}`,
			want:  `{}`,
		},
		{
			name:  "empty array stays empty",
			input: `[]`,
			want:  `[]`,
		},
		{
			name:  "nested",
			input: "{\"a\": {\n  \"b\": 1\n}}",
			want:  "{\"a\": {\n  \"b\": 1,\n},}",
		},
		{
			name:  "already has trailing comma is not doubled",
			input: `{"a": 1,}`,
			want:  `{"a": 1,}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(addTrailingCommas([]byte(tc.input)))
			if got != tc.want {
				t.Errorf("addTrailingCommas(%q) =\n%s\nwant:\n%s", tc.input, got, tc.want)
			}
		})
	}
}

func TestBunLockFormatter_PeerDepsInMetadata(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionMeta("A", "1.0.0", baseTime, &ecosystem.VersionMetadata{
		PeerDeps: map[string]string{
			"react":  "^18.0.0",
			"preact": "^10.0.0",
		},
		PeerDepsMeta: map[string]ecosystem.PeerDepMeta{
			"preact": {Optional: true},
		},
	})
	reg.addVersion("react", "18.2.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepRegular},
		},
	}

	result := resolveForFormatTest(t, reg, project)
	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	content := string(output)

	// Metadata should contain peerDependencies section.
	if !strings.Contains(content, `"peerDependencies"`) {
		t.Error("output missing peerDependencies in metadata")
	}

	// Metadata should contain optionalPeers for the optional peer.
	if !strings.Contains(content, `"optionalPeers"`) {
		t.Error("output missing optionalPeers in metadata")
	}

	// Parse and verify structure.
	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}

	packages := parsed["packages"].(map[string]interface{})
	aEntry := packages["A"].([]interface{})
	meta := aEntry[2].(map[string]interface{})

	peerDeps, ok := meta["peerDependencies"].(map[string]interface{})
	if !ok {
		t.Fatal("peerDependencies is not an object")
	}
	if peerDeps["react"] != "^18.0.0" {
		t.Errorf("peerDependencies.react = %v, want ^18.0.0", peerDeps["react"])
	}

	optionalPeers, ok := meta["optionalPeers"].([]interface{})
	if !ok {
		t.Fatal("optionalPeers is not an array")
	}
	found := false
	for _, p := range optionalPeers {
		if p == "preact" {
			found = true
		}
	}
	if !found {
		t.Error("optionalPeers should contain 'preact'")
	}
}

func TestBunLockFormatter_DeterministicOutput(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	f := NewBunLockFormatter()

	// Format twice with the same input.
	result1 := resolveForFormatTest(t, reg, project)
	output1, err := f.FormatFromResult(result1, project)
	if err != nil {
		t.Fatalf("first format failed: %v", err)
	}

	result2 := resolveForFormatTest(t, reg, project)
	output2, err := f.FormatFromResult(result2, project)
	if err != nil {
		t.Fatalf("second format failed: %v", err)
	}

	if !bytes.Equal(output1, output2) {
		t.Error("output is not deterministic; two identical runs produced different bytes")
		t.Logf("output1:\n%s", string(output1))
		t.Logf("output2:\n%s", string(output2))
	}
}

func TestBunLockFormatter_FormatInterfaceReturnsError(t *testing.T) {
	f := NewBunLockFormatter()
	_, err := f.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error from Format(), got nil")
	}
	if !strings.Contains(err.Error(), "FormatFromResult") {
		t.Errorf("error message should mention FormatFromResult, got: %s", err.Error())
	}
}

func TestBunLockFormatter_SingleOrSlice(t *testing.T) {
	// Single element returns a string.
	result := singleOrSlice([]string{"linux"})
	if s, ok := result.(string); !ok || s != "linux" {
		t.Errorf("singleOrSlice([linux]) = %v, want string 'linux'", result)
	}

	// Multiple elements returns the slice.
	result = singleOrSlice([]string{"linux", "darwin"})
	if sl, ok := result.([]string); !ok || len(sl) != 2 {
		t.Errorf("singleOrSlice([linux, darwin]) = %v, want []string{linux, darwin}", result)
	}
}

func TestBunLockFormatter_NormalizeBunCPU(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"x64"}, []string{"x64"}},
		{[]string{"arm64"}, []string{"arm64"}},
		{[]string{"arm"}, []string{"arm"}},
		{[]string{"ia32"}, []string{"ia32"}},
		{[]string{"ppc64"}, []string{"ppc64"}},
		{[]string{"s390x"}, []string{"s390x"}},
		{[]string{"none"}, []string{"none"}},
		// Unknown architectures become "none".
		{[]string{"riscv64"}, []string{"none"}},
		{[]string{"wasm32"}, []string{"none"}},
		{[]string{"mips"}, []string{"none"}},
		// Mixed known and unknown.
		{[]string{"x64", "riscv64", "arm64"}, []string{"x64", "none", "arm64"}},
	}

	for _, tc := range tests {
		got := normalizeBunCPU(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("normalizeBunCPU(%v) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("normalizeBunCPU(%v)[%d] = %s, want %s", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

func TestBunLockFormatter_RealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specJSON := []byte(`{
		"name": "test-project",
		"version": "1.0.0",
		"dependencies": {
			"is-odd": "^3.0.0"
		}
	}`)

	parser := npm.NewSpecParser()
	spec, err := parser.Parse(specJSON)
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	registry := npm.NewRegistryClient("")

	resolver := NewResolver()
	result, err := resolver.ResolveForLockfile(context.Background(), spec, registry, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	formatter := NewBunLockFormatter()
	output, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// Strip trailing commas for JSON validation.
	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("output is not valid JSONC: %v\nfirst 500 bytes:\n%s", err, truncate(output, 500))
	}

	content := string(output)

	// Verify is-odd is present.
	if !strings.Contains(content, "is-odd") {
		t.Error("output should contain is-odd")
	}

	// Verify is-number (transitive dep of is-odd) is present.
	if !strings.Contains(content, "is-number") {
		t.Error("output should contain is-number (transitive dep)")
	}

	// Verify top-level keys.
	for _, key := range []string{"lockfileVersion", "configVersion", "workspaces", "packages"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}

	t.Logf("generated %d bytes of bun.lock", len(output))
}

func TestBunLockFormatter_FileDepEntry(t *testing.T) {
	// Build a ResolveResult with a file: dependency manually, bypassing the
	// resolver, to test the formatter in isolation.
	localNode := &ecosystem.Node{
		Name:       "local-pkg",
		Version:    "1.0.0",
		TarballURL: "file:./local-pkg",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test",
			Version: "1.0.0",
			Dependencies: []*ecosystem.Edge{
				{
					Name:       "local-pkg",
					Constraint: "file:./local-pkg",
					Target:     localNode,
					Type:       ecosystem.DepRegular,
				},
			},
		},
		Nodes: map[string]*ecosystem.Node{
			"local-pkg@file:./local-pkg": localNode,
		},
	}

	result := &ResolveResult{
		Graph: graph,
		Packages: map[string]*ResolvedPackage{
			"local-pkg@file:./local-pkg": {
				Node:         localNode,
				Dependencies: map[string]DepInfo{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "local-pkg", Constraint: "file:./local-pkg", Type: ecosystem.DepRegular},
		},
	}

	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, string(output))
	}

	packages := parsed["packages"].(map[string]interface{})
	entry, ok := packages["local-pkg"]
	if !ok {
		t.Fatalf("missing local-pkg entry in packages; got keys: %v", packagesKeys(packages))
	}

	arr, ok := entry.([]interface{})
	if !ok {
		t.Fatalf("local-pkg entry is not an array")
	}

	if len(arr) != 2 {
		t.Fatalf("local-pkg entry has %d elements, want 2; got: %v", len(arr), arr)
	}

	if arr[0] != "local-pkg@file:./local-pkg" {
		t.Errorf("element 0 = %v, want %q", arr[0], "local-pkg@file:./local-pkg")
	}
	if arr[1] != "" {
		t.Errorf("element 1 = %v, want empty string", arr[1])
	}
}

func TestBunLockFormatter_GitDepEntry(t *testing.T) {
	// GitHub-style git dep: constraint is "github:jonschlinkert/is-odd",
	// resolved TarballURL is the git+ssh URL with commit hash.
	gitNode := &ecosystem.Node{
		Name:       "is-odd",
		Version:    "3.0.1",
		TarballURL: "git+ssh://git@github.com/jonschlinkert/is-odd.git#abc123def",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test",
			Version: "1.0.0",
			Dependencies: []*ecosystem.Edge{
				{
					Name:       "git-pkg",
					Constraint: "github:jonschlinkert/is-odd",
					Target:     gitNode,
					Type:       ecosystem.DepRegular,
				},
			},
		},
		Nodes: map[string]*ecosystem.Node{
			"is-odd@github:jonschlinkert/is-odd": gitNode,
		},
	}

	result := &ResolveResult{
		Graph: graph,
		Packages: map[string]*ResolvedPackage{
			"is-odd@github:jonschlinkert/is-odd": {
				Node:         gitNode,
				Dependencies: map[string]DepInfo{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "git-pkg", Constraint: "github:jonschlinkert/is-odd", Type: ecosystem.DepRegular},
		},
	}

	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, string(output))
	}

	packages := parsed["packages"].(map[string]interface{})
	// The entry key should be "git-pkg" (the declared alias name).
	entry, ok := packages["git-pkg"]
	if !ok {
		t.Fatalf("missing git-pkg entry in packages; got keys: %v", packagesKeys(packages))
	}

	arr, ok := entry.([]interface{})
	if !ok {
		t.Fatalf("git-pkg entry is not an array")
	}

	if len(arr) != 2 {
		t.Fatalf("git-pkg entry has %d elements, want 2; got: %v", len(arr), arr)
	}

	// Element 0: declared name + original constraint.
	if arr[0] != "git-pkg@github:jonschlinkert/is-odd" {
		t.Errorf("element 0 = %v, want %q", arr[0], "git-pkg@github:jonschlinkert/is-odd")
	}
	// Element 1: resolved URL with commit hash.
	if arr[1] != "is-odd@git+ssh://git@github.com/jonschlinkert/is-odd.git#abc123def" {
		t.Errorf("element 1 = %v, want %q", arr[1], "is-odd@git+ssh://git@github.com/jonschlinkert/is-odd.git#abc123def")
	}
}

func TestBunLockFormatter_TarballDepEntry(t *testing.T) {
	// Tarball URL dep: constraint is the full URL, resolved via registry.
	tarballNode := &ecosystem.Node{
		Name:       "is-odd",
		Version:    "3.0.1",
		TarballURL: "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz",
		Integrity:  "sha512-fakehash",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test",
			Version: "1.0.0",
			Dependencies: []*ecosystem.Edge{
				{
					Name:       "tarball-pkg",
					Constraint: "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz",
					Target:     tarballNode,
					Type:       ecosystem.DepRegular,
				},
			},
		},
		Nodes: map[string]*ecosystem.Node{
			"is-odd@3.0.1": tarballNode,
		},
	}

	result := &ResolveResult{
		Graph: graph,
		Packages: map[string]*ResolvedPackage{
			"is-odd@3.0.1": {
				Node:         tarballNode,
				Dependencies: map[string]DepInfo{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "tarball-pkg", Constraint: "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz", Type: ecosystem.DepRegular},
		},
	}

	f := NewBunLockFormatter()
	output, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, string(output))
	}

	packages := parsed["packages"].(map[string]interface{})
	// Entry key should be "tarball-pkg" (the declared alias name).
	entry, ok := packages["tarball-pkg"]
	if !ok {
		t.Fatalf("missing tarball-pkg entry in packages; got keys: %v", packagesKeys(packages))
	}

	arr, ok := entry.([]interface{})
	if !ok {
		t.Fatalf("tarball-pkg entry is not an array")
	}

	if len(arr) != 2 {
		t.Fatalf("tarball-pkg entry has %d elements, want 2; got: %v", len(arr), arr)
	}

	url := "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz"
	if arr[0] != "tarball-pkg@"+url {
		t.Errorf("element 0 = %v, want %q", arr[0], "tarball-pkg@"+url)
	}
	if arr[1] != "tarball-pkg@"+url {
		t.Errorf("element 1 = %v, want %q", arr[1], "tarball-pkg@"+url)
	}
}

func TestBunLockFormatter_NonRegistryDeps_RealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	// Create a temp workspace with a local-pkg subdirectory.
	tmpDir := t.TempDir()

	// Create local-pkg/package.json.
	localDir := tmpDir + "/local-pkg"
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localDir+"/package.json", []byte(`{"name":"local-pkg","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	specJSON := []byte(`{
		"name": "test",
		"version": "1.0.0",
		"dependencies": {
			"ms": "^2.1.0",
			"local-pkg": "file:./local-pkg"
		}
	}`)

	parser := npm.NewSpecParser()
	spec, err := parser.Parse(specJSON)
	if err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	registry := npm.NewRegistryClient("")
	resolver := NewResolver()
	result, err := resolver.ResolveForLockfile(context.Background(), spec, registry, ecosystem.ResolveOptions{
		SpecDir: tmpDir,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	formatter := NewBunLockFormatter()
	output, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		t.Fatalf("format: %v", err)
	}

	// Validate JSONC.
	cleaned := stripTrailingCommas(output)
	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("output is not valid JSONC: %v\n%s", err, truncate(output, 500))
	}

	packages, ok := parsed["packages"].(map[string]interface{})
	if !ok {
		t.Fatal("packages is not an object")
	}

	// Verify ms is a 4-element array (normal registry dep).
	msEntry, ok := packages["ms"]
	if !ok {
		t.Fatal("missing ms entry in packages")
	}
	msArr, ok := msEntry.([]interface{})
	if !ok {
		t.Fatal("ms entry is not an array")
	}
	if len(msArr) != 4 {
		t.Errorf("ms entry has %d elements, want 4 (normal registry dep)", len(msArr))
	}

	// Verify local-pkg is a 2-element array (file: dep).
	localEntry, ok := packages["local-pkg"]
	if !ok {
		t.Fatalf("missing local-pkg entry in packages; got keys: %v", packagesKeys(packages))
	}
	localArr, ok := localEntry.([]interface{})
	if !ok {
		t.Fatal("local-pkg entry is not an array")
	}
	if len(localArr) != 2 {
		t.Errorf("local-pkg entry has %d elements, want 2 (file: dep); got: %v", len(localArr), localArr)
	}

	t.Logf("generated %d bytes of bun.lock", len(output))
}

// packagesKeys returns sorted keys from a packages map for diagnostics.
func packagesKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncate(data []byte, n int) string {
	if len(data) <= n {
		return string(data)
	}
	return string(data[:n]) + "..."
}

func TestBunLockFormatter_AliasedDep(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-positive", "1.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "alias-test",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "positive", Constraint: "npm:is-positive@1.0.0", Type: ecosystem.DepRegular},
		},
	}

	result := resolveForFormatTest(t, reg, project)
	f := NewBunLockFormatter()

	data, err := f.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	lockfile := string(data)
	t.Logf("Generated lockfile:\n%s", lockfile)

	// Bun aliases: the package key should use the alias name "positive".
	if !strings.Contains(lockfile, `"positive"`) {
		t.Error("missing alias key 'positive' in packages section")
	}

	// Should reference is-positive in the resolved URL.
	if !strings.Contains(lockfile, "is-positive") {
		t.Error("missing resolved is-positive package reference")
	}
}
