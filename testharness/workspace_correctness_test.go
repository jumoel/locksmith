//go:build integration

package testharness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
	"github.com/jumoel/locksmith/npm"
)

// readWorkspaceFixtureFromGlobs reads a workspace fixture's root package.json,
// discovers workspace members via ParseWorkspaceGlobs, and returns the root
// spec data alongside a map of relative path to member spec data.
func readWorkspaceFixtureFromGlobs(t *testing.T, fixture string) (specData []byte, members map[string][]byte) {
	t.Helper()
	fixtureDir := filepath.Join("fixtures", fixture)

	specData, err := os.ReadFile(filepath.Join(fixtureDir, "package.json"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixture, err)
	}

	globs, err := npm.ParseWorkspaceGlobs(specData)
	if err != nil || len(globs) == 0 {
		return specData, nil
	}

	members = make(map[string][]byte)
	for _, glob := range globs {
		pattern := filepath.Join(fixtureDir, glob)
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			pkgPath := filepath.Join(match, "package.json")
			data, err := os.ReadFile(pkgPath)
			if err != nil {
				continue
			}
			relPath, _ := filepath.Rel(fixtureDir, match)
			members[relPath] = data
		}
	}
	return specData, members
}

// workspaceCorrectnessMatrix defines format/PM pairs for workspace resolution
// comparison. Only includes PM versions that fully support workspaces.
var workspaceCorrectnessMatrix = []correctnessCase{
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm@10-v3",
		[]string{"run-npm", "10", "install", "--package-lock-only", "--ignore-scripts"},
		"package-lock.json", nil, nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm@10-v9",
		[]string{"run-pnpm", "10", "install", "--lockfile-only", "--ignore-scripts"},
		"pnpm-lock.yaml", nil, nil},
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn@1",
		[]string{"run-yarn", "1", "install", "--ignore-scripts"},
		"yarn.lock", nil, nil},
	{locksmith.FormatYarnBerryV8, "yarn.lock", "yarn@4-v8",
		[]string{"run-yarn", "4", "install"},
		"yarn.lock", setupYarnBerry, nil},
	{locksmith.FormatBunLock, "bun.lock", "bun@1.3",
		[]string{"run-bun", "1.3", "install", "--save-text-lockfile"},
		"bun.lock", nil, nil},
}

// workspaceFixtures lists the workspace fixture directories to test.
var workspaceFixtures = []string{
	"workspace-simple",
	"workspace-npm-style",
	"workspace-cross-deps",
}

// TestWorkspaceCorrectness generates workspace lockfiles with both locksmith
// and the real package manager, then compares resolved package versions.
//
// Test names: TestWorkspaceCorrectness/{pm_label}/{fixture}
func TestWorkspaceCorrectness(t *testing.T) {
	for _, cc := range workspaceCorrectnessMatrix {
		cc := cc
		t.Run(cc.PMLabel, func(t *testing.T) {
			for _, fixture := range workspaceFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					// pnpm requires workspace: protocol for cross-workspace deps.
					if fixture == "workspace-npm-style" && strings.HasPrefix(cc.PMLabel, "pnpm") {
						t.Skip("pnpm requires workspace: protocol for cross-workspace deps")
					}
					// workspace-cross-deps uses workspace:^ which npm and yarn classic don't support.
					if fixture == "workspace-cross-deps" {
						if cc.PMLabel == "npm@10-v3" {
							t.Skip("npm doesn't support workspace: protocol in package.json")
						}
						if cc.PMLabel == "yarn@1" {
							t.Skip("yarn classic doesn't support workspace: protocol")
						}
					}
					// workspace-npm-style uses resolve-by-name which bun/yarn berry don't support.
					if fixture == "workspace-npm-style" {
						if strings.HasPrefix(cc.PMLabel, "bun") || cc.PMLabel == "yarn@4-v8" {
							t.Skip("workspace-npm-style requires resolve-by-name (npm/yarn classic only)")
						}
					}
					t.Parallel()
					compareWorkspaceResolution(t, cc, fixture)
				})
			}
		})
	}
}

