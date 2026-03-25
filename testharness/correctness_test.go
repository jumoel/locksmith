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
	"github.com/jumoel/locksmith/ecosystem"
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
	// PolicyOverride overrides the default resolution policy for this PM version.
	// e.g., npm 3-6 and pnpm 7 don't auto-install peer deps.
	PolicyOverride *ecosystem.ResolverPolicy
}

// noPeerAutoInstall matches npm 3-6 and pnpm 7 behavior where peer deps
// are NOT automatically installed.
var noPeerAutoInstall = &ecosystem.ResolverPolicy{
	CrossTreeDedup:   true,
	AutoInstallPeers: false,
}

// correctnessMatrix defines all format/pm pairs for resolution comparison.
// Each entry pairs a locksmith format with every real package manager version
// that natively produces that format.
var correctnessMatrix = []correctnessCase{
	// --- npm shrinkwrap v1 ---
	// npm 1-2: peer deps ARE auto-installed (default policy). npm@2 can
	// resolve simple fixtures directly.
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@2-shrinkwrap", []string{"bash", "-c", "run-npm 2 install --ignore-scripts && run-npm 2 shrinkwrap"}, "npm-shrinkwrap.json", nil, nil},
	// npm 3-6: peer deps NOT auto-installed. npm@3-4 crash on complex trees,
	// so we use npm@5 as a reference resolver (same resolution algorithm,
	// no crash bugs). npm@5 also doesn't auto-install peers.
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@3-shrinkwrap", []string{"bash", "-c", "run-npm 5 install --ignore-scripts && run-npm 5 shrinkwrap"}, "npm-shrinkwrap.json", nil, noPeerAutoInstall},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@4-shrinkwrap", []string{"bash", "-c", "run-npm 5 install --ignore-scripts && run-npm 5 shrinkwrap"}, "npm-shrinkwrap.json", nil, noPeerAutoInstall},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@5-shrinkwrap", []string{"bash", "-c", "run-npm 5 install --ignore-scripts && run-npm 5 shrinkwrap"}, "npm-shrinkwrap.json", nil, noPeerAutoInstall},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm@6-shrinkwrap", []string{"bash", "-c", "run-npm 6 install --ignore-scripts && run-npm 6 shrinkwrap"}, "npm-shrinkwrap.json", nil, noPeerAutoInstall},

	// --- package-lock v1: npm 5-6 (no peer auto-install) ---
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm@5-v1", []string{"run-npm", "5", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, noPeerAutoInstall},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm@6-v1", []string{"run-npm", "6", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, noPeerAutoInstall},

	// --- package-lock v2: npm 7-8 (native v2 producers) ---
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm@7-v2", []string{"run-npm", "7", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm@8-v2", []string{"run-npm", "8", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},

	// --- package-lock v3: npm 7-11 (all understand v3) ---
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@7-v3", []string{"run-npm", "7", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@8-v3", []string{"run-npm", "8", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@9-v3", []string{"run-npm", "9", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@10-v3", []string{"run-npm", "10", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@11-v3", []string{"run-npm", "11", "install", "--package-lock-only", "--ignore-scripts"}, "package-lock.json", nil, nil},

	// --- pnpm: each version with its native lockfile format ---
	{locksmith.FormatPnpmLockV4, "pnpm-lock.yaml", "pnpm@4-v5.1", []string{"run-pnpm", "4", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, noPeerAutoInstall},
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm@5-v5.2", []string{"run-pnpm", "5", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, noPeerAutoInstall},
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm@7-v5.4", []string{"run-pnpm", "7", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, noPeerAutoInstall},
	{locksmith.FormatPnpmLockV6, "pnpm-lock.yaml", "pnpm@8-v6", []string{"run-pnpm", "8", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm@9-v9", []string{"run-pnpm", "9", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm@10-v9", []string{"run-pnpm", "10", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, nil},

	// --- pnpm v5.3: pnpm@6 (via @pnpm/exe, bundles own Node) ---
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm@6-v5.3", []string{"run-pnpm", "6", "install", "--lockfile-only", "--ignore-scripts"}, "pnpm-lock.yaml", nil, noPeerAutoInstall},

	// --- yarn classic ---
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn@1", []string{"run-yarn", "1", "install", "--ignore-scripts"}, "yarn.lock", nil, nil},

	// --- yarn berry ---
	{locksmith.FormatYarnBerryV4, "yarn.lock", "yarn@2-v4", []string{"run-yarn", "2", "install"}, "yarn.lock", setupYarnBerry, nil},
	{locksmith.FormatYarnBerryV5, "yarn.lock", "yarn@3.1-v5", []string{"run-yarn", "3.1", "install"}, "yarn.lock", setupYarnBerry, nil},
	{locksmith.FormatYarnBerryV6, "yarn.lock", "yarn@3-v6", []string{"run-yarn", "3", "install"}, "yarn.lock", setupYarnBerry, nil},
	{locksmith.FormatYarnBerryV8, "yarn.lock", "yarn@4-v8", []string{"run-yarn", "4", "install"}, "yarn.lock", setupYarnBerry, nil},

	// --- bun ---
	{locksmith.FormatBunLock, "bun.lock", "bun", []string{"bun", "install", "--save-text-lockfile"}, "bun.lock", nil, nil},
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

	// Correctness fixtures include small fixtures plus real-world packages.
	// Excludes very large fixtures (webpack, next-app) that are too slow for
	// per-PM-version testing. Large fixtures get tested in TestGenerate instead.
	correctnessFixtures := filterFixtures(allFixtures, []string{
		// Core patterns
		"minimal", "transitive", "diamond", "multi-dep",
		"dev-deps", "pinned", "scoped", "zero-deps",
		// React versions
		"react-15", "react-16", "react-17", "react-18", "react-19",
		// Next.js (lighter ones)
		"next-12", "next-13",
		// TypeScript
		"typescript-4", "typescript-5",
		// Package managers
		"yarn-classic-pkg",
		// Arborist test fixtures (real @isaacs/ test packages)
		"arborist-dedupe", "arborist-dev-deps", "arborist-optional-missing",
		"arborist-bundle", "arborist-peer-cycle",
		// pnpm/yarn-inspired edge cases
		"pnpm-peer-ajv", "peer-chain", "conflicting-ranges",
		"deprecated-pkg",
		// Other real-world patterns
		"peer-deps", "monorepo-tools", "cli-tools",
		// Mixed
		"mixed-large", "many-direct",
	})

	// Known incompatible combinations where the real PM can't generate a
	// lockfile for the fixture, or where PM defaults differ from our config.
	skipCombos := map[string]string{
		// bun and pnpm@4-7 don't produce lockfiles for empty projects
		"bun/zero-deps":          "bun deletes lockfile for empty projects",
		"pnpm@4-v5.1/zero-deps": "pnpm@4 errors on empty projects",
		"pnpm@5-v5.2/zero-deps": "pnpm@5 errors on empty projects",
		"pnpm@6-v5.3/zero-deps": "pnpm@6 errors on empty projects",
		"pnpm@7-v5.4/zero-deps": "pnpm@7 errors on empty projects",
		// yarn errors on optional deps that don't exist on the registry
		"yarn@1/arborist-optional-missing":   "yarn@1 errors on missing optional deps",
		"yarn@2-v4/arborist-optional-missing": "yarn@2 errors on missing optional deps",
		"yarn@3-v6/arborist-optional-missing": "yarn@3 errors on missing optional deps",
		"yarn@4-v8/arborist-optional-missing": "yarn@4 errors on missing optional deps",
		// yarn@2 can't resolve some newer packages
		"yarn@2-v4/mixed-large":   "yarn@2 can't resolve some newer packages",
		"yarn@2-v4/typescript-4":  "yarn@2 can't resolve typescript@4",
		"yarn@2-v4/typescript-5":  "yarn@2 can't resolve typescript@5",
	}

	// npm 2-4 shrinkwrap excludes devDependencies.
	npm234DevDepFixtures := []string{
		"dev-deps", "arborist-dev-deps",
		"typescript-4", "typescript-5",
		"mixed-large", "monorepo-tools",
	}
	for _, pm := range []string{"npm@2-shrinkwrap", "npm@3-shrinkwrap", "npm@4-shrinkwrap"} {
		for _, f := range npm234DevDepFixtures {
			skipCombos[pm+"/"+f] = "npm 2-4 shrinkwrap excludes devDependencies"
		}
	}

	// npm@2 crashes on large dep trees (ENOTDIR .staging bug). Can't use
	// npm@5 as fallback because npm@2 auto-installs peers but npm@5 doesn't.
	npm2CrashFixtures := []string{
		"peer-chain", "peer-deps", "cli-tools", "next-12", "next-13",
	}
	for _, f := range npm2CrashFixtures {
		skipCombos["npm@2-shrinkwrap/"+f] = "npm@2 crashes on complex dep trees (ENOTDIR)"
	}

	for _, cc := range correctnessMatrix {
		cc := cc
		// PM versions run sequentially to avoid spawning hundreds of
		// Docker containers at once. Fixtures within each PM run in parallel.
		t.Run(cc.PMLabel, func(t *testing.T) {
			for _, fixture := range correctnessFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					if reason, skip := skipCombos[cc.PMLabel+"/"+fixture]; skip {
						t.Skipf("known incompatibility: %s", reason)
					}
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

	// Step 1: Generate with locksmith using the PM-version-specific policy.
	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:       specData,
		OutputFormat:   cc.Format,
		PolicyOverride: cc.PolicyOverride,
	})
	if err != nil {
		t.Fatalf("locksmith Generate failed: %v", err)
	}

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

	// Step 3a: Textual comparison - the gold standard.
	// If the lockfile bytes are identical, we're done.
	if string(result.Lockfile) == string(realLockfile) {
		t.Logf("byte-identical lockfile (%d bytes)", len(result.Lockfile))
		return
	}

	// Step 3b: Lockfiles differ textually. This may be acceptable if the
	// resolved versions are the same (field ordering, formatting, extra
	// metadata like license fields can differ). Log the textual diff for
	// inspection but only fail if the resolved versions differ.
	locksmith_versions := extractVersions(t, result.Lockfile, cc.LockFileName)
	realVersions := extractVersions(t, realLockfile, cc.RealLockFileName)

	if !versionsMatch(locksmith_versions, realVersions) {
		// Save both lockfiles for manual inspection.
		debugDir := filepath.Join("..", ".tmp", "correctness-debug", cc.PMLabel, fixture)
		os.MkdirAll(debugDir, 0o755)
		os.WriteFile(filepath.Join(debugDir, "locksmith-"+cc.LockFileName), result.Lockfile, 0o644)
		os.WriteFile(filepath.Join(debugDir, "real-"+cc.RealLockFileName), realLockfile, 0o644)

		t.Errorf("resolution mismatch for %s/%s:\nlocksmith: %v\nreal %s: %v\ndiff:\n%s\nfull lockfiles saved to %s",
			cc.Format, fixture,
			locksmith_versions, cc.PMLabel, realVersions,
			versionDiff(locksmith_versions, realVersions),
			debugDir)
	} else {
		t.Logf("versions match (%d packages), but lockfile text differs (formatting/metadata)", len(locksmith_versions))
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

// stripJSONCCommas strips trailing commas from JSONC to make it valid JSON.
func stripJSONCCommas(data []byte) []byte {
	s := string(data)
	// Remove trailing commas before } or ]
	// This is a rough approach but works for bun.lock format
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			// Look ahead for only whitespace then } or ]
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue // skip the trailing comma
			}
		}
		result.WriteByte(s[i])
	}
	return []byte(result.String())
}

func extractVersionsJSON(t *testing.T, data []byte) []string {
	t.Helper()

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		// Try stripping JSONC trailing commas (bun.lock)
		cleaned := stripJSONCCommas(data)
		if err2 := json.Unmarshal(cleaned, &parsed); err2 != nil {
			return nil
		}
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

	// Deduplicate: when the same name@version appears at multiple
	// node_modules paths we only care about the unique set.
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
	// Only match entries at the top level of packages/snapshots sections
	// (indented exactly 2 spaces), not deeply nested entries like
	// transitivePeerDependencies list items.
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)

		// v5/v6 format: /name@version: or /name/version: (2-space indent)
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(line, "    ") {
			key := strings.TrimSuffix(strings.TrimPrefix(trimmed, "/"), ":")

			// Strip v5 peer context suffix (underscore-delimited) FIRST,
			// before checking for @ format.
			if idx := strings.Index(key, "_"); idx > 0 {
				key = key[:idx]
			}

			// v5 format: name/version -> name@version
			if !strings.Contains(key, "@") {
				lastSlash := strings.LastIndex(key, "/")
				if lastSlash > 0 {
					key = key[:lastSlash] + "@" + key[lastSlash+1:]
				}
			}
			versions = append(versions, key)
		}

		// v9 snapshots/packages format: name@version: (2-space indent, no leading /)
		// Must contain version digits after the last @, not just a bare scoped name.
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") &&
			!strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "#") &&
			!strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "'") &&
			strings.Contains(trimmed, "@") && strings.HasSuffix(trimmed, ":") {
			key := strings.TrimSuffix(trimmed, ":")
			// Verify it has a version component (digits after the last @)
			lastAt := strings.LastIndex(key, "@")
			if lastAt > 0 && lastAt < len(key)-1 {
				versionPart := key[lastAt+1:]
				if len(versionPart) > 0 && versionPart[0] >= '0' && versionPart[0] <= '9' {
					versions = append(versions, key)
				}
			}
		}
	}

	// Deduplicate and normalize.
	// Strip pnpm peer context suffixes: "react-dom@18.3.1(react@18.3.1)" -> "react-dom@18.3.1"
	// v9 has both packages and snapshots with same keys.
	seen := make(map[string]bool)
	var deduped []string
	for _, v := range versions {
		// Strip pnpm peer context suffixes:
		// v9: "react-dom@18.3.1(react@18.3.1)" -> "react-dom@18.3.1"
		// v5: "react-dom@18.3.1_react@18.3.1" -> "react-dom@18.3.1"
		if idx := strings.Index(v, "("); idx > 0 {
			v = v[:idx]
		}
		// For v5 underscore format: find the version part and strip everything after
		// the first underscore that follows a version number.
		if parts := strings.SplitN(v, "_", 2); len(parts) == 2 {
			// Check if the part before _ looks like name@version
			if strings.Contains(parts[0], "@") {
				v = parts[0]
			}
		}
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
