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
		t.Run(string(vc.Format), func(t *testing.T) {
			t.Parallel()
			for _, fixture := range allFixtures {
				fixture := fixture
				t.Run(fixture, func(t *testing.T) {
					// Skip specific PM version crashes (not locksmith bugs).
					if fixture == "aliased-dep" && vc.PMName == "npm" && vc.PMVersion == "6" {
						t.Skip("npm 6 crashes on npm: alias syntax (fetchSpec undefined)")
					}
					t.Parallel()
					pmTag := vc.PMName + "_" + vc.PMVersion
					t.Run(pmTag, func(t *testing.T) {
						runVerification(t, vc, fixture)
					})
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
	specData, err := os.ReadFile(filepath.Join("fixtures", fixture, "package.json"))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", fixture, err)
	}

	// Generate lockfile targeting the Docker runner's platform (linux/x64).
	ctx := context.Background()
	fixtureDir := filepath.Join("fixtures", fixture)
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: vc.Format,
		Platform:     "linux/x64",
		SpecDir:      fixtureDir,
	})
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
