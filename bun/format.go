package bun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/orderedjson"
)

// BunLockFormatter produces bun.lock output in JSONC format.
// The output is valid JSON (which is a subset of JSONC).
type BunLockFormatter struct{}

func NewBunLockFormatter() *BunLockFormatter {
	return &BunLockFormatter{}
}

func (f *BunLockFormatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for bun lockfile generation")
}

// FormatFromResult produces bun.lock bytes from a resolve result.
func (f *BunLockFormatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	workspaceDeps := buildWorkspaceDeps(project)
	workspaces := orderedjson.Map{
		{Key: "", Value: workspaceDeps},
	}

	packages := buildPackagesFromGraph(result)

	lockfile := orderedjson.Map{
		{Key: "lockfileVersion", Value: 1},
		{Key: "configVersion", Value: 1},
		{Key: "workspaces", Value: workspaces},
		{Key: "packages", Value: packages},
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(lockfile); err != nil {
		return nil, fmt.Errorf("encoding bun lockfile: %w", err)
	}

	// Convert strict JSON to JSONC by adding trailing commas.
	// bun.lock is JSONC format and bun's parser expects trailing commas.
	return addTrailingCommas(buf.Bytes()), nil
}

func buildWorkspaceDeps(project *ecosystem.ProjectSpec) orderedjson.Map {
	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	entry := orderedjson.Map{
		{Key: "name", Value: project.Name},
	}

	// Workspace deps always use original constraints from package.json.
	if len(g.Regular) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: orderedjson.FromStringMap(g.Regular)})
	}
	if len(g.Dev) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "devDependencies", Value: orderedjson.FromStringMap(g.Dev)})
	}
	if len(g.Optional) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "optionalDependencies", Value: orderedjson.FromStringMap(g.Optional)})
	}

	return entry
}

// nonRegInfo holds the original constraint and declared name for a
// non-registry dependency so the formatter can produce 2-element entries.
type nonRegInfo struct {
	DeclaredName       string
	OriginalConstraint string
}

// isNonRegistryConstraint checks whether a dependency constraint refers to a
// non-registry source (file:, git+, github:, http(s):// etc.).
func isNonRegistryConstraint(constraint string) bool {
	prefixes := []string{
		"file:", "link:", "portal:",
		"git+", "git://", "git@",
		"github:", "bitbucket:", "gitlab:",
		"http://", "https://",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(constraint, p) {
			return true
		}
	}
	return false
}

// buildNonRegistryPackageEntry constructs the 2-element array for a
// non-registry package entry in bun.lock. Non-registry deps use a simpler
// format than registry deps: [specifier, resolvedURL].
//
// Returns nil if the dep doesn't match any non-registry pattern (fallback
// to standard 4-element format).
func buildNonRegistryPackageEntry(node *ecosystem.Node, declaredName, originalConstraint string) []interface{} {
	switch {
	case strings.HasPrefix(originalConstraint, "file:") || strings.HasPrefix(node.TarballURL, "file:"):
		// File dep: [name@file:path, ""]
		path := originalConstraint
		if !strings.HasPrefix(path, "file:") {
			path = node.TarballURL
		}
		return []interface{}{
			declaredName + "@" + path,
			"",
		}

	case strings.HasPrefix(node.TarballURL, "git+"):
		// Git dep (resolved URL starts with git+): [name@constraint, resolvedName@url]
		return []interface{}{
			declaredName + "@" + originalConstraint,
			node.Name + "@" + node.TarballURL,
		}

	case strings.HasPrefix(originalConstraint, "https://") || strings.HasPrefix(originalConstraint, "http://"):
		// Tarball URL dep: [name@url, name@url]
		return []interface{}{
			declaredName + "@" + originalConstraint,
			declaredName + "@" + originalConstraint,
		}

	case strings.HasPrefix(originalConstraint, "github:") ||
		strings.HasPrefix(originalConstraint, "bitbucket:") ||
		strings.HasPrefix(originalConstraint, "gitlab:"):
		// Git shorthand that wasn't resolved to a git+ssh URL (placeholder).
		return []interface{}{
			declaredName + "@" + originalConstraint,
			"",
		}

	default:
		return nil
	}
}

