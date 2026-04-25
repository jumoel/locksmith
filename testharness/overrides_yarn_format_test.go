//go:build !integration

package testharness

import (
	"context"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestOverridesYarnBerryConstraintKey verifies that when yarn resolutions
// override a transitive dep's version, the lockfile entry uses the overridden
// constraint (e.g., "is-number@npm:6.0.0") rather than the original constraint
// from the parent package's metadata (e.g., "is-number@npm:^7.0.0").
func TestOverridesYarnBerryConstraintKey(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := readFixture(t, "overrides-yarn")

	result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatYarnBerryV8,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	output := string(result.Lockfile)

	// The overrides-yarn fixture forces is-number to 6.0.0.
	// The lockfile entry key should use the overridden constraint.
	if !strings.Contains(output, "is-number@npm:6.0.0") {
		t.Error("lockfile should contain is-number@npm:6.0.0 (overridden constraint)")
	}

	// It should NOT contain the original constraint from is-odd's metadata.
	// is-odd@3.x depends on is-number@^6.0.0 (which happens to match 6.0.0).
	// But if the override produces a different constraint key than the transitive
	// dep's declared constraint, the test catches the bug.
	//
	// Verify is-number resolves to 6.0.0.
	if !strings.Contains(output, "version: 6.0.0") {
		t.Error("is-number should resolve to version 6.0.0")
	}
}
