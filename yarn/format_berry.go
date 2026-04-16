package yarn

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
	"github.com/jumoel/locksmith/internal/semver"
)

// YarnBerryV4Formatter produces yarn.lock output in yarn berry v4 format (yarn 2.0 initial stable).
type YarnBerryV4Formatter struct{}

func NewYarnBerryV4Formatter() *YarnBerryV4Formatter { return &YarnBerryV4Formatter{} }

func (f *YarnBerryV4Formatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn berry lockfile generation")
}

func (f *YarnBerryV4Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	return formatBerryWithConfig(result, project, berryConfig{
		MetadataVersion: 4, CacheKey: "7", ChecksumPrefix: "", IncludeRoot: true, SkipChecksum: true,
	})
}

// YarnBerryV5Formatter produces yarn.lock output in yarn berry v5 format (yarn 3.1.0 only).
type YarnBerryV5Formatter struct{}

func NewYarnBerryV5Formatter() *YarnBerryV5Formatter { return &YarnBerryV5Formatter{} }

func (f *YarnBerryV5Formatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn berry lockfile generation")
}

func (f *YarnBerryV5Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	return formatBerryWithConfig(result, project, berryConfig{
		MetadataVersion: 5, CacheKey: "8", ChecksumPrefix: "", IncludeRoot: true, SkipChecksum: true,
	})
}

// YarnBerryV6Formatter produces yarn.lock output in yarn berry v6 format (yarn 3.0-3.4).
type YarnBerryV6Formatter struct{}

func NewYarnBerryV6Formatter() *YarnBerryV6Formatter { return &YarnBerryV6Formatter{} }

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since yarn berry lockfiles require resolution
// metadata that only ResolveResult provides.
func (f *YarnBerryV6Formatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn berry lockfile generation")
}

// FormatFromResult produces yarn.lock v6 bytes from a resolve result.
func (f *YarnBerryV6Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	return formatBerryWithConfig(result, project, berryConfig{
		MetadataVersion: 6, CacheKey: "10", ChecksumPrefix: "", IncludeRoot: true,
	})
}

// YarnBerryV8Formatter produces yarn.lock output in yarn berry v8 format (yarn 4).
type YarnBerryV8Formatter struct{}

// NewYarnBerryV8Formatter returns a new yarn berry v8 formatter.
func NewYarnBerryV8Formatter() *YarnBerryV8Formatter {
	return &YarnBerryV8Formatter{}
}

// Format implements ecosystem.Formatter but returns an error directing callers
// to use FormatFromResult instead, since yarn berry lockfiles require resolution
// metadata that only ResolveResult provides.
func (f *YarnBerryV8Formatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn berry lockfile generation")
}

// FormatFromResult produces yarn.lock v8 bytes from a resolve result.
func (f *YarnBerryV8Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	return formatBerryWithConfig(result, project, berryConfig{
		MetadataVersion: 8, CacheKey: "10c0", ChecksumPrefix: "10/", IncludeRoot: true, RootDepsNpmPrefix: true,
	})
}

// berryEntry holds the data for a single yarn berry lockfile entry.
// Multiple constraints can map to the same resolved package.
type berryEntry struct {
	// constraints are the "name@npm:constraint" strings that resolve to this package.
	constraints []string
	pkg         *ResolvedPackage
}

// berryConfig holds format-specific settings that differ between berry versions.
type berryConfig struct {
	MetadataVersion    int
	CacheKey           string
	ChecksumPrefix     string // "10/" for v8, "" for v5-v6
	IncludeRoot        bool   // true for yarn berry (adds workspace root entry)
	SkipChecksum       bool   // v4/v5: omit checksums (yarn 2/3.1 computes cache-specific hashes)
	RootDepsNpmPrefix  bool   // v8: root deps use "npm:constraint" format
}

