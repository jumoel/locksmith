package pnpmconfig

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
	cfg, err := ParseFile(fixture(t, "full.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	wantPackages := []string{"packages/*", "apps/*"}
	if !reflect.DeepEqual(cfg.Packages, wantPackages) {
		t.Errorf("Packages = %v, want %v", cfg.Packages, wantPackages)
	}

	// Catalog (unnamed) lands in Catalogs["default"].
	if cfg.Catalogs["default"]["react"] != "^18.3.0" {
		t.Errorf("default catalog react = %q, want \"^18.3.0\"", cfg.Catalogs["default"]["react"])
	}
	// Named catalog stays under its own name.
	if cfg.Catalogs["react17"]["react"] != "^17.0.0" {
		t.Errorf("react17 catalog react = %q", cfg.Catalogs["react17"]["react"])
	}

	if cfg.ResolutionMode != "time-based" {
		t.Errorf("ResolutionMode = %q, want \"time-based\"", cfg.ResolutionMode)
	}
	if cfg.AutoInstallPeers == nil || *cfg.AutoInstallPeers != false {
		t.Errorf("AutoInstallPeers = %v, want *false (explicit setting)", cfg.AutoInstallPeers)
	}
	if cfg.StrictPeerDependencies == nil || *cfg.StrictPeerDependencies != true {
		t.Errorf("StrictPeerDependencies = %v, want *true", cfg.StrictPeerDependencies)
	}
	if cfg.DedupePeerDependents == nil || *cfg.DedupePeerDependents != false {
		t.Errorf("DedupePeerDependents = %v, want *false", cfg.DedupePeerDependents)
	}

	if cfg.CatalogMode != "prefer" {
		t.Errorf("CatalogMode = %q, want \"prefer\"", cfg.CatalogMode)
	}

	if cfg.MinimumReleaseAge != 86400 {
		t.Errorf("MinimumReleaseAge = %d, want 86400", cfg.MinimumReleaseAge)
	}
	wantExcludes := []string{"@types/node", "typescript"}
	if !reflect.DeepEqual(cfg.MinimumReleaseAgeExclude, wantExcludes) {
		t.Errorf("MinimumReleaseAgeExclude = %v, want %v", cfg.MinimumReleaseAgeExclude, wantExcludes)
	}

	if !reflect.DeepEqual(cfg.SupportedArchitectures.OS, []string{"linux", "darwin"}) {
		t.Errorf("OS = %v", cfg.SupportedArchitectures.OS)
	}
	if !reflect.DeepEqual(cfg.SupportedArchitectures.CPU, []string{"x64", "arm64"}) {
		t.Errorf("CPU = %v", cfg.SupportedArchitectures.CPU)
	}
	if !reflect.DeepEqual(cfg.SupportedArchitectures.Libc, []string{"glibc", "musl"}) {
		t.Errorf("Libc = %v", cfg.SupportedArchitectures.Libc)
	}

	// packageExtensions and overrides are kept as raw JSON-shaped values so
	// the existing npm.ParsePackageExtensions / npm.ParseNpmOverrides paths
	// can consume them.
	if cfg.PackageExtensions == nil {
		t.Error("PackageExtensions = nil, want non-nil")
	}
	if cfg.Overrides == nil {
		t.Error("Overrides = nil, want non-nil")
	}
}

func TestParse_Minimal(t *testing.T) {
	cfg, err := ParseFile(fixture(t, "minimal.yaml"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(cfg.Packages) != 1 || cfg.Packages[0] != "packages/*" {
		t.Errorf("Packages = %v, want [packages/*]", cfg.Packages)
	}
	// Every other field zero / nil.
	if cfg.ResolutionMode != "" {
		t.Errorf("ResolutionMode = %q, want \"\"", cfg.ResolutionMode)
	}
	if cfg.MinimumReleaseAge != 0 {
		t.Errorf("MinimumReleaseAge = %d, want 0", cfg.MinimumReleaseAge)
	}
	if cfg.AutoInstallPeers != nil {
		t.Errorf("AutoInstallPeers = %v, want nil (unset)", cfg.AutoInstallPeers)
	}
}

func TestParse_MissingFile(t *testing.T) {
	_, err := ParseFile("/nonexistent.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
