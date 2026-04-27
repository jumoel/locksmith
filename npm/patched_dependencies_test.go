package npm

import (
	"encoding/json"
	"testing"
)

func TestParsePatchedDependencies_ValidSingle(t *testing.T) {
	raw := json.RawMessage(`{"is-odd@3.0.1": "patches/is-odd@3.0.1.patch"}`)
	m, err := ParsePatchedDependencies(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
	if m["is-odd@3.0.1"] != "patches/is-odd@3.0.1.patch" {
		t.Errorf("unexpected value: %q", m["is-odd@3.0.1"])
	}
}

func TestParsePatchedDependencies_EmptyObject(t *testing.T) {
	raw := json.RawMessage(`{}`)
	m, err := ParsePatchedDependencies(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 0 {
		t.Errorf("expected 0 entries, got %d", len(m))
	}
}

func TestParsePatchedDependencies_InvalidJSON(t *testing.T) {
	raw := json.RawMessage(`{not valid json`)
	_, err := ParsePatchedDependencies(raw)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestParsePatchedDependencies_MultipleEntries(t *testing.T) {
	raw := json.RawMessage(`{
		"is-odd@3.0.1": "patches/is-odd@3.0.1.patch",
		"lodash@4.17.21": "patches/lodash@4.17.21.patch",
		"@scope/pkg@1.0.0": "patches/@scope-pkg@1.0.0.patch"
	}`)
	m, err := ParsePatchedDependencies(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m))
	}
	if m["is-odd@3.0.1"] != "patches/is-odd@3.0.1.patch" {
		t.Errorf("is-odd: %q", m["is-odd@3.0.1"])
	}
	if m["lodash@4.17.21"] != "patches/lodash@4.17.21.patch" {
		t.Errorf("lodash: %q", m["lodash@4.17.21"])
	}
	if m["@scope/pkg@1.0.0"] != "patches/@scope-pkg@1.0.0.patch" {
		t.Errorf("@scope/pkg: %q", m["@scope/pkg@1.0.0"])
	}
}