func formatBerryWithConfig(result *ResolveResult, project *ecosystem.ProjectSpec, cfg berryConfig) ([]byte, error) {
	// Build entries: group constraints by resolved "name@version".
	// In a flat resolution, each constraint on a given package name resolves to
	// the same version, so we collect all constraints that point to each package.
	constraintsByKey := buildConstraintMap(result, project)

	// Build sorted entries. Skip packages with no constraints (e.g., filtered
	// out by platform but still in the Packages map).
	entries := make([]*berryEntry, 0, len(result.Packages))
	for key, pkg := range result.Packages {
		constraints := deduplicateConstraints(constraintsByKey[key])
		if len(constraints) == 0 {
			continue
		}
		sort.Strings(constraints)
		entries = append(entries, &berryEntry{
			constraints: constraints,
			pkg:         pkg,
		})
	}

	// Sort entries by their first constraint for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].constraints[0] < entries[j].constraints[0]
	})

	var b strings.Builder

	// Write standard yarn berry header comment.
	b.WriteString("# This file is generated by running \"yarn install\" inside your project.\n")
	b.WriteString("# Manual changes might be lost - proceed with caution!\n")
	b.WriteString("\n")

	// Write metadata header. Yarn v6 (yarn 3) omits cacheKey for empty projects.
	// Yarn v8 (yarn 4) always includes it.
	b.WriteString("__metadata:\n")
	b.WriteString(fmt.Sprintf("  version: %d\n", cfg.MetadataVersion))
	if len(result.Packages) > 0 || cfg.RootDepsNpmPrefix {
		b.WriteString(fmt.Sprintf("  cacheKey: %s\n", cfg.CacheKey))
	}

	// Build workspace root entry so it sorts with the rest.
	type writeFunc func(b *strings.Builder)
	type sortableEntry struct {
		sortKey string
		write   writeFunc
	}
	var allEntries []sortableEntry

	for _, entry := range entries {
		e := entry // capture
		allEntries = append(allEntries, sortableEntry{
			sortKey: e.constraints[0],
			write: func(b *strings.Builder) {
				writeEntryKey(b, e.constraints)
				writeEntryBody(b, e.pkg, e.constraints[0], cfg.ChecksumPrefix, cfg.SkipChecksum)
			},
		})
	}

	if cfg.IncludeRoot && project.Name != "" {
		rootConstraint := fmt.Sprintf("%s@workspace:.", project.Name)
		allEntries = append(allEntries, sortableEntry{
			sortKey: rootConstraint,
			write: func(b *strings.Builder) {
				b.WriteString(fmt.Sprintf("%q:\n", rootConstraint))
				b.WriteString(fmt.Sprintf("  version: %s\n", "0.0.0-use.local"))
				b.WriteString(fmt.Sprintf("  resolution: \"%s@workspace:.\"\n", project.Name))
				// Collect non-peer deps (deduplicated via map).
				depMap := make(map[string]string)
				for _, d := range project.Dependencies {
					if d.Type == ecosystem.DepPeer {
						continue
					}
					depMap[d.Name] = d.Constraint
				}
				if len(depMap) > 0 {
					b.WriteString("  dependencies:\n")
					depNames := make([]string, 0, len(depMap))
					for name := range depMap {
						depNames = append(depNames, name)
					}
					sort.Strings(depNames)
					for _, name := range depNames {
						yamlName := name
						if strings.HasPrefix(name, "@") {
							yamlName = fmt.Sprintf("%q", name)
						}
						constraint := depMap[name]
						if cfg.RootDepsNpmPrefix {
							// Don't add npm: prefix for non-registry constraints or npm: aliases.
							if strings.HasPrefix(constraint, "npm:") || isNonRegistryBerryConstraint(constraint) {
								b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, constraint))
							} else {
								b.WriteString(fmt.Sprintf("    %s: \"npm:%s\"\n", yamlName, constraint))
							}
						} else {
							// Quote values containing YAML special chars.
							// * is a YAML alias, : is a mapping, bare numbers/bools are types.
							if strings.ContainsAny(constraint, ":*") || constraint == "true" || constraint == "false" || constraint == "null" {
								b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, constraint))
							} else {
								b.WriteString(fmt.Sprintf("    %s: %s\n", yamlName, constraint))
							}
						}
					}
				}
				// dependenciesMeta: mark optional deps.
				var optDeps []string
				for _, d := range project.Dependencies {
					if d.Type == ecosystem.DepOptional {
						optDeps = append(optDeps, d.Name)
					}
				}
				if len(optDeps) > 0 {
					sort.Strings(optDeps)
					b.WriteString("  dependenciesMeta:\n")
					for _, name := range optDeps {
						yamlName := name
						if strings.HasPrefix(name, "@") {
							yamlName = fmt.Sprintf("%q", name)
						}
						b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
						b.WriteString("      optional: true\n")
					}
				}
				// peerDependencies and peerDependenciesMeta from package.json.
				g := ecosystem.GroupDependenciesByType(project.Dependencies)
				if len(g.Peer) > 0 {
					peerNames := make([]string, 0, len(g.Peer))
					for name := range g.Peer {
						peerNames = append(peerNames, name)
					}
					sort.Strings(peerNames)
					b.WriteString("  peerDependencies:\n")
					for _, name := range peerNames {
						yamlName := name
						if strings.HasPrefix(name, "@") {
							yamlName = fmt.Sprintf("%q", name)
						}
						b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, g.Peer[name]))
					}
					// Mark all peers as optional in peerDependenciesMeta.
					// TODO: track actual peerDependenciesMeta from package.json
					// to distinguish required vs optional peers.
					b.WriteString("  peerDependenciesMeta:\n")
					for _, name := range peerNames {
						yamlName := name
						if strings.HasPrefix(name, "@") {
							yamlName = fmt.Sprintf("%q", name)
						}
						b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
						b.WriteString("      optional: true\n")
					}
				}
				b.WriteString("  languageName: unknown\n")
				b.WriteString("  linkType: soft\n")
			},
		})
	}

	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].sortKey < allEntries[j].sortKey
	})

	for _, e := range allEntries {
		b.WriteByte('\n')
		e.write(&b)
	}

	return []byte(b.String()), nil
}

