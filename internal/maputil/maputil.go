// Package maputil provides shared map utilities for locksmith.
package maputil

import "sort"

// SortedKeys returns the keys of a map[string]string in sorted order.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
