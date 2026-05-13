package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSlice5_BunfigAffectsConfig: bunfig.toml next to package.json affects
// the bun-lock generator's config. Fixture sets registry, scope, embedded
// credential, and minimumReleaseAge.
func TestSlice5_BunfigAffectsConfig(t *testing.T) {
	t.Setenv("ACME_TOKEN", "from-env-bun")
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)

	bunfig := `[install]
registry = "https://npm.bun.example/"
minimumReleaseAge = 3600

[install.scopes]
acme = { url = "https://npm.acme.example/", token = "$ACME_TOKEN" }
`
	os.WriteFile(filepath.Join(tmp, "bunfig.toml"), []byte(bunfig), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "bun-lock",
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

	if view["registry"] != "https://npm.bun.example/" {
		t.Errorf("registry = %v, want \"https://npm.bun.example/\"", view["registry"])
	}
	scopes, _ := view["scope_registries"].(map[string]any)
	if scopes["@acme"] != "https://npm.acme.example/" {
		t.Errorf("scope_registries[@acme] = %v", scopes["@acme"])
	}
	auth, _ := view["auth"].(map[string]any)
	if len(auth) < 1 {
		t.Errorf("expected at least one credential from embedded token, got: %v", auth)
	}
	if view["cutoff"] == "" {
		t.Errorf("cutoff should be set from minimumReleaseAge=3600")
	}
}

// TestSlice5_BunfigWinsOverNpmrc: per ticket #23, when both files set the
// same setting, bunfig.toml wins.
func TestSlice5_BunfigWinsOverNpmrc(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte("registry=https://from-npmrc.example/\n"), 0o644)
	os.WriteFile(filepath.Join(tmp, "bunfig.toml"), []byte(
		"[install]\nregistry = \"https://from-bunfig.example/\"\n",
	), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "bun-lock",
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
	if view["registry"] != "https://from-bunfig.example/" {
		t.Errorf("registry = %v, want bunfig value (bunfig wins over .npmrc per ticket #23)", view["registry"])
	}
}

// TestSlice5_NpmrcStillReadForBun: bun reads BOTH files; the .npmrc-only
// settings (no bunfig override) must still apply.
func TestSlice5_NpmrcStillReadForBun(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte(
		"@only-in-npmrc:registry=https://only-npmrc.example/\n",
	), 0o644)
	os.WriteFile(filepath.Join(tmp, "bunfig.toml"), []byte(
		"[install]\nregistry = \"https://from-bunfig.example/\"\n",
	), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "bun-lock",
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
	scopes, _ := view["scope_registries"].(map[string]any)
	if scopes["@only-in-npmrc"] != "https://only-npmrc.example/" {
		t.Errorf("@only-in-npmrc scope = %v, want \"https://only-npmrc.example/\" (npmrc-only setting must survive)", scopes["@only-in-npmrc"])
	}
}
