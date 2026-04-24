//go:build !integration

package testharness

import (
	"context"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

func TestPnpmExtensions_IsOddWithMs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	specData := readFixture(t, "pnpm-package-extensions")
	opts := locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPnpmLockV9,
	}

	result, err := locksmith.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	content := string(result.Lockfile)

	// is-odd should be in the lockfile.
	if !strings.Contains(content, "is-odd") {
		t.Error("is-odd not found in pnpm lockfile")
	}

	// ms should be in the lockfile as a dependency injected via packageExtensions.
	// The real is-odd package does NOT depend on ms, so if ms is present
	// it must have been injected by the extension.
	if !strings.Contains(content, "ms@") || !strings.Contains(content, "ms") {
		t.Error("ms not found in pnpm lockfile - packageExtensions not applied")
	}

	// Verify ms appears as a dependency of is-odd in the lockfile structure.
	// In pnpm lockfile format, dependencies are listed under the package entry.
	// Look for "ms:" somewhere after "is-odd" in the packages section.
	lines := strings.Split(content, "\n")
	inIsOddPackage := false
	foundMsUnderIsOdd := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "is-odd@") && !strings.HasPrefix(trimmed, "#") {
			inIsOddPackage = true
		}
		if inIsOddPackage {
			// Look for ms as a dependency entry.
			if strings.HasPrefix(trimmed, "ms:") || strings.Contains(trimmed, "'ms':") {
				foundMsUnderIsOdd = true
				break
			}
			// A new top-level package entry ends the is-odd section.
			// In pnpm v9, package keys start without indentation.
			if len(line) > 0 && line[0] != ' ' && !strings.Contains(line, "is-odd") {
				inIsOddPackage = false
			}
		}
	}

	if !foundMsUnderIsOdd {
		t.Error("ms not found as a dependency of is-odd in the lockfile - packageExtensions injection not reflected in lockfile structure")
		if len(content) < 5000 {
			t.Logf("Full lockfile:\n%s", content)
		} else {
			t.Logf("First 5000 bytes of lockfile:\n%s", content[:5000])
		}
	}
}

func TestPnpmExtensions_AllFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	pnpmFormats := []locksmith.OutputFormat{
		locksmith.FormatPnpmLockV5,
		locksmith.FormatPnpmLockV6,
		locksmith.FormatPnpmLockV9,
	}

	specData := readFixture(t, "pnpm-package-extensions")

	for _, format := range pnpmFormats {
		format := format
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			opts := locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: format,
			}

			result, err := locksmith.Generate(context.Background(), opts)
			if err != nil {
				t.Fatalf("Generate(%s) failed: %v", format, err)
			}

			content := string(result.Lockfile)

			// ms must appear in the lockfile for all pnpm formats.
			if !strings.Contains(content, "ms") {
				t.Errorf("ms not found in %s lockfile - packageExtensions not applied", format)
			}
		})
	}
}
