package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jumoel/locksmith"
	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/pnpmconfig"
)

// pnpmConfigContribution holds the slice-2 settings translated from
// pnpm-workspace.yaml. It's applied on top of npmrc results and overridden
// by CLI flags, per the precedence chain in ticket #18.
type pnpmConfigContribution struct {
	Catalogs               map[string]map[string]string
	WorkspaceGlobs         []string
	ResolutionMode         string
	StrictPeerDependencies bool
	DedupePeerDependents   bool
	CatalogMode            string
	MinimumReleaseAge      int
	CutoffExcludes         []string
	SupportedArchitectures ecosystem.Architectures
	PackageExtensionsJSON  []byte
	OverridesJSON          []byte
}

// loadPnpmConfig discovers pnpm-workspace.yaml next to the spec file and
// translates it into the slice-2 contribution shape. Returns a zero-valued
// contribution and nil error when the file is missing - that's the
// no-pnpm-workspace-file case, identical to the historical behavior.
func loadPnpmConfig(specPath string) (*pnpmConfigContribution, error) {
	specDir := filepath.Dir(specPath)
	path := filepath.Join(specDir, "pnpm-workspace.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &pnpmConfigContribution{}, nil
	} else if err != nil {
		return nil, err
	}
	cfg, err := pnpmconfig.ParseFile(path)
	if err != nil {
		return nil, err
	}
	out := &pnpmConfigContribution{
		Catalogs:               cfg.Catalogs,
		WorkspaceGlobs:         cfg.Packages,
		ResolutionMode:         cfg.ResolutionMode,
		CatalogMode:            cfg.CatalogMode,
		MinimumReleaseAge:      cfg.MinimumReleaseAge,
		CutoffExcludes:         cfg.MinimumReleaseAgeExclude,
		SupportedArchitectures: cfg.SupportedArchitectures,
		PackageExtensionsJSON:  cfg.PackageExtensions,
		OverridesJSON:          cfg.Overrides,
	}
	if cfg.StrictPeerDependencies != nil {
		out.StrictPeerDependencies = *cfg.StrictPeerDependencies
	}
	if cfg.DedupePeerDependents != nil {
		out.DedupePeerDependents = *cfg.DedupePeerDependents
	}
	return out, nil
}

// applyPnpmConfigToOptions overlays the pnpm-workspace.yaml contribution
// onto a GenerateOptions value built from earlier sources (npmrc + CLI
// flags). The caller is responsible for ensuring CLI flags still win for
// the fields they touched.
func applyPnpmConfigToOptions(opts *locksmith.GenerateOptions, c *pnpmConfigContribution, now time.Time) {
	if c == nil {
		return
	}

	// Catalogs: only set when not already provided.
	if opts.Catalogs == nil && len(c.Catalogs) > 0 {
		opts.Catalogs = c.Catalogs
	}

	// SupportedArchitectures: workspace value wins when opts is still zero.
	if isZeroArchitecturesCLI(opts.SupportedArchitectures) && !isZeroArchitecturesCLI(c.SupportedArchitectures) {
		opts.SupportedArchitectures = c.SupportedArchitectures
	}

	// Relative-age cutoff (ticket #11): seconds-from-now applied at CLI parse time.
	if c.MinimumReleaseAge > 0 && opts.CutoffDate == nil {
		cutoff := now.Add(-time.Duration(c.MinimumReleaseAge) * time.Second)
		opts.CutoffDate = &cutoff
	}
	if len(c.CutoffExcludes) > 0 {
		opts.CutoffExcludes = appendUnique(opts.CutoffExcludes, c.CutoffExcludes...)
	}

	// Policy fields: build a complete PolicyOverride per ticket #14's
	// merge model. If opts.PolicyOverride is already set (slice 1 case),
	// extend it; otherwise materialize a fresh policy from the format
	// baseline.
	if c.ResolutionMode != "" || c.StrictPeerDependencies || c.DedupePeerDependents {
		var policy ecosystem.ResolverPolicy
		if opts.PolicyOverride != nil {
			policy = *opts.PolicyOverride
		} else {
			policy = defaultPolicyForFormat(opts.OutputFormat)
		}
		if c.ResolutionMode != "" {
			policy.ResolutionMode = c.ResolutionMode
		}
		if c.StrictPeerDependencies {
			policy.StrictPeerDeps = true
		}
		if c.DedupePeerDependents {
			policy.DedupePeerDependents = true
		}
		opts.PolicyOverride = &policy
	}
}

func isZeroArchitecturesCLI(a ecosystem.Architectures) bool {
	return len(a.OS) == 0 && len(a.CPU) == 0 && len(a.Libc) == 0
}

// isPnpmFormat reports whether the output format is one of the pnpm
// lockfile variants - the only ones that consume pnpm-workspace.yaml
// resolution policy.
func isPnpmFormat(f locksmith.OutputFormat) bool {
	switch f {
	case locksmith.FormatPnpmLockV4,
		locksmith.FormatPnpmLockV5,
		locksmith.FormatPnpmLockV6,
		locksmith.FormatPnpmLockV9:
		return true
	}
	return false
}

func appendUnique(existing []string, additional ...string) []string {
	seen := make(map[string]bool, len(existing))
	for _, e := range existing {
		seen[e] = true
	}
	out := existing
	for _, a := range additional {
		if !seen[a] {
			out = append(out, a)
			seen[a] = true
		}
	}
	return out
}
