package npm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer creates a mock npm registry serving the is-odd packument.
// It returns the server, a cleanup function, and a pointer to the request counter.
func newTestServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	isOddData, err := os.ReadFile("testdata/packument-is-odd.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	// Separate packument data for the scoped package test.
	scopedData := isOddData // reuse the same payload; URL routing is what matters.

	var reqCount atomic.Int64

	mux := http.NewServeMux()
	mux.HandleFunc("/is-odd", func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(isOddData)
	})
	mux.HandleFunc("/@myscope/is-odd", func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(scopedData)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &reqCount
}

func TestFetchVersions(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	versions, err := client.FetchVersions(context.Background(), "is-odd", nil)
	if err != nil {
		t.Fatalf("FetchVersions: %v", err)
	}

	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}

	got := make([]string, len(versions))
	for i, v := range versions {
		got[i] = v.Version
	}
	sort.Strings(got)

	expected := []string{"1.0.0", "2.0.0", "3.0.0", "3.0.1"}
	for i, want := range expected {
		if got[i] != want {
			t.Errorf("version[%d] = %q, want %q", i, got[i], want)
		}
	}

	// Verify that PublishedAt is set on all versions.
	for _, v := range versions {
		if v.PublishedAt.IsZero() {
			t.Errorf("version %s has zero PublishedAt", v.Version)
		}
	}
}

func TestFetchVersionsWithCutoff(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	cutoff := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	versions, err := client.FetchVersions(context.Background(), "is-odd", &cutoff)
	if err != nil {
		t.Fatalf("FetchVersions with cutoff: %v", err)
	}

	got := make([]string, len(versions))
	for i, v := range versions {
		got[i] = v.Version
	}
	sort.Strings(got)

	// 3.0.0 is 2017-06-01 (after cutoff), 3.0.1 is 2025 (after cutoff).
	expected := []string{"1.0.0", "2.0.0"}
	if len(got) != len(expected) {
		t.Fatalf("expected %d versions, got %d: %v", len(expected), len(got), got)
	}
	for i, want := range expected {
		if got[i] != want {
			t.Errorf("version[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestFetchMetadata(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	meta, err := client.FetchMetadata(context.Background(), "is-odd", "3.0.1")
	if err != nil {
		t.Fatalf("FetchMetadata: %v", err)
	}

	if meta.Name != "is-odd" {
		t.Errorf("Name = %q, want %q", meta.Name, "is-odd")
	}
	if meta.Version != "3.0.1" {
		t.Errorf("Version = %q, want %q", meta.Version, "3.0.1")
	}
	if meta.Integrity == "" {
		t.Error("Integrity is empty")
	}
	if meta.Shasum == "" {
		t.Error("Shasum is empty")
	}
	if meta.TarballURL != "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz" {
		t.Errorf("TarballURL = %q, want registry URL", meta.TarballURL)
	}

	// Dependencies
	if dep, ok := meta.Dependencies["is-number"]; !ok || dep != "^7.0.0" {
		t.Errorf("Dependencies[is-number] = %q, want %q", dep, "^7.0.0")
	}

	// DevDeps
	if dep, ok := meta.DevDeps["mocha"]; !ok || dep != "^10.0.0" {
		t.Errorf("DevDeps[mocha] = %q, want %q", dep, "^10.0.0")
	}

	// PeerDeps
	if dep, ok := meta.PeerDeps["is-even"]; !ok || dep != "^1.0.0" {
		t.Errorf("PeerDeps[is-even] = %q, want %q", dep, "^1.0.0")
	}

	// PeerDepsMeta
	if pm, ok := meta.PeerDepsMeta["is-even"]; !ok || !pm.Optional {
		t.Errorf("PeerDepsMeta[is-even].Optional = %v, want true", pm.Optional)
	}

	// Engines
	if eng, ok := meta.Engines["node"]; !ok || eng != ">=14" {
		t.Errorf("Engines[node] = %q, want %q", eng, ">=14")
	}

	// OS and CPU
	if len(meta.OS) != 2 {
		t.Errorf("OS length = %d, want 2", len(meta.OS))
	}
	if len(meta.CPU) != 2 {
		t.Errorf("CPU length = %d, want 2", len(meta.CPU))
	}

	// Install script (postinstall exists in 3.0.1)
	if !meta.HasInstallScript {
		t.Error("HasInstallScript = false, want true")
	}

	// Bin
	if bin, ok := meta.Bin["is-odd"]; !ok || bin != "./bin/cli.js" {
		t.Errorf("Bin[is-odd] = %q, want %q", bin, "./bin/cli.js")
	}

	// License
	if meta.License != "MIT" {
		t.Errorf("License = %q, want %q", meta.License, "MIT")
	}

	// Verify version without install script
	meta100, err := client.FetchMetadata(context.Background(), "is-odd", "1.0.0")
	if err != nil {
		t.Fatalf("FetchMetadata 1.0.0: %v", err)
	}
	if meta100.HasInstallScript {
		t.Error("1.0.0 HasInstallScript = true, want false")
	}
}

func TestFetchMetadataNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	_, err := client.FetchMetadata(context.Background(), "is-odd", "9.9.9")
	if err == nil {
		t.Fatal("expected error for non-existent version, got nil")
	}
}

func TestPackageNotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	_, err := client.FetchVersions(context.Background(), "nonexistent-pkg", nil)
	if err == nil {
		t.Fatal("expected error for non-existent package, got nil")
	}
}

func TestCaching(t *testing.T) {
	srv, reqCount := newTestServer(t)
	client := NewRegistryClient(srv.URL)
	ctx := context.Background()

	// First call should hit the server.
	_, err := client.FetchVersions(ctx, "is-odd", nil)
	if err != nil {
		t.Fatalf("first FetchVersions: %v", err)
	}
	if reqCount.Load() != 1 {
		t.Fatalf("expected 1 request after first call, got %d", reqCount.Load())
	}

	// Second call should use the cache.
	_, err = client.FetchVersions(ctx, "is-odd", nil)
	if err != nil {
		t.Fatalf("second FetchVersions: %v", err)
	}
	if reqCount.Load() != 1 {
		t.Fatalf("expected 1 request after second call (cached), got %d", reqCount.Load())
	}

	// FetchMetadata for the same package should also use the cache.
	_, err = client.FetchMetadata(ctx, "is-odd", "1.0.0")
	if err != nil {
		t.Fatalf("FetchMetadata: %v", err)
	}
	if reqCount.Load() != 1 {
		t.Fatalf("expected 1 request after FetchMetadata (cached), got %d", reqCount.Load())
	}
}

func TestScopedPackage(t *testing.T) {
	srv, _ := newTestServer(t)
	client := NewRegistryClient(srv.URL)

	versions, err := client.FetchVersions(context.Background(), "@myscope/is-odd", nil)
	if err != nil {
		t.Fatalf("FetchVersions for scoped package: %v", err)
	}

	if len(versions) != 4 {
		t.Fatalf("expected 4 versions for scoped package, got %d", len(versions))
	}

	// Also verify FetchMetadata works for scoped packages.
	meta, err := client.FetchMetadata(context.Background(), "@myscope/is-odd", "3.0.0")
	if err != nil {
		t.Fatalf("FetchMetadata for scoped package: %v", err)
	}
	if meta.Version != "3.0.0" {
		t.Errorf("Version = %q, want %q", meta.Version, "3.0.0")
	}
	if dep, ok := meta.Dependencies["is-number"]; !ok || dep != "^6.0.0" {
		t.Errorf("Dependencies[is-number] = %q, want %q", dep, "^6.0.0")
	}
}
