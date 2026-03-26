package pnpm

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
	"gopkg.in/yaml.v3"
)

// specifierNode creates a scalar node for a specifier value, quoting it only
// when YAML would misinterpret it (e.g., bare numbers like "1" becoming integers).
func specifierNode(value string) *yaml.Node {
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return scalarNode(value, yaml.SingleQuotedStyle)
	}
	return scalarNode(value, 0)
}

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
func buildImporter(project *ecosystem.ProjectSpec, result *ResolveResult, v6KeyFormat ...bool) *yaml.Node {
	useV6 := len(v6KeyFormat) > 0 && v6KeyFormat[0]
	node := &yaml.Node{Kind: yaml.MappingNode}

	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	// Merge peer deps into regular deps for the importer section.
	// pnpm v6/v9 with autoInstallPeers expects peer deps from package.json
	// to appear in the importer specifiers.
	regular := make(map[string]string, len(g.Regular)+len(g.Peer))
	for k, v := range g.Regular {
		regular[k] = v
	}
	for k, v := range g.Peer {
		if _, exists := regular[k]; !exists {
			regular[k] = v
		}
	}

	if len(regular) > 0 {
		depsNode := buildImporterDeps(regular, result, false, useV6)
		addMapping(node, "dependencies", depsNode)
	}
	if len(g.Dev) > 0 {
		devNode := buildImporterDeps(g.Dev, result, false, useV6)
		addMapping(node, "devDependencies", devNode)
	}
	if len(g.Optional) > 0 {
		optNode := buildImporterDeps(g.Optional, result, true, useV6)
		if len(optNode.Content) > 0 {
			addMapping(node, "optionalDependencies", optNode)
		}
	}

	return node
}

// buildImporterDeps constructs a dependency group within an importer entry.
// The deps map is name -> constraint (specifier from package.json).
// If skipUnresolved is true, deps with no resolved version are omitted.
func buildImporterDeps(deps map[string]string, result *ResolveResult, skipUnresolved bool, v6KeyFormat ...bool) *yaml.Node {
	useV6Key := len(v6KeyFormat) > 0 && v6KeyFormat[0]
	node := &yaml.Node{Kind: yaml.MappingNode}

	// Build lookups from dep name to resolved info via root edges.
	rootVersions := make(map[string]string)
	rootTargetNames := make(map[string]string)
	rootTarballURLs := make(map[string]string)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
				rootTargetNames[edge.Name] = edge.Target.Name
				rootTarballURLs[edge.Name] = edge.Target.TarballURL
			}
		}
	}

	names := maputil.SortedKeys(deps)

	for _, name := range names {
		resolvedVersion := rootVersions[name]
		if skipUnresolved && resolvedVersion == "" {
			continue
		}
		constraint := deps[name]

		// Determine the version value for the importer entry.
		versionValue := resolvedVersion
		tarballURL := rootTarballURLs[name]
		targetName := rootTargetNames[name]

		if strings.HasPrefix(tarballURL, "file:") {
			// file: deps use the specifier as the version.
			versionValue = tarballURL
		} else if strings.HasPrefix(tarballURL, "git+") {
			// git deps use the full git URL with commit as the version.
			versionValue = targetName + "@" + tarballURL
		} else if strings.HasPrefix(constraint, "github:") {
			// github: shorthand - pnpm converts to git+https format.
			versionValue = targetName + "@" + tarballURL
		} else if tarballURL != "" && strings.HasPrefix(tarballURL, "https://") && resolvedVersion == "0.0.0-local" {
			// tarball URL deps - use the resolved real name@version.
			versionValue = targetName + "@" + resolvedVersion
		} else if targetName != "" && targetName != name {
			// Aliases: use targetName@version format.
			if useV6Key {
				versionValue = "/" + targetName + "@" + resolvedVersion
			} else {
				versionValue = targetName + "@" + resolvedVersion
			}
		}

		depNode := &yaml.Node{Kind: yaml.MappingNode}
		addMapping(depNode, "specifier", specifierNode(constraint))
		addMapping(depNode, "version", scalarNode(versionValue, 0))
		addMapping(node, name, depNode)
	}

	return node
}

// pnpmPackageKey returns the pnpm v9 package key for a resolved package.
// For git deps: name@git+https://...#hash
// For file: deps: name@file:path
// For regular deps: name@version
func pnpmPackageKey(pkg *ResolvedPackage) string {
	url := pkg.Node.TarballURL
	if strings.HasPrefix(url, "git+") {
		return pkg.Node.Name + "@" + url
	}
	if strings.HasPrefix(url, "file:") {
		return pkg.Node.Name + "@" + url
	}
	return pkg.Node.Name + "@" + pkg.Node.Version
}