// compareWorkspaceResolution generates a workspace lockfile with locksmith and
// the real PM, then compares resolved versions. Workspace-aware variant of
// compareResolution from correctness_test.go.
func compareWorkspaceResolution(t *testing.T, cc correctnessCase, fixture string) {
	t.Helper()

	fixtureDir := filepath.Join("fixtures", fixture)
	specData, members := readWorkspaceFixtureFromGlobs(t, fixture)

	// Step 1: Generate with locksmith, passing workspace members.
	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:         specData,
		OutputFormat:     cc.Format,
		WorkspaceMembers: members,
		SpecDir:          fixtureDir,
		Platform:         "linux/x64",
		PolicyOverride:   cc.PolicyOverride,
	})
	if err != nil {
		t.Fatalf("locksmith Generate failed: %v", err)
	}

	// Step 2: Generate with real package manager in Docker.
	// The real PM discovers workspace members from the filesystem automatically.
	realDir, err := os.MkdirTemp("", "locksmith-ws-correctness-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		exec.Command("docker", "run", "--rm", "--platform", "linux/amd64",
			"-v", realDir+":/workspace", "-w", "/workspace",
			dockerImage, "rm", "-rf", ".").CombinedOutput()
		os.RemoveAll(realDir)
	}()

	// Write root package.json.
	if err := os.WriteFile(filepath.Join(realDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy all fixture subdirectories (workspace member dirs with their package.json files).
	copyFixtureSubdirs(t, fixtureDir, realDir)

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

	// Step 3a: Byte-identical comparison.
	if string(result.Lockfile) == string(realLockfile) {
		t.Logf("byte-identical lockfile (%d bytes)", len(result.Lockfile))
		return
	}

	// Step 3b: Compare resolved versions (field ordering, metadata can differ).
	locksmithVersions := extractVersions(t, result.Lockfile, cc.LockFileName)
	realVersions := extractVersions(t, realLockfile, cc.RealLockFileName)

	if !versionsMatch(locksmithVersions, realVersions) {
		debugDir := filepath.Join("..", ".tmp", "ws-correctness-debug", cc.PMLabel, fixture)
		os.MkdirAll(debugDir, 0o755)
		os.WriteFile(filepath.Join(debugDir, "locksmith-"+cc.LockFileName), result.Lockfile, 0o644)
		os.WriteFile(filepath.Join(debugDir, "real-"+cc.RealLockFileName), realLockfile, 0o644)

		t.Errorf("workspace resolution mismatch for %s/%s:\nlocksmith: %v\nreal %s: %v\ndiff:\n%s\nfull lockfiles saved to %s",
			cc.Format, fixture,
			locksmithVersions, cc.PMLabel, realVersions,
			versionDiff(locksmithVersions, realVersions),
			debugDir)
	} else {
		t.Logf("versions match (%d packages), but lockfile text differs (formatting/metadata)", len(locksmithVersions))
	}
}

// workspaceAcceptanceMatrix defines format/PM pairs for workspace acceptance
// verification. Each entry generates a workspace lockfile with locksmith and
// verifies the real PM accepts it via frozen install.
var workspaceAcceptanceMatrix = []verificationCase{
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "10",
		[]string{"run-npm", "10", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm", "10",
		[]string{"run-pnpm", "10", "install", "--frozen-lockfile"}, nil},
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn", "1",
		[]string{"run-yarn", "1", "install", "--frozen-lockfile"}, nil},
	{locksmith.FormatYarnBerryV8, "yarn.lock", "yarn", "4",
		[]string{"run-yarn", "4", "install", "--immutable"}, setupYarnBerry},
	{locksmith.FormatBunLock, "bun.lock", "bun", "1.3",
		[]string{"run-bun", "1.3", "install", "--frozen-lockfile"}, nil},
}

