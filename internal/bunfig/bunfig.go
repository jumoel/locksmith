// Package bunfig parses bun's bunfig.toml into the fields locksmith needs
// for byte-accurate lockfile generation. Covers `[install]` and
// `[install.scopes]` plus env-var interpolation per ticket #6.
//
// What this package does NOT do: discover files on disk, merge bunfig.toml
// with .npmrc (the CLI does that per ticket #23), or apply CLI flag
// overrides. The parser only deals with one file at a time.
package bunfig

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Config is the parsed contents of a bunfig.toml file.
type Config struct {
	Install InstallConfig
}

// InstallConfig holds the `[install]` block and its nested tables.
type InstallConfig struct {
	Registry                  RegistryConfig
	Scopes                    map[string]RegistryConfig
	CaFile                    string
	Ca                        string
	MinimumReleaseAge         int
	MinimumReleaseAgeExcludes []string

	// Dependency-type filters. Stored as pointers so the CLI can distinguish
	// absent from explicit-false. Per ticket #15 these are parsed but not
	// honored in the resolver (parsed-for-print-config only).
	Optional   *bool
	Dev        *bool
	Peer       *bool
	Production *bool
}

// RegistryConfig represents the value of `install.registry` or
// `install.scopes.<name>`. Both forms accept a bare URL string OR an inline
// table with `{ url, token }` / `{ url, username, password }`. The parser
// normalizes both forms into this struct.
type RegistryConfig struct {
	URL      string
	Token    string
	Username string
	Password string
}

// ParseFile reads a bunfig.toml at path and returns its parsed contents.
// Env-var interpolation ($VAR and ${VAR}) is applied to every string value.
func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return Parse(data)
}

// Parse decodes bunfig.toml bytes into a Config. The registry/scope-value
// dual-form (string or inline table) is handled by decoding into any and
// post-processing.
func Parse(data []byte) (*Config, error) {
	// Stage 1: decode into a permissive intermediate. This lets the
	// registry/scope value be either a string or a map, which TOML allows
	// but Go's typed decoding does not.
	var raw struct {
		Install struct {
			Registry                  any            `toml:"registry"`
			Scopes                    map[string]any `toml:"scopes"`
			CaFile                    string         `toml:"cafile"`
			Ca                        string         `toml:"ca"`
			MinimumReleaseAge         int            `toml:"minimumReleaseAge"`
			MinimumReleaseAgeExcludes []string       `toml:"minimumReleaseAgeExcludes"`
			Optional                  *bool          `toml:"optional"`
			Dev                       *bool          `toml:"dev"`
			Peer                      *bool          `toml:"peer"`
			Production                *bool          `toml:"production"`
		} `toml:"install"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("parsing toml: %w", err)
	}

	cfg := &Config{}
	cfg.Install.Registry = coerceRegistry(raw.Install.Registry)
	cfg.Install.CaFile = expand(raw.Install.CaFile)
	cfg.Install.Ca = expand(raw.Install.Ca)
	cfg.Install.MinimumReleaseAge = raw.Install.MinimumReleaseAge
	cfg.Install.MinimumReleaseAgeExcludes = raw.Install.MinimumReleaseAgeExcludes
	cfg.Install.Optional = raw.Install.Optional
	cfg.Install.Dev = raw.Install.Dev
	cfg.Install.Peer = raw.Install.Peer
	cfg.Install.Production = raw.Install.Production

	if len(raw.Install.Scopes) > 0 {
		cfg.Install.Scopes = make(map[string]RegistryConfig, len(raw.Install.Scopes))
		for name, value := range raw.Install.Scopes {
			cfg.Install.Scopes[name] = coerceRegistry(value)
		}
	}

	return cfg, nil
}

// coerceRegistry turns the loose `any` shape into a typed RegistryConfig.
// Accepts a bare string (URL only) or a map with url/token/username/password
// keys. Every string value is env-expanded.
func coerceRegistry(v any) RegistryConfig {
	switch t := v.(type) {
	case nil:
		return RegistryConfig{}
	case string:
		return RegistryConfig{URL: expand(t)}
	case map[string]any:
		rc := RegistryConfig{}
		if s, ok := t["url"].(string); ok {
			rc.URL = expand(s)
		}
		if s, ok := t["token"].(string); ok {
			rc.Token = expand(s)
		}
		if s, ok := t["username"].(string); ok {
			rc.Username = expand(s)
		}
		if s, ok := t["password"].(string); ok {
			rc.Password = expand(s)
		}
		return rc
	}
	return RegistryConfig{}
}

func expand(s string) string {
	return os.Expand(s, os.Getenv)
}
