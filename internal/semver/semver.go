package semver

import (
	"strings"

	masterminds "github.com/Masterminds/semver/v3"
)

// Version wraps a parsed semver version.
type Version struct {
	inner *masterminds.Version
}

// Parse parses a version string. Tolerant of npm quirks like "v" prefix.
func Parse(s string) (*Version, error) {
	s = strings.TrimPrefix(s, "v")
	v, err := masterminds.StrictNewVersion(s)
	if err != nil {
		// Fall back to lenient parsing for non-standard versions
		v, err = masterminds.NewVersion(s)
		if err != nil {
			return nil, err
		}
	}
	return &Version{inner: v}, nil
}

// String returns the version string.
func (v *Version) String() string {
	return v.inner.String()
}

// Original returns the original version string before normalization.
func (v *Version) Original() string {
	return v.inner.Original()
}

// LessThan returns true if v < other.
func (v *Version) LessThan(other *Version) bool {
	return v.inner.LessThan(other.inner)
}

// GreaterThan returns true if v > other.
func (v *Version) GreaterThan(other *Version) bool {
	return v.inner.GreaterThan(other.inner)
}

// Equal returns true if v == other.
func (v *Version) Equal(other *Version) bool {
	return v.inner.Equal(other.inner)
}

// Prerelease returns the prerelease string.
func (v *Version) Prerelease() string {
	return v.inner.Prerelease()
}

// Constraint wraps a parsed semver constraint (range).
type Constraint struct {
	inner *masterminds.Constraints
	raw   string
}

// ParseConstraint parses an npm-style version constraint.
// Handles npm-specific patterns like "", "*", "1.x", "1.x.x".
func ParseConstraint(s string) (*Constraint, error) {
	raw := s

	// Normalize npm-specific patterns
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || s == "latest" {
		s = ">=0.0.0"
	}

	// Handle x-ranges: "1.x.x" -> "1.x", "1.*.*" -> "1.*"
	// Masterminds handles these natively, but normalize doubled wildcards.
	s = strings.ReplaceAll(s, ".x.x", ".x")
	s = strings.ReplaceAll(s, ".*.*", ".*")

	// Masterminds supports || natively, so no transformation needed.

	c, err := masterminds.NewConstraint(s)
	if err != nil {
		return nil, err
	}

	return &Constraint{inner: c, raw: raw}, nil
}

// Check returns true if the version satisfies the constraint.
// Follows npm semantics: pre-release versions only match if the constraint
// explicitly includes a pre-release on the same major.minor.patch.
func (c *Constraint) Check(v *Version) bool {
	return c.inner.Check(v.inner)
}

// String returns the original constraint string.
func (c *Constraint) String() string {
	return c.raw
}

// MaxSatisfying returns the highest version from the list that satisfies
// the constraint. Returns nil if no version satisfies it.
func MaxSatisfying(versions []*Version, constraint *Constraint) *Version {
	var best *Version
	for _, v := range versions {
		if constraint.Check(v) {
			if best == nil || v.GreaterThan(best) {
				best = v
			}
		}
	}
	return best
}
