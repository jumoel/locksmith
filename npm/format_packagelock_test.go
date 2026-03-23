package npm

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
)

func TestPackageLockV3_SimpleProject(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, nil)
	reg.addVersion("B", "2.3.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "my-app",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "B", Constraint: "^2.0.0", Type: ecosystem.DepDev},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	formatter := NewPackageLockV3Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	// Must be valid JSON.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, data)
	}

	// Check top-level fields.
	if raw["name"] != "my-app" {
		t.Errorf("name = %v, want my-app", raw["name"])
	}
	if raw["version"] != "1.0.0" {
		t.Errorf("version = %v, want 1.0.0", raw["version"])
	}
	if raw["lockfileVersion"].(float64) != 3 {
		t.Errorf("lockfileVersion = %v, want 3", raw["lockfileVersion"])
	}
	if raw["requires"] != true {
		t.Errorf("requires = %v, want true", raw["requires"])
	}

	packages, ok := raw["packages"].(map[string]interface{})
	if !ok {
		t.Fatalf("packages is not an object: %T", raw["packages"])
	}

	// Root entry.
	rootEntry, ok := packages[""].(map[string]interface{})
	if !ok {
		t.Fatalf("root entry not found or not an object")
	}
	if rootEntry["name"] != "my-app" {
		t.Errorf("root name = %v, want my-app", rootEntry["name"])
	}

	rootDeps, ok := rootEntry["dependencies"].(map[string]interface{})
	if !ok {
		t.Fatalf("root dependencies not found")
	}
	if rootDeps["A"] != "^1.0.0" {
		t.Errorf("root dep A = %v, want ^1.0.0", rootDeps["A"])
	}

	rootDevDeps, ok := rootEntry["devDependencies"].(map[string]interface{})
	if !ok {
		t.Fatalf("root devDependencies not found")
	}
	if rootDevDeps["B"] != "^2.0.0" {
		t.Errorf("root devDep B = %v, want ^2.0.0", rootDevDeps["B"])
	}

	// Package A entry.
	aEntry, ok := packages["node_modules/A"].(map[string]interface{})
	if !ok {
		t.Fatalf("node_modules/A not found")
	}
	if aEntry["version"] != "1.0.0" {
		t.Errorf("A version = %v, want 1.0.0", aEntry["version"])
	}
	if aEntry["resolved"] != "https://registry.npmjs.org/A/-/A-1.0.0.tgz" {
		t.Errorf("A resolved = %v", aEntry["resolved"])
	}
	if aEntry["integrity"] != "sha512-fake-A-1.0.0" {
		t.Errorf("A integrity = %v", aEntry["integrity"])
	}

	// Package B entry - should have dev: true.
	bEntry, ok := packages["node_modules/B"].(map[string]interface{})
	if !ok {
		t.Fatalf("node_modules/B not found")
	}
	if bEntry["version"] != "2.3.0" {
		t.Errorf("B version = %v, want 2.3.0", bEntry["version"])
	}
	if bEntry["dev"] != true {
		t.Errorf("B dev = %v, want true", bEntry["dev"])
	}

	t.Logf("Output:\n%s", data)
}

func TestPackageLockV3_DeterministicOutput(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("alpha", "1.0.0", baseTime, nil)
	reg.addVersion("beta", "2.0.0", baseTime, nil)
	reg.addVersion("gamma", "3.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "determinism-test",
		Version: "0.1.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "gamma", Constraint: "^3.0.0", Type: ecosystem.DepRegular},
			{Name: "alpha", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "beta", Constraint: "^2.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	formatter := NewPackageLockV3Formatter()

	var outputs [][]byte
	for i := 0; i < 10; i++ {
		result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
		if err != nil {
			t.Fatalf("resolve failed on iteration %d: %v", i, err)
		}

		data, err := formatter.FormatFromResult(result, project)
		if err != nil {
			t.Fatalf("format failed on iteration %d: %v", i, err)
		}
		outputs = append(outputs, data)
	}

	for i := 1; i < len(outputs); i++ {
		if !bytes.Equal(outputs[0], outputs[i]) {
			t.Errorf("output %d differs from output 0:\n--- run 0 ---\n%s\n--- run %d ---\n%s",
				i, outputs[0], i, outputs[i])
		}
	}
}

func TestPackageLockV3_NestedDeps(t *testing.T) {
	reg := newMockRegistry()
	// A depends on C@^1.0.0, B depends on C@^2.0.0 - forces nesting.
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, map[string]string{"C": "^2.0.0"})
	reg.addVersion("C", "1.5.0", baseTime, nil)
	reg.addVersion("C", "2.3.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "nested-test",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "B", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	r := NewResolver()
	result, err := r.ResolveWithPlacement(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	formatter := NewPackageLockV3Formatter()
	data, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	packages := raw["packages"].(map[string]interface{})

	// A should be hoisted.
	if _, ok := packages["node_modules/A"]; !ok {
		t.Error("node_modules/A not found")
	}

	// B should be hoisted.
	if _, ok := packages["node_modules/B"]; !ok {
		t.Error("node_modules/B not found")
	}

	// C@1.5.0 should be hoisted (resolved first via A).
	rootC, ok := packages["node_modules/C"].(map[string]interface{})
	if !ok {
		t.Fatal("node_modules/C not found")
	}
	if rootC["version"] != "1.5.0" {
		t.Errorf("root C version = %v, want 1.5.0", rootC["version"])
	}

	// C@2.3.0 should be nested under B.
	nestedC, ok := packages["node_modules/B/node_modules/C"].(map[string]interface{})
	if !ok {
		t.Fatal("node_modules/B/node_modules/C not found")
	}
	if nestedC["version"] != "2.3.0" {
		t.Errorf("nested C version = %v, want 2.3.0", nestedC["version"])
	}

	// A's entry should have a dependencies field with C constraint.
	aEntry := packages["node_modules/A"].(map[string]interface{})
	aDeps, ok := aEntry["dependencies"].(map[string]interface{})
	if !ok {
		t.Fatal("A should have dependencies field")
	}
	if aDeps["C"] != "^1.0.0" {
		t.Errorf("A dep C constraint = %v, want ^1.0.0", aDeps["C"])
	}

	// B's entry should have a dependencies field with C constraint.
	bEntry := packages["node_modules/B"].(map[string]interface{})
	bDeps, ok := bEntry["dependencies"].(map[string]interface{})
	if !ok {
		t.Fatal("B should have dependencies field")
	}
	if bDeps["C"] != "^2.0.0" {
		t.Errorf("B dep C constraint = %v, want ^2.0.0", bDeps["C"])
	}

	// Verify total package count: root + A + B + C@root + C@nested = 5 entries.
	if len(packages) != 5 {
		t.Errorf("expected 5 package entries, got %d", len(packages))
		for k := range packages {
			t.Logf("  %q", k)
		}
	}

	t.Logf("Output:\n%s", data)
}
