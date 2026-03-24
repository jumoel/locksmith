package ecosystem

import (
	"github.com/jumoel/locksmith/internal/semver"
)

// NodeIndex provides O(1) lookups by package name.
type NodeIndex struct {
	byName map[string][]*Node
}

func NewNodeIndex() *NodeIndex {
	return &NodeIndex{byName: make(map[string][]*Node)}
}

func (idx *NodeIndex) Add(name string, node *Node) {
	idx.byName[name] = append(idx.byName[name], node)
}

func (idx *NodeIndex) HasName(name string) bool {
	return len(idx.byName[name]) > 0
}

// FindSatisfying returns the first node for the given name whose version
// satisfies the constraint, or nil if none does.
func (idx *NodeIndex) FindSatisfying(name string, c *semver.Constraint) *Node {
	for _, node := range idx.byName[name] {
		v, err := semver.Parse(node.Version)
		if err == nil && c.Check(v) {
			return node
		}
	}
	return nil
}