// buildPackages constructs the packages section with resolution info only.
func buildPackages(result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	keys := maputil.SortedMapKeys(result.Packages)

	for _, key := range keys {
		pkg := result.Packages[key]
		pkgNode := &yaml.Node{Kind: yaml.MappingNode}

		// resolution - varies by dep type.
		url := pkg.Node.TarballURL
		resolution := &yaml.Node{Kind: yaml.MappingNode}
		if strings.HasPrefix(url, "git+ssh://") || strings.HasPrefix(url, "git+https://") {
			// Git dep: {commit: HASH, repo: URL, type: git}
			parts := strings.SplitN(url, "#", 2)
			repo := strings.TrimPrefix(parts[0], "git+ssh://git@github.com/")
			repo = strings.TrimPrefix(repo, "git+https://")
			if strings.Contains(parts[0], "github.com") {
				repo = "https://github.com/" + strings.TrimPrefix(repo, "github.com/")
			} else {
				repo = strings.TrimPrefix(parts[0], "git+")
			}
			hash := ""
			if len(parts) > 1 {
				hash = parts[1]
			}
			addMapping(resolution, "commit", scalarNode(hash, 0))
			addMapping(resolution, "repo", scalarNode(repo, 0))
			addMapping(resolution, "type", scalarNode("git", 0))
			addMapping(pkgNode, "resolution", resolution)
			addMapping(pkgNode, "version", scalarNode(pkg.Node.Version, 0))
		} else if strings.HasPrefix(url, "file:") {
			// File dep: {directory: path, type: directory}
			path := strings.TrimPrefix(url, "file:")
			addMapping(resolution, "directory", scalarNode(path, 0))
			addMapping(resolution, "type", scalarNode("directory", 0))
			addMapping(pkgNode, "resolution", resolution)
		} else {
			addMapping(resolution, "integrity", scalarNode(pkg.Node.Integrity, 0))
			addMapping(pkgNode, "resolution", resolution)
		}

		displayKey := pnpmPackageKey(pkg)

		if len(pkg.Node.Engines) > 0 {
			enginesNode := &yaml.Node{Kind: yaml.MappingNode}
			engineKeys := maputil.SortedKeys(pkg.Node.Engines)
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

		addMapping(node, displayKey, pkgNode)
	}

	return node
}

// buildSnapshots constructs the snapshots section with dependency relationships.
func buildSnapshots(result *ResolveResult) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode}

	keys := maputil.SortedMapKeys(result.Packages)

	for _, key := range keys {
		pkg := result.Packages[key]
		snapNode := &yaml.Node{Kind: yaml.MappingNode}

		if len(pkg.Dependencies) > 0 {
			depsNode := &yaml.Node{Kind: yaml.MappingNode}

			depNames := maputil.SortedKeys(pkg.Dependencies)

			for _, depName := range depNames {
				depVersion := pkg.Dependencies[depName]
				addMapping(depsNode, depName, scalarNode(depVersion, 0))
			}
			addMapping(snapNode, "dependencies", depsNode)
		}

		addMapping(node, pnpmPackageKey(pkg), snapNode)
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
		engineKeys := maputil.SortedKeys(pkg.Node.Engines)
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
		depNames := maputil.SortedKeys(pkg.Dependencies)
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

	// Build lookup from dep name to resolved version and target name via root edges.
	rootVersions := make(map[string]string)
	rootTargetNames := make(map[string]string) // edge name -> target real name (for aliases)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
				rootTargetNames[edge.Name] = edge.Target.Name
			}
		}
	}

	// Group declared deps by type.
	g := ecosystem.GroupDependenciesByType(project.Dependencies)
	deps := g.Regular
	devDeps := g.Dev
	optDeps := g.Optional

	// specifiers: map of non-peer dep names to their constraint from package.json.
	// Skip peer deps (pnpm doesn't include them in specifiers) and unresolved optional deps.
	allDeps := make(map[string]string)
	for _, d := range project.Dependencies {
		if d.Type == ecosystem.DepPeer {
			continue
		}
		if d.Type == ecosystem.DepOptional && rootVersions[d.Name] == "" {
			continue
		}
		allDeps[d.Name] = d.Constraint
	}
	// Always emit specifiers section - pnpm 4/5 crash if it's missing.
	specNode := &yaml.Node{Kind: yaml.MappingNode}
	specNames := maputil.SortedKeys(allDeps)
	for _, name := range specNames {
		addMapping(specNode, name, specifierNode(allDeps[name]))
	}
	addMapping(root, "specifiers", specNode)

	// v5DepValue returns the dependency value for the v5 format.
	// For aliases (dep name != target name), use the /target-name/version path.
	// For regular deps, use just the version.
	v5DepValue := func(depName string) string {
		version := rootVersions[depName]
		targetName := rootTargetNames[depName]
		if targetName != "" && targetName != depName {
			return buildV5PackageKey(targetName, version)
		}
		return version
	}

	// dependencies: map of regular dep names to resolved versions.
	if len(deps) > 0 {
		depsNode := &yaml.Node{Kind: yaml.MappingNode}
		names := maputil.SortedKeys(deps)
		for _, name := range names {
			addMapping(depsNode, name, scalarNode(v5DepValue(name), 0))
		}
		addMapping(root, "dependencies", depsNode)
	}

	// devDependencies: map of dev dep names to resolved versions.
	if len(devDeps) > 0 {
		devNode := &yaml.Node{Kind: yaml.MappingNode}
		names := maputil.SortedKeys(devDeps)
		for _, name := range names {
			addMapping(devNode, name, scalarNode(v5DepValue(name), 0))
		}
		addMapping(root, "devDependencies", devNode)
	}

	// optionalDependencies: skip unresolved optional deps.
	if len(optDeps) > 0 {
		optNode := &yaml.Node{Kind: yaml.MappingNode}
		names := maputil.SortedKeys(optDeps)
		for _, name := range names {
			if rootVersions[name] == "" {
				continue
			}
			addMapping(optNode, name, scalarNode(rootVersions[name], 0))
		}
		if len(optNode.Content) > 0 {
			addMapping(root, "optionalDependencies", optNode)
		}
	}

	// packages section with /name/version keys.
	packagesNode := &yaml.Node{Kind: yaml.MappingNode}
	keys := maputil.SortedMapKeys(result.Packages)

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

	// importers (reuse the v9 importer builder, with v6 key format for aliases)
	importers := &yaml.Node{Kind: yaml.MappingNode}
	importerDot := buildImporter(project, result, true)
	addMapping(importers, ".", importerDot)
	addMapping(root, "importers", importers)

	devFlags := computeDevFlags(result, project)

	// packages section with /name@version keys.
	packagesNode := &yaml.Node{Kind: yaml.MappingNode}
	keys := maputil.SortedMapKeys(result.Packages)

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