// buildPackagesFromGraph walks the dependency graph to build bun.lock's
// packages section using hierarchical path keys.
//
// Bun uses bare package names as keys when only one version exists.
// When multiple versions of a package exist, the root-level version gets
// the bare name key, and nested versions get path keys like "parent/name"
// reflecting the dependency chain that leads to them.
func buildPackagesFromGraph(result *ResolveResult) orderedjson.Map {
	// Build alias map: when a root dependency's declared name differs from
	// the resolved package name (npm aliases, git deps), bun expects the
	// declared name as the package key.
	aliasKey := make(map[string]string) // "resolvedName@version" -> declaredName
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil && edge.Name != edge.Target.Name {
				aliasKey[edge.Target.Name+"@"+edge.Target.Version] = edge.Name
			}
		}
	}

	// Build a map of non-registry dep info from root edges. This maps the
	// node pointer to the original constraint so the formatter can detect
	// non-registry deps and produce 2-element entries.
	nonRegDeps := make(map[*ecosystem.Node]*nonRegInfo)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil && isNonRegistryConstraint(edge.Constraint) {
				nonRegDeps[edge.Target] = &nonRegInfo{
					DeclaredName:       edge.Name,
					OriginalConstraint: edge.Constraint,
				}
			}
		}
	}

	// First pass: find which names have multiple versions.
	byName := make(map[string]map[string]*ResolvedPackage) // name -> version -> pkg
	for _, pkg := range result.Packages {
		name := pkg.Node.Name
		if byName[name] == nil {
			byName[name] = make(map[string]*ResolvedPackage)
		}
		byName[name][pkg.Node.Version] = pkg
	}

	// Pre-compute packages under a bundling parent. When a dependency
	// declares bundleDependencies, bun nests ALL its transitive deps under
	// the parent's key (e.g., npm/@isaacs/cliui instead of bare @isaacs/cliui).
	// These packages must NOT get bare keys.
	bundledPkg := make(map[string]bool) // "name@version" -> true
	if result.Graph != nil && result.Graph.Root != nil {
		var markBundled func(node *ecosystem.Node)
		markBundled = func(node *ecosystem.Node) {
			for _, edge := range node.Dependencies {
				if edge.Target == nil {
					continue
				}
				key := edge.Target.Name + "@" + edge.Target.Version
				if bundledPkg[key] {
					continue
				}
				bundledPkg[key] = true
				markBundled(edge.Target)
			}
		}
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil && len(edge.Target.BundleDeps) > 0 {
				markBundled(edge.Target)
			}
		}
	}

	// directlyBundled marks packages that are directly listed in a parent's
	// bundleDependencies. These get "bundled": true in the lockfile metadata.
	directlyBundled := make(map[string]bool)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil || len(edge.Target.BundleDeps) == 0 {
				continue
			}
			for _, childEdge := range edge.Target.Dependencies {
				if childEdge.Target != nil && edge.Target.BundleDeps[childEdge.Target.Name] {
					directlyBundled[childEdge.Target.Name+"@"+childEdge.Target.Version] = true
				}
			}
		}
	}

	// For single-version packages, key is the bare name.
	// For multi-version packages, we need to walk the graph to find the path.
	type keyedPkg struct {
		key     string
		pkg     *ResolvedPackage
		bundled bool // directly listed in parent's bundleDependencies
	}
	var entries []keyedPkg

	// Track which multi-version packages have been placed (by name@version).
	placed := make(map[string]bool)

	// Place single-version packages first with bare name keys.
	// Skip packages under bundling parents - they need path keys.
	for name, versions := range byName {
		if len(versions) == 1 {
			for _, pkg := range versions {
				pkgKey := name + "@" + pkg.Node.Version
				if bundledPkg[pkgKey] {
					continue // will be placed with path key during BFS
				}
				displayName := name
				if alias, ok := aliasKey[pkgKey]; ok {
					displayName = alias
				}
				entries = append(entries, keyedPkg{key: displayName, pkg: pkg})
				placed[pkgKey] = true
			}
		}
	}

	// For multi-version packages, walk from root to find dependency paths.
	// The first version encountered (BFS order) gets the bare name key.
	// Other versions get path-based keys like "parent/child" or
	// "grandparent/parent/child" to avoid collisions.
	barePlaced := make(map[string]bool) // tracks which names have bare key
	usedKeys := make(map[string]bool)   // tracks all used path keys to detect collisions
	if result.Graph != nil && result.Graph.Root != nil {
		// First, place root-level versions with bare name keys.
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil {
				continue
			}
			name := edge.Target.Name
			if len(byName[name]) <= 1 {
				continue // already placed as single-version
			}
			key := name + "@" + edge.Target.Version
			if placed[key] {
				continue
			}
			if pkg, ok := result.Packages[key]; ok {
				displayName := name
				if alias, ok := aliasKey[key]; ok {
					displayName = alias
				}
				entries = append(entries, keyedPkg{key: displayName, pkg: pkg})
				placed[key] = true
				barePlaced[name] = true
				usedKeys[displayName] = true
			}
		}

		// BFS to place nested versions with path keys.
		// Use declared dependency names (edge.Name) for path construction,
		// not resolved names (edge.Target.Name), because bun resolves deps
		// by the name used in the parent's dependency listing.
		type walkItem struct {
			path       string // full path using declared names, e.g. "express/send"
			name       string // declared name of this node (may be alias)
			node       *ecosystem.Node
			bundleRoot string // non-empty when inside a bundled subtree (e.g. "npm")
		}
		var queue []walkItem
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				br := ""
				if len(edge.Target.BundleDeps) > 0 {
					br = edge.Name
				}
				queue = append(queue, walkItem{
					path: edge.Name, name: edge.Name, node: edge.Target,
					bundleRoot: br,
				})
			}
		}
		seen := make(map[string]bool)
		for len(queue) > 0 {
			item := queue[0]
			queue = queue[1:]

			nodeKey := item.node.Name + "@" + item.node.Version
			if seen[nodeKey] {
				continue
			}
			seen[nodeKey] = true

			for _, edge := range item.node.Dependencies {
				if edge.Target == nil {
					continue
				}
				childKey := edge.Target.Name + "@" + edge.Target.Version
				childPath := item.path + "/" + edge.Name

				if !placed[childKey] {
					if pkg, ok := result.Packages[childKey]; ok {
						if item.bundleRoot != "" {
							// Inside a bundled subtree: same hoisting logic
							// as non-bundled, but all keys prefixed with the
							// bundle root (e.g., npm/packageName).
							prefix := item.bundleRoot + "/"
							isAlias := edge.Name != edge.Target.Name
							isMulti := len(byName[edge.Target.Name]) > 1
							if isAlias || (!isMulti) || (isMulti && !barePlaced[edge.Target.Name]) {
								// Aliases, single-version, and first multi-version
								// all get hoisted (short) keys.
								key := prefix + edge.Name
								entries = append(entries, keyedPkg{
									key: key, pkg: pkg,
									bundled: directlyBundled[childKey],
								})
								if isMulti && !isAlias {
									barePlaced[edge.Target.Name] = true
								}
								usedKeys[key] = true
							} else {
								// Subsequent multi-version: use full path.
								entries = append(entries, keyedPkg{
									key: childPath, pkg: pkg,
									bundled: directlyBundled[childKey],
								})
								usedKeys[childPath] = true
							}
						} else {
							// First version of a multi-version package gets bare name.
							useBare := len(byName[edge.Target.Name]) > 1 && !barePlaced[edge.Target.Name]
							if useBare {
								displayName := edge.Name
								entries = append(entries, keyedPkg{key: displayName, pkg: pkg})
								barePlaced[edge.Target.Name] = true
								usedKeys[displayName] = true
							} else {
								// Try immediate parent/child first; if that
								// would collide with an existing key, use the
								// full path from the root dep.
								pathKey := item.name + "/" + edge.Name
								if usedKeys[pathKey] {
									pathKey = childPath
								}
								entries = append(entries, keyedPkg{key: pathKey, pkg: pkg})
								usedKeys[pathKey] = true
							}
						}
						placed[childKey] = true
					}
				} else if edge.Name != edge.Target.Name {
					// Alias dep (npm: syntax) where the resolved package is
					// already placed under its real name. Bun creates separate
					// package entries for alias names so they can be looked up
					// by the declared dependency name.
					var aliasPathKey string
					if item.bundleRoot != "" {
						// In bundled subtrees, alias entries are hoisted
						// to the bundle root level (e.g., npm/string-width-cjs).
						aliasPathKey = item.bundleRoot + "/" + edge.Name
					} else {
						aliasPathKey = item.name + "/" + edge.Name
					}
					if !usedKeys[aliasPathKey] {
						if pkg, ok := result.Packages[childKey]; ok {
							entries = append(entries, keyedPkg{key: aliasPathKey, pkg: pkg})
							usedKeys[aliasPathKey] = true
						}
					}
				}

				queue = append(queue, walkItem{
					path: childPath, name: edge.Name, node: edge.Target,
					bundleRoot: item.bundleRoot,
				})
			}
		}
	}

	// Post-pass: add duplicate entries for multi-version packages in
	// bundled subtrees. Bun nests bundled deps and needs a separate
	// entry for each parent that references a non-hoisted multi-version
	// package. Hoisted versions (at prefix/name) are already findable
	// and don't need duplicate entries.
	if result.Graph != nil && result.Graph.Root != nil {
		for _, rootEdge := range result.Graph.Root.Dependencies {
			if rootEdge.Target == nil || len(rootEdge.Target.BundleDeps) == 0 {
				continue
			}
			prefix := rootEdge.Name + "/"

			// Identify which versions are at the hoisted level.
			hoisted := make(map[string]bool) // "name@version"
			for _, e := range entries {
				if !strings.HasPrefix(e.key, prefix) {
					continue
				}
				rel := e.key[len(prefix):]
				// Hoisted = no slash (or one slash for scoped @scope/pkg).
				slashes := strings.Count(rel, "/")
				isScoped := strings.HasPrefix(rel, "@")
				if slashes == 0 || (isScoped && slashes == 1) {
					nk := e.pkg.Node.Name + "@" + e.pkg.Node.Version
					hoisted[nk] = true
				}
			}

			// Walk bundled subtree. For non-hoisted multi-version
			// children, create entries at each parent path. Run
			// multiple passes so entries created in one pass can
			// serve as parents for the next.
			// Collect bundled nodes once.
			var bundledNodes []*ecosystem.Node
			{
				vis := make(map[string]bool)
				q := []*ecosystem.Node{rootEdge.Target}
				for len(q) > 0 {
					n := q[0]
					q = q[1:]
					nk := n.Name + "@" + n.Version
					if vis[nk] {
						continue
					}
					vis[nk] = true
					bundledNodes = append(bundledNodes, n)
					for _, e := range n.Dependencies {
						if e.Target != nil {
							q = append(q, e.Target)
						}
					}
				}
			}
			for pass := 0; pass < 3; pass++ {
				added := 0
				nodeKeys := make(map[string][]string)
				for _, e := range entries {
					nk := e.pkg.Node.Name + "@" + e.pkg.Node.Version
					nodeKeys[nk] = append(nodeKeys[nk], e.key)
				}
				for _, node := range bundledNodes {
					nk := node.Name + "@" + node.Version
					parentPaths := nodeKeys[nk]
					for _, edge := range node.Dependencies {
						if edge.Target == nil {
							continue
						}
						childKey := edge.Target.Name + "@" + edge.Target.Version
						if len(byName[edge.Target.Name]) > 1 && !hoisted[childKey] {
							for _, pp := range parentPaths {
								ek := pp + "/" + edge.Name
								if !usedKeys[ek] {
									if pkg, ok := result.Packages[childKey]; ok {
										entries = append(entries, keyedPkg{key: ek, pkg: pkg})
										usedKeys[ek] = true
										added++
									}
								}
							}
						}
					}
				}
				if added == 0 {
					break
				}
			}
		}
	}

	// Sort entries by key.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	packages := make(orderedjson.Map, 0, len(entries))
	for _, e := range entries {
		// Check if this is a non-registry dep that needs a 2-element entry.
		if info, ok := nonRegDeps[e.pkg.Node]; ok {
			if entry := buildNonRegistryPackageEntry(e.pkg.Node, info.DeclaredName, info.OriginalConstraint); entry != nil {
				packages = append(packages, orderedjson.Entry{Key: e.key, Value: entry})
				continue
			}
		}
		packages = append(packages, orderedjson.Entry{Key: e.key, Value: buildPackageEntry(e.pkg, e.bundled)})
	}

	return packages
}

