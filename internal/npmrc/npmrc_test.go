package npmrc

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fixturePath returns the path to a checked-in fixture under testdata/.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestParse_AzureArtifacts(t *testing.T) {
	cfg, err := ParseFile(fixturePath(t, "azure-artifacts.npmrc"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	wantRegistry := "https://pkgs.dev.azure.com/myorg/myproject/_packaging/myfeed/npm/registry/"
	if cfg.Registry != wantRegistry {
		t.Errorf("Registry = %q, want %q", cfg.Registry, wantRegistry)
	}
	if cfg.Defaults["always-auth"] != "true" {
		t.Errorf("Defaults[always-auth] = %q, want \"true\"", cfg.Defaults["always-auth"])
	}

	// Two registry URLs configured, each with username, _password, email.
	wantHost := "https://pkgs.dev.azure.com/myorg/myproject/_packaging/myfeed/npm/registry"
	host := cfg.HostConfig[wantHost]
	if host == nil {
		t.Fatalf("HostConfig missing key %q. Got keys: %v", wantHost, hostKeys(cfg))
	}
	if host["username"] != "anyValue" {
		t.Errorf("username = %q, want \"anyValue\"", host["username"])
	}
	if host["_password"] != "REDACTED-BASE64" {
		t.Errorf("_password = %q, want \"REDACTED-BASE64\"", host["_password"])
	}
	// Email values can contain colons and spaces; verify the parser doesn't truncate.
	wantEmail := "npm requires email to be set but doesn't use the value"
	if host["email"] != wantEmail {
		t.Errorf("email = %q, want %q", host["email"], wantEmail)
	}
}

func TestParse_Artifactory(t *testing.T) {
	cfg, err := ParseFile(fixturePath(t, "artifactory.npmrc"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	wantScope := "https://example.jfrog.io/artifactory/api/npm/npm-local/"
	if cfg.ScopeRegistries["@myorg"] != wantScope {
		t.Errorf("ScopeRegistries[@myorg] = %q, want %q", cfg.ScopeRegistries["@myorg"], wantScope)
	}

	wantHost := "https://example.jfrog.io/artifactory/api/npm/npm-local"
	host := cfg.HostConfig[wantHost]
	if host == nil {
		t.Fatalf("HostConfig missing key %q. Got keys: %v", wantHost, hostKeys(cfg))
	}
	if host["_auth"] != "REDACTED-BASE64-USERPASS" {
		t.Errorf("_auth = %q, want \"REDACTED-BASE64-USERPASS\"", host["_auth"])
	}
	if host["always-auth"] != "true" {
		t.Errorf("always-auth = %q, want \"true\"", host["always-auth"])
	}
}

func TestParse_GithubPackages_EnvInterpolation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_FixtureToken")

	cfg, err := ParseFile(fixturePath(t, "github-packages.npmrc"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if cfg.ScopeRegistries["@myorg"] != "https://npm.pkg.github.com" {
		t.Errorf("scope registry = %q, want %q", cfg.ScopeRegistries["@myorg"], "https://npm.pkg.github.com")
	}
	host := cfg.HostConfig["https://npm.pkg.github.com"]
	if host == nil {
		t.Fatalf("HostConfig missing key. Got keys: %v", hostKeys(cfg))
	}
	if host["_authToken"] != "ghp_FixtureToken" {
		t.Errorf("_authToken = %q, want \"ghp_FixtureToken\" (env interpolation should have happened)", host["_authToken"])
	}
}

func TestParse_Complex(t *testing.T) {
	t.Setenv("COMPANY_NPM_TOKEN", "company-token-from-env")

	cfg, err := ParseFile(fixturePath(t, "complex.npmrc"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Top-level scalars (commented lines should NOT pollute these).
	wantDefaults := map[string]string{
		"strict-ssl":       "false",
		"cafile":           "/etc/ssl/internal-ca.pem",
		"before":           "2025-01-01",
		"legacy-peer-deps": "true",
		"engine-strict":    "true",
		"node-version":     "20.0.0", // Quoted in fixture; quotes stripped.
	}
	for k, want := range wantDefaults {
		if cfg.Defaults[k] != want {
			t.Errorf("Defaults[%q] = %q, want %q", k, cfg.Defaults[k], want)
		}
	}

	// Registry + scope registries.
	if cfg.Registry != "https://registry.npmjs.org/" {
		t.Errorf("Registry = %q, want \"https://registry.npmjs.org/\"", cfg.Registry)
	}
	if cfg.ScopeRegistries["@company"] != "https://npm.company.com/" {
		t.Errorf("@company registry = %q", cfg.ScopeRegistries["@company"])
	}
	if cfg.ScopeRegistries["@private"] != "https://npm.private.example/path/" {
		t.Errorf("@private registry = %q", cfg.ScopeRegistries["@private"])
	}

	// Env interpolation happened.
	companyHost := cfg.HostConfig["https://npm.company.com"]
	if companyHost == nil {
		t.Fatalf("HostConfig missing company host. Got keys: %v", hostKeys(cfg))
	}
	if companyHost["_authToken"] != "company-token-from-env" {
		t.Errorf("company _authToken = %q, want env-expanded", companyHost["_authToken"])
	}

	// Array syntax accumulated both entries.
	wantOmit := []string{"dev", "optional"}
	if !reflect.DeepEqual(cfg.Arrays["omit"], wantOmit) {
		t.Errorf("Arrays[omit] = %v, want %v", cfg.Arrays["omit"], wantOmit)
	}
}

// TestParse_MissingFile verifies the parser surfaces a clear error rather
// than returning a zero-valued config.
func TestParse_MissingFile(t *testing.T) {
	_, err := ParseFile(filepath.Join(t.TempDir(), "does-not-exist.npmrc"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

// TestParse_Empty: empty file produces empty Config without erroring.
func TestParse_Empty(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".npmrc")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	cfg, err := ParseFile(p)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if cfg == nil {
		t.Fatal("Config should not be nil")
	}
	if cfg.Registry != "" || len(cfg.ScopeRegistries) != 0 || len(cfg.HostConfig) != 0 {
		t.Errorf("expected zero-valued Config, got %+v", cfg)
	}
}

// TestParse_UnsetEnvVarBecomesEmpty: per ticket #6, unset env vars resolve to "".
func TestParse_UnsetEnvVarBecomesEmpty(t *testing.T) {
	// GITHUB_TOKEN intentionally unset.
	os.Unsetenv("GITHUB_TOKEN")

	cfg, err := ParseFile(fixturePath(t, "github-packages.npmrc"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	host := cfg.HostConfig["https://npm.pkg.github.com"]
	if host == nil {
		t.Fatalf("HostConfig missing key. Got keys: %v", hostKeys(cfg))
	}
	if host["_authToken"] != "" {
		t.Errorf("_authToken = %q, want \"\" (unset env var should expand to empty per ticket #6)", host["_authToken"])
	}
}

func hostKeys(cfg *Config) []string {
	keys := make([]string, 0, len(cfg.HostConfig))
	for k := range cfg.HostConfig {
		keys = append(keys, k)
	}
	return keys
}
