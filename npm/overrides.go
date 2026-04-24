package npm

import (
	"encoding/json"

	"github.com/jumoel/locksmith/ecosystem"
)

// ParseNpmOverrides parses npm's nested overrides format.
// rootDeps maps dependency names to their declared constraints in the root
// package.json, used to resolve "$dependency" references.
func ParseNpmOverrides(raw json.RawMessage, rootDeps map[string]string) (*ecosystem.OverrideSet, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var overrides map[string]json.RawMessage
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return nil, err
	}

	var rules []ecosystem.OverrideRule
	for pkg, val := range overrides {
		rule, err := parseNpmOverrideEntry(pkg, val, rootDeps)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, nil
	}
	return &ecosystem.OverrideSet{Rules: rules}, nil
}

func parseNpmOverrideEntry(pkg string, val json.RawMessage, rootDeps map[string]string) (ecosystem.OverrideRule, error) {
	// Try string value first (leaf override).
	var version string
	if err := json.Unmarshal(val, &version); err == nil {
		// Handle $dependency references.
		if len(version) > 0 && version[0] == '$' {
			depName := version[1:]
			if constraint, ok := rootDeps[depName]; ok {
				version = constraint
			}
		}
		return ecosystem.OverrideRule{Package: pkg, Version: version}, nil
	}

	// Object value - nested overrides scoped to this package's deps.
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(val, &nested); err != nil {
		return ecosystem.OverrideRule{}, err
	}

	var children []ecosystem.OverrideRule
	for childPkg, childVal := range nested {
		child, err := parseNpmOverrideEntry(childPkg, childVal, rootDeps)
		if err != nil {
			return ecosystem.OverrideRule{}, err
		}
		children = append(children, child)
	}

	return ecosystem.OverrideRule{Package: pkg, Children: children}, nil
}

// ParsePnpmOverrides parses pnpm's flat overrides format.
// Keys can be plain names ("foo"), parent-scoped ("bar>foo"),
// or version-scoped ("foo@^2").
func ParsePnpmOverrides(raw json.RawMessage) (*ecosystem.OverrideSet, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var overrides map[string]string
	if err := json.Unmarshal(raw, &overrides); err != nil {
		return nil, err
	}

	var rules []ecosystem.OverrideRule
	for selector, version := range overrides {
		rule := parsePnpmSelector(selector, version)
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, nil
	}
	return &ecosystem.OverrideSet{Rules: rules}, nil
}

func parsePnpmSelector(selector, version string) ecosystem.OverrideRule {
	// Parent selector: "bar>foo"
	for i := 0; i < len(selector); i++ {
		if selector[i] == '>' {
			parent := selector[:i]
			pkg := selector[i+1:]
			// Strip version selector from parent if present (e.g., "bar@^2>foo").
			if atIdx := lastIndexByte(parent, '@'); atIdx > 0 {
				parent = parent[:atIdx]
			}
			// Strip version selector from pkg if present.
			if atIdx := lastIndexByte(pkg, '@'); atIdx > 0 {
				pkg = pkg[:atIdx]
			}
			return ecosystem.OverrideRule{Package: pkg, Version: version, Parent: parent}
		}
	}

	// Version selector: "foo@^2" - strip the version part, treat as global.
	pkg := selector
	if atIdx := lastIndexByte(selector, '@'); atIdx > 0 {
		pkg = selector[:atIdx]
	}

	return ecosystem.OverrideRule{Package: pkg, Version: version}
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// ParseYarnResolutions parses yarn's resolutions format.
// Keys can be plain names ("foo"), glob patterns ("**/foo"),
// or parent-scoped ("bar/foo", "bar/**/foo").
func ParseYarnResolutions(raw json.RawMessage) (*ecosystem.OverrideSet, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var resolutions map[string]string
	if err := json.Unmarshal(raw, &resolutions); err != nil {
		return nil, err
	}

	var rules []ecosystem.OverrideRule
	for pattern, version := range resolutions {
		rule := parseYarnPattern(pattern, version)
		rules = append(rules, rule)
	}

	if len(rules) == 0 {
		return nil, nil
	}
	return &ecosystem.OverrideSet{Rules: rules}, nil
}

func parseYarnPattern(pattern, version string) ecosystem.OverrideRule {
	// Strip leading "**/" - it's a global override.
	if len(pattern) > 3 && pattern[:3] == "**/" {
		return ecosystem.OverrideRule{Package: pattern[3:], Version: version}
	}

	// Check for parent scoping: "bar/foo" or "bar/**/foo"
	slashIdx := -1
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '/' {
			slashIdx = i
			break
		}
	}
	if slashIdx > 0 {
		parent := pattern[:slashIdx]
		rest := pattern[slashIdx+1:]
		// "bar/**/foo" - strip the **/ prefix.
		if len(rest) > 3 && rest[:3] == "**/" {
			rest = rest[3:]
		}
		return ecosystem.OverrideRule{Package: rest, Version: version, Parent: parent}
	}

	// Plain name - global override.
	return ecosystem.OverrideRule{Package: pattern, Version: version}
}
