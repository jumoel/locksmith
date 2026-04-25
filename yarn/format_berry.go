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
	ProjectName        string // for portal: locator suffix on file: deps
}

func formatBerryWithConfig(result *ResolveResult, project *ecosystem.ProjectSpec, cfg berryConfig) ([]byte, error) {
	cfg.ProjectName = project.Name
	// Build entries: group constraints by resolved "name@version".
	// In a flat resolution, each constraint on a given package name resolves to
	// the same version, so we collect all constraints that point to each package.
	constraintsByKey := buildConstraintMap(result, project)

	// Build sorted entries. Skip packages with no constraints (e.g., filtered
	// out by platform but still in the Packages map).
	entries := make([]*berryEntry, 0, len(result.Packages))
	for key, pkg := range result.Packages {
		constraints := constraintsByKey[key]
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
				writeEntryBody(b, e.pkg, e.constraints[0], cfg)
			},
		})
	}

	if cfg.IncludeRoot && project.Name != "" {
		rootConstraint := fmt.Sprintf("%s@workspace:.", project.Name)
		allEntries = append(allEntries, sortableEntry{
			sortKey: rootConstraint,
			write: func(b *strings.Builder) {
				writeBerryWorkspaceEntry(b, project.Name, ".", project.Dependencies, project.PeerDepsMeta, cfg)
			},
		})

		// Collect cross-workspace dependency constraints per member.
		// When lib-b depends on lib-a via "workspace:*", the lib-a entry
		// needs "workspace:*" as an additional constraint key.
		wsExtraConstraints := make(map[string][]string) // member name -> extra constraints
		for _, member := range project.Workspaces {
			if member.Spec == nil {
				continue
			}
			for _, dep := range member.Spec.Dependencies {
				if strings.HasPrefix(dep.Constraint, "workspace:") {
					wsExtraConstraints[dep.Name] = append(wsExtraConstraints[dep.Name], dep.Constraint)
				}
			}
		}

		// Add workspace member entries.
		for _, member := range project.Workspaces {
			if member.Spec == nil || member.Spec.Name == "" {
				continue
			}
			m := member // capture for closure
			extras := wsExtraConstraints[m.Spec.Name]
			memberConstraint := fmt.Sprintf("%s@workspace:%s", m.Spec.Name, m.RelPath)
			allEntries = append(allEntries, sortableEntry{
				sortKey: memberConstraint,
				write: func(b *strings.Builder) {
					writeBerryWorkspaceEntry(b, m.Spec.Name, m.RelPath, m.Spec.Dependencies, m.Spec.PeerDepsMeta, cfg, extras...)
				},
			})
		}
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

// workspaceMemberNamesYarn returns the set of package names that are workspace members.
func workspaceMemberNamesYarn(project *ecosystem.ProjectSpec) map[string]bool {
	names := make(map[string]bool)
	for _, m := range project.Workspaces {
		if m.Spec != nil && m.Spec.Name != "" {
			names[m.Spec.Name] = true
		}
	}
	return names
}

// writeBerryWorkspaceEntry writes a workspace entry (root or member) to the
// yarn berry lockfile. workspacePath is "." for root or the relative path
// (e.g., "packages/lib-a") for members. extraConstraints are additional
// constraint keys from cross-workspace deps (e.g., "workspace:*").
func writeBerryWorkspaceEntry(b *strings.Builder, name, workspacePath string, deps []ecosystem.DeclaredDep, peerDepsMeta map[string]ecosystem.PeerDepMeta, cfg berryConfig, extraConstraints ...string) {
	constraint := fmt.Sprintf("%s@workspace:%s", name, workspacePath)
	if len(extraConstraints) > 0 {
		// Combine the primary constraint with extra constraint keys.
		// e.g., "@workspace/lib-a@workspace:*, @workspace/lib-a@workspace:packages/lib-a"
		allConstraints := make([]string, 0, len(extraConstraints)+1)
		for _, ec := range extraConstraints {
			allConstraints = append(allConstraints, fmt.Sprintf("%s@%s", name, ec))
		}
		allConstraints = append(allConstraints, constraint)
		sort.Strings(allConstraints)
		b.WriteString(fmt.Sprintf("%q:\n", strings.Join(allConstraints, ", ")))
	} else {
		b.WriteString(fmt.Sprintf("%q:\n", constraint))
	}
	b.WriteString(fmt.Sprintf("  version: %s\n", "0.0.0-use.local"))
	b.WriteString(fmt.Sprintf("  resolution: \"%s@workspace:%s\"\n", name, workspacePath))

	// Collect non-peer deps (deduplicated via map).
	depMap := make(map[string]string)
	for _, d := range deps {
		if d.Type == ecosystem.DepPeer {
			continue
		}
		depMap[d.Name] = d.Constraint
	}
	if len(depMap) > 0 {
		b.WriteString("  dependencies:\n")
		depNames := make([]string, 0, len(depMap))
		for n := range depMap {
			depNames = append(depNames, n)
		}
		sort.Strings(depNames)
		for _, n := range depNames {
			yamlName := n
			if strings.HasPrefix(n, "@") {
				yamlName = fmt.Sprintf("%q", n)
			}
			c := depMap[n]
			if cfg.RootDepsNpmPrefix {
				// workspace: constraints keep their protocol.
				if strings.HasPrefix(c, "workspace:") || strings.HasPrefix(c, "npm:") || isNonRegistryBerryConstraint(c) {
					b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, c))
				} else {
					b.WriteString(fmt.Sprintf("    %s: \"npm:%s\"\n", yamlName, c))
				}
			} else {
				if strings.ContainsAny(c, ":*") || c == "true" || c == "false" || c == "null" {
					b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, c))
				} else {
					b.WriteString(fmt.Sprintf("    %s: %s\n", yamlName, c))
				}
			}
		}
	}

	// dependenciesMeta: mark optional deps.
	var optDeps []string
	for _, d := range deps {
		if d.Type == ecosystem.DepOptional {
			optDeps = append(optDeps, d.Name)
		}
	}
	if len(optDeps) > 0 {
		sort.Strings(optDeps)
		b.WriteString("  dependenciesMeta:\n")
		for _, n := range optDeps {
			yamlName := n
			if strings.HasPrefix(n, "@") {
				yamlName = fmt.Sprintf("%q", n)
			}
			b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
			b.WriteString("      optional: true\n")
		}
	}

	// peerDependencies and peerDependenciesMeta.
	g := ecosystem.GroupDependenciesByType(deps)
	if len(g.Peer) > 0 {
		peerNames := make([]string, 0, len(g.Peer))
		for n := range g.Peer {
			peerNames = append(peerNames, n)
		}
		sort.Strings(peerNames)
		b.WriteString("  peerDependencies:\n")
		for _, n := range peerNames {
			yamlName := n
			if strings.HasPrefix(n, "@") {
				yamlName = fmt.Sprintf("%q", n)
			}
			c := g.Peer[n]
			if strings.ContainsAny(c, ":*") || c == "true" || c == "false" || c == "null" {
				b.WriteString(fmt.Sprintf("    %s: \"%s\"\n", yamlName, c))
			} else {
				b.WriteString(fmt.Sprintf("    %s: %s\n", yamlName, c))
			}
		}
		if peerDepsMeta != nil {
			var optionalPeerNames []string
			for _, n := range peerNames {
				if pm, ok := peerDepsMeta[n]; ok && pm.Optional {
					optionalPeerNames = append(optionalPeerNames, n)
				}
			}
			if len(optionalPeerNames) > 0 {
				b.WriteString("  peerDependenciesMeta:\n")
				for _, n := range optionalPeerNames {
					yamlName := n
					if strings.HasPrefix(n, "@") {
						yamlName = fmt.Sprintf("%q", n)
					}
					b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
					b.WriteString("      optional: true\n")
				}
			}
		}
	}

	b.WriteString("  languageName: unknown\n")
	b.WriteString("  linkType: soft\n")
}

