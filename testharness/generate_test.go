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

// formatTestCase defines a lockfile format and its expected output characteristics.
type formatTestCase struct {
	Format    locksmith.OutputFormat
	FileName  string // expected output filename
	Ecosystem string // for CI matrix grouping
	IsJSON    bool   // whether the output is JSON (vs YAML or other)
}

// formatTestCases covers all 11 lockfile formats.
var formatTestCases = []formatTestCase{
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", true},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", true},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", true},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", true},
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm", false},
	{locksmith.FormatPnpmLockV6, "pnpm-lock.yaml", "pnpm", false},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm", false},
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn", false},
	{locksmith.FormatYarnBerryV6, "yarn.lock", "yarn", false},
	{locksmith.FormatYarnBerryV8, "yarn.lock", "yarn", false},
	{locksmith.FormatBunLock, "bun.lock", "bun", false},
}

// TestGenerate tests that locksmith.Generate succeeds for all formats against all
// fixtures using the real npm registry. This does NOT verify the lockfile is
// accepted by a real package manager - for that, run integration tests.
//
// Test names follow the pattern TestGenerate/{format}/{fixture} for CI matrix
// filtering with -run.
func TestGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry tests in short mode")
	}

	fixtures := fixtureNames(t)

	for _, tc := range formatTestCases {
		tc := tc
		t.Run(string(tc.Format), func(t *testing.T) {
			t.Parallel()
			for _, fixture := range fixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					t.Parallel()
					specData := readFixture(t, fixture)

					ctx := context.Background()
					result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
						SpecFile:     specData,
						OutputFormat: tc.Format,
					})
					if err != nil {
						t.Fatalf("Generate(%s, %s) failed: %v", tc.Format, fixture, err)
					}

					if len(result.Lockfile) == 0 {
						t.Fatal("generated empty lockfile")
					}

					// Format-specific sanity checks.
					validateOutput(t, tc, result.Lockfile)

					t.Logf("generated %d bytes for %s/%s", len(result.Lockfile), tc.Format, fixture)
				})
			}
		})
	}
}

// validateOutput performs basic structural validation on generated lockfile content.
func validateOutput(t *testing.T, tc formatTestCase, data []byte) {
	t.Helper()

	if tc.IsJSON {
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("generated lockfile is not valid JSON: %v\nfirst 500 bytes:\n%s", err, truncate(data, 500))
		}
	}

	content := string(data)

	switch tc.Format {
	case locksmith.FormatPackageLockV1:
		requireJSONField(t, data, "lockfileVersion", float64(1))
	case locksmith.FormatPackageLockV2:
		requireJSONField(t, data, "lockfileVersion", float64(2))
	case locksmith.FormatPackageLockV3:
		requireJSONField(t, data, "lockfileVersion", float64(3))
	case locksmith.FormatNpmShrinkwrap:
		// npm-shrinkwrap.json uses the same structure as package-lock.json.
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err == nil {
			if _, ok := parsed["lockfileVersion"]; !ok {
				t.Error("npm-shrinkwrap.json missing lockfileVersion")
			}
		}
	case locksmith.FormatPnpmLockV5, locksmith.FormatPnpmLockV6, locksmith.FormatPnpmLockV9:
		if !strings.Contains(content, "lockfileVersion") {
			t.Error("pnpm lockfile missing lockfileVersion")
		}
	case locksmith.FormatYarnClassic:
		if !strings.Contains(content, "# yarn lockfile v1") {
			t.Error("yarn classic lockfile missing header comment")
		}
	case locksmith.FormatYarnBerryV6, locksmith.FormatYarnBerryV8:
		if !strings.Contains(content, "__metadata") {
			t.Error("yarn berry lockfile missing __metadata section")
		}
	case locksmith.FormatBunLock:
		// bun.lock is a JSONC-like format; check it's non-trivial.
		if len(data) < 10 {
			t.Error("bun.lock output too small")
		}
	}
}

// requireJSONField checks that a top-level JSON field has the expected value.
func requireJSONField(t *testing.T, data []byte, field string, expected float64) {
	t.Helper()
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return // already reported by the JSON validity check
	}
	val, ok := parsed[field]
	if !ok {
		t.Errorf("missing field %q", field)
		return
	}
	if num, ok := val.(float64); ok && num != expected {
		t.Errorf("%s = %v, want %v", field, num, expected)
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
	if len(names) == 0 {
		t.Fatal("no fixture directories found in fixtures/")
	}
	return names
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("fixtures", name, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func truncate(data []byte, n int) string {
	if len(data) <= n {
		return string(data)
	}
	return string(data[:n]) + "..."
}
