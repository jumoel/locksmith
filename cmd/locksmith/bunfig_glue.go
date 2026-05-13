package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jumoel/locksmith"
	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/bunfig"
	"github.com/jumoel/locksmith/internal/registryurl"
)

// bunfigContribution carries the slice-5 surface from a parsed bunfig.toml.
type bunfigContribution struct {
	Registry          string
	ScopeRegistries   map[string]string
	AuthCredentials   map[string]ecosystem.Credential
	TLSOptions        *ecosystem.TLSOptions
	MinimumReleaseAge int
	CutoffExcludes    []string
}

// loadBunfig discovers bunfig.toml next to the spec and at the user level,
// then translates the result into a contribution.
//
// Per ticket #23 (provisional), bunfig.toml wins over .npmrc on collision.
// The CLI applies bunfig AFTER .npmrc so its values overwrite.
//
// User-config path honors $XDG_CONFIG_HOME first, then $HOME (matches bun).
func loadBunfig(projectPath, userPath string) (*bunfigContribution, error) {
	project, err := parseBunfigIfExists(projectPath)
	if err != nil {
		return nil, err
	}
	user, err := parseBunfigIfExists(userPath)
	if err != nil {
		return nil, err
	}
	out := &bunfigContribution{
		ScopeRegistries: map[string]string{},
		AuthCredentials: map[string]ecosystem.Credential{},
	}
	for _, cfg := range []*bunfig.Config{user, project} {
		if cfg == nil {
			continue
		}
		if cfg.Install.Registry.URL != "" {
			out.Registry = cfg.Install.Registry.URL
			// Object-form embedded credentials: unpack onto AuthCredentials.
			addEmbeddedCredential(out.AuthCredentials, cfg.Install.Registry)
		}
		for scope, rc := range cfg.Install.Scopes {
			scopeKey := "@" + scope
			if rc.URL != "" {
				out.ScopeRegistries[scopeKey] = rc.URL
				addEmbeddedCredential(out.AuthCredentials, rc)
			}
		}
		if cfg.Install.CaFile != "" {
			if pem, err := os.ReadFile(cfg.Install.CaFile); err == nil {
				if out.TLSOptions == nil {
					out.TLSOptions = &ecosystem.TLSOptions{}
				}
				out.TLSOptions.RootCAs = append(out.TLSOptions.RootCAs, string(pem))
			}
		}
		if cfg.Install.Ca != "" {
			if out.TLSOptions == nil {
				out.TLSOptions = &ecosystem.TLSOptions{}
			}
			out.TLSOptions.RootCAs = append(out.TLSOptions.RootCAs, cfg.Install.Ca)
		}
		if cfg.Install.MinimumReleaseAge > 0 {
			out.MinimumReleaseAge = cfg.Install.MinimumReleaseAge
		}
		if len(cfg.Install.MinimumReleaseAgeExcludes) > 0 {
			out.CutoffExcludes = cfg.Install.MinimumReleaseAgeExcludes
		}
	}
	return out, nil
}

// addEmbeddedCredential converts the inline-table form of registry config
// (object with token / username+password) into AuthCredentials keyed on
// the registry URL.
func addEmbeddedCredential(creds map[string]ecosystem.Credential, rc bunfig.RegistryConfig) {
	if rc.URL == "" {
		return
	}
	key := registryurl.Normalize(rc.URL)
	if rc.Token != "" {
		creds[key] = ecosystem.BearerCredential{Token: rc.Token}
		return
	}
	if rc.Username != "" || rc.Password != "" {
		creds[key] = ecosystem.BasicCredential{Username: rc.Username, Password: rc.Password}
	}
}

func parseBunfigIfExists(path string) (*bunfig.Config, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return bunfig.ParseFile(path)
}

// applyBunfigToOptions overlays the bunfig.toml contribution onto opts.
// Per ticket #23, bunfig.toml wins over any .npmrc values previously set
// on opts.
func applyBunfigToOptions(opts *locksmith.GenerateOptions, c *bunfigContribution, now time.Time) {
	if c == nil {
		return
	}
	if c.Registry != "" {
		opts.RegistryURL = c.Registry
	}
	if opts.ScopeRegistries == nil && len(c.ScopeRegistries) > 0 {
		opts.ScopeRegistries = map[string]string{}
	}
	for k, v := range c.ScopeRegistries {
		opts.ScopeRegistries[k] = v
	}
	if opts.AuthCredentials == nil && len(c.AuthCredentials) > 0 {
		opts.AuthCredentials = map[string]ecosystem.Credential{}
	}
	for k, v := range c.AuthCredentials {
		opts.AuthCredentials[k] = v
	}
	if c.TLSOptions != nil {
		if opts.TLSOptions == nil {
			opts.TLSOptions = c.TLSOptions
		} else {
			opts.TLSOptions.RootCAs = append(opts.TLSOptions.RootCAs, c.TLSOptions.RootCAs...)
		}
	}
	if c.MinimumReleaseAge > 0 && opts.CutoffDate == nil {
		cutoff := now.Add(-time.Duration(c.MinimumReleaseAge) * time.Second)
		opts.CutoffDate = &cutoff
	}
	if len(c.CutoffExcludes) > 0 {
		opts.CutoffExcludes = appendUnique(opts.CutoffExcludes, c.CutoffExcludes...)
	}
}

// isBunFormat reports whether the format is bun-lock.
func isBunFormat(f locksmith.OutputFormat) bool {
	return f == locksmith.FormatBunLock
}

// bunfigUserPath resolves the user-level bunfig.toml path. Honors
// $XDG_CONFIG_HOME first (bun's documented behavior), then $HOME.
func bunfigUserPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, ".bunfig.toml")
	}
	if home, _ := os.UserHomeDir(); home != "" {
		return filepath.Join(home, ".bunfig.toml")
	}
	return ""
}
