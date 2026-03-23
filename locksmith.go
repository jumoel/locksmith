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
	parser, registry, resolver, formatter, err := buildPipeline(opts)
	if err != nil {
		return nil, err
	}

	spec, err := parser.Parse(opts.SpecFile)
	if err != nil {
		return nil, fmt.Errorf("parsing spec file: %w", err)
	}

	resolveOpts := ecosystem.ResolveOptions{
		CutoffDate: opts.CutoffDate,
	}

	graph, err := resolver.Resolve(ctx, spec, registry, resolveOpts)
	if err != nil {
		return nil, fmt.Errorf("resolving dependencies: %w", err)
	}

	lockfile, err := formatter.Format(graph, spec)
	if err != nil {
		return nil, fmt.Errorf("formatting lockfile: %w", err)
	}

	return &GenerateResult{
		Lockfile: lockfile,
		Graph:    graph,
	}, nil
}

// buildPipeline wires up the parser, registry, resolver, and formatter
// based on the requested output format.
func buildPipeline(opts GenerateOptions) (ecosystem.SpecParser, ecosystem.Registry, ecosystem.Resolver, ecosystem.Formatter, error) {
	switch opts.OutputFormat {
	case FormatPackageLockV3, FormatNpmShrinkwrap:
		return buildNpmPipeline(opts)
	case FormatPackageLockV1:
		return nil, nil, nil, nil, fmt.Errorf("package-lock.json v1 is not yet implemented")
	case FormatPackageLockV2:
		return nil, nil, nil, nil, fmt.Errorf("package-lock.json v2 is not yet implemented")
	case FormatPnpmLockV9:
		return nil, nil, nil, nil, fmt.Errorf("pnpm-lock.yaml v9 is not yet implemented")
	case FormatPnpmLockV5:
		return nil, nil, nil, nil, fmt.Errorf("pnpm-lock.yaml v5 is not yet implemented")
	case FormatPnpmLockV6:
		return nil, nil, nil, nil, fmt.Errorf("pnpm-lock.yaml v6 is not yet implemented")
	default:
		return nil, nil, nil, nil, fmt.Errorf("unknown output format: %s", opts.OutputFormat)
	}
}

func buildNpmPipeline(opts GenerateOptions) (ecosystem.SpecParser, ecosystem.Registry, ecosystem.Resolver, ecosystem.Formatter, error) {
	parser := npm.NewSpecParser()
	registry := npm.NewRegistryClient(opts.RegistryURL)

	// TODO: implement npm resolver and formatter
	_ = parser
	_ = registry

	return nil, nil, nil, nil, fmt.Errorf("npm pipeline not yet fully wired (resolver and formatter pending)")
}
