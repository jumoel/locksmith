package ecosystem

import (
	"context"
	"time"
)

// VersionInfo is the minimal version data needed for resolution.
type VersionInfo struct {
	Version     string
	PublishedAt time.Time
	Deprecated  string
}

// VersionMetadata is the full metadata needed for lockfile generation.
type VersionMetadata struct {
	Name             string
	Version          string
	Integrity        string
	Shasum           string
	TarballURL       string
	Dependencies     map[string]string // name -> constraint
	DevDeps          map[string]string
	PeerDeps         map[string]string
	OptionalDeps     map[string]string
	PeerDepsMeta     map[string]PeerDepMeta
	Engines          map[string]string
	OS               []string
	CPU              []string
	HasInstallScript bool
	Bin              map[string]string
	License          string
	Deprecated       string
	Funding          interface{} // URL string or structured object
}

// PeerDepMeta holds metadata about a peer dependency.
type PeerDepMeta struct {
	Optional bool
}

// Registry provides version information for packages from a remote registry.
type Registry interface {
	// FetchVersions returns all available versions of a package, optionally filtered by cutoff date.
	FetchVersions(ctx context.Context, name string, cutoff *time.Time) ([]VersionInfo, error)
	// FetchMetadata returns full metadata for a specific package version.
	FetchMetadata(ctx context.Context, name string, version string) (*VersionMetadata, error)
	// FetchDistTags returns the dist-tags for a package (e.g., {"latest": "1.0.0"}).
	FetchDistTags(ctx context.Context, name string) (map[string]string, error)
}
