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
	Overrides  json.RawMessage `json:"overrides,omitempty"`
	Pnpm *struct {
		Overrides         json.RawMessage `json:"overrides,omitempty"`
		PackageExtensions json.RawMessage `json:"packageExtensions,omitempty"`
	} `json:"pnpm,omitempty"`
	Resolutions json.RawMessage `json:"resolutions,omitempty"`
	Workspaces  json.RawMessage `json:"workspaces,omitempty"`
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

// ParseResult holds the spec plus raw override fields for PM-specific parsing.
type ParseResult struct {
	Spec                  *ecosystem.ProjectSpec
	NpmOverrides          json.RawMessage
	PnpmOverrides         json.RawMessage
	YarnResolutions       json.RawMessage
	PnpmPackageExtensions json.RawMessage
}

// ParseFull reads a package.json and returns the spec plus raw override data.
// This lets callers parse PM-specific overrides after knowing which PM they target.
func (p *SpecParser) ParseFull(data []byte) (*ParseResult, error) {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing package.json: %w", err)
	}

	spec, err := p.Parse(data)
	if err != nil {
		return nil, err
	}

	result := &ParseResult{
		Spec:            spec,
		NpmOverrides:    pkg.Overrides,
		YarnResolutions: pkg.Resolutions,
	}
	if pkg.Pnpm != nil {
		result.PnpmOverrides = pkg.Pnpm.Overrides
		result.PnpmPackageExtensions = pkg.Pnpm.PackageExtensions
	}

	return result, nil
}

// ParseFullWithWorkspaces parses a root spec with workspace members and returns raw override data.
func (p *SpecParser) ParseFullWithWorkspaces(rootData []byte, memberData map[string][]byte) (*ParseResult, error) {
	result, err := p.ParseFull(rootData)
	if err != nil {
		return nil, err
	}

	if len(memberData) == 0 {
		return result, nil
	}

	result.Spec.Workspaces = make([]*ecosystem.WorkspaceMember, 0, len(memberData))
	paths := make([]string, 0, len(memberData))
	for p := range memberData {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		data := memberData[relPath]
		memberSpec, err := p.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace member %s: %w", relPath, err)
		}
		result.Spec.Workspaces = append(result.Spec.Workspaces, &ecosystem.WorkspaceMember{
			RelPath: relPath,
			Spec:    memberSpec,
		})
	}

	return result, nil
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

// ParseWorkspaceGlobs extracts workspace globs from a raw package.json.
// Returns nil if no workspaces field is present.
func ParseWorkspaceGlobs(data []byte) ([]string, error) {
	var pkg struct {
		Workspaces json.RawMessage `json:"workspaces,omitempty"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	if pkg.Workspaces == nil {
		return nil, nil
	}

	// Try array form first: ["packages/*"]
	var globs []string
	if err := json.Unmarshal(pkg.Workspaces, &globs); err == nil {
		return globs, nil
	}

	// Try object form: {"packages": ["packages/*"]}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(pkg.Workspaces, &obj); err == nil {
		return obj.Packages, nil
	}

	return nil, nil
}

// ParseWithWorkspaces parses a root spec and workspace member specs.
// memberData maps relative paths to raw package.json contents.
func (p *SpecParser) ParseWithWorkspaces(rootData []byte, memberData map[string][]byte) (*ecosystem.ProjectSpec, error) {
	spec, err := p.Parse(rootData)
	if err != nil {
		return nil, err
	}

	if len(memberData) == 0 {
		return spec, nil
	}

	spec.Workspaces = make([]*ecosystem.WorkspaceMember, 0, len(memberData))
	// Sort keys for deterministic order
	paths := make([]string, 0, len(memberData))
	for p := range memberData {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		data := memberData[relPath]
		memberSpec, err := p.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parsing workspace member %s: %w", relPath, err)
		}
		spec.Workspaces = append(spec.Workspaces, &ecosystem.WorkspaceMember{
			RelPath: relPath,
			Spec:    memberSpec,
		})
	}

	return spec, nil
}
