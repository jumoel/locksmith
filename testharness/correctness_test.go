//go:build integration

package testharness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// correctnessCase defines a locksmith format paired with the real package manager
// command that produces the equivalent lockfile for comparison.
type correctnessCase struct {
	Format       locksmith.OutputFormat
	LockFileName string // lockfile name locksmith writes
	PMLabel      string // e.g., "npm@10"
	// GenCommand is the Docker command to generate a lockfile from scratch.
	// It runs in a directory containing only package.json.
	GenCommand []string
	// RealLockFileName is the lockfile name the real PM produces (may differ).
	RealLockFileName string
	SetupFunc        func(t *testing.T, dir string)
}

// correctnessMatrix defines all format/pm pairs for resolution comparison.
var correctnessMatrix = []correctnessCase{
	// npm: compare our package-lock-v3 against npm 7-11 output
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@7", []string{"run-npm", "7", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@8", []string{"run-npm", "8", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@9", []string{"run-npm", "9", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@10", []string{"run-npm", "10", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@11", []string{"run-npm", "11", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil},

	// npm shrinkwrap: compare against npm 6 output
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@6", []string{"run-npm", "6", "shrinkwrap"}, "npm-shrinkwrap.json", nil},

	// pnpm: compare against each pnpm version's native output
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm@7", []string{"run-pnpm", "7", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil},
	{locksmith.FormatPnpmLockV6, "pnpm-lock.yaml", "pnpm@8", []string{"run-pnpm", "8", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm@9", []string{"run-pnpm", "9", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm@10", []string{"run-pnpm", "10", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil},

	// yarn classic: compare against yarn 1 output
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn@1", []string{"run-yarn", "1", "install", "--ignore-scripts"}, "yarn.lock", nil},

	// bun: compare against bun output
	{locksmith.FormatBunLock, "bun.lock", "bun", []string{"bun", "install", "--save-text-lockfile"}, "bun.lock", nil},
}

// TestCorrectness generates lockfiles with BOTH locksmith and the real package
// manager, then compares the resolved package versions. This catches resolution
// differences where our lockfile might be accepted but resolve to different
// versions than the real tool.
//
// Test names: TestCorrectness/{pm_label}/{fixture}
// CI filter: go test -tags integration -run "TestCorrectness/npm" ./testharness/
func TestCorrectness(t *testing.T) {
	allFixtures := fixtures(t)

	// Use a subset of small fixtures for correctness - large ones are too slow
	smallFixtures := filterFixtures(allFixtures, []string{
		"minimal", "transitive", "diamond", "multi-dep",
		"dev-deps", "pinned", "scoped",
	})

	for _, cc := range correctnessMatrix {
		cc := cc
		t.Run(cc.PMLabel, func(t *testing.T) {
			t.Parallel()
			for _, fixture := range smallFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					t.Parallel()
					compareResolution(t, cc, fixture)
				})
			}
		})
	}
}

func filterFixtures(all []string, keep []string) []string {
	keepSet := make(map[string]bool)
	for _, k := range keep {
		keepSet[k] = true
	}
	var filtered []string
	for _, f := range all {
		if keepSet[f] {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func compareResolution(t *testing.T, cc correctnessCase, fixture string) {
	t.Helper()

	specData, err := os.ReadFile(filepath.Join("fixtures", fixture, "package.json"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixture, err)
	}

	// Step 1: Generate with locksmith
	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: cc.Format,
	})
	if err != nil {
		t.Fatalf("locksmith Generate failed: %v", err)
	}

	// Extract resolved versions from locksmith output
	locksmith_versions := extractVersions(t, result.Lockfile, cc.LockFileName)

	// Step 2: Generate with real package manager in Docker
	realDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(realDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}
	if cc.SetupFunc != nil {
		cc.SetupFunc(t, realDir)
	}

	args := []string{
		"run", "--rm",
		"--platform", "linux/amd64",
		"-v", realDir + ":/workspace",
		"-w", "/workspace",
		dockerImage,
	}
	args = append(args, cc.GenCommand...)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real %s failed to generate lockfile:\n%s\nerror: %v",
			cc.PMLabel, string(output), err)
	}

	realLockfile, err := os.ReadFile(filepath.Join(realDir, cc.RealLockFileName))
	if err != nil {
		t.Fatalf("reading real lockfile: %v", err)
	}

	realVersions := extractVersions(t, realLockfile, cc.RealLockFileName)

	// Step 3: Compare
	if !versionsMatch(locksmith_versions, realVersions) {
		t.Errorf("resolution mismatch for %s/%s:\nlocksmith: %v\nreal %s: %v\ndiff:\n%s",
			cc.Format, fixture,
			locksmith_versions, cc.PMLabel, realVersions,
			versionDiff(locksmith_versions, realVersions))
	} else {
		t.Logf("exact match: %d packages resolved identically", len(locksmith_versions))
	}
}

// extractVersions pulls "name@version" pairs from a lockfile.
// Supports package-lock.json, pnpm-lock.yaml, yarn.lock, and bun.lock.
func extractVersions(t *testing.T, data []byte, filename string) []string {
	t.Helper()

	switch {
	case strings.HasSuffix(filename, ".json") || strings.HasSuffix(filename, ".lock") && strings.Contains(filename, "bun"):
		return extractVersionsJSON(t, data)
	case strings.HasSuffix(filename, ".yaml"):
		return extractVersionsYAML(t, data)
	case filename == "yarn.lock":
		return extractVersionsYarnLock(t, data)
	default:
		// Try JSON first, then YAML
		if v := extractVersionsJSON(t, data); len(v) > 0 {
			return v
		}
		return extractVersionsYAML(t, data)
	}
}

