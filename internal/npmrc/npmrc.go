// Package npmrc parses .npmrc files into the shape locksmith's CLI glue
// layer needs to populate GenerateOptions. It handles every documented
// .npmrc syntax variant (INI key/value, INI comments, env interpolation,
// host-keyed config, scope-keyed registries, array values) and normalizes
// host URLs so the resulting keys match those used by the registry client.
//
// What this package does NOT do: discover .npmrc files on disk, merge
// multiple files together, or apply precedence between sources. Those
// concerns live in cmd/locksmith per ticket #8.
package npmrc

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jumoel/locksmith/internal/registryurl"
)

// Config holds the parsed contents of an .npmrc file.
type Config struct {
	// Registry is the value of the bare `registry=` line, empty if unset.
	Registry string

	// ScopeRegistries maps scope (with leading `@`) to registry URL.
	// Sourced from `@scope:registry=URL` lines.
	ScopeRegistries map[string]string

	// HostConfig maps a normalized registry URL (see internal/registryurl)
	// to a map of host-scoped settings. Sourced from `//host/path/:setting=value`
	// lines. Inner keys are the literal setting name (`_authToken`, `_auth`,
	// `username`, `_password`, `email`, `ca`, `cafile`, `always-auth`,
	// `strict-ssl`, etc.) - callers categorize them.
	HostConfig map[string]map[string]string

	// Defaults holds top-level scalar settings that don't fall into any of
	// the categorized buckets above (e.g. `strict-ssl`, `before`,
	// `legacy-peer-deps`, `engine-strict`, `cafile`).
	Defaults map[string]string

	// Arrays holds values from `key[] = value` lines, in source order.
	Arrays map[string][]string
}

// ParseFile reads an .npmrc file at path and returns its parsed contents.
// Env interpolation (${VAR} and $VAR forms) happens at parse time per
// ticket #6; unset variables become empty strings.
func ParseFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	cfg := &Config{
		ScopeRegistries: map[string]string{},
		HostConfig:      map[string]map[string]string{},
		Defaults:        map[string]string{},
		Arrays:          map[string][]string{},
	}

	scanner := bufio.NewScanner(f)
	// .npmrc lines can in principle be long when they carry base64 tokens or
	// inline PEM bodies. Bump the per-line limit so a CA bundle on one line
	// doesn't trip bufio's default 64KB ceiling.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = stripQuotes(val)
		val = os.Expand(val, os.Getenv)
		categorize(cfg, key, val)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return cfg, nil
}

// categorize sorts a single (key, value) pair into the right bucket on cfg.
func categorize(cfg *Config, key, val string) {
	// Array syntax: "key[] = value".
	if strings.HasSuffix(key, "[]") {
		k := strings.TrimSuffix(key, "[]")
		k = strings.TrimSpace(k)
		cfg.Arrays[k] = append(cfg.Arrays[k], val)
		return
	}
	// Scope registry: "@scope:registry".
	if strings.HasPrefix(key, "@") && strings.HasSuffix(key, ":registry") {
		scope := strings.TrimSuffix(key, ":registry")
		cfg.ScopeRegistries[scope] = val
		return
	}
	// Host-keyed config: "//host/path/:settingName".
	// Use LastIndex because the host part can contain forward slashes but
	// never colons (URL hosts can't contain colons except for port, but the
	// .npmrc form is path-only after the host; setting names never contain colons).
	if strings.HasPrefix(key, "//") {
		colon := strings.LastIndex(key, ":")
		if colon > 1 {
			hostPart := key[:colon]
			field := key[colon+1:]
			normalized := registryurl.Normalize(hostPart)
			if cfg.HostConfig[normalized] == nil {
				cfg.HostConfig[normalized] = map[string]string{}
			}
			cfg.HostConfig[normalized][field] = val
			return
		}
	}
	// Top-level registry.
	if key == "registry" {
		cfg.Registry = val
		return
	}
	// Everything else.
	cfg.Defaults[key] = val
}

// stripQuotes removes surrounding `"..."` or `'...'` pairs.
// `node-version = "20.0.0"` -> `20.0.0`.
func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
