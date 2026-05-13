package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/jumoel/locksmith"
	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/npmrc"
	"github.com/jumoel/locksmith/internal/registryurl"
)

// npmrcOptions is the slice-1 surface of a merged .npmrc config translated
// into the shape locksmith.GenerateOptions expects. The CLI converts this
// into GenerateOptions while applying CLI-flag overrides on top.
type npmrcOptions struct {
	Registry                     string
	ScopeRegistries              map[string]string
	AuthCredentials              map[string]ecosystem.Credential
	TLSOptions                   *ecosystem.TLSOptions
	LegacyPeerDeps               bool
	StrictPeerDeps               bool
	EngineStrict                 bool
	OmitLockfileRegistryResolved bool
	MinifyPackageLock            bool
	CutoffDate                   *time.Time
}

// loadNpmrcOptions reads project and user .npmrc files and returns the
// merged result translated into GenerateOptions fields. An empty path skips
// that file; a missing-but-nonempty path is also silent (matches every PM's
// behavior when the rc file isn't there).
//
// Merge precedence: project > user. Per-source maps (ScopeRegistries,
// AuthCredentials) are unioned; on key collision the project value wins.
func loadNpmrcOptions(projectPath, userPath string) (*npmrcOptions, error) {
	user, err := parseIfExists(userPath)
	if err != nil {
		return nil, fmt.Errorf("reading user .npmrc at %s: %w", userPath, err)
	}
	project, err := parseIfExists(projectPath)
	if err != nil {
		return nil, fmt.Errorf("reading project .npmrc at %s: %w", projectPath, err)
	}

	out := &npmrcOptions{
		ScopeRegistries: map[string]string{},
		AuthCredentials: map[string]ecosystem.Credential{},
	}

	// Apply user first, then project so project overwrites on collision.
	for _, cfg := range []*npmrc.Config{user, project} {
		if cfg == nil {
			continue
		}
		if cfg.Registry != "" {
			out.Registry = cfg.Registry
		}
		for scope, url := range cfg.ScopeRegistries {
			out.ScopeRegistries[scope] = url
		}
		applyHostConfig(out, cfg.HostConfig)
		applyDefaults(out, cfg.Defaults)
	}

	return out, nil
}

