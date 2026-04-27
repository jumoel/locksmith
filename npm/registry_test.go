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

// ---------------------------------------------------------------------------
// Retry tests
// ---------------------------------------------------------------------------

func TestRetry_429ThenSuccess(t *testing.T) {
	isOddData, err := os.ReadFile("testdata/packument-is-odd.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(isOddData)
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClient(srv.URL)
	versions, err := client.FetchVersions(context.Background(), "is-odd", nil)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}
	if got := reqCount.Load(); got != 2 {
		t.Fatalf("expected exactly 2 requests (1 fail + 1 success), got %d", got)
	}
}

func TestRetry_500ThenSuccess(t *testing.T) {
	isOddData, err := os.ReadFile("testdata/packument-is-odd.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}

	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(isOddData)
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClient(srv.URL)
	versions, err := client.FetchVersions(context.Background(), "is-odd", nil)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if len(versions) != 4 {
		t.Fatalf("expected 4 versions, got %d", len(versions))
	}
	if got := reqCount.Load(); got != 2 {
		t.Fatalf("expected exactly 2 requests (1 fail + 1 success), got %d", got)
	}
}

func TestRetry_ExhaustedRetries(t *testing.T) {
	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClient(srv.URL)
	_, err := client.FetchVersions(context.Background(), "is-odd", nil)
	if err == nil {
		t.Fatal("expected error after exhausted retries, got nil")
	}
	// 1 initial attempt + 3 retries = 4 total requests.
	if got := reqCount.Load(); got != 4 {
		t.Fatalf("expected 4 total requests (1 + 3 retries), got %d", got)
	}
}

func TestRetry_404NotRetried(t *testing.T) {
	var reqCount atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClient(srv.URL)
	_, err := client.FetchVersions(context.Background(), "nonexistent-pkg", nil)
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if got := reqCount.Load(); got != 1 {
		t.Fatalf("expected exactly 1 request (404 should not be retried), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Scope registry routing tests
// ---------------------------------------------------------------------------

func TestRegistryForPackage_ScopeConfigured(t *testing.T) {
	client := NewRegistryClientWithConfig("https://registry.npmjs.org", map[string]string{
		"@company": "https://private.registry.com",
	}, nil)

	got := client.registryForPackage("@company/pkg")
	want := "https://private.registry.com"
	if got != want {
		t.Errorf("registryForPackage(@company/pkg) = %q, want %q", got, want)
	}
}

func TestRegistryForPackage_ScopeNotConfigured(t *testing.T) {
	client := NewRegistryClientWithConfig("https://registry.npmjs.org", map[string]string{
		"@company": "https://private.registry.com",
	}, nil)

	got := client.registryForPackage("@other/pkg")
	want := "https://registry.npmjs.org"
	if got != want {
		t.Errorf("registryForPackage(@other/pkg) = %q, want %q", got, want)
	}
}

func TestRegistryForPackage_UnscopedPackage(t *testing.T) {
	client := NewRegistryClientWithConfig("https://registry.npmjs.org", map[string]string{
		"@company": "https://private.registry.com",
	}, nil)

	got := client.registryForPackage("lodash")
	want := "https://registry.npmjs.org"
	if got != want {
		t.Errorf("registryForPackage(lodash) = %q, want %q", got, want)
	}
}

func TestRegistryForPackage_MultipleScopes(t *testing.T) {
	client := NewRegistryClientWithConfig("https://registry.npmjs.org", map[string]string{
		"@company": "https://private.registry.com",
		"@internal": "https://internal.registry.com",
	}, nil)

	tests := []struct {
		name string
		want string
	}{
		{"@company/foo", "https://private.registry.com"},
		{"@internal/bar", "https://internal.registry.com"},
		{"@other/baz", "https://registry.npmjs.org"},
		{"unscoped", "https://registry.npmjs.org"},
	}
	for _, tt := range tests {
		got := client.registryForPackage(tt.name)
		if got != tt.want {
			t.Errorf("registryForPackage(%s) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRegistryForPackage_NilScopeRegistries(t *testing.T) {
	client := NewRegistryClient("https://registry.npmjs.org")

	got := client.registryForPackage("@company/pkg")
	want := "https://registry.npmjs.org"
	if got != want {
		t.Errorf("registryForPackage(@company/pkg) with nil scopes = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Auth token tests
// ---------------------------------------------------------------------------

func TestAuthToken_SentWhenConfigured(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// Return a minimal valid packument.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"test-pkg","versions":{"1.0.0":{"name":"test-pkg","version":"1.0.0","dist":{"tarball":"http://x/t.tgz","shasum":"abc"}}},"time":{"1.0.0":"2020-01-01T00:00:00.000Z"},"dist-tags":{"latest":"1.0.0"}}`))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClientWithConfig(srv.URL, nil, map[string]string{
		srv.URL: "my-secret-token",
	})
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err != nil {
		t.Fatalf("FetchVersions: %v", err)
	}
	want := "Bearer my-secret-token"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

func TestAuthToken_NotSentWhenNotConfigured(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"name":"test-pkg","versions":{"1.0.0":{"name":"test-pkg","version":"1.0.0","dist":{"tarball":"http://x/t.tgz","shasum":"abc"}}},"time":{"1.0.0":"2020-01-01T00:00:00.000Z"},"dist-tags":{"latest":"1.0.0"}}`))
	}))
	t.Cleanup(srv.Close)

	client := NewRegistryClientWithConfig(srv.URL, nil, nil)
	_, err := client.FetchVersions(context.Background(), "test-pkg", nil)
	if err != nil {
		t.Fatalf("FetchVersions: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (no token configured)", gotAuth)
	}
}

// ---------------------------------------------------------------------------
// Combined scope routing + auth test
// ---------------------------------------------------------------------------

func TestScopeRouting_WithAuth(t *testing.T) {
	minimalPackument := `{"name":"pkg","versions":{"1.0.0":{"name":"pkg","version":"1.0.0","dist":{"tarball":"http://x/t.tgz","shasum":"abc"}}},"time":{"1.0.0":"2020-01-01T00:00:00.000Z"},"dist-tags":{"latest":"1.0.0"}}`

	var publicAuth, privateAuth string
	var publicHits, privateHits int

	publicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicHits++
		publicAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackument))
	}))
	t.Cleanup(publicSrv.Close)

	privateSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		privateHits++
		privateAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(minimalPackument))
	}))
	t.Cleanup(privateSrv.Close)

	client := NewRegistryClientWithConfig(publicSrv.URL, map[string]string{
		"@company": privateSrv.URL,
	}, map[string]string{
		privateSrv.URL: "private-token",
	})

	ctx := context.Background()

	// Scoped package should hit private server with auth.
	_, err := client.FetchVersions(ctx, "@company/pkg", nil)
	if err != nil {
		t.Fatalf("FetchVersions @company/pkg: %v", err)
	}
	if privateHits != 1 {
		t.Errorf("private server hits = %d, want 1", privateHits)
	}
	if publicHits != 0 {
		t.Errorf("public server hits = %d, want 0 (scoped pkg should not hit public)", publicHits)
	}
	if privateAuth != "Bearer private-token" {
		t.Errorf("private server auth = %q, want %q", privateAuth, "Bearer private-token")
	}

	// Unscoped package should hit public server without auth.
	_, err = client.FetchVersions(ctx, "lodash", nil)
	if err != nil {
		t.Fatalf("FetchVersions lodash: %v", err)
	}
	if publicHits != 1 {
		t.Errorf("public server hits = %d, want 1", publicHits)
	}
	if publicAuth != "" {
		t.Errorf("public server auth = %q, want empty", publicAuth)
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility test
// ---------------------------------------------------------------------------

func TestNewRegistryClient_BackwardCompatible(t *testing.T) {
	client := NewRegistryClient("https://custom.registry.com")
	if client.baseURL != "https://custom.registry.com" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "https://custom.registry.com")
	}
	if client.scopeRegistries != nil {
		t.Errorf("scopeRegistries = %v, want nil", client.scopeRegistries)
	}
	if client.authTokens != nil {
		t.Errorf("authTokens = %v, want nil", client.authTokens)
	}
	if client.cache == nil {
		t.Error("cache is nil, want initialized map")
	}
}

func TestNewRegistryClient_DefaultURL(t *testing.T) {
	client := NewRegistryClient("")
	if client.baseURL != "https://registry.npmjs.org" {
		t.Errorf("baseURL = %q, want %q", client.baseURL, "https://registry.npmjs.org")
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
