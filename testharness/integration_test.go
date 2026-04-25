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

const dockerImage = "locksmith-test-runner"

// verificationCase defines a single format + package manager version combination
// that should be able to consume the generated lockfile.
type verificationCase struct {
	Format    locksmith.OutputFormat
	FileName  string   // lockfile name to write
	PMName    string   // package manager name for test naming
	PMVersion string   // version string passed to helper script
	Command   []string // docker command to run
	SetupFunc func(t *testing.T, dir string) // optional per-case setup
}

// verificationMatrix defines the full compatibility matrix.
// Each entry pairs a lockfile format with a package manager version that should
// accept it via frozen install.
var verificationMatrix = []verificationCase{
	// package-lock-v1: npm 6 through 11.
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "6", []string{"run-npm", "6", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "7", []string{"run-npm", "7", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "8", []string{"run-npm", "8", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "9", []string{"run-npm", "9", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "10", []string{"run-npm", "10", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV1, "package-lock.json", "npm", "11", []string{"run-npm", "11", "ci", "--ignore-scripts"}, nil},

	// package-lock-v2: npm 7 through 11.
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", "7", []string{"run-npm", "7", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", "8", []string{"run-npm", "8", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", "9", []string{"run-npm", "9", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", "10", []string{"run-npm", "10", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV2, "package-lock.json", "npm", "11", []string{"run-npm", "11", "ci", "--ignore-scripts"}, nil},

	// package-lock-v3: npm 7 through 11.
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "7", []string{"run-npm", "7", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "8", []string{"run-npm", "8", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "9", []string{"run-npm", "9", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "10", []string{"run-npm", "10", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatPackageLockV3, "package-lock.json", "npm", "11", []string{"run-npm", "11", "ci", "--ignore-scripts"}, nil},

	// npm-shrinkwrap: npm 2 through 11. npm 2-5 use install (no ci).
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "2", []string{"run-npm", "2", "install", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "5", []string{"run-npm", "5", "install", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "6", []string{"run-npm", "6", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "7", []string{"run-npm", "7", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "8", []string{"run-npm", "8", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "9", []string{"run-npm", "9", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "10", []string{"run-npm", "10", "ci", "--ignore-scripts"}, nil},
	{locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json", "npm", "11", []string{"run-npm", "11", "ci", "--ignore-scripts"}, nil},

	// pnpm-lock v5.1: pnpm 4.
	{locksmith.FormatPnpmLockV4, "pnpm-lock.yaml", "pnpm", "4", []string{"run-pnpm", "4", "install", "--frozen-lockfile"}, nil},

	// pnpm-lock-v5: pnpm 5 and 7.
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm", "5", []string{"run-pnpm", "5", "install", "--frozen-lockfile"}, nil},
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm", "7", []string{"run-pnpm", "7", "install", "--frozen-lockfile"}, nil},

	// pnpm-lock-v5: also pnpm 6 (produces lockfileVersion 5.3).
	{locksmith.FormatPnpmLockV5, "pnpm-lock.yaml", "pnpm", "6", []string{"run-pnpm", "6", "install", "--frozen-lockfile"}, nil},

	// pnpm-lock-v6: pnpm 8.
	{locksmith.FormatPnpmLockV6, "pnpm-lock.yaml", "pnpm", "8", []string{"run-pnpm", "8", "install", "--frozen-lockfile"}, nil},

	// pnpm-lock-v9: pnpm 9 and 10.
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm", "9", []string{"run-pnpm", "9", "install", "--frozen-lockfile"}, nil},
	{locksmith.FormatPnpmLockV9, "pnpm-lock.yaml", "pnpm", "10", []string{"run-pnpm", "10", "install", "--frozen-lockfile"}, nil},

	// yarn-classic: yarn 1.
	{locksmith.FormatYarnClassic, "yarn.lock", "yarn", "1", []string{"run-yarn", "1", "install", "--frozen-lockfile"}, nil},

	// yarn-berry-v4: yarn 2 (no --immutable, checksums need first-install update).
	{locksmith.FormatYarnBerryV4, "yarn.lock", "yarn", "2", []string{"run-yarn", "2", "install"}, setupYarnBerry},

	// yarn-berry-v5: yarn 3.1 (no --immutable, checksums need first-install update).
	{locksmith.FormatYarnBerryV5, "yarn.lock", "yarn", "3.1", []string{"run-yarn", "3.1", "install"}, setupYarnBerry},

	// yarn-berry-v6: yarn 3.
	{locksmith.FormatYarnBerryV6, "yarn.lock", "yarn", "3", []string{"run-yarn", "3", "install", "--immutable"}, setupYarnBerry},

	// yarn-berry-v8: yarn 4.
	{locksmith.FormatYarnBerryV8, "yarn.lock", "yarn", "4", []string{"run-yarn", "4", "install", "--immutable"}, setupYarnBerry},

	// bun-lock: bun (latest).
	{locksmith.FormatBunLock, "bun.lock", "bun", "latest", []string{"bun", "install", "--frozen-lockfile"}, nil},
}

func TestMain(m *testing.M) {
	// Build docker image before running tests.
	cmd := exec.Command("docker", "build", "-t", dockerImage, "--platform", "linux/amd64", ".")
	cmd.Dir = "." // testharness/ directory
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("failed to build test docker image: " + err.Error())
	}
	os.Exit(m.Run())
}

// TestIntegration runs the full compatibility matrix: generate a lockfile with
// locksmith, then verify it installs cleanly with the target package manager
// version inside Docker.
//
// Test names follow the pattern TestIntegration/{format}/{fixture}/{pm}_{version}
// so CI can filter by format:
//
//	go test -tags integration -run "TestIntegration/package-lock-v3" ./testharness/
func TestIntegration(t *testing.T) {
	allFixtures := fixtures(t)

	for _, vc := range verificationMatrix {
		vc := vc
		groupName := string(vc.Format) + "/" + vc.PMName + "@" + vc.PMVersion
		t.Run(groupName, func(t *testing.T) {
			t.Parallel()
			for _, fixture := range allFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					// Skip specific PM version crashes and unfixable PM-specific behaviors.
					if vc.PMName == "bun" {
						bunSkips := map[string]string{
							"zero-deps":         "bun deletes lockfile for empty projects",
							"non-registry-deps": "fixture has git+ssh:// deps that require SSH keys in Docker; formatter supports non-registry deps",
						}
						if reason, ok := bunSkips[fixture]; ok {
							t.Skip(reason)
						}
					}
					if fixture == "aliased-dep" && vc.PMName == "npm" {
						if vc.PMVersion == "2" || vc.PMVersion == "5" || vc.PMVersion == "6" {
							t.Skip("npm 2/5/6 crashes on npm: alias syntax")
						}
					}
					// npm 2 crashes with ENOTDIR on packages with complex dep trees.
					if vc.PMName == "npm" && vc.PMVersion == "2" {
						npm2Crashes := map[string]bool{
							"cli-tools": true, "deep-chain": true, "multiple-peer-providers": true,
							"next-12": true, "npm-10": true, "peer-chain": true, "peer-deps": true,
							"non-registry-deps": true,
						}
						if npm2Crashes[fixture] {
							t.Skip("npm 2 crashes with ENOTDIR on complex dep trees")
						}
					}
					// non-registry-deps has git+ssh:// deps requiring SSH keys unavailable in Docker.
					// Skip for yarn berry and pnpm (pnpm tries to resolve git+ssh and fails).
					if fixture == "non-registry-deps" && (vc.PMName == "pnpm" || (vc.PMName == "yarn" && vc.PMVersion != "1")) {
						t.Skip("non-registry-deps fixture requires SSH keys for git+ssh:// deps in Docker")
					}
					// Yarn berry applies internal patches to typescript and resolve packages.
					if vc.PMName == "yarn" && vc.PMVersion != "1" {
						yarnPatchFixtures := map[string]bool{
							"typescript-4": true, "typescript-5": true,
							"mixed-large": true,
						}
						if yarnPatchFixtures[fixture] {
							t.Skip("yarn berry applies internal patches to typescript (plugin-compat)")
						}
						if vc.PMVersion == "3" || vc.PMVersion == "4" {
							yarnV6V8Patches := map[string]bool{
								"multiple-peer-providers": true,
								"npm-6":                  true,
							}
							if yarnV6V8Patches[fixture] {
								t.Skip("yarn v6/v8 patches resolve and adds @types/* auto-types")
							}
						}
					}
					// Skip workspace fixtures based on PM workspace: protocol support.
					if strings.HasPrefix(fixture, "workspace-") {
						// Fixtures using workspace: protocol can't be tested with PMs that don't support it.
						usesWorkspaceProtocol := fixture == "workspace-simple" || fixture == "workspace-cross-deps"
						if usesWorkspaceProtocol {
							if vc.PMName == "npm" {
								t.Skip("npm doesn't support workspace: protocol in package.json")
							}
							if vc.PMName == "yarn" && vc.PMVersion == "1" {
								t.Skip("yarn classic doesn't support workspace: protocol")
							}
							if vc.PMName == "yarn" && vc.PMVersion == "2" && fixture == "workspace-cross-deps" {
								t.Skip("yarn 2 doesn't support workspace:^ protocol")
							}
						}
						// npm-style workspace fixtures use regular semver for cross-deps.
						// Only npm and yarn classic support resolve-by-name.
						// Other PMs (pnpm, bun, yarn berry) require workspace: protocol.
						if fixture == "workspace-npm-style" {
							// npm-style workspaces use resolve-by-name for cross-deps.
							// Only yarn classic and npm v3 format (which has workspace placement
							// support) pass acceptance. Other PMs need workspace: protocol.
							if vc.PMName == "yarn" && vc.PMVersion == "1" {
								// yarn classic supports npm-style workspaces
							} else if vc.PMName == "npm" && vc.Format == locksmith.FormatPackageLockV3 {
								// npm v3 format has workspace member placement support
							} else {
								t.Skip("workspace-npm-style only supported by yarn classic and npm v3")
							}
						}
						if vc.PMName == "pnpm" && vc.PMVersion == "4" {
							t.Skip("pnpm 4 doesn't support workspaces")
						}
					}
					// Override/extension fixtures use PM-specific package.json fields.
					// Skip for PMs that don't support the specific field.
					if fixture == "overrides-npm" && vc.PMName != "npm" && vc.PMName != "bun" {
						t.Skip("npm overrides fixture only applies to npm and bun")
					}
					if fixture == "overrides-yarn" && vc.PMName != "yarn" {
						t.Skip("yarn resolutions fixture only applies to yarn")
					}
					if fixture == "pnpm-package-extensions" {
						t.Skip("pnpm packageExtensions checksum computation needs version-specific hashing (pnpm 9: MD5, pnpm 10: SHA-256)")
					}
					if fixture == "pnpm-peer-rules" && vc.PMName != "pnpm" {
						t.Skip("pnpm peerDependencyRules fixture only applies to pnpm")
					}
					t.Parallel()
					runVerification(t, vc, fixture)
				})
			}
		})
	}
}

// fixtures returns all fixture directory names.
func fixtures(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir("fixtures")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no fixture directories found in fixtures/")
	}
	return names
}

// runVerification generates a lockfile and verifies it with Docker.
func runVerification(t *testing.T, vc verificationCase, fixture string) {
	t.Helper()

	// Read fixture.
	fixtureDir := filepath.Join("fixtures", fixture)
	specData, err := os.ReadFile(filepath.Join(fixtureDir, "package.json"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixture, err)
	}

	// Generate lockfile targeting the Docker runner's platform (linux/x64).
	ctx := context.Background()
	opts := locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: vc.Format,
		Platform:     "linux/x64",
		SpecDir:      fixtureDir,
	}

	// Detect workspace fixtures and pass workspace members.
	if members := discoverWorkspaceMembersInteg(t, fixtureDir, specData); len(members) > 0 {
		opts.WorkspaceMembers = members
	}

	result, err := locksmith.Generate(ctx, opts)
	if err != nil {
		t.Fatalf("Generate(%s, %s) failed: %v", vc.Format, fixture, err)
	}
	if len(result.Lockfile) == 0 {
		t.Fatal("generated empty lockfile")
	}

	// Write to temp directory for Docker mount.
	// Use manual temp dir because Docker creates root-owned files.
	tmpDir, err := os.MkdirTemp("", "locksmith-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		exec.Command("docker", "run", "--rm", "--platform", "linux/amd64",
			"-v", tmpDir+":/workspace", "-w", "/workspace",
			dockerImage, "rm", "-rf", ".").CombinedOutput()
		os.RemoveAll(tmpDir)
	}()
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, vc.FileName), result.Lockfile, 0o644); err != nil {
		t.Fatal(err)
	}

	// Copy fixture subdirectories (e.g., local packages for file: deps).
	copyFixtureSubdirs(t, filepath.Join("fixtures", fixture), tmpDir)

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
		t.Fatalf("%s %s verification failed for %s/%s:\n%s\nerror: %v",
			vc.PMName, vc.PMVersion, vc.Format, fixture,
			string(output), err)
	}
	t.Logf("%s %s verified %s/%s: %s",
		vc.PMName, vc.PMVersion, vc.Format, fixture,
		strings.TrimSpace(string(output)))
}

// copyFixtureSubdirs copies subdirectories from a fixture into the target dir.
// This supports fixtures with local packages (file: deps) that need to exist
// alongside package.json in the Docker mount.
func copyFixtureSubdirs(t *testing.T, fixtureDir, targetDir string) {
	t.Helper()
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src := filepath.Join(fixtureDir, e.Name())
		dst := filepath.Join(targetDir, e.Name())
		if err := copyDir(src, dst); err != nil {
			t.Fatalf("copying fixture subdir %s: %v", e.Name(), err)
		}
	}
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// setupYarnBerry creates the .yarnrc.yml needed for yarn berry to use
// node_modules instead of PnP mode.
func setupYarnBerry(t *testing.T, dir string) {
	t.Helper()
	yarnrc := []byte("nodeLinker: node-modules\n")
	if err := os.WriteFile(filepath.Join(dir, ".yarnrc.yml"), yarnrc, 0o644); err != nil {
		t.Fatalf("writing .yarnrc.yml: %v", err)
	}
}

// discoverWorkspaceMembersInteg detects workspace globs in a package.json and reads
// member spec files. Returns nil if the fixture is not a workspace project.
func discoverWorkspaceMembersInteg(t *testing.T, fixtureDir string, specData []byte) map[string][]byte {
	t.Helper()
	globs, err := npm.ParseWorkspaceGlobs(specData)
	if err != nil || len(globs) == 0 {
		return nil
	}
	members := make(map[string][]byte)
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
	if len(members) == 0 {
		return nil
	}
	return members
}
