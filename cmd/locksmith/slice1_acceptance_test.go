package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSlice1_NpmrcAffectsConfig is the slice-1 acceptance proof per ticket #20:
// a project-level .npmrc next to package.json must change locksmith's effective
// configuration. Without honoring the file, the divergence locksmith exists to
// prevent reappears.
//
// The fixture sets a private registry, two credential types, a CA file, legacy
// peer deps, engine-strict, and a cutoff date. The test invokes locksmith with
// --print-config and asserts every setting flows through.
func TestSlice1_NpmrcAffectsConfig(t *testing.T) {
	tmp := t.TempDir()

	// Project-level files: package.json + .npmrc.
	specFile := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specFile, []byte(`{"name":"test","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	caFile := filepath.Join(tmp, "ca.pem")
	if err := os.WriteFile(caFile, []byte("-----BEGIN CERTIFICATE-----\nMIIBhTCCASs=\n-----END CERTIFICATE-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	npmrc := `registry=https://npm.acme.example/
@acme:registry=https://npm.acme.example/scoped/

//npm.acme.example/:_authToken=ACME_BEARER
//npm.acme.example/scoped/:_auth=Zm9vOmJhcg==

cafile=` + caFile + `
legacy-peer-deps=true
engine-strict=true
before=2025-06-01
format-package-lock=false
omit-lockfile-registry-resolved=true
`
	if err := os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte(npmrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Invoke CLI with --print-config and --no-user-config (so the test isn't
	// affected by the developer's actual ~/.npmrc).
	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "package-lock-v3",
		"--no-user-config",
		"--print-config",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("generate --print-config: %v\nstdout: %s", err, out.String())
	}

	// Parse the JSON.
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, out.String())
	}

	// Registry from rc.
	if view["registry"] != "https://npm.acme.example/" {
		t.Errorf("registry = %v, want \"https://npm.acme.example/\" (from .npmrc)", view["registry"])
	}
	// Scope registry from rc.
	scopes, _ := view["scope_registries"].(map[string]any)
	if scopes["@acme"] != "https://npm.acme.example/scoped/" {
		t.Errorf("scope_registries[@acme] = %v, want \"https://npm.acme.example/scoped/\"", scopes["@acme"])
	}
	// Two credentials present, both redacted.
	auth, _ := view["auth"].(map[string]any)
	if len(auth) != 2 {
		t.Errorf("auth has %d entries, want 2; got: %v", len(auth), auth)
	}
	// Verify at least one bearer and one basic shape, redacted.
	var sawBearer, sawBasic bool
	for _, entry := range auth {
		m, _ := entry.(map[string]any)
		switch m["type"] {
		case "bearer":
			sawBearer = true
			if m["value"] != "***" {
				t.Errorf("bearer value not redacted: %v", m["value"])
			}
		case "basic":
			sawBasic = true
			if m["username"] != "***" || m["password"] != "***" {
				t.Errorf("basic credential not redacted: %v", m)
			}
		}
	}
	if !sawBearer || !sawBasic {
		t.Errorf("expected one bearer + one basic credential, got: %v", auth)
	}

	// TLS: one root CA from cafile.
	tls, _ := view["tls"].(map[string]any)
	if n, _ := tls["num_roots"].(float64); n != 1 {
		t.Errorf("tls.num_roots = %v, want 1", tls["num_roots"])
	}

	// Policy fields from rc.
	policy, _ := view["policy"].(map[string]any)
	if policy["legacy_peer_deps"] != true {
		t.Errorf("legacy_peer_deps = %v, want true", policy["legacy_peer_deps"])
	}
	if policy["engine_strict"] != true {
		t.Errorf("engine_strict = %v, want true", policy["engine_strict"])
	}

	// Format knobs.
	format, _ := view["format"].(map[string]any)
	if format["minify_package_lock"] != true {
		t.Errorf("minify_package_lock = %v, want true", format["minify_package_lock"])
	}
	if format["omit_lockfile_registry_resolved"] != true {
		t.Errorf("omit_lockfile_registry_resolved = %v, want true", format["omit_lockfile_registry_resolved"])
	}

	// Cutoff date.
	cutoff, _ := view["cutoff"].(string)
	if !strings.HasPrefix(cutoff, "2025-06-01") {
		t.Errorf("cutoff = %q, want a 2025-06-01 RFC3339 string", cutoff)
	}
}

// TestSlice1_CLIOverridesNpmrc verifies that --registry on the CLI wins over
// the registry= value in .npmrc, per ticket #18.
func TestSlice1_CLIOverridesNpmrc(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte("registry=https://from-rc.example/\n"), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "package-lock-v3",
		"--no-user-config",
		"--registry", "https://from-cli.example/",
		"--print-config",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if view["registry"] != "https://from-cli.example/" {
		t.Errorf("registry = %v, want CLI-flag value (CLI must beat .npmrc per ticket #18)", view["registry"])
	}
}

// TestSlice1_NoProjectConfig_DisablesRC verifies the --no-project-config flag
// skips reading the project .npmrc even when one is present.
func TestSlice1_NoProjectConfig_DisablesRC(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	os.WriteFile(specFile, []byte(`{"name":"t","version":"1.0.0"}`), 0o644)
	os.WriteFile(filepath.Join(tmp, ".npmrc"), []byte("registry=https://from-rc.example/\n"), 0o644)

	root := rootCmd()
	out := new(bytes.Buffer)
	root.SetOut(out)
	root.SetErr(new(bytes.Buffer))
	root.SetArgs([]string{
		"generate",
		"--spec", specFile,
		"--format", "package-lock-v3",
		"--no-project-config",
		"--no-user-config",
		"--print-config",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	var view map[string]any
	if err := json.Unmarshal(out.Bytes(), &view); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if view["registry"] != "" {
		t.Errorf("registry = %v, want empty (--no-project-config should have skipped the file)", view["registry"])
	}
}
