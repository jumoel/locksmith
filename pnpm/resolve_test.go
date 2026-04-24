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

func (m *mockRegistry) addVersionWithPeers(name, version string, published time.Time, deps, peerDeps map[string]string) {
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
		PeerDeps:     peerDeps,
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

func (m *mockRegistry) FetchDistTags(ctx context.Context, name string) (map[string]string, error) {
	pkg, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	var latest string
	for v := range pkg.versions {
		if latest == "" || v > latest {
			latest = v
		}
	}
	return map[string]string{"latest": latest}, nil
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

func TestPnpmResolve_PackageExtensions(t *testing.T) {
	// Setup: A@1.0.0 depends on B@^1.0.0.
	// B@1.0.0 has NO dependency on C.
	// packageExtensions adds C as a dep of B.
	// After resolution, C should appear in the graph via B.
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil) // B has no deps
	reg.addVersion("C", "2.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
		PackageExtensions: &ecosystem.PackageExtensionSet{
			Extensions: []ecosystem.PackageExtension{
				{
					Name:         "B",
					Dependencies: map[string]string{"C": "^2.0.0"},
				},
			},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify A is resolved.
	if len(result.Graph.Root.Dependencies) != 1 {
		t.Fatalf("expected 1 root dep, got %d", len(result.Graph.Root.Dependencies))
	}

	// Verify C@2.0.0 is in the packages map (injected via extension).
	cPkg, ok := result.Packages["C@2.0.0"]
	if !ok {
		t.Fatal("C@2.0.0 not in packages map - packageExtensions not applied")
	}
	if cPkg.Node.Version != "2.0.0" {
		t.Errorf("C version = %s, want 2.0.0", cPkg.Node.Version)
	}

	// Verify B@1.0.0 lists C in its dependencies.
	bPkg, ok := result.Packages["B@1.0.0"]
	if !ok {
		t.Fatal("B@1.0.0 not in packages map")
	}
	if bPkg.Dependencies["C"] != "2.0.0" {
		t.Errorf("B's dep C version = %q, want %q", bPkg.Dependencies["C"], "2.0.0")
	}
}

func TestPnpmResolve_PackageExtensions_VersionRange(t *testing.T) {
	// Setup: A@1.0.0 depends on B@^1.0.0 and B@^2.0.0.
	// packageExtensions adds C as a dep of B, but only for B@^2.0.0.
	// After resolution, C should appear via B@2.0.0 but NOT via B@1.0.0.
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^2.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.0.0", baseTime, nil)
	reg.addVersion("C", "1.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
		PackageExtensions: &ecosystem.PackageExtensionSet{
			Extensions: []ecosystem.PackageExtension{
				{
					Name:         "B",
					VersionRange: "^2.0.0",
					Dependencies: map[string]string{"C": "^1.0.0"},
				},
			},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B@2.0.0 should have C as dep.
	bPkg, ok := result.Packages["B@2.0.0"]
	if !ok {
		t.Fatal("B@2.0.0 not in packages map")
	}
	if bPkg.Dependencies["C"] != "1.0.0" {
		t.Errorf("B@2.0.0 dep C = %q, want %q", bPkg.Dependencies["C"], "1.0.0")
	}

	// C should be in the graph.
	if _, ok := result.Packages["C@1.0.0"]; !ok {
		t.Error("C@1.0.0 not in packages map - extension not applied for B@2.0.0")
	}
}

func TestPnpmResolve_PeerDependencyRules_IgnoreMissing(t *testing.T) {
	// Setup: A@1.0.0 has a non-optional peer dep on B.
	// Without rules, B would be auto-installed.
	// With ignoreMissing: ["B"], B should NOT be auto-installed.
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)

	// First: verify B IS auto-installed without rules.
	projectNoRules := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	resultNoRules, err := r.ResolveForLockfile(context.Background(), projectNoRules, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error (no rules): %v", err)
	}
	if _, ok := resultNoRules.Packages["B@1.0.0"]; !ok {
		t.Fatal("baseline check failed: B@1.0.0 should be auto-installed without rules")
	}

	// Now: with ignoreMissing, B should NOT be auto-installed.
	projectWithRules := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
		PeerDependencyRules: &ecosystem.PeerDependencyRules{
			IgnoreMissing: []string{"B"},
		},
	}

	resultWithRules, err := r.ResolveForLockfile(context.Background(), projectWithRules, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error (with rules): %v", err)
	}
	if _, ok := resultWithRules.Packages["B@1.0.0"]; ok {
		t.Error("B@1.0.0 should NOT be auto-installed when ignoreMissing includes 'B'")
	}
}

func TestPnpmResolve_PeerDependencyRules_IgnoreMissing_Glob(t *testing.T) {
	// Setup: A@1.0.0 has non-optional peer deps on @babel/core and @babel/parser.
	// With ignoreMissing: ["@babel/*"], both should be skipped.
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil, map[string]string{
		"@babel/core":   "^7.0.0",
		"@babel/parser": "^7.0.0",
	})
	reg.addVersion("@babel/core", "7.0.0", baseTime, nil)
	reg.addVersion("@babel/parser", "7.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
		PeerDependencyRules: &ecosystem.PeerDependencyRules{
			IgnoreMissing: []string{"@babel/*"},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := result.Packages["@babel/core@7.0.0"]; ok {
		t.Error("@babel/core should NOT be auto-installed when ignoreMissing includes '@babel/*'")
	}
	if _, ok := result.Packages["@babel/parser@7.0.0"]; ok {
		t.Error("@babel/parser should NOT be auto-installed when ignoreMissing includes '@babel/*'")
	}
}

func TestPnpmResolve_PeerDependencyRules_AllowedVersions(t *testing.T) {
	// Setup: A@1.0.0 has peer dep on B with constraint "^1.0.0".
	// B exists at versions 1.0.0 and 2.0.0.
	// Without rules: B@1.0.0 would be installed (matches ^1.0.0).
	// With allowedVersions: {"B": "^2.0.0"}: B@2.0.0 should be installed.
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.0.0", baseTime.Add(24*time.Hour), nil)

	// Baseline: without rules, B@1.0.0 is chosen (^1.0.0 picks latest in 1.x).
	projectNoRules := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	resultNoRules, err := r.ResolveForLockfile(context.Background(), projectNoRules, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error (no rules): %v", err)
	}
	if _, ok := resultNoRules.Packages["B@1.0.0"]; !ok {
		t.Fatal("baseline check failed: B@1.0.0 should be auto-installed with ^1.0.0 constraint")
	}
	if _, ok := resultNoRules.Packages["B@2.0.0"]; ok {
		t.Fatal("baseline check failed: B@2.0.0 should NOT be auto-installed with ^1.0.0 constraint")
	}

	// With allowedVersions: B constraint overridden to ^2.0.0.
	projectWithRules := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
		PeerDependencyRules: &ecosystem.PeerDependencyRules{
			AllowedVersions: map[string]string{"B": "^2.0.0"},
		},
	}

	resultWithRules, err := r.ResolveForLockfile(context.Background(), projectWithRules, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error (with rules): %v", err)
	}
	if _, ok := resultWithRules.Packages["B@2.0.0"]; !ok {
		t.Error("B@2.0.0 should be auto-installed when allowedVersions overrides constraint to ^2.0.0")
	}
	if _, ok := resultWithRules.Packages["B@1.0.0"]; ok {
		t.Error("B@1.0.0 should NOT be present when allowedVersions overrides constraint to ^2.0.0")
	}
}
