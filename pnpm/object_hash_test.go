package pnpm

import (
	"encoding/json"
	"testing"
)

// Ground truth values computed by running the npm object-hash library v3.0.0
// with options: respectType=false, algorithm=sha256, encoding=base64,
// unorderedObjects=true, unorderedArrays=true, unorderedSets=true.

func TestObjectHashSHA256_FixtureExtensions(t *testing.T) {
	// The actual pnpm-package-extensions fixture.
	raw := json.RawMessage(`{"is-odd@*":{"dependencies":{"ms":"^2.1.0"}}}`)
	got, err := objectHashSHA256(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "sha256-RLbmtJ1E0wdYtArm43+SJklp6yt8ZrjNs24L/qMIimU="
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashSHA256_SimpleExtension(t *testing.T) {
	raw := json.RawMessage(`{"foo@^1.0.0":{"dependencies":{"bar":"^2.0.0"}}}`)
	got, err := objectHashSHA256(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "sha256-9ya27pg2vGl4+WM38k9yPq/SzeQBfhhuH02ADVnUYPk="
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashSHA256_MultipleExtensions(t *testing.T) {
	raw := json.RawMessage(`{"pkg-a@*":{"dependencies":{"dep1":"^1.0.0"}},"pkg-b@^2.0.0":{"peerDependencies":{"peer1":"^3.0.0"}}}`)
	got, err := objectHashSHA256(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "sha256-W0ZeVpFAqirwmEdjuzWx+YZLy8HhBWT7p6/VErkapI8="
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashSHA256_EmptyObject(t *testing.T) {
	raw := json.RawMessage(`{}`)
	got, err := objectHashSHA256(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "sha256-PV5SIH4Pb4Rd3cOKB0TmpocWXJ4M8i9zrFifAtVgtUQ="
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashMD5_FixtureExtensions(t *testing.T) {
	raw := json.RawMessage(`{"is-odd@*":{"dependencies":{"ms":"^2.1.0"}}}`)
	got, err := objectHashMD5(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "9268f5b32a6691c02b6445daeb76002f"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashMD5_SimpleExtension(t *testing.T) {
	raw := json.RawMessage(`{"foo@^1.0.0":{"dependencies":{"bar":"^2.0.0"}}}`)
	got, err := objectHashMD5(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "2d5a3bcc8eb8c1d1eb19b874985a8ea7"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestObjectHashSerialize_FixtureExtensions(t *testing.T) {
	raw := json.RawMessage(`{"is-odd@*":{"dependencies":{"ms":"^2.1.0"}}}`)
	got, err := objectHashSerialize(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "object:1:string:8:is-odd@*:object:1:string:12:dependencies:object:1:string:2:ms:string:6:^2.1.0,,,"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
