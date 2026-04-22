package yarn

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
)

// YarnClassicFormatter produces yarn.lock v1 (classic) output.
// The classic format is a custom text format (not JSON or YAML) with two-space
// indentation and entries sorted alphabetically by constraint key.
type YarnClassicFormatter struct{}

// NewYarnClassicFormatter returns a new classic yarn.lock formatter.
func NewYarnClassicFormatter() *YarnClassicFormatter {
	return &YarnClassicFormatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since yarn lockfiles require resolution
// metadata that only ResolveResult provides.
func (f *YarnClassicFormatter) Format(graph *ecosystem.Graph, project *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn classic lockfile generation")
}

// classicEntry holds all the data needed to write a single yarn.lock entry.
type classicEntry struct {
	sortKey      string   // first constraint, used for sorting
	constraints  []string // all "name@constraint" keys that resolve to this version
	version      string
	resolvedURL  string
	integrity    string
	dependencies map[string]string // dep name -> constraint (NOT resolved version)
}

// FormatFromResult produces yarn.lock v1 bytes from a resolve result.
// Output is deterministic: entries are sorted alphabetically by their first
// constraint key, and multiple constraints resolving to the same version
// are grouped into a single entry.
func (f *YarnClassicFormatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	// Build a map from "name@version" to all constraints that resolve to it.
	// We need to walk every edge in the graph to discover which constraints
	// map to which resolved versions.
	type constraintInfo struct {
		name       string
		constraint string
	}

	// Collect all edges from every node (including root).
	versionConstraints := make(map[string][]constraintInfo) // "name@version" -> constraints

	// classicEdgeKey returns the key that matches the Packages map.
	// For registry deps: "name@version". For non-registry deps (file:, git, etc.):
	// "name@constraint" because the resolver uses the constraint as the key suffix.
	classicEdgeKey := func(edge *ecosystem.Edge) string {
		if isNonRegistryConstraint(edge.Constraint) {
			// For tarball URL deps that were fully resolved, the Packages map
			// uses the resolved name@version key, not the constraint URL.
			if strings.HasPrefix(edge.Constraint, "https://") && edge.Target.Version != "0.0.0-local" {
				return edge.Target.Name + "@" + edge.Target.Version
			}
			// Non-registry deps: the Packages map key uses the original dep name
			// (not target name) because the resolver stores them under
			// actualName@constraint BEFORE updating actualName from GitHub API.
			return edge.Name + "@" + edge.Constraint
		}
		return edge.Target.Name + "@" + edge.Target.Version
	}

	wsNames := workspaceMemberNamesYarn(project)

	// Walk root edges.
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil {
				continue
			}
			// Skip workspace member edges.
			if strings.HasPrefix(edge.Constraint, "workspace:") {
				continue
			}
			key := classicEdgeKey(edge)
			versionConstraints[key] = append(versionConstraints[key], constraintInfo{
				name:       edge.Name,
				constraint: edge.Constraint,
			})
		}

		// Walk workspace member node edges to collect constraints for their deps.
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil || !strings.HasPrefix(edge.Constraint, "workspace:") {
				continue
			}
			for _, depEdge := range edge.Target.Dependencies {
				if depEdge.Target == nil {
					continue
				}
				// Skip edges to other workspace members.
				if wsNames[depEdge.Target.Name] {
					continue
				}
				key := classicEdgeKey(depEdge)
				versionConstraints[key] = append(versionConstraints[key], constraintInfo{
					name:       depEdge.Name,
					constraint: depEdge.Constraint,
				})
			}
		}
	}

	// Walk all package edges.
	for _, pkg := range result.Packages {
		if pkg.Node == nil {
			continue
		}
		for _, edge := range pkg.Node.Dependencies {
			if edge.Target == nil {
				continue
			}
			key := classicEdgeKey(edge)
			versionConstraints[key] = append(versionConstraints[key], constraintInfo{
				name:       edge.Name,
				constraint: edge.Constraint,
			})
		}
	}

	// Build entries from packages, merging constraints.
	entries := make(map[string]*classicEntry) // keyed by "name@version"

	for key, pkg := range result.Packages {
		if pkg.Node == nil {
			continue
		}

		// Deduplicate constraints for this package.
		constraintSet := make(map[string]bool)
		var constraints []string

		if infos, ok := versionConstraints[key]; ok {
			for _, info := range infos {
				cKey := formatConstraintKey(info.name, info.constraint)
				if !constraintSet[cKey] {
					constraintSet[cKey] = true
					constraints = append(constraints, cKey)
				}
			}
		}

		// If no constraints were found (shouldn't happen in a well-formed graph),
		// fall back to "name@version".
		if len(constraints) == 0 {
			constraints = []string{formatConstraintKey(pkg.Node.Name, pkg.Node.Version)}
		}

		sort.Strings(constraints)

		// Build the dependencies sub-section. Yarn classic writes the original
		// constraint string for each dependency, not the resolved version.
		depConstraints := make(map[string]string)
		for _, edge := range pkg.Node.Dependencies {
			depConstraints[edge.Name] = edge.Constraint
		}

		entries[key] = &classicEntry{
			sortKey:      constraints[0],
			constraints:  constraints,
			version:      pkg.Node.Version,
			resolvedURL:  resolvedURLWithShasum(pkg.Node.TarballURL, pkg.Node.Shasum),
			integrity:    pkg.Node.Integrity,
			dependencies: depConstraints,
		}
	}

	// Add workspace member entries.
	for _, member := range project.Workspaces {
		if member.Spec == nil || member.Spec.Name == "" {
			continue
		}
		// Workspace member constraint key: "@scope/name@0.0.0"
		constraintKey := formatConstraintKey(member.Spec.Name, member.Spec.Version)

		// Build dependencies: only non-workspace registry deps.
		depConstraints := make(map[string]string)
		for _, d := range member.Spec.Dependencies {
			if strings.HasPrefix(d.Constraint, "workspace:") {
				continue
			}
			depConstraints[d.Name] = d.Constraint
		}

		entries[constraintKey] = &classicEntry{
			sortKey:      constraintKey,
			constraints:  []string{constraintKey},
			version:      member.Spec.Version,
			resolvedURL:  "",
			integrity:    "",
			dependencies: depConstraints,
		}
	}

	// Sort entries alphabetically by their first constraint key.
	sortedEntries := make([]*classicEntry, 0, len(entries))
	for _, e := range entries {
		sortedEntries = append(sortedEntries, e)
	}
	sort.Slice(sortedEntries, func(i, j int) bool {
		return sortedEntries[i].sortKey < sortedEntries[j].sortKey
	})

	// Write the lockfile.
	var buf bytes.Buffer

	buf.WriteString("# THIS IS AN AUTOGENERATED FILE. DO NOT EDIT THIS FILE DIRECTLY.\n")
	buf.WriteString("# yarn lockfile v1\n")
	buf.WriteString("\n")

	for i, entry := range sortedEntries {
		if i > 0 {
			buf.WriteString("\n")
		}

		// Write constraint header line.
		// Format: key1, key2, key3:
		// Scoped packages get quoted, unscoped do not (but quoting all is also valid).
		var headerParts []string
		for _, c := range entry.constraints {
			headerParts = append(headerParts, quoteConstraintKey(c))
		}
		buf.WriteString(strings.Join(headerParts, ", "))
		buf.WriteString(":\n")

		// version
		fmt.Fprintf(&buf, "  version %q\n", entry.version)

		// resolved
		fmt.Fprintf(&buf, "  resolved %q\n", entry.resolvedURL)

		// integrity (omit for deps without integrity hash, e.g., git deps)
		if entry.integrity != "" {
			fmt.Fprintf(&buf, "  integrity %s\n", entry.integrity)
		}

		// dependencies
		if len(entry.dependencies) > 0 {
			buf.WriteString("  dependencies:\n")
			depNames := maputil.SortedKeys(entry.dependencies)
			for _, depName := range depNames {
				depConstraint := entry.dependencies[depName]
				quotedName := depName
				if strings.HasPrefix(depName, "@") {
					quotedName = fmt.Sprintf("%q", depName)
				}
				fmt.Fprintf(&buf, "    %s %q\n", quotedName, depConstraint)
			}
		}
	}

	return buf.Bytes(), nil
}

