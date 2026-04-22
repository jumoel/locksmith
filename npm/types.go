package npm

import (
	"encoding/json"
)

// LicenseField handles npm's license field which can be a string or {"type": "MIT", "url": "..."}.
type LicenseField string

func (l *LicenseField) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*l = LicenseField(s)
		return nil
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		*l = LicenseField(obj.Type)
		return nil
	}
	*l = ""
	return nil
}

// FlexString accepts string, bool, or number JSON values without failing.
type FlexString string

func (f *FlexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexString(s)
		return nil
	}
	*f = ""
	return nil
}

// FlexMap handles fields that can be either a map or an unexpected type (null, string, etc.).
type FlexMap map[string]string

func (fm *FlexMap) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err == nil {
		*fm = FlexMap(m)
		return nil
	}
	*fm = nil
	return nil
}

// Packument is the full package document from the npm registry.
type Packument struct {
	ID       string                     `json:"_id"`
	Name     string                     `json:"name"`
	DistTags map[string]string          `json:"dist-tags"`
	Versions map[string]Version         `json:"versions"`
	Time     map[string]string          `json:"time,omitempty"`
}

// Version represents a specific version of a package in the registry.
type Version struct {
	Name                 string                  `json:"name"`
	Version              string                  `json:"version"`
	Dependencies         FlexMap                 `json:"dependencies,omitempty"`
	DevDependencies      FlexMap                 `json:"devDependencies,omitempty"`
	PeerDependencies     FlexMap                 `json:"peerDependencies,omitempty"`
	OptionalDependencies FlexMap                 `json:"optionalDependencies,omitempty"`
	PeerDependenciesMeta map[string]PeerMeta     `json:"peerDependenciesMeta,omitempty"`
	BundleDependencies   json.RawMessage         `json:"bundleDependencies,omitempty"`
	BundledDependencies  json.RawMessage         `json:"bundledDependencies,omitempty"`
	Dist                 Dist                    `json:"dist"`
	Engines              FlexMap                 `json:"engines,omitempty"`
	OS                   []string                `json:"os,omitempty"`
	CPU                  []string                `json:"cpu,omitempty"`
	Scripts              map[string]string       `json:"scripts,omitempty"`
	Bin                  json.RawMessage         `json:"bin,omitempty"`
	License              LicenseField            `json:"license,omitempty"`
	Deprecated           FlexString              `json:"deprecated,omitempty"`
	Funding              json.RawMessage         `json:"funding,omitempty"`
}

// HasInstallScript returns true if the version has preinstall, install, or postinstall scripts.
func (v *Version) HasInstallScript() bool {
	if v.Scripts == nil {
		return false
	}
	for _, key := range []string{"preinstall", "install", "postinstall"} {
		if _, ok := v.Scripts[key]; ok {
			return true
		}
	}
	return false
}

// ParseBin parses the bin field which can be a string or a map.
func (v *Version) ParseBin() map[string]string {
	if v.Bin == nil {
		return nil
	}
	// Try map first
	var m map[string]string
	if err := json.Unmarshal(v.Bin, &m); err == nil {
		return m
	}
	// Try string (shorthand: binary name = package name)
	var s string
	if err := json.Unmarshal(v.Bin, &s); err == nil {
		return map[string]string{v.Name: s}
	}
	return nil
}

// ParseBundleDeps parses bundleDependencies / bundledDependencies.
// The field can be an array of package names or boolean true (= bundle all deps).
func (v *Version) ParseBundleDeps() map[string]bool {
	raw := v.BundleDependencies
	if raw == nil {
		raw = v.BundledDependencies
	}
	if raw == nil {
		return nil
	}
	// Try array of strings (most common).
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil && len(names) > 0 {
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	// Try boolean true (= bundle all deps).
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil && b {
		m := make(map[string]bool, len(v.Dependencies))
		for name := range v.Dependencies {
			m[name] = true
		}
		return m
	}
	return nil
}

// PeerMeta holds metadata about a peer dependency.
type PeerMeta struct {
	Optional bool `json:"optional,omitempty"`
}

// Dist holds distribution info for a package version.
type Dist struct {
	Integrity    string `json:"integrity"`
	Shasum       string `json:"shasum"`
	Tarball      string `json:"tarball"`
	UnpackedSize int64  `json:"unpackedSize,omitempty"`
	FileCount    int64  `json:"fileCount,omitempty"`
}
