package bunfig

import (
	"path/filepath"
	"reflect"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestParse_Full(t *testing.T) {
	t.Setenv("ACME_TOKEN", "from-env-bearer")
	cfg, err := ParseFile(fixture(t, "full.toml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	if cfg.Install.Registry.URL != "https://registry.npmjs.org" {
		t.Errorf("registry URL = %q", cfg.Install.Registry.URL)
	}
	if cfg.Install.MinimumReleaseAge != 86400 {
		t.Errorf("minimumReleaseAge = %d", cfg.Install.MinimumReleaseAge)
	}
	if !reflect.DeepEqual(cfg.Install.MinimumReleaseAgeExcludes, []string{"@types/bun"}) {
		t.Errorf("excludes = %v", cfg.Install.MinimumReleaseAgeExcludes)
	}
	if cfg.Install.CaFile != "testdata/ca.pem" {
		t.Errorf("cafile = %q", cfg.Install.CaFile)
	}

	// Scopes: string form, object-with-token form (env-expanded), object-with-userpass form.
	if cfg.Install.Scopes["acme"].URL != "https://npm.acme.example/" {
		t.Errorf("acme scope URL = %q", cfg.Install.Scopes["acme"].URL)
	}
	if cfg.Install.Scopes["private"].Token != "from-env-bearer" {
		t.Errorf("private scope token = %q (env expansion?)", cfg.Install.Scopes["private"].Token)
	}
	if cfg.Install.Scopes["basicScope"].Username != "user" {
		t.Errorf("basicScope username = %q", cfg.Install.Scopes["basicScope"].Username)
	}
	if cfg.Install.Scopes["basicScope"].Password != "pass" {
		t.Errorf("basicScope password = %q", cfg.Install.Scopes["basicScope"].Password)
	}

	// Boolean install-type filters parsed (per ticket #15, surfaced but not honored).
	if cfg.Install.Dev != nil && *cfg.Install.Dev != false {
		t.Errorf("install.dev = %v, want *false", cfg.Install.Dev)
	}
	if cfg.Install.Production != nil && *cfg.Install.Production != false {
		t.Errorf("install.production = %v", cfg.Install.Production)
	}
}

func TestParse_RegistryObject(t *testing.T) {
	cfg, err := ParseFile(fixture(t, "registry-object.toml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if cfg.Install.Registry.URL != "https://registry.npmjs.org/" {
		t.Errorf("URL = %q", cfg.Install.Registry.URL)
	}
	if cfg.Install.Registry.Token != "embedded-bearer" {
		t.Errorf("Token = %q, want \"embedded-bearer\"", cfg.Install.Registry.Token)
	}
}

func TestParse_MissingFile(t *testing.T) {
	_, err := ParseFile("/nonexistent.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
