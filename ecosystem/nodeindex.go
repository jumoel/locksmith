package ecosystem

import (
	"github.com/jumoel/locksmith/internal/semver"
)

// indexedNode pairs a node with its pre-parsed semver version.
type indexedNode struct {
	Node    *Node
	Version *semver.Version
}

// NodeIndex provides O(1) lookups by package name.
type NodeIndex struct {
	byName map[string][]indexedNode
}

func NewNodeIndex() *NodeIndex {
	return &NodeIndex{byName: make(map[string][]indexedNode)}
}

func (idx *NodeIndex) Add(name string, node *Node) {
	v, _ := semver.Parse(node.Version)
	idx.byName[name] = append(idx.byName[name], indexedNode{Node: node, Version: v})
}

func (idx *NodeIndex) HasName(name string) bool {
	return len(idx.byName[name]) > 0
}

// FindSatisfying returns the first node for the given name whose version
// satisfies the constraint, or nil if none does.
func (idx *NodeIndex) FindSatisfying(name string, c *semver.Constraint) *Node {
	for _, entry := range idx.byName[name] {
		if entry.Version != nil && c.Check(entry.Version) {
			return entry.Node
		}
	}
	return nil
}
