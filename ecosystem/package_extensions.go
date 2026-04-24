package ecosystem

import (
	"github.com/jumoel/locksmith/internal/semver"
)

// PackageExtension describes additional deps to merge into a package's metadata.
type PackageExtension struct {
	Name         string            // package name
	VersionRange string            // semver range (empty = all versions)
	Dependencies map[string]string // extra regular deps
	PeerDeps     map[string]string // extra peer deps
}

// PackageExtensionSet holds parsed packageExtensions.
type PackageExtensionSet struct {
	Extensions []PackageExtension
}

// Apply merges matching extensions into the given metadata.
func (pes *PackageExtensionSet) Apply(meta *VersionMetadata) {
	if pes == nil || meta == nil {
		return
	}
	for _, ext := range pes.Extensions {
		if ext.Name != meta.Name {
			continue
		}
		// Check version range if specified.
		if ext.VersionRange != "" {
			c, err := semver.ParseConstraint(ext.VersionRange)
			if err != nil {
				continue
			}
			v, err := semver.Parse(meta.Version)
			if err != nil {
				continue
			}
			if !c.Check(v) {
				continue
			}
		}
		// Merge extra deps.
		if len(ext.Dependencies) > 0 {
			if meta.Dependencies == nil {
				meta.Dependencies = make(map[string]string)
			}
			for k, v := range ext.Dependencies {
				if _, exists := meta.Dependencies[k]; !exists {
					meta.Dependencies[k] = v
				}
			}
		}
		if len(ext.PeerDeps) > 0 {
			if meta.PeerDeps == nil {
				meta.PeerDeps = make(map[string]string)
			}
			for k, v := range ext.PeerDeps {
				if _, exists := meta.PeerDeps[k]; !exists {
					meta.PeerDeps[k] = v
				}
			}
		}
	}
}
