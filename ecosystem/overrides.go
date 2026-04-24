package ecosystem

// OverrideRule represents a single version override directive.
type OverrideRule struct {
	Package  string         // dep name to override (e.g., "foo")
	Version  string         // replacement version (e.g., "1.0.0", "^2.0.0")
	Parent   string         // parent package that must require this dep (empty = global)
	Children []OverrideRule // nested overrides (npm only)
}

// OverrideSet holds parsed overrides for a project.
type OverrideSet struct {
	Rules []OverrideRule
}

// FindOverride returns the replacement version if any rule matches
// the given package name with the given parent chain.
// Returns ("", false) if no override applies.
func (os *OverrideSet) FindOverride(pkg string, parents []string) (string, bool) {
	if os == nil {
		return "", false
	}
	for _, rule := range os.Rules {
		if v, ok := matchRule(rule, pkg, parents); ok {
			return v, true
		}
	}
	return "", false
}

func matchRule(rule OverrideRule, pkg string, parents []string) (string, bool) {
	// Check children first (npm nested overrides).
	if len(rule.Children) > 0 {
		// This rule scopes overrides to deps of rule.Package.
		// Check if rule.Package is in the parent chain.
		found := false
		for _, p := range parents {
			if p == rule.Package {
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
		for _, child := range rule.Children {
			if v, ok := matchRule(child, pkg, parents); ok {
				return v, true
			}
		}
		return "", false
	}

	// Leaf rule: direct override.
	if rule.Package != pkg {
		return "", false
	}

	// Check parent constraint.
	if rule.Parent != "" {
		found := false
		for _, p := range parents {
			if p == rule.Parent {
				found = true
				break
			}
		}
		if !found {
			return "", false
		}
	}

	return rule.Version, true
}
