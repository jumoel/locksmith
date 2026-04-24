package npm

import (
	"encoding/json"
	"testing"
)

func TestParseNpmOverrides_Flat(t *testing.T) {
	raw := json.RawMessage(`{"is-number": "6.0.0"}`)
	os, err := ParseNpmOverrides(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Version != "6.0.0" {
		t.Errorf("Version = %q, want %q", r.Version, "6.0.0")
	}
	if r.Parent != "" {
		t.Errorf("Parent = %q, want empty", r.Parent)
	}
	if len(r.Children) != 0 {
		t.Errorf("Children = %d, want 0", len(r.Children))
	}
}

func TestParseNpmOverrides_Nested(t *testing.T) {
	raw := json.RawMessage(`{"is-odd": {"is-number": "6.0.0"}}`)
	os, err := ParseNpmOverrides(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-odd" {
		t.Errorf("Package = %q, want %q", r.Package, "is-odd")
	}
	if len(r.Children) != 1 {
		t.Fatalf("Children = %d, want 1", len(r.Children))
	}
	child := r.Children[0]
	if child.Package != "is-number" {
		t.Errorf("child Package = %q, want %q", child.Package, "is-number")
	}
	if child.Version != "6.0.0" {
		t.Errorf("child Version = %q, want %q", child.Version, "6.0.0")
	}
}

func TestParseNpmOverrides_DollarDep(t *testing.T) {
	raw := json.RawMessage(`{"is-number": "$is-number"}`)
	rootDeps := map[string]string{"is-number": "^5.0.0"}
	os, err := ParseNpmOverrides(raw, rootDeps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Version != "^5.0.0" {
		t.Errorf("Version = %q, want %q (should resolve $ reference)", r.Version, "^5.0.0")
	}
}

func TestParseNpmOverrides_Empty(t *testing.T) {
	// nil raw
	os, err := ParseNpmOverrides(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != nil {
		t.Error("expected nil OverrideSet for nil input")
	}

	// empty object
	os, err = ParseNpmOverrides(json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != nil {
		t.Error("expected nil OverrideSet for empty object")
	}

	// "null" literal
	os, err = ParseNpmOverrides(json.RawMessage(`null`), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != nil {
		t.Error("expected nil OverrideSet for null")
	}
}

func TestParsePnpmOverrides_Flat(t *testing.T) {
	raw := json.RawMessage(`{"is-number": "6.0.0"}`)
	os, err := ParsePnpmOverrides(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Version != "6.0.0" {
		t.Errorf("Version = %q, want %q", r.Version, "6.0.0")
	}
	if r.Parent != "" {
		t.Errorf("Parent = %q, want empty", r.Parent)
	}
}

func TestParsePnpmOverrides_Parent(t *testing.T) {
	raw := json.RawMessage(`{"is-odd>is-number": "6.0.0"}`)
	os, err := ParsePnpmOverrides(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Version != "6.0.0" {
		t.Errorf("Version = %q, want %q", r.Version, "6.0.0")
	}
	if r.Parent != "is-odd" {
		t.Errorf("Parent = %q, want %q", r.Parent, "is-odd")
	}
}

func TestParsePnpmOverrides_Empty(t *testing.T) {
	os, err := ParsePnpmOverrides(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != nil {
		t.Error("expected nil OverrideSet for nil input")
	}
}

func TestParseYarnResolutions_Flat(t *testing.T) {
	raw := json.RawMessage(`{"is-number": "6.0.0"}`)
	os, err := ParseYarnResolutions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Version != "6.0.0" {
		t.Errorf("Version = %q, want %q", r.Version, "6.0.0")
	}
	if r.Parent != "" {
		t.Errorf("Parent = %q, want empty", r.Parent)
	}
}

func TestParseYarnResolutions_Scoped(t *testing.T) {
	raw := json.RawMessage(`{"is-odd/is-number": "6.0.0"}`)
	os, err := ParseYarnResolutions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Parent != "is-odd" {
		t.Errorf("Parent = %q, want %q", r.Parent, "is-odd")
	}
}

func TestParseYarnResolutions_DoubleGlob(t *testing.T) {
	raw := json.RawMessage(`{"**/is-number": "6.0.0"}`)
	os, err := ParseYarnResolutions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Parent != "" {
		t.Errorf("Parent = %q, want empty (** is global)", r.Parent)
	}
}

func TestParseYarnResolutions_Empty(t *testing.T) {
	os, err := ParseYarnResolutions(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os != nil {
		t.Error("expected nil OverrideSet for nil input")
	}
}

func TestParseYarnResolutions_ParentGlob(t *testing.T) {
	raw := json.RawMessage(`{"is-odd/**/is-number": "6.0.0"}`)
	os, err := ParseYarnResolutions(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if os == nil {
		t.Fatal("expected non-nil OverrideSet")
	}
	if len(os.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(os.Rules))
	}
	r := os.Rules[0]
	if r.Package != "is-number" {
		t.Errorf("Package = %q, want %q", r.Package, "is-number")
	}
	if r.Parent != "is-odd" {
		t.Errorf("Parent = %q, want %q", r.Parent, "is-odd")
	}
}