func extractVersionsJSON(t *testing.T, data []byte) []string {
	t.Helper()

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Might be bun.lock - try it
		return nil
	}

	var versions []string

	// package-lock.json v2/v3 - packages section
	if packages, ok := parsed["packages"].(map[string]interface{}); ok {
		for key, val := range packages {
			if key == "" {
				continue
			}
			if entry, ok := val.(map[string]interface{}); ok {
				if version, ok := entry["version"].(string); ok {
					name := key
					// Strip node_modules/ prefix, keep last segment
					parts := strings.Split(name, "node_modules/")
					name = parts[len(parts)-1]
					versions = append(versions, name+"@"+version)
				}
			}
			// bun.lock array format
			if arr, ok := val.([]interface{}); ok && len(arr) > 0 {
				if spec, ok := arr[0].(string); ok {
					versions = append(versions, spec)
				}
			}
		}
	}

	// package-lock.json v1 - dependencies section
	if deps, ok := parsed["dependencies"].(map[string]interface{}); ok && len(versions) == 0 {
		extractV1Deps("", deps, &versions)
	}

	// bun.lock workspaces/packages format
	if _, ok := parsed["workspaces"]; ok {
		if packages, ok := parsed["packages"].(map[string]interface{}); ok && len(versions) == 0 {
			for _, val := range packages {
				if arr, ok := val.([]interface{}); ok && len(arr) > 0 {
					if spec, ok := arr[0].(string); ok {
						versions = append(versions, spec)
					}
				}
			}
		}
	}

	sort.Strings(versions)
	return versions
}

func extractV1Deps(prefix string, deps map[string]interface{}, versions *[]string) {
	for name, val := range deps {
		if entry, ok := val.(map[string]interface{}); ok {
			if version, ok := entry["version"].(string); ok {
				*versions = append(*versions, name+"@"+version)
			}
			// Recurse into nested dependencies
			if nested, ok := entry["dependencies"].(map[string]interface{}); ok {
				extractV1Deps(name+"/", nested, versions)
			}
		}
	}
}

func extractVersionsYAML(t *testing.T, data []byte) []string {
	t.Helper()
	content := string(data)
	var versions []string

	// Extract from pnpm-lock.yaml: lines matching /name@version: or /name/version:
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "/") && strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(strings.TrimPrefix(trimmed, "/"), ":")
			// Normalize /name/version -> name@version and /name@version -> name@version
			if !strings.Contains(key, "@") {
				// v5 format: /name/version -> name@version
				lastSlash := strings.LastIndex(key, "/")
				if lastSlash > 0 {
					key = key[:lastSlash] + "@" + key[lastSlash+1:]
				}
			}
			versions = append(versions, key)
		}
		// v9 snapshots format: name@version:
		if !strings.HasPrefix(trimmed, "/") && strings.Contains(trimmed, "@") && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "#") {
			key := strings.TrimSuffix(trimmed, ":")
			if strings.Count(key, "@") == 1 || (strings.HasPrefix(key, "@") && strings.Count(key, "@") == 2) {
				versions = append(versions, key)
			}
		}
	}

	// Deduplicate (v9 has both packages and snapshots with same keys)
	seen := make(map[string]bool)
	var deduped []string
	for _, v := range versions {
		if !seen[v] {
			seen[v] = true
			deduped = append(deduped, v)
		}
	}

	sort.Strings(deduped)
	return deduped
}

func extractVersionsYarnLock(t *testing.T, data []byte) []string {
	t.Helper()
	content := string(data)
	var versions []string

	// yarn.lock: extract "name@version" from version lines
	var currentName string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// Entry header line: "name@constraint:" or "name@constraint", "name@constraint":
		if !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, "@") && strings.HasSuffix(trimmed, ":") {
			// Extract the package name from the first constraint
			parts := strings.SplitN(strings.TrimSuffix(trimmed, ":"), "@", 2)
			if len(parts) >= 1 {
				name := strings.Trim(parts[0], "\"")
				// Handle scoped packages
				if strings.HasPrefix(trimmed, "\"@") || strings.HasPrefix(trimmed, "@") {
					// Scoped: find the second @
					unquoted := strings.Trim(strings.TrimSuffix(trimmed, ":"), "\" ")
					idx := strings.Index(unquoted[1:], "@")
					if idx >= 0 {
						name = unquoted[:idx+1]
					}
				}
				currentName = name
			}
		}

		// Version line
		if strings.HasPrefix(trimmed, "version ") && currentName != "" {
			version := strings.Trim(strings.TrimPrefix(trimmed, "version "), "\"")
			versions = append(versions, currentName+"@"+version)
			currentName = ""
		}
	}

	sort.Strings(versions)
	return versions
}

func versionsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func versionDiff(locksmith, real []string) string {
	libSet := make(map[string]bool)
	for _, v := range locksmith {
		libSet[v] = true
	}
	realSet := make(map[string]bool)
	for _, v := range real {
		realSet[v] = true
	}

	var lines []string
	for _, v := range locksmith {
		if !realSet[v] {
			lines = append(lines, "- locksmith has: "+v)
		}
	}
	for _, v := range real {
		if !libSet[v] {
			lines = append(lines, "+ real has: "+v)
		}
	}
	if len(lines) == 0 {
		return "(same packages, different count)"
	}
	return strings.Join(lines, "\n")
}