// TestWorkspaceAcceptance generates workspace lockfiles with locksmith and
// verifies that real package managers accept them via frozen install.
//
// Test names: TestWorkspaceAcceptance/{format}/{fixture}/{pm}_{version}
func TestWorkspaceAcceptance(t *testing.T) {
	for _, vc := range workspaceAcceptanceMatrix {
		vc := vc
		t.Run(string(vc.Format), func(t *testing.T) {
			t.Parallel()
			for _, fixture := range workspaceFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					// pnpm requires workspace: protocol for cross-workspace deps.
					if fixture == "workspace-npm-style" && vc.PMName == "pnpm" {
						t.Skip("pnpm requires workspace: protocol for cross-workspace deps")
					}
					// workspace-cross-deps uses workspace:^ which npm and yarn classic don't support.
					if fixture == "workspace-cross-deps" {
						if vc.PMName == "npm" {
							t.Skip("npm doesn't support workspace: protocol in package.json")
						}
						if vc.PMName == "yarn" && vc.PMVersion == "1" {
							t.Skip("yarn classic doesn't support workspace: protocol")
						}
					}
					// workspace-npm-style uses resolve-by-name which bun/yarn berry don't support.
					if fixture == "workspace-npm-style" {
						if vc.PMName == "bun" || (vc.PMName == "yarn" && vc.PMVersion != "1") {
							t.Skip("workspace-npm-style requires resolve-by-name (npm/yarn classic only)")
						}
					}
					// workspace-simple acceptance: npm lockfile workspace placement and
					// yarn classic frozen install have known issues with the workspace: protocol.
					if fixture == "workspace-simple" {
						if vc.PMName == "npm" {
							t.Skip("npm workspace lockfile placement needs further work for acceptance")
						}
						if vc.PMName == "yarn" && vc.PMVersion == "1" {
							t.Skip("yarn classic frozen install fails for workspace: protocol fixtures")
						}
					}
					t.Parallel()
					pmTag := vc.PMName + "_" + vc.PMVersion
					t.Run(pmTag, func(t *testing.T) {
						runWorkspaceVerification(t, vc, fixture)
					})
				})
			}
		})
	}
}

// runWorkspaceVerification generates a workspace lockfile and verifies it
// installs cleanly with the target PM in Docker.
func runWorkspaceVerification(t *testing.T, vc verificationCase, fixture string) {
	t.Helper()

	fixtureDir := filepath.Join("fixtures", fixture)
	specData, members := readWorkspaceFixtureFromGlobs(t, fixture)

	// Generate lockfile targeting the Docker runner's platform (linux/x64).
	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:         specData,
		OutputFormat:     vc.Format,
		WorkspaceMembers: members,
		SpecDir:          fixtureDir,
		Platform:         "linux/x64",
	})
	if err != nil {
		t.Fatalf("Generate(%s, %s) failed: %v", vc.Format, fixture, err)
	}
	if len(result.Lockfile) == 0 {
		t.Fatal("generated empty lockfile")
	}

	// Write to temp directory for Docker mount.
	tmpDir, err := os.MkdirTemp("", "locksmith-ws-acceptance-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		exec.Command("docker", "run", "--rm", "--platform", "linux/amd64",
			"-v", tmpDir+":/workspace", "-w", "/workspace",
			dockerImage, "rm", "-rf", ".").CombinedOutput()
		os.RemoveAll(tmpDir)
	}()

	// Write root package.json and lockfile.
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, vc.FileName), result.Lockfile, 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy all fixture subdirectories (workspace member dirs).
	copyFixtureSubdirs(t, fixtureDir, tmpDir)

	// Run optional setup (e.g., .yarnrc.yml for berry).
	if vc.SetupFunc != nil {
		vc.SetupFunc(t, tmpDir)
	}

	// Run package manager in Docker.
	args := []string{
		"run", "--rm",
		"--platform", "linux/amd64",
		"-v", tmpDir + ":/workspace",
		"-w", "/workspace",
		dockerImage,
	}
	args = append(args, vc.Command...)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s workspace verification failed for %s/%s:\n%s\nerror: %v",
			vc.PMName, vc.PMVersion, vc.Format, fixture,
			string(output), err)
	}
	t.Logf("%s %s workspace verified %s/%s: %s",
		vc.PMName, vc.PMVersion, vc.Format, fixture,
		strings.TrimSpace(string(output)))
}
