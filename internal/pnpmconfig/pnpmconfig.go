// Package pnpmconfig parses pnpm-workspace.yaml into a typed struct. It
// consolidates what was previously split between cmd/locksmith/workspace.go
// (workspace globs + catalogs) and the new slice-2 surface (resolution
// policy, package extensions, supported architectures, minimum release
// age, catalog mode, overrides). See ticket #29 for the rationale.
package pnpmconfig

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jumoel/locksmith/ecosystem"
	"gopkg.in/yaml.v3"
)

// Config is the parsed contents of a pnpm-workspace.yaml file. Booleans
// that map to ResolverPolicy fields use pointer types so the caller can
// distinguish "absent" from "explicitly false."
type Config struct {
	Packages []string

	// Catalogs: the unnamed `catalog:` block lands under the "default" key,
	// and named catalogs keep their declared names. Matches the existing
	// shape from cmd/locksmith/workspace.go's parsePnpmCatalogs.
	Catalogs map[string]map[string]string

	// Resolution-policy fields (ticket #14).
	ResolutionMode         string
	AutoInstallPeers       *bool
	StrictPeerDependencies *bool
	DedupePeerDependents   *bool

	// CatalogMode (ticket #26): strict | manual | prefer | only-direct.
	CatalogMode string

	// Time-based filtering (ticket #11). MinimumReleaseAge is in seconds.
	MinimumReleaseAge        int
	MinimumReleaseAgeExclude []string

	// Multi-axis architecture filter (ticket #13).
	SupportedArchitectures ecosystem.Architectures

	// PackageExtensions and Overrides are kept as raw JSON-shaped bytes so
	// the existing npm.ParsePackageExtensions / npm.ParseNpmOverrides paths
	// can ingest them without a parallel YAML-typed code path. The CLI
	// converts YAML -> JSON via a round-trip.
	PackageExtensions json.RawMessage
	Overrides         json.RawMessage
}

// ParseFile reads pnpm-workspace.yaml at path and returns its parsed contents.
// Returns a clear error for missing files; YAML parse errors propagate too.
func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes pnpm-workspace.yaml bytes into a Config.
func Parse(data []byte) (*Config, error) {
	var raw struct {
		Packages []string                     `yaml:"packages"`
		Catalog  map[string]string            `yaml:"catalog"`
		Catalogs map[string]map[string]string `yaml:"catalogs"`

		ResolutionMode         string `yaml:"resolutionMode"`
		AutoInstallPeers       *bool  `yaml:"autoInstallPeers"`
		StrictPeerDependencies *bool  `yaml:"strictPeerDependencies"`
		DedupePeerDependents   *bool  `yaml:"dedupePeerDependents"`

		CatalogMode string `yaml:"catalogMode"`

		MinimumReleaseAge        int      `yaml:"minimumReleaseAge"`
		MinimumReleaseAgeExclude []string `yaml:"minimumReleaseAgeExclude"`

		SupportedArchitectures struct {
			OS   []string `yaml:"os"`
			CPU  []string `yaml:"cpu"`
			Libc []string `yaml:"libc"`
		} `yaml:"supportedArchitectures"`

		PackageExtensions map[string]any `yaml:"packageExtensions"`
		Overrides         map[string]any `yaml:"overrides"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}

	cfg := &Config{
		Packages:                 raw.Packages,
		ResolutionMode:           raw.ResolutionMode,
		AutoInstallPeers:         raw.AutoInstallPeers,
		StrictPeerDependencies:   raw.StrictPeerDependencies,
		DedupePeerDependents:     raw.DedupePeerDependents,
		CatalogMode:              raw.CatalogMode,
		MinimumReleaseAge:        raw.MinimumReleaseAge,
		MinimumReleaseAgeExclude: raw.MinimumReleaseAgeExclude,
		SupportedArchitectures: ecosystem.Architectures{
			OS:   raw.SupportedArchitectures.OS,
			CPU:  raw.SupportedArchitectures.CPU,
			Libc: raw.SupportedArchitectures.Libc,
		},
	}

	// Merge catalog (default) into catalogs map.
	if raw.Catalog != nil || raw.Catalogs != nil {
		cfg.Catalogs = map[string]map[string]string{}
		for name, entries := range raw.Catalogs {
			cfg.Catalogs[name] = entries
		}
		if raw.Catalog != nil {
			cfg.Catalogs["default"] = raw.Catalog
		}
	}

	// Convert YAML maps to JSON bytes for the consumers that expect JSON.
	if raw.PackageExtensions != nil {
		b, err := json.Marshal(raw.PackageExtensions)
		if err != nil {
			return nil, fmt.Errorf("re-encoding packageExtensions as JSON: %w", err)
		}
		cfg.PackageExtensions = b
	}
	if raw.Overrides != nil {
		b, err := json.Marshal(raw.Overrides)
		if err != nil {
			return nil, fmt.Errorf("re-encoding overrides as JSON: %w", err)
		}
		cfg.Overrides = b
	}

	return cfg, nil
}
