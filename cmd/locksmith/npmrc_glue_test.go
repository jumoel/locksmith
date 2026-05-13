package main

import (
	"path/filepath"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/registryurl"
)

// TestLoadNpmrcOptions_ProjectAndUser exercises the merge: project values
// win over user values. The fixtures are checked-in real-shape .npmrc files
// (Artifactory-like project rc + personal-token user rc).
func TestLoadNpmrcOptions_ProjectAndUser(t *testing.T) {
	projectRc := filepath.Join("testdata", "npmrc-fixture", ".npmrc")
	userRc := filepath.Join("testdata", "npmrc-fixture", "user.npmrc")

	opts, err := loadNpmrcOptions(projectRc, userRc)
	if err != nil {
		t.Fatalf("loadNpmrcOptions: %v", err)
	}

	// Project sets registry; project wins over user.
	if opts.Registry != "https://npm.company.com/" {
		t.Errorf("Registry = %q, want %q (project should win over user)", opts.Registry, "https://npm.company.com/")
	}

	// @company scope only in project.
	if opts.ScopeRegistries["@company"] != "https://npm.company.com/scoped/" {
		t.Errorf("@company scope = %q", opts.ScopeRegistries["@company"])
	}
	// @personal scope only in user; should still appear (no project conflict).
	if opts.ScopeRegistries["@personal"] != "https://npm.user.example/" {
		t.Errorf("@personal scope = %q (user-only scope must survive merge)", opts.ScopeRegistries["@personal"])
	}

	// Auth credentials: project has bearer token for npm.company.com and basic for scoped path; user has bearer for npmjs.org.
	wantCompany := registryurl.Normalize("//npm.company.com/")
	companyCred, ok := opts.AuthCredentials[wantCompany]
	if !ok {
		t.Errorf("missing credential for %q. Got keys: %v", wantCompany, mapKeys(opts.AuthCredentials))
	} else {
		bearer, ok := companyCred.(ecosystem.BearerCredential)
		if !ok || bearer.Token != "REDACTED-bearer-token" {
			t.Errorf("company credential = %#v, want BearerCredential{REDACTED-bearer-token}", companyCred)
		}
	}

	wantScoped := registryurl.Normalize("//npm.company.com/scoped/")
	scopedCred, ok := opts.AuthCredentials[wantScoped]
	if !ok {
		t.Errorf("missing credential for %q", wantScoped)
	} else {
		basic, ok := scopedCred.(ecosystem.BasicCredential)
		if !ok {
			t.Errorf("scoped credential type = %T, want BasicCredential", scopedCred)
		} else if basic.Username != "foo" || basic.Password != "bar" {
			// base64("foo:bar") == "Zm9vOmJhcg==" was in the fixture.
			t.Errorf("scoped basic = %+v, want {foo bar}", basic)
		}
	}

	wantNpmjs := registryurl.Normalize("//registry.npmjs.org/")
	npmjsCred, ok := opts.AuthCredentials[wantNpmjs]
	if !ok {
		t.Errorf("user-level npmjs credential missing (user-only credential must survive merge)")
	} else if bearer, ok := npmjsCred.(ecosystem.BearerCredential); !ok || bearer.Token != "npm-personal-token" {
		t.Errorf("npmjs credential = %#v", npmjsCred)
	}

	// TLS: project sets cafile.
	if opts.TLSOptions == nil || len(opts.TLSOptions.RootCAs) != 1 {
		t.Errorf("TLSOptions = %+v, want one RootCA loaded from cafile", opts.TLSOptions)
	}

	// Policy fields from project.
	if !opts.LegacyPeerDeps {
		t.Errorf("LegacyPeerDeps = false, want true (project sets legacy-peer-deps=true)")
	}
	if !opts.EngineStrict {
		t.Errorf("EngineStrict = false, want true (project sets engine-strict=true)")
	}

	// Cutoff date.
	if opts.CutoffDate == nil {
		t.Errorf("CutoffDate is nil, want 2025-06-01")
	} else {
		got := opts.CutoffDate.Format("2006-01-02")
		if got != "2025-06-01" {
			t.Errorf("CutoffDate = %q, want 2025-06-01", got)
		}
	}

	// Formatter knobs.
	if !opts.MinifyPackageLock {
		t.Errorf("MinifyPackageLock = false, want true (project sets format-package-lock=false)")
	}
	if !opts.OmitLockfileRegistryResolved {
		t.Errorf("OmitLockfileRegistryResolved = false, want true")
	}
}

// TestLoadNpmrcOptions_ProjectOnly: user path empty should skip user file.
func TestLoadNpmrcOptions_ProjectOnly(t *testing.T) {
	projectRc := filepath.Join("testdata", "npmrc-fixture", ".npmrc")
	opts, err := loadNpmrcOptions(projectRc, "")
	if err != nil {
		t.Fatalf("loadNpmrcOptions: %v", err)
	}
	// @personal is user-only; with user skipped it must NOT appear.
	if _, ok := opts.ScopeRegistries["@personal"]; ok {
		t.Errorf("@personal scope leaked from user file when user path was empty")
	}
}

// TestLoadNpmrcOptions_MissingFile: nonexistent path should not error - it
// silently degrades to "no config found", matching what every PM does when
// a config file is absent.
func TestLoadNpmrcOptions_MissingFile(t *testing.T) {
	opts, err := loadNpmrcOptions("/nonexistent/path/.npmrc", "")
	if err != nil {
		t.Fatalf("loadNpmrcOptions on missing file: expected no error, got %v", err)
	}
	if opts == nil {
		t.Fatalf("opts is nil")
	}
	if opts.Registry != "" || len(opts.ScopeRegistries) != 0 || len(opts.AuthCredentials) != 0 {
		t.Errorf("expected zero-valued opts for missing file, got %+v", opts)
	}
}

func mapKeys(m map[string]ecosystem.Credential) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
