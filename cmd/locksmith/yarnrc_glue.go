package main

import (
	"os"

	"github.com/jumoel/locksmith"
	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/registryurl"
	"github.com/jumoel/locksmith/internal/yarnrc"
)

// yarnrcContribution carries the slice-3 surface from a parsed .yarnrc.yml.
type yarnrcContribution struct {
	Registry               string
	ScopeRegistries        map[string]string
	AuthCredentials        map[string]ecosystem.Credential
	TLSOptions             *ecosystem.TLSOptions
	CompressionLevel       string
	DefaultProtocol        string
	SupportedArchitectures ecosystem.Architectures
	PackageExtensionsJSON  []byte
}

// loadYarnrc discovers .yarnrc.yml next to the spec file and at the user
// level, then translates the result into a contribution shape ready to be
// applied to GenerateOptions.
//
// Yarn berry explicitly ignores .npmrc (per ticket #4 and yarn's migration
// guide). The CLI is responsible for not feeding .npmrc data into yarn-berry
// outputs - this loader is the only path for yarn-berry config.
func loadYarnrc(projectPath, userPath string) (*yarnrcContribution, error) {
	project, err := parseYarnrcIfExists(projectPath)
	if err != nil {
		return nil, err
	}
	user, err := parseYarnrcIfExists(userPath)
	if err != nil {
		return nil, err
	}

	out := &yarnrcContribution{
		ScopeRegistries: map[string]string{},
		AuthCredentials: map[string]ecosystem.Credential{},
	}
	// User first, then project so project overwrites on collision.
	for _, cfg := range []*yarnrc.Config{user, project} {
		if cfg == nil {
			continue
		}
		if cfg.NpmRegistryServer != "" {
			out.Registry = cfg.NpmRegistryServer
		}
		applyYarnrcScopes(out, cfg.NpmScopes)
		applyYarnrcHosts(out, cfg.NpmRegistries)
		if cfg.CompressionLevel != "" {
			out.CompressionLevel = cfg.CompressionLevel
		}
		if cfg.DefaultProtocol != "" {
			out.DefaultProtocol = cfg.DefaultProtocol
		}
		if !isZeroArchitecturesCLI(cfg.SupportedArchitectures) {
			out.SupportedArchitectures = cfg.SupportedArchitectures
		}
		if cfg.PackageExtensions != nil {
			out.PackageExtensionsJSON = cfg.PackageExtensions
		}
		if cfg.HttpsCaFilePath != "" {
			if pem, err := os.ReadFile(cfg.HttpsCaFilePath); err == nil {
				if out.TLSOptions == nil {
					out.TLSOptions = &ecosystem.TLSOptions{}
				}
				out.TLSOptions.RootCAs = append(out.TLSOptions.RootCAs, string(pem))
			}
		}
	}
	return out, nil
}

func parseYarnrcIfExists(path string) (*yarnrc.Config, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return yarnrc.Parse(path)
}

func applyYarnrcScopes(out *yarnrcContribution, scopes map[string]yarnrc.ScopeConfig) {
	for scope, sc := range scopes {
		scopeKey := "@" + scope
		if sc.NpmRegistryServer != "" {
			out.ScopeRegistries[scopeKey] = sc.NpmRegistryServer
		}
		// Materialize the per-scope credential keyed on the scope's
		// registry URL (per ticket #10's "approach (a)").
		if sc.NpmRegistryServer == "" {
			continue
		}
		regKey := registryurl.Normalize(sc.NpmRegistryServer)
		if sc.NpmAuthToken != "" {
			out.AuthCredentials[regKey] = ecosystem.BearerCredential{Token: sc.NpmAuthToken}
			continue
		}
		if sc.NpmAuthIdent != "" {
			// npmAuthIdent is "user:pass" cleartext.
			if u, p, ok := splitUserPass(sc.NpmAuthIdent); ok {
				out.AuthCredentials[regKey] = ecosystem.BasicCredential{Username: u, Password: p}
			}
		}
	}
}

func applyYarnrcHosts(out *yarnrcContribution, hosts map[string]yarnrc.RegistryHost) {
	for host, r := range hosts {
		regKey := registryurl.Normalize(host)
		if r.NpmAuthToken != "" {
			out.AuthCredentials[regKey] = ecosystem.BearerCredential{Token: r.NpmAuthToken}
			continue
		}
		if r.NpmAuthIdent != "" {
			if u, p, ok := splitUserPass(r.NpmAuthIdent); ok {
				out.AuthCredentials[regKey] = ecosystem.BasicCredential{Username: u, Password: p}
			}
		}
	}
}

func splitUserPass(s string) (user, pass string, ok bool) {
	for i := range s {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// applyYarnrcToOptions overlays the contribution onto opts. Only invoked
// when the target format is yarn-berry-*.
func applyYarnrcToOptions(opts *locksmith.GenerateOptions, c *yarnrcContribution) {
	if c == nil {
		return
	}
	if opts.RegistryURL == "" {
		opts.RegistryURL = c.Registry
	}
	for k, v := range c.ScopeRegistries {
		if _, ok := opts.ScopeRegistries[k]; !ok {
			if opts.ScopeRegistries == nil {
				opts.ScopeRegistries = map[string]string{}
			}
			opts.ScopeRegistries[k] = v
		}
	}
	if opts.AuthCredentials == nil && len(c.AuthCredentials) > 0 {
		opts.AuthCredentials = map[string]ecosystem.Credential{}
	}
	for k, v := range c.AuthCredentials {
		if _, ok := opts.AuthCredentials[k]; !ok {
			opts.AuthCredentials[k] = v
		}
	}
	if opts.TLSOptions == nil && c.TLSOptions != nil {
		opts.TLSOptions = c.TLSOptions
	}
	if opts.YarnCompressionLevel == "" {
		opts.YarnCompressionLevel = c.CompressionLevel
	}
	if isZeroArchitecturesCLI(opts.SupportedArchitectures) && !isZeroArchitecturesCLI(c.SupportedArchitectures) {
		opts.SupportedArchitectures = c.SupportedArchitectures
	}
}

// isYarnBerryFormat reports whether the format is yarn-berry-*.
func isYarnBerryFormat(f locksmith.OutputFormat) bool {
	switch f {
	case locksmith.FormatYarnBerryV4,
		locksmith.FormatYarnBerryV5,
		locksmith.FormatYarnBerryV6,
		locksmith.FormatYarnBerryV8:
		return true
	}
	return false
}

// isYarnClassicFormat reports whether the format is yarn-classic.
func isYarnClassicFormat(f locksmith.OutputFormat) bool {
	return f == locksmith.FormatYarnClassic
}
