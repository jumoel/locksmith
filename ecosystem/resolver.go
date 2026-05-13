package ecosystem

import (
	"context"
	"time"
)

// ResolveOptions configures the dependency resolution process.
type ResolveOptions struct {
	CutoffDate *time.Time

	// CutoffExcludes is a list of package names that bypass the CutoffDate
	// filter entirely. Sourced from `pnpm-workspace.yaml minimumReleaseAgeExclude`
	// and `bunfig.toml install.minimumReleaseAgeExcludes`. Exact-name
	// matching only per ticket #28 (no globs).
	CutoffExcludes []string

	// SpecDir is the directory containing the spec file, for resolving
	// file: dependencies by reading local package versions.
	SpecDir string
	// WorkspaceIndex provides workspace member lookups for resolving workspace: protocol deps.
	// Nil for single-package projects.
	WorkspaceIndex *WorkspaceIndex
	// NodeVersion, if set, skips package versions whose engines.node
	// constraint is incompatible with this version during resolution.
	// Format: semver string (e.g., "18.0.0"). When all candidate versions
	// are incompatible, the fallback depends on EngineStrict.
	NodeVersion string

	// EngineStrict: when true, resolution fails with an error if no version
	// of a package is compatible with NodeVersion. When false (default),
	// the best-available version is used despite the incompatibility,
	// matching npm's advisory behavior. Sourced from .npmrc `engine-strict=true`.
	EngineStrict bool
}

// Resolver takes a project spec and produces a fully resolved dependency graph.
type Resolver interface {
	Resolve(ctx context.Context, project *ProjectSpec, registry Registry, opts ResolveOptions) (*Graph, error)
}
