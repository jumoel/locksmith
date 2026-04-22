package yarn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/npm"
)

// TestBerryRootDepsQuoting verifies that YAML-special constraint values
// get quoted in the workspace root dependencies section.
func TestBerryRootDepsQuoting(t *testing.T) {
	// Create a minimal resolve result with a "*" constraint dep.
	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
			Version: "1.0.0",
			Dependencies: []*ecosystem.Edge{
				{
					Name:       "wrappy",
					Constraint: "*",
					Target:     &ecosystem.Node{Name: "wrappy", Version: "1.0.2"},
					Type:       ecosystem.DepRegular,
				},
			},
		},
		Nodes: map[string]*ecosystem.Node{
			"wrappy@1.0.2": {Name: "wrappy", Version: "1.0.2", Integrity: "sha512-fake"},
		},
	}

	result := &ResolveResult{
		Graph: graph,
		Packages: map[string]*ResolvedPackage{
			"wrappy@1.0.2": {
				Node:         graph.Nodes["wrappy@1.0.2"],
				Dependencies: map[string]string{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "wrappy", Constraint: "*", Type: ecosystem.DepRegular},
		},
	}

	// Test v6 format (non-RootDepsNpmPrefix).
	formatter := NewYarnBerryV6Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult failed: %v", err)
	}

	output := string(data)

	// The "*" constraint must be quoted to avoid YAML alias interpretation.
	if strings.Contains(output, "wrappy: *\n") {
		t.Error("wrappy constraint is unquoted bare * (YAML alias); should be quoted")
	}
	if !strings.Contains(output, `wrappy: "*"`) && !strings.Contains(output, `wrappy: "*"`) {
		t.Errorf("wrappy constraint should be quoted as \"*\", got:\n%s", output)
	}

	// Test v8 format (RootDepsNpmPrefix) - should use "npm:*".
	formatter8 := NewYarnBerryV8Formatter()
	data8, err := formatter8.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult v8 failed: %v", err)
	}
	output8 := string(data8)
	if !strings.Contains(output8, `wrappy: "npm:*"`) {
		t.Errorf("v8 wrappy constraint should be \"npm:*\", got:\n%s", output8)
	}
}

// TestBerryPeerDepsMetaAccuracy verifies that only peers explicitly marked
// optional in peerDependenciesMeta get the optional: true flag.
func TestBerryPeerDepsMetaAccuracy(t *testing.T) {
	// 3 peers: react (required), react-dom (optional), @types/react (required).
	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
			Version: "1.0.0",
		},
		Nodes: map[string]*ecosystem.Node{},
	}

	result := &ResolveResult{
		Graph:    graph,
		Packages: map[string]*ResolvedPackage{},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
			{Name: "react-dom", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
			{Name: "@types/react", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
		},
		PeerDepsMeta: map[string]ecosystem.PeerDepMeta{
			"react-dom": {Optional: true},
		},
	}

	formatter := NewYarnBerryV6Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult failed: %v", err)
	}
	output := string(data)

	// peerDependencies should list all 3 peers.
	if !strings.Contains(output, "  peerDependencies:\n") {
		t.Fatal("expected peerDependencies section")
	}
	if !strings.Contains(output, "react: ^18.0.0") {
		t.Error("missing react in peerDependencies")
	}
	if !strings.Contains(output, "react-dom: ^18.0.0") {
		t.Error("missing react-dom in peerDependencies")
	}

	// peerDependenciesMeta should only contain react-dom.
	if !strings.Contains(output, "  peerDependenciesMeta:\n") {
		t.Fatal("expected peerDependenciesMeta section")
	}
	if !strings.Contains(output, "    react-dom:\n      optional: true\n") {
		t.Error("react-dom should be marked optional in peerDependenciesMeta")
	}

	// react and @types/react must NOT appear in peerDependenciesMeta.
	// Count occurrences of "optional: true" - should be exactly 1.
	count := strings.Count(output, "optional: true")
	if count != 1 {
		t.Errorf("expected 1 occurrence of 'optional: true', got %d.\nOutput:\n%s", count, output)
	}
}