// PnpmLockV4Formatter produces pnpm-lock.yaml lockfileVersion 5.1 output.
// Despite the name, pnpm CLI v4 actually produces lockfileVersion 5.1.
// Structurally identical to v5.4 but with a different version number.
type PnpmLockV4Formatter struct{}

func NewPnpmLockV4Formatter() *PnpmLockV4Formatter { return &PnpmLockV4Formatter{} }

func (f *PnpmLockV4Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for pnpm lockfile generation")
}

func (f *PnpmLockV4Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	doc := &yaml.Node{Kind: yaml.DocumentNode}
	root := &yaml.Node{Kind: yaml.MappingNode}
	doc.Content = append(doc.Content, root)

	addMapping(root, "lockfileVersion", scalarNode("5.1", 0))

	devFlags := computeDevFlags(result, project)

	rootVersions := make(map[string]string)
	rootTargetNamesV4 := make(map[string]string)
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target != nil {
				rootVersions[edge.Name] = edge.Target.Version
				rootTargetNamesV4[edge.Name] = edge.Target.Name
			}
		}
	}

	g := ecosystem.GroupDependenciesByType(project.Dependencies)

	// V5.1 has specifiers section (same as v5.4).
	// Skip peer deps and unresolved optional deps.
	allDeps := make(map[string]string)
	for _, d := range project.Dependencies {
		if d.Type == ecosystem.DepPeer {
			continue
		}
		if d.Type == ecosystem.DepOptional && rootVersions[d.Name] == "" {
			continue
		}
		allDeps[d.Name] = d.Constraint
	}
	specNode := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range maputil.SortedKeys(allDeps) {
		addMapping(specNode, name, specifierNode(allDeps[name]))
	}
	addMapping(root, "specifiers", specNode)

	// v4DepValue returns the v4/v5.1 dep value, using path format for aliases.
	v4DepValue := func(depName string) string {
		version := rootVersions[depName]
		targetName := rootTargetNamesV4[depName]
		if targetName != "" && targetName != depName {
			return buildV5PackageKey(targetName, version)
		}
		return version
	}

	if len(g.Regular) > 0 {
		depsNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range maputil.SortedKeys(g.Regular) {
			addMapping(depsNode, name, scalarNode(v4DepValue(name), 0))
		}
		addMapping(root, "dependencies", depsNode)
	}

	if len(g.Dev) > 0 {
		devNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range maputil.SortedKeys(g.Dev) {
			addMapping(devNode, name, scalarNode(v4DepValue(name), 0))
		}
		addMapping(root, "devDependencies", devNode)
	}

	if len(g.Optional) > 0 {
		optNode := &yaml.Node{Kind: yaml.MappingNode}
		for _, name := range maputil.SortedKeys(g.Optional) {
			if rootVersions[name] == "" {
				continue
			}
			addMapping(optNode, name, scalarNode(rootVersions[name], 0))
		}
		if len(optNode.Content) > 0 {
			addMapping(root, "optionalDependencies", optNode)
		}
	}

	packagesNode := &yaml.Node{Kind: yaml.MappingNode}
	for _, key := range maputil.SortedMapKeys(result.Packages) {
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
		return nil, fmt.Errorf("encoding pnpm lockfile v4: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("closing pnpm lockfile v4 encoder: %w", err)
	}

	return buf.Bytes(), nil
}
