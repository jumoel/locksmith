// Package locksmith generates valid lockfiles from package spec files.
//
// It supports multiple ecosystems (npm, pnpm, yarn, bun) and lockfile formats.
// The core architecture is ecosystem-agnostic, with each ecosystem
// providing its own registry client, resolver, and formatter implementations.
package locksmith

import (
	"context"
	"fmt"

	"github.com/jumoel/locksmith/bun"
	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/npm"
	"github.com/jumoel/locksmith/pnpm"
	"github.com/jumoel/locksmith/yarn"
)

// GenerateResult holds the output of lockfile generation.
type GenerateResult struct {
	// Lockfile is the generated lockfile contents.
	Lockfile []byte

	// Graph is the resolved dependency graph, available for inspection.
	Graph *ecosystem.Graph
}

// Generate parses the spec file, resolves dependencies, and produces a lockfile.
func Generate(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	switch opts.OutputFormat {
	case FormatPackageLockV1, FormatPackageLockV2, FormatPackageLockV3, FormatNpmShrinkwrap:
		return generateNpm(ctx, opts)
	case FormatPnpmLockV4, FormatPnpmLockV5, FormatPnpmLockV6, FormatPnpmLockV9:
		return generatePnpm(ctx, opts)
	case FormatYarnClassic, FormatYarnBerryV4, FormatYarnBerryV5, FormatYarnBerryV6, FormatYarnBerryV8:
		return generateYarn(ctx, opts)
	case FormatBunLock:
		return generateBun(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown output format: %s", opts.OutputFormat)
	}
}

// applyPlatformFilter parses the platform string and filters the graph,
// returning the set of removed keys. If no platform is set, it returns nil.
func applyPlatformFilter(graph *ecosystem.Graph, platform string) (map[string]bool, error) {
	if platform == "" {
		return nil, nil
	}
	plat, err := ecosystem.ParsePlatform(platform)
	if err != nil {
		return nil, err
	}
	return ecosystem.FilterGraphByPlatform(graph, plat), nil
}

// unreachableKeys returns package keys that are not reachable from the graph
// root. After platform filtering removes a parent node, its platform-agnostic
// transitive deps may remain in the packages map despite being orphaned.
func unreachableKeys(graph *ecosystem.Graph, packageKeys map[string]bool) map[string]bool {
	if graph == nil || graph.Root == nil {
		return nil
	}
	reachable := make(map[string]bool)
	var walk func(node *ecosystem.Node)
	walk = func(node *ecosystem.Node) {
		if node == nil {
			return
		}
		for _, edge := range node.Dependencies {
			if edge.Target == nil {
				continue
			}
			key := edge.Target.Name + "@" + edge.Target.Version
			if reachable[key] {
				continue
			}
			reachable[key] = true
			walk(edge.Target)
		}
	}
	walk(graph.Root)
	orphaned := make(map[string]bool)
	for key := range packageKeys {
		if !reachable[key] {
			orphaned[key] = true
		}
	}
	return orphaned
}

// npmFormatter is implemented by all npm lockfile formatters.
type npmFormatter interface {
	FormatFromResult(result *npm.ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error)
}

func generateNpm(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)
	resolver := npm.NewResolver()
	resolver.PolicyOverride = opts.PolicyOverride

	var formatter npmFormatter
	switch opts.OutputFormat {
	case FormatPackageLockV1:
		formatter = npm.NewPackageLockV1Formatter()
	case FormatPackageLockV2:
		formatter = npm.NewPackageLockV2Formatter()
	case FormatPackageLockV3:
		formatter = npm.NewPackageLockV3Formatter()
	case FormatNpmShrinkwrap:
		// npm-shrinkwrap.json uses v1 format for maximum backward compatibility.
		// npm 1-6 only understand the v1 hierarchical dependencies format, and
		// npm 7+ can also read v1 (with an upgrade warning).
		formatter = npm.NewPackageLockV1Formatter()
	}

	var parseResult *npm.ParseResult
	if len(opts.WorkspaceMembers) > 0 {
		var err error
		parseResult, err = parser.ParseFullWithWorkspaces(opts.SpecFile, opts.WorkspaceMembers)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace: %w", err)
		}
	} else {
		var err error
		parseResult, err = parser.ParseFull(opts.SpecFile)
		if err != nil {
			return nil, fmt.Errorf("parsing package.json: %w", err)
		}
	}
	spec := parseResult.Spec

	// Parse npm overrides and attach to spec.
	if parseResult.NpmOverrides != nil {
		rootDeps := make(map[string]string)
		for _, dep := range spec.Dependencies {
			rootDeps[dep.Name] = dep.Constraint
		}
		overrides, err := npm.ParseNpmOverrides(parseResult.NpmOverrides, rootDeps)
		if err != nil {
			return nil, fmt.Errorf("parsing npm overrides: %w", err)
		}
		spec.Overrides = overrides
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate, SpecDir: opts.SpecDir}
	if spec.Workspaces != nil {
		resolveOpts.WorkspaceIndex = ecosystem.NewWorkspaceIndex(spec.Workspaces)
	}
	result, err := resolver.ResolveWithPlacement(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	removed, err := applyPlatformFilter(result.Graph, opts.Platform)
	if err != nil {
		return nil, fmt.Errorf("filtering by platform: %w", err)
	}
	// PlacedNodes is keyed by path (e.g., "node_modules/foo"), not "name@version".
	// Match by checking the embedded Node pointer's name+version.
	removedPaths := make(map[string]bool)
	for path, placed := range result.PlacedNodes {
		if placed.Node != nil {
			key := placed.Node.Name + "@" + placed.Node.Version
			if removed[key] {
				delete(result.PlacedNodes, path)
				removedPaths[path] = true
			}
		}
	}
	// Also clean up Children maps in remaining PlacedNodes.
	for _, placed := range result.PlacedNodes {
		for childName, child := range placed.Children {
			if removedPaths[child.Path] {
				delete(placed.Children, childName)
			}
		}
	}
	// Clean root children too.
	if result.Root != nil {
		for childName, child := range result.Root.Children {
			if removedPaths[child.Path] {
				delete(result.Root.Children, childName)
			}
		}
	}

	lockfile, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{Lockfile: lockfile, Graph: result.Graph}, nil
}

