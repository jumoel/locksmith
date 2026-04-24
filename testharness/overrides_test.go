//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

func TestOverridesGenerate_Npm(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	specData := readFixture(t, "overrides-npm")
	opts := locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPackageLockV3,
	}

	result, err := locksmith.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Parse the lockfile and verify is-number is exactly 6.0.0.
	var lockfile struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &lockfile); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	found := false
	for path, pkg := range lockfile.Packages {
		if strings.HasSuffix(path, "/is-number") || path == "node_modules/is-number" {
			found = true
			if pkg.Version != "6.0.0" {
				t.Errorf("is-number version = %s, want 6.0.0 (overridden)", pkg.Version)
			}
		}
	}
	if !found {
		t.Error("is-number not found in lockfile packages")
	}
}

func TestOverridesGenerate_Yarn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	specData := readFixture(t, "overrides-yarn")
	opts := locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatYarnBerryV8,
	}

	result, err := locksmith.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// yarn berry lockfile is YAML-like. Check that is-number resolves to 6.0.0.
	content := string(result.Lockfile)
	if !strings.Contains(content, "is-number") {
		t.Error("is-number not found in yarn lockfile")
	}

	// Look for "version: 6.0.0" near "is-number" entries.
	// This is a rough check - the yarn lockfile format groups entries.
	lines := strings.Split(content, "\n")
	inIsNumber := false
	foundCorrectVersion := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "is-number") && !strings.HasPrefix(trimmed, "#") {
			inIsNumber = true
		}
		if inIsNumber && strings.Contains(trimmed, "version:") {
			if strings.Contains(trimmed, "6.0.0") {
				foundCorrectVersion = true
			}
			inIsNumber = false
		}
	}

	if !foundCorrectVersion {
		t.Error("is-number version 6.0.0 not found in yarn lockfile (override not applied)")
	}
}