// TestBerryPeerDepsMetaAllRequired verifies that the peerDependenciesMeta
// section is absent when no peers are optional.
func TestBerryPeerDepsMetaAllRequired(t *testing.T) {
	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
			Version: "1.0.0",
		},
		Nodes: map[string]*ecosystem.Node{},
	}

	result := &ResolveResult{
		Graph:    graph,
		Packages: map[string]*ResolvedPackage{},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
			{Name: "react-dom", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
		},
		// No PeerDepsMeta - all peers are required.
	}

	formatter := NewYarnBerryV6Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult failed: %v", err)
	}
	output := string(data)

	// peerDependencies should be present.
	if !strings.Contains(output, "  peerDependencies:\n") {
		t.Fatal("expected peerDependencies section")
	}

	// peerDependenciesMeta should NOT be present.
	if strings.Contains(output, "peerDependenciesMeta") {
		t.Errorf("peerDependenciesMeta should be absent when no peers are optional.\nOutput:\n%s", output)
	}
}

// TestBerryPeerDepsMetaAllOptional verifies that all peers appear in
// peerDependenciesMeta when all are optional.
func TestBerryPeerDepsMetaAllOptional(t *testing.T) {
	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
			Version: "1.0.0",
		},
		Nodes: map[string]*ecosystem.Node{},
	}

	result := &ResolveResult{
		Graph:    graph,
		Packages: map[string]*ResolvedPackage{},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
			{Name: "react-dom", Constraint: "^18.0.0", Type: ecosystem.DepPeer},
		},
		PeerDepsMeta: map[string]ecosystem.PeerDepMeta{
			"react":     {Optional: true},
			"react-dom": {Optional: true},
		},
	}

	formatter := NewYarnBerryV6Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("FormatFromResult failed: %v", err)
	}
	output := string(data)

	// peerDependenciesMeta should list both peers.
	if !strings.Contains(output, "  peerDependenciesMeta:\n") {
		t.Fatal("expected peerDependenciesMeta section")
	}

	count := strings.Count(output, "optional: true")
	if count != 2 {
		t.Errorf("expected 2 occurrences of 'optional: true', got %d.\nOutput:\n%s", count, output)
	}

	if !strings.Contains(output, "    react:\n      optional: true\n") {
		t.Error("react should be marked optional")
	}
	if !strings.Contains(output, "    react-dom:\n      optional: true\n") {
		t.Error("react-dom should be marked optional")
	}
}

// TestBerryFileDepEntry verifies that file: dependencies produce the correct
// berry lockfile entry with portal: resolution and soft linkType.
func TestBerryFileDepEntry(t *testing.T) {
	localNode := &ecosystem.Node{
		Name:       "local-pkg",
		Version:    "0.0.0-local",
		TarballURL: "file:./local-pkg",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
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
				Dependencies: map[string]string{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "local-pkg", Constraint: "file:./local-pkg", Type: ecosystem.DepRegular},
		},
	}

	for name, formatter := range map[string]interface {
		FormatFromResult(*ResolveResult, *ecosystem.ProjectSpec) ([]byte, error)
	}{
		"v6": NewYarnBerryV6Formatter(),
		"v8": NewYarnBerryV8Formatter(),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := formatter.FormatFromResult(result, project)
			if err != nil {
				t.Fatalf("FormatFromResult failed: %v", err)
			}
			output := string(data)

			// Constraint key should use file: protocol.
			if !strings.Contains(output, `"local-pkg@file:./local-pkg"`) {
				t.Errorf("missing file: constraint key in output:\n%s", output)
			}

			// Resolution should use portal: with locator suffix.
			expectedRes := `"local-pkg@portal:./local-pkg::locator=test-project%40workspace%3A."`
			if !strings.Contains(output, expectedRes) {
				t.Errorf("expected portal: resolution %s, got:\n%s", expectedRes, output)
			}

			// linkType must be soft for file: deps.
			// Find the file dep entry and check its linkType.
			entryIdx := strings.Index(output, `"local-pkg@file:./local-pkg"`)
			if entryIdx == -1 {
				t.Fatal("file dep entry not found")
			}
			entrySection := output[entryIdx:]
			// Find the next entry boundary (double newline or end).
			nextEntry := strings.Index(entrySection[1:], "\n\n")
			if nextEntry > 0 {
				entrySection = entrySection[:nextEntry+1]
			}
			if !strings.Contains(entrySection, "linkType: soft") {
				t.Errorf("file dep should have linkType: soft, got:\n%s", entrySection)
			}
			if strings.Contains(entrySection, "linkType: hard") {
				t.Errorf("file dep should NOT have linkType: hard, got:\n%s", entrySection)
			}
		})
	}
}

