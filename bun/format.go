package bun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jumoel/locksmith/ecosystem"
)

// BunLockFormatter produces bun.lock output in JSONC format.
// The output is valid JSON (which is a subset of JSONC).
type BunLockFormatter struct{}

// NewBunLockFormatter returns a new bun.lock formatter.
func NewBunLockFormatter() *BunLockFormatter {
	return &BunLockFormatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since bun lockfiles require resolution
// metadata that only ResolveResult provides.
func (f *BunLockFormatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for bun lockfile generation")
}

// orderedEntry is a single key-value pair in an ordered JSON object.
type orderedEntry struct {
	Key   string
	Value interface{}
}

// orderedMap is a JSON-serializable ordered key-value list that preserves
// insertion order when marshaled.
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
		var valBuf bytes.Buffer
		enc := json.NewEncoder(&valBuf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(entry.Value); err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimRight(valBuf.Bytes(), "\n"))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// FormatFromResult produces bun.lock bytes from a resolve result.
// Output is deterministic: all map keys are sorted alphabetically.
func (f *BunLockFormatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	// Build workspace dependencies.
	workspaceDeps := buildWorkspaceDeps(project)

	// Build workspace entry.
	workspaces := orderedMap{
		{Key: "", Value: workspaceDeps},
	}

	// Build packages map.
	packages := buildPackages(result)

	// Top-level lockfile structure.
	lockfile := orderedMap{
		{Key: "lockfileVersion", Value: 0},
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

	return buf.Bytes(), nil
}

// buildWorkspaceDeps constructs the workspace dependency declaration object.
func buildWorkspaceDeps(project *ecosystem.ProjectSpec) orderedMap {
	deps := make(map[string]string)
	devDeps := make(map[string]string)
	optDeps := make(map[string]string)

	for _, d := range project.Dependencies {
		switch d.Type {
		case ecosystem.DepRegular:
			deps[d.Name] = d.Constraint
		case ecosystem.DepDev:
			devDeps[d.Name] = d.Constraint
		case ecosystem.DepOptional:
			optDeps[d.Name] = d.Constraint
		}
	}

	var entry orderedMap

	if len(deps) > 0 {
		entry = append(entry, orderedEntry{Key: "dependencies", Value: sortedStringMap(deps)})
	}
	if len(devDeps) > 0 {
		entry = append(entry, orderedEntry{Key: "devDependencies", Value: sortedStringMap(devDeps)})
	}
	if len(optDeps) > 0 {
		entry = append(entry, orderedEntry{Key: "optionalDependencies", Value: sortedStringMap(optDeps)})
	}

	return entry
}

// buildPackages constructs the packages map from the resolve result.
// Each entry maps a package name to an array:
// [resolved-spec, tarball-url, integrity, dependencies-map, license]
func buildPackages(result *ResolveResult) orderedMap {
	// Collect package names (not "name@version" keys - bun uses bare names).
	type pkgInfo struct {
		name string
		pkg  *ResolvedPackage
	}

	// Dedup by name since flat resolution means one version per name.
	byName := make(map[string]*ResolvedPackage)
	for _, pkg := range result.Packages {
		byName[pkg.Node.Name] = pkg
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	packages := make(orderedMap, 0, len(names))
	for _, name := range names {
		pkg := byName[name]
		entry := buildPackageEntry(pkg)
		packages = append(packages, orderedEntry{Key: name, Value: entry})
	}

	return packages
}

// buildPackageEntry constructs the array value for a single package in the
// packages map: [resolved-spec, tarball-url, integrity, deps, license]
func buildPackageEntry(pkg *ResolvedPackage) []interface{} {
	node := pkg.Node

	// resolved-spec: "name@version"
	resolvedSpec := fmt.Sprintf("%s@%s", node.Name, node.Version)

	// dependencies map (constraint strings, not resolved versions)
	var depsMap orderedMap
	if len(pkg.Dependencies) > 0 {
		depNames := make([]string, 0, len(pkg.Dependencies))
		for name := range pkg.Dependencies {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)

		depsMap = make(orderedMap, len(depNames))
		for i, name := range depNames {
			depsMap[i] = orderedEntry{Key: name, Value: pkg.Dependencies[name]}
		}
	} else {
		depsMap = orderedMap{}
	}

	// License string (empty string if not set).
	license := node.License

	return []interface{}{
		resolvedSpec,
		node.TarballURL,
		node.Integrity,
		depsMap,
		license,
	}
}

// sortedStringMap converts a map[string]string to an orderedMap with sorted keys.
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
