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

func TestPnpmLockV5_FormatInterfaceReturnsError(t *testing.T) {
	formatter := NewPnpmLockV5Formatter()
	_, err := formatter.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error from Format(), got nil")
	}
	if !strings.Contains(err.Error(), "FormatFromResult") {
		t.Errorf("expected error mentioning FormatFromResult, got: %v", err)
	}
}

func TestPnpmLockV6_FormatInterfaceReturnsError(t *testing.T) {
	formatter := NewPnpmLockV6Formatter()
	_, err := formatter.Format(nil, nil)
	if err == nil {
		t.Fatal("expected error from Format(), got nil")
	}
	if !strings.Contains(err.Error(), "FormatFromResult") {
		t.Errorf("expected error mentioning FormatFromResult, got: %v", err)
	}
}

func TestPnpmLockV5_SimpleProject(t *testing.T) {
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

	formatter := NewPnpmLockV5Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// Verify lockfileVersion is unquoted 5.4.
	if !strings.Contains(yaml, "lockfileVersion: 5.4") {
		t.Error("missing or wrong lockfileVersion")
	}

	// Verify specifiers section.
	if !strings.Contains(yaml, "specifiers:") {
		t.Error("missing specifiers section")
	}
	if !strings.Contains(yaml, "A: ^1.0.0") {
		t.Error("missing specifier for A")
	}

	// Verify top-level dependencies section.
	if !strings.Contains(yaml, "dependencies:") {
		t.Error("missing dependencies section")
	}

	// Verify NO importers section (v5 does not use importers).
	if strings.Contains(yaml, "importers:") {
		t.Error("v5 should not have importers section")
	}

	// Verify NO snapshots section.
	if strings.Contains(yaml, "snapshots:") {
		t.Error("v5 should not have snapshots section")
	}

	// Verify package keys use /name/version format.
	if !strings.Contains(yaml, "/A/1.0.0:") {
		t.Error("missing /A/1.0.0 in packages (v5 format)")
	}
	if !strings.Contains(yaml, "/B/1.2.0:") {
		t.Error("missing /B/1.2.0 in packages (v5 format)")
	}

	// Verify dev flag is present.
	if !strings.Contains(yaml, "dev: false") {
		t.Error("missing dev: false flag")
	}

	// Verify resolution uses flow style (inline braces).
	if !strings.Contains(yaml, "resolution: {") {
		t.Error("resolution should use flow/inline style")
	}

	t.Logf("Generated V5 YAML:\n%s", yaml)
}

func TestPnpmLockV5_DevDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("prod-dep", "1.0.0", baseTime, nil)
	reg.addVersion("dev-dep", "2.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "prod-dep", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "dev-dep", Constraint: "^2.0.0", Type: ecosystem.DepDev},
		},
	}

	resolver := NewResolver()
	result, err := resolver.ResolveForLockfile(context.Background(), project, reg, ecosystem.ResolveOptions{})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}

	formatter := NewPnpmLockV5Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// V5 separates dependencies and devDependencies at top level.
	if !strings.Contains(yaml, "dependencies:") {
		t.Error("missing dependencies section")
	}
	if !strings.Contains(yaml, "devDependencies:") {
		t.Error("missing devDependencies section")
	}

	// Dev dep package should have dev: true.
	if !strings.Contains(yaml, "dev: true") {
		t.Error("dev-dep should have dev: true")
	}

	t.Logf("Generated V5 YAML:\n%s", yaml)
}

func TestPnpmLockV5_DeterministicOutput(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("Z-pkg", "1.0.0", baseTime, nil)
	reg.addVersion("A-pkg", "2.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "Z-pkg", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "A-pkg", Constraint: "^2.0.0", Type: ecosystem.DepRegular},
		},
	}

	resolver := NewResolver()
	formatter := NewPnpmLockV5Formatter()

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
		t.Error("v5 output is not deterministic across runs")
		t.Logf("First:\n%s", string(output1))
		t.Logf("Second:\n%s", string(output2))
	}
}