// buildConstraintMap collects all "name@npm:constraint" strings that resolve
// to each "name@version" key in the result.
func buildConstraintMap(result *ResolveResult, project *ecosystem.ProjectSpec) map[string][]string {
	m := make(map[string][]string)

	// Constraints from the root project's declared dependencies.
	// Use root graph edges to find the exact resolved version for each dep,
	// not a name-based scan which would match multiple versions incorrectly.
	if result.Graph != nil && result.Graph.Root != nil {
		for _, edge := range result.Graph.Root.Dependencies {
			if edge.Target == nil {
				continue
			}
			// Skip peer dep edges from root - yarn berry doesn't include
			// optional peer deps in the lockfile.
			if edge.Type == ecosystem.DepPeer {
				continue
			}
			// For non-registry deps, use edge.Name (dep alias) + constraint as the key
			// to match the Packages map (which stores git deps under depName@constraint).
			constraint := edge.Constraint
			var targetKey string
			if isNonRegistryBerryConstraint(constraint) {
				targetKey = edge.Name + "@" + constraint
			} else {
				targetKey = edge.Target.Name + "@" + edge.Target.Version
			}
			// Don't add npm: prefix for non-registry constraints.
			if strings.HasPrefix(constraint, "npm:") {
				descriptor := fmt.Sprintf("%s@%s", edge.Name, constraint)
				m[targetKey] = appendUnique(m[targetKey], descriptor)
			} else if isNonRegistryBerryConstraint(constraint) {
				descriptor := fmt.Sprintf("%s@%s", edge.Name, constraint)
				m[targetKey] = appendUnique(m[targetKey], descriptor)
			} else {
				descriptor := fmt.Sprintf("%s@npm:%s", edge.Name, constraint)
				m[targetKey] = appendUnique(m[targetKey], descriptor)
			}
		}
	}

	// Constraints from transitive dependencies.
	for _, pkg := range result.Packages {
		for _, edge := range pkg.Node.Dependencies {
			if edge.Target == nil {
				continue
			}
			targetKey := edge.Target.Name + "@" + edge.Target.Version
			descriptor := fmt.Sprintf("%s@npm:%s", edge.Name, edge.Constraint)
			m[targetKey] = appendUnique(m[targetKey], descriptor)
		}
	}

	// Note: yarn keeps ALL constraints for a resolved version, even redundant
	// ones. No deduplication needed. If a constraint is missing, it means the
	// edge that produced it was removed (e.g., by platform filtering).

	return m
}

// deduplicateConstraints removes redundant caret constraints.
// When "name@npm:^2.4.0" and "name@npm:^2.8.0" both exist, the less specific
// one (^2.4.0) is removed since the more specific one (^2.8.0) implies it.
func deduplicateConstraints(constraints []string) []string {
	if len(constraints) <= 1 {
		return constraints
	}

	// Group constraints by package name (before @npm:).
	type parsed struct {
		full    string
		name    string
		semver  *semver.Version
	}

	byName := make(map[string][]parsed)
	for _, c := range constraints {
		// Parse "name@npm:^version" format.
		atIdx := strings.Index(c, "@npm:^")
		if atIdx == -1 {
			// Not a caret npm constraint, keep as-is.
			byName[""] = append(byName[""], parsed{full: c})
			continue
		}
		name := c[:atIdx]
		ver := c[atIdx+6:] // after "@npm:^"
		sv, err := semver.Parse(ver)
		if err != nil {
			// Unparseable version, keep as-is.
			byName[""] = append(byName[""], parsed{full: c})
			continue
		}
		byName[name] = append(byName[name], parsed{full: c, name: name, semver: sv})
	}

	var result []string
	for _, group := range byName {
		if len(group) <= 1 || group[0].name == "" {
			for _, p := range group {
				result = append(result, p.full)
			}
			continue
		}
		// Keep only the constraint with the highest version (most specific).
		best := group[0]
		for _, p := range group[1:] {
			if p.semver.GreaterThan(best.semver) {
				best = p
			}
		}
		result = append(result, best.full)
	}

	sort.Strings(result)
	return result
}

