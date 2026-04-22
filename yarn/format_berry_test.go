package yarn

import (
	"strings"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
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
	if !strings.Contains(output, `react: "^18.0.0"`) {
		t.Error("missing react in peerDependencies")
	}
	if !strings.Contains(output, `react-dom: "^18.0.0"`) {
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