// pnpmFormatter is implemented by all pnpm lockfile formatters.
type pnpmFormatter interface {
	FormatFromResult(result *pnpm.ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error)
}

func generatePnpm(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)
	resolver := pnpm.NewResolver()
	resolver.PolicyOverride = opts.PolicyOverride

	var formatter pnpmFormatter
	switch opts.OutputFormat {
	case FormatPnpmLockV4:
		formatter = pnpm.NewPnpmLockV4Formatter()
	case FormatPnpmLockV5:
		formatter = pnpm.NewPnpmLockV5Formatter()
	case FormatPnpmLockV6:
		formatter = pnpm.NewPnpmLockV6Formatter()
	case FormatPnpmLockV9:
		formatter = pnpm.NewPnpmLockV9Formatter()
	}

	var parseResult *npm.ParseResult
	if len(opts.WorkspaceMembers) > 0 {
		var err error
		parseResult, err = parser.ParseFullWithWorkspaces(opts.SpecFile, opts.WorkspaceMembers)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace: %w", err)
		}
	} else {
		var err error
		parseResult, err = parser.ParseFull(opts.SpecFile)
		if err != nil {
			return nil, fmt.Errorf("parsing package.json: %w", err)
		}
	}
	spec := parseResult.Spec

	// Parse pnpm overrides and attach to spec.
	if parseResult.PnpmOverrides != nil {
		overrides, err := npm.ParsePnpmOverrides(parseResult.PnpmOverrides)
		if err != nil {
			return nil, fmt.Errorf("parsing pnpm overrides: %w", err)
		}
		spec.Overrides = overrides
	}

	// Parse pnpm packageExtensions and attach to spec.
	if parseResult.PnpmPackageExtensions != nil {
		extensions, err := npm.ParsePackageExtensions(parseResult.PnpmPackageExtensions)
		if err != nil {
			return nil, fmt.Errorf("parsing pnpm packageExtensions: %w", err)
		}
		spec.PackageExtensions = extensions
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate, SpecDir: opts.SpecDir}
	if spec.Workspaces != nil {
		resolveOpts.WorkspaceIndex = ecosystem.NewWorkspaceIndex(spec.Workspaces)
	}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	removed, err := applyPlatformFilter(result.Graph, opts.Platform)
	if err != nil {
		return nil, fmt.Errorf("filtering by platform: %w", err)
	}
	for key := range removed {
		delete(result.Packages, key)
	}
	// Also clean dep references within remaining packages.
	for _, pkg := range result.Packages {
		for depName := range pkg.Dependencies {
			depKey := depName + "@" + pkg.Dependencies[depName]
			if removed[depKey] {
				delete(pkg.Dependencies, depName)
			}
		}
	}

	lockfile, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{Lockfile: lockfile, Graph: result.Graph}, nil
}

// yarnFormatter is implemented by all yarn lockfile formatters.
type yarnFormatter interface {
	FormatFromResult(result *yarn.ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error)
}