func TestPnpmLockV6_SimpleProject(t *testing.T) {
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

	formatter := NewPnpmLockV6Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// Verify lockfileVersion is quoted '6.0'.
	if !strings.Contains(yaml, "lockfileVersion: '6.0'") {
		t.Error("missing or wrong lockfileVersion")
	}

	// Verify settings section.
	if !strings.Contains(yaml, "autoInstallPeers: true") {
		t.Error("missing autoInstallPeers setting")
	}

	// Verify importers section (v6 uses importers like v9).
	if !strings.Contains(yaml, "importers:") {
		t.Error("missing importers section")
	}
	if !strings.Contains(yaml, "specifier: ^1.0.0") {
		t.Error("missing specifier for A in importers")
	}

	// Verify NO snapshots section.
	if strings.Contains(yaml, "snapshots:") {
		t.Error("v6 should not have snapshots section")
	}

	// Verify NO top-level specifiers section (that's v5 only).
	// Count occurrences: specifiers should only appear inside importers.
	specCount := strings.Count(yaml, "specifiers:")
	if specCount > 0 {
		t.Errorf("v6 should not have top-level specifiers section")
	}

	// Verify package keys use /name@version format (with leading slash).
	if !strings.Contains(yaml, "/A@1.0.0:") {
		t.Error("missing /A@1.0.0 in packages (v6 format)")
	}
	if !strings.Contains(yaml, "/B@1.2.0:") {
		t.Error("missing /B@1.2.0 in packages (v6 format)")
	}

	// Verify dev flag is present.
	if !strings.Contains(yaml, "dev: false") {
		t.Error("missing dev: false flag")
	}

	// Verify resolution uses flow style.
	if !strings.Contains(yaml, "resolution: {") {
		t.Error("resolution should use flow/inline style")
	}

	t.Logf("Generated V6 YAML:\n%s", yaml)
}

func TestPnpmLockV6_DevAndOptionalDeps(t *testing.T) {
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

	formatter := NewPnpmLockV6Formatter()
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

	// Dev dep should be marked dev: true.
	if !strings.Contains(yaml, "dev: true") {
		t.Error("dev-dep should have dev: true")
	}

	t.Logf("Generated V6 YAML:\n%s", yaml)
}

func TestPnpmLockV6_DeterministicOutput(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("Z-pkg", "1.0.0", baseTime, nil)
	reg.addVersion("A-pkg", "2.0.0", baseTime, nil)

	project := &ecosystem.ProjectSpec{
		Name:    "test-project",
		Version: "1.0.0",
		Dependencies: []ecosystem.DeclaredDep{
			{Name: "Z-pkg", Constraint: "^1.0.0", Type: ecosystem.DepRegular},
			{Name: "A-pkg", Constraint: "^2.0.0", Type: ecosystem.DepRegular},
		},
	}

	resolver := NewResolver()
	formatter := NewPnpmLockV6Formatter()

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
		t.Error("v6 output is not deterministic across runs")
		t.Logf("First:\n%s", string(output1))
		t.Logf("Second:\n%s", string(output2))
	}
}

func TestPnpmLockV5_TransitiveDeps(t *testing.T) {
	reg := newMockRegistry()
	reg.addVersion("A", "1.0.0", baseTime, map[string]string{"B": "^2.0.0"})
	reg.addVersion("B", "2.1.0", baseTime, map[string]string{"C": "^1.0.0"})
	reg.addVersion("C", "1.3.0", baseTime, nil)

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

	formatter := NewPnpmLockV5Formatter()
	output, err := formatter.FormatFromResult(result, project)
	if err != nil {
		t.Fatalf("format failed: %v", err)
	}

	yaml := string(output)

	// Verify transitive deps are inlined in packages section.
	if !strings.Contains(yaml, "/A/1.0.0:") {
		t.Error("missing /A/1.0.0")
	}
	if !strings.Contains(yaml, "/B/2.1.0:") {
		t.Error("missing /B/2.1.0")
	}
	if !strings.Contains(yaml, "/C/1.3.0:") {
		t.Error("missing /C/1.3.0")
	}

	// Verify A's dependencies include B.
	if !strings.Contains(yaml, "B: 2.1.0") {
		t.Error("A should list B as dependency")
	}

	t.Logf("Generated V5 YAML:\n%s", yaml)
}
