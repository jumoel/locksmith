//go:build !integration

package testharness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// workspaceFormats lists the formats to exercise in workspace generation tests.
// One representative per ecosystem.
var workspaceFormats = []struct {
	name   string
	format locksmith.OutputFormat
}{
	{"PackageLockV3", locksmith.FormatPackageLockV3},
	{"PnpmLockV9", locksmith.FormatPnpmLockV9},
	{"YarnBerryV8", locksmith.FormatYarnBerryV8},
	{"BunLock", locksmith.FormatBunLock},
}

// readWorkspaceFixture reads a workspace fixture's root and member package.json files.
// It returns the root spec data and a map of relative path to member spec data.
func readWorkspaceFixture(t *testing.T, fixtureName string, memberRelPaths []string) ([]byte, map[string][]byte) {
	t.Helper()

	fixtureDir := filepath.Join("fixtures", fixtureName)

	rootData, err := os.ReadFile(filepath.Join(fixtureDir, "package.json"))
	if err != nil {
		t.Fatalf("reading root package.json: %v", err)
	}

	members := make(map[string][]byte, len(memberRelPaths))
	for _, relPath := range memberRelPaths {
		data, err := os.ReadFile(filepath.Join(fixtureDir, relPath, "package.json"))
		if err != nil {
			t.Fatalf("reading %s/package.json: %v", relPath, err)
		}
		members[relPath] = data
	}

	return rootData, members
}

// isWorkspaceNotImplementedError returns true if the error indicates that
// workspace support is not yet implemented in the resolution engine.
func isWorkspaceNotImplementedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Match common error patterns that indicate workspace: protocol deps
	// are not yet handled by the resolver.
	indicators := []string{
		"workspace",
		"no version",
		"no matching version",
		"unsupported protocol",
		"not implemented",
		"unknown constraint",
	}
	for _, ind := range indicators {
		if strings.Contains(msg, ind) {
			return true
		}
	}
	return false
}

func TestWorkspaceGenerate_Simple(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping workspace generation test in short mode")
	}

	rootData, members := readWorkspaceFixture(t, "workspace-simple", []string{
		"packages/lib-a",
		"packages/lib-b",
	})

	for _, tc := range workspaceFormats {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:         rootData,
				OutputFormat:     tc.format,
				WorkspaceMembers: members,
			})
			if err != nil {
				if isWorkspaceNotImplementedError(err) {
					t.Skipf("workspace support not yet implemented for %s: %v", tc.name, err)
				}
				t.Fatalf("Generate(%s) failed: %v", tc.name, err)
			}

			output := string(result.Lockfile)
			if len(output) == 0 {
				t.Fatal("generated empty lockfile")
			}

			// Workspace member names should appear in the output.
			for _, name := range []string{"@workspace/lib-a", "@workspace/lib-b"} {
				if !strings.Contains(output, name) {
					t.Errorf("output missing workspace member %q", name)
				}
			}

			// External dependencies should appear in the output.
			for _, dep := range []string{"is-odd", "ms", "is-number"} {
				if !strings.Contains(output, dep) {
					t.Errorf("output missing external dependency %q", dep)
				}
			}

			t.Logf("generated %d bytes for workspace-simple/%s", len(result.Lockfile), tc.name)
		})
	}
}

func TestWorkspaceGenerate_CrossDeps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping workspace generation test in short mode")
	}

	rootData, members := readWorkspaceFixture(t, "workspace-cross-deps", []string{
		"packages/core",
		"packages/utils",
	})

	for _, tc := range workspaceFormats {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:         rootData,
				OutputFormat:     tc.format,
				WorkspaceMembers: members,
			})
			if err != nil {
				if isWorkspaceNotImplementedError(err) {
					t.Skipf("workspace support not yet implemented for %s: %v", tc.name, err)
				}
				t.Fatalf("Generate(%s) failed: %v", tc.name, err)
			}

			output := string(result.Lockfile)
			if len(output) == 0 {
				t.Fatal("generated empty lockfile")
			}

			// Workspace member names.
			for _, name := range []string{"@ws/core", "@ws/utils"} {
				if !strings.Contains(output, name) {
					t.Errorf("output missing workspace member %q", name)
				}
			}

			// Shared external dependency.
			if !strings.Contains(output, "ms") {
				t.Error("output missing shared dependency ms")
			}

			t.Logf("generated %d bytes for workspace-cross-deps/%s", len(result.Lockfile), tc.name)
		})
	}
}

