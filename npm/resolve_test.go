package npm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
)

// mockRegistry implements ecosystem.Registry for testing.
type mockRegistry struct {
	packages map[string]*mockPackage
}

type mockPackage struct {
	versions map[string]*ecosystem.VersionMetadata
	times    map[string]time.Time
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{packages: make(map[string]*mockPackage)}
}

func (m *mockRegistry) addVersion(name, version string, published time.Time, deps map[string]string) {
	m.addVersionFull(name, version, published, deps, nil)
}

func (m *mockRegistry) addVersionFull(name, version string, published time.Time, deps, optDeps map[string]string) {
	pkg, ok := m.packages[name]
	if !ok {
		pkg = &mockPackage{
			versions: make(map[string]*ecosystem.VersionMetadata),
			times:    make(map[string]time.Time),
		}
		m.packages[name] = pkg
	}
	pkg.versions[version] = &ecosystem.VersionMetadata{
		Name:         name,
		Version:      version,
		Integrity:    fmt.Sprintf("sha512-fake-%s-%s", name, version),
		TarballURL:   fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version),
		Dependencies: deps,
		OptionalDeps: optDeps,
	}
	pkg.times[version] = published
}

func (m *mockRegistry) FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]ecosystem.VersionInfo, error) {
	pkg, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	var versions []ecosystem.VersionInfo
	for v, t := range pkg.times {
		if cutoff != nil && t.After(*cutoff) {
			continue
		}
		versions = append(versions, ecosystem.VersionInfo{
			Version:     v,
			PublishedAt: t,
		})
	}
	return versions, nil
}

func (m *mockRegistry) FetchMetadata(ctx context.Context, name, version string) (*ecosystem.VersionMetadata, error) {
	pkg, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	meta, ok := pkg.versions[version]
	if !ok {
		return nil, fmt.Errorf("version %s of %s not found", version, name)
	}
	return meta, nil
}

// baseTime is a fixed reference point for test timestamps.
var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

func TestResolve_SingleDep(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(24*time.Hour), nil)
	reg.addVersion("A", "1.2.0", baseTime.Add(48*time.Hour), nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve to highest matching version.
	if result.Graph.Root == nil {
		t.Fatal("root is nil")
	}
	if len(result.Graph.Root.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(result.Graph.Root.Dependencies))
	}

	edge := result.Graph.Root.Dependencies[0]
	if edge.Target.Version != "1.2.0" {
		t.Errorf("expected A@1.2.0, got A@%s", edge.Target.Version)
	}

	// Should be placed at root node_modules.
	placed, ok := result.PlacedNodes["node_modules/A"]
	if !ok {
		t.Fatal("A not placed at node_modules/A")
	}
	if placed.Node.Version != "1.2.0" {
		t.Errorf("placed node version = %s, want 1.2.0", placed.Node.Version)
	}
}

func TestResolve_TransitiveDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^2.0.0"})
	reg.addVersion("B", "2.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "2.1.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.3.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify full chain resolved with highest versions.
	aEdge := result.Graph.Root.Dependencies[0]
	if aEdge.Target.Version != "1.0.0" {
		t.Errorf("A version = %s, want 1.0.0", aEdge.Target.Version)
	}

	bEdge := aEdge.Target.Dependencies[0]
	if bEdge.Target.Version != "2.1.0" {
		t.Errorf("B version = %s, want 2.1.0", bEdge.Target.Version)
	}

	cEdge := bEdge.Target.Dependencies[0]
	if cEdge.Target.Version != "1.3.0" {
		t.Errorf("C version = %s, want 1.3.0", cEdge.Target.Version)
	}

	// All should be hoisted to root since no conflicts.
	for _, name := range []string{"A", "B", "C"} {
		path := "node_modules/" + name
		if _, ok := result.PlacedNodes[path]; !ok {
			t.Errorf("%s not placed at %s", name, path)
		}
	}

	// Should be exactly 3 placed nodes.
	if len(result.PlacedNodes) != 3 {
		t.Errorf("expected 3 placed nodes, got %d", len(result.PlacedNodes))
	}
}