// buildConstraintMap collects all "name@npm:constraint" strings that resolve
// to each "name@version" key in the result.
func buildConstraintMap(result *ResolveResult, project *ecosystem.ProjectSpec) map[string][]string {
	m := make(map[string][]string)
	wsNames := workspaceMemberNamesYarn(project)

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
			// Skip workspace member edges - they get their own workspace entries.
			if strings.HasPrefix(edge.Constraint, "workspace:") {
				continue
			}
			// Compute targetKey to match the Packages map key.
			// The resolve engine stores non-registry deps under
			// "actualName@constraint" where actualName is the resolved package
			// name (not the dep alias). Tarball URL deps that resolved to a
			// real npm package are stored under "name@version".
			constraint := edge.Constraint
			var targetKey string
			if isNonRegistryBerryConstraint(constraint) {
				if isNpmRegistryURL(constraint) {
					// Tarball URL pointing at a known registry: the resolve engine
					// resolved it to a real npm package stored under name@version.
					targetKey = edge.Target.Name + "@" + edge.Target.Version
				} else {
					// Git, file, or other non-registry dep: stored under
					// actualName@constraint (actualName may differ from edge.Name
					// when the dep is aliased, e.g. "git-pkg" -> "is-odd").
					targetKey = edge.Target.Name + "@" + constraint
				}
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

		// Constraints from workspace member dependencies.
		// Workspace members are not in result.Packages (no OnNodeResolved call),
		// so we must walk their graph nodes to collect constraints for their deps.
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
				targetKey := depEdge.Target.Name + "@" + depEdge.Target.Version
				descriptor := fmt.Sprintf("%s@npm:%s", depEdge.Name, depEdge.Constraint)
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
		// Dedup caret constraints that differ by minor version.
		// Yarn deduplicates "^2.4.0" when "^2.8.0" exists (different minor),
		// but keeps both "^1.0.1" and "^1.0.2" (same minor, different patch).
		// Group by minor version and keep only the highest per minor group.
		byMinor := make(map[uint64]parsed)
		for _, p := range group {
			minor := p.semver.Minor()
			if existing, ok := byMinor[minor]; !ok || p.semver.GreaterThan(existing.semver) {
				byMinor[minor] = p
			}
		}
		// If all constraints collapse to one minor group, keep only the highest.
		// If multiple minor groups exist, keep only the highest overall
		// (yarn deduplicates across minor versions).
		if len(byMinor) == 1 {
			// Same minor version - keep all constraints (yarn doesn't dedup patch differences).
			for _, p := range group {
				result = append(result, p.full)
			}
		} else {
			// Different minor versions - keep only the highest constraint.
			best := group[0]
			for _, p := range group[1:] {
				if p.semver.GreaterThan(best.semver) {
					best = p
				}
			}
			result = append(result, best.full)
		}
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
func writeEntryBody(b *strings.Builder, pkg *ResolvedPackage, constraintKey string, cfg berryConfig) {
	node := pkg.Node

	b.WriteString(fmt.Sprintf("  version: %s\n", node.Version))

	// Extract the dep name from the constraint key (part before first @).
	// For "git-pkg@github:owner/repo", depName = "git-pkg".
	// For "wrappy@npm:^1.0.0", depName = "wrappy".
	depName := node.Name
	if atIdx := strings.Index(constraintKey, "@"); atIdx > 0 {
		depName = constraintKey[:atIdx]
	}

	// Determine resolution and linkType based on dep type.
	linkType := "hard"
	url := node.TarballURL
	if strings.HasPrefix(url, "git+ssh://") || strings.HasPrefix(url, "git+https://") {
		// Git dep: convert to HTTPS URL with #commit= hash format.
		cleanURL := strings.TrimPrefix(url, "git+ssh://git@github.com/")
		cleanURL = strings.TrimPrefix(cleanURL, "git+https://github.com/")
		cleanURL = "https://github.com/" + cleanURL
		cleanURL = strings.Replace(cleanURL, "#", "#commit=", 1)
		b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", depName, cleanURL))
	} else if strings.HasPrefix(url, "file:") {
		// file: dep: use portal: protocol with locator suffix.
		path := strings.TrimPrefix(url, "file:")
		locator := strings.NewReplacer("@", "%40", ":", "%3A").Replace(cfg.ProjectName) + "%40workspace%3A."
		b.WriteString(fmt.Sprintf("  resolution: \"%s@portal:%s::locator=%s\"\n", depName, path, locator))
		linkType = "soft"
	} else if isNpmRegistryURL(url) {
		// Tarball URL pointing at npm/yarn registry: this resolved to a known
		// npm package, so use standard npm resolution.
		b.WriteString(fmt.Sprintf("  resolution: \"%s@npm:%s\"\n", node.Name, node.Version))
	} else if strings.HasPrefix(url, "https://") && strings.HasSuffix(url, ".tgz") {
		// Non-registry tarball URL: use dep name + URL.
		b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", depName, url))
	} else {
		// Default: standard npm resolution.
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

	// dependenciesMeta: mark deps with install scripts as built.
	var builtDeps []string
	if node != nil {
		for _, edge := range node.Dependencies {
			if edge.Target != nil && edge.Target.HasInstallScript {
				builtDeps = append(builtDeps, edge.Name)
			}
		}
	}
	if len(builtDeps) > 0 {
		sort.Strings(builtDeps)
		b.WriteString("  dependenciesMeta:\n")
		for _, name := range builtDeps {
			yamlName := name
			if strings.HasPrefix(name, "@") {
				yamlName = fmt.Sprintf("%q", name)
			}
			b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
			b.WriteString("      built: true\n")
		}
	}

	// Checksum: for v6/v8, emit sha512 hex (yarn 3.2+/4 don't validate).
	// For v4/v5, omit entirely - yarn 2/3.1 compute cache-specific hashes
	// that can't be derived from registry data. Yarn fills them on install.
	if !cfg.SkipChecksum && node.Integrity != "" {
		checksum := integrityToYarnChecksum(node.Integrity, cfg.ChecksumPrefix)
		if checksum != "" {
			b.WriteString(fmt.Sprintf("  checksum: %s\n", checksum))
		}
	}

	b.WriteString("  languageName: node\n")
	b.WriteString(fmt.Sprintf("  linkType: %s\n", linkType))
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

// isNpmRegistryURL returns true if the URL is a standard npm registry tarball URL.
func isNpmRegistryURL(url string) bool {
	return strings.HasPrefix(url, "https://registry.npmjs.org/") ||
		strings.HasPrefix(url, "https://registry.yarnpkg.com/")
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
