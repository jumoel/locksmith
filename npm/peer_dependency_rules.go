package npm

import (
	"encoding/json"

	"github.com/jumoel/locksmith/ecosystem"
)

// peerDependencyRulesJSON is the JSON structure for pnpm peerDependencyRules.
type peerDependencyRulesJSON struct {
	IgnoreMissing   []string          `json:"ignoreMissing,omitempty"`
	AllowedVersions map[string]string `json:"allowedVersions,omitempty"`
	AllowAny        []string          `json:"allowAny,omitempty"`
}

// ParsePeerDependencyRules parses the pnpm peerDependencyRules field.
// Returns nil when the input is nil, empty, or "null".
func ParsePeerDependencyRules(raw json.RawMessage) (*ecosystem.PeerDependencyRules, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var parsed peerDependencyRulesJSON
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	return &ecosystem.PeerDependencyRules{
		IgnoreMissing:   parsed.IgnoreMissing,
		AllowedVersions: parsed.AllowedVersions,
		AllowAny:        parsed.AllowAny,
	}, nil
}
