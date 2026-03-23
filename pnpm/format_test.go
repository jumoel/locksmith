package pnpm

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
)

func TestPnpmLockV9_SimpleProject(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^1.0.0"})
	reg.addVersion("B", "1.0.0", baseTime, nil)
	reg.addVersion("B", "1.2.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "A", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
		},
	}

	resolver := NewResolver()
	result, err := resolver.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	formatter := NewPnpmLockV9Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// Verify top-level structure.
	if !strings.Contains(yaml, "lockfileVersion: '9.0'") {
		t.Error("missing lockfileVersion")
	}
	if !strings.Contains(yaml, "autoInstallPeers: true") {
		t.Error("missing autoInstallPeers setting")
	}
	if !strings.Contains(yaml, "excludeLinksFromLockfile: false") {
		t.Error("missing excludeLinksFromLockfile setting")
	}

	// Verify importers section.
	if !strings.Contains(yaml, "importers:") {
		t.Error("missing importers section")
	}
	if !strings.Contains(yaml, "specifier: ^1.0.0") {
		t.Error("missing specifier for A")
	}

	// Verify packages section has resolution info.
	if !strings.Contains(yaml, "packages:") {
		t.Error("missing packages section")
	}
	if !strings.Contains(yaml, "A@1.0.0:") {
		t.Error("missing A@1.0.0 in packages")
	}
	if !strings.Contains(yaml, "B@1.2.0:") {
		t.Error("missing B@1.2.0 in packages")
	}
	if !strings.Contains(yaml, "integrity: sha512-fake-A-1.0.0") {
		t.Error("missing integrity for A")
	}

	// Verify snapshots section has dependency relationships.
	if !strings.Contains(yaml, "snapshots:") {
		t.Error("missing snapshots section")
	}

	t.Logf("Generated YAML:\n%s", yaml)
}

func TestPnpmLockV9_DevAndOptionalDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("prod-dep", "1.0.0", baseTime, nil)
	reg.addVersion("dev-dep", "2.0.0", baseTime, nil)
	reg.addVersion("opt-dep", "3.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "prod-dep", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "dev-dep", Constraint: "^2.0.0", Type: ecosystem.DepDev},
			{Name: "opt-dep", Constraint: "^3.0.0", Type: ecosystem.DepOptional},
		},
	}

	resolver := NewResolver()
	result, err := resolver.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	formatter := NewPnpmLockV9Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// Verify all dependency groups appear in importers.
	if !strings.Contains(yaml, "dependencies:") {
		t.Error("missing dependencies group in importers")
	}
	if !strings.Contains(yaml, "devDependencies:") {
		t.Error("missing devDependencies group in importers")
	}
	if !strings.Contains(yaml, "optionalDependencies:") {
		t.Error("missing optionalDependencies group in importers")
	}

	t.Logf("Generated YAML:\n%s", yaml)
}

func TestPnpmLockV9_DeterministicOutput(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("Z-pkg", "1.0.0", baseTime, nil)
	reg.addVersion("A-pkg", "2.0.0", baseTime, nil)
	reg.addVersion("M-pkg", "3.0.0", baseTime, map[string]string{
		"Z-pkg": "^1.0.0",
		"A-pkg": "^2.0.0",
	})

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "M-pkg", Constraint: "^3.0.0", Type: ecosystem.DepRegular},
			{Name: "Z-pkg", Constraint: "^1.0.0", Type: ecosystem.DepDev},
		},
	}

	resolver := NewResolver()
	formatter := NewPnpmLockV9Formatter()

	// Format twice and verify byte-identical output.
	result1, err := resolver.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	output1, err := formatter.FormatFromResult(result1, project)
	if err != nil {
		t.Fatalf("first format failed: %v", err)
	}

	result2, err := resolver.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	output2, err := formatter.FormatFromResult(result2, project)
	if err != nil {
		t.Fatalf("second format failed: %v", err)
	}

	if !bytes.Equal(output1, output2) {
		t.Error("output is not deterministic across runs")
		t.Logf("First:\n%s", string(output1))
		t.Logf("Second:\n%s", string(output2))
	}
}

func TestPnpmLockV9_FormatInterfaceReturnsError(t *testing.T) {
	formatter := NewPnpmLockV9Formatter()
	_, err := formatter.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error from Format(), got nil")
	}
	if !strings.Contains(err.Error(), "FormatFromResult") {
		t.Errorf("expected error mentioning FormatFromResult, got: %v", err)
	}
}

func TestPnpmLockV5_NotImplemented(t *testing.T) {
	formatter := NewPnpmLockV5Formatter()
	_, err := formatter.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' error, got: %v", err)
	}
}

func TestPnpmLockV6_NotImplemented(t *testing.T) {
	formatter := NewPnpmLockV6Formatter()
	_, err := formatter.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("expected 'not yet implemented' error, got: %v", err)
	}
}
