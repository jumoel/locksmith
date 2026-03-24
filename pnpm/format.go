package pnpm

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/jumoel/locksmith/ecosystem"
	"gopkg.in/yaml.v3"
)

// PnpmLockV9Formatter produces pnpm-lock.yaml lockfileVersion 9.0 output.
// V9 splits package metadata (packages section) from dependency relationships
// (snapshots section) to reduce merge conflicts.
type PnpmLockV9Formatter struct{}

// NewPnpmLockV9Formatter returns a new v9 formatter.
func NewPnpmLockV9Formatter() *PnpmLockV9Formatter {
	return &PnpmLockV9Formatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since pnpm lockfiles require resolution
// metadata that only ResolveResult provides.
func (f *PnpmLockV9Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for pnpm lockfile generation")
}

// FormatFromResult produces pnpm-lock.yaml v9 bytes from a resolve result.
// Output is deterministic: all map keys are sorted alphabetically.
func (f *PnpmLockV9Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	// Build the YAML document using yaml.Node for ordered output.
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode}
	doc.Content = append(doc.Content, root)

	// lockfileVersion
	addMapping(root, "lockfileVersion", scalarNode("9.0", yaml.SingleQuotedStyle))

	// settings
	settings := &yaml.Node{Kind: yaml.MappingNode}
	addMapping(settings, "autoInstallPeers", scalarNode("true", 0))
	addMapping(settings, "excludeLinksFromLockfile", scalarNode("false", 0))
	addMapping(root, "settings", settings)

	// importers
	importers := &yaml.Node{Kind: yaml.MappingNode}
	importerDot := buildImporter(project, result)
	addMapping(importers, ".", importerDot)
	addMapping(root, "importers", importers)

	// packages (resolution info only)
	packagesNode := buildPackages(result)
	addMapping(root, "packages", packagesNode)

	// snapshots (dependency relationships)
	snapshotsNode := buildSnapshots(result)
	addMapping(root, "snapshots", snapshotsNode)

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding pnpm lockfile: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("closing pnpm lockfile encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// buildImporter constructs the importer entry for the root project (".").
func buildImporter(project *ecosystem.ProjectSpec, result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	// Group by dep type.
	deps := make(map[string]ecosystem.DeclaredDep)
	devDeps := make(map[string]ecosystem.DeclaredDep)
	optDeps := make(map[string]ecosystem.DeclaredDep)

	for _, d := range project.Dependencies {
		switch d.Type {
		case ecosystem.DepRegular:
			deps[d.Name] = d
		case ecosystem.DepDev:
			devDeps[d.Name] = d
		case ecosystem.DepOptional:
			optDeps[d.Name] = d
		}
	}

	if len(deps) > 0 {
		depsNode := buildImporterDeps(deps, result)
		addMapping(node, "dependencies", depsNode)
	}
	if len(devDeps) > 0 {
		devNode := buildImporterDeps(devDeps, result)
		addMapping(node, "devDependencies", devNode)
	}
	if len(optDeps) > 0 {
		optNode := buildImporterDeps(optDeps, result)
		addMapping(node, "optionalDependencies", optNode)
	}

	return node
}

// buildImporterDeps constructs a dependency group within an importer entry.
func buildImporterDeps(deps map[string]ecosystem.DeclaredDep, result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	// Build a lookup from dep name to the resolved version via root edges.
	// This is necessary when multiple versions of the same package exist
	// (e.g., is-odd@3.0.1 as root dep and is-odd@0.1.2 as transitive dep).
	rootVersions := make(map[string]string)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
			}
		}
	}

	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		dep := deps[name]
		resolvedVersion := rootVersions[name]

		depNode := &yaml.Node{Kind: yaml.MappingNode}
		addMapping(depNode, "specifier", scalarNode(dep.Constraint, 0))
		addMapping(depNode, "version", scalarNode(resolvedVersion, 0))
		addMapping(node, name, depNode)
	}

	return node
}

