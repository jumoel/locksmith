package bun

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

// addVersionMeta registers a version using a full VersionMetadata struct.
func (m *mockRegistry) addVersionMeta(name, version string, published time.Time, meta *ecosystem.VersionMetadata) {
	pkg, ok := m.packages[name]
	if !ok {
		pkg = &mockPackage{
			versions: make(map[string]*ecosystem.VersionMetadata),
			times:    make(map[string]time.Time),
		}
		m.packages[name] = pkg
	}
	// Fill in defaults if not set.
	if meta.Name == "" {
		meta.Name = name
	}
	if meta.Version == "" {
		meta.Version = version
	}
	if meta.Integrity == "" {
		meta.Integrity = fmt.Sprintf("sha512-fake-%s-%s", name, version)
	}
	if meta.TarballURL == "" {
		meta.TarballURL = fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version)
	}
	pkg.versions[version] = meta
	pkg.times[version] = published
}

// addVersion is a convenience wrapper matching the npm/pnpm API.
func (m *mockRegistry) addVersion(name, version string, published time.Time, deps map[string]string) {
	m.addVersionFull(name, version, published, deps, nil)
}

// addVersionFull is a convenience wrapper matching the npm/pnpm API.
func (m *mockRegistry) addVersionFull(name, version string, published time.Time, deps, optDeps map[string]string) {
	m.addVersionMeta(name, version, published, &ecosystem.VersionMetadata{
		Dependencies: deps,
		OptionalDeps: optDeps,
	})
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

func TestBunResolve_SingleDep(t *testing.T) {
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

	// Should have A@1.2.0 in packages map with correct DepInfo.
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

func TestBunResolve_TransitiveDeps(t *testing.T) {
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

	bEdge := aEdge.Target.Dependencies[0]
	if bEdge.Target.Version != "2.1.0" {
		t.Errorf("B version = %s, want 2.1.0", bEdge.Target.Version)
	}

	cEdge := bEdge.Target.Dependencies[0]
	if cEdge.Target.Version != "1.3.0" {
		t.Errorf("C version = %s, want 1.3.0", cEdge.Target.Version)
	}

	// Verify packages map has all three.
	if len(result.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d", len(result.Packages))
	}

	// Verify DepInfo structs contain Constraint, ResolvedName, ResolvedVersion.
	aPkg := result.Packages["A@1.0.0"]
	if aPkg == nil {
		t.Fatal("A@1.0.0 not in packages")
	}
	bDep, ok := aPkg.Dependencies["B"]
	if !ok {
		t.Fatal("A's dependencies missing B")
	}
	if bDep.Constraint != "^2.0.0" {
		t.Errorf("A dep B constraint = %s, want ^2.0.0", bDep.Constraint)
	}
	if bDep.ResolvedName != "B" {
		t.Errorf("A dep B resolved name = %s, want B", bDep.ResolvedName)
	}
	if bDep.ResolvedVersion != "2.1.0" {
		t.Errorf("A dep B resolved version = %s, want 2.1.0", bDep.ResolvedVersion)
	}

	bPkg := result.Packages["B@2.1.0"]
	if bPkg == nil {
		t.Fatal("B@2.1.0 not in packages")
	}
	cDep, ok := bPkg.Dependencies["C"]
	if !ok {
		t.Fatal("B's dependencies missing C")
	}
	if cDep.Constraint != "^1.0.0" {
		t.Errorf("B dep C constraint = %s, want ^1.0.0", cDep.Constraint)
	}
	if cDep.ResolvedName != "C" {
		t.Errorf("B dep C resolved name = %s, want C", cDep.ResolvedName)
	}
	if cDep.ResolvedVersion != "1.3.0" {
		t.Errorf("B dep C resolved version = %s, want 1.3.0", cDep.ResolvedVersion)
	}

	cPkg := result.Packages["C@1.3.0"]
	if cPkg == nil {
		t.Fatal("C@1.3.0 not in packages")
	}
	if len(cPkg.Dependencies) != 0 {
		t.Errorf("expected 0 deps for C, got %d", len(cPkg.Dependencies))
	}
}

func TestBunResolve_PeerDepsSkipped(t *testing.T) {
	reg := newMockRegistry()

	// A declares a peer dep on "react" and a regular dep on B.
	reg.addVersionMeta("A", "1.0.0", baseTime, &ecosystem.VersionMetadata{
		Dependencies: map[string]string{"B": "^1.0.0"},
		PeerDeps:     map[string]string{"react": "^18.0.0"},
	})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("react", "18.2.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aPkg := result.Packages["A@1.0.0"]
	if aPkg == nil {
		t.Fatal("A@1.0.0 not in packages")
	}

	// Peer dep edges should NOT appear in Dependencies.
	if _, hasPeerInDeps := aPkg.Dependencies["react"]; hasPeerInDeps {
		t.Error("peer dep 'react' should not be in Dependencies map")
	}

	// Regular dep B should be there.
	if _, hasB := aPkg.Dependencies["B"]; !hasB {
		t.Error("regular dep 'B' missing from Dependencies")
	}

	// PeerDeps should be populated from metadata.
	if aPkg.PeerDeps == nil {
		t.Fatal("PeerDeps is nil")
	}
	if aPkg.PeerDeps["react"] != "^18.0.0" {
		t.Errorf("PeerDeps[react] = %s, want ^18.0.0", aPkg.PeerDeps["react"])
	}
}

func TestBunResolve_OptionalDepsSkipped(t *testing.T) {
	reg := newMockRegistry()

	// A has a regular dep on B and an optional dep on C.
	reg.addVersionMeta("A", "1.0.0", baseTime, &ecosystem.VersionMetadata{
		Dependencies: map[string]string{"B": "^1.0.0"},
		OptionalDeps: map[string]string{"C": "^1.0.0"},
	})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.0.0", baseTime, nil)

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

	aPkg := result.Packages["A@1.0.0"]
	if aPkg == nil {
		t.Fatal("A@1.0.0 not in packages")
	}

	// Optional dep edges should NOT appear in Dependencies.
	if _, hasC := aPkg.Dependencies["C"]; hasC {
		t.Error("optional dep 'C' should not be in Dependencies map")
	}

	// Regular dep B should be there.
	if _, hasB := aPkg.Dependencies["B"]; !hasB {
		t.Error("regular dep 'B' missing from Dependencies")
	}

	// OptionalDeps should be populated from metadata.
	if aPkg.OptionalDeps == nil {
		t.Fatal("OptionalDeps is nil")
	}
	if aPkg.OptionalDeps["C"] != "^1.0.0" {
		t.Errorf("OptionalDeps[C] = %s, want ^1.0.0", aPkg.OptionalDeps["C"])
	}
}

func TestBunResolve_PeerDepsMeta(t *testing.T) {
	reg := newMockRegistry()

	// A has two peer deps: "react" (required) and "preact" (optional).
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
	// preact is not installed (optional).

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "react", Constraint: "^18.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aPkg := result.Packages["A@1.0.0"]
	if aPkg == nil {
		t.Fatal("A@1.0.0 not in packages")
	}

	// Both peer deps should be in PeerDeps.
	if aPkg.PeerDeps["react"] != "^18.0.0" {
		t.Errorf("PeerDeps[react] = %s, want ^18.0.0", aPkg.PeerDeps["react"])
	}
	if aPkg.PeerDeps["preact"] != "^10.0.0" {
		t.Errorf("PeerDeps[preact] = %s, want ^10.0.0", aPkg.PeerDeps["preact"])
	}

	// PeerDepsMeta should carry through.
	if aPkg.PeerDepsMeta == nil {
		t.Fatal("PeerDepsMeta is nil")
	}
	if !aPkg.PeerDepsMeta["preact"].Optional {
		t.Error("PeerDepsMeta[preact].Optional should be true")
	}
	// react should not appear in PeerDepsMeta (not marked optional).
	if _, hasReact := aPkg.PeerDepsMeta["react"]; hasReact {
		t.Error("react should not be in PeerDepsMeta")
	}
}

func TestBunResolve_BinMetadata(t *testing.T) {
	reg := newMockRegistry()

	reg.addVersionMeta("eslint", "8.50.0", baseTime, &ecosystem.VersionMetadata{
		Bin: map[string]string{
			"eslint": "./bin/eslint.js",
		},
	})

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "eslint", Constraint: "^8.50.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkg := result.Packages["eslint@8.50.0"]
	if pkg == nil {
		t.Fatal("eslint@8.50.0 not in packages")
	}

	if pkg.Bin == nil {
		t.Fatal("Bin is nil")
	}
	if pkg.Bin["eslint"] != "./bin/eslint.js" {
		t.Errorf("Bin[eslint] = %s, want ./bin/eslint.js", pkg.Bin["eslint"])
	}
}

func TestBunResolve_HasInstallScript(t *testing.T) {
	reg := newMockRegistry()

	reg.addVersionMeta("esbuild", "0.19.0", baseTime, &ecosystem.VersionMetadata{
		HasInstallScript: true,
	})

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "esbuild", Constraint: "^0.19.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pkg := result.Packages["esbuild@0.19.0"]
	if pkg == nil {
		t.Fatal("esbuild@0.19.0 not in packages")
	}

	if !pkg.HasInstallScript {
		t.Error("HasInstallScript should be true")
	}
}

func TestBunResolve_CrossTreeDedup(t *testing.T) {
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
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// C should be deduplicated: only one entry in packages.
	cCount := 0
	for key := range result.Packages {
		if key == "C@1.0.0" || key == "C@1.2.0" {
			cCount++
		}
	}
	if cCount != 1 {
		t.Errorf("expected 1 C entry in packages, got %d", cCount)
		for key := range result.Packages {
			t.Logf("  package: %s", key)
		}
	}

	// Should be exactly 3 packages: A, B, C.
	if len(result.Packages) != 3 {
		t.Errorf("expected 3 packages (deduped), got %d", len(result.Packages))
		for key := range result.Packages {
			t.Logf("  package: %s", key)
		}
	}
}

func TestBunResolve_OptionalDepFails(t *testing.T) {
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
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolution should succeed despite missing optional dep, got: %v", err)
	}

	// A and B should be resolved; missing-pkg should not appear.
	if len(result.Packages) != 2 {
		t.Errorf("expected 2 packages (A, B), got %d", len(result.Packages))
		for key := range result.Packages {
			t.Logf("  package: %s", key)
		}
	}

	if _, ok := result.Packages["A@1.0.0"]; !ok {
		t.Error("A@1.0.0 not in packages")
	}
	if _, ok := result.Packages["B@1.0.0"]; !ok {
		t.Error("B@1.0.0 not in packages")
	}
}

func TestBunResolve_CutoffDate(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(30*24*time.Hour), nil)  // Jan 31
	reg.addVersion("A", "1.2.0", baseTime.Add(60*24*time.Hour), nil)  // Mar 1
	reg.addVersion("A", "2.0.0", baseTime.Add(90*24*time.Hour), nil)  // Mar 31

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
	result, err := r.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{
		CutoffDate: &cutoff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := result.Graph.Root.Dependencies[0]
	if edge.Target.Version != "1.1.0" {
		t.Errorf("expected A@1.1.0 with cutoff, got A@%s", edge.Target.Version)
	}

	// Packages map should only contain A@1.1.0.
	if _, ok := result.Packages["A@1.1.0"]; !ok {
		t.Error("A@1.1.0 not in packages")
	}
	if _, ok := result.Packages["A@1.2.0"]; ok {
		t.Error("A@1.2.0 should not be in packages (published after cutoff)")
	}
}
