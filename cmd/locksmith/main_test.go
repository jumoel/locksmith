package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jumoel/locksmith"
)

func TestRootCmd_HasSubcommands(t *testing.T) {
	root := rootCmd()
	cmds := root.Commands()

	found := map[string]bool{}
	for _, c := range cmds {
		found[c.Name()] = true
	}

	for _, want := range []string{"generate", "version"} {
		if !found[want] {
			t.Errorf("rootCmd missing subcommand %q; got %v", want, found)
		}
	}
}

func TestVersionCmd(t *testing.T) {
	// versionCmd uses fmt.Printf which writes to os.Stdout, not the cobra
	// writer. We verify it executes without error; the output content is
	// implicitly validated by TestVersionCmd_Output below which captures
	// stdout via os.Pipe.
	root := rootCmd()
	root.SetArgs([]string{"version"})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	// Capture stdout since versionCmd uses fmt.Printf.
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	execErr := root.Execute()

	w.Close()
	os.Stdout = oldStdout

	var captured bytes.Buffer
	captured.ReadFrom(r)

	if execErr != nil {
		t.Fatalf("version command failed: %v", execErr)
	}

	out := captured.String()
	if !strings.Contains(out, "locksmith") {
		t.Errorf("version output should contain 'locksmith', got %q", out)
	}
}

func TestIsValidFormat(t *testing.T) {
	// All known formats should be valid.
	for _, f := range locksmith.AllFormats() {
		if !isValidFormat(f) {
			t.Errorf("isValidFormat(%q) = false, want true", f)
		}
	}

	// Invalid inputs.
	for _, bad := range []string{"invalid", "", "json", "npm", "PACKAGE-LOCK-V3"} {
		if isValidFormat(locksmith.OutputFormat(bad)) {
			t.Errorf("isValidFormat(%q) = true, want false", bad)
		}
	}
}

func TestValidFormatsStr(t *testing.T) {
	s := validFormatsStr()

	for _, want := range []string{"package-lock-v3", "pnpm-lock-v9", "yarn-classic", "bun-lock"} {
		if !strings.Contains(s, want) {
			t.Errorf("validFormatsStr() = %q, missing %q", s, want)
		}
	}
}

func TestGenerateCmd_RequiredFlags(t *testing.T) {
	root := rootCmd()
	root.SetArgs([]string{"generate"})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when required flags are missing")
	}

	msg := err.Error() + buf.String()
	if !strings.Contains(strings.ToLower(msg), "spec") && !strings.Contains(strings.ToLower(msg), "format") {
		t.Errorf("error should mention 'spec' or 'format', got %q", msg)
	}
}

func TestGenerateCmd_InvalidFormat(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := rootCmd()
	root.SetArgs([]string{"generate", "--spec", specFile, "--format", "invalid-format"})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid format")
	}

	if !strings.Contains(err.Error(), "unknown format") {
		t.Errorf("error should contain 'unknown format', got %q", err.Error())
	}
}

func TestGenerateCmd_CutoffDateParsing_Invalid(t *testing.T) {
	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	if err := os.WriteFile(specFile, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	root := rootCmd()
	root.SetArgs([]string{"generate", "--spec", specFile, "--format", "bun-lock", "--cutoff", "not-a-date"})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for invalid cutoff date")
	}

	if !strings.Contains(err.Error(), "RFC3339 or YYYY-MM-DD") {
		t.Errorf("error should mention 'RFC3339 or YYYY-MM-DD', got %q", err.Error())
	}
}

func TestGenerateCmd_RealGeneration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real generation test in short mode")
	}

	tmp := t.TempDir()
	specFile := filepath.Join(tmp, "package.json")
	spec := `{"name":"test","version":"1.0.0","dependencies":{"is-odd":"^3.0.0"}}`
	if err := os.WriteFile(specFile, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	outputFile := filepath.Join(tmp, "bun.lock")

	root := rootCmd()
	root.SetArgs([]string{"generate", "--spec", specFile, "--format", "bun-lock", "--output", outputFile})
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)

	if err := root.Execute(); err != nil {
		t.Fatalf("generate command failed: %v", err)
	}

	info, err := os.Stat(outputFile)
	if err != nil {
		t.Fatalf("output file does not exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
}