// resolvedURLWithShasum appends the #shasum fragment to a tarball URL.
// Yarn classic uses this format: "https://registry.yarnpkg.com/pkg/-/pkg-1.0.0.tgz#sha1hash"
func resolvedURLWithShasum(url, shasum string) string {
	if shasum != "" && url != "" {
		return url + "#" + shasum
	}
	return url
}

// formatConstraintKey builds the "name@constraint" key for a yarn.lock entry.
func formatConstraintKey(name, constraint string) string {
	return name + "@" + constraint
}

// quoteConstraintKey wraps a constraint key in quotes if it contains
// characters that yarn classic's parser can't handle unquoted.
// Scoped packages (@scope/name@^1.0.0) and keys with spaces or special
// characters (e.g., "^3.0.0 || ^4.0.0") must be quoted. Rather than
// maintaining a fragile allow-list, we quote any key that contains @, spaces,
// pipes, or other non-trivial characters.
// isNonRegistryConstraint returns true if the constraint is a non-registry specifier.
func isNonRegistryConstraint(constraint string) bool {
	prefixes := []string{"file:", "link:", "git+", "git://", "git@", "github:", "http://", "https://"}
	for _, p := range prefixes {
		if strings.HasPrefix(constraint, p) {
			return true
		}
	}
	return false
}

func quoteConstraintKey(key string) string {
	if strings.HasPrefix(key, "@") || strings.ContainsAny(key, " |><=!:") {
		return fmt.Sprintf("%q", key)
	}
	return key
}
