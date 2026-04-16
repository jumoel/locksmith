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

// TestBerryConstraintDedup verifies that redundant caret constraints are
// deduplicated. When ^2.4.0 and ^2.8.0 both resolve to 2.8.1, yarn keeps
// only the most specific one (^2.8.0).
func TestBerryConstraintDedup(t *testing.T) {
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

	// Both v6 AND v8 should deduplicate: keep ^2.8.0, drop ^2.4.0.
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
		if strings.Contains(out, "tslib@npm:^2.4.0") {
			t.Errorf("%s should deduplicate ^2.4.0 (redundant with ^2.8.0), got:\n%s", name, out)
		}
		if !strings.Contains(out, "tslib@npm:^2.8.0") {
			t.Errorf("%s should keep ^2.8.0 constraint, got:\n%s", name, out)
		}
	}
}
