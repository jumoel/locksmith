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

func NewBunLockFormatter() *BunLockFormatter {
	return &BunLockFormatter{}
}

func (f *BunLockFormatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for bun lockfile generation")
}

type orderedEntry struct {
	Key   string
	Value interface{}
}

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
func (f *BunLockFormatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	workspaceDeps := buildWorkspaceDeps(project)
	workspaces := orderedMap{
		{Key: "", Value: workspaceDeps},
	}

	packages := buildPackages(result)

	lockfile := orderedMap{
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

	return buf.Bytes(), nil
}

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

	entry := orderedMap{
		{Key: "name", Value: project.Name},
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

	return entry
}

// buildPackages constructs the packages map. Each entry maps a package name
// to an array: [resolved-spec, "", {dependencies: {...}}, integrity]
func buildPackages(result *ResolveResult) orderedMap {
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

// buildPackageEntry constructs the array for a single package:
// [resolved-spec, "", metadata-object, integrity]
func buildPackageEntry(pkg *ResolvedPackage) []interface{} {
	node := pkg.Node
	resolvedSpec := fmt.Sprintf("%s@%s", node.Name, node.Version)

	// metadata object - contains dependencies if any
	var metadata orderedMap
	if len(pkg.Dependencies) > 0 {
		depNames := make([]string, 0, len(pkg.Dependencies))
		for name := range pkg.Dependencies {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)

		depsMap := make(orderedMap, len(depNames))
		for i, name := range depNames {
			depsMap[i] = orderedEntry{Key: name, Value: pkg.Dependencies[name]}
		}
		metadata = orderedMap{
			{Key: "dependencies", Value: depsMap},
		}
	} else {
		metadata = orderedMap{}
	}

	return []interface{}{
		resolvedSpec,
		"",
		metadata,
		node.Integrity,
	}
}

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
