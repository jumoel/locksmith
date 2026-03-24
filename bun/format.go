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

	packages := buildPackages(result)

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

	return buf.Bytes(), nil
}

func buildWorkspaceDeps(project *ecosystem.ProjectSpec) orderedjson.Map {
	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	entry := orderedjson.Map{
		{Key: "name", Value: project.Name},
	}

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

// buildPackages constructs the packages map. Each entry maps a package key
// to an array: [resolved-spec, "", {dependencies: {...}}, integrity]
//
// When only one version of a package exists, the key is the bare name.
// When multiple versions exist, each is keyed by "name@version".
func buildPackages(result *ResolveResult) orderedjson.Map {
	// Group packages by name to detect multi-version cases.
	byName := make(map[string][]*ResolvedPackage)
	for _, pkg := range result.Packages {
		byName[pkg.Node.Name] = append(byName[pkg.Node.Name], pkg)
	}

	// Build keyed entries: bare name for single-version, name@version for multi.
	type keyedPkg struct {
		key string
		pkg *ResolvedPackage
	}
	var entries []keyedPkg
	for name, pkgs := range byName {
		if len(pkgs) == 1 {
			entries = append(entries, keyedPkg{key: name, pkg: pkgs[0]})
		} else {
			for _, pkg := range pkgs {
				entries = append(entries, keyedPkg{
					key: fmt.Sprintf("%s@%s", name, pkg.Node.Version),
					pkg: pkg,
				})
			}
		}
	}

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

	// metadata object - contains dependencies if any
	var metadata orderedjson.Map
	if len(pkg.Dependencies) > 0 {
		depNames := make([]string, 0, len(pkg.Dependencies))
		for name := range pkg.Dependencies {
			depNames = append(depNames, name)
		}
		sort.Strings(depNames)

		depsMap := make(orderedjson.Map, len(depNames))
		for i, name := range depNames {
			depsMap[i] = orderedjson.Entry{Key: name, Value: pkg.Dependencies[name]}
		}
		metadata = orderedjson.Map{
			{Key: "dependencies", Value: depsMap},
		}
	} else {
		metadata = orderedjson.Map{}
	}

	return []interface{}{
		resolvedSpec,
		"",
		metadata,
		node.Integrity,
	}
}

