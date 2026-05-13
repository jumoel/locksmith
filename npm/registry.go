package npm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/registryurl"
	"golang.org/x/sync/singleflight"
)

// RegistryClient fetches package metadata from the npm registry.
type RegistryClient struct {
	baseURL         string
	defaultClient   *http.Client                    // used when no per-host TLS override matches
	mu              sync.Mutex
	cache           map[string]*Packument
	sf              singleflight.Group
	scopeRegistries map[string]string               // "@scope" -> registry URL
	authCredentials map[string]ecosystem.Credential // normalized registry URL -> credential

	tlsOptions *ecosystem.TLSOptions
	clientMu   sync.Mutex
	hostClients map[string]*http.Client // normalized registry URL -> cached per-host client
}

// NewRegistryClient creates a new npm registry client with no scope routing,
// auth, or custom TLS.
func NewRegistryClient(baseURL string) *RegistryClient {
	return NewRegistryClientWithConfig(baseURL, nil, nil)
}

// NewRegistryClientWithConfig creates a registry client with per-scope routing
// and per-registry credentials. See NewRegistryClientWithTLS for the variant
// that also takes TLS settings.
func NewRegistryClientWithConfig(baseURL string, scopeRegistries map[string]string, authCredentials map[string]ecosystem.Credential) *RegistryClient {
	return NewRegistryClientWithTLS(baseURL, scopeRegistries, authCredentials, nil)
}

// NewRegistryClientWithTLS creates a registry client with full configuration:
// per-scope routing, per-registry credentials, and TLS options. tlsOptions
// may be nil for default (system-roots, strict-validation) behavior.
//
// authCredentials keys MUST be normalized via internal/registryurl.Normalize;
// the registry client normalizes its own per-request URLs the same way before
// lookup.
//
// tlsOptions.PerHost replaces the outer TLSOptions for the matching host
// rather than merging with it (per ticket #22). If a host needs the global
// CA plus an extra one, the caller must build the union themselves.
func NewRegistryClientWithTLS(baseURL string, scopeRegistries map[string]string, authCredentials map[string]ecosystem.Credential, tlsOptions *ecosystem.TLSOptions) *RegistryClient {
	if baseURL == "" {
		baseURL = "https://registry.npmjs.org"
	}
	return &RegistryClient{
		baseURL:         baseURL,
		defaultClient:   buildHTTPClient(tlsOptions),
		cache:           make(map[string]*Packument),
		scopeRegistries: scopeRegistries,
		authCredentials: authCredentials,
		tlsOptions:      tlsOptions,
		hostClients:     map[string]*http.Client{},
	}
}

// httpClientFor returns the *http.Client to use for a request to registryURL.
// Per ticket #22, if tlsOptions.PerHost[normalized(registryURL)] is set, it
// fully replaces the outer TLSOptions for that request - no merging. Clients
// are cached per host so we don't rebuild a transport on every fetch.
func (r *RegistryClient) httpClientFor(registryURL string) *http.Client {
	if r.tlsOptions == nil || len(r.tlsOptions.PerHost) == 0 {
		return r.defaultClient
	}
	host := registryurl.Normalize(registryURL)
	r.clientMu.Lock()
	defer r.clientMu.Unlock()
	if c, ok := r.hostClients[host]; ok {
		return c
	}
	override, ok := r.tlsOptions.PerHost[host]
	if !ok {
		return r.defaultClient
	}
	c := buildHTTPClient(override)
	r.hostClients[host] = c
	return c
}

// httpClient returns the default client. Kept around because the existing
// retry loop reaches for "r.httpClient" - retained as a stub that delegates
// to defaultClient so the retry loop doesn't need restructuring.
func (r *RegistryClient) httpClient() *http.Client { return r.defaultClient }

// buildHTTPClient constructs an *http.Client honoring the given TLSOptions.
// nil opts produces a default client with system roots + strict validation,
// matching the pre-#17 behavior.
func buildHTTPClient(opts *ecosystem.TLSOptions) *http.Client {
	tlsCfg := &tls.Config{}
	if opts != nil {
		if opts.Insecure {
			tlsCfg.InsecureSkipVerify = true
		}
		if len(opts.RootCAs) > 0 {
			pool := x509.NewCertPool()
			allOK := true
			for _, pem := range opts.RootCAs {
				if !pool.AppendCertsFromPEM([]byte(pem)) {
					allOK = false
				}
			}
			// If parsing failed for any block AND we ended up with an empty
			// pool, install the empty pool anyway: the resulting handshake
			// will fail loudly rather than silently fall through to system
			// roots (which would hide a misconfigured cafile).
			if !allOK && len(pool.Subjects()) == 0 {
				// Force an empty pool so verification fails.
			}
			tlsCfg.RootCAs = pool
		}
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
			// Keep other Transport fields at default; locksmith doesn't
			// override proxy or dial behavior at this layer.
		},
	}
}

// doWithRetry executes an HTTP request with retry and exponential backoff.
// Retries on 429 (rate limit) and 5xx (server error). All other status codes
// are returned immediately. registryURL is used to look up a per-host
// http.Client (TLS overrides) per ticket #22.
func (r *RegistryClient) doWithRetry(req *http.Request, registryURL string) (*http.Response, error) {
	const maxRetries = 3
	client := r.httpClientFor(registryURL)
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			req = req.Clone(req.Context())
			time.Sleep(retryBackoff(attempt))
		}
		resp, err := client.Do(req)
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
//
// Concurrent calls for the same name coalesce via singleflight: the first
// caller performs the HTTP+parse work, every other caller blocks on the same
// in-flight request and receives the shared result. Errors are not cached -
// a subsequent retry after a transient failure will re-issue the request.
func (r *RegistryClient) fetchPackument(ctx context.Context, name string) (*Packument, error) {
	r.mu.Lock()
	if p, ok := r.cache[name]; ok {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	v, err, _ := r.sf.Do(name, func() (any, error) {
		// Re-check the cache under singleflight in case a sibling won the race
		// and populated it between the initial read and entering the flight.
		r.mu.Lock()
		if p, ok := r.cache[name]; ok {
			r.mu.Unlock()
			return p, nil
		}
		r.mu.Unlock()

		p, err := r.doFetchPackument(ctx, name)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		r.cache[name] = p
		r.mu.Unlock()
		return p, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*Packument), nil
}

// doFetchPackument performs the actual HTTP fetch and JSON parse. Pulled out
// so fetchPackument can wrap it in singleflight + cache management.
func (r *RegistryClient) doFetchPackument(ctx context.Context, name string) (*Packument, error) {
	registryURL := r.registryForPackage(name)
	url := fmt.Sprintf("%s/%s", registryURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", name, err)
	}
	req.Header.Set("Accept", "application/json")
	if header := r.authHeaderFor(registryURL); header != "" {
		req.Header.Set("Authorization", header)
	}

	resp, err := r.doWithRetry(req, registryURL)
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

// authHeaderFor returns the Authorization header value to send when fetching
// from registryURL, or "" if no credential is configured. Lookup goes through
// the registryurl.Normalize canonicalization so caller-supplied URLs (config
// files) and registry-client URLs (constructed at runtime) match without
// callers worrying about trailing slashes or hostname case.
func (r *RegistryClient) authHeaderFor(registryURL string) string {
	if r.authCredentials == nil {
		return ""
	}
	cred, ok := r.authCredentials[registryurl.Normalize(registryURL)]
	if !ok {
		return ""
	}
	return cred.AuthHeader()
}
