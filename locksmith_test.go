package locksmith

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestUnreachableKeys_NonRegistryDepKeyedByConstraint covers the case the
// formatter actually hits in production: non-registry deps (portal:, file:,
// link:, etc.) are stored in result.Packages under "name@constraint" rather
// than "name@version". Naively reconstructing the lookup key from
// `edge.Target.Name + "@" + edge.Target.Version` during the reachability
// walk produces "name@0.0.0-local" (or whatever placeholder version the
// resolver picked), which never matches the constraint-keyed packages
// entry, so the entry gets dropped as "orphaned" after platform filtering.
//
// The walk must identify nodes by pointer (or look the package key up via
// the graph's keyed Node map) so that the constraint-keyed entries are
// preserved.
func TestUnreachableKeys_NonRegistryDepKeyedByConstraint(t *testing.T) {
	// Portal dep: the resolver creates a node with Version "0.0.0-local"
	// and stores it in graph.Nodes under "name@constraint".
	portalNode := &ecosystem.Node{
		Name:       "local-pkg",
		Version:    "0.0.0-local",
		TarballURL: "portal:./local-pkg",
	}

	root := &ecosystem.Node{
		Name:    "root",
		Version: "0.0.0",
		Dependencies: []*ecosystem.Edge{
			{Name: "local-pkg", Constraint: "portal:./local-pkg", Target: portalNode},
		},
	}

	g := &ecosystem.Graph{
		Root: root,
		Nodes: map[string]*ecosystem.Node{
			// The actual key used in result.Packages for portal: deps.
			"local-pkg@portal:./local-pkg": portalNode,
		},
	}

	pkgKeys := map[string]bool{
		"local-pkg@portal:./local-pkg": true,
	}

	got := unreachableKeys(g, pkgKeys)
	if got["local-pkg@portal:./local-pkg"] {
		t.Fatalf("portal: dep should be reachable through its parent root, but was marked orphaned; got %v", got)
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
	removed, err := applyPlatformFilter(g, GenerateOptions{})
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
	_, err := applyPlatformFilter(g, GenerateOptions{Platform: "invalid-no-slash"})
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

	removed, err := applyPlatformFilter(g, GenerateOptions{Platform: "linux/x64"})
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

func TestGenerate_PnpmCatalogs(t *testing.T) {
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
	mux.HandleFunc("/is-number", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(isNumberData)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// package.json uses catalog: protocol for is-number.
	spec := []byte(`{"name":"test","version":"1.0.0","dependencies":{"is-number":"catalog:"}}`)

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     spec,
		OutputFormat: FormatPnpmLockV9,
		RegistryURL:  srv.URL,
		Catalogs: map[string]map[string]string{
			"default": {"is-number": "^7.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Generate with catalogs error: %v", err)
	}
	if len(result.Lockfile) == 0 {
		t.Fatal("generated empty lockfile")
	}

	lockfileStr := string(result.Lockfile)
	// The lockfile should contain the resolved is-number package.
	if !strings.Contains(lockfileStr, "is-number") {
		t.Error("lockfile should contain is-number")
	}
	// The lockfile should contain the catalog: specifier in the importers section.
	if !strings.Contains(lockfileStr, "catalog:") {
		t.Error("lockfile should preserve catalog: specifier in importers section")
	}
}

// TestGenerate_PortalWithPlatformFilter regresses the bug where a yarn-berry
// project with both a portal: dep and an OS-restricted optional dep would
// lose the portal: lockfile entry after platform filtering. The unreachable
// sweep was reconstructing reachability keys as "name@version" but portal:
// deps live in result.Packages under "name@constraint" (with a placeholder
// version), so the entry got marked orphaned and deleted.
//
// Same shape as @storybook/addon-docs@9.1.10's scripts/ workspace, which is
// what the rebuilder hit in CI (chainguard-dev/ecosystems-rebuilder.js#1186).
// scripts/package.json declares `eslint-plugin-local-rules: portal:./...`
// plus transitive deps that pull in fsevents (os=darwin); on linux/x64
// generation the portal entry vanished and `yarn install --immutable`
// failed with "Manifest not found: eslint-plugin-local-rules@portal:...".
func TestGenerate_PortalWithPlatformFilter(t *testing.T) {
	// Minimal packument for an OS-restricted optional dep. fsevents@2.3.2's
	// os field is "darwin", so platform=linux/x64 filters it out and the
	// `if len(removed) > 0 { ... unreachableKeys }` branch fires.
	fseventsPkg := []byte(`{
  "_id": "fsevents",
  "name": "fsevents",
  "dist-tags": { "latest": "2.3.2" },
  "time": {
    "created": "2015-01-01T00:00:00.000Z",
    "modified": "2024-01-01T00:00:00.000Z",
    "2.3.2": "2021-06-03T00:00:00.000Z"
  },
  "versions": {
    "2.3.2": {
      "name": "fsevents",
      "version": "2.3.2",
      "os": ["darwin"],
      "dependencies": {},
      "dist": {
        "integrity": "sha512-MockIntegrity2032000000000000000000000000000000000000000000000==",
        "shasum": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
        "tarball": "https://registry.npmjs.org/fsevents/-/fsevents-2.3.2.tgz"
      }
    }
  }
}`)

	mux := http.NewServeMux()
	mux.HandleFunc("/fsevents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(fseventsPkg)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Workspace dir with a portal target sibling so the spec-dir-aware
	// version lookup can succeed for the portal dep.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "local-rules"), 0o755); err != nil {
		t.Fatalf("mkdir local-rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local-rules", "package.json"),
		[]byte(`{"name":"local-rules","version":"0.0.0-local"}`), 0o644); err != nil {
		t.Fatalf("writing local-rules manifest: %v", err)
	}
	specPath := filepath.Join(dir, "package.json")
	specData := []byte(`{
  "name": "test-portal-with-platform-filter",
  "version": "1.0.0",
  "private": true,
  "dependencies": {
    "local-rules": "portal:./local-rules",
    "fsevents": "^2.3.2"
  }
}`)
	if err := os.WriteFile(specPath, specData, 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     specData,
		SpecDir:      dir,
		OutputFormat: FormatYarnBerryV8,
		RegistryURL:  srv.URL,
		Platform:     "linux/x64",
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	out := string(result.Lockfile)
	wantKey := `"local-rules@portal:./local-rules"`
	if !strings.Contains(out, wantKey) {
		t.Errorf("expected portal entry key %s in output, got:\n%s", wantKey, out)
	}
	wantResolution := `resolution: "local-rules@portal:./local-rules`
	if !strings.Contains(out, wantResolution) {
		t.Errorf("expected portal resolution containing %q, got:\n%s", wantResolution, out)
	}
}

// TestGenerate_PortalAtWorkspaceMember regresses the same shape as
// TestGenerate_PortalWithPlatformFilter but for a portal dep declared by a
// workspace member, not the root. buildConstraintMap's workspace-member
// loop also synthesizes targetKey as `Name + "@" + Version` and would skip
// emitting the entry for the member's non-registry deps.
func TestGenerate_PortalAtWorkspaceMember(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	// Workspace root with one member.
	if err := os.MkdirAll(filepath.Join(dir, "packages", "lib"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "packages", "lib", "local-helper"), 0o755); err != nil {
		t.Fatalf("mkdir local-helper: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "packages", "lib", "local-helper", "package.json"),
		[]byte(`{"name":"local-helper","version":"0.0.0-local"}`), 0o644); err != nil {
		t.Fatalf("writing helper manifest: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "packages", "lib", "package.json"),
		[]byte(`{"name":"member-lib","version":"1.0.0","dependencies":{"local-helper":"portal:./local-helper"}}`), 0o644); err != nil {
		t.Fatalf("writing member manifest: %v", err)
	}
	rootSpec := []byte(`{
  "name": "ws-root",
  "version": "1.0.0",
  "private": true,
  "workspaces": ["packages/*"]
}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), rootSpec, 0o644); err != nil {
		t.Fatalf("writing root manifest: %v", err)
	}

	members := map[string][]byte{
		"packages/lib": []byte(`{"name":"member-lib","version":"1.0.0","dependencies":{"local-helper":"portal:./local-helper"}}`),
	}

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:         rootSpec,
		SpecDir:          dir,
		OutputFormat:     FormatYarnBerryV8,
		RegistryURL:      srv.URL,
		WorkspaceMembers: members,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	out := string(result.Lockfile)
	wantKey := `"local-helper@portal:`
	if !strings.Contains(out, wantKey) {
		t.Errorf("expected workspace member's portal dep to appear in lockfile, got:\n%s", out)
	}
}

// TestGenerate_PnpmLinkDep verifies that the pnpm formatter emits a usable
// lockfile entry key for a `link:` dep. pnpmPackageKey handles git+ and
// file: but falls back to "name@version" for link:, which produces a
// placeholder-versioned key like "local-pkg@0.0.0-local". pnpm install
// against that key tries to fetch `local-pkg-0.0.0-local.tgz` from the
// registry and 404s.
//
// Verified end-to-end against pnpm v9: with the locksmith-shaped output
// `local-pkg@link:./local-pkg`, pnpm install --frozen-lockfile succeeds
// and symlinks the local directory.
func TestGenerate_PnpmLinkDep(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "local-pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local-pkg", "package.json"),
		[]byte(`{"name":"local-pkg","version":"1.2.3"}`), 0o644); err != nil {
		t.Fatalf("writing local manifest: %v", err)
	}
	spec := []byte(`{
  "name": "test-pnpm-link",
  "version": "1.0.0",
  "private": true,
  "dependencies": {
    "local-pkg": "link:./local-pkg"
  }
}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), spec, 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     spec,
		SpecDir:      dir,
		OutputFormat: FormatPnpmLockV9,
		RegistryURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	out := string(result.Lockfile)
	// pnpm v9 packages section key. yaml library may or may not quote it,
	// so check the bare form.
	wantKey := "local-pkg@link:./local-pkg:"
	if !strings.Contains(out, wantKey) {
		t.Errorf("expected pnpm lockfile entry keyed %q, got:\n%s", wantKey, out)
	}
	// Importer should record the link: form as the version, not the
	// placeholder 0.0.0-local.
	wantImporterVersion := "version: link:./local-pkg"
	if !strings.Contains(out, wantImporterVersion) {
		t.Errorf("expected importer version %q, got:\n%s", wantImporterVersion, out)
	}
}

// TestNonRegistryDepsNotPlatformFiltered documents and locks in the invariant
// that locksmith never platform-filters non-registry deps. The platform
// filter, architecture filter, npm PlacedNodes cleanup, and bun peer-only
// sweep all assume `name@version` keys; they would mishandle a non-registry
// dep that got filtered, but only if such a dep gets filtered in the first
// place. The resolver does not propagate `os` / `cpu` metadata to nodes
// created from portal:/file:/link:/github: constraints (it only sets Name,
// Version, TarballURL), so NodeMatchesPlatform always returns true for
// them and they survive every filter. This test catches a future change
// that would start populating OS metadata for non-registry deps without
// updating the downstream cleanup helpers.
func TestNonRegistryDepsNotPlatformFiltered(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "darwin-only-helper"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Local manifest declares os: ["darwin"], but the resolver doesn't
	// propagate this for portal: deps.
	if err := os.WriteFile(filepath.Join(dir, "darwin-only-helper", "package.json"),
		[]byte(`{"name":"darwin-only-helper","version":"0.0.0-local","os":["darwin"]}`), 0o644); err != nil {
		t.Fatalf("writing local manifest: %v", err)
	}
	spec := []byte(`{
  "name": "test-non-reg-os",
  "version": "1.0.0",
  "private": true,
  "dependencies": {
    "darwin-only-helper": "portal:./darwin-only-helper"
  }
}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), spec, 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     spec,
		SpecDir:      dir,
		OutputFormat: FormatYarnBerryV8,
		RegistryURL:  srv.URL,
		Platform:     "linux/x64", // would filter the portal dep if OS metadata was propagated
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	out := string(result.Lockfile)
	if !strings.Contains(out, `"darwin-only-helper@portal:`) {
		t.Errorf("portal dep should survive platform filtering on linux/x64 "+
			"(resolver does not propagate os field for non-registry deps); got:\n%s", out)
	}
}

// TestGenerate_PnpmV6DevFlagForNonRegistryDep regresses the walkDeps
// inconsistency. pnpm's v5/v6 lockfile encodes "this package is dev-only"
// as `dev: true` on the package entry. computeDevFlags walks the graph
// and stores reached nodes under `Name + "@" + Version`, but the format
// loop looks them up under the `result.Packages` key, which is
// `name@constraint` for non-registry deps. The mismatch caused dev-only
// non-registry deps to be flagged `dev: false` in the lockfile.
func TestGenerate_PnpmV6DevFlagForNonRegistryDep(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "local-pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "local-pkg", "package.json"),
		[]byte(`{"name":"local-pkg","version":"1.0.0"}`), 0o644); err != nil {
		t.Fatalf("writing local manifest: %v", err)
	}
	spec := []byte(`{
  "name": "test-dev-file",
  "version": "1.0.0",
  "private": true,
  "devDependencies": {
    "local-pkg": "file:./local-pkg"
  }
}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), spec, 0o644); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	result, err := Generate(context.Background(), GenerateOptions{
		SpecFile:     spec,
		SpecDir:      dir,
		OutputFormat: FormatPnpmLockV6,
		RegistryURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}
	out := string(result.Lockfile)
	if !strings.Contains(out, "dev: true") {
		t.Errorf("expected dev: true on the dev-only file: dep, got:\n%s", out)
	}
	if strings.Contains(out, "dev: false") {
		t.Errorf("dev: false should not appear for a dev-only dep, got:\n%s", out)
	}
}

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