// TestBerryGitDepEntry verifies that git/github dependencies produce the
// correct berry lockfile entry with https resolution and commit hash.
func TestBerryGitDepEntry(t *testing.T) {
	gitNode := &ecosystem.Node{
		Name:       "is-odd",
		Version:    "3.0.1",
		TarballURL: "git+ssh://git@github.com/jonschlinkert/is-odd.git#abc123",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
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
				Dependencies: map[string]string{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "git-pkg", Constraint: "github:jonschlinkert/is-odd", Type: ecosystem.DepRegular},
		},
	}

	for name, formatter := range map[string]interface {
		FormatFromResult(*ResolveResult, *ecosystem.ProjectSpec) ([]byte, error)
	}{
		"v6": NewYarnBerryV6Formatter(),
		"v8": NewYarnBerryV8Formatter(),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := formatter.FormatFromResult(result, project)
			if err != nil {
				t.Fatalf("FormatFromResult failed: %v", err)
			}
			output := string(data)

			// Constraint key should contain the dep alias and github constraint.
			if !strings.Contains(output, `"git-pkg@github:jonschlinkert/is-odd"`) {
				t.Errorf("missing github constraint key in output:\n%s", output)
			}

			// Resolution should use https URL with #commit= format.
			expectedRes := `"git-pkg@https://github.com/jonschlinkert/is-odd.git#commit=abc123"`
			if !strings.Contains(output, expectedRes) {
				t.Errorf("expected git resolution %s, got:\n%s", expectedRes, output)
			}

			// linkType must be hard for git deps.
			entryIdx := strings.Index(output, `"git-pkg@github:jonschlinkert/is-odd"`)
			if entryIdx == -1 {
				t.Fatal("git dep entry not found")
			}
			entrySection := output[entryIdx:]
			nextEntry := strings.Index(entrySection[1:], "\n\n")
			if nextEntry > 0 {
				entrySection = entrySection[:nextEntry+1]
			}
			if !strings.Contains(entrySection, "linkType: hard") {
				t.Errorf("git dep should have linkType: hard, got:\n%s", entrySection)
			}
		})
	}
}

// TestBerryTarballDepEntry verifies that tarball URL dependencies that resolved
// to a known npm package produce the correct berry lockfile entry with npm resolution.
func TestBerryTarballDepEntry(t *testing.T) {
	tarballNode := &ecosystem.Node{
		Name:       "is-odd",
		Version:    "3.0.1",
		TarballURL: "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz",
		Integrity:  "sha512-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}

	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test-project",
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
				Dependencies: map[string]string{},
			},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "tarball-pkg", Constraint: "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz", Type: ecosystem.DepRegular},
		},
	}

	for name, formatter := range map[string]interface {
		FormatFromResult(*ResolveResult, *ecosystem.ProjectSpec) ([]byte, error)
	}{
		"v6": NewYarnBerryV6Formatter(),
		"v8": NewYarnBerryV8Formatter(),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := formatter.FormatFromResult(result, project)
			if err != nil {
				t.Fatalf("FormatFromResult failed: %v", err)
			}
			output := string(data)

			// Tarball dep that resolved to a known npm package should use npm resolution.
			if !strings.Contains(output, `"is-odd@npm:3.0.1"`) {
				t.Errorf("tarball dep should have npm resolution, got:\n%s", output)
			}

			// Should have a checksum (since it's a real npm package with integrity).
			if name == "v6" {
				if !strings.Contains(output, "checksum: ") {
					t.Errorf("tarball dep should have checksum in %s, got:\n%s", name, output)
				}
			}
			if name == "v8" {
				if !strings.Contains(output, "checksum: 10/") {
					t.Errorf("tarball dep should have checksum with 10/ prefix in %s, got:\n%s", name, output)
				}
			}

			// linkType must be hard for tarball deps.
			if !strings.Contains(output, "linkType: hard") {
				t.Errorf("tarball dep should have linkType: hard, got:\n%s", output)
			}
		})
	}
}

