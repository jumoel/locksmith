package npm

import (
	"context"
	"fmt"

	"github.com/jumoel/locksmith/ecosystem"
)

// Resolver implements npm-style dependency resolution with hoisting.
type Resolver struct {
	// PolicyOverride, if set, overrides the default resolution policy.
	PolicyOverride *ecosystem.ResolverPolicy
}

// NewResolver returns a new npm dependency resolver.
func NewResolver() *Resolver {
	return &Resolver{}
}

// PlacedNode represents a resolved package placed at a specific location
// in the node_modules tree.
type PlacedNode struct {
	Path     string
	Node     *ecosystem.Node
	Children map[string]*PlacedNode
	Parent   *PlacedNode
}

// ResolveResult extends the ecosystem.Graph with npm-specific placement info.
type ResolveResult struct {
	Graph       *ecosystem.Graph
	Root        *PlacedNode
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

// ResolveWithPlacement resolves all dependencies and computes npm-style
// hoisted placement in the node_modules tree.
func (r *Resolver) ResolveWithPlacement(ctx context.Context, project *ecosystem.ProjectSpec, registry ecosystem.Registry, opts ecosystem.ResolveOptions) (*ResolveResult, error) {
	policy := ecosystem.ResolverPolicy{
		CrossTreeDedup:      true,
		AutoInstallPeers:    true,
		StorePeerMetaOnNode: true,
	}
	policy.ApplyOverride(r.PolicyOverride)

	graph, err := ecosystem.Resolve(ctx, project, registry, opts, policy)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	return computePlacement(graph)
}

// computePlacement takes the logical graph and determines where each package
// lives in the node_modules hierarchy (hoisting/dedup).
//
// Root dependencies are placed first to ensure they get the root-level slot.
// Then transitive dependencies are placed with BFS hoisting, nesting when
// conflicts arise.
func computePlacement(graph *ecosystem.Graph) (*ResolveResult, error) {
	result := &ResolveResult{
		Graph:       graph,
		PlacedNodes: make(map[string]*PlacedNode),
	}

	rootPlaced := &PlacedNode{
		Path:     "",
		Node:     graph.Root,
		Children: make(map[string]*PlacedNode),
	}
	result.Root = rootPlaced

	// Phase 1: Place root's direct dependencies at root level first.
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
	type placeWork struct {
		parent *PlacedNode
		node   *ecosystem.Node
	}
	var queue []placeWork
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
				// Use path as seen key so that the same package placed at
				// different locations in the tree gets its deps processed
				// at each location. Using just name@version caused transitive
				// deps to be missing when a package was nested in multiple places.
				key := placed.Path
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
func placeDep(requiredBy *PlacedNode, edge *ecosystem.Edge, result *ResolveResult) *PlacedNode {
	target := edge.Target
	if target == nil {
		return nil
	}

	bestPlacement := requiredBy
	current := requiredBy

	for current != nil {
		existing, hasExisting := current.Children[edge.Name]
		if hasExisting {
			if existing.Node.Version == target.Version {
				return existing
			}
			break
		}
		bestPlacement = current
		current = current.Parent
	}

	path := buildPath(bestPlacement, edge.Name)
	if existing, ok := result.PlacedNodes[path]; ok {
		return existing
	}

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

func buildPath(parent *PlacedNode, name string) string {
	if parent.Path == "" {
		return "node_modules/" + name
	}
	return parent.Path + "/node_modules/" + name
}