// loadNpmrcOptionsWalkUp is the yarn-classic variant: walks up from
// startDir to $HOME, parsing .npmrc and .yarnrc at each level, merging
// innermost-wins (ticket #27). The yarn-classic-specific `.yarnrc` parsing
// is added in slice 4; for now this implementation handles `.npmrc`
// walk-up only, which already covers most real-world cases (yarn classic
// honors .npmrc for registry+auth identically to npm).
func loadNpmrcOptionsWalkUp(startDir, userHome string, noProject, noUser bool) (*npmrcOptions, error) {
	paths := []string{} // ordered outermost-first; later entries override earlier
	if !noUser && userHome != "" {
		paths = append(paths, filepath.Join(userHome, ".npmrc"))
	}
	if !noProject {
		// Walk from startDir upward, accumulating .npmrc paths from outer
		// directories to inner. Stop at $HOME (which is already covered above)
		// or at the filesystem root. Cap at 32 levels to avoid pathological
		// cases.
		var chain []string
		dir := startDir
		for i := 0; i < 32; i++ {
			chain = append(chain, filepath.Join(dir, ".npmrc"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break // root
			}
			if userHome != "" && dir == userHome {
				break // already added above
			}
			dir = parent
		}
		// Reverse so outermost (highest in tree) lands first.
		for i := len(chain) - 1; i >= 0; i-- {
			paths = append(paths, chain[i])
		}
	}

	out := &npmrcOptions{
		ScopeRegistries: map[string]string{},
		AuthCredentials: map[string]ecosystem.Credential{},
	}
	for _, path := range paths {
		cfg, err := parseIfExists(path)
		if err != nil {
			return nil, err
		}
		if cfg == nil {
			continue
		}
		if cfg.Registry != "" {
			out.Registry = cfg.Registry
		}
		for scope, url := range cfg.ScopeRegistries {
			out.ScopeRegistries[scope] = url
		}
		applyHostConfig(out, cfg.HostConfig)
		applyDefaults(out, cfg.Defaults)
	}
	return out, nil
}

// parseIfExists wraps npmrc.ParseFile so callers can treat absence as nil.
func parseIfExists(path string) (*npmrc.Config, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return npmrc.ParseFile(path)
}

// applyHostConfig walks the host-keyed map and lands credentials + TLS bits
// onto npmrcOptions. Credentials are categorized by the inner key
// (_authToken, _auth, username+_password, etc.) per ticket #7.
func applyHostConfig(out *npmrcOptions, host map[string]map[string]string) {
	for url, fields := range host {
		// Credentials: Bearer (_authToken), Basic-prepacked (_auth),
		// Basic-split (username + _password).
		switch {
		case fields["_authToken"] != "":
			out.AuthCredentials[url] = ecosystem.BearerCredential{Token: fields["_authToken"]}
		case fields["_auth"] != "":
			cred, err := ecosystem.NewBasicCredentialFromEncoded(fields["_auth"])
			if err == nil {
				out.AuthCredentials[url] = cred
			}
			// Silent on error: a misparsed _auth is a 401-at-fetch, not a
			// CLI-time error. Surfacing it here would mean a typo in
			// `~/.npmrc` aborts locksmith before it gets to the part that
			// matters.
		case fields["username"] != "" && fields["_password"] != "":
			cred, err := ecosystem.NewBasicCredentialFromSplit(fields["username"], fields["_password"])
			if err == nil {
				out.AuthCredentials[url] = cred
			}
		}

		// TLS: cafile / strict-ssl applied per-host.
		if fields["cafile"] != "" || fields["strict-ssl"] != "" {
			ensureTLS(out)
			perHost := &ecosystem.TLSOptions{}
			if pem, err := readCAFile(fields["cafile"]); err == nil && pem != "" {
				perHost.RootCAs = []string{pem}
			}
			if fields["strict-ssl"] == "false" {
				perHost.Insecure = true
			}
			if out.TLSOptions.PerHost == nil {
				out.TLSOptions.PerHost = map[string]*ecosystem.TLSOptions{}
			}
			out.TLSOptions.PerHost[url] = perHost
		}
	}
}

// applyDefaults handles top-level scalar keys (cafile, strict-ssl,
// legacy-peer-deps, engine-strict, before, format-package-lock,
// omit-lockfile-registry-resolved).
func applyDefaults(out *npmrcOptions, defaults map[string]string) {
	if v := defaults["cafile"]; v != "" {
		ensureTLS(out)
		if pem, err := readCAFile(v); err == nil && pem != "" {
			out.TLSOptions.RootCAs = append(out.TLSOptions.RootCAs, pem)
		}
	}
	if v := defaults["strict-ssl"]; v == "false" {
		ensureTLS(out)
		out.TLSOptions.Insecure = true
	}
	if v := defaults["legacy-peer-deps"]; v == "true" {
		out.LegacyPeerDeps = true
	}
	if v := defaults["strict-peer-deps"]; v == "true" {
		out.StrictPeerDeps = true
	}
	if v := defaults["engine-strict"]; v == "true" {
		out.EngineStrict = true
	}
	if v := defaults["before"]; v != "" {
		if t, err := parseCutoff(v); err == nil {
			out.CutoffDate = &t
		}
	}
	// format-package-lock=false -> minify.
	if v := defaults["format-package-lock"]; v == "false" {
		out.MinifyPackageLock = true
	}
	if v := defaults["omit-lockfile-registry-resolved"]; v == "true" {
		out.OmitLockfileRegistryResolved = true
	}
}

// ensureTLS lazy-initializes the TLS sub-struct.
func ensureTLS(out *npmrcOptions) {
	if out.TLSOptions == nil {
		out.TLSOptions = &ecosystem.TLSOptions{}
	}
}

// readCAFile reads a PEM-encoded CA bundle from disk. Returns "" with no
// error when path is empty (the convenience-of-the-caller path).
func readCAFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseCutoff handles both `before=YYYY-MM-DD` and RFC3339 forms (npm
// accepts both).
func parseCutoff(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}

// Ensure registryurl is referenced even if applyHostConfig is later refactored.
var _ = registryurl.Normalize

// pickString returns cli when the cli flag was explicitly set, else rc.
func pickString(rc, cli string, cliWasSet bool) string {
	if cliWasSet {
		return cli
	}
	if rc != "" {
		return rc
	}
	return cli
}

// mergeScopeRegistries merges rc scope-registries with CLI ones. CLI wins on
// key collision (per ticket #18).
func mergeScopeRegistries(rc, cli map[string]string) map[string]string {
	if len(rc) == 0 && len(cli) == 0 {
		return nil
	}
	out := make(map[string]string, len(rc)+len(cli))
	for k, v := range rc {
		out[k] = v
	}
	for k, v := range cli {
		out[k] = v
	}
	return out
}

// mergeCredentials merges rc credentials with CLI ones. CLI wins on collision.
func mergeCredentials(rc, cli map[string]ecosystem.Credential) map[string]ecosystem.Credential {
	if len(rc) == 0 && len(cli) == 0 {
		return nil
	}
	out := make(map[string]ecosystem.Credential, len(rc)+len(cli))
	for k, v := range rc {
		out[k] = v
	}
	for k, v := range cli {
		out[k] = v
	}
	return out
}

// credentialsFromCLIFlags maps the three credential flag sets onto the
// AuthCredentials shape. Keys are normalized so they line up with rc-loaded
// credentials and the registry client's per-request normalization.
func credentialsFromCLIFlags(bearer, basicPlain, basicEncoded map[string]string) map[string]ecosystem.Credential {
	if len(bearer) == 0 && len(basicPlain) == 0 && len(basicEncoded) == 0 {
		return nil
	}
	out := map[string]ecosystem.Credential{}
	for url, token := range bearer {
		out[registryurl.Normalize(url)] = ecosystem.BearerCredential{Token: token}
	}
	for url, userPass := range basicPlain {
		// Format: user:pass.
		sep := -1
		for i, c := range userPass {
			if c == ':' {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		out[registryurl.Normalize(url)] = ecosystem.BasicCredential{
			Username: userPass[:sep],
			Password: userPass[sep+1:],
		}
	}
	for url, encoded := range basicEncoded {
		cred, err := ecosystem.NewBasicCredentialFromEncoded(encoded)
		if err != nil {
			continue
		}
		out[registryurl.Normalize(url)] = cred
	}
	return out
}

// defaultPolicyForFormat returns the format's baseline ResolverPolicy. The
// CLI layers config-file values on top of this baseline per ticket #14.
//
// Slice 1 only handles npm formats; later slices add pnpm/yarn/bun. For now
// any non-npm format gets the npm baseline as a sensible default - the
// per-format resolver re-applies its own baseline before the override lands,
// so this only matters when the rc actually sets a policy field.
func defaultPolicyForFormat(format locksmith.OutputFormat) ecosystem.ResolverPolicy {
	switch format {
	case locksmith.FormatPackageLockV1,
		locksmith.FormatPackageLockV2,
		locksmith.FormatPackageLockV3,
		locksmith.FormatNpmShrinkwrap:
		return ecosystem.ResolverPolicy{
			CrossTreeDedup:         true,
			AutoInstallPeers:       true,
			StorePeerMetaOnNode:    true,
			ResolveWorkspaceByName: true,
		}
	}
	return ecosystem.ResolverPolicy{}
}

// emitPrintConfig writes the merged effective config as JSON. Credentials
// are redacted to "***" per ticket #19; the rest is shown verbatim so the
// user can see "where did this registry URL come from."
func emitPrintConfig(w io.Writer, opts *locksmith.GenerateOptions, rc *npmrcOptions) error {
	type credSummary struct {
		Type     string `json:"type"`
		Value    string `json:"value,omitempty"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	}
	auth := make(map[string]credSummary, len(opts.AuthCredentials))
	for k, c := range opts.AuthCredentials {
		switch c.(type) {
		case ecosystem.BearerCredential:
			auth[k] = credSummary{Type: "bearer", Value: "***"}
		case ecosystem.BasicCredential:
			auth[k] = credSummary{Type: "basic", Username: "***", Password: "***"}
		default:
			auth[k] = credSummary{Type: fmt.Sprintf("%T", c)}
		}
	}
	view := map[string]any{
		"registry":         opts.RegistryURL,
		"scope_registries": opts.ScopeRegistries,
		"auth":             auth,
		"tls":              tlsView(opts.TLSOptions),
		"policy": map[string]any{
			"legacy_peer_deps": rc.LegacyPeerDeps,
			"strict_peer_deps": rc.StrictPeerDeps,
			"engine_strict":    rc.EngineStrict,
		},
		"format": map[string]any{
			"omit_lockfile_registry_resolved": opts.OmitLockfileRegistryResolved,
			"minify_package_lock":             opts.MinifyPackageLock,
		},
		"cutoff":        formatCutoff(opts.CutoffDate),
		"platform":      opts.Platform,
		"node_version":  opts.NodeVersion,
		"output_format": string(opts.OutputFormat),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(view)
}

func tlsView(t *ecosystem.TLSOptions) any {
	if t == nil {
		return nil
	}
	view := map[string]any{
		"insecure":  t.Insecure,
		"num_roots": len(t.RootCAs),
	}
	if len(t.PerHost) > 0 {
		hosts := make([]string, 0, len(t.PerHost))
		for k := range t.PerHost {
			hosts = append(hosts, k)
		}
		view["per_host"] = hosts
	}
	return view
}

func formatCutoff(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
