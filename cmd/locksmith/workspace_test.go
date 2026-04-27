package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePnpmCatalogs_DefaultCatalogOnly(t *testing.T) {
	data := []byte(`
packages:
  - packages/*
catalog:
  lodash: "^4.17.0"
  react: "^18.0.0"
`)
	result, err := parsePnpmCatalogs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	def, ok := result["default"]
	if !ok {
		t.Fatal("expected 'default' catalog")
	}
	if def["lodash"] != "^4.17.0" {
		t.Errorf("lodash = %q, want %q", def["lodash"], "^4.17.0")
	}
	if def["react"] != "^18.0.0" {
		t.Errorf("react = %q, want %q", def["react"], "^18.0.0")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 catalog, got %d", len(result))
	}
}

func TestParsePnpmCatalogs_NamedCatalogsOnly(t *testing.T) {
	data := []byte(`
packages:
  - packages/*
catalogs:
  special:
    lodash: "^3.10.0"
  react17:
    react: "^17.0.0"
`)
	result, err := parsePnpmCatalogs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["special"]["lodash"] != "^3.10.0" {
		t.Errorf("special/lodash = %q, want %q", result["special"]["lodash"], "^3.10.0")
	}
	if result["react17"]["react"] != "^17.0.0" {
		t.Errorf("react17/react = %q, want %q", result["react17"]["react"], "^17.0.0")
	}
	if _, ok := result["default"]; ok {
		t.Error("should not have 'default' catalog when only named catalogs exist")
	}
}

func TestParsePnpmCatalogs_BothDefaultAndNamed(t *testing.T) {
	data := []byte(`
catalog:
  lodash: "^4.17.0"
catalogs:
  special:
    lodash: "^3.10.0"
`)
	result, err := parsePnpmCatalogs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result["default"]["lodash"] != "^4.17.0" {
		t.Errorf("default/lodash = %q, want %q", result["default"]["lodash"], "^4.17.0")
	}
	if result["special"]["lodash"] != "^3.10.0" {
		t.Errorf("special/lodash = %q, want %q", result["special"]["lodash"], "^3.10.0")
	}
}

func TestParsePnpmCatalogs_NeitherSection(t *testing.T) {
	data := []byte(`
packages:
  - packages/*
`)
	result, err := parsePnpmCatalogs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestParsePnpmCatalogs_EmptyYaml(t *testing.T) {
	data := []byte(``)
	result, err := parsePnpmCatalogs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestDiscoverPnpmCatalogs_WithCatalogs(t *testing.T) {
	tmp := t.TempDir()
	specPath := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wsYaml := `
packages:
  - packages/*
catalog:
  is-number: "^7.0.0"
catalogs:
  special:
    lodash: "^3.10.0"
`
	if err := os.WriteFile(filepath.Join(tmp, "pnpm-workspace.yaml"), []byte(wsYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := discoverPnpmCatalogs(specPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if catalogs == nil {
		t.Fatal("expected non-nil catalogs")
	}
	if catalogs["default"]["is-number"] != "^7.0.0" {
		t.Errorf("default/is-number = %q, want %q", catalogs["default"]["is-number"], "^7.0.0")
	}
	if catalogs["special"]["lodash"] != "^3.10.0" {
		t.Errorf("special/lodash = %q, want %q", catalogs["special"]["lodash"], "^3.10.0")
	}
}

func TestDiscoverPnpmCatalogs_NoPnpmWorkspace(t *testing.T) {
	tmp := t.TempDir()
	specPath := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := discoverPnpmCatalogs(specPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if catalogs != nil {
		t.Errorf("expected nil catalogs when no pnpm-workspace.yaml, got %v", catalogs)
	}
}

func TestDiscoverPnpmCatalogs_NoCatalogsInWorkspace(t *testing.T) {
	tmp := t.TempDir()
	specPath := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wsYaml := `
packages:
  - packages/*
`
	if err := os.WriteFile(filepath.Join(tmp, "pnpm-workspace.yaml"), []byte(wsYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	catalogs, err := discoverPnpmCatalogs(specPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if catalogs != nil {
		t.Errorf("expected nil catalogs when workspace has no catalogs, got %v", catalogs)
	}
}

func TestGenerateCmd_PnpmCatalogs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping pnpm catalogs test in short mode (needs real registry)")
	}

	tmp := t.TempDir()

	rootSpec := `{"name":"pnpm-catalog-test","version":"1.0.0","dependencies":{"is-number":"catalog:"}}`
	if err := os.WriteFile(filepath.Join(tmp, "package.json"), []byte(rootSpec), 0o644); err != nil {
		t.Fatal(err)
	}

	wsYaml := "packages:\n  - \"pkgs/*\"\ncatalog:\n  is-number: \"^7.0.0\"\n"
	if err := os.WriteFile(filepath.Join(tmp, "pnpm-workspace.yaml"), []byte(wsYaml), 0o644); err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(tmp, "pnpm-lock.yaml")

	root := rootCmd()
	root.SetArgs([]string{
		"generate",
		"--spec", filepath.Join(tmp, "package.json"),
		"--format", "pnpm-lock-v9",
		"--output", outputFile,
	})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	if err := root.Execute(); err != nil {
		t.Fatalf("generate command failed: %v", err)
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	output := string(data)

	// The lockfile should contain the resolved is-number package.
	if !strings.Contains(output, "is-number") {
		t.Errorf("output should contain 'is-number' but does not.\nOutput:\n%s", output)
	}

	// The lockfile should preserve the catalog: specifier in importers.
	if !strings.Contains(output, "catalog:") {
		t.Errorf("output should contain 'catalog:' specifier but does not.\nOutput:\n%s", output)
	}
}
