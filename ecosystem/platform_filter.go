package ecosystem

import (
	"fmt"
	"strings"
)

// Platform represents a target OS/CPU pair parsed from a string like "linux/x64".
type Platform struct {
	OS  string
	CPU string
}

// ParsePlatform parses a platform string in "os/cpu" format (e.g., "linux/x64").
func ParsePlatform(s string) (Platform, error) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Platform{}, fmt.Errorf("invalid platform %q: expected os/cpu format (e.g., linux/x64)", s)
	}
	return Platform{OS: parts[0], CPU: parts[1]}, nil
}

// NodeMatchesPlatform returns true if the node is compatible with the given platform.
// A node is compatible if:
//   - it has no OS restrictions and no CPU restrictions, OR
//   - its OS list contains the target OS (or a negation pattern does not exclude it)
//     AND its CPU list contains the target CPU (or a negation pattern does not exclude it)
func NodeMatchesPlatform(node *Node, plat Platform) bool {
	if !fieldMatchesPlatform(node.OS, plat.OS) {
		return false
	}
	if !fieldMatchesPlatform(node.CPU, plat.CPU) {
		return false
	}
	return true
}

// fieldMatchesPlatform checks whether a single OS or CPU restriction list matches
// the target value. npm supports negation entries like "!win32" meaning "everything
// except win32".
func fieldMatchesPlatform(allowed []string, target string) bool {
	if len(allowed) == 0 {
		return true
	}

	hasNegation := false
	hasPositive := false
	for _, entry := range allowed {
		if strings.HasPrefix(entry, "!") {
			hasNegation = true
			if entry[1:] == target {
				// Explicitly excluded.
				return false
			}
		} else {
			hasPositive = true
		}
	}

	// If the list is entirely negations, then anything not excluded is allowed.
	if hasNegation && !hasPositive {
		return true
	}

	// Otherwise check for an explicit positive match.
	for _, entry := range allowed {
		if entry == target {
			return true
		}
	}

	return false
}

// FilterGraphByPlatform removes nodes from the graph that are incompatible with
// the target platform. It returns the set of removed node keys ("name@version")
// so callers can clean up format-specific maps.
func FilterGraphByPlatform(graph *Graph, plat Platform) map[string]bool {
	removed := make(map[string]bool)

	for key, node := range graph.Nodes {
		if !NodeMatchesPlatform(node, plat) {
			removed[key] = true
			delete(graph.Nodes, key)
		}
	}

	if len(removed) == 0 {
		return removed
	}

	// Remove edges pointing to filtered-out nodes.
	pruneEdges(graph.Root, removed)
	for _, node := range graph.Nodes {
		pruneEdges(node, removed)
	}

	return removed
}

// pruneEdges removes edges from a node whose targets were filtered out.
func pruneEdges(node *Node, removed map[string]bool) {
	filtered := node.Dependencies[:0]
	for _, edge := range node.Dependencies {
		if edge.Target == nil {
			filtered = append(filtered, edge)
			continue
		}
		key := edge.Target.Name + "@" + edge.Target.Version
		if removed[key] {
			continue
		}
		filtered = append(filtered, edge)
	}
	node.Dependencies = filtered
}
