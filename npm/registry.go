package npm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
)

// RegistryClient fetches package metadata from the npm registry.
type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.Mutex
	cache      map[string]*Packument
}

// NewRegistryClient creates a new npm registry client.
func NewRegistryClient(baseURL string) *RegistryClient {
	if baseURL == "" {
		baseURL = "https://registry.npmjs.org"
	}
	return &RegistryClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		cache:      make(map[string]*Packument),
	}
}

// fetchPackument fetches and caches the full packument for a package.
func (r *RegistryClient) fetchPackument(ctx context.Context, name string) (*Packument, error) {
	r.mu.Lock()
	if p, ok := r.cache[name]; ok {
		r.mu.Unlock()
		return p, nil
	}
	r.mu.Unlock()

	url := fmt.Sprintf("%s/%s", r.baseURL, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", name, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
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

	// Convert peer deps meta
	if v.PeerDependenciesMeta != nil {
		meta.PeerDepsMeta = make(map[string]ecosystem.PeerDepMeta)
		for k, pm := range v.PeerDependenciesMeta {
			meta.PeerDepsMeta[k] = ecosystem.PeerDepMeta{Optional: pm.Optional}
		}
	}

	return meta, nil
}
