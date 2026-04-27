package ecosystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// mockRegistry implements Registry for testing the resolution engine.
type mockRegistry struct {
	packages map[string]*mockPackage
	distTags map[string]map[string]string // pkg -> tag -> version
}

type mockPackage struct {
	versions map[string]*VersionMetadata
	times    map[string]time.Time
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		packages: make(map[string]*mockPackage),
		distTags: make(map[string]map[string]string),
	}
}

func (m *mockRegistry) addVersion(name, version string, published time.Time, deps map[string]string) {
	m.addVersionMeta(name, &VersionMetadata{
		Name:         name,
		Version:      version,
		Integrity:    fmt.Sprintf("sha512-fake-%s-%s", name, version),
		TarballURL:   fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version),
		Dependencies: deps,
	}, published)
}

func (m *mockRegistry) addVersionMeta(name string, meta *VersionMetadata, published time.Time) {
	pkg, ok := m.packages[name]
	if !ok {
		pkg = &mockPackage{
			versions: make(map[string]*VersionMetadata),
			times:    make(map[string]time.Time),
		}
		m.packages[name] = pkg
	}
	pkg.versions[meta.Version] = meta
	pkg.times[meta.Version] = published
}

func (m *mockRegistry) addVersionWithPeers(name, version string, published time.Time, deps, peerDeps map[string]string, peerDepsMeta map[string]PeerDepMeta) {
	m.addVersionMeta(name, &VersionMetadata{
		Name:         name,
		Version:      version,
		Integrity:    fmt.Sprintf("sha512-fake-%s-%s", name, version),
		TarballURL:   fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version),
		Dependencies: deps,
		PeerDeps:     peerDeps,
		PeerDepsMeta: peerDepsMeta,
	}, published)
}

func (m *mockRegistry) addVersionWithEngines(name, version string, published time.Time, deps, engines map[string]string) {
	m.addVersionMeta(name, &VersionMetadata{
		Name:         name,
		Version:      version,
		Integrity:    fmt.Sprintf("sha512-fake-%s-%s", name, version),
		TarballURL:   fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version),
		Dependencies: deps,
		Engines:      engines,
	}, published)
}

func (m *mockRegistry) addVersionWithOptional(name, version string, published time.Time, deps, optDeps map[string]string) {
	m.addVersionMeta(name, &VersionMetadata{
		Name:         name,
		Version:      version,
		Integrity:    fmt.Sprintf("sha512-fake-%s-%s", name, version),
		TarballURL:   fmt.Sprintf("https://registry.npmjs.org/%s/-/%s-%s.tgz", name, name, version),
		Dependencies: deps,
		OptionalDeps: optDeps,
	}, published)
}

func (m *mockRegistry) setDistTags(name string, tags map[string]string) {
	m.distTags[name] = tags
}

