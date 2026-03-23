package npm

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/semver"
)

// Resolver implements npm-style dependency resolution with hoisting.
type Resolver struct{}

// NewResolver returns a new npm dependency resolver.
func NewResolver() *Resolver {
	return &Resolver{}
}

// PlacedNode represents a resolved package placed at a specific location
// in the node_modules tree.
type PlacedNode struct {
	// Path is the node_modules path, e.g., "node_modules/foo" or
	// "node_modules/bar/node_modules/foo".
	Path string

	// Node is the resolved package metadata.
	Node *ecosystem.Node

	// Children are packages nested directly under this one's node_modules.
	Children map[string]*PlacedNode

	// Parent is the parent in the node_modules tree (nil for root).
	Parent *PlacedNode
}

// ResolveResult extends the ecosystem.Graph with npm-specific placement info.
type ResolveResult struct {
	Graph *ecosystem.Graph
	// Root is the root of the placed node tree (represents the project itself).
	Root *PlacedNode
	// PlacedNodes maps path -> placed node for all packages.
	PlacedNodes map[string]*PlacedNode
}

// Resolve satisfies the ecosystem.Resolver interface by returning just the graph.
func (r *Resolver) Resolve(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ecosystem.Graph, error) {
	result, err := r.ResolveWithPlacement(ctx, project, registry, opts)
	if err != nil {
		return nil, err
	}
	return result.Graph, nil
}

// ResolveWithPlacement resolves all dependencies and produces a dependency graph
// with npm-style hoisted placement information.
func (r *Resolver) ResolveWithPlacement(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ResolveResult, error) {
	res := &resolver{
		registry:  registry,
		cutoff:    opts.CutoffDate,
		ctx:       ctx,
		nodes:     make(map[string]*ecosystem.Node),
		resolving: make(map[string]bool),
	}

	// Phase 1: Build the logical dependency graph.
	graph, err := res.resolveLogical(project)
	if err != nil {
		return nil, fmt.Errorf("logical resolution: %w", err)
	}

	// Phase 2: Compute physical placement (hoisting).
	result, err := res.computePlacement(graph)
	if err != nil {
		return nil, fmt.Errorf("computing placement: %w", err)
	}

	return result, nil
}

// resolver holds state during resolution.
type resolver struct {
	registry  ecosystem.Registry
	cutoff    *time.Time
	ctx       context.Context
	nodes     map[string]*ecosystem.Node // cache: "name@version" -> node
	resolving map[string]bool            // cycle detection: "name@version" -> in progress
}

// resolveLogical builds the logical dependency graph.
func (r *resolver) resolveLogical(project *ecosystem.ProjectSpec) (*ecosystem.Graph, error) {
	graph := &ecosystem.Graph{
		Nodes: make(map[string]*ecosystem.Node),
	}

	// Create root node for the project.
	root := &ecosystem.Node{
		Name:    project.Name,
		Version: project.Version,
	}
	graph.Root = root

	// Resolve each declared dependency.
	for _, dep := range project.Dependencies {
		node, err := r.resolveDep(graph, dep.Name, dep.Constraint, dep.Type)
		if err != nil {
			// Optional dependencies don't fail resolution.
			if dep.Type == ecosystem.DepOptional {
				continue
			}
			return nil, fmt.Errorf("resolving %s@%s: %w", dep.Name, dep.Constraint, err)
		}

		root.Dependencies = append(root.Dependencies, &ecosystem.Edge{
			Name:       dep.Name,
			Constraint: dep.Constraint,
			Target:     node,
			Type:       dep.Type,
		})
	}

	return graph, nil
}

