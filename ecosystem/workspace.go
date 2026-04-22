package ecosystem

// WorkspaceIndex provides O(1) lookups of workspace members by package name.
type WorkspaceIndex struct {
	byName map[string]*WorkspaceMember
}

// NewWorkspaceIndex creates a WorkspaceIndex from workspace members.
func NewWorkspaceIndex(members []*WorkspaceMember) *WorkspaceIndex {
	idx := &WorkspaceIndex{byName: make(map[string]*WorkspaceMember, len(members))}
	for _, m := range members {
		if m.Spec != nil && m.Spec.Name != "" {
			idx.byName[m.Spec.Name] = m
		}
	}
	return idx
}

// Resolve returns the workspace member with the given package name, or nil.
func (wi *WorkspaceIndex) Resolve(name string) *WorkspaceMember {
	if wi == nil {
		return nil
	}
	return wi.byName[name]
}

// Names returns all workspace member package names.
func (wi *WorkspaceIndex) Names() []string {
	names := make([]string, 0, len(wi.byName))
	for name := range wi.byName {
		names = append(names, name)
	}
	return names
}
