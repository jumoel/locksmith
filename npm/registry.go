package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
)

// RegistryClient fetches package metadata from the npm registry.
type RegistryClient struct {
	baseURL         string
	httpClient      *http.Client
	mu              sync.Mutex
	cache           map[string]*Packument
	scopeRegistries map[string]string // "@scope" -> registry URL
	authTokens      map[string]string // registry URL -> bearer token
}

// NewRegistryClient creates a new npm registry client with no scope routing or auth.
func NewRegistryClient(baseURL string) *RegistryClient {
	return NewRegistryClientWithConfig(baseURL, nil, nil)
}

// NewRegistryClientWithConfig creates a registry client with per-scope routing and auth tokens.
// scopeRegistries maps npm scopes (e.g., "@company") to registry URLs.
// authTokens maps registry base URLs to Bearer tokens.
func NewRegistryClientWithConfig(baseURL string, scopeRegistries, authTokens map[string]string) *RegistryClient {
	if baseURL == "" {
		baseURL = "https://registry.npmjs.org"
	}
	return &RegistryClient{
		baseURL:         baseURL,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		cache:           make(map[string]*Packument),
		scopeRegistries: scopeRegistries,
		authTokens:      authTokens,
	}
}

// doWithRetry executes an HTTP request with retry and exponential backoff.
// Retries on 429 (rate limit) and 5xx (server error). All other status codes
// are returned immediately.
func (r *RegistryClient) doWithRetry(req *http.Request) (*http.Response, error) {
	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			req = req.Clone(req.Context())
			time.Sleep(retryBackoff(attempt))
		}
		resp, err := r.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return nil, err
			}
			continue
		}
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("HTTP %d for %s after %d attempts", resp.StatusCode, req.URL.Path, maxRetries+1)
			}
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("retry logic error")
}

// retryBackoff returns the backoff duration for a given retry attempt (1-indexed).
// Produces 200ms, 400ms, 800ms for attempts 1, 2, 3.
func retryBackoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt-1)) * 200 * time.Millisecond
}

// registryForPackage returns the registry URL for a given package name.
// Scoped packages (@scope/name) use scope-specific registries if configured.
func (r *RegistryClient) registryForPackage(name string) string {
	if r.scopeRegistries != nil && strings.HasPrefix(name, "@") {
		if idx := strings.Index(name, "/"); idx > 0 {
			scope := name[:idx]
			if url, ok := r.scopeRegistries[scope]; ok {
				return url
			}
		}
	}
	return r.baseURL
}

// fetchPackument fetches and caches the full packument for a package.
func (r *RegistryClient) fetchPackument(ctx context.Context, name string) (*Packument, error) {
	r.mu.Lock()
	if p, ok := r.cache[name]; ok {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	registryURL := r.registryForPackage(name)
	url := fmt.Sprintf("%s/%s", registryURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", name, err)
	}
	req.Header.Set("Accept", "application/json")
	if r.authTokens != nil {
		if token, ok := r.authTokens[registryURL]; ok {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := r.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("fetching packument for %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("package %s not found", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, name)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response for %s: %w", name, err)
	}

	var p Packument
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("parsing packument for %s: %w", name, err)
	}

	r.mu.Lock()
	r.cache[name] = &p
	r.mu.Unlock()

	return &p, nil
}

// FetchVersions returns all available versions, optionally filtered by cutoff date.
func (r *RegistryClient) FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]ecosystem.VersionInfo, error) {
	p, err := r.fetchPackument(ctx, name)
	if err != nil {
		return nil, err
	}

	var versions []ecosystem.VersionInfo
	for ver := range p.Versions {
		vi := ecosystem.VersionInfo{
			Version:    ver,
			Deprecated: string(p.Versions[ver].Deprecated),
		}

		// Parse publish time if available
		if timeStr, ok := p.Time[ver]; ok {
			t, err := time.Parse(time.RFC3339, timeStr)
			if err == nil {
				vi.PublishedAt = t
				// Filter by cutoff date
				if cutoff != nil && t.After(*cutoff) {
					continue
				}
			}
		}

		versions = append(versions, vi)
	}

	return versions, nil
}

// FetchMetadata returns full metadata for a specific package version.
func (r *RegistryClient) FetchMetadata(ctx context.Context, name string, version string) (*ecosystem.VersionMetadata, error) {
	p, err := r.fetchPackument(ctx, name)
	if err != nil {
		return nil, err
	}

	v, ok := p.Versions[version]
	if !ok {
		return nil, fmt.Errorf("version %s not found for package %s", version, name)
	}

	meta := &ecosystem.VersionMetadata{
		Name:             v.Name,
		Version:          v.Version,
		Integrity:        v.Dist.Integrity,
		Shasum:           v.Dist.Shasum,
		TarballURL:       v.Dist.Tarball,
		Dependencies:     map[string]string(v.Dependencies),
		DevDeps:          map[string]string(v.DevDependencies),
		PeerDeps:         map[string]string(v.PeerDependencies),
		OptionalDeps:     map[string]string(v.OptionalDependencies),
		Engines:          map[string]string(v.Engines),
		OS:               v.OS,
		CPU:              v.CPU,
		HasInstallScript: v.HasInstallScript(),
		Bin:              v.ParseBin(),
		License:          string(v.License),
		Deprecated:       string(v.Deprecated),
	}

	// Parse funding (can be string, object, or array)
	if v.Funding != nil {
		var funding interface{}
		if err := json.Unmarshal(v.Funding, &funding); err == nil {
			meta.Funding = funding
		}
	}

	meta.BundleDeps = v.ParseBundleDeps()

	// Convert peer deps meta
	if v.PeerDependenciesMeta != nil {
		meta.PeerDepsMeta = make(map[string]ecosystem.PeerDepMeta)
		for k, pm := range v.PeerDependenciesMeta {
			meta.PeerDepsMeta[k] = ecosystem.PeerDepMeta{Optional: pm.Optional}
		}
	}

	return meta, nil
}

// FetchDistTags returns the dist-tags for a package.
func (r *RegistryClient) FetchDistTags(ctx context.Context, name string) (map[string]string, error) {
	p, err := r.fetchPackument(ctx, name)
	if err != nil {
		return nil, err
	}
	return p.DistTags, nil
}
