package ecosystem

import (
	"context"
	"time"
)

// ResolveOptions configures the dependency resolution process.
type ResolveOptions struct {
	CutoffDate *time.Time
	// SpecDir is the directory containing the spec file, for resolving
	// file: dependencies by reading local package versions.
	SpecDir string
	// WorkspaceIndex provides workspace member lookups for resolving workspace: protocol deps.
	// Nil for single-package projects.
	WorkspaceIndex *WorkspaceIndex
	// NodeVersion, if set, skips package versions whose engines.node
	// constraint is incompatible with this version during resolution.
	// Format: semver string (e.g., "18.0.0"). When all candidate versions
	// are incompatible, the best version is used regardless (matches npm behavior).
	NodeVersion string
}

// Resolver takes a project spec and produces a fully resolved dependency graph.
type Resolver interface {
	Resolve(ctx context.Context, project *ProjectSpec, registry Registry, opts ResolveOptions) (*Graph, error)
}
