package ecosystem

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jumoel/locksmith/internal/maputil"
	"github.com/jumoel/locksmith/internal/semver"
)

// ResolverPolicy parameterizes the shared resolution algorithm.
type ResolverPolicy struct {
	// CrossTreeDedup: reuse an already-resolved version of a transitive dep
	// when it satisfies the current constraint (npm, pnpm, bun behavior).
	// When false, each constraint resolves independently (yarn behavior).
	CrossTreeDedup bool

	// AutoInstallPeers: auto-resolve non-optional peer deps that aren't
	// provided by the project or already resolved (npm 7+, pnpm, yarn-berry, bun).
	AutoInstallPeers bool

	// StorePeerMetaOnNode: copy PeerDeps and PeerDepsMeta from metadata
	// onto the Node (npm needs this for lockfile output).
	StorePeerMetaOnNode bool

	// OnNodeResolved is called after each node is fully resolved including
	// all transitive deps. PM-specific resolvers use this to collect their
	// own bookkeeping data.
	OnNodeResolved func(key string, node *Node, meta *VersionMetadata, edges []*Edge)
}

// resolverState holds state during resolution.
type resolverState struct {
	registry    Registry
	cutoff      *time.Time
	ctx         context.Context
	nodes       map[string]*Node // "name@version" -> node
	nodeIndex   *NodeIndex       // O(1) name lookups
	resolving   map[string]bool  // cycle detection
	projectDeps map[string]bool  // root dep names
	policy      ResolverPolicy
}

// Resolve executes the shared dependency resolution algorithm.
// PM-specific data is collected via the policy.OnNodeResolved callback.
func Resolve(ctx context.Context, project *ProjectSpec, registry Registry, opts ResolveOptions, policy ResolverPolicy) (*Graph, error) {
	state := &resolverState{
		registry:    registry,
		cutoff:      opts.CutoffDate,
		ctx:         ctx,
		nodes:       make(map[string]*Node),
		nodeIndex:   NewNodeIndex(),
		resolving:   make(map[string]bool),
		projectDeps: make(map[string]bool),
		policy:      policy,
	}

	for _, dep := range project.Dependencies {
		state.projectDeps[dep.Name] = true
	}

	graph := &Graph{
		Root:  &Node{Name: project.Name, Version: project.Version},
		Nodes: make(map[string]*Node),
	}

	for _, dep := range project.Dependencies {
		node, err := state.resolveDep(graph, dep.Name, dep.Constraint, dep.Type)
		if err != nil {
			if dep.Type == DepOptional {
				continue
			}
			return nil, fmt.Errorf("resolving %s@%s: %w", dep.Name, dep.Constraint, err)
		}
		graph.Root.Dependencies = append(graph.Root.Dependencies, &Edge{
			Name: dep.Name, Constraint: dep.Constraint, Target: node, Type: dep.Type,
		})
	}

	return graph, nil
}

