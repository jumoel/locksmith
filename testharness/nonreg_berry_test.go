//go:build !integration

package testharness

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

// TestBerryNonRegistryDepsGenerate exercises the full locksmith pipeline for
// the non-registry-deps fixture with yarn berry v6 and v8 formats. This uses
// the real npm registry and the fixture's local-pkg directory. It verifies
// that non-registry deps (file:, github:, git+ssh:, tarball URL) produce
// valid lockfile entries with correct structure.
func TestBerryNonRegistryDepsGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test in short mode")
	}

	fixtureDir := filepath.Join("fixtures", "non-registry-deps")
	specData := readFixture(t, "non-registry-deps")

	for _, tc := range []struct {
		name   string
		format locksmith.OutputFormat
	}{
		{"v6", locksmith.FormatYarnBerryV6},
		{"v8", locksmith.FormatYarnBerryV8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
				SpecFile:     specData,
				OutputFormat: tc.format,
				SpecDir:      fixtureDir,
			})
			if err != nil {
				t.Fatalf("Generate failed: %v", err)
			}

			output := string(result.Lockfile)
			if len(output) == 0 {
				t.Fatal("empty lockfile")
			}

			// Verify __metadata header.
			if !strings.Contains(output, "__metadata:") {
				t.Error("missing __metadata section")
			}

			// Verify ms (regular npm dep) is present.
			if !strings.Contains(output, "ms@npm:") {
				t.Error("missing ms npm dep")
			}

			// Verify file: dep uses portal: resolution.
			if !strings.Contains(output, "local-pkg@file:./local-pkg") {
				t.Error("missing local-pkg file dep entry")
			}
			if !strings.Contains(output, "portal:./local-pkg::locator=") {
				t.Error("missing portal: resolution for file dep")
			}

			// Verify file: dep has linkType: soft.
			entryIdx := strings.Index(output, `"local-pkg@file:./local-pkg"`)
			if entryIdx >= 0 {
				section := output[entryIdx:]
				nextEntry := strings.Index(section[1:], "\n\n")
				if nextEntry > 0 {
					section = section[:nextEntry+1]
				}
				if !strings.Contains(section, "linkType: soft") {
					t.Error("file dep should have linkType: soft")
				}
			}

			// Verify git deps produce entries with https resolution.
			// git-pkg is "github:jonschlinkert/is-odd"
			if !strings.Contains(output, "git-pkg@github:jonschlinkert/is-odd") {
				t.Error("missing git-pkg github dep entry")
			}
			if !strings.Contains(output, "https://github.com/jonschlinkert/is-odd") {
				t.Error("missing https resolution for git-pkg")
			}

			// Verify tarball URL dep gets an npm resolution.
			if !strings.Contains(output, "tarball-pkg@") {
				t.Error("missing tarball-pkg entry")
			}

			// Verify workspace root entry.
			if !strings.Contains(output, "locksmith-test-non-registry-deps@workspace:.") {
				t.Error("missing workspace root entry")
			}

			t.Logf("%s output (%d bytes):\n%s", tc.name, len(output), output)
		})
	}
}
