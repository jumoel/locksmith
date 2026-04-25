//go:build !integration

package testharness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestPlatformFiltering verifies that platform-specific optional dependencies
// are correctly filtered from generated lockfiles. The platform-specific fixture
// uses esbuild, which has @esbuild/OS-ARCH optional subpackages.
func TestPlatformFiltering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := readFixture(t, "platform-specific")

	for _, tc := range []struct {
		name     string
		format   locksmith.OutputFormat
		checkFn  func(t *testing.T, lockfile []byte)
	}{
		{
			name:   "npm-v3",
			format: locksmith.FormatPackageLockV3,
			checkFn: func(t *testing.T, lockfile []byte) {
				var parsed struct {
					Packages map[string]json.RawMessage `json:"packages"`
				}
				if err := json.Unmarshal(lockfile, &parsed); err != nil {
					t.Fatalf("parsing lockfile: %v", err)
				}

				// @esbuild/linux-x64 should be present.
				linuxFound := false
				for path := range parsed.Packages {
					if strings.Contains(path, "@esbuild/linux-x64") {
						linuxFound = true
					}
					// darwin-arm64 and win32 should NOT be present.
					if strings.Contains(path, "@esbuild/darwin-arm64") {
						t.Error("@esbuild/darwin-arm64 should be filtered for linux/x64")
					}
					if strings.Contains(path, "@esbuild/win32-x64") {
						t.Error("@esbuild/win32-x64 should be filtered for linux/x64")
					}
				}
				if !linuxFound {
					t.Error("@esbuild/linux-x64 should be present for linux/x64 platform")
				}
			},
		},
		{
			name:   "pnpm-v9",
			format: locksmith.FormatPnpmLockV9,
			checkFn: func(t *testing.T, lockfile []byte) {
				output := string(lockfile)

				if !strings.Contains(output, "@esbuild/linux-x64") {
					t.Error("@esbuild/linux-x64 should be present for linux/x64 platform")
				}
				if strings.Contains(output, "@esbuild/darwin-arm64") {
					t.Error("@esbuild/darwin-arm64 should be filtered for linux/x64")
				}
				if strings.Contains(output, "@esbuild/win32-x64") {
					t.Error("@esbuild/win32-x64 should be filtered for linux/x64")
				}
			},
		},
		{
			name:   "yarn-berry-v8",
			format: locksmith.FormatYarnBerryV8,
			checkFn: func(t *testing.T, lockfile []byte) {
				output := string(lockfile)

				if !strings.Contains(output, "@esbuild/linux-x64") {
					t.Error("@esbuild/linux-x64 should be present for linux/x64 platform")
				}
				if strings.Contains(output, "@esbuild/darwin-arm64") {
					t.Error("@esbuild/darwin-arm64 should be filtered for linux/x64")
				}
				if strings.Contains(output, "@esbuild/win32-x64") {
					t.Error("@esbuild/win32-x64 should be filtered for linux/x64")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: tc.format,
				Platform:     "linux/x64",
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			if len(result.Lockfile) == 0 {
				t.Fatal("empty lockfile")
			}

			tc.checkFn(t, result.Lockfile)
		})
	}
}

// TestPlatformFiltering_NoPlatform verifies that WITHOUT a platform filter, all
// platform-specific packages are included.
func TestPlatformFiltering_NoPlatform(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	specData := readFixture(t, "platform-specific")

	result, err := locksmith.Generate(context.Background(), locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPackageLockV3,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	var parsed struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(result.Lockfile, &parsed); err != nil {
		t.Fatalf("parsing lockfile: %v", err)
	}

	// Without platform filter, multiple @esbuild/* packages should be present.
	esbuildCount := 0
	for path := range parsed.Packages {
		if strings.HasPrefix(path, "node_modules/@esbuild/") {
			esbuildCount++
		}
	}
	if esbuildCount < 3 {
		t.Errorf("expected multiple @esbuild/* packages without platform filter, got %d", esbuildCount)
	}
}
