package bun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
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
	// Detect multi-version packages for correct dep references.
	byName := make(map[string]int)
	for _, pkg := range result.Packages {
		byName[pkg.Node.Name]++
	}
	multiVersion := make(map[string]bool)
	for name, count := range byName {
		if count > 1 {
			multiVersion[name] = true
		}
	}

	workspaceDeps := buildWorkspaceDeps(project, result, multiVersion)
	workspaces := orderedjson.Map{
		{Key: "", Value: workspaceDeps},
	}

	packages := buildPackagesFromResult(result, multiVersion)

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

func buildWorkspaceDeps(project *ecosystem.ProjectSpec, result *ResolveResult, multiVersion map[string]bool) orderedjson.Map {
	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	// Build root version lookup for multi-version dep resolution.
	rootVersions := make(map[string]string)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
			}
		}
	}

	// resolveDepMap uses original constraints for single-version packages
	// and resolved versions for multi-version packages. Bun's frozen mode
	// can't do semver matching against "name@version" keys, so it needs the
	// exact resolved version to construct the key lookup.
	resolveDepMap := func(deps map[string]string) orderedjson.Map {
		m := make(orderedjson.Map, 0, len(deps))
		keys := maputil.SortedKeys(deps)
		for _, name := range keys {
			value := deps[name]
			if multiVersion[name] {
				if v, ok := rootVersions[name]; ok {
					value = v
				}
			}
			m = append(m, orderedjson.Entry{Key: name, Value: value})
		}
		return m
	}

	entry := orderedjson.Map{
		{Key: "name", Value: project.Name},
	}

	if len(g.Regular) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: resolveDepMap(g.Regular)})
	}
	if len(g.Dev) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "devDependencies", Value: resolveDepMap(g.Dev)})
	}
	if len(g.Optional) > 0 {
		entry = append(entry, orderedjson.Entry{Key: "optionalDependencies", Value: resolveDepMap(g.Optional)})
	}

	return entry
}

// buildPackages constructs the packages map. Each entry maps a package key
// to an array: [resolved-spec, "", {dependencies: {...}}, integrity]
//
// When only one version of a package exists, the key is the bare name.
// When multiple versions exist, each is keyed by "name@version".
func buildPackagesFromResult(result *ResolveResult, multiVersion map[string]bool) orderedjson.Map {
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
		packages = append(packages, orderedjson.Entry{Key: e.key, Value: buildPackageEntry(e.pkg, multiVersion)})
	}

	return packages
}

// buildPackageEntry constructs the array for a single package:
// [resolved-spec, "", metadata-object, integrity]
func buildPackageEntry(pkg *ResolvedPackage, multiVersion map[string]bool) []interface{} {
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
			dep := pkg.Dependencies[name]
			// For multi-version deps, use the resolved version so bun can
			// match the dependency to the correct "name@version" package key.
			// Bun looks up by dep name + resolved version to find the key.
			value := dep.Constraint
			if multiVersion[dep.ResolvedName] {
				value = dep.ResolvedVersion
			}
			depsMap[i] = orderedjson.Entry{Key: name, Value: value}
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