// buildPackages constructs the packages section with resolution info only.
func buildPackages(result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	keys := make([]string, 0, len(result.Packages))
	for k := range result.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pkg := result.Packages[key]
		pkgNode := &yaml.Node{Kind: yaml.MappingNode}

		// resolution
		resolution := &yaml.Node{Kind: yaml.MappingNode}
		addMapping(resolution, "integrity", scalarNode(pkg.Node.Integrity, 0))
		addMapping(pkgNode, "resolution", resolution)

		if len(pkg.Node.Engines) > 0 {
			enginesNode := &yaml.Node{Kind: yaml.MappingNode}
			engineKeys := make([]string, 0, len(pkg.Node.Engines))
			for k := range pkg.Node.Engines {
				engineKeys = append(engineKeys, k)
			}
			sort.Strings(engineKeys)
			for _, ek := range engineKeys {
				addMapping(enginesNode, ek, scalarNode(pkg.Node.Engines[ek], yaml.SingleQuotedStyle))
			}
			addMapping(pkgNode, "engines", enginesNode)
		}

		if pkg.Node.HasInstallScript {
			addMapping(pkgNode, "hasBin", scalarNode("true", 0))
		}

		if pkg.Node.Deprecated != "" {
			addMapping(pkgNode, "deprecated", scalarNode(pkg.Node.Deprecated, 0))
		}

		addMapping(node, key, pkgNode)
	}

	return node
}

// buildSnapshots constructs the snapshots section with dependency relationships.
func buildSnapshots(result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	keys := make([]string, 0, len(result.Packages))
	for k := range result.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pkg := result.Packages[key]
		snapNode := &yaml.Node{Kind: yaml.MappingNode}

		if len(pkg.Dependencies) > 0 {
			depsNode := &yaml.Node{Kind: yaml.MappingNode}

			depNames := make([]string, 0, len(pkg.Dependencies))
			for n := range pkg.Dependencies {
				depNames = append(depNames, n)
			}
			sort.Strings(depNames)

			for _, depName := range depNames {
				depVersion := pkg.Dependencies[depName]
				addMapping(depsNode, depName, scalarNode(depVersion, 0))
			}
			addMapping(snapNode, "dependencies", depsNode)
		}

		addMapping(node, key, snapNode)
	}

	return node
}

// scalarNode creates a yaml.Node with ScalarNode kind.
func scalarNode(value string, style yaml.Style) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: value,
		Style: style,
	}
}

// addMapping appends a key-value pair to a MappingNode.
func addMapping(mapping *yaml.Node, key string, value *yaml.Node) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		value,
	)
}

// computeDevFlags walks root edges to determine which packages are dev-only.
// A package is dev-only if it is only reachable through devDependency edges
// from the root. Returns a set of "name@version" keys that are dev-only.
func computeDevFlags(result *ResolveResult, project *ecosystem.ProjectSpec) map[string]bool {
	devOnly := make(map[string]bool)
	nonDev := make(map[string]bool)

	if result.Graph == nil || result.Graph.Root == nil {
		return devOnly
	}

	// Walk from each root edge. If it's a dev edge, mark reachable packages
	// as dev candidates. If it's a non-dev edge, mark reachable as non-dev.
	for _, edge := range result.Graph.Root.Dependencies {
		if edge.Target == nil {
			continue
		}
		isDev := edge.Type == ecosystem.DepDev
		walkDeps(edge.Target, isDev, devOnly, nonDev, make(map[string]bool))
	}

	// A package is dev-only if it was reached via dev edges and never via non-dev edges.
	result2 := make(map[string]bool)
	for key := range devOnly {
		if !nonDev[key] {
			result2[key] = true
		}
	}
	return result2
}

// walkDeps recursively marks packages as dev or non-dev reachable.
func walkDeps(node *ecosystem.Node, isDev bool, devSet, nonDevSet map[string]bool, visited map[string]bool) {
	key := node.Name + "@" + node.Version
	if visited[key] {
		return
	}
	visited[key] = true

	if isDev {
		devSet[key] = true
	} else {
		nonDevSet[key] = true
	}

	for _, edge := range node.Dependencies {
		if edge.Target != nil {
			walkDeps(edge.Target, isDev, devSet, nonDevSet, visited)
		}
	}
}

// buildV5PackageKey produces a v5 package path: /name/version for regular
// packages, /@scope/name/version for scoped packages.
func buildV5PackageKey(name, version string) string {
	return "/" + name + "/" + version
}

// buildV6PackageKey produces a v6 package path: /name@version.
func buildV6PackageKey(name, version string) string {
	return "/" + name + "@" + version
}

