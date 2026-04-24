package ecosystem

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumoel/locksmith/internal/maputil"
	"github.com/jumoel/locksmith/internal/semver"
)

// VersionSelection controls version picking behavior during resolution.
type VersionSelection int

const (
	// VersionSelectPreferLatest prefers the npm "latest" dist-tag if it
	// satisfies the constraint, falling back to highest satisfying version.
	// This is the default and matches npm, pnpm, bun, and yarn classic (v1).
	VersionSelectPreferLatest VersionSelection = iota

	// VersionSelectHighest always picks the highest satisfying version,
	// ignoring dist-tags. Matches yarn berry (v2+) behavior.
	VersionSelectHighest
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

	// SkipOptionalPeerDeps: skip regular dependencies that are also listed
	// as optional peer dependencies (pnpm behavior). When a dep appears in
	// both `dependencies` and `peerDependencies` with optional=true, pnpm
	// treats the peer declaration as taking precedence and does not install
	// the dep unless the consumer provides it.
	SkipOptionalPeerDeps bool

	// VersionSelection controls how the resolver picks a version when
	// multiple versions satisfy a constraint. The zero value
	// (VersionSelectPreferLatest) is the safe default for most PMs.
	VersionSelection VersionSelection

	// ResolveWorkspaceByName: when true and a workspace index is available,
	// resolve regular semver constraints to workspace members if the dep name
	// matches a member. This is npm and yarn classic behavior where cross-workspace
	// deps use regular version ranges, not the workspace: protocol.
	ResolveWorkspaceByName bool

	// OnNodeResolved is called after each node is fully resolved including
	// all transitive deps. PM-specific resolvers use this to collect their
	// own bookkeeping data.
	OnNodeResolved func(key string, node *Node, meta *VersionMetadata, edges []*Edge)
}

// ApplyOverride copies fields from override into the policy.
// OnNodeResolved is never overridden - PM-specific resolvers always set it.
func (p *ResolverPolicy) ApplyOverride(override *ResolverPolicy) {
	if override == nil {
		return
	}
	p.CrossTreeDedup = override.CrossTreeDedup
	p.AutoInstallPeers = override.AutoInstallPeers
	p.StorePeerMetaOnNode = override.StorePeerMetaOnNode
	p.SkipOptionalPeerDeps = override.SkipOptionalPeerDeps
	p.VersionSelection = override.VersionSelection
	p.ResolveWorkspaceByName = override.ResolveWorkspaceByName
}

// resolverState holds state during resolution.
type resolverState struct {
	registry          Registry
	cutoff            *time.Time
	ctx               context.Context
	nodes             map[string]*Node // "name@version" -> node
	nodeIndex         *NodeIndex       // O(1) name lookups
	resolving         map[string]bool  // cycle detection
	projectDeps       map[string]bool  // root dep names
	policy            ResolverPolicy
	specDir           string // for resolving file: deps
	workspaceIndex    *WorkspaceIndex
	overrides         *OverrideSet         // version overrides from package.json
	packageExtensions *PackageExtensionSet // pnpm packageExtensions
	ancestry          []string             // current resolution chain for override matching
}

