package pnpm

import (
	"context"

	"github.com/jumoel/locksmith/ecosystem"
)

// Resolver implements pnpm-style dependency resolution (strict, no hoisting).
type Resolver struct {
	PolicyOverride *ecosystem.ResolverPolicy
}

// NewResolver returns a new pnpm dependency resolver.
func NewResolver() *Resolver {
	return &Resolver{}
}

// ResolveResult holds the pnpm-specific resolution output.
type ResolveResult struct {
	Graph *ecosystem.Graph
	// Packages maps "name@version" to resolved metadata.
	Packages map[string]*ResolvedPackage
}

// ResolvedPackage holds pnpm-specific resolution info for a package.
type ResolvedPackage struct {
	Node         *ecosystem.Node
	Dependencies map[string]string // name -> resolved version
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
// including pnpm-specific package metadata needed for lockfile generation.
func (r *Resolver) ResolveForLockfile(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ResolveResult, error) {
	packages := make(map[string]*ResolvedPackage)

	policy := ecosystem.ResolverPolicy{
		CrossTreeDedup:   true,
		AutoInstallPeers: true,
	}
	policy.ApplyOverride(r.PolicyOverride)
	// OnNodeResolved is always set by this resolver, never overridden.
	policy.OnNodeResolved = func(key string, node *ecosystem.Node, meta *ecosystem.VersionMetadata, edges []*ecosystem.Edge) {
		resolvedDeps := make(map[string]string)
		for _, e := range edges {
			if e.Target != nil {
				// Use the target's real name, not the edge's alias name.
				// This ensures dep references match the packages section keys
				// (e.g., "string-width" not "string-width-cjs").
				resolvedDeps[e.Target.Name] = e.Target.Version
			}
		}
		packages[key] = &ResolvedPackage{Node: node, Dependencies: resolvedDeps}
	}

	graph, err := ecosystem.Resolve(ctx, project, registry, opts, policy)
	if err != nil {
		return nil, err
	}

	return &ResolveResult{Graph: graph, Packages: packages}, nil
}