func (m *mockRegistry) FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]VersionInfo, error) {
	pkg, ok := m.packages[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	var versions []VersionInfo
	for v, t := range pkg.times {
		if cutoff != nil && t.After(*cutoff) {
			continue
		}
		versions = append(versions, VersionInfo{Version: v, PublishedAt: t})
	}
	return versions, nil
}

func (m *mockRegistry) FetchMetadata(ctx context.Context, name, version string) (*VersionMetadata, error) {
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
	if tags, ok := m.distTags[name]; ok {
		return tags, nil
	}
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

var baseTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// --- Basic resolution ---

func TestResolve_SingleDep(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(24*time.Hour), nil)

	project := &ProjectSpec{
		Name:    "test",
		Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	policy := ResolverPolicy{CrossTreeDedup: true, AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(graph.Root.Dependencies) != 1 {
		t.Fatalf("expected 1 dep, got %d", len(graph.Root.Dependencies))
	}
	if graph.Root.Dependencies[0].Target.Version != "1.1.0" {
		t.Errorf("expected A@1.1.0, got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

func TestResolve_TransitiveDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A -> B -> C chain.
	aEdge := graph.Root.Dependencies[0]
	if aEdge.Target.Name != "A" {
		t.Fatalf("expected root dep A, got %s", aEdge.Target.Name)
	}
	if len(aEdge.Target.Dependencies) != 1 || aEdge.Target.Dependencies[0].Target.Name != "B" {
		t.Fatal("A should depend on B")
	}
	bNode := aEdge.Target.Dependencies[0].Target
	if len(bNode.Dependencies) != 1 || bNode.Dependencies[0].Target.Name != "C" {
		t.Fatal("B should depend on C")
	}
}

func TestResolve_DiamondDedup(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	policy := ResolverPolicy{CrossTreeDedup: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both A and B depend on C - with dedup, C should appear once in graph.Nodes.
	if _, ok := graph.Nodes["C@1.0.0"]; !ok {
		t.Fatal("C@1.0.0 should be in graph.Nodes")
	}
	// A's C and B's C should point to the same node.
	aDeps := graph.Root.Dependencies[0].Target.Dependencies
	bDeps := graph.Root.Dependencies[1].Target.Dependencies
	if len(aDeps) == 0 || len(bDeps) == 0 {
		t.Fatal("A and B should both have C as dep")
	}
	if aDeps[0].Target != bDeps[0].Target {
		t.Error("A's C and B's C should be the same node (dedup)")
	}
}

// --- npm alias resolution ---

func TestResolve_NpmAlias(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-positive", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "positive", Constraint: "npm:is-positive@^1.0.0", Type: DepRegular},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	// Edge should preserve the alias name.
	if edge.Name != "positive" {
		t.Errorf("edge.Name = %q, want %q", edge.Name, "positive")
	}
	// Target should resolve to the actual package.
	if edge.Target.Name != "is-positive" {
		t.Errorf("target name = %q, want %q", edge.Target.Name, "is-positive")
	}
	if edge.Target.Version != "1.0.0" {
		t.Errorf("target version = %q, want %q", edge.Target.Version, "1.0.0")
	}
}

func TestResolve_NpmAlias_NoVersion(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-positive", "3.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "pos", Constraint: "npm:is-positive", Type: DepRegular},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Name != "is-positive" {
		t.Errorf("expected is-positive, got %s", graph.Root.Dependencies[0].Target.Name)
	}
}

// --- Overrides ---

func TestResolve_OverrideEdgeConstraint(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
		},
		Overrides: &OverrideSet{
			Rules: []OverrideRule{{Package: "B", Version: "2.0.0"}},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The edge from A to B should have the OVERRIDDEN constraint (2.0.0),
	// not the original constraint from A's metadata (^1.0.0).
	aNode := graph.Root.Dependencies[0].Target
	if len(aNode.Dependencies) == 0 {
		t.Fatal("A should have dep B")
	}
	bEdge := aNode.Dependencies[0]
	if bEdge.Constraint != "2.0.0" {
		t.Errorf("edge constraint = %q, want %q (overridden)", bEdge.Constraint, "2.0.0")
	}
}

func TestResolve_OverrideGlobal(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
		},
		Overrides: &OverrideSet{
			Rules: []OverrideRule{{Package: "B", Version: "2.0.0"}},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B should be overridden to 2.0.0 despite A asking for ^1.0.0.
	bNode := graph.Root.Dependencies[0].Target.Dependencies[0].Target
	if bNode.Version != "2.0.0" {
		t.Errorf("B version = %s, want 2.0.0 (overridden)", bNode.Version)
	}
}

func TestResolve_OverrideScoped(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "2.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular},
		},
		Overrides: &OverrideSet{
			Rules: []OverrideRule{
				// Only override C when it's under A.
				{Package: "A", Children: []OverrideRule{
					{Package: "C", Version: "2.0.0"},
				}},
			},
		},
	}

	// Disable cross-tree dedup so A and B get independent C resolutions.
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A's C should be 2.0.0 (scoped override).
	aNode := graph.Root.Dependencies[0].Target
	if aNode.Name != "A" {
		t.Fatalf("expected first dep to be A, got %s", aNode.Name)
	}
	if len(aNode.Dependencies) == 0 {
		t.Fatal("A should have dependency C")
	}
	aC := aNode.Dependencies[0].Target
	if aC.Version != "2.0.0" {
		t.Errorf("A's C version = %s, want 2.0.0 (scoped override)", aC.Version)
	}
}

// --- Workspace protocol ---

func TestResolve_WorkspaceProtocol(t *testing.T) {
	reg := newMockRegistry()

	memberSpec := &ProjectSpec{Name: "lib-a", Version: "1.0.0"}
	members := []*WorkspaceMember{
		{RelPath: "packages/lib-a", Spec: memberSpec},
	}
	wsIndex := NewWorkspaceIndex(members)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "lib-a", Constraint: "workspace:*", Type: DepRegular},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{WorkspaceIndex: wsIndex}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	if edge.Target.Name != "lib-a" {
		t.Errorf("target name = %q, want %q", edge.Target.Name, "lib-a")
	}
	if edge.Target.WorkspacePath != "packages/lib-a" {
		t.Errorf("WorkspacePath = %q, want %q", edge.Target.WorkspacePath, "packages/lib-a")
	}
}

func TestResolve_WorkspaceProtocol_NotFound(t *testing.T) {
	reg := newMockRegistry()
	wsIndex := NewWorkspaceIndex(nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "nonexistent", Constraint: "workspace:*", Type: DepRegular},
		},
	}

	_, err := Resolve(context.Background(), project, reg, ResolveOptions{WorkspaceIndex: wsIndex}, ResolverPolicy{})
	if err == nil {
		t.Fatal("expected error for missing workspace member")
	}
}

