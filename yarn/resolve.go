package yarn

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/semver"
)

// Resolver implements yarn-classic-style dependency resolution (strict, no hoisting).
// Yarn classic resolves to flat versions like pnpm - the lockfile contains a flat
// list of resolved packages, not a nested tree.
type Resolver struct{}

// NewResolver returns a new yarn dependency resolver.
func NewResolver() *Resolver {
	return &Resolver{}
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
	res := &yarnResolver{
		registry:  registry,
		cutoff:    opts.CutoffDate,
		ctx:       ctx,
		nodes:     make(map[string]*ecosystem.Node),
		resolving: make(map[string]bool),
		packages:  make(map[string]*ResolvedPackage),
	}

	graph, err := res.resolve(project)
	if err != nil {
		return nil, err
	}

	return &ResolveResult{
		Graph:    graph,
		Packages: res.packages,
	}, nil
}

// yarnResolver holds state during resolution.
type yarnResolver struct {
	registry  ecosystem.Registry
	cutoff    *time.Time
	ctx       context.Context
	nodes     map[string]*ecosystem.Node // cache: "name@version" -> node
	resolving map[string]bool            // cycle detection
	packages  map[string]*ResolvedPackage
}

// resolve builds the dependency graph from a project spec.
func (r *yarnResolver) resolve(project *ecosystem.ProjectSpec) (*ecosystem.Graph, error) {
	graph := &ecosystem.Graph{
		Root:  &ecosystem.Node{Name: project.Name, Version: project.Version},
		Nodes: make(map[string]*ecosystem.Node),
	}

	for _, dep := range project.Dependencies {
		node, err := r.resolveDep(graph, dep.Name, dep.Constraint, dep.Type)
		if err != nil {
			if dep.Type == ecosystem.DepOptional {
				continue
			}
			return nil, fmt.Errorf("resolving %s@%s: %w", dep.Name, dep.Constraint, err)
		}
		graph.Root.Dependencies = append(graph.Root.Dependencies, &ecosystem.Edge{
			Name: dep.Name, Constraint: dep.Constraint, Target: node, Type: dep.Type,
		})
	}

	return graph, nil
}

// resolveDep resolves a single dependency to a specific version,
// then recursively resolves its transitive dependencies.
func (r *yarnResolver) resolveDep(graph *ecosystem.Graph, name, constraint string, depType ecosystem.DepType) (*ecosystem.Node, error) {
	c, err := semver.ParseConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("parsing constraint %q: %w", constraint, err)
	}

	versions, err := r.registry.FetchVersions(r.ctx, name, r.cutoff)
	if err != nil {
		return nil, err
	}

	var parsed []*semver.Version
	versionMap := make(map[string]string) // normalized -> original
	for _, vi := range versions {
		v, err := semver.Parse(vi.Version)
		if err != nil {
			continue
		}
		parsed = append(parsed, v)
		versionMap[v.String()] = vi.Version
	}

	// Cross-tree dedup: reuse an already-resolved version if it satisfies.
	for key, node := range r.nodes {
		if !strings.HasPrefix(key, name+"@") {
			continue
		}
		existingVer, err := semver.Parse(node.Version)
		if err == nil && c.Check(existingVer) {
			return node, nil
		}
	}

	distTags, _ := r.registry.FetchDistTags(r.ctx, name)
	best := semver.PickVersion(parsed, c, distTags["latest"])
	if best == nil {
		return nil, fmt.Errorf("no version of %s satisfies %s", name, constraint)
	}

	version := versionMap[best.String()]
	key := name + "@" + version

	// Dedup: return cached node if already resolved.
	if node, ok := r.nodes[key]; ok {
		return node, nil
	}

	// Cycle detection: if already resolving this package, create a stub.
	if r.resolving[key] {
		node := &ecosystem.Node{Name: name, Version: version}
		r.nodes[key] = node
		graph.Nodes[key] = node
		return node, nil
	}
	r.resolving[key] = true
	defer func() { delete(r.resolving, key) }()

	meta, err := r.registry.FetchMetadata(r.ctx, name, version)
	if err != nil {
		return nil, err
	}

	node := &ecosystem.Node{
		Name:             meta.Name,
		Version:          meta.Version,
		Integrity:        meta.Integrity,
		TarballURL:       meta.TarballURL,
		HasInstallScript: meta.HasInstallScript,
		Engines:          meta.Engines,
		OS:               meta.OS,
		CPU:              meta.CPU,
		Bin:              meta.Bin,
		License:          meta.License,
		Deprecated:       meta.Deprecated,
	}
	if depType == ecosystem.DepDev {
		node.DevOnly = true
	}
	if depType == ecosystem.DepOptional {
		node.Optional = true
	}

	r.nodes[key] = node
	graph.Nodes[key] = node

	// Track resolved deps for yarn lockfile format.
	resolvedDeps := make(map[string]string)

	// Resolve regular dependencies.
	depNames := sortedKeys(meta.Dependencies)
	for _, depName := range depNames {
		depConstraint := meta.Dependencies[depName]
		child, err := r.resolveDep(graph, depName, depConstraint, ecosystem.DepRegular)
		if err != nil {
			return nil, err
		}
		node.Dependencies = append(node.Dependencies, &ecosystem.Edge{
			Name: depName, Constraint: depConstraint, Target: child, Type: ecosystem.DepRegular,
		})
		resolvedDeps[depName] = child.Version
	}

	// Resolve optional dependencies (failures are not fatal).
	optNames := sortedKeys(meta.OptionalDeps)
	for _, depName := range optNames {
		if _, already := meta.Dependencies[depName]; already {
			continue
		}
		depConstraint := meta.OptionalDeps[depName]
		child, err := r.resolveDep(graph, depName, depConstraint, ecosystem.DepOptional)
		if err != nil {
			continue
		}
		node.Dependencies = append(node.Dependencies, &ecosystem.Edge{
			Name: depName, Constraint: depConstraint, Target: child, Type: ecosystem.DepOptional,
		})
		resolvedDeps[depName] = child.Version
	}

	r.packages[key] = &ResolvedPackage{
		Node:         node,
		Dependencies: resolvedDeps,
	}

	return node, nil
}

// sortedKeys returns the keys of a map in sorted order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
