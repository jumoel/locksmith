package locksmith

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jumoel/locksmith/ecosystem"
)

// ---------------------------------------------------------------------------
// unreachableKeys
// ---------------------------------------------------------------------------

func TestUnreachableKeys_NilGraph(t *testing.T) {
	got := unreachableKeys(nil, map[string]bool{"foo@1.0.0": true})
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestUnreachableKeys_NilRoot(t *testing.T) {
	g := &ecosystem.Graph{Root: nil, Nodes: map[string]*ecosystem.Node{}}
	got := unreachableKeys(g, map[string]bool{"foo@1.0.0": true})
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestUnreachableKeys_AllReachable(t *testing.T) {
	nodeA := &ecosystem.Node{Name: "a", Version: "1.0.0"}
	nodeB := &ecosystem.Node{Name: "b", Version: "2.0.0"}

	root := &ecosystem.Node{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "a", Target: nodeA},
			{Name: "b", Target: nodeB},
		},
	}

	g := &ecosystem.Graph{
		Root: root,
		Nodes: map[string]*ecosystem.Node{
			"a@1.0.0": nodeA,
			"b@2.0.0": nodeB,
		},
	}

	pkgKeys := map[string]bool{
		"a@1.0.0": true,
		"b@2.0.0": true,
	}

	got := unreachableKeys(g, pkgKeys)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestUnreachableKeys_OrphanedNode(t *testing.T) {
	nodeA := &ecosystem.Node{Name: "a", Version: "1.0.0"}

	root := &ecosystem.Node{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "a", Target: nodeA},
		},
	}

	g := &ecosystem.Graph{
		Root: root,
		Nodes: map[string]*ecosystem.Node{
			"a@1.0.0": nodeA,
		},
	}

	// "orphan@1.0.0" is in packageKeys but not reachable from root.
	pkgKeys := map[string]bool{
		"a@1.0.0":      true,
		"orphan@1.0.0": true,
	}

	got := unreachableKeys(g, pkgKeys)
	if !got["orphan@1.0.0"] {
		t.Fatalf("expected orphan@1.0.0 to be orphaned, got %v", got)
	}
	if got["a@1.0.0"] {
		t.Fatalf("a@1.0.0 should not be orphaned")
	}
}

func TestUnreachableKeys_TransitiveReachability(t *testing.T) {
	nodeC := &ecosystem.Node{Name: "c", Version: "1.0.0"}
	nodeB := &ecosystem.Node{
		Name: "b", Version: "1.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "c", Target: nodeC},
		},
	}
	nodeA := &ecosystem.Node{
		Name: "a", Version: "1.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "b", Target: nodeB},
		},
	}

	root := &ecosystem.Node{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "a", Target: nodeA},
		},
	}

	g := &ecosystem.Graph{
		Root: root,
		Nodes: map[string]*ecosystem.Node{
			"a@1.0.0": nodeA,
			"b@1.0.0": nodeB,
			"c@1.0.0": nodeC,
		},
	}

	// d is in packageKeys but has no edges leading to it.
	pkgKeys := map[string]bool{
		"a@1.0.0": true,
		"b@1.0.0": true,
		"c@1.0.0": true,
		"d@1.0.0": true,
	}

	got := unreachableKeys(g, pkgKeys)
	if !got["d@1.0.0"] {
		t.Fatalf("expected d@1.0.0 to be orphaned, got %v", got)
	}
	for _, key := range []string{"a@1.0.0", "b@1.0.0", "c@1.0.0"} {
		if got[key] {
			t.Fatalf("%s should be reachable, but was reported orphaned", key)
		}
	}
}

// ---------------------------------------------------------------------------
// applyPlatformFilter
// ---------------------------------------------------------------------------

