package bun

import (
	"context"

	"github.com/jumoel/locksmith/ecosystem"
)

// Resolver implements bun-style dependency resolution as a thin wrapper
// around the shared resolution engine.
type Resolver struct {
	// PolicyOverride, if set, overrides the default resolution policy.
	PolicyOverride *ecosystem.ResolverPolicy
}

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
	Dependencies map[string]DepInfo // name -> dep info
}

// DepInfo holds both the constraint and the resolved version for a dependency.
type DepInfo struct {
	Constraint      string
	ResolvedName    string
	ResolvedVersion string
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
	}
	if r.PolicyOverride != nil {
		policy.CrossTreeDedup = r.PolicyOverride.CrossTreeDedup
		policy.AutoInstallPeers = r.PolicyOverride.AutoInstallPeers
		policy.StorePeerMetaOnNode = r.PolicyOverride.StorePeerMetaOnNode
	}
	// OnNodeResolved is always set by this resolver, never overridden.
	policy.OnNodeResolved = func(key string, node *ecosystem.Node, meta *ecosystem.VersionMetadata, edges []*ecosystem.Edge) {
		deps := make(map[string]DepInfo)
		for _, e := range edges {
			if e.Target != nil {
				// Skip peer dep edges - bun doesn't auto-install optional
				// peers and handles peer resolution internally.
				if e.Type == ecosystem.DepPeer {
					continue
				}
				deps[e.Target.Name] = DepInfo{
					Constraint:      e.Constraint,
					ResolvedName:    e.Target.Name,
					ResolvedVersion: e.Target.Version,
				}
			}
		}
		packages[key] = &ResolvedPackage{Node: node, Dependencies: deps}
	}

	graph, err := ecosystem.Resolve(ctx, project, registry, opts, policy)
	if err != nil {
		return nil, err
	}

	return &ResolveResult{Graph: graph, Packages: packages}, nil
}