// TestBerryNonRegistryDeps_RealRegistry is an end-to-end test that exercises the
// full berry pipeline (parse, resolve, format) with a fixture containing file: deps
// and regular deps, using the real npm registry.
func TestBerryNonRegistryDeps_RealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	// Create a temp workspace with a local-pkg subdirectory.
	tmpDir := t.TempDir()
	localPkgDir := filepath.Join(tmpDir, "local-pkg")
	if err := os.MkdirAll(localPkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localPkgDir, "package.json"), []byte(`{"name":"local-pkg","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	specJSON := []byte(`{
		"name": "berry-nonreg-test",
		"version": "1.0.0",
		"dependencies": {
			"ms": "^2.1.0",
			"local-pkg": "file:./local-pkg"
		}
	}`)

	parser := npm.NewSpecParser()
	spec, err := parser.Parse(specJSON)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	registry := npm.NewRegistryClient("")
	resolver := NewBerryResolver()
	resolveOpts := ecosystem.ResolveOptions{SpecDir: tmpDir}

	result, err := resolver.ResolveForLockfile(context.Background(), spec, registry, resolveOpts)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	for name, formatter := range map[string]interface {
		FormatFromResult(*ResolveResult, *ecosystem.ProjectSpec) ([]byte, error)
	}{
		"v6": NewYarnBerryV6Formatter(),
		"v8": NewYarnBerryV8Formatter(),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := formatter.FormatFromResult(result, spec)
			if err != nil {
				t.Fatalf("FormatFromResult failed: %v", err)
			}
			output := string(data)

			// Should contain the local-pkg file dep.
			if !strings.Contains(output, "local-pkg@file:./local-pkg") {
				t.Errorf("missing local-pkg file dep in output:\n%s", output)
			}

			// Should contain portal: resolution with locator.
			if !strings.Contains(output, "portal:./local-pkg::locator=") {
				t.Errorf("missing portal: resolution in output:\n%s", output)
			}

			// Should contain the ms npm dep.
			if !strings.Contains(output, "ms@npm:") {
				t.Errorf("missing ms npm dep in output:\n%s", output)
			}

			// Should contain the __metadata header.
			if !strings.Contains(output, "__metadata:") {
				t.Errorf("missing __metadata in output:\n%s", output)
			}

			t.Logf("%s output (%d bytes):\n%s", name, len(data), output)
		})
	}
}

// TestBerryConstraintPreservation verifies that all constraints are kept
// when multiple ranges resolve to the same version.
func TestBerryConstraintPreservation(t *testing.T) {
	node := &ecosystem.Node{Name: "tslib", Version: "2.8.1", Integrity: "sha512-fake"}
	graph := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "test",
			Version: "1.0.0",
			Dependencies: []*ecosystem.Edge{
				{Name: "tslib", Constraint: "^2.4.0", Target: node, Type: ecosystem.DepRegular},
			},
		},
		Nodes: map[string]*ecosystem.Node{"tslib@2.8.1": node},
	}
	otherNode := &ecosystem.Node{
		Name: "other", Version: "1.0.0", Integrity: "sha512-other",
		Dependencies: []*ecosystem.Edge{
			{Name: "tslib", Constraint: "^2.8.0", Target: node, Type: ecosystem.DepRegular},
		},
	}
	graph.Nodes["other@1.0.0"] = otherNode

	result := &ResolveResult{
		Graph: graph,
		Packages: map[string]*ResolvedPackage{
			"tslib@2.8.1": {Node: node, Dependencies: map[string]string{}},
			"other@1.0.0": {Node: otherNode, Dependencies: map[string]string{"tslib": "2.8.1"}},
		},
	}

	project := &ecosystem.ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "tslib", Constraint: "^2.4.0", Type: ecosystem.DepRegular},
		},
	}

	// Both v6 AND v8 should keep ALL constraints.
	for name, formatter := range map[string]interface {
		FormatFromResult(*ResolveResult, *ecosystem.ProjectSpec) ([]byte, error)
	}{
		"v6": NewYarnBerryV6Formatter(),
		"v8": NewYarnBerryV8Formatter(),
	} {
		data, err := formatter.FormatFromResult(result, project)
		if err != nil {
			t.Fatalf("%s format failed: %v", name, err)
		}
		out := string(data)
		if !strings.Contains(out, "tslib@npm:^2.4.0") {
			t.Errorf("%s should keep ^2.4.0 constraint, got:\n%s", name, out)
		}
		if !strings.Contains(out, "tslib@npm:^2.8.0") {
			t.Errorf("%s should keep ^2.8.0 constraint, got:\n%s", name, out)
		}
	}
}