func TestApplyPlatformFilter_EmptyPlatform(t *testing.T) {
	g := &ecosystem.Graph{
		Root:  &ecosystem.Node{Name: "root", Version: "0.0.0"},
		Nodes: map[string]*ecosystem.Node{},
	}
	removed, err := applyPlatformFilter(g, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if removed != nil {
		t.Fatalf("expected nil removed, got %v", removed)
	}
}

func TestApplyPlatformFilter_InvalidPlatform(t *testing.T) {
	g := &ecosystem.Graph{
		Root:  &ecosystem.Node{Name: "root", Version: "0.0.0"},
		Nodes: map[string]*ecosystem.Node{},
	}
	_, err := applyPlatformFilter(g, "invalid-no-slash")
	if err == nil {
		t.Fatal("expected error for invalid platform, got nil")
	}
}

func TestApplyPlatformFilter_ValidPlatform(t *testing.T) {
	darwinOnly := &ecosystem.Node{
		Name: "darwin-only", Version: "1.0.0",
		OS: []string{"darwin"},
	}
	universal := &ecosystem.Node{
		Name: "universal", Version: "1.0.0",
	}

	g := &ecosystem.Graph{
		Root: &ecosystem.Node{
			Name:    "root",
			Version: "0.0.0",
			Dependencies: []*ecosystem.Edge{
				{Name: "darwin-only", Target: darwinOnly, Type: ecosystem.DepRegular},
				{Name: "universal", Target: universal, Type: ecosystem.DepRegular},
			},
		},
		Nodes: map[string]*ecosystem.Node{
			"darwin-only@1.0.0": darwinOnly,
			"universal@1.0.0":   universal,
		},
	}

	removed, err := applyPlatformFilter(g, "linux/x64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !removed["darwin-only@1.0.0"] {
		t.Error("expected darwin-only@1.0.0 to be removed")
	}
	if removed["universal@1.0.0"] {
		t.Error("expected universal@1.0.0 to be kept")
	}
}

// ---------------------------------------------------------------------------
// Generate - unknown format
// ---------------------------------------------------------------------------

func TestGenerate_UnknownFormat(t *testing.T) {
	_, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     []byte(`{"name":"test","version":"1.0.0"}`),
		OutputFormat: OutputFormat("totally-bogus"),
	})
	if err == nil {
		t.Fatal("expected error for unknown format, got nil")
	}
}

// ---------------------------------------------------------------------------
// Generate - real registry (skip in short mode)
// ---------------------------------------------------------------------------

func TestGenerate_RealRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real-registry test in short mode")
	}

	spec := []byte(`{"name":"test","version":"1.0.0","dependencies":{"is-odd":"^3.0.0"}}`)

	formats := []struct {
		name   string
		format OutputFormat
	}{
		{"PackageLockV3", FormatPackageLockV3},
		{"PnpmLockV9", FormatPnpmLockV9},
		{"YarnClassic", FormatYarnClassic},
		{"BunLock", FormatBunLock},
	}

	for _, tt := range formats {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Generate(context.Background(), GenerateOptions{
				SpecFile:     spec,
				OutputFormat: tt.format,
			})
			if err != nil {
				t.Fatalf("Generate(%s) error: %v", tt.name, err)
			}
			if len(result.Lockfile) == 0 {
				t.Fatalf("Generate(%s) produced empty lockfile", tt.name)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Generate - mock server
// ---------------------------------------------------------------------------

func TestGenerate_MockServer(t *testing.T) {
	// Load the real is-odd packument fixture.
	isOddData, err := os.ReadFile("npm/testdata/packument-is-odd.json")
	if err != nil {
		t.Fatalf("reading is-odd fixture: %v", err)
	}

	// Minimal is-number packument - is-odd ^3.0.0 resolves to 3.0.1 which
	// depends on is-number ^7.0.0, so we need at least version 7.0.0.
	isNumberData := []byte(`{
  "_id": "is-number",
  "name": "is-number",
  "dist-tags": { "latest": "7.0.0" },
  "time": {
    "created": "2015-01-01T00:00:00.000Z",
    "modified": "2018-01-01T00:00:00.000Z",
    "7.0.0": "2018-01-01T00:00:00.000Z"
  },
  "versions": {
    "7.0.0": {
      "name": "is-number",
      "version": "7.0.0",
      "license": "MIT",
      "dependencies": {},
      "dist": {
        "integrity": "sha512-MockIntegrity7000000000000000000000000000000000000000000000000==",
        "shasum": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        "tarball": "https://registry.npmjs.org/is-number/-/is-number-7.0.0.tgz"
      }
    }
  }
}`)

	mux := http.NewServeMux()
	mux.HandleFunc("/is-odd", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(isOddData)
	})
	mux.HandleFunc("/is-number", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(isNumberData)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	spec := []byte(`{"name":"test","version":"1.0.0","dependencies":{"is-odd":"^3.0.0"}}`)

	// Test with a representative format from each PM family.
	formats := []struct {
		name   string
		format OutputFormat
	}{
		{"PackageLockV3", FormatPackageLockV3},
		{"PnpmLockV9", FormatPnpmLockV9},
		{"YarnClassic", FormatYarnClassic},
		{"BunLock", FormatBunLock},
	}

	for _, tt := range formats {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Generate(context.Background(), GenerateOptions{
				SpecFile:     spec,
				OutputFormat: tt.format,
				RegistryURL:  srv.URL,
			})
			if err != nil {
				t.Fatalf("Generate(%s) error: %v", tt.name, err)
			}
			if len(result.Lockfile) == 0 {
				t.Fatalf("Generate(%s) produced empty lockfile", tt.name)
			}
		})
	}
}
