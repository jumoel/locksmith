package ecosystem

// GroupedDeps holds dependencies separated by type.
type GroupedDeps struct {
	Regular  map[string]string
	Dev      map[string]string
	Optional map[string]string
	Peer     map[string]string
}

// GroupDependenciesByType separates declared dependencies into groups by type.
func GroupDependenciesByType(deps []DeclaredDep) GroupedDeps {
	g := GroupedDeps{
		Regular:  make(map[string]string),
		Dev:      make(map[string]string),
		Optional: make(map[string]string),
		Peer:     make(map[string]string),
	}
	for _, d := range deps {
		switch d.Type {
		case DepRegular:
			g.Regular[d.Name] = d.Constraint
		case DepDev:
			g.Dev[d.Name] = d.Constraint
		case DepOptional:
			g.Optional[d.Name] = d.Constraint
		case DepPeer:
			g.Peer[d.Name] = d.Constraint
		}
	}
	return g
}
