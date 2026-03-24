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
	case FormatPnpmLockV5, FormatPnpmLockV6, FormatPnpmLockV9:
		return generatePnpm(ctx, opts)
	case FormatYarnClassic, FormatYarnBerryV5, FormatYarnBerryV6, FormatYarnBerryV7, FormatYarnBerryV8:
		return generateYarn(ctx, opts)
	case FormatBunLock:
		return generateBun(ctx, opts)
	default:
		return nil, fmt.Errorf("unknown output format: %s", opts.OutputFormat)
	}
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

	spec, err := parser.Parse(opts.SpecFile)
	if err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate}
	result, err := resolver.ResolveWithPlacement(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
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
	case FormatPnpmLockV5:
		formatter = pnpm.NewPnpmLockV5Formatter()
	case FormatPnpmLockV6:
		formatter = pnpm.NewPnpmLockV6Formatter()
	case FormatPnpmLockV9:
		formatter = pnpm.NewPnpmLockV9Formatter()
	}

	spec, err := parser.Parse(opts.SpecFile)
	if err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
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
	case FormatYarnBerryV5:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV5Formatter()
	case FormatYarnBerryV6:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV6Formatter()
	case FormatYarnBerryV7:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV7Formatter()
	case FormatYarnBerryV8:
		resolver = yarn.NewBerryResolver()
		formatter = yarn.NewYarnBerryV8Formatter()
	}

	spec, err := parser.Parse(opts.SpecFile)
	if err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
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

	spec, err := parser.Parse(opts.SpecFile)
	if err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	resolveOpts := ecosystem.ResolveOptions{CutoffDate: opts.CutoffDate}
	result, err := resolver.ResolveForLockfile(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	lockfile, err := formatter.FormatFromResult(result, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{Lockfile: lockfile, Graph: result.Graph}, nil
}