// Resolve executes the shared dependency resolution algorithm.
// PM-specific data is collected via the policy.OnNodeResolved callback.
func Resolve(ctx context.Context, project *ProjectSpec, registry Registry, opts ResolveOptions, policy ResolverPolicy) (*Graph, error) {
	state := &resolverState{
		registry:          registry,
		cutoff:            opts.CutoffDate,
		ctx:               ctx,
		nodes:             make(map[string]*Node),
		nodeIndex:         NewNodeIndex(),
		resolving:         make(map[string]bool),
		projectDeps:       make(map[string]bool),
		policy:            policy,
		specDir:           opts.SpecDir,
		workspaceIndex:    opts.WorkspaceIndex,
		overrides:         project.Overrides,
		packageExtensions: project.PackageExtensions,
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

	// Resolve workspace member dependencies.
	if len(project.Workspaces) > 0 {
		for _, member := range project.Workspaces {
			if member.Spec == nil {
				continue
			}
			memberNode := &Node{Name: member.Spec.Name, Version: member.Spec.Version, WorkspacePath: member.RelPath}
			for _, dep := range member.Spec.Dependencies {
				node, err := state.resolveDep(graph, dep.Name, dep.Constraint, dep.Type)
				if err != nil {
					if dep.Type == DepOptional {
						continue
					}
					return nil, fmt.Errorf("resolving %s@%s (workspace member %s): %w", dep.Name, dep.Constraint, member.RelPath, err)
				}
				memberNode.Dependencies = append(memberNode.Dependencies, &Edge{
					Name: dep.Name, Constraint: dep.Constraint, Target: node, Type: dep.Type,
				})
			}
			graph.Root.Dependencies = append(graph.Root.Dependencies, &Edge{
				Name: member.Spec.Name, Constraint: "workspace:" + member.RelPath,
				Target: memberNode, Type: DepRegular,
			})
		}
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

	// Apply version overrides before any resolution logic.
	if s.overrides != nil {
		if overrideVersion, ok := s.overrides.FindOverride(actualName, s.ancestry); ok {
			actualConstraint = overrideVersion
		}
	}

	// Handle workspace: protocol - resolve to a local workspace member.
	if strings.HasPrefix(actualConstraint, "workspace:") {
		member := s.workspaceIndex.Resolve(actualName)
		if member == nil {
			return nil, fmt.Errorf("workspace member %q not found in workspace index", actualName)
		}
		return s.resolveWorkspaceMember(graph, member)
	}

	// npm/yarn-classic: resolve regular constraints to workspace members by name.
	if s.policy.ResolveWorkspaceByName && s.workspaceIndex != nil {
		if member := s.workspaceIndex.Resolve(actualName); member != nil {
			return s.resolveWorkspaceMember(graph, member)
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

		version := "0.0.0-local"
		resolvedURL := actualConstraint

		if strings.HasPrefix(actualConstraint, "file:") && s.specDir != "" {
			// file: deps - read version from local package.json.
			if v := readLocalPackageVersion(s.specDir, strings.TrimPrefix(actualConstraint, "file:")); v != "" {
				version = v
			}
		} else if strings.HasPrefix(actualConstraint, "https://") && strings.HasSuffix(actualConstraint, ".tgz") {
			// Tarball URL - resolve from registry to get full metadata.
			if pkgName, ver := parseTarballURL(actualConstraint); pkgName != "" && ver != "" {
				meta, err := s.registry.FetchMetadata(s.ctx, pkgName, ver)
				if err == nil {
					// Fully resolved - create a proper node, not a placeholder.
					resolvedKey := meta.Name + "@" + meta.Version
					if existingNode, ok := s.nodes[resolvedKey]; ok {
						return existingNode, nil
					}
					node := &Node{
						Name:       meta.Name,
						Version:    meta.Version,
						Integrity:  meta.Integrity,
						Shasum:     meta.Shasum,
						TarballURL: meta.TarballURL,
						Engines:    meta.Engines,
					}
					s.nodes[key] = node
					s.nodes[resolvedKey] = node
					s.nodeIndex.Add(meta.Name, node)
					graph.Nodes[resolvedKey] = node
					// Resolve transitive deps of the tarball package.
					for depName, depConstraint := range meta.Dependencies {
						child, err := s.resolveDep(graph, depName, depConstraint, DepRegular)
						if err != nil {
							continue
						}
						node.Dependencies = append(node.Dependencies, &Edge{
							Name: depName, Constraint: depConstraint, Target: child, Type: DepRegular,
						})
					}
					if s.policy.OnNodeResolved != nil {
						s.policy.OnNodeResolved(resolvedKey, node, meta, node.Dependencies)
					}
					return node, nil
				}
			}
		} else if owner, repo, ok := parseGitHubURL(actualConstraint); ok {
			// GitHub deps - fetch version, name, commit hash, and deps via HTTPS API.
			if info := resolveGitHubDep(s.ctx, owner, repo); info != nil {
				version = info.Version
				if info.Name != "" {
					actualName = info.Name
				}
				resolvedURL = fmt.Sprintf("git+ssh://git@github.com/%s/%s.git#%s", owner, repo, info.CommitHash)

				// Create node and resolve transitive deps.
				node := &Node{
					Name:       actualName,
					Version:    version,
					TarballURL: resolvedURL,
				}
				s.nodes[key] = node
				s.nodeIndex.Add(actualName, node)
				graph.Nodes[key] = node

				// Resolve transitive deps from the GitHub package.json.
				for depName, depConstraint := range info.Dependencies {
					child, err := s.resolveDep(graph, depName, depConstraint, DepRegular)
					if err != nil {
						continue
					}
					node.Dependencies = append(node.Dependencies, &Edge{
						Name: depName, Constraint: depConstraint, Target: child, Type: DepRegular,
					})
				}
				if s.policy.OnNodeResolved != nil {
					s.policy.OnNodeResolved(key, node, &VersionMetadata{
						Name: actualName, Version: version,
					}, node.Dependencies)
				}
				return node, nil
			}
		}

		node := &Node{
			Name:       actualName,
			Version:    version,
			TarballURL: resolvedURL,
		}
		s.nodes[key] = node
		s.nodeIndex.Add(actualName, node)
		graph.Nodes[key] = node
		if s.policy.OnNodeResolved != nil {
			s.policy.OnNodeResolved(key, node, &VersionMetadata{
				Name: actualName, Version: version,
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

	// Version picking strategy: yarn berry uses highest satisfying version;
	// npm/pnpm/bun/yarn classic prefer the "latest" dist-tag per npm-pick-manifest.
	var best *semver.Version
	switch s.policy.VersionSelection {
	case VersionSelectHighest:
		best = semver.MaxSatisfying(parsed, c)
	default: // VersionSelectPreferLatest
		distTags, _ := s.registry.FetchDistTags(s.ctx, actualName)
		best = semver.PickVersion(parsed, c, distTags["latest"])
	}
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

	// Apply packageExtensions to inject additional deps before building the node.
	s.packageExtensions.Apply(meta)

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

	if len(meta.BundleDeps) > 0 {
		node.BundleDeps = meta.BundleDeps
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

	// Push current package onto ancestry for override matching.
	s.ancestry = append(s.ancestry, actualName)
	defer func() { s.ancestry = s.ancestry[:len(s.ancestry)-1] }()

	// Resolve transitive regular deps.
	depNames := maputil.SortedKeys(meta.Dependencies)
	for _, depName := range depNames {
		// pnpm skips regular deps that are also optional peer deps.
		// When a dep appears in both `dependencies` and `peerDependencies`
		// with optional=true, the peer declaration takes precedence and the
		// dep is only installed if the consumer provides it.
		if s.policy.SkipOptionalPeerDeps {
			if _, isPeer := meta.PeerDeps[depName]; isPeer {
				if pm, hasMeta := meta.PeerDepsMeta[depName]; hasMeta && pm.Optional {
					continue
				}
			}
		}

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

// resolveWorkspaceMember resolves a dependency to a local workspace member.
// Handles cycle detection, node caching, and recursive member dep resolution.
func (s *resolverState) resolveWorkspaceMember(graph *Graph, member *WorkspaceMember) (*Node, error) {
	key := member.Spec.Name + "@" + member.Spec.Version
	// Prevent infinite recursion for circular workspace deps.
	if s.resolving[key] {
		node := &Node{Name: member.Spec.Name, Version: member.Spec.Version, WorkspacePath: member.RelPath}
		return node, nil
	}
	if node, ok := s.nodes[key]; ok {
		return node, nil
	}
	s.resolving[key] = true
	node := &Node{Name: member.Spec.Name, Version: member.Spec.Version, WorkspacePath: member.RelPath}
	s.nodes[key] = node
	// Resolve the workspace member's own dependencies.
	for _, dep := range member.Spec.Dependencies {
		child, err := s.resolveDep(graph, dep.Name, dep.Constraint, dep.Type)
		if err != nil {
			if dep.Type == DepOptional {
				continue
			}
			delete(s.resolving, key)
			return nil, fmt.Errorf("resolving %s@%s (workspace member %s): %w", dep.Name, dep.Constraint, member.RelPath, err)
		}
		node.Dependencies = append(node.Dependencies, &Edge{
			Name: dep.Name, Constraint: dep.Constraint, Target: child, Type: dep.Type,
		})
	}
	delete(s.resolving, key)
	return node, nil
}

// isNonRegistrySpecifier returns true if the constraint is a non-registry
// dependency type that cannot be resolved from the npm registry.
// gitHubDepInfo holds resolved metadata for a GitHub dependency.
type gitHubDepInfo struct {
	Name         string
	Version      string
	CommitHash   string
	Dependencies map[string]string
}

// parseGitHubURL extracts owner/repo from GitHub dependency specifiers.
// Supports: github:owner/repo, git+ssh://git@github.com/owner/repo.git,
// git+https://github.com/owner/repo.git
func parseGitHubURL(constraint string) (owner, repo string, ok bool) {
	// github:owner/repo
	if strings.HasPrefix(constraint, "github:") {
		parts := strings.SplitN(strings.TrimPrefix(constraint, "github:"), "/", 2)
		if len(parts) == 2 {
			return parts[0], strings.TrimSuffix(parts[1], ".git"), true
		}
	}
	// git+ssh://git@github.com/owner/repo.git or git+https://github.com/owner/repo.git
	for _, prefix := range []string{"git+ssh://git@github.com/", "git+https://github.com/", "git@github.com:"} {
		if strings.HasPrefix(constraint, prefix) {
			path := strings.TrimPrefix(constraint, prefix)
			path = strings.TrimSuffix(path, ".git")
			parts := strings.SplitN(path, "/", 2)
			if len(parts) == 2 {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}

// resolveGitHubDep fetches version and commit hash for a public GitHub repo.
// Uses raw.githubusercontent.com for package.json (no auth needed for public repos)
// and the GitHub API for the commit hash.
func resolveGitHubDep(ctx context.Context, owner, repo string) *gitHubDepInfo {
	// Fetch package.json to get version.
	pkgURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/package.json", owner, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", pkgURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return nil
	}
	defer resp.Body.Close()

	var pkg struct {
		Name         string            `json:"name"`
		Version      string            `json:"version"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil
	}

	// Fetch commit hash via API.
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/HEAD", owner, repo)
	req2, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil
	}
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil || resp2.StatusCode != 200 {
		if resp2 != nil {
			resp2.Body.Close()
		}
		// Return with version but no hash.
		return &gitHubDepInfo{Name: pkg.Name, Version: pkg.Version, Dependencies: pkg.Dependencies}
	}
	defer resp2.Body.Close()

	var commit struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&commit); err != nil {
		return &gitHubDepInfo{Name: pkg.Name, Version: pkg.Version, Dependencies: pkg.Dependencies}
	}

	return &gitHubDepInfo{Name: pkg.Name, Version: pkg.Version, CommitHash: commit.SHA, Dependencies: pkg.Dependencies}
}

// parseTarballURL extracts the package name and version from a npm registry
// tarball URL like https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz
func parseTarballURL(url string) (name, version string) {
	// Extract the filename from the URL.
	lastSlash := strings.LastIndex(url, "/")
	if lastSlash == -1 {
		return "", ""
	}
	filename := strings.TrimSuffix(url[lastSlash+1:], ".tgz")
	// filename is like "is-odd-3.0.1"
	// Find the last dash that separates name from version.
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '-' {
			candidate := filename[i+1:]
			if len(candidate) > 0 && candidate[0] >= '0' && candidate[0] <= '9' {
				return filename[:i], candidate
			}
		}
	}
	return "", ""
}

// readLocalPackageVersion reads the version from a local package.json.
// relPath is relative to specDir (e.g., "./local-pkg").
func readLocalPackageVersion(specDir, relPath string) string {
	pkgPath := filepath.Join(specDir, relPath, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return pkg.Version
}

func isNonRegistrySpecifier(constraint string) bool {
	nonRegistryPrefixes := []string{
		"file:", "link:", "portal:",       // local filesystem
		"git+", "git://", "git@",         // git URLs
		"github:", "bitbucket:", "gitlab:", // shorthand git hosts
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
