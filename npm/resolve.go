package npm

import (
	"context"
	"fmt"
	"sort"
	"strings"
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
	registry    ecosystem.Registry
	cutoff      *time.Time
	ctx         context.Context
	nodes       map[string]*ecosystem.Node // cache: "name@version" -> node
	resolving   map[string]bool            // cycle detection: "name@version" -> in progress
	projectDeps map[string]bool            // names of packages declared at project level
}

// resolveLogical builds the logical dependency graph.
func (r *resolver) resolveLogical(project *ecosystem.ProjectSpec) (*ecosystem.Graph, error) {
	// Record project-level dep names for peer dep provider checks.
	r.projectDeps = make(map[string]bool)
	for _, dep := range project.Dependencies {
		r.projectDeps[dep.Name] = true
	}

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

	// Cross-tree deduplication for transitive deps only: if an existing
	// resolved version satisfies the constraint AND this is a transitive dep
	// (not a root dep), reuse it. Root deps always get fresh resolution to
	// allow multiple versions (e.g., arborist-dedupe needs b@1.0.0 nested
	// under a AND b@2.0.0 at root).
	if !r.projectDeps[name] {
		for key, node := range r.nodes {
			if !strings.HasPrefix(key, name+"@") {
				continue
			}
			existingVer, err := semver.Parse(node.Version)
			if err == nil && c.Check(existingVer) {
				return node, nil
			}
		}
	}

	// npm-pick-manifest algorithm: prefer the "latest" dist-tag version
	// if it satisfies the constraint. Otherwise fall back to highest matching.
	distTags, _ := r.registry.FetchDistTags(r.ctx, name)
	best := semver.PickVersion(parsed, c, distTags["latest"])
	if best == nil {
		return nil, fmt.Errorf("no version of %s satisfies %s", name, constraint)
	}

	version := versionMap[best.String()]
	key := name + "@" + version

	// Check if already resolved (exact version dedup).
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
		PeerDeps:         meta.PeerDeps,
		PeerDepsMeta:     meta.PeerDepsMeta,
		Funding:          meta.Funding,
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

	// Auto-install peer dependencies (npm 7+ behavior).
	// Skip optional peer deps and peers already provided by the project
	// or already resolved elsewhere in the tree.
	peerNames := sortedKeys(meta.PeerDeps)
	for _, depName := range peerNames {
		// Skip if already resolved as a regular or optional dep.
		alreadyResolved := false
		for _, edge := range node.Dependencies {
			if edge.Name == depName {
				alreadyResolved = true
				break
			}
		}
		if alreadyResolved {
			continue
		}
		// Skip optional peer deps.
		if pm, ok := meta.PeerDepsMeta[depName]; ok && pm.Optional {
			continue
		}
		// Skip if provided by the project.
		if r.projectDeps[depName] {
			continue
		}
		// Skip if already resolved elsewhere in the tree.
		found := false
		for k := range r.nodes {
			if strings.HasPrefix(k, depName+"@") {
				found = true
				break
			}
		}
		if found {
			continue
		}
		depConstraint := meta.PeerDeps[depName]
		child, err := r.resolveDep(graph, depName, depConstraint, ecosystem.DepPeer)
		if err != nil {
			continue // Peer dep resolution failure is non-fatal.
		}
		node.Dependencies = append(node.Dependencies, &ecosystem.Edge{
			Name:       depName,
			Constraint: depConstraint,
			Target:     child,
			Type:       ecosystem.DepPeer,
		})
	}

	return node, nil
}

// computePlacement takes the logical graph and determines where each package
// lives in the node_modules hierarchy (hoisting/dedup).
//
// Root dependencies are placed first to ensure they get the root-level slot.
// Then transitive dependencies are placed with hoisting, nesting when conflicts
// arise with already-placed root deps.
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

	// Phase 1: Place root's direct dependencies at root level first.
	// This ensures root deps always win the root-level slot, matching
	// npm's Arborist behavior.
	for _, edge := range graph.Root.Dependencies {
		if edge.Target == nil {
			continue
		}
		path := "node_modules/" + edge.Name
		placed := &PlacedNode{
			Path:     path,
			Node:     edge.Target,
			Children: make(map[string]*PlacedNode),
			Parent:   rootPlaced,
		}
		rootPlaced.Children[edge.Name] = placed
		result.PlacedNodes[path] = placed
	}

	// Phase 2: Place transitive dependencies using BFS.
	// BFS ensures shallower deps get the root slot before deeper deps,
	// matching npm's Arborist behavior where direct dep transitive deps
	// take priority over deeply nested ones.
	type placeWork struct {
		parent *PlacedNode
		node   *ecosystem.Node
	}
	queue := make([]placeWork, 0)
	for _, edge := range graph.Root.Dependencies {
		if edge.Target == nil {
			continue
		}
		queue = append(queue, placeWork{
			parent: rootPlaced.Children[edge.Name],
			node:   edge.Target,
		})
	}

	seen := make(map[string]bool)
	for len(queue) > 0 {
		work := queue[0]
		queue = queue[1:]

		for _, edge := range work.node.Dependencies {
			if edge.Target == nil {
				continue
			}

			placed := placeDep(work.parent, edge, result)
			if placed != nil {
				key := edge.Target.Name + "@" + edge.Target.Version
				if !seen[key] {
					seen[key] = true
					queue = append(queue, placeWork{parent: placed, node: edge.Target})
				}
			}
		}
	}

	return result, nil
}

// placeDep attempts to place a dependency as high as possible in the tree.
// Returns the placed node (or existing node if deduplicated), or nil if target is nil.
//
// The algorithm walks up from requiredBy toward root. At each level it checks:
// 1. Does this level already have a DIFFERENT version of the same package? -> stop, can't go higher
// 2. Does this level already have the SAME version? -> deduplicate, return existing
// 3. Otherwise -> this level is a valid placement candidate, keep going up
func placeDep(requiredBy *PlacedNode, edge *ecosystem.Edge, result *ResolveResult) *PlacedNode {
	target := edge.Target
	if target == nil {
		return nil
	}

	// Walk up from requiredBy to find the shallowest valid placement.
	bestPlacement := requiredBy
	current := requiredBy

	for current != nil {
		existing, hasExisting := current.Children[edge.Name]
		if hasExisting {
			if existing.Node.Version == target.Version {
				// Same version already placed here - deduplicated.
				return existing
			}
			// Conflict at this level - can't place here or higher.
			break
		}

		// This level has no conflict - it's a valid placement.
		bestPlacement = current
		current = current.Parent
	}

	// Build the path for this placement.
	path := buildPath(bestPlacement, edge.Name)

	// If already placed at this exact path, nothing to do.
	if existing, ok := result.PlacedNodes[path]; ok {
		return existing
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

	return placed
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