func TestResolve_DiamondDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.2.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C should be deduplicated - only one placement at root.
	cPlaced, ok := result.PlacedNodes["node_modules/C"]
	if !ok {
		t.Fatal("C not placed at node_modules/C")
	}
	if cPlaced.Node.Version != "1.2.0" {
		t.Errorf("C version = %s, want 1.2.0", cPlaced.Node.Version)
	}

	// Should be exactly 3 placed nodes (A, B, C), not 4.
	if len(result.PlacedNodes) != 3 {
		t.Errorf("expected 3 placed nodes (deduped), got %d", len(result.PlacedNodes))
		for path := range result.PlacedNodes {
			t.Logf("  placed: %s", path)
		}
	}
}

func TestResolve_ConflictingVersions(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^2.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.5.0", baseTime, nil)
	reg.addVersion("C", "2.0.0", baseTime, nil)
	reg.addVersion("C", "2.3.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// One version of C should be hoisted to root, the other nested.
	// npm's algorithm processes deps in order: A first, then B.
	// A's dep C@^1.0.0 resolves to C@1.5.0 and gets hoisted to root.
	// B's dep C@^2.0.0 resolves to C@2.3.0 and conflicts, so it gets nested under B.
	rootC, hasRootC := result.PlacedNodes["node_modules/C"]
	nestedC, hasNestedC := result.PlacedNodes["node_modules/B/node_modules/C"]

	if !hasRootC {
		t.Fatal("expected C at node_modules/C")
	}
	if !hasNestedC {
		t.Fatal("expected C at node_modules/B/node_modules/C")
	}

	// The first-resolved version (from A) should be at root.
	if rootC.Node.Version != "1.5.0" {
		t.Errorf("root C version = %s, want 1.5.0", rootC.Node.Version)
	}
	if nestedC.Node.Version != "2.3.0" {
		t.Errorf("nested C version = %s, want 2.3.0", nestedC.Node.Version)
	}

	// Should be 4 placed nodes: A, B, C@root, C@nested.
	if len(result.PlacedNodes) != 4 {
		t.Errorf("expected 4 placed nodes, got %d", len(result.PlacedNodes))
		for path, pn := range result.PlacedNodes {
			t.Logf("  placed: %s -> %s@%s", path, pn.Node.Name, pn.Node.Version)
		}
	}
}

func TestResolve_CutoffDate(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(30*24*time.Hour), nil)   // Jan 31
	reg.addVersion("A", "1.2.0", baseTime.Add(60*24*time.Hour), nil)   // Mar 1
	reg.addVersion("A", "2.0.0", baseTime.Add(90*24*time.Hour), nil)   // Mar 31

	// Cutoff at Feb 15 - should only see 1.0.0 and 1.1.0.
	cutoff := baseTime.Add(45 * 24 * time.Hour)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{
		CutoffDate: &cutoff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := result.Graph.Root.Dependencies[0]
	if edge.Target.Version != "1.1.0" {
		t.Errorf("expected A@1.1.0 with cutoff, got A@%s", edge.Target.Version)
	}
}

func TestResolve_OptionalDepFails(t *testing.T) {
	reg := newMockRegistry()
	// A has a regular dep on B and an optional dep on "missing-pkg".
	reg.addVersionFull("A", "1.0.0", baseTime,
		map[string]string{"B": "^1.0.0"},
		map[string]string{"missing-pkg": "^1.0.0"},
	)
	reg.addVersion("B", "1.0.0", baseTime, nil)
	// "missing-pkg" is NOT in the registry.

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolution should succeed despite missing optional dep, got: %v", err)
	}

	// A and B should be resolved, missing-pkg should not appear.
	if len(result.PlacedNodes) != 2 {
		t.Errorf("expected 2 placed nodes (A, B), got %d", len(result.PlacedNodes))
		for path := range result.PlacedNodes {
			t.Logf("  placed: %s", path)
		}
	}

	if _, ok := result.PlacedNodes["node_modules/A"]; !ok {
		t.Error("A not placed")
	}
	if _, ok := result.PlacedNodes["node_modules/B"]; !ok {
		t.Error("B not placed")
	}
}
