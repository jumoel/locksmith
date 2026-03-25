package yarn

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jumoel/locksmith/ecosystem"
	"github.com/jumoel/locksmith/internal/maputil"
)

// YarnBerryV4Formatter produces yarn.lock output in yarn berry v4 format (yarn 2.0 initial stable).
type YarnBerryV4Formatter struct{}

func NewYarnBerryV4Formatter() *YarnBerryV4Formatter { return &YarnBerryV4Formatter{} }

func (f *YarnBerryV4Formatter) Format(_ *ecosystem.Graph, _ *ecosystem.ProjectSpec) ([]byte, error) {
	return nil, fmt.Errorf("use FormatFromResult for yarn berry lockfile generation")
}

func (f *YarnBerryV4Formatter) FormatFromResult(result *ResolveResult, project *ecosystem.ProjectSpec) ([]byte, error) {
	return formatBerryWithConfig(result, project, berryConfig{
		MetadataVersion: 4, CacheKey: 7, ChecksumPrefix: "", IncludeRoot: true, SkipChecksum: true,
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
		MetadataVersion: 5, CacheKey: 8, ChecksumPrefix: "", IncludeRoot: true, SkipChecksum: true,
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
		MetadataVersion: 6, CacheKey: 10, ChecksumPrefix: "", IncludeRoot: true,
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
		MetadataVersion: 8, CacheKey: 10, ChecksumPrefix: "10/", IncludeRoot: true,
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
	MetadataVersion int
	CacheKey        int
	ChecksumPrefix  string // "10/" for v8, "" for v5-v6
	IncludeRoot     bool   // true for yarn berry (adds workspace root entry)
	SkipChecksum bool // v4/v5: omit checksums (yarn 2/3.1 computes cache-specific hashes)
}

func formatBerryWithConfig(result *ResolveResult, project *ecosystem.ProjectSpec, cfg berryConfig) ([]byte, error) {
	// Build entries: group constraints by resolved "name@version".
	// In a flat resolution, each constraint on a given package name resolves to
	// the same version, so we collect all constraints that point to each package.
	constraintsByKey := buildConstraintMap(result, project)

	// Build sorted entries.
	entries := make([]*berryEntry, 0, len(result.Packages))
	for key, pkg := range result.Packages {
		constraints := constraintsByKey[key]
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

	// Write metadata header.
	b.WriteString("__metadata:\n")
	b.WriteString(fmt.Sprintf("  version: %d\n", cfg.MetadataVersion))
	b.WriteString(fmt.Sprintf("  cacheKey: %d\n", cfg.CacheKey))

	// Write each package entry.
	for _, entry := range entries {
		b.WriteByte('\n')
		writeEntryKey(&b, entry.constraints)
		writeEntryBody(&b, entry.pkg, cfg.ChecksumPrefix, cfg.SkipChecksum)
	}

	// Write workspace root entry if enabled.
	if cfg.IncludeRoot && project.Name != "" {
		b.WriteByte('\n')
		rootKey := fmt.Sprintf("%q:\n", fmt.Sprintf("%s@workspace:.", project.Name))
		b.WriteString(rootKey)
		// Yarn berry uses "0.0.0-use.local" for workspace root version.
		ver := "0.0.0-use.local"
		b.WriteString(fmt.Sprintf("  version: %s\n", ver))
		b.WriteString(fmt.Sprintf("  resolution: \"%s@workspace:.\"\n", project.Name))
		// Add root dependencies.
		if len(project.Dependencies) > 0 {
			b.WriteString("  dependencies:\n")
			depNames := make([]string, 0, len(project.Dependencies))
			for _, d := range project.Dependencies {
				depNames = append(depNames, d.Name)
			}
			sort.Strings(depNames)
			depMap := make(map[string]string)
			for _, d := range project.Dependencies {
				depMap[d.Name] = d.Constraint
			}
			for _, name := range depNames {
				b.WriteString(fmt.Sprintf("    %s: %s\n", name, depMap[name]))
			}
		}
		b.WriteString("  languageName: unknown\n")
		b.WriteString("  linkType: soft\n")
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
			targetKey := edge.Target.Name + "@" + edge.Target.Version
			descriptor := fmt.Sprintf("%s@npm:%s", edge.Name, edge.Constraint)
			m[targetKey] = appendUnique(m[targetKey], descriptor)
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

	return m
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
func writeEntryBody(b *strings.Builder, pkg *ResolvedPackage, checksumPrefix string, skipChecksum bool) {
	node := pkg.Node

	b.WriteString(fmt.Sprintf("  version: %s\n", node.Version))
	b.WriteString(fmt.Sprintf("  resolution: \"%s@npm:%s\"\n", node.Name, node.Version))

	// Dependencies (sorted by name).
	if len(pkg.Dependencies) > 0 {
		b.WriteString("  dependencies:\n")
		depNames := maputil.SortedKeys(pkg.Dependencies)

		for _, name := range depNames {
			// Find the constraint used for this dependency from the node's edges.
			constraint := findConstraint(node, name)
			b.WriteString(fmt.Sprintf("    %s: \"npm:%s\"\n", name, constraint))
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
