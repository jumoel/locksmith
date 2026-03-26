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