func (s *resolverState) resolveDep(graph *Graph, name, constraint string, depType DepType) (*Node, error) {
	// Handle npm alias syntax: "npm:actual-package@^1.0.0"
	// The alias name is used for the dependency key, but the actual package
	// name and constraint are extracted for registry resolution.
	actualName := name
	actualConstraint := constraint
	if strings.HasPrefix(constraint, "npm:") {
		aliasSpec := strings.TrimPrefix(constraint, "npm:")
		// Split "package-name@constraint" - handle scoped packages like @scope/pkg@^1.0.0
		atIdx := strings.LastIndex(aliasSpec, "@")
		if atIdx > 0 {
			actualName = aliasSpec[:atIdx]
			actualConstraint = aliasSpec[atIdx+1:]
		} else {
			actualName = aliasSpec
			actualConstraint = "*"
		}
	}

	// Detect non-registry dependency specifiers (file:, git+, github:, etc.).
	// These can't be resolved from the npm registry. Create a placeholder node
	// so the lockfile structure is valid but with minimal metadata.
	if isNonRegistrySpecifier(actualConstraint) {
		key := actualName + "@" + actualConstraint
		if node, ok := s.nodes[key]; ok {
			return node, nil
		}
		node := &Node{
			Name:    actualName,
			Version: actualConstraint,
		}
		s.nodes[key] = node
		s.nodeIndex.Add(actualName, node)
		graph.Nodes[key] = node
		if s.policy.OnNodeResolved != nil {
			s.policy.OnNodeResolved(key, node, &VersionMetadata{
				Name: actualName, Version: actualConstraint,
			}, nil)
		}
		return node, nil
	}

	c, err := semver.ParseConstraint(actualConstraint)
	if err != nil {
		return nil, fmt.Errorf("parsing constraint %q: %w", actualConstraint, err)
	}

	versions, err := s.registry.FetchVersions(s.ctx, actualName, s.cutoff)
	if err != nil {
		return nil, fmt.Errorf("fetching versions for %s: %w", actualName, err)
	}

	var parsed []*semver.Version
	versionMap := make(map[string]string)
	for _, vi := range versions {
		v, err := semver.Parse(vi.Version)
		if err != nil {
			continue
		}
		parsed = append(parsed, v)
		versionMap[v.String()] = vi.Version
	}

	// Cross-tree dedup for transitive deps: O(1) via NodeIndex.
	if s.policy.CrossTreeDedup && !s.projectDeps[name] {
		if existing := s.nodeIndex.FindSatisfying(actualName, c); existing != nil {
			return existing, nil
		}
	}

	// npm-pick-manifest: prefer latest dist-tag.
	distTags, _ := s.registry.FetchDistTags(s.ctx, actualName)
	best := semver.PickVersion(parsed, c, distTags["latest"])
	if best == nil {
		return nil, fmt.Errorf("no version of %s satisfies %s", actualName, actualConstraint)
	}

	version := versionMap[best.String()]
	key := actualName + "@" + version

	// Exact version dedup.
	if node, ok := s.nodes[key]; ok {
		return node, nil
	}

	// Cycle detection.
	if s.resolving[key] {
		node := &Node{Name: actualName, Version: version}
		s.nodes[key] = node
		s.nodeIndex.Add(actualName, node)
		graph.Nodes[key] = node
		return node, nil
	}
	s.resolving[key] = true
	defer func() { delete(s.resolving, key) }()

	meta, err := s.registry.FetchMetadata(s.ctx, actualName, version)
	if err != nil {
		return nil, fmt.Errorf("fetching metadata for %s@%s: %w", actualName, version, err)
	}

	node := &Node{
		Name:             meta.Name,
		Version:          meta.Version,
		Integrity:        meta.Integrity,
		Shasum:           meta.Shasum,
		TarballURL:       meta.TarballURL,
		HasInstallScript: meta.HasInstallScript,
		Engines:          meta.Engines,
		OS:               meta.OS,
		CPU:              meta.CPU,
		Bin:              meta.Bin,
		License:          meta.License,
		Deprecated:       meta.Deprecated,
		Funding:          meta.Funding,
	}

	if s.policy.StorePeerMetaOnNode {
		node.PeerDeps = meta.PeerDeps
		node.PeerDepsMeta = meta.PeerDepsMeta
	}

	if depType == DepDev {
		node.DevOnly = true
	}
	if depType == DepOptional {
		node.Optional = true
	}

	s.nodes[key] = node
	s.nodeIndex.Add(actualName, node)
	graph.Nodes[key] = node

	// Resolve transitive regular deps.
	depNames := maputil.SortedKeys(meta.Dependencies)
	for _, depName := range depNames {
		depConstraint := meta.Dependencies[depName]
		child, err := s.resolveDep(graph, depName, depConstraint, DepRegular)
		if err != nil {
			return nil, fmt.Errorf("resolving transitive dep %s@%s (from %s): %w", depName, depConstraint, key, err)
		}
		node.Dependencies = append(node.Dependencies, &Edge{
			Name: depName, Constraint: depConstraint, Target: child, Type: DepRegular,
		})
	}

	// Resolve optional deps (failures not fatal).
	optNames := maputil.SortedKeys(meta.OptionalDeps)
	for _, depName := range optNames {
		if _, already := meta.Dependencies[depName]; already {
			continue
		}
		depConstraint := meta.OptionalDeps[depName]
		child, err := s.resolveDep(graph, depName, depConstraint, DepOptional)
		if err != nil {
			continue
		}
		node.Dependencies = append(node.Dependencies, &Edge{
			Name: depName, Constraint: depConstraint, Target: child, Type: DepOptional,
		})
	}

	// Auto-install peer deps if enabled.
	if s.policy.AutoInstallPeers {
		// Build edge name set once for O(1) lookups.
		resolvedEdgeNames := make(map[string]bool, len(node.Dependencies))
		for _, edge := range node.Dependencies {
			resolvedEdgeNames[edge.Name] = true
		}

		peerNames := maputil.SortedKeys(meta.PeerDeps)
		for _, depName := range peerNames {
			// Skip if already resolved as regular or optional dep.
			if resolvedEdgeNames[depName] {
				continue
			}
			// Skip optional peers.
			if pm, ok := meta.PeerDepsMeta[depName]; ok && pm.Optional {
				continue
			}
			// Skip if provided by the project.
			if s.projectDeps[depName] {
				continue
			}
			// Skip if already resolved elsewhere (O(1) via NodeIndex).
			if s.nodeIndex.HasName(depName) {
				continue
			}
			depConstraint := meta.PeerDeps[depName]
			child, err := s.resolveDep(graph, depName, depConstraint, DepPeer)
			if err != nil {
				continue
			}
			node.Dependencies = append(node.Dependencies, &Edge{
				Name: depName, Constraint: depConstraint, Target: child, Type: DepPeer,
			})
		}
	}

	// Notify PM-specific callback.
	if s.policy.OnNodeResolved != nil {
		s.policy.OnNodeResolved(key, node, meta, node.Dependencies)
	}

	return node, nil
}

// isNonRegistrySpecifier returns true if the constraint is a non-registry
// dependency type that cannot be resolved from the npm registry.
func isNonRegistrySpecifier(constraint string) bool {
	nonRegistryPrefixes := []string{
		"file:", "link:", "portal:",       // local filesystem
		"git+", "git://", "git@",         // git URLs
		"github:", "bitbucket:", "gitlab:", // shorthand git hosts
		"workspace:",                      // pnpm workspace protocol
		"patch:", "exec:",                 // pnpm extensions
		"http://", "https://",            // tarball URLs
	}
	for _, p := range nonRegistryPrefixes {
		if strings.HasPrefix(constraint, p) {
			return true
		}
	}
	return false
}
