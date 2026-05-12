package npm

import (
	"encoding/json"
	"testing"
)

// TestVersionScripts_NonStringValue_DoesNotError covers a real packument quirk:
// coveralls@2.1.0 publishes `scripts.blanket` as an object (blanket.js coverage
// config), not a string. A strict map[string]string parser fails the entire
// packument, taking out anything that resolves coveralls transitively.
// Locksmith parses scripts as map[string]json.RawMessage and only checks key
// presence, mirroring npm's tolerant behavior.
func TestVersionScripts_NonStringValue_DoesNotError(t *testing.T) {
	// Verbatim shape from coveralls@2.1.0 on registry.npmjs.org.
	body := []byte(`{
		"name": "coveralls",
		"version": "2.1.0",
		"scripts": {
			"test": "make test",
			"blanket": {
				"pattern": "lib",
				"data-cover-never": "node_modules"
			}
		}
	}`)

	var v Version
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("Unmarshal: unexpected error %v", err)
	}
	if _, ok := v.Scripts["blanket"]; !ok {
		t.Errorf("Scripts[blanket] missing; got keys %v", keysOf(v.Scripts))
	}
	if _, ok := v.Scripts["test"]; !ok {
		t.Errorf("Scripts[test] missing; got keys %v", keysOf(v.Scripts))
	}
}

// TestVersionScripts_InstallHooksDetected confirms HasInstallScript still
// works after the type change.
func TestVersionScripts_InstallHooksDetected(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "postinstall present",
			body: `{"scripts":{"postinstall":"node build.js","test":"jest"}}`,
			want: true,
		},
		{
			name: "preinstall present",
			body: `{"scripts":{"preinstall":"echo hi"}}`,
			want: true,
		},
		{
			name: "install present",
			body: `{"scripts":{"install":"node-gyp rebuild"}}`,
			want: true,
		},
		{
			name: "no install hooks",
			body: `{"scripts":{"test":"jest","build":"tsc"}}`,
			want: false,
		},
		{
			name: "no scripts at all",
			body: `{}`,
			want: false,
		},
		{
			name: "install hook with object value still detected",
			body: `{"scripts":{"postinstall":{"weird":"shape"}}}`,
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var v Version
			if err := json.Unmarshal([]byte(tc.body), &v); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got := v.HasInstallScript(); got != tc.want {
				t.Errorf("HasInstallScript() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPackument_RealCoverallsShape_FullPackumentParses fetches the live
// coveralls packument and asserts a full parse succeeds. Skipped under -short.
func TestPackument_FullParse_TolerantScripts(t *testing.T) {
	// A minimal multi-version packument containing the malformed scripts shape.
	body := []byte(`{
		"_id": "coveralls",
		"name": "coveralls",
		"dist-tags": {"latest": "3.1.1"},
		"versions": {
			"2.1.0": {
				"name": "coveralls",
				"version": "2.1.0",
				"scripts": {"test": "make test", "blanket": {"pattern": "lib"}},
				"dist": {"tarball": "https://example.com/coveralls-2.1.0.tgz"}
			},
			"3.1.1": {
				"name": "coveralls",
				"version": "3.1.1",
				"scripts": {"test": "jest", "postinstall": "node hello.js"},
				"dist": {"tarball": "https://example.com/coveralls-3.1.1.tgz"}
			}
		},
		"time": {"2.1.0": "2014-04-25T00:00:00Z", "3.1.1": "2021-05-19T00:00:00Z"}
	}`)
	var p Packument
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("Packument parse failed on real-world scripts shape: %v", err)
	}
	if len(p.Versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(p.Versions))
	}
	v311 := p.Versions["3.1.1"]
	if !v311.HasInstallScript() {
		t.Error("3.1.1 should have install script")
	}
	v210 := p.Versions["2.1.0"]
	if v210.HasInstallScript() {
		t.Error("2.1.0 should not have install script")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
