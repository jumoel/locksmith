package npm

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jumoel/locksmith/ecosystem"
)

// packageJSON represents the relevant fields of a package.json file.
type packageJSON struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Dependencies         map[string]string `json:"dependencies,omitempty"`
	DevDependencies      map[string]string `json:"devDependencies,omitempty"`
	PeerDependencies     map[string]string `json:"peerDependencies,omitempty"`
	OptionalDependencies map[string]string `json:"optionalDependencies,omitempty"`
	PeerDependenciesMeta map[string]struct {
		Optional bool `json:"optional"`
	} `json:"peerDependenciesMeta,omitempty"`
	Overrides json.RawMessage `json:"overrides,omitempty"`
}

// SpecParser parses package.json files.
type SpecParser struct{}

// NewSpecParser returns a new package.json parser.
func NewSpecParser() *SpecParser {
	return &SpecParser{}
}

// Parse reads a package.json and returns a ProjectSpec.
func (p *SpecParser) Parse(data []byte) (*ecosystem.ProjectSpec, error) {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	spec := &ecosystem.ProjectSpec{
		Name:    pkg.Name,
		Version: pkg.Version,
	}

	for name, constraint := range pkg.Dependencies {
		spec.Dependencies = append(spec.Dependencies, ecosystem.DeclaredDep{
			Name:       name,
			Constraint: constraint,
			Type:       ecosystem.DepRegular,
		})
	}

	for name, constraint := range pkg.DevDependencies {
		spec.Dependencies = append(spec.Dependencies, ecosystem.DeclaredDep{
			Name:       name,
			Constraint: constraint,
			Type:       ecosystem.DepDev,
		})
	}

	for name, constraint := range pkg.PeerDependencies {
		spec.Dependencies = append(spec.Dependencies, ecosystem.DeclaredDep{
			Name:       name,
			Constraint: constraint,
			Type:       ecosystem.DepPeer,
		})
	}

	for name, constraint := range pkg.OptionalDependencies {
		spec.Dependencies = append(spec.Dependencies, ecosystem.DeclaredDep{
			Name:       name,
			Constraint: constraint,
			Type:       ecosystem.DepOptional,
		})
	}

	if len(pkg.PeerDependenciesMeta) > 0 {
		spec.PeerDepsMeta = make(map[string]ecosystem.PeerDepMeta, len(pkg.PeerDependenciesMeta))
		for name, pm := range pkg.PeerDependenciesMeta {
			spec.PeerDepsMeta[name] = ecosystem.PeerDepMeta{Optional: pm.Optional}
		}
	}

	// Sort dependencies by name for deterministic output.
	sortDeps(spec.Dependencies)

	return spec, nil
}

// sortDeps sorts declared dependencies by name, then by type.
func sortDeps(deps []ecosystem.DeclaredDep) {
	sort.Slice(deps, func(i, j int) bool {
		if deps[i].Name != deps[j].Name {
			return deps[i].Name < deps[j].Name
		}
		return deps[i].Type < deps[j].Type
	})
}
