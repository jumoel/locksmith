//go:build !integration

package testharness

import (
	"context"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

func TestPnpmPeerRules_IgnoreMissing(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	specData := readFixture(t, "pnpm-peer-rules")
	opts := locksmith.GenerateOptions{
		SpecFile:     specData,
		OutputFormat: locksmith.FormatPnpmLockV9,
	}

	result, err := locksmith.Generate(context.Background(), opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	content := string(result.Lockfile)

	// react-dom should be present - it's a direct dependency.
	if !strings.Contains(content, "react-dom") {
		t.Error("react-dom not found in pnpm lockfile")
	}

	// react SHOULD be present - ignoreMissing suppresses errors for unresolvable
	// peers but doesn't prevent auto-installation. react-dom has a peer dep on
	// react, and with autoInstallPeers=true (pnpm default), react gets auto-installed.
	reactFound := false
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "react@") && !strings.HasPrefix(trimmed, "react-") {
			reactFound = true
			break
		}
	}
	if !reactFound {
		t.Error("react should be auto-installed (ignoreMissing suppresses errors, doesn't prevent installation)")
	}
}

func TestPnpmPeerRules_AllFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real registry test")
	}

	pnpmFormats := []locksmith.OutputFormat{
		locksmith.FormatPnpmLockV5,
		locksmith.FormatPnpmLockV6,
		locksmith.FormatPnpmLockV9,
	}

	specData := readFixture(t, "pnpm-peer-rules")

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

			// react-dom should appear in all formats.
			if !strings.Contains(content, "react-dom") {
				t.Errorf("react-dom not found in %s lockfile", format)
			}
		})
	}
}
