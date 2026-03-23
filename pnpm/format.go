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

	names := make([]string, 0, len(deps))
	for n := range deps {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		dep := deps[name]
		// Find resolved version from result.
		resolvedVersion := ""
		for _, pkg := range result.Packages {
			if pkg.Node.Name == name {
				resolvedVersion = pkg.Node.Version
				break
			}
		}

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

// PnpmLockV5Formatter is a stub for pnpm-lock.yaml v5 format.
type PnpmLockV5Formatter struct{}

// NewPnpmLockV5Formatter returns a new v5 formatter stub.
func NewPnpmLockV5Formatter() *PnpmLockV5Formatter {
	return &PnpmLockV5Formatter{}
}

// Format returns an error indicating v5 is not yet implemented.
func (f *PnpmLockV5Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("pnpm-lock.yaml v5 is not yet implemented")
}

// PnpmLockV6Formatter is a stub for pnpm-lock.yaml v6 format.
type PnpmLockV6Formatter struct{}

// NewPnpmLockV6Formatter returns a new v6 formatter stub.
func NewPnpmLockV6Formatter() *PnpmLockV6Formatter {
	return &PnpmLockV6Formatter{}
}

// Format returns an error indicating v6 is not yet implemented.
func (f *PnpmLockV6Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("pnpm-lock.yaml v6 is not yet implemented")
}
