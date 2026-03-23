package npm

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/jumoel/locksmith/ecosystem"
)

// PackageLockV2Formatter produces package-lock.json lockfileVersion 2 output.
// V2 includes both `packages` (flat, same as v3) and `dependencies` (hierarchical,
// same as v1) for backwards compatibility with npm 5-6.
type PackageLockV2Formatter struct{}

func NewPackageLockV2Formatter() *PackageLockV2Formatter {
	return &PackageLockV2Formatter{}
}

func (f *PackageLockV2Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for npm lockfile generation")
}

// FormatFromResult produces package-lock.json v2 bytes from a resolve result.
func (f *PackageLockV2Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	// Build the packages map (same as v3).
	packages := make(map[string]orderedMap, len(result.PlacedNodes)+1)
	packages[""] = buildRootEntry(project)
	for path, placed := range result.PlacedNodes {
		packages[path] = buildPackageEntry(placed.Node)
	}

	// Build the hierarchical dependencies (same as v1).
	deps := buildV1Dependencies(result.Root)

	lockfile := orderedMap{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
		{Key: "lockfileVersion", Value: 2},
		{Key: "requires", Value: true},
		{Key: "packages", Value: buildOrderedPackages(packages)},
	}

	if deps != nil {
		lockfile = append(lockfile, orderedEntry{Key: "dependencies", Value: deps})
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(lockfile); err != nil {
		return nil, fmt.Errorf("encoding lockfile: %w", err)
	}

	return buf.Bytes(), nil
}
