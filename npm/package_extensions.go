package npm

import (
	"encoding/json"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
)

// packageExtensionValue represents the value in a packageExtensions entry.
type packageExtensionValue struct {
	Dependencies     map[string]string `json:"dependencies,omitempty"`
	PeerDependencies map[string]string `json:"peerDependencies,omitempty"`
}

// ParsePackageExtensions parses pnpm's packageExtensions format.
// Keys are "name" or "name@range"; values are objects with optional
// dependencies and peerDependencies maps.
func ParsePackageExtensions(raw json.RawMessage) (*ecosystem.PackageExtensionSet, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var entries map[string]packageExtensionValue
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	var extensions []ecosystem.PackageExtension
	for key, val := range entries {
		name, versionRange := splitPackageKey(key)
		ext := ecosystem.PackageExtension{
			Name:         name,
			VersionRange: versionRange,
			Dependencies: val.Dependencies,
			PeerDeps:     val.PeerDependencies,
		}
		extensions = append(extensions, ext)
	}

	return &ecosystem.PackageExtensionSet{Extensions: extensions, RawJSON: raw}, nil
}

// splitPackageKey splits a key like "name@range" or "@scope/name@range"
// into (name, range). If no @ version separator is present, range is empty.
func splitPackageKey(key string) (name, versionRange string) {
	// Handle scoped packages: @scope/name@range
	// The first @ is part of the scope, so we need to find the @ after the package name.
	if strings.HasPrefix(key, "@") {
		// Scoped package: find the slash first, then look for @ after it.
		slashIdx := strings.Index(key, "/")
		if slashIdx == -1 {
			// Malformed scoped package, return as-is.
			return key, ""
		}
		// Look for @ after the slash.
		atIdx := strings.Index(key[slashIdx+1:], "@")
		if atIdx == -1 {
			return key, ""
		}
		// Offset atIdx relative to full string.
		realIdx := slashIdx + 1 + atIdx
		return key[:realIdx], key[realIdx+1:]
	}

	// Non-scoped package: find the first @.
	atIdx := strings.Index(key, "@")
	if atIdx == -1 {
		return key, ""
	}
	return key[:atIdx], key[atIdx+1:]
}
