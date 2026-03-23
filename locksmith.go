// Package locksmith generates valid lockfiles from package spec files.
//
// It supports multiple ecosystems (npm, pnpm) and lockfile formats.
// The core architecture is ecosystem-agnostic, with each ecosystem
// providing its own registry client, resolver, and formatter implementations.
package locksmith

import (
	"context"
	"fmt"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/npm"
	"github.com/jumoel/locksmith/pnpm"
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
	case FormatPackageLockV3, FormatNpmShrinkwrap:
		return generateNpm(ctx, opts)
	case FormatPnpmLockV9:
		return generatePnpm(ctx, opts)
	case FormatPackageLockV1:
		return nil, fmt.Errorf("package-lock.json v1 is not yet implemented")
	case FormatPackageLockV2:
		return nil, fmt.Errorf("package-lock.json v2 is not yet implemented")
	case FormatPnpmLockV5:
		return nil, fmt.Errorf("pnpm-lock.yaml v5 is not yet implemented")
	case FormatPnpmLockV6:
		return nil, fmt.Errorf("pnpm-lock.yaml v6 is not yet implemented")
	default:
		return nil, fmt.Errorf("unknown output format: %s", opts.OutputFormat)
	}
}

func generateNpm(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)
	resolver := npm.NewResolver()
	formatter := npm.NewPackageLockV3Formatter()

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

func generatePnpm(ctx context.Context, opts GenerateOptions) (*GenerateResult, error) {
	// pnpm uses the npm registry, just a different resolver and formatter.
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)
	resolver := pnpm.NewResolver()
	formatter := pnpm.NewPnpmLockV9Formatter()

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