// resolveDep resolves a single dependency to a specific version,
// then recursively resolves its transitive dependencies.
func (r *resolver) resolveDep(graph *ecosystem.Graph, name, constraint string, depType ecosystem.DepType) (*ecosystem.Node, error) {
	// Parse the constraint.
	c, err := semver.ParseConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("parsing constraint %q: %w", constraint, err)
	}

	// Fetch available versions.
	versions, err := r.registry.FetchVersions(r.ctx, name, r.cutoff)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s: %w", name, err)
	}

	// Parse and filter versions.
	var parsed []*semver.Version
	versionMap := make(map[string]string) // normalized -> original
	for _, vi := range versions {
		v, err := semver.Parse(vi.Version)
		if err != nil {
			continue // Skip unparseable versions.
		}
		parsed = append(parsed, v)
		versionMap[v.String()] = vi.Version
	}

	// Pick the highest satisfying version.
	best := semver.MaxSatisfying(parsed, c)
	if best == nil {
		return nil, fmt.Errorf("no version of %s satisfies %s", name, constraint)
	}

	version := versionMap[best.String()]
	key := name + "@" + version

	// Check if already resolved (dedup).
	if node, ok := r.nodes[key]; ok {
		return node, nil
	}

	// Cycle detection: if we're already in the process of resolving this
	// exact package@version, create a stub node and return it. The full
	// node will be populated by the outer call.
	if r.resolving[key] {
		node := &ecosystem.Node{
			Name:    name,
			Version: version,
		}
		r.nodes[key] = node
		graph.Nodes[key] = node
		return node, nil
	}
	r.resolving[key] = true
	defer func() { delete(r.resolving, key) }()

	// Fetch full metadata.
	meta, err := r.registry.FetchMetadata(r.ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s@%s: %w", name, version, err)
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

	// Resolve transitive dependencies (regular).
	depNames := sortedKeys(meta.Dependencies)
	for _, depName := range depNames {
		depConstraint := meta.Dependencies[depName]
		child, err := r.resolveDep(graph, depName, depConstraint, ecosystem.DepRegular)
		if err != nil {
			return nil, fmt.Errorf("resolving transitive dep %s@%s (from %s): %w", depName, depConstraint, key, err)
		}
		node.Dependencies = append(node.Dependencies, &ecosystem.Edge{
			Name:       depName,
			Constraint: depConstraint,
			Target:     child,
			Type:       ecosystem.DepRegular,
		})
	}

	// Resolve optional dependencies (failures are not fatal).
	optNames := sortedKeys(meta.OptionalDeps)
	for _, depName := range optNames {
		if _, already := meta.Dependencies[depName]; already {
			continue // Already resolved as regular dep.
		}
		depConstraint := meta.OptionalDeps[depName]
		child, err := r.resolveDep(graph, depName, depConstraint, ecosystem.DepOptional)
		if err != nil {
			continue // Optional deps don't fail resolution.
		}
		node.Dependencies = append(node.Dependencies, &ecosystem.Edge{
			Name:       depName,
			Constraint: depConstraint,
			Target:     child,
			Type:       ecosystem.DepOptional,
		})
	}

	return node, nil
}

// computePlacement takes the logical graph and determines where each package
// lives in the node_modules hierarchy (hoisting/dedup).
func (r *resolver) computePlacement(graph *ecosystem.Graph) (*ResolveResult, error) {
	result := &ResolveResult{
		Graph:       graph,
		PlacedNodes: make(map[string]*PlacedNode),
	}

	// Create root placed node representing the project itself.
	rootPlaced := &PlacedNode{
		Path:     "",
		Node:     graph.Root,
		Children: make(map[string]*PlacedNode),
	}
	result.Root = rootPlaced

	// Place each dependency using hoisting.
	placeChildren(rootPlaced, graph.Root, result)

	return result, nil
}

// placeChildren places all dependencies of a node into the node_modules tree,
// hoisting as high as possible.
func placeChildren(parent *PlacedNode, node *ecosystem.Node, result *ResolveResult) {
	for _, edge := range node.Dependencies {
		placeDep(parent, edge, result)
	}
}

// placeDep attempts to place a dependency as high as possible in the tree.
func placeDep(requiredBy *PlacedNode, edge *ecosystem.Edge, result *ResolveResult) {
	target := edge.Target
	if target == nil {
		return
	}

	// Try to hoist: walk up the tree to find the highest placement
	// where no conflicting version exists.
	bestPlacement := requiredBy
	current := requiredBy

	for current != nil {
		existing, hasExisting := current.Children[edge.Name]
		if hasExisting {
			if existing.Node.Version == target.Version {
				// Same version already placed here - deduplicated.
				return
			}
			// Conflict at this level - can't hoist to or past here.
			break
		}

		// Check if an ancestor already has this package with a different version.
		if ancestorHasConflict(current, edge.Name, target.Version) {
			break
		}

		bestPlacement = current
		current = current.Parent
	}

	// Build the path for this placement.
	path := buildPath(bestPlacement, edge.Name)

	// If already placed at this exact path, nothing to do.
	if _, exists := result.PlacedNodes[path]; exists {
		return
	}

	// Place the node.
	placed := &PlacedNode{
		Path:     path,
		Node:     target,
		Children: make(map[string]*PlacedNode),
		Parent:   bestPlacement,
	}
	bestPlacement.Children[edge.Name] = placed
	result.PlacedNodes[path] = placed

	// Recursively place this node's dependencies.
	placeChildren(placed, target, result)
}

// ancestorHasConflict checks if any ancestor of the given node already has
// a different version of the named package placed.
func ancestorHasConflict(node *PlacedNode, name, version string) bool {
	current := node.Parent
	for current != nil {
		if existing, ok := current.Children[name]; ok {
			return existing.Node.Version != version
		}
		current = current.Parent
	}
	return false
}

// buildPath constructs the node_modules path for a placed package.
func buildPath(parent *PlacedNode, name string) string {
	if parent.Path == "" {
		return "node_modules/" + name
	}
	return parent.Path + "/node_modules/" + name
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
