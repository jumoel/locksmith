package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jumoel/locksmith/ecosystem"
)

// orderedEntry is a single key-value pair in an ordered JSON object.
type orderedEntry struct {
	Key   string
	Value interface{}
}

// orderedMap is a JSON-serializable ordered key-value list that preserves
// insertion order when marshaled, unlike Go's built-in map type.
type orderedMap []orderedEntry

func (om orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, entry := range om {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		val, err := json.Marshal(entry.Value)
		if err != nil {
			return nil, err
		}
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

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
	packages := make(map[string]orderedMap, len(result.PlacedNodes)+1)

	// Root entry.
	packages[""] = buildRootEntry(project)

	// All placed packages.
	for path, placed := range result.PlacedNodes {
		packages[path] = buildPackageEntry(placed.Node)
	}

	// Top-level lockfile structure with deterministic key order.
	lockfile := orderedMap{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
		{Key: "lockfileVersion", Value: 3},
		{Key: "requires", Value: true},
		{Key: "packages", Value: buildOrderedPackages(packages)},
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
func buildRootEntry(project *ecosystem.ProjectSpec) orderedMap {
	entry := orderedMap{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
	}

	// Group declared dependencies by type.
	deps := make(map[string]string)
	devDeps := make(map[string]string)
	optDeps := make(map[string]string)
	peerDeps := make(map[string]string)

	for _, d := range project.Dependencies {
		switch d.Type {
		case ecosystem.DepRegular:
			deps[d.Name] = d.Constraint
		case ecosystem.DepDev:
			devDeps[d.Name] = d.Constraint
		case ecosystem.DepOptional:
			optDeps[d.Name] = d.Constraint
		case ecosystem.DepPeer:
			peerDeps[d.Name] = d.Constraint
		}
	}

	if len(deps) > 0 {
		entry = append(entry, orderedEntry{Key: "dependencies", Value: sortedStringMap(deps)})
	}
	if len(devDeps) > 0 {
		entry = append(entry, orderedEntry{Key: "devDependencies", Value: sortedStringMap(devDeps)})
	}
	if len(optDeps) > 0 {
		entry = append(entry, orderedEntry{Key: "optionalDependencies", Value: sortedStringMap(optDeps)})
	}
	if len(peerDeps) > 0 {
		entry = append(entry, orderedEntry{Key: "peerDependencies", Value: sortedStringMap(peerDeps)})
	}

	return entry
}

// buildPackageEntry constructs a non-root package entry from a resolved node.
func buildPackageEntry(node *ecosystem.Node) orderedMap {
	entry := orderedMap{
		{Key: "version", Value: node.Version},
		{Key: "resolved", Value: node.TarballURL},
		{Key: "integrity", Value: node.Integrity},
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
			entry = append(entry, orderedEntry{Key: "dependencies", Value: sortedStringMap(regularDeps)})
		}
		if len(optionalDeps) > 0 {
			entry = append(entry, orderedEntry{Key: "optionalDependencies", Value: sortedStringMap(optionalDeps)})
		}
	}

	if node.DevOnly {
		entry = append(entry, orderedEntry{Key: "dev", Value: true})
	}
	if node.Optional {
		entry = append(entry, orderedEntry{Key: "optional", Value: true})
	}
	if node.HasInstallScript {
		entry = append(entry, orderedEntry{Key: "hasInstallScript", Value: true})
	}
	if node.License != "" {
		entry = append(entry, orderedEntry{Key: "license", Value: node.License})
	}
	if len(node.Bin) > 0 {
		entry = append(entry, orderedEntry{Key: "bin", Value: sortedStringMap(node.Bin)})
	}
	if len(node.Engines) > 0 {
		entry = append(entry, orderedEntry{Key: "engines", Value: sortedStringMap(node.Engines)})
	}
	if len(node.OS) > 0 {
		entry = append(entry, orderedEntry{Key: "os", Value: node.OS})
	}
	if len(node.CPU) > 0 {
		entry = append(entry, orderedEntry{Key: "cpu", Value: node.CPU})
	}
	if node.Deprecated != "" {
		entry = append(entry, orderedEntry{Key: "deprecated", Value: node.Deprecated})
	}

	return entry
}

// sortedStringMap converts a map[string]string to an orderedMap with
// alphabetically sorted keys.
func sortedStringMap(m map[string]string) orderedMap {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make(orderedMap, len(keys))
	for i, k := range keys {
		result[i] = orderedEntry{Key: k, Value: m[k]}
	}
	return result
}

// buildOrderedPackages converts a packages map to an orderedMap with sorted paths.
// The empty string key (root) sorts first alphabetically, which matches the npm
// lockfile convention.
func buildOrderedPackages(packages map[string]orderedMap) orderedMap {
	keys := make([]string, 0, len(packages))
	for k := range packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make(orderedMap, len(keys))
	for i, k := range keys {
		result[i] = orderedEntry{Key: k, Value: packages[k]}
	}
	return result
}
