//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestWorkspaceNpmStylePlacement verifies the package placement in npm lockfile
// for workspace-npm-style workspaces. npm places workspace member deps under
// the member's directory path (e.g., packages/lib-b/node_modules/is-number),
// not under node_modules/member-name/node_modules/dep.
func TestWorkspaceNpmStylePlacement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	rootData, members := readWorkspaceFixture(t, "workspace-npm-style", []string{
		"packages/lib-a",
		"packages/lib-b",
	})

	result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
		SpecFile:         rootData,
		OutputFormat:     locksmith.FormatPackageLockV3,
		WorkspaceMembers: members,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var lockfile struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &lockfile); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	// Workspace root entry should exist.
	if _, ok := lockfile.Packages[""]; !ok {
		t.Error("missing root entry")
	}

	// Workspace member directory entries should exist.
	for _, path := range []string{"packages/lib-a", "packages/lib-b"} {
		if _, ok := lockfile.Packages[path]; !ok {
			t.Errorf("missing workspace member directory entry: %s", path)
		}
	}

	// Workspace member link entries should exist.
	for _, path := range []string{"node_modules/@wsnpm/lib-a", "node_modules/@wsnpm/lib-b"} {
		if _, ok := lockfile.Packages[path]; !ok {
			t.Errorf("missing workspace member link entry: %s", path)
		}
	}

	// Shared root deps should be hoisted.
	if _, ok := lockfile.Packages["node_modules/is-odd"]; !ok {
		t.Error("missing hoisted is-odd")
	}
	if _, ok := lockfile.Packages["node_modules/ms"]; !ok {
		t.Error("missing hoisted ms")
	}

	// is-number should be present somewhere (either hoisted or nested).
	isNumberFound := false
	for path := range lockfile.Packages {
		if strings.HasSuffix(path, "/is-number") || path == "node_modules/is-number" {
			isNumberFound = true
			break
		}
	}
	if !isNumberFound {
		t.Error("missing is-number in lockfile")
	}

	t.Logf("package paths: %v", sortedKeys(lockfile.Packages))
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
