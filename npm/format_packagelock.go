package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
	"github.com/jumoel/locksmith/internal/orderedjson"
)

// PackageLockV3Formatter produces package-lock.json lockfileVersion 3 output.
// The same format is used for npm-shrinkwrap.json - the only difference is the
// filename, which is the caller's concern.
type PackageLockV3Formatter struct{}

// NewPackageLockV3Formatter returns a new v3 formatter.
func NewPackageLockV3Formatter() *PackageLockV3Formatter {
	return &PackageLockV3Formatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since npm lockfiles require placement info
// that only ResolveResult provides.
func (f *PackageLockV3Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for npm lockfile generation; the ecosystem.Formatter interface cannot provide placement info")
}

// FormatFromResult produces package-lock.json v3 bytes from a resolve result.
// Output is deterministic: all map keys are sorted alphabetically.
func (f *PackageLockV3Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	// Build the packages map entries.
	packages := make(map[string]orderedjson.Map, len(result.PlacedNodes)+1)

	// Root entry.
	packages[""] = buildRootEntry(project)

	// All placed packages.
	for path, placed := range result.PlacedNodes {
		packages[path] = buildPackageEntry(placed.Node, placedDepName(path))
		// Workspace members need a directory entry with their full spec.
		if placed.Node.WorkspacePath != "" {
			wsEntry := orderedjson.Map{
				{Key: "name", Value: placed.Node.Name},
				{Key: "version", Value: placed.Node.Version},
			}
			// Include the workspace member's dependencies. Resolve workspace:
			// constraints to the target member's actual version.
			if project != nil {
				wsVersions := make(map[string]string)
				for _, ws := range project.Workspaces {
					if ws.Spec != nil {
						wsVersions[ws.Spec.Name] = ws.Spec.Version
					}
				}
				for _, ws := range project.Workspaces {
					if ws.Spec != nil && ws.Spec.Name == placed.Node.Name {
						g := ecosystem.GroupDependenciesByType(ws.Spec.Dependencies)
						resolveWsDeps := func(deps map[string]string) map[string]string {
							out := make(map[string]string, len(deps))
							for name, c := range deps {
								if strings.HasPrefix(c, "workspace:") {
									if v, ok := wsVersions[name]; ok {
										out[name] = v
									}
								} else {
									out[name] = c
								}
							}
							return out
						}
						if len(g.Regular) > 0 {
							wsEntry = append(wsEntry, orderedjson.Entry{Key: "dependencies", Value: orderedjson.FromStringMap(resolveWsDeps(g.Regular))})
						}
						if len(g.Dev) > 0 {
							wsEntry = append(wsEntry, orderedjson.Entry{Key: "devDependencies", Value: orderedjson.FromStringMap(resolveWsDeps(g.Dev))})
						}
						break
					}
				}
			}
			packages[placed.Node.WorkspacePath] = wsEntry
		} else if strings.HasPrefix(placed.Node.TarballURL, "file:") {
			// file: deps also need a top-level directory entry with their version.
			dirName := strings.TrimPrefix(placed.Node.TarballURL, "file:")
			dirName = strings.TrimPrefix(dirName, "./")
			packages[dirName] = orderedjson.Map{
				{Key: "version", Value: placed.Node.Version},
			}
		}
	}

	// Top-level lockfile structure with deterministic key order.
	lockfile := orderedjson.Map{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
		{Key: "lockfileVersion", Value: 3},
		{Key: "requires", Value: true},
		{Key: "packages", Value: orderedjson.FromStringMapSorted(packages)},
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(lockfile); err != nil {
		return nil, fmt.Errorf("encoding lockfile: %w", err)
	}

	return buf.Bytes(), nil
}

// buildRootEntry constructs the root package entry (key "") from the project spec.
func buildRootEntry(project *ecosystem.ProjectSpec) orderedjson.Map {
	entry := orderedjson.Map{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
	}

	// Add workspaces field if this is a workspace project.
	if len(project.Workspaces) > 0 {
		wsPaths := make([]string, 0, len(project.Workspaces))
		for _, ws := range project.Workspaces {
			wsPaths = append(wsPaths, ws.RelPath)
		}
		entry = append(entry, orderedjson.Entry{Key: "workspaces", Value: wsPaths})
	}

	// Group declared dependencies by type.
	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	if len(g.Regular) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: orderedjson.FromStringMap(g.Regular)})
	}
	if len(g.Dev) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "devDependencies", Value: orderedjson.FromStringMap(g.Dev)})
	}
	if len(g.Optional) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "optionalDependencies", Value: orderedjson.FromStringMap(g.Optional)})
	}
	if len(g.Peer) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "peerDependencies", Value: orderedjson.FromStringMap(g.Peer)})
	}

	return entry
}

// placedDepName extracts the dependency name from a node_modules path.
// "node_modules/foo" -> "foo", "node_modules/a/node_modules/b" -> "b",
// "node_modules/@scope/pkg" -> "@scope/pkg".
func placedDepName(path string) string {
	const prefix = "node_modules/"
	idx := strings.LastIndex(path, prefix)
	if idx < 0 {
		return path
	}
	return path[idx+len(prefix):]
}