// flowMapping creates a yaml.Node mapping with FlowStyle for inline output
// like {integrity: sha512-..., node: '>=4'}.
func flowMapping(pairs ...string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
	for i := 0; i < len(pairs); i += 2 {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: pairs[i]},
			&yaml.Node{Kind: yaml.ScalarNode, Value: pairs[i+1]},
		)
	}
	return node
}

// buildInlinePackageNode constructs a package entry for v5/v6 format, where
// resolution, engines, dependencies, and dev flag are all inline in the
// packages section (no separate snapshots section).
func buildInlinePackageNode(pkg *ResolvedPackage, isDev bool) *yaml.Node {
	pkgNode := &yaml.Node{Kind: yaml.MappingNode}

	// resolution: {integrity: sha512-...}
	resNode := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
	addMapping(resNode, "integrity", scalarNode(pkg.Node.Integrity, 0))
	addMapping(pkgNode, "resolution", resNode)

	// engines: {node: '>=4'}
	if len(pkg.Node.Engines) > 0 {
		enginesNode := &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle}
		engineKeys := make([]string, 0, len(pkg.Node.Engines))
		for k := range pkg.Node.Engines {
			engineKeys = append(engineKeys, k)
		}
		sort.Strings(engineKeys)
		for _, ek := range engineKeys {
			addMapping(enginesNode, ek, scalarNode(pkg.Node.Engines[ek], yaml.SingleQuotedStyle))
		}
		addMapping(pkgNode, "engines", enginesNode)
	}

	if pkg.Node.HasInstallScript {
		addMapping(pkgNode, "hasBin", scalarNode("true", 0))
	}

	if pkg.Node.Deprecated != "" {
		addMapping(pkgNode, "deprecated", scalarNode(pkg.Node.Deprecated, 0))
	}

	// dependencies (inline, not in separate snapshots)
	if len(pkg.Dependencies) > 0 {
		depsNode := &yaml.Node{Kind: yaml.MappingNode}
		depNames := make([]string, 0, len(pkg.Dependencies))
		for n := range pkg.Dependencies {
			depNames = append(depNames, n)
		}
		sort.Strings(depNames)
		for _, depName := range depNames {
			addMapping(depsNode, depName, scalarNode(pkg.Dependencies[depName], 0))
		}
		addMapping(pkgNode, "dependencies", depsNode)
	}

	// dev flag
	if isDev {
		addMapping(pkgNode, "dev", scalarNode("true", 0))
	} else {
		addMapping(pkgNode, "dev", scalarNode("false", 0))
	}

	return pkgNode
}

// PnpmLockV5Formatter produces pnpm-lock.yaml lockfileVersion 5.4 output.
// V5 uses top-level specifiers/dependencies/devDependencies sections and
// package paths in /name/version format with inline dependency information.
type PnpmLockV5Formatter struct{}