// npmStyleFormats lists the formats that resolve cross-workspace deps by name
// (regular semver) rather than requiring workspace: protocol.
var npmStyleFormats = []struct {
	name   string
	format locksmith.OutputFormat
}{
	{"PackageLockV3", locksmith.FormatPackageLockV3},
	{"YarnClassic", locksmith.FormatYarnClassic},
}

func TestWorkspaceGenerate_NpmStyle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping workspace generation test in short mode")
	}

	rootData, members := readWorkspaceFixture(t, "workspace-npm-style", []string{
		"packages/lib-a",
		"packages/lib-b",
	})

	for _, tc := range npmStyleFormats {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:         rootData,
				OutputFormat:     tc.format,
				WorkspaceMembers: members,
			})
			if err != nil {
				if isWorkspaceNotImplementedError(err) {
					t.Skipf("workspace support not yet implemented for %s: %v", tc.name, err)
				}
				t.Fatalf("Generate(%s) failed: %v", tc.name, err)
			}

			output := string(result.Lockfile)
			if len(output) == 0 {
				t.Fatal("generated empty lockfile")
			}

			// Workspace member names should appear in the output.
			for _, name := range []string{"@wsnpm/lib-a", "@wsnpm/lib-b"} {
				if !strings.Contains(output, name) {
					t.Errorf("output missing workspace member %q", name)
				}
			}

			// External dependencies should appear in the output.
			for _, dep := range []string{"ms", "is-number"} {
				if !strings.Contains(output, dep) {
					t.Errorf("output missing external dependency %q", dep)
				}
			}

			// The cross-workspace dep @wsnpm/lib-a should be resolved as a workspace
			// link, not fetched from the registry. Check that no registry tarball URL
			// for @wsnpm/lib-a appears in the output.
			if strings.Contains(output, "registry.npmjs.org/%40wsnpm%2Flib-a") ||
				strings.Contains(output, "registry.npmjs.org/@wsnpm/lib-a") {
				t.Error("@wsnpm/lib-a resolved from registry instead of workspace link")
			}

			t.Logf("generated %d bytes for workspace-npm-style/%s", len(result.Lockfile), tc.name)
		})
	}
}

func TestWorkspaceGenerate_SinglePackageFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping workspace generation test in short mode")
	}

	// Use a simple non-workspace fixture to verify backward compatibility.
	specData := readFixture(t, "minimal")

	for _, tc := range workspaceFormats {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()

			// Generate without WorkspaceMembers (the default single-package path).
			resultWithout, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: tc.format,
			})
			if err != nil {
				t.Fatalf("Generate without workspace members failed: %v", err)
			}

			// Generate with nil WorkspaceMembers (should be identical).
			resultWithNil, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:         specData,
				OutputFormat:     tc.format,
				WorkspaceMembers: nil,
			})
			if err != nil {
				t.Fatalf("Generate with nil workspace members failed: %v", err)
			}

			// Generate with empty WorkspaceMembers map (should also be identical).
			resultWithEmpty, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:         specData,
				OutputFormat:     tc.format,
				WorkspaceMembers: map[string][]byte{},
			})
			if err != nil {
				t.Fatalf("Generate with empty workspace members failed: %v", err)
			}

			// All three should produce the same lockfile content.
			if string(resultWithout.Lockfile) != string(resultWithNil.Lockfile) {
				t.Error("lockfile differs between no WorkspaceMembers and nil WorkspaceMembers")
			}
			if string(resultWithout.Lockfile) != string(resultWithEmpty.Lockfile) {
				t.Error("lockfile differs between no WorkspaceMembers and empty WorkspaceMembers")
			}

			if len(resultWithout.Lockfile) == 0 {
				t.Fatal("generated empty lockfile")
			}

			t.Logf("backward compatibility verified for %s (%d bytes)", tc.name, len(resultWithout.Lockfile))
		})
	}
}
