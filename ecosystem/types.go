package ecosystem

// DepType represents the kind of dependency relationship.
type DepType int

const (
	DepRegular DepType = iota
	DepDev
	DepOptional
	DepPeer
)

// Graph is the resolved dependency tree.
type Graph struct {
	Root  *Node
	Nodes map[string]*Node // keyed by "name@version"
}

// Node represents a resolved package in the graph.
type Node struct {
	Name             string
	Version          string
	Integrity        string // SRI hash (sha512-...)
	Shasum           string // SHA-1 hash of the tarball
	TarballURL       string
	Dependencies     []*Edge
	DevOnly          bool
	Optional         bool
	HasInstallScript bool
	Engines          map[string]string
	OS               []string
	CPU              []string
	Bin              map[string]string
	License          string
	Deprecated       string
	// PeerDeps maps peer dependency names to version constraints.
	// These are declared by the package but not automatically installed.
	PeerDeps map[string]string
	// PeerDepsMeta holds metadata about peer dependencies (e.g., optional flag).
	PeerDepsMeta map[string]PeerDepMeta
	// Funding holds funding information for the package.
	// Can be a URL string or a structured object (stored as raw JSON).
	Funding interface{}
	// BundleDeps lists packages declared in bundleDependencies.
	// When non-empty, bun nests all transitive deps under this package's key.
	BundleDeps map[string]bool
}

// Edge represents a dependency relationship.
type Edge struct {
	Name       string  // Dependency name
	Constraint string  // Original version constraint (e.g., "^1.2.3")
	Target     *Node   // Resolved target node
	Type       DepType
}

// WorkspaceMember represents a single package within a workspace.
type WorkspaceMember struct {
	// RelPath is the workspace member's relative path from the root (e.g., "packages/foo").
	RelPath string
	// Spec is the parsed package.json for this workspace member.
	Spec *ProjectSpec
}

// ProjectSpec represents the declared dependencies of a project.
type ProjectSpec struct {
	Name         string
	Version      string
	Dependencies []DeclaredDep
	// PeerDepsMeta holds metadata about peer dependencies (e.g., optional flag)
	// as declared in the project's package.json peerDependenciesMeta field.
	PeerDepsMeta map[string]PeerDepMeta
	// Workspaces holds workspace members for monorepo projects.
	// Nil for single-package projects.
	Workspaces []*WorkspaceMember
}

// DeclaredDep is a dependency declared in a spec file.
type DeclaredDep struct {
	Name       string
	Constraint string // Semver range, URL, git ref, etc.
	Type       DepType
}