// NewPnpmLockV5Formatter returns a new v5 formatter.
func NewPnpmLockV5Formatter() *PnpmLockV5Formatter {
	return &PnpmLockV5Formatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead.
func (f *PnpmLockV5Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for pnpm lockfile generation")
}

// FormatFromResult produces pnpm-lock.yaml v5 bytes from a resolve result.
// Output is deterministic: all map keys are sorted alphabetically.
func (f *PnpmLockV5Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode}
	doc.Content = append(doc.Content, root)

	// lockfileVersion: 5.4 (unquoted number)
	addMapping(root, "lockfileVersion", scalarNode("5.4", 0))

	devFlags := computeDevFlags(result, project)

	// Build lookup from dep name to resolved version via root edges.
	rootVersions := make(map[string]string)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
			}
		}
	}

	// Group declared deps by type.
	deps := make(map[string]ecosystem.DeclaredDep)
	devDeps := make(map[string]ecosystem.DeclaredDep)
	optDeps := make(map[string]ecosystem.DeclaredDep)

	for _, d := range project.Dependencies {
		switch d.Type {
		case ecosystem.DepRegular:
			deps[d.Name] = d
		case ecosystem.DepDev:
			devDeps[d.Name] = d
		case ecosystem.DepOptional:
			optDeps[d.Name] = d
		}
	}

	// specifiers: map of all dep names to their constraint from package.json.
	allDeps := make(map[string]string)
	for _, d := range project.Dependencies {
		allDeps[d.Name] = d.Constraint
	}
	if len(allDeps) > 0 {
		specNode := &yaml.Node{Kind: yaml.MappingNode}
		specNames := sortedKeys(allDeps)
		for _, name := range specNames {
			addMapping(specNode, name, scalarNode(allDeps[name], 0))
		}
		addMapping(root, "specifiers", specNode)
	}

	// dependencies: map of regular dep names to resolved versions.
	if len(deps) > 0 {
		depsNode := &yaml.Node{Kind: yaml.MappingNode}
		names := make([]string, 0, len(deps))
		for n := range deps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			addMapping(depsNode, name, scalarNode(rootVersions[name], 0))
		}
		addMapping(root, "dependencies", depsNode)
	}

	// devDependencies: map of dev dep names to resolved versions.
	if len(devDeps) > 0 {
		devNode := &yaml.Node{Kind: yaml.MappingNode}
		names := make([]string, 0, len(devDeps))
		for n := range devDeps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			addMapping(devNode, name, scalarNode(rootVersions[name], 0))
		}
		addMapping(root, "devDependencies", devNode)
	}

	// optionalDependencies
	if len(optDeps) > 0 {
		optNode := &yaml.Node{Kind: yaml.MappingNode}
		names := make([]string, 0, len(optDeps))
		for n := range optDeps {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			addMapping(optNode, name, scalarNode(rootVersions[name], 0))
		}
		addMapping(root, "optionalDependencies", optNode)
	}

	// packages section with /name/version keys.
	packagesNode := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(result.Packages))
	for k := range result.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pkg := result.Packages[key]
		v5Key := buildV5PackageKey(pkg.Node.Name, pkg.Node.Version)
		pkgNode := buildInlinePackageNode(pkg, devFlags[key])
		addMapping(packagesNode, v5Key, pkgNode)
	}
	addMapping(root, "packages", packagesNode)

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding pnpm lockfile v5: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("closing pnpm lockfile v5 encoder: %w", err)
	}

	return buf.Bytes(), nil
}

// PnpmLockV6Formatter produces pnpm-lock.yaml lockfileVersion 6.0 output.
// V6 uses importers (like v9), package paths in /name@version format, and
// inline dependency information in the packages section (no snapshots).
type PnpmLockV6Formatter struct{}

// NewPnpmLockV6Formatter returns a new v6 formatter.
func NewPnpmLockV6Formatter() *PnpmLockV6Formatter {
	return &PnpmLockV6Formatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead.
func (f *PnpmLockV6Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for pnpm lockfile generation")
}

// FormatFromResult produces pnpm-lock.yaml v6 bytes from a resolve result.
// Output is deterministic: all map keys are sorted alphabetically.
func (f *PnpmLockV6Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode}
	doc.Content = append(doc.Content, root)

	// lockfileVersion: '6.0' (quoted string, like v9)
	addMapping(root, "lockfileVersion", scalarNode("6.0", yaml.SingleQuotedStyle))

	// settings
	settings := &yaml.Node{Kind: yaml.MappingNode}
	addMapping(settings, "autoInstallPeers", scalarNode("true", 0))
	addMapping(settings, "excludeLinksFromLockfile", scalarNode("false", 0))
	addMapping(root, "settings", settings)

	// importers (reuse the v9 importer builder)
	importers := &yaml.Node{Kind: yaml.MappingNode}
	importerDot := buildImporter(project, result)
	addMapping(importers, ".", importerDot)
	addMapping(root, "importers", importers)

	devFlags := computeDevFlags(result, project)

	// packages section with /name@version keys.
	packagesNode := &yaml.Node{Kind: yaml.MappingNode}
	keys := make([]string, 0, len(result.Packages))
	for k := range result.Packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pkg := result.Packages[key]
		v6Key := buildV6PackageKey(pkg.Node.Name, pkg.Node.Version)
		pkgNode := buildInlinePackageNode(pkg, devFlags[key])
		addMapping(packagesNode, v6Key, pkgNode)
	}
	addMapping(root, "packages", packagesNode)

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding pnpm lockfile v6: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("closing pnpm lockfile v6 encoder: %w", err)
	}

	return buf.Bytes(), nil
}