// buildPackageEntry constructs the array for a single package:
// [resolved-spec, "", metadata-object, integrity]
func buildPackageEntry(pkg *ResolvedPackage, bundled bool) []interface{} {
	node := pkg.Node
	resolvedSpec := fmt.Sprintf("%s@%s", node.Name, node.Version)

	// metadata object - bun includes dependencies, optionalDependencies,
	// peerDependencies, and bin when present.
	metadata := orderedjson.Map{}

	if len(pkg.Dependencies) > 0 {
		depNames := make([]string, 0, len(pkg.Dependencies))
		for name := range pkg.Dependencies {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)

		depsMap := make(orderedjson.Map, len(depNames))
		for i, name := range depNames {
			dep := pkg.Dependencies[name]
			depsMap[i] = orderedjson.Entry{Key: name, Value: dep.Constraint}
		}
		metadata = append(metadata, orderedjson.Entry{Key: "dependencies", Value: depsMap})
	}

	if len(pkg.OptionalDeps) > 0 {
		metadata = append(metadata, orderedjson.Entry{
			Key: "optionalDependencies", Value: orderedjson.FromStringMap(pkg.OptionalDeps),
		})
	}

	if bundled {
		metadata = append(metadata, orderedjson.Entry{Key: "bundled", Value: true})
	}

	if len(pkg.PeerDeps) > 0 {
		metadata = append(metadata, orderedjson.Entry{
			Key: "peerDependencies", Value: orderedjson.FromStringMap(pkg.PeerDeps),
		})
	}

	// optionalPeers lists peer deps that are optional (from peerDependenciesMeta).
	// Bun requires this to know which peer deps can be skipped.
	// Only include peers that are also declared in peerDependencies - some
	// packages have peerDependenciesMeta entries without a matching
	// peerDependencies entry (e.g., styled-jsx).
	if len(pkg.PeerDepsMeta) > 0 {
		var optionalPeers []string
		for name, meta := range pkg.PeerDepsMeta {
			if meta.Optional && pkg.PeerDeps[name] != "" {
				optionalPeers = append(optionalPeers, name)
			}
		}
		if len(optionalPeers) > 0 {
			sort.Strings(optionalPeers)
			metadata = append(metadata, orderedjson.Entry{
				Key: "optionalPeers", Value: optionalPeers,
			})
		}
	}

	if len(pkg.Bin) > 0 {
		metadata = append(metadata, orderedjson.Entry{
			Key: "bin", Value: orderedjson.FromStringMap(pkg.Bin),
		})
	}

	// os/cpu metadata tells bun which platform a package targets.
	// Bun uses a single string when there's one value, array otherwise.
	if len(node.OS) > 0 {
		metadata = append(metadata, orderedjson.Entry{Key: "os", Value: singleOrSlice(node.OS)})
	}
	if len(node.CPU) > 0 {
		metadata = append(metadata, orderedjson.Entry{Key: "cpu", Value: singleOrSlice(normalizeBunCPU(node.CPU))})
	}

	return []interface{}{
		resolvedSpec,
		"",
		metadata,
		node.Integrity,
	}
}