func generateYarn(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)

	var resolver *yarn.Resolver
	var formatter yarnFormatter
	switch opts.OutputFormat {
	case FormatYarnClassic:
		resolver = yarn.NewResolver()
		formatter = yarn.NewYarnClassicFormatter()
	case FormatYarnBerryV4:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV4Formatter()
	case FormatYarnBerryV5:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV5Formatter()
	case FormatYarnBerryV6:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV6Formatter()
	case FormatYarnBerryV8:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV8Formatter()
	}
	resolver.PolicyOverride = opts.PolicyOverride

	var parseResult *npm.ParseResult
	if len(opts.WorkspaceMembers) > 0 {
		var err error
		parseResult, err = parser.ParseFullWithWorkspaces(opts.SpecFile, opts.WorkspaceMembers)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace: %w", err)
		}
	} else {
		var err error
		parseResult, err = parser.ParseFull(opts.SpecFile)
		if err != nil {
			return nil, fmt.Errorf("parsing package.json: %w", err)
		}
	}
	spec := parseResult.Spec

	// Parse yarn resolutions and attach to spec.
	if parseResult.YarnResolutions != nil {
		overrides, err := npm.ParseYarnResolutions(parseResult.YarnResolutions)
		if err != nil {
			return nil, fmt.Errorf("parsing yarn resolutions: %w", err)
		}
		spec.Overrides = overrides
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate, SpecDir: opts.SpecDir}
	if spec.Workspaces != nil {
		resolveOpts.WorkspaceIndex = ecosystem.NewWorkspaceIndex(spec.Workspaces)
	}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	removed, err := applyPlatformFilter(result.Graph, opts.Platform)
	if err != nil {
		return nil, fmt.Errorf("filtering by platform: %w", err)
	}
	for key := range removed {
		delete(result.Packages, key)
	}
	// Also clean dep references within remaining yarn packages.
	for _, pkg := range result.Packages {
		for depName := range pkg.Dependencies {
			depKey := depName + "@" + pkg.Dependencies[depName]
			if removed[depKey] {
				delete(pkg.Dependencies, depName)
			}
		}
	}
	// Remove packages that became unreachable after platform filtering.
	// When a platform-specific node is removed (e.g., @img/sharp-wasm32),
	// its platform-agnostic transitive deps (e.g., @emnapi/runtime) may
	// remain in result.Packages despite being unreachable from root.
	// Their stale edges would pollute the berry constraint map.
	if len(removed) > 0 {
		pkgKeys := make(map[string]bool, len(result.Packages))
		for k := range result.Packages {
			pkgKeys[k] = true
		}
		for key := range unreachableKeys(result.Graph, pkgKeys) {
			delete(result.Packages, key)
		}
	}

	lockfile, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{Lockfile: lockfile, Graph: result.Graph}, nil
}

func generateBun(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)
	resolver := bun.NewResolver()
	formatter := bun.NewBunLockFormatter()

	var parseResult *npm.ParseResult
	if len(opts.WorkspaceMembers) > 0 {
		var err error
		parseResult, err = parser.ParseFullWithWorkspaces(opts.SpecFile, opts.WorkspaceMembers)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace: %w", err)
		}
	} else {
		var err error
		parseResult, err = parser.ParseFull(opts.SpecFile)
		if err != nil {
			return nil, fmt.Errorf("parsing package.json: %w", err)
		}
	}
	spec := parseResult.Spec

	// Parse npm overrides for bun (bun reads npm's overrides field).
	if parseResult.NpmOverrides != nil {
		rootDeps := make(map[string]string)
		for _, dep := range spec.Dependencies {
			rootDeps[dep.Name] = dep.Constraint
		}
		overrides, err := npm.ParseNpmOverrides(parseResult.NpmOverrides, rootDeps)
		if err != nil {
			return nil, fmt.Errorf("parsing npm overrides: %w", err)
		}
		spec.Overrides = overrides
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate, SpecDir: opts.SpecDir}
	if spec.Workspaces != nil {
		resolveOpts.WorkspaceIndex = ecosystem.NewWorkspaceIndex(spec.Workspaces)
	}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	// Note: bun lockfiles are platform-independent. All platform-specific
	// packages are included with os/cpu metadata; bun filters at install time.
	// So we skip applyPlatformFilter() here, unlike npm/pnpm/yarn.

	// Remove packages that are ONLY reachable as peer deps from root and
	// are not depended on by any other package in the graph. Bun doesn't
	// auto-install optional peers that nothing else needs.
	if result.Graph != nil && result.Graph.Root != nil {
		peerOnlyKeys := make(map[string]bool)
		nonPeerKeys := make(map[string]bool)
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil {
				continue
			}
			key := edge.Target.Name + "@" + edge.Target.Version
			if edge.Type == ecosystem.DepPeer {
				peerOnlyKeys[key] = true
			} else {
				nonPeerKeys[key] = true
			}
		}
		// Check if any non-root package needs the peer-only package.
		// This includes both regular dependencies AND peer dependencies
		// declared by installed packages (e.g., react-dom peers on react).
		needed := make(map[string]bool)
		for _, pkg := range result.Packages {
			for _, dep := range pkg.Dependencies {
				depKey := dep.ResolvedName + "@" + dep.ResolvedVersion
				needed[depKey] = true
			}
			// Also check peer deps - if a non-peer-only package peers on
			// a package, that package is needed in the lockfile.
			for peerName := range pkg.PeerDeps {
				// Find the resolved version of this peer dep.
				if node, ok := result.Graph.Nodes[peerName]; ok {
					needed[peerName+"@"+node.Version] = true
				}
				// Also check by iterating graph nodes for name match.
				for nodeKey, node := range result.Graph.Nodes {
					if node.Name == peerName {
						needed[nodeKey] = true
					}
				}
			}
		}
		for key := range peerOnlyKeys {
			if !nonPeerKeys[key] && !needed[key] {
				delete(result.Packages, key)
			}
		}
	}

	lockfile, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{Lockfile: lockfile, Graph: result.Graph}, nil
}
