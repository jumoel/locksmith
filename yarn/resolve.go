package yarn

import (
	"context"

	"github.com/jumoel/locksmith/ecosystem"
)

// Resolver implements yarn dependency resolution as a thin wrapper around
// the shared resolution engine.
type Resolver struct {
	// AutoInstallPeers controls whether peer dependencies are automatically
	// resolved. Yarn classic (v1) does NOT auto-install peers. Yarn berry
	// (v2+) does.
	AutoInstallPeers bool

	// VersionSelection controls version picking behavior. Yarn classic uses
	// the default (VersionSelectPreferLatest); yarn berry uses VersionSelectHighest.
	VersionSelection ecosystem.VersionSelection

	// PolicyOverride, if set, overrides the default resolution policy.
	PolicyOverride *ecosystem.ResolverPolicy
}

// NewResolver returns a new yarn dependency resolver with default settings.
// Defaults to no peer auto-install (yarn classic behavior).
func NewResolver() *Resolver {
	return &Resolver{AutoInstallPeers: false}
}

// NewBerryResolver returns a yarn resolver with peer auto-install enabled
// and highest-version selection (yarn berry v2+ behavior).
func NewBerryResolver() *Resolver {
	return &Resolver{
		AutoInstallPeers: true,
		VersionSelection: ecosystem.VersionSelectHighest,
	}
}

// ResolveResult holds the yarn-specific resolution output.
type ResolveResult struct {
	Graph *ecosystem.Graph
	// Packages maps "name@version" to resolved metadata.
	Packages map[string]*ResolvedPackage
}

// ResolvedPackage holds yarn-specific resolution info for a package.
type ResolvedPackage struct {
	Node         *ecosystem.Node
	Dependencies map[string]string // name -> resolved version
	PeerDepsMeta map[string]ecosystem.PeerDepMeta
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
// including yarn-specific package metadata needed for lockfile generation.
func (r *Resolver) ResolveForLockfile(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ResolveResult, error) {
	packages := make(map[string]*ResolvedPackage)

	policy := ecosystem.ResolverPolicy{
		CrossTreeDedup:   false, // yarn resolves each constraint independently
		AutoInstallPeers: r.AutoInstallPeers,
		VersionSelection: r.VersionSelection,
	}
	policy.ApplyOverride(r.PolicyOverride)
	// OnNodeResolved is always set by this resolver, never overridden.
	policy.OnNodeResolved = func(key string, node *ecosystem.Node, meta *ecosystem.VersionMetadata, edges []*ecosystem.Edge) {
		resolvedDeps := make(map[string]string)
		for _, e := range edges {
			if e.Target != nil {
				resolvedDeps[e.Name] = e.Target.Version
			}
		}
		pkg := &ResolvedPackage{Node: node, Dependencies: resolvedDeps}
		if len(meta.PeerDepsMeta) > 0 {
			pkg.PeerDepsMeta = meta.PeerDepsMeta
		}
		packages[key] = pkg
	}

	graph, err := ecosystem.Resolve(ctx, project, registry, opts, policy)
	if err != nil {
		return nil, err
	}

	return &ResolveResult{Graph: graph, Packages: packages}, nil
}

