// Package orderedjson provides a JSON-serializable ordered key-value map
// that preserves insertion order, unlike Go's built-in map type.
package orderedjson

import (
	"bytes"
	"encoding/json"
	"sort"
)

// Entry is a single key-value pair in an ordered JSON object.
type Entry struct {
	Key   string
	Value interface{}
}

// Map is a JSON-serializable ordered key-value list.
type Map []Entry

// MarshalJSON serializes the ordered map to JSON with keys in insertion order
// and HTML escaping disabled (so characters like > and < in engine constraints
// are not escaped).
func (om Map) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, entry := range om {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := json.Marshal(entry.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		var valBuf bytes.Buffer
		enc := json.NewEncoder(&valBuf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(entry.Value); err != nil {
			return nil, err
		}
		buf.Write(bytes.TrimRight(valBuf.Bytes(), "\n"))
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// FromStringMap converts a map[string]string to an ordered Map with
// alphabetically sorted keys.
func FromStringMap(m map[string]string) Map {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make(Map, len(keys))
	for i, k := range keys {
		result[i] = Entry{Key: k, Value: m[k]}
	}
	return result
}

// FromStringMapSorted converts a map to an ordered Map sorted by keys,
// useful for building the packages section of lockfiles.
func FromStringMapSorted(packages map[string]Map) Map {
	keys := make([]string, 0, len(packages))
	for k := range packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make(Map, len(keys))
	for i, k := range keys {
		result[i] = Entry{Key: k, Value: packages[k]}
	}
	return result
}