// --- ResolveWorkspaceByName ---

func TestResolve_WorkspaceByName(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("lib-a", "1.0.0", baseTime, nil) // Also in registry.

	memberSpec := &ProjectSpec{Name: "lib-a", Version: "2.0.0"}
	members := []*WorkspaceMember{
		{RelPath: "packages/lib-a", Spec: memberSpec},
	}
	wsIndex := NewWorkspaceIndex(members)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "lib-a", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	policy := ResolverPolicy{ResolveWorkspaceByName: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{WorkspaceIndex: wsIndex}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve to workspace member, not registry.
	edge := graph.Root.Dependencies[0]
	if edge.Target.Version != "2.0.0" {
		t.Errorf("expected workspace version 2.0.0, got %s", edge.Target.Version)
	}
	if edge.Target.WorkspacePath != "packages/lib-a" {
		t.Errorf("expected WorkspacePath, got %q", edge.Target.WorkspacePath)
	}
}

func TestResolve_WorkspaceByName_Disabled(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("lib-a", "1.0.0", baseTime, nil)

	memberSpec := &ProjectSpec{Name: "lib-a", Version: "2.0.0"}
	members := []*WorkspaceMember{
		{RelPath: "packages/lib-a", Spec: memberSpec},
	}
	wsIndex := NewWorkspaceIndex(members)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "lib-a", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	// ResolveWorkspaceByName is false - should use registry.
	policy := ResolverPolicy{ResolveWorkspaceByName: false}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{WorkspaceIndex: wsIndex}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	if edge.Target.Version != "1.0.0" {
		t.Errorf("expected registry version 1.0.0, got %s", edge.Target.Version)
	}
	if edge.Target.WorkspacePath != "" {
		t.Errorf("expected no WorkspacePath, got %q", edge.Target.WorkspacePath)
	}
}

// --- Non-registry specifiers ---

func TestResolve_FileDep(t *testing.T) {
	// Create a temp directory with a local package.json.
	tmpDir := t.TempDir()
	localDir := filepath.Join(tmpDir, "local-pkg")
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "package.json"), []byte(`{"version":"3.2.1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := newMockRegistry()

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "local-pkg", Constraint: "file:./local-pkg", Type: DepRegular},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{SpecDir: tmpDir}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	if edge.Target.Name != "local-pkg" {
		t.Errorf("target name = %q, want %q", edge.Target.Name, "local-pkg")
	}
	if edge.Target.Version != "3.2.1" {
		t.Errorf("version = %q, want %q (from local package.json)", edge.Target.Version, "3.2.1")
	}
	if edge.Target.TarballURL != "file:./local-pkg" {
		t.Errorf("TarballURL = %q, want %q", edge.Target.TarballURL, "file:./local-pkg")
	}
}

func TestResolve_FileDep_NoSpecDir(t *testing.T) {
	reg := newMockRegistry()

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "local-pkg", Constraint: "file:./local-pkg", Type: DepRegular},
		},
	}

	// No specDir - version should be the placeholder.
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Version != "0.0.0-local" {
		t.Errorf("expected placeholder version, got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

// --- Version selection strategies ---

func TestResolve_VersionSelectHighest(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(time.Hour), nil)
	reg.addVersion("A", "1.2.0", baseTime.Add(2*time.Hour), nil)
	// Set "latest" dist-tag to 1.1.0 (not the highest).
	reg.setDistTags("A", map[string]string{"latest": "1.1.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	// VersionSelectHighest ignores dist-tags, picks 1.2.0.
	policy := ResolverPolicy{VersionSelection: VersionSelectHighest}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Version != "1.2.0" {
		t.Errorf("expected 1.2.0 (highest), got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

func TestResolve_VersionSelectPreferLatest(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "1.1.0", baseTime.Add(time.Hour), nil)
	reg.addVersion("A", "1.2.0", baseTime.Add(2*time.Hour), nil)
	// Set "latest" dist-tag to 1.1.0.
	reg.setDistTags("A", map[string]string{"latest": "1.1.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	// PreferLatest picks the dist-tag version when it satisfies the constraint.
	policy := ResolverPolicy{VersionSelection: VersionSelectPreferLatest}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Version != "1.1.0" {
		t.Errorf("expected 1.1.0 (latest dist-tag), got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

// --- Cross-tree dedup ---

func TestResolve_CrossTreeDedup_Enabled(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.1.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	policy := ResolverPolicy{CrossTreeDedup: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A and B both need C - should resolve to same node.
	aC := graph.Root.Dependencies[0].Target.Dependencies[0].Target
	bC := graph.Root.Dependencies[1].Target.Dependencies[0].Target
	if aC != bC {
		t.Error("expected same C node (cross-tree dedup enabled)")
	}
}

func TestResolve_CrossTreeDedup_Disabled(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.0.0", baseTime, nil)
	reg.addVersion("C", "1.1.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	// Without cross-tree dedup, exact-version dedup still happens
	// because both get the same version of C via the nodes map.
	policy := ResolverPolicy{CrossTreeDedup: false}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both should still resolve to a C node (version may vary).
	aC := graph.Root.Dependencies[0].Target.Dependencies[0].Target
	bC := graph.Root.Dependencies[1].Target.Dependencies[0].Target
	if aC.Name != "C" || bC.Name != "C" {
		t.Error("both A and B should depend on C")
	}
}

// --- Cycle detection ---

func TestResolve_CycleDetection(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"A": "^1.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error (cycle should not crash): %v", err)
	}

	// Both A and B should exist in the graph.
	if _, ok := graph.Nodes["A@1.0.0"]; !ok {
		t.Error("A@1.0.0 should be in graph")
	}
	if _, ok := graph.Nodes["B@1.0.0"]; !ok {
		t.Error("B@1.0.0 should be in graph")
	}
}

// --- engines.node filtering ---

func TestResolve_EnginesNode_Compatible(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithEngines("A", "1.0.0", baseTime, nil, map[string]string{"node": ">=16.0.0"})
	reg.addVersionWithEngines("A", "2.0.0", baseTime, nil, map[string]string{"node": ">=18.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: ">=1.0.0", Type: DepRegular}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{NodeVersion: "20.0.0"}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both versions are compatible with Node 20 - should pick highest (2.0.0).
	if graph.Root.Dependencies[0].Target.Version != "2.0.0" {
		t.Errorf("expected 2.0.0, got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

func TestResolve_EnginesNode_Fallback(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithEngines("A", "1.0.0", baseTime, nil, map[string]string{"node": ">=14.0.0"})
	reg.addVersionWithEngines("A", "2.0.0", baseTime, nil, map[string]string{"node": ">=20.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: ">=1.0.0", Type: DepRegular}},
	}

	// Node 16 is compatible with 1.0.0 but not 2.0.0.
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{NodeVersion: "16.0.0"}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Version != "1.0.0" {
		t.Errorf("expected fallback to 1.0.0, got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

func TestResolve_EnginesNode_AllIncompatible(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithEngines("A", "1.0.0", baseTime, nil, map[string]string{"node": ">=20.0.0"})
	reg.addVersionWithEngines("A", "2.0.0", baseTime, nil, map[string]string{"node": ">=22.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: ">=1.0.0", Type: DepRegular}},
	}

	// Node 14 is incompatible with all versions. Advisory: keep original best.
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{NodeVersion: "14.0.0"}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error (engines is advisory): %v", err)
	}

	// Should still resolve (advisory, not a hard failure). Uses highest.
	if graph.Root.Dependencies[0].Target.Version != "2.0.0" {
		t.Errorf("expected 2.0.0 (advisory), got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

// --- AutoInstallPeers ---

func TestResolve_AutoInstallPeers(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"}, nil)
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A has peer dep B - should be auto-installed.
	aNode := graph.Root.Dependencies[0].Target
	found := false
	for _, e := range aNode.Dependencies {
		if e.Name == "B" {
			found = true
			break
		}
	}
	if !found {
		t.Error("peer dep B should be auto-installed on A")
	}
}

func TestResolve_AutoInstallPeers_OptionalSkipped(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"},
		map[string]PeerDepMeta{"B": {Optional: true}})
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B is an optional peer - should NOT be auto-installed.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "B" {
			t.Error("optional peer dep B should NOT be auto-installed")
		}
	}
}

func TestResolve_AutoInstallPeers_ProvidedByProject(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"}, nil)
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: DepRegular}, // Provided by project.
		},
	}

	policy := ResolverPolicy{AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B is already a project dep - should NOT be auto-installed as peer of A.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "B" && e.Type == DepPeer {
			t.Error("peer dep B should not be auto-installed when project provides it")
		}
	}
}

func TestResolve_AutoInstallPeers_Disabled(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"}, nil)
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{AutoInstallPeers: false}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Peers should NOT be auto-installed.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "B" {
			t.Error("peer dep B should not be installed with AutoInstallPeers=false")
		}
	}
}

// --- SkipOptionalPeerDeps (pnpm behavior) ---

func TestResolve_SkipOptionalPeerDeps(t *testing.T) {
	reg := newMockRegistry()
	// A depends on B as both a regular dep and an optional peer dep.
	reg.addVersionMeta("A", &VersionMetadata{
		Name: "A", Version: "1.0.0",
		Integrity:    "sha512-fake-A-1.0.0",
		TarballURL:   "https://registry.npmjs.org/A/-/A-1.0.0.tgz",
		Dependencies: map[string]string{"B": "^1.0.0"},
		PeerDeps:     map[string]string{"B": "^1.0.0"},
		PeerDepsMeta: map[string]PeerDepMeta{"B": {Optional: true}},
	}, baseTime)
	reg.addVersion("B", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{SkipOptionalPeerDeps: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With SkipOptionalPeerDeps, B should NOT appear as a dependency of A.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "B" {
			t.Error("B should be skipped (SkipOptionalPeerDeps: dep is also optional peer)")
		}
	}
}

// --- Optional dep error handling ---

func TestResolve_OptionalDepFailure(t *testing.T) {
	reg := newMockRegistry()
	// "missing-pkg" is not in the registry.

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "missing-pkg", Constraint: "^1.0.0", Type: DepOptional},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("optional dep failure should not error: %v", err)
	}

	// Optional dep should be silently skipped.
	if len(graph.Root.Dependencies) != 0 {
		t.Errorf("expected 0 deps (optional failed), got %d", len(graph.Root.Dependencies))
	}
}

func TestResolve_RequiredDepFailure(t *testing.T) {
	reg := newMockRegistry()

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "missing-pkg", Constraint: "^1.0.0", Type: DepRegular},
		},
	}

	_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err == nil {
		t.Fatal("expected error for missing required dep")
	}
}

// --- OnNodeResolved callback ---

func TestResolve_OnNodeResolved(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)

	var callbackKeys []string

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{
		OnNodeResolved: func(key string, node *Node, meta *VersionMetadata, edges []*Edge) {
			callbackKeys = append(callbackKeys, key)
		},
	}
	_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Callback should fire for both A and B.
	if len(callbackKeys) != 2 {
		t.Fatalf("expected 2 callbacks, got %d: %v", len(callbackKeys), callbackKeys)
	}
}

// --- PackageExtensions ---

func TestResolve_PackageExtensions(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("injected", "2.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
		PackageExtensions: &PackageExtensionSet{
			Extensions: []PackageExtension{
				{
					Name:         "A",
					Dependencies: map[string]string{"injected": "^2.0.0"},
				},
			},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A should have "injected" as a dependency via packageExtensions.
	aNode := graph.Root.Dependencies[0].Target
	found := false
	for _, e := range aNode.Dependencies {
		if e.Name == "injected" {
			found = true
			if e.Target.Version != "2.0.0" {
				t.Errorf("injected version = %s, want 2.0.0", e.Target.Version)
			}
		}
	}
	if !found {
		t.Error("packageExtension should inject 'injected' dep into A")
	}
}

// --- PeerDependencyRules ---

func TestResolve_PeerDepRules_IgnoreMissing(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"missing-peer": "^1.0.0"}, nil)
	// missing-peer is NOT in registry.

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
		PeerDependencyRules: &PeerDependencyRules{
			IgnoreMissing: []string{"missing-*"},
		},
	}

	policy := ResolverPolicy{AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// missing-peer should be skipped (matches ignoreMissing pattern).
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "missing-peer" {
			t.Error("missing-peer should be skipped by ignoreMissing rule")
		}
	}
	_ = graph
}

func TestResolve_PeerDepRules_AllowedVersions(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"}, nil)
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
		PeerDependencyRules: &PeerDependencyRules{
			AllowedVersions: map[string]string{"B": "^2.0.0"},
		},
	}

	policy := ResolverPolicy{AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// B's constraint should be overridden to ^2.0.0 by allowedVersions.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "B" {
			if e.Target.Version != "2.0.0" {
				t.Errorf("B version = %s, want 2.0.0 (allowedVersions override)", e.Target.Version)
			}
			return
		}
	}
	t.Error("B should be auto-installed as peer of A")
}

// --- CutoffDate ---

func TestResolve_CutoffDate(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("A", "2.0.0", baseTime.Add(30*24*time.Hour), nil) // 30 days later.

	cutoff := baseTime.Add(15 * 24 * time.Hour) // 15 days - only 1.0.0 visible.

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: ">=1.0.0", Type: DepRegular}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{CutoffDate: &cutoff}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if graph.Root.Dependencies[0].Target.Version != "1.0.0" {
		t.Errorf("expected 1.0.0 (cutoff), got %s", graph.Root.Dependencies[0].Target.Version)
	}
}

// --- StorePeerMetaOnNode ---

func TestResolve_StorePeerMetaOnNode(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"},
		map[string]PeerDepMeta{"B": {Optional: true}})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{StorePeerMetaOnNode: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aNode := graph.Root.Dependencies[0].Target
	if aNode.PeerDeps == nil || aNode.PeerDeps["B"] != "^1.0.0" {
		t.Error("PeerDeps should be stored on node when StorePeerMetaOnNode is true")
	}
	if aNode.PeerDepsMeta == nil || !aNode.PeerDepsMeta["B"].Optional {
		t.Error("PeerDepsMeta should be stored on node when StorePeerMetaOnNode is true")
	}
}

func TestResolve_StorePeerMetaOnNode_Disabled(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithPeers("A", "1.0.0", baseTime, nil,
		map[string]string{"B": "^1.0.0"},
		map[string]PeerDepMeta{"B": {Optional: true}})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	policy := ResolverPolicy{StorePeerMetaOnNode: false}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aNode := graph.Root.Dependencies[0].Target
	if len(aNode.PeerDeps) > 0 {
		t.Error("PeerDeps should NOT be stored when StorePeerMetaOnNode is false")
	}
}

// --- DevOnly and Optional flags ---

func TestResolve_DevOnlyFlag(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepDev}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !graph.Root.Dependencies[0].Target.DevOnly {
		t.Error("DevOnly should be true for dev dependencies")
	}
}

func TestResolve_OptionalFlag(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepOptional}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !graph.Root.Dependencies[0].Target.Optional {
		t.Error("Optional should be true for optional dependencies")
	}
}

// --- Workspace member deps resolution ---

func TestResolve_WorkspaceMembers(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("shared", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Workspaces: []*WorkspaceMember{
			{
				RelPath: "packages/lib-a",
				Spec: &ProjectSpec{
					Name: "lib-a", Version: "1.0.0",
					Dependencies: []DeclaredDep{
						{Name: "shared", Constraint: "^1.0.0", Type: DepRegular},
					},
				},
			},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Root should have a workspace edge for lib-a.
	if len(graph.Root.Dependencies) != 1 {
		t.Fatalf("expected 1 dep (workspace member), got %d", len(graph.Root.Dependencies))
	}
	wsEdge := graph.Root.Dependencies[0]
	if wsEdge.Target.Name != "lib-a" {
		t.Errorf("expected workspace member lib-a, got %s", wsEdge.Target.Name)
	}
	// lib-a should have resolved its dep on shared.
	if len(wsEdge.Target.Dependencies) != 1 || wsEdge.Target.Dependencies[0].Target.Name != "shared" {
		t.Error("lib-a should depend on shared@1.0.0")
	}
}

// --- Transitive optional dep failure ---

func TestResolve_TransitiveOptionalDepFailure(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersionWithOptional("A", "1.0.0", baseTime, nil, map[string]string{"missing": "^1.0.0"})
	// "missing" is NOT in registry.

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^1.0.0", Type: DepRegular}},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("transitive optional dep failure should not error: %v", err)
	}

	// A should be resolved. Its optional dep "missing" should be silently skipped.
	aNode := graph.Root.Dependencies[0].Target
	for _, e := range aNode.Dependencies {
		if e.Name == "missing" {
			t.Error("missing optional dep should be skipped")
		}
	}
	_ = graph
}

// --- Catalog resolution ---

func TestResolve_CatalogDefault(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-number", "7.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "is-number", Constraint: "catalog:", Type: DepRegular},
		},
		Catalogs: map[string]map[string]string{
			"default": {"is-number": "^7.0.0"},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	if edge.Target.Version != "7.0.0" {
		t.Errorf("expected is-number@7.0.0, got %s", edge.Target.Version)
	}
	// Root edge should keep original catalog: constraint.
	if edge.Constraint != "catalog:" {
		t.Errorf("edge constraint = %q, want %q", edge.Constraint, "catalog:")
	}
}

func TestResolve_CatalogNamed(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("lodash", "3.10.1", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "lodash", Constraint: "catalog:special", Type: DepRegular},
		},
		Catalogs: map[string]map[string]string{
			"special": {"lodash": "^3.10.0"},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := graph.Root.Dependencies[0]
	if edge.Target.Version != "3.10.1" {
		t.Errorf("expected lodash@3.10.1, got %s", edge.Target.Version)
	}
	if edge.Constraint != "catalog:special" {
		t.Errorf("edge constraint = %q, want %q", edge.Constraint, "catalog:special")
	}
}

func TestResolve_CatalogUnknownName(t *testing.T) {
	reg := newMockRegistry()

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "lodash", Constraint: "catalog:nonexistent", Type: DepRegular},
		},
		Catalogs: map[string]map[string]string{
			"default": {"lodash": "^4.0.0"},
		},
	}

	_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err == nil {
		t.Fatal("expected error for unknown catalog name")
	}
}

func TestResolve_CatalogPackageNotFound(t *testing.T) {
	reg := newMockRegistry()

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "unknown-pkg", Constraint: "catalog:", Type: DepRegular},
		},
		Catalogs: map[string]map[string]string{
			"default": {"lodash": "^4.0.0"},
		},
	}

	_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err == nil {
		t.Fatal("expected error for package not found in catalog")
	}
}

func TestResolve_CatalogInWorkspaceMember(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-number", "7.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Workspaces: []*WorkspaceMember{
			{
				RelPath: "packages/lib-a",
				Spec: &ProjectSpec{
					Name: "lib-a", Version: "1.0.0",
					Dependencies: []DeclaredDep{
						{Name: "is-number", Constraint: "catalog:", Type: DepRegular},
					},
				},
			},
		},
		Catalogs: map[string]map[string]string{
			"default": {"is-number": "^7.0.0"},
		},
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Root should have workspace edge for lib-a.
	wsEdge := graph.Root.Dependencies[0]
	if wsEdge.Target.Name != "lib-a" {
		t.Fatalf("expected workspace member lib-a, got %s", wsEdge.Target.Name)
	}
	// lib-a should have resolved is-number via catalog.
	if len(wsEdge.Target.Dependencies) != 1 {
		t.Fatalf("expected 1 dep on lib-a, got %d", len(wsEdge.Target.Dependencies))
	}
	isNumEdge := wsEdge.Target.Dependencies[0]
	if isNumEdge.Target.Version != "7.0.0" {
		t.Errorf("expected is-number@7.0.0, got %s", isNumEdge.Target.Version)
	}
}

// --- No satisfying version ---

func TestResolve_NoSatisfyingVersion(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{{Name: "A", Constraint: "^5.0.0", Type: DepRegular}},
	}

	_, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err == nil {
		t.Fatal("expected error for no satisfying version")
	}
}

// --- PatchedDependencies ---

func TestResolve_PatchedDependencies(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("is-number", "7.0.0", baseTime, nil)
	reg.addVersion("is-odd", "3.0.1", baseTime, map[string]string{"is-number": "^7.0.0"})

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "is-odd", Constraint: "3.0.1", Type: DepRegular},
		},
		PatchedDependencies: map[string]string{
			"is-odd@3.0.1": "patches/is-odd@3.0.1.patch",
		},
	}

	policy := ResolverPolicy{CrossTreeDedup: true, AutoInstallPeers: true}
	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// is-odd@3.0.1 should have Patched=true.
	isOddNode := graph.Root.Dependencies[0].Target
	if isOddNode.Name != "is-odd" {
		t.Fatalf("expected is-odd, got %s", isOddNode.Name)
	}
	if !isOddNode.Patched {
		t.Error("is-odd@3.0.1 should have Patched=true")
	}

	// is-number@7.0.0 should NOT have Patched=true.
	isNumberNode := isOddNode.Dependencies[0].Target
	if isNumberNode.Name != "is-number" {
		t.Fatalf("expected is-number, got %s", isNumberNode.Name)
	}
	if isNumberNode.Patched {
		t.Error("is-number@7.0.0 should NOT have Patched=true")
	}
}

func TestResolve_PatchedDependencies_Nil(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)

	project := &ProjectSpec{
		Name: "test", Version: "1.0.0",
		Dependencies: []DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: DepRegular},
		},
		// PatchedDependencies is nil.
	}

	graph, err := Resolve(context.Background(), project, reg, ResolveOptions{}, ResolverPolicy{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	aNode := graph.Root.Dependencies[0].Target
	if aNode.Patched {
		t.Error("A should NOT have Patched=true when no PatchedDependencies")
	}
}
