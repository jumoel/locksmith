package npm

import (
	"encoding/json"
	"testing"
)

func TestParsePeerDependencyRules_Full(t *testing.T) {
	raw := json.RawMessage(`{
		"ignoreMissing": ["@babel/*", "webpack"],
		"allowedVersions": {
			"react": "17 || 18",
			"typescript": ">=4.0.0"
		},
		"allowAny": ["eslint", "prettier"]
	}`)

	rules, err := ParsePeerDependencyRules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}

	// IgnoreMissing
	if len(rules.IgnoreMissing) != 2 {
		t.Fatalf("expected 2 ignoreMissing patterns, got %d", len(rules.IgnoreMissing))
	}
	wantIgnore := map[string]bool{"@babel/*": true, "webpack": true}
	for _, p := range rules.IgnoreMissing {
		if !wantIgnore[p] {
			t.Errorf("unexpected ignoreMissing pattern: %s", p)
		}
	}

	// AllowedVersions
	if len(rules.AllowedVersions) != 2 {
		t.Fatalf("expected 2 allowedVersions entries, got %d", len(rules.AllowedVersions))
	}
	if rules.AllowedVersions["react"] != "17 || 18" {
		t.Errorf("react allowedVersion = %q, want %q", rules.AllowedVersions["react"], "17 || 18")
	}
	if rules.AllowedVersions["typescript"] != ">=4.0.0" {
		t.Errorf("typescript allowedVersion = %q, want %q", rules.AllowedVersions["typescript"], ">=4.0.0")
	}

	// AllowAny
	if len(rules.AllowAny) != 2 {
		t.Fatalf("expected 2 allowAny entries, got %d", len(rules.AllowAny))
	}
	wantAny := map[string]bool{"eslint": true, "prettier": true}
	for _, p := range rules.AllowAny {
		if !wantAny[p] {
			t.Errorf("unexpected allowAny entry: %s", p)
		}
	}
}

func TestParsePeerDependencyRules_IgnoreMissingOnly(t *testing.T) {
	raw := json.RawMessage(`{
		"ignoreMissing": ["react", "@types/*"]
	}`)

	rules, err := ParsePeerDependencyRules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}

	if len(rules.IgnoreMissing) != 2 {
		t.Fatalf("expected 2 ignoreMissing patterns, got %d", len(rules.IgnoreMissing))
	}
	if len(rules.AllowedVersions) != 0 {
		t.Errorf("expected 0 allowedVersions, got %d", len(rules.AllowedVersions))
	}
	if len(rules.AllowAny) != 0 {
		t.Errorf("expected 0 allowAny, got %d", len(rules.AllowAny))
	}
}

func TestParsePeerDependencyRules_AllowedVersionsOnly(t *testing.T) {
	raw := json.RawMessage(`{
		"allowedVersions": {
			"react": "^18.0.0"
		}
	}`)

	rules, err := ParsePeerDependencyRules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules == nil {
		t.Fatal("expected non-nil rules")
	}

	if len(rules.IgnoreMissing) != 0 {
		t.Errorf("expected 0 ignoreMissing, got %d", len(rules.IgnoreMissing))
	}
	if len(rules.AllowedVersions) != 1 {
		t.Fatalf("expected 1 allowedVersions entry, got %d", len(rules.AllowedVersions))
	}
	if rules.AllowedVersions["react"] != "^18.0.0" {
		t.Errorf("react allowedVersion = %q, want %q", rules.AllowedVersions["react"], "^18.0.0")
	}
	if len(rules.AllowAny) != 0 {
		t.Errorf("expected 0 allowAny, got %d", len(rules.AllowAny))
	}
}

func TestParsePeerDependencyRules_Empty(t *testing.T) {
	// nil input
	rules, err := ParsePeerDependencyRules(nil)
	if err != nil {
		t.Fatalf("unexpected error for nil: %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil for nil input, got %+v", rules)
	}

	// empty bytes
	rules, err = ParsePeerDependencyRules(json.RawMessage(``))
	if err != nil {
		t.Fatalf("unexpected error for empty: %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil for empty input, got %+v", rules)
	}

	// null
	rules, err = ParsePeerDependencyRules(json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("unexpected error for null: %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil for null input, got %+v", rules)
	}
}

func TestParsePeerDependencyRules_EmptyObject(t *testing.T) {
	raw := json.RawMessage(`{}`)

	rules, err := ParsePeerDependencyRules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules == nil {
		t.Fatal("expected non-nil rules for empty object")
	}

	if len(rules.IgnoreMissing) != 0 {
		t.Errorf("expected 0 ignoreMissing, got %d", len(rules.IgnoreMissing))
	}
	if len(rules.AllowedVersions) != 0 {
		t.Errorf("expected 0 allowedVersions, got %d", len(rules.AllowedVersions))
	}
	if len(rules.AllowAny) != 0 {
		t.Errorf("expected 0 allowAny, got %d", len(rules.AllowAny))
	}
}
