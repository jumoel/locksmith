//go:build !integration

package testharness

import (
	"context"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestBerryPeerDepsMetaRealRegistry generates a yarn berry lockfile for a
// project with mixed optional/required peer deps and verifies only the
// optional ones appear in peerDependenciesMeta.
func TestBerryPeerDepsMetaRealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := []byte(`{
		"name": "locksmith-test-peer-meta",
		"version": "1.0.0",
		"dependencies": {
			"react-dom": "^18.0.0"
		},
		"peerDependencies": {
			"react": "^18.0.0"
		},
		"peerDependenciesMeta": {
			"react": {
				"optional": true
			}
		}
	}`)

	for _, format := range []locksmith.OutputFormat{
		locksmith.FormatYarnBerryV6,
		locksmith.FormatYarnBerryV8,
	} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: format,
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			output := string(result.Lockfile)

			// The workspace root entry should have peerDependencies with react.
			if !strings.Contains(output, "peerDependencies:") {
				t.Fatal("expected peerDependencies section in lockfile")
			}

			// peerDependenciesMeta should be present and contain react (optional).
			if !strings.Contains(output, "peerDependenciesMeta:") {
				t.Fatal("expected peerDependenciesMeta section in lockfile")
			}
			if !strings.Contains(output, "react:\n      optional: true") {
				t.Error("react should be marked optional in peerDependenciesMeta")
			}

			// react-dom is NOT a peer dep, it's a regular dep - should not appear
			// in peerDependenciesMeta.
			// Count "optional: true" occurrences - should be exactly 1.
			count := strings.Count(output, "optional: true")
			if count != 1 {
				t.Errorf("expected exactly 1 'optional: true' in output, got %d.\nOutput:\n%s", count, output)
			}
		})
	}
}
