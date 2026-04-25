//go:build !integration

package testharness

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestPnpmNonRegistryDepsGenerate exercises the full locksmith pipeline for the
// non-registry-deps fixture with pnpm-lock v9 format. This uses the real npm
// registry and the fixture's local-pkg directory. It verifies that non-registry
// deps (file:, github:, tarball URL) produce valid lockfile entries.
func TestPnpmNonRegistryDepsGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	fixtureDir := filepath.Join("fixtures", "non-registry-deps")
	specData := readFixture(t, "non-registry-deps")

	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPnpmLockV9,
		SpecDir:      fixtureDir,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	output := string(result.Lockfile)
	if len(output) == 0 {
		t.Fatal("empty lockfile")
	}

	// Verify lockfileVersion header.
	if !strings.Contains(output, "lockfileVersion:") {
		t.Error("missing lockfileVersion header")
	}

	// Verify ms (regular npm dep) is present in packages.
	if !strings.Contains(output, "ms@") {
		t.Error("missing ms entry (regular registry dep)")
	}

	// Verify file: dep (local-pkg) is present.
	if !strings.Contains(output, "local-pkg") {
		t.Error("missing local-pkg reference (file: dep)")
	}

	// Verify tarball URL dep is present (resolves to is-odd via parseTarballURL).
	if !strings.Contains(output, "is-odd") && !strings.Contains(output, "tarball-pkg") {
		t.Error("missing tarball dep reference")
	}

	// Verify git dep reference.
	if !strings.Contains(output, "git-pkg") && !strings.Contains(output, "github:") {
		t.Error("missing git dep reference")
	}

	t.Logf("generated %d bytes of pnpm-lock.yaml v9", len(output))
}