// appendUnique appends s to slice only if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// writeEntryKey writes the quoted, comma-joined constraint key line.
// For a single constraint: "name@npm:^1.0.0":
// For multiple: "name@npm:^1.0.0, name@npm:^1.1.0":
func writeEntryKey(b *strings.Builder, constraints []string) {
	quoted := make([]string, len(constraints))
	for i, c := range constraints {
		quoted[i] = fmt.Sprintf("%s", c)
	}
	b.WriteString(fmt.Sprintf("%q:\n", strings.Join(quoted, ", ")))
}

// writeEntryBody writes the indented fields for a single package entry.
func writeEntryBody(b *strings.Builder, pkg *ResolvedPackage, constraintKey string, checksumPrefix string, skipChecksum bool) {
	node := pkg.Node

	b.WriteString(fmt.Sprintf("  version: %s\n", node.Version))

	// Extract the dep name from the constraint key (part before first @).
	// For "git-pkg@github:owner/repo", depName = "git-pkg".
	// For "wrappy@npm:^1.0.0", depName = "wrappy".
	depName := node.Name
	if atIdx := strings.Index(constraintKey, "@"); atIdx > 0 {
		depName = constraintKey[:atIdx]
	}

	// Resolution varies by dep type.
	url := node.TarballURL
	if strings.HasPrefix(url, "git+ssh://") || strings.HasPrefix(url, "git+https://") {
		// Git dep: use dep name + HTTPS URL with commit hash.
		cleanURL := strings.TrimPrefix(url, "git+ssh://git@github.com/")
		cleanURL = strings.TrimPrefix(cleanURL, "git+https://github.com/")
		cleanURL = "https://github.com/" + cleanURL
		cleanURL = strings.Replace(cleanURL, "#", "#commit=", 1)
		b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", depName, cleanURL))
	} else if strings.HasPrefix(url, "file:") {
		b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", depName, url))
	} else if strings.HasPrefix(url, "https://") && strings.HasSuffix(url, ".tgz") {
		// Tarball URL dep: use dep name + URL.
		b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", depName, url))
	} else {
		b.WriteString(fmt.Sprintf("  resolution: \"%s@npm:%s\"\n", node.Name, node.Version))
	}

	// Dependencies (sorted by name).
	if len(pkg.Dependencies) > 0 {
		b.WriteString("  dependencies:\n")
		depNames := maputil.SortedKeys(pkg.Dependencies)

		for _, name := range depNames {
			// Find the constraint used for this dependency from the node's edges.
			constraint := findConstraint(node, name)
			// Quote scoped package names (starting with @) for valid YAML.
			yamlName := name
			if strings.HasPrefix(name, "@") {
				yamlName = fmt.Sprintf("%q", name)
			}
			b.WriteString(fmt.Sprintf("    %s: \"npm:%s\"\n", yamlName, constraint))
		}
	}

	// Checksum: for v6/v8, emit sha512 hex (yarn 3.2+/4 don't validate).
	// For v4/v5, omit entirely - yarn 2/3.1 compute cache-specific hashes
	// that can't be derived from registry data. Yarn fills them on install.
	if !skipChecksum && node.Integrity != "" {
		checksum := integrityToYarnChecksum(node.Integrity, checksumPrefix)
		if checksum != "" {
			b.WriteString(fmt.Sprintf("  checksum: %s\n", checksum))
		}
	}

	b.WriteString("  languageName: node\n")
	b.WriteString("  linkType: hard\n")
}

// findConstraint looks up the original constraint for a dependency by name
// from the node's edges.
func findConstraint(node *ecosystem.Node, depName string) string {
	for _, edge := range node.Dependencies {
		if edge.Name == depName {
			return edge.Constraint
		}
	}
	// Fallback: should not happen for well-formed data.
	return "*"
}

// integrityToYarnChecksum converts an SRI integrity hash (e.g., "sha512-abc...")
// to yarn berry's checksum format ("10/{hex}").
// The "10" prefix is yarn's internal hash algorithm version identifier.
func integrityToYarnChecksum(integrity string, prefix string) string {
	// Expected format: "sha512-{base64}"
	if !strings.HasPrefix(integrity, "sha512-") {
		return ""
	}

	b64 := strings.TrimPrefix(integrity, "sha512-")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Try URL-safe base64 and RawStdEncoding as fallbacks.
		decoded, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return ""
		}
	}

	return prefix + hex.EncodeToString(decoded)
}

// isNonRegistryBerryConstraint checks if a constraint is a non-registry specifier.
func isNonRegistryBerryConstraint(constraint string) bool {
	prefixes := []string{"file:", "git+", "github:", "http://", "https://", "link:", "portal:", "patch:", "exec:"}
	for _, p := range prefixes {
		if strings.HasPrefix(constraint, p) {
			return true
		}
	}
	return false
}
