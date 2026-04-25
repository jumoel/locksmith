//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jumoel/locksmith"
)

// TestCutoffDate_RestrictsVersions verifies that CutoffDate restricts
// resolution to versions published before the cutoff.
//
// ms@2.1.1 was published 2017-11-30.
// ms@2.1.2 was published 2019-06-06.
// Using a cutoff between them should yield 2.1.1 instead of 2.1.2+.
func TestCutoffDate_RestrictsVersions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := []byte(`{"name": "cutoff-test", "version": "1.0.0", "dependencies": {"ms": "^2.1.0"}}`)

	// Cutoff: Jan 1, 2019 - after 2.1.1 but before 2.1.2.
	cutoff := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)

	result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPackageLockV3,
		CutoffDate:   &cutoff,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var lockfile struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &lockfile); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	pkg, ok := lockfile.Packages["node_modules/ms"]
	if !ok {
		t.Fatal("ms not found in lockfile packages")
	}
	if pkg.Version != "2.1.1" {
		t.Errorf("ms version = %s, want 2.1.1 (cutoff should exclude 2.1.2+)", pkg.Version)
	}
}

// TestCutoffDate_WithoutCutoff verifies that without a cutoff, the latest
// version is resolved.
func TestCutoffDate_WithoutCutoff(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := []byte(`{"name": "cutoff-test", "version": "1.0.0", "dependencies": {"ms": "^2.1.0"}}`)

	result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPackageLockV3,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var lockfile struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &lockfile); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	pkg, ok := lockfile.Packages["node_modules/ms"]
	if !ok {
		t.Fatal("ms not found in lockfile packages")
	}
	// Without cutoff, should resolve to 2.1.3 (latest in ^2.1.0 range).
	if pkg.Version == "2.1.1" {
		t.Error("without cutoff, should resolve to version newer than 2.1.1")
	}
}
