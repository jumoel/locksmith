//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestGenerate_AllFixtures_PackageLockV3 tests that locksmith.Generate() succeeds
// for all fixtures against the real npm registry. This does NOT verify the lockfile
// is accepted by a package manager - for that, run integration tests.
func TestGenerate_AllFixtures_PackageLockV3(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	for _, fixture := range fixtureNames(t) {
		t.Run(fixture, func(t *testing.T) {
			specData, err := os.ReadFile(filepath.Join("fixtures", fixture, "package.json"))
			if err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: locksmith.FormatPackageLockV3,
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			// Verify valid JSON.
			var parsed map[string]interface{}
			if err := json.Unmarshal(result.Lockfile, &parsed); err != nil {
				t.Fatalf("invalid JSON: %v\nlockfile:\n%s", err, string(result.Lockfile))
			}

			// Basic structure checks.
			if parsed["lockfileVersion"].(float64) != 3 {
				t.Error("lockfileVersion should be 3")
			}
			packages, ok := parsed["packages"].(map[string]interface{})
			if !ok {
				t.Fatal("packages field missing or wrong type")
			}
			if _, ok := packages[""]; !ok {
				t.Error("root entry missing")
			}
			// Should have at least 1 non-root package.
			if len(packages) < 2 {
				t.Errorf("expected at least 2 packages (root + deps), got %d", len(packages))
			}

			t.Logf("generated %d bytes, %d packages", len(result.Lockfile), len(packages))
		})
	}
}

func TestGenerate_AllFixtures_PnpmLockV9(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	for _, fixture := range fixtureNames(t) {
		t.Run(fixture, func(t *testing.T) {
			specData, err := os.ReadFile(filepath.Join("fixtures", fixture, "package.json"))
			if err != nil {
				t.Fatal(err)
			}

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: locksmith.FormatPnpmLockV9,
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			// Basic YAML structure check - should contain key sections.
			lockfileStr := string(result.Lockfile)
			if !strings.Contains(lockfileStr, "lockfileVersion") {
				t.Error("missing lockfileVersion in output")
			}
			if !strings.Contains(lockfileStr, "importers") {
				t.Error("missing importers in output")
			}
			if !strings.Contains(lockfileStr, "packages") {
				t.Error("missing packages in output")
			}

			t.Logf("generated %d bytes", len(result.Lockfile))
		})
	}
}

func fixtureNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}
