package npm

import "encoding/json"

// ParsePatchedDependencies parses the pnpm.patchedDependencies field.
// It maps "name@version" keys to patch file paths.
func ParsePatchedDependencies(raw json.RawMessage) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
