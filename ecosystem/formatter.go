package ecosystem

// Formatter serializes a resolved dependency graph into a lockfile.
type Formatter interface {
	Format(graph *Graph, project *ProjectSpec) ([]byte, error)
}
