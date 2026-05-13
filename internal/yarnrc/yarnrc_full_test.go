package yarnrc

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
	t.Setenv("COMPANY_NPM_TOKEN", "company-bearer-from-env")

	cfg, err := Parse(fixture(t, "full.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if cfg.NpmRegistryServer != "https://registry.yarnpkg.com" {
		t.Errorf("NpmRegistryServer = %q", cfg.NpmRegistryServer)
	}

	// Scope: company has registry + bearer (env-expanded).
	if cfg.NpmScopes["company"].NpmRegistryServer != "https://npm.company.com" {
		t.Errorf("company scope registry = %q", cfg.NpmScopes["company"].NpmRegistryServer)
	}
	if cfg.NpmScopes["company"].NpmAuthToken != "company-bearer-from-env" {
		t.Errorf("company scope token = %q (env expansion?)", cfg.NpmScopes["company"].NpmAuthToken)
	}
	if !cfg.NpmScopes["company"].NpmAlwaysAuth {
		t.Errorf("company scope alwaysAuth = false, want true")
	}

	// Scope: private has registry + ident.
	if cfg.NpmScopes["private"].NpmAuthIdent != "user:pass-cleartext" {
		t.Errorf("private scope ident = %q", cfg.NpmScopes["private"].NpmAuthIdent)
	}

	// Host-keyed registry config.
	host := cfg.NpmRegistries["//npm.private.example/path"]
	if host.NpmAuthToken != "host-keyed-token" {
		t.Errorf("host-keyed token = %q", host.NpmAuthToken)
	}

	if cfg.CompressionLevel != "mixed" {
		t.Errorf("CompressionLevel = %q, want \"mixed\"", cfg.CompressionLevel)
	}
	if cfg.DefaultProtocol != "npm:" {
		t.Errorf("DefaultProtocol = %q", cfg.DefaultProtocol)
	}

	wantOS := []string{"linux", "darwin"}
	if !reflect.DeepEqual(cfg.SupportedArchitectures.OS, wantOS) {
		t.Errorf("SupportedArchitectures.OS = %v", cfg.SupportedArchitectures.OS)
	}
	wantLibc := []string{"glibc"}
	if !reflect.DeepEqual(cfg.SupportedArchitectures.Libc, wantLibc) {
		t.Errorf("Libc = %v", cfg.SupportedArchitectures.Libc)
	}

	if cfg.PackageExtensions == nil {
		t.Error("PackageExtensions = nil, want non-nil")
	}

	if cfg.HttpsCaFilePath != "testdata/ca.pem" {
		t.Errorf("HttpsCaFilePath = %q", cfg.HttpsCaFilePath)
	}
}

func TestParse_Minimal(t *testing.T) {
	cfg, err := Parse(fixture(t, "minimal.yml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.NpmRegistryServer != "" {
		t.Errorf("NpmRegistryServer = %q, want \"\"", cfg.NpmRegistryServer)
	}
	if len(cfg.NpmScopes) != 0 {
		t.Errorf("NpmScopes should be empty, got %v", cfg.NpmScopes)
	}
}

// TestReadCompressionLevel_StillWorks pins the existing slice 0 entry point.
func TestReadCompressionLevel_StillWorks(t *testing.T) {
	got, err := ReadCompressionLevel(fixture(t, "full.yml"))
	if err != nil {
		t.Fatalf("ReadCompressionLevel: %v", err)
	}
	if got != "mixed" {
		t.Errorf("got %q, want \"mixed\"", got)
	}
}
