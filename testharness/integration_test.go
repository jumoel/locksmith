//go:build integration

package testharness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

const dockerImage = "locksmith-test-runner"

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
	return names
}

func TestNpmPackageLockV3(t *testing.T) {
	for _, fixture := range fixtures(t) {
		t.Run(fixture, func(t *testing.T) {
			testNpmLockfile(t, fixture, locksmith.FormatPackageLockV3, "package-lock.json")
		})
	}
}

func TestNpmShrinkwrap(t *testing.T) {
	for _, fixture := range fixtures(t) {
		t.Run(fixture, func(t *testing.T) {
			testNpmLockfile(t, fixture, locksmith.FormatNpmShrinkwrap, "npm-shrinkwrap.json")
		})
	}
}

func TestPnpmLockV9(t *testing.T) {
	for _, fixture := range fixtures(t) {
		t.Run(fixture, func(t *testing.T) {
			testPnpmLockfile(t, fixture)
		})
	}
}

func testNpmLockfile(t *testing.T, fixture string, format locksmith.OutputFormat, filename string) {
	t.Helper()

	// Read fixture package.json.
	specPath := filepath.Join("fixtures", fixture, "package.json")
	specData, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	// Generate lockfile.
	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: format,
	})
	if err != nil {
		t.Fatalf("generating lockfile: %v", err)
	}

	// Verify it is valid JSON.
	var parsed map[string]interface{}
	if err := json.Unmarshal(result.Lockfile, &parsed); err != nil {
		t.Fatalf("generated lockfile is not valid JSON: %v", err)
	}

	// Write to temp directory for npm ci.
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, filename), result.Lockfile, 0o644); err != nil {
		t.Fatal(err)
	}

	// Test with multiple npm versions.
	// package-lock.json v3 works with npm 7+; shrinkwrap is tested with npm 10 only.
	npmVersions := []string{"10"}
	if format == locksmith.FormatPackageLockV3 {
		npmVersions = []string{"7", "8", "9", "10"}
	}

	for _, npmVersion := range npmVersions {
		t.Run("npm"+npmVersion, func(t *testing.T) {
			runNpmCi(t, tmpDir, npmVersion)
		})
	}
}

func testPnpmLockfile(t *testing.T, fixture string) {
	t.Helper()

	specPath := filepath.Join("fixtures", fixture, "package.json")
	specData, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	ctx := context.Background()
	result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPnpmLockV9,
	})
	if err != nil {
		t.Fatalf("generating pnpm lockfile: %v", err)
	}

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), specData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pnpm-lock.yaml"), result.Lockfile, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run pnpm install --frozen-lockfile in Docker.
	runPnpmFrozen(t, tmpDir)
}

func runNpmCi(t *testing.T, projectDir, npmVersion string) {
	t.Helper()

	cmd := exec.Command("docker", "run", "--rm",
		"--platform", "linux/amd64",
		"-v", projectDir+":/workspace",
		"-w", "/workspace",
		dockerImage,
		"run-npm", npmVersion, "ci", "--ignore-scripts",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("npm %s ci failed:\n%s\nerror: %v", npmVersion, string(output), err)
	}
	t.Logf("npm %s ci succeeded: %s", npmVersion, strings.TrimSpace(string(output)))
}

func runPnpmFrozen(t *testing.T, projectDir string) {
	t.Helper()

	cmd := exec.Command("docker", "run", "--rm",
		"--platform", "linux/amd64",
		"-v", projectDir+":/workspace",
		"-w", "/workspace",
		dockerImage,
		"pnpm", "install", "--frozen-lockfile",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pnpm install --frozen-lockfile failed:\n%s\nerror: %v", string(output), err)
	}
	t.Logf("pnpm install --frozen-lockfile succeeded: %s", strings.TrimSpace(string(output)))
}
