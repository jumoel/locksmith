package bun

import (
	"context"

	"github.com/jumoel/locksmith/ecosystem"
)

// Resolver implements bun-style dependency resolution as a thin wrapper
// around the shared resolution engine.
type Resolver struct{}

// NewResolver returns a new bun dependency resolver.
func NewResolver() *Resolver { return &Resolver{} }

// ResolveResult holds the bun-specific resolution output.
type ResolveResult struct {
	Graph *ecosystem.Graph
	// Packages maps "name@version" to resolved metadata.
	Packages map[string]*ResolvedPackage
}

// ResolvedPackage holds bun-specific resolution info for a package.
type ResolvedPackage struct {
	Node         *ecosystem.Node
	Dependencies map[string]string // name -> constraint (not resolved version)
}

// Resolve satisfies the ecosystem.Resolver interface by returning just the graph.
func (r *Resolver) Resolve(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ecosystem.Graph, error) {
	result, err := r.ResolveForLockfile(ctx, project, registry, opts)
	if err != nil {
		return nil, err
	}
	return result.Graph, nil
}

// ResolveForLockfile resolves all dependencies and returns the full result
// including bun-specific package metadata needed for lockfile generation.
func (r *Resolver) ResolveForLockfile(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ResolveResult, error) {
	packages := make(map[string]*ResolvedPackage)

	policy := ecosystem.ResolverPolicy{
		CrossTreeDedup:   true, // bun deduplicates like pnpm
		AutoInstallPeers: true,
		OnNodeResolved: func(key string, node *ecosystem.Node, meta *ecosystem.VersionMetadata, edges []*ecosystem.Edge) {
			depConstraints := make(map[string]string)
			for _, e := range edges {
				depConstraints[e.Name] = e.Constraint // bun stores constraints
			}
			packages[key] = &ResolvedPackage{Node: node, Dependencies: depConstraints}
		},
	}

	graph, err := ecosystem.Resolve(ctx, project, registry, opts, policy)
	if err != nil {
		return nil, err
	}

	return &ResolveResult{Graph: graph, Packages: packages}, nil
}
