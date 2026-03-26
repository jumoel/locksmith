package npm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/orderedjson"
)

// PackageLockV1Formatter produces package-lock.json lockfileVersion 1 output.
// V1 uses a hierarchical `dependencies` section (no `packages` section).
// This format was used by npm 5-6 and is also valid for npm-shrinkwrap.json
// consumed by npm 1-4.
type PackageLockV1Formatter struct{}

func NewPackageLockV1Formatter() *PackageLockV1Formatter {
	return &PackageLockV1Formatter{}
}

func (f *PackageLockV1Formatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for npm lockfile generation")
}

// FormatFromResult produces package-lock.json v1 bytes from a resolve result.
func (f *PackageLockV1Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	lockfile := orderedjson.Map{
		{Key: "name", Value: project.Name},
		{Key: "version", Value: project.Version},
		{Key: "lockfileVersion", Value: 1},
		{Key: "requires", Value: true},
	}

	// Build hierarchical dependencies from the placed node tree.
	deps := buildV1Dependencies(result.Root)
	if deps != nil {
		lockfile = append(lockfile, orderedjson.Entry{Key: "dependencies", Value: deps})
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

// buildV1Dependencies recursively constructs the hierarchical dependencies
// section from a placed node's children.
func buildV1Dependencies(parent *PlacedNode) orderedjson.Map {
	if len(parent.Children) == 0 {
		return nil
	}

	// Sort children by name.
	names := make([]string, 0, len(parent.Children))
	for name := range parent.Children {
		names = append(names, name)
	}
	sort.Strings(names)

	deps := make(orderedjson.Map, 0, len(names))
	for _, name := range names {
		child := parent.Children[name]
		entry := buildV1DepEntry(child, name)
		deps = append(deps, orderedjson.Entry{Key: name, Value: entry})
	}

	return deps
}

// buildV1DepEntry constructs a single dependency entry in the v1 format.
func buildV1DepEntry(placed *PlacedNode, depName string) orderedjson.Map {
	node := placed.Node

	// Tarball URL aliases: when the dep name differs from the node's real name,
	// it was resolved from a tarball URL or other alias. Use the TarballURL as version.
	if depName != node.Name && !strings.HasPrefix(node.TarballURL, "file:") && !strings.HasPrefix(node.TarballURL, "git+") {
		entry := orderedjson.Map{
			{Key: "version", Value: node.TarballURL},
			{Key: "integrity", Value: node.Integrity},
		}
		if len(node.Dependencies) > 0 {
			requires := make(map[string]string)
			for _, edge := range node.Dependencies {
				requires[edge.Name] = edge.Constraint
			}
			entry = append(entry, orderedjson.Entry{Key: "requires", Value: orderedjson.FromStringMap(requires)})
		}
		nestedDeps := buildV1Dependencies(placed)
		if nestedDeps != nil {
			entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: nestedDeps})
		}
		return entry
	}

	// Non-registry deps (file:, git) use the specifier as version in v1 format.
	if strings.HasPrefix(node.TarballURL, "file:") {
		entry := orderedjson.Map{
			{Key: "version", Value: node.TarballURL},
		}
		nestedDeps := buildV1Dependencies(placed)
		if nestedDeps != nil {
			entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: nestedDeps})
		}
		return entry
	}
	if strings.HasPrefix(node.TarballURL, "git+") || strings.HasPrefix(node.TarballURL, "github:") ||
		(strings.HasPrefix(node.TarballURL, "https://") && node.Version == "0.0.0-local") {
		entry := orderedjson.Map{
			{Key: "version", Value: node.TarballURL},
			{Key: "from", Value: node.TarballURL},
		}
		if len(node.Dependencies) > 0 {
			requires := make(map[string]string)
			for _, edge := range node.Dependencies {
				requires[edge.Name] = edge.Constraint
			}
			entry = append(entry, orderedjson.Entry{Key: "requires", Value: orderedjson.FromStringMap(requires)})
		}
		nestedDeps := buildV1Dependencies(placed)
		if nestedDeps != nil {
			entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: nestedDeps})
		}
		return entry
	}

	entry := orderedjson.Map{
		{Key: "version", Value: node.Version},
		{Key: "resolved", Value: node.TarballURL},
		{Key: "integrity", Value: node.Integrity},
	}

	if node.DevOnly {
		entry = append(entry, orderedjson.Entry{Key: "dev", Value: true})
	}
	if node.Optional {
		entry = append(entry, orderedjson.Entry{Key: "optional", Value: true})
	}

	// "requires" is a flat map of dependency name -> constraint.
	if len(node.Dependencies) > 0 {
		requires := make(map[string]string)
		for _, edge := range node.Dependencies {
			requires[edge.Name] = edge.Constraint
		}
		if len(requires) > 0 {
			entry = append(entry, orderedjson.Entry{Key: "requires", Value: orderedjson.FromStringMap(requires)})
		}
	}

	// Nested dependencies (children that couldn't be hoisted).
	nestedDeps := buildV1Dependencies(placed)
	if nestedDeps != nil {
		entry = append(entry, orderedjson.Entry{Key: "dependencies", Value: nestedDeps})
	}

	return entry
}
