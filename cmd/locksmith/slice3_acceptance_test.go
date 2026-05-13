package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSlice3_YarnrcAffectsConfig is the slice-3 acceptance proof:
// .yarnrc.yml next to package.json must change locksmith's effective config
// when the target is a yarn-berry format. The fixture sets npmRegistryServer,
// scoped registries with auth, compressionLevel, packageExtensions, and
// supportedArchitectures.
func TestSlice3_YarnrcAffectsConfig(t *testing.T) {
	t.Setenv("ACME_TOKEN", "from-env-token")
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)

	yarnrc := `npmRegistryServer: "https://npm.acme.example/"
npmScopes:
  acme:
    npmRegistryServer: "https://npm.acme.example/scoped/"
    npmAuthToken: "${ACME_TOKEN}"
npmRegistries:
  "//npm.host.example/":
    npmAuthIdent: "user:pass"
compressionLevel: 9
supportedArchitectures:
  os: [linux]
  cpu: [x64, arm64]
  libc: [glibc, musl]
`
	os.WriteFile(filepath.Join(tmp, ".yarnrc.yml"), []byte(yarnrc), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "yarn-berry-v8",
		"--no-user-config",
		"--print-config",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}

	if view["registry"] != "https://npm.acme.example/" {
		t.Errorf("registry = %v, want \"https://npm.acme.example/\"", view["registry"])
	}
	scopes, _ := view["scope_registries"].(map[string]any)
	if scopes["@acme"] != "https://npm.acme.example/scoped/" {
		t.Errorf("scope_registries[@acme] = %v", scopes["@acme"])
	}
	// Two auth entries: scope's bearer + npmRegistries' basic.
	auth, _ := view["auth"].(map[string]any)
	if len(auth) != 2 {
		t.Errorf("auth count = %d, want 2; got: %v", len(auth), auth)
	}
}

// TestSlice3_YarnBerryIgnoresNpmrc verifies that for yarn-berry-* targets,
// a .npmrc next to the spec does NOT affect locksmith. Yarn 4 ignores
// .npmrc; locksmith must match.
func TestSlice3_YarnBerryIgnoresNpmrc(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)
	// Drop a .npmrc that would set a wrong registry; .yarnrc.yml is absent.
	os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte("registry=https://from-npmrc.example/\n"), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "yarn-berry-v8",
		"--no-user-config",
		"--print-config",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view["registry"] != "" {
		t.Errorf("registry = %v, want empty (yarn-berry must ignore .npmrc per ticket #4)", view["registry"])
	}
}
