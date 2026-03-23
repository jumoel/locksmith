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
}

// Edge represents a dependency relationship.
type Edge struct {
	Name       string  // Dependency name
	Constraint string  // Original version constraint (e.g., "^1.2.3")
	Target     *Node   // Resolved target node
	Type       DepType
}

// ProjectSpec represents the declared dependencies of a project.
type ProjectSpec struct {
	Name         string
	Version      string
	Dependencies []DeclaredDep
}

// DeclaredDep is a dependency declared in a spec file.
type DeclaredDep struct {
	Name       string
	Constraint string // Semver range, URL, git ref, etc.
	Type       DepType
}
