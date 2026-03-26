package bun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

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

// buildPackagesFromGraph walks the dependency graph to build bun.lock's
// packages section using hierarchical path keys.
//
// Bun uses bare package names as keys when only one version exists.
// When multiple versions of a package exist, the root-level version gets
// the bare name key, and nested versions get path keys like "parent/name"
// reflecting the dependency chain that leads to them.
func buildPackagesFromGraph(result *ResolveResult) orderedjson.Map {
	// First pass: find which names have multiple versions.
	byName := make(map[string]map[string]*ResolvedPackage) // name -> version -> pkg
	for _, pkg := range result.Packages {
		name := pkg.Node.Name
		if byName[name] == nil {
			byName[name] = make(map[string]*ResolvedPackage)
		}
		byName[name][pkg.Node.Version] = pkg
	}

	// For single-version packages, key is the bare name.
	// For multi-version packages, we need to walk the graph to find the path.
	type keyedPkg struct {
		key string
		pkg *ResolvedPackage
	}
	var entries []keyedPkg

	// Track which multi-version packages have been placed (by name@version).
	placed := make(map[string]bool)

	// Place single-version packages first with bare name keys.
	for name, versions := range byName {
		if len(versions) == 1 {
			for _, pkg := range versions {
				entries = append(entries, keyedPkg{key: name, pkg: pkg})
				placed[name+"@"+pkg.Node.Version] = true
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
				entries = append(entries, keyedPkg{key: name, pkg: pkg})
				placed[key] = true
				barePlaced[name] = true
				usedKeys[name] = true
			}
		}

		// BFS to place nested versions with path keys.
		// Track full path from root dep so we can build unique keys.
		type walkItem struct {
			path string // full path from root dep, e.g. "express/send"
			node *ecosystem.Node
		}
		var queue []walkItem
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				queue = append(queue, walkItem{path: edge.Target.Name, node: edge.Target})
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
				childPath := item.path + "/" + edge.Target.Name

				if !placed[childKey] {
					if pkg, ok := result.Packages[childKey]; ok {
						// First version of a multi-version package gets bare name.
						useBare := len(byName[edge.Target.Name]) > 1 && !barePlaced[edge.Target.Name]
						if useBare {
							entries = append(entries, keyedPkg{key: edge.Target.Name, pkg: pkg})
							barePlaced[edge.Target.Name] = true
							usedKeys[edge.Target.Name] = true
						} else {
							// Try immediate parent/child first; if that
							// would collide with an existing key, use the
							// full path from the root dep.
							pathKey := item.node.Name + "/" + edge.Target.Name
							if usedKeys[pathKey] {
								pathKey = childPath
							}
							entries = append(entries, keyedPkg{key: pathKey, pkg: pkg})
							usedKeys[pathKey] = true
						}
						placed[childKey] = true
					}
				}

				queue = append(queue, walkItem{path: childPath, node: edge.Target})
			}
		}
	}

	// Sort entries by key.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].key < entries[j].key
	})

	packages := make(orderedjson.Map, 0, len(entries))
	for _, e := range entries {
		packages = append(packages, orderedjson.Entry{Key: e.key, Value: buildPackageEntry(e.pkg)})
	}

	return packages
}

// buildPackageEntry constructs the array for a single package:
// [resolved-spec, "", metadata-object, integrity]
func buildPackageEntry(pkg *ResolvedPackage) []interface{} {
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

	if len(pkg.PeerDeps) > 0 {
		metadata = append(metadata, orderedjson.Entry{
			Key: "peerDependencies", Value: orderedjson.FromStringMap(pkg.PeerDeps),
		})
	}

	// optionalPeers lists peer deps that are optional (from peerDependenciesMeta).
	// Bun requires this to know which peer deps can be skipped.
	if len(pkg.PeerDepsMeta) > 0 {
		var optionalPeers []string
		for name, meta := range pkg.PeerDepsMeta {
			if meta.Optional {
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

	return []interface{}{
		resolvedSpec,
		"",
		metadata,
		node.Integrity,
	}
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
