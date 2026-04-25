//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestNpmNonRegistryDepsGenerate exercises the full locksmith pipeline for the
// non-registry-deps fixture with package-lock v3 format. This uses the real npm
// registry and the fixture's local-pkg directory. It verifies that non-registry
// deps (file:, github:, tarball URL) produce valid lockfile entries.
func TestNpmNonRegistryDepsGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	fixtureDir := filepath.Join("fixtures", "non-registry-deps")
	specData := readFixture(t, "non-registry-deps")

	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPackageLockV3,
		SpecDir:      fixtureDir,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	output := string(result.Lockfile)
	if len(output) == 0 {
		t.Fatal("empty lockfile")
	}

	// Parse the lockfile JSON.
	var lockfile struct {
		Packages map[string]struct {
			Version  string `json:"version"`
			Resolved string `json:"resolved"`
			Link     bool   `json:"link"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &lockfile); err != nil {
		t.Fatalf("parsing lockfile JSON: %v\nfirst 500 bytes:\n%s", err, truncate(result.Lockfile, 500))
	}

	// Verify ms (regular npm dep) is present.
	found := false
	for path, pkg := range lockfile.Packages {
		if path == "node_modules/ms" {
			found = true
			if pkg.Version == "" {
				t.Error("ms should have a version")
			}
			break
		}
	}
	if !found {
		t.Error("missing ms entry in packages (regular registry dep)")
	}

	// Verify file: dep (local-pkg) is present with link: true.
	if pkg, ok := lockfile.Packages["node_modules/local-pkg"]; ok {
		if !pkg.Link {
			t.Error("local-pkg should have link: true")
		}
	} else {
		t.Error("missing local-pkg entry in packages (file: dep)")
	}

	// Verify tarball URL dep is present.
	tarballFound := false
	for path, pkg := range lockfile.Packages {
		if path == "node_modules/tarball-pkg" || path == "node_modules/is-odd" {
			if pkg.Version != "" {
				tarballFound = true
				break
			}
		}
	}
	if !tarballFound {
		t.Error("missing tarball dep entry in packages")
	}

	// Verify git dep is present (github: shorthand).
	gitFound := false
	for path := range lockfile.Packages {
		if path == "node_modules/git-pkg" || path == "node_modules/is-odd" {
			gitFound = true
			break
		}
	}
	if !gitFound {
		t.Error("missing git dep entry in packages")
	}

	t.Logf("generated %d bytes of package-lock.json v3", len(output))
}
