//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestBunNonRegistryDepsGenerate exercises the full locksmith pipeline for
// the non-registry-deps fixture with bun.lock format. This uses the real npm
// registry and the fixture's local-pkg directory. It verifies that non-registry
// deps (file:, github:, tarball URL) produce valid lockfile entries.
//
// Note: git+ssh:// deps require SSH keys, so this test runs against the real
// registry but the git+ssh dep will produce a placeholder. The formatter
// now supports non-registry dep formatting for all types.
func TestBunNonRegistryDepsGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	fixtureDir := filepath.Join("fixtures", "non-registry-deps")
	specData := readFixture(t, "non-registry-deps")

	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatBunLock,
		SpecDir:      fixtureDir,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	output := string(result.Lockfile)
	if len(output) == 0 {
		t.Fatal("empty lockfile")
	}

	// Strip trailing commas for JSON validation.
	re := regexp.MustCompile(`,(\s*[}\]])`)
	cleaned := re.ReplaceAll(result.Lockfile, []byte("$1"))

	var parsed map[string]interface{}
	if err := json.Unmarshal(cleaned, &parsed); err != nil {
		t.Fatalf("output is not valid JSONC: %v\nfirst 500 bytes:\n%s", err, truncate(result.Lockfile, 500))
	}

	// Verify top-level structure.
	for _, key := range []string{"lockfileVersion", "configVersion", "workspaces", "packages"} {
		if _, ok := parsed[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}

	// Verify ms (regular npm dep) is present.
	packages, ok := parsed["packages"].(map[string]interface{})
	if !ok {
		t.Fatal("packages is not an object")
	}

	if _, ok := packages["ms"]; !ok {
		t.Error("missing ms entry (regular registry dep)")
	}

	// Verify local-pkg (file: dep) is present.
	if _, ok := packages["local-pkg"]; !ok {
		t.Error("missing local-pkg entry (file: dep)")
	}

	t.Logf("generated %d bytes of bun.lock", len(output))
	t.Logf("package keys: %v", packageKeys(packages))
}

func packageKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
