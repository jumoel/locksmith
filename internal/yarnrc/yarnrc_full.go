package yarnrc

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jumoel/locksmith/ecosystem"
	"gopkg.in/yaml.v3"
)

// Config holds the parsed contents of a yarn berry `.yarnrc.yml`. Only the
// settings locksmith honors are surfaced; everything else is decoded into a
// generic map and ignored.
type Config struct {
	NpmRegistryServer string                  // global default registry
	NpmScopes         map[string]ScopeConfig  // keyed by scope WITHOUT leading @
	NpmRegistries     map[string]RegistryHost // keyed by "//host/path"

	CompressionLevel string
	DefaultProtocol  string

	SupportedArchitectures ecosystem.Architectures

	PackageExtensions json.RawMessage // raw JSON for npm.ParsePackageExtensions

	HttpsCaFilePath string

	// EnableImmutableInstalls is parsed for surfacing in --print-config but
	// locksmith always operates immutably (it generates a lockfile, never
	// rewrites one). The field is informational only.
	EnableImmutableInstalls bool
}

// ScopeConfig is a per-scope override block: registry routing, auth, and
// flags. All fields are optional.
type ScopeConfig struct {
	NpmRegistryServer string
	NpmAuthToken      string
	NpmAuthIdent      string
	NpmAlwaysAuth     bool
}

// RegistryHost is a per-host registry override block, keyed by the URL host
// (matching .npmrc's `//host/path/` form).
type RegistryHost struct {
	NpmAuthToken  string
	NpmAuthIdent  string
	NpmAlwaysAuth bool
}

// Parse reads a yarn berry .yarnrc.yml at path and returns the parsed
// settings. Env-var interpolation (${VAR}) is applied to every string value
// per ticket #6.
func Parse(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return parseBytes(data)
}

func parseBytes(data []byte) (*Config, error) {
	var raw struct {
		NpmRegistryServer string                    `yaml:"npmRegistryServer"`
		NpmScopes         map[string]rawScopeConfig `yaml:"npmScopes"`
		NpmRegistries     map[string]rawRegistry    `yaml:"npmRegistries"`
		CompressionLevel  any                       `yaml:"compressionLevel"`
		DefaultProtocol   string                    `yaml:"defaultProtocol"`
		SupportedArchitectures struct {
			OS   []string `yaml:"os"`
			CPU  []string `yaml:"cpu"`
			Libc []string `yaml:"libc"`
		} `yaml:"supportedArchitectures"`
		PackageExtensions       map[string]any `yaml:"packageExtensions"`
		HttpsCaFilePath         string         `yaml:"httpsCaFilePath"`
		EnableImmutableInstalls bool           `yaml:"enableImmutableInstalls"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}

	cfg := &Config{
		NpmRegistryServer:       expand(raw.NpmRegistryServer),
		CompressionLevel:        coerceCompressionLevel(raw.CompressionLevel),
		DefaultProtocol:         expand(raw.DefaultProtocol),
		HttpsCaFilePath:         expand(raw.HttpsCaFilePath),
		EnableImmutableInstalls: raw.EnableImmutableInstalls,
		SupportedArchitectures: ecosystem.Architectures{
			OS:   raw.SupportedArchitectures.OS,
			CPU:  raw.SupportedArchitectures.CPU,
			Libc: raw.SupportedArchitectures.Libc,
		},
	}

	if len(raw.NpmScopes) > 0 {
		cfg.NpmScopes = make(map[string]ScopeConfig, len(raw.NpmScopes))
		for name, sc := range raw.NpmScopes {
			cfg.NpmScopes[name] = ScopeConfig{
				NpmRegistryServer: expand(sc.NpmRegistryServer),
				NpmAuthToken:      expand(sc.NpmAuthToken),
				NpmAuthIdent:      expand(sc.NpmAuthIdent),
				NpmAlwaysAuth:     sc.NpmAlwaysAuth,
			}
		}
	}
	if len(raw.NpmRegistries) > 0 {
		cfg.NpmRegistries = make(map[string]RegistryHost, len(raw.NpmRegistries))
		for host, r := range raw.NpmRegistries {
			cfg.NpmRegistries[host] = RegistryHost{
				NpmAuthToken:  expand(r.NpmAuthToken),
				NpmAuthIdent:  expand(r.NpmAuthIdent),
				NpmAlwaysAuth: r.NpmAlwaysAuth,
			}
		}
	}

	if raw.PackageExtensions != nil {
		b, err := json.Marshal(raw.PackageExtensions)
		if err != nil {
			return nil, fmt.Errorf("re-encoding packageExtensions: %w", err)
		}
		cfg.PackageExtensions = b
	}

	return cfg, nil
}

type rawScopeConfig struct {
	NpmRegistryServer string `yaml:"npmRegistryServer"`
	NpmAuthToken      string `yaml:"npmAuthToken"`
	NpmAuthIdent      string `yaml:"npmAuthIdent"`
	NpmAlwaysAuth     bool   `yaml:"npmAlwaysAuth"`
}

type rawRegistry struct {
	NpmAuthToken  string `yaml:"npmAuthToken"`
	NpmAuthIdent  string `yaml:"npmAuthIdent"`
	NpmAlwaysAuth bool   `yaml:"npmAlwaysAuth"`
}

func expand(s string) string {
	return os.Expand(s, os.Getenv)
}

func coerceCompressionLevel(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return ""
	}
	return ""
}
