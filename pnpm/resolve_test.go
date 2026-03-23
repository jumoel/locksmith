package pnpm

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

func TestPnpmResolve_SingleDep(t *testing.T) {
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
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
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

	// Should have A@1.2.0 in packages.
	pkg, ok := result.Packages["A@1.2.0"]
	if !ok {
		t.Fatal("A@1.2.0 not in packages map")
	}
	if pkg.Node.Integrity != "sha512-fake-A-1.2.0" {
		t.Errorf("unexpected integrity: %s", pkg.Node.Integrity)
	}
	if len(pkg.Dependencies) != 0 {
		t.Errorf("expected 0 dependencies for A, got %d", len(pkg.Dependencies))
	}
}

func TestPnpmResolve_TransitiveDeps(t *testing.T) {
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
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify full chain resolved with highest versions.
	aEdge := result.Graph.Root.Dependencies[0]
	if aEdge.Target.Version != "1.0.0" {
		t.Errorf("A version = %s, want 1.0.0", aEdge.Target.Version)
	}

	// A's transitive dep B should resolve to 2.1.0.
	bEdge := aEdge.Target.Dependencies[0]
	if bEdge.Target.Version != "2.1.0" {
		t.Errorf("B version = %s, want 2.1.0", bEdge.Target.Version)
	}

	// B's transitive dep C should resolve to 1.3.0.
	cEdge := bEdge.Target.Dependencies[0]
	if cEdge.Target.Version != "1.3.0" {
		t.Errorf("C version = %s, want 1.3.0", cEdge.Target.Version)
	}

	// Verify packages map has all three.
	if len(result.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(result.Packages))
	}

	// Verify transitive deps are tracked in ResolvedPackage.Dependencies.
	aPkg := result.Packages["A@1.0.0"]
	if aPkg == nil {
		t.Fatal("A@1.0.0 not in packages")
	}
	if aPkg.Dependencies["B"] != "2.1.0" {
		t.Errorf("A's dep B version = %s, want 2.1.0", aPkg.Dependencies["B"])
	}

	bPkg := result.Packages["B@2.1.0"]
	if bPkg == nil {
		t.Fatal("B@2.1.0 not in packages")
	}
	if bPkg.Dependencies["C"] != "1.3.0" {
		t.Errorf("B's dep C version = %s, want 1.3.0", bPkg.Dependencies["C"])
	}

	cPkg := result.Packages["C@1.3.0"]
	if cPkg == nil {
		t.Fatal("C@1.3.0 not in packages")
	}
	if len(cPkg.Dependencies) != 0 {
		t.Errorf("expected 0 deps for C, got %d", len(cPkg.Dependencies))
	}
}