// buildPackageEntry constructs a non-root package entry from a resolved node.
// placedName is the dependency name from the node_modules path - when it
// differs from node.Name, this is an npm alias and we include a "name" field.
// Field order matches npm's Arborist output:
// version, resolved, integrity, dev, optional, hasInstallScript, license,
// dependencies, optionalDependencies, peerDependencies, peerDependenciesMeta,
// bin, engines, os, cpu, funding, deprecated.
func buildPackageEntry(node *ecosystem.Node, placedName string) orderedjson.Map {
	// Workspace members are symlinks - emit link format.
	if node.WorkspacePath != "" {
		return orderedjson.Map{
			{Key: "resolved", Value: node.WorkspacePath},
			{Key: "link", Value: true},
		}
	}

	// file: dependencies are symlinks - emit link format instead of regular entry.
	if strings.HasPrefix(node.TarballURL, "file:") {
		dirName := strings.TrimPrefix(node.TarballURL, "file:")
		dirName = strings.TrimPrefix(dirName, "./")
		return orderedjson.Map{
			{Key: "resolved", Value: dirName},
			{Key: "link", Value: true},
		}
	}

	entry := orderedjson.Map{}

	// npm aliases: when the placed name differs from the real package name,
	// include a "name" field so npm can identify the real package.
	if placedName != "" && placedName != node.Name {
		entry = append(entry, orderedjson.Entry{Key: "name", Value: node.Name})
	}

	entry = append(entry,
		orderedjson.Entry{Key: "version", Value: node.Version},
		orderedjson.Entry{Key: "resolved", Value: node.TarballURL},
		orderedjson.Entry{Key: "integrity", Value: node.Integrity},
	)

	if node.DevOnly {
		entry = append(entry, orderedjson.Entry{Key: "dev", Value: true})
	}
	if node.Optional {
		entry = append(entry, orderedjson.Entry{Key: "optional", Value: true})
	}
	if node.HasInstallScript {
		entry = append(entry, orderedjson.Entry{Key: "hasInstallScript", Value: true})
	}
	if node.Deprecated != "" {
		entry = append(entry, orderedjson.Entry{Key: "deprecated", Value: node.Deprecated})
	}
	if node.Funding != nil {
		entry = append(entry, orderedjson.Entry{Key: "funding", Value: normalizeFunding(node.Funding)})
	}
	if node.License != "" {
		entry = append(entry, orderedjson.Entry{Key: "license", Value: node.License})
	}

	// Collect dependency constraints grouped by type.
	if len(node.Dependencies) > 0 {
		regularDeps := make(map[string]string)
		optionalDeps := make(map[string]string)

		for _, edge := range node.Dependencies {
			switch edge.Type {
			case ecosystem.DepRegular:
				regularDeps[edge.Name] = edge.Constraint
			case ecosystem.DepOptional:
				optionalDeps[edge.Name] = edge.Constraint
			}
		}

		if len(regularDeps) > 0 {
			entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: orderedjson.FromStringMap(regularDeps)})
		}
		if len(optionalDeps) > 0 {
			entry = append(entry, orderedjson.Entry{Key: "optionalDependencies", Value: orderedjson.FromStringMap(optionalDeps)})
		}
	}

	if len(node.PeerDeps) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "peerDependencies", Value: orderedjson.FromStringMap(node.PeerDeps)})
	}
	if len(node.PeerDepsMeta) > 0 {
		peerMeta := make(orderedjson.Map, 0, len(node.PeerDepsMeta))
		peerNames := maputil.SortedMapKeys(node.PeerDepsMeta)
		for _, name := range peerNames {
			pm := node.PeerDepsMeta[name]
			metaObj := orderedjson.Map{}
			if pm.Optional {
				metaObj = append(metaObj, orderedjson.Entry{Key: "optional", Value: true})
			}
			peerMeta = append(peerMeta, orderedjson.Entry{Key: name, Value: metaObj})
		}
		entry = append(entry, orderedjson.Entry{Key: "peerDependenciesMeta", Value: peerMeta})
	}

	if len(node.Bin) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "bin", Value: orderedjson.FromStringMap(node.Bin)})
	}
	if len(node.Engines) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "engines", Value: orderedjson.FromStringMap(node.Engines)})
	}
	if len(node.OS) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "os", Value: node.OS})
	}
	if len(node.CPU) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "cpu", Value: node.CPU})
	}

	return entry
}

// normalizeFunding converts funding to npm's canonical format.
// npm normalizes string URLs to {"url": "..."} objects.
func normalizeFunding(funding interface{}) interface{} {
	switch v := funding.(type) {
	case string:
		if v != "" {
			return orderedjson.Map{{Key: "url", Value: v}}
		}
		return nil
	case map[string]interface{}:
		// Already an object, pass through
		return v
	case []interface{}:
		// Array of funding objects, pass through
		return v
	default:
		return funding
	}
}

