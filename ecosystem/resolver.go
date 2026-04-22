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
}

// Resolver takes a project spec and produces a fully resolved dependency graph.
type Resolver interface {
	Resolve(ctx context.Context, project *ProjectSpec, registry Registry, opts ResolveOptions) (*Graph, error)
}
