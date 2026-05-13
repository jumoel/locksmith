package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSlice4_YarnClassicWalkUp verifies the yarn-classic walk-up algorithm:
// .npmrc files in ancestor directories are merged with innermost winning
// on key collision. Per ticket #27.
//
// Layout used by the test:
//
//	root/
//	  .npmrc       (registry=outer, @lib:registry=outer-lib)
//	  inner/
//	    .npmrc     (registry=inner)             (innermost; should win)
//	    package.json
func TestSlice4_YarnClassicWalkUp(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".npmrc"), []byte(
		"registry=https://outer.example/\n@lib:registry=https://outer-lib.example/\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".npmrc"), []byte(
		"registry=https://inner.example/\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	specFile := filepath.Join(inner, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)

	cmd := rootCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "yarn-classic",
		"--no-user-config",
		"--print-config",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	// Innermost .npmrc should win for registry.
	if view["registry"] != "https://inner.example/" {
		t.Errorf("registry = %v, want \"https://inner.example/\" (innermost-wins per ticket #27)", view["registry"])
	}
	// Outer-only scope registry should still be present.
	scopes, _ := view["scope_registries"].(map[string]any)
	if scopes["@lib"] != "https://outer-lib.example/" {
		t.Errorf("scope_registries[@lib] = %v, want \"https://outer-lib.example/\" (outer-only setting must survive merge)", scopes["@lib"])
	}
}