// singleOrSlice returns the single string if the slice has one element,
// or the full slice otherwise. Bun uses scalar values for single-element
// os/cpu arrays in bun.lock.
func singleOrSlice(s []string) interface{} {
	if len(s) == 1 {
		return s[0]
	}
	return s
}

// bunKnownCPU is the set of CPU architectures bun recognizes.
// Unknown values are normalized to "none" in bun.lock.
var bunKnownCPU = map[string]bool{
	"x64": true, "arm64": true, "arm": true,
	"ia32": true, "ppc64": true, "s390x": true,
	"none": true,
}

// normalizeBunCPU maps CPU values to what bun expects. Unrecognized
// architectures (e.g., riscv64, wasm32) become "none".
func normalizeBunCPU(cpus []string) []string {
	out := make([]string, len(cpus))
	for i, c := range cpus {
		if bunKnownCPU[c] {
			out[i] = c
		} else {
			out[i] = "none"
		}
	}
	return out
}

// addTrailingCommas converts strict JSON to JSONC by adding trailing commas
// before closing braces and brackets. bun.lock requires JSONC format.
func addTrailingCommas(data []byte) []byte {
	var result []byte
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if ch == '}' || ch == ']' {
			// Look back to find the last non-whitespace character.
			j := len(result) - 1
			for j >= 0 && (result[j] == ' ' || result[j] == '\t' || result[j] == '\n' || result[j] == '\r') {
				j--
			}
			// Add trailing comma if the last value isn't already a comma,
			// and isn't an opening brace/bracket (empty container).
			if j >= 0 && result[j] != ',' && result[j] != '{' && result[j] != '[' {
				// Insert comma after the last non-whitespace.
				tail := make([]byte, len(result)-j-1)
				copy(tail, result[j+1:])
				result = append(result[:j+1], ',')
				result = append(result, tail...)
			}
		}
		result = append(result, ch)
	}
	return result
}
