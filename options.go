package locksmith

import (
	"time"

	"github.com/jumoel/locksmith/ecosystem"
)

// OutputFormat identifies the lockfile format to generate.
type OutputFormat string

const (
	// npm lockfile formats.
	FormatPackageLockV1 OutputFormat = "package-lock-v1"
	FormatPackageLockV2 OutputFormat = "package-lock-v2"
	FormatPackageLockV3 OutputFormat = "package-lock-v3"
	FormatNpmShrinkwrap OutputFormat = "npm-shrinkwrap"

	// pnpm lockfile formats.
	FormatPnpmLockV4 OutputFormat = "pnpm-lock-v4"
	FormatPnpmLockV5 OutputFormat = "pnpm-lock-v5"
	FormatPnpmLockV6 OutputFormat = "pnpm-lock-v6"
	FormatPnpmLockV9 OutputFormat = "pnpm-lock-v9"

	// yarn lockfile formats.
	FormatYarnClassic OutputFormat = "yarn-classic"
	FormatYarnBerryV4 OutputFormat = "yarn-berry-v4"
	FormatYarnBerryV5 OutputFormat = "yarn-berry-v5"
	FormatYarnBerryV6 OutputFormat = "yarn-berry-v6"
	FormatYarnBerryV8 OutputFormat = "yarn-berry-v8"

	// bun lockfile formats.
	FormatBunLock OutputFormat = "bun-lock"
)

// AllFormats returns all known output formats.
func AllFormats() []OutputFormat {
	return []OutputFormat{
		FormatPackageLockV1,
		FormatPackageLockV2,
		FormatPackageLockV3,
		FormatNpmShrinkwrap,
		FormatPnpmLockV4,
		FormatPnpmLockV5,
		FormatPnpmLockV6,
		FormatPnpmLockV9,
		FormatYarnClassic,
		FormatYarnBerryV4,
		FormatYarnBerryV5,
		FormatYarnBerryV6,
		FormatYarnBerryV8,
		FormatBunLock,
	}
}

// GenerateOptions configures lockfile generation.
type GenerateOptions struct {
	// SpecFile is the raw contents of the spec file (e.g., package.json).
	SpecFile []byte

	// OutputFormat selects which lockfile format to produce.
	OutputFormat OutputFormat

	// CutoffDate, if set, restricts resolution to versions published before this time.
	CutoffDate *time.Time

	// RegistryURL overrides the default registry for the ecosystem.
	RegistryURL string

	// PolicyOverride, if set, overrides the default ResolverPolicy for the
	// chosen format. Use this to match the behavior of a specific package
	// manager version (e.g., npm 5-6 which don't auto-install peers).
	PolicyOverride *ecosystem.ResolverPolicy

	// Platform, if set, filters out packages whose OS/CPU restrictions
	// are incompatible with the target platform. Format: "os/cpu" (e.g., "linux/x64").
	Platform string

	// SpecDir is the directory containing the spec file on disk. Used to
	// resolve file: dependencies by reading the local package's version.
	// If empty, file: deps get a placeholder version.
	SpecDir string

	// WorkspaceMembers provides the spec files for workspace members.
	// Each entry maps a relative path (e.g., "packages/foo") to its raw spec file contents.
	// If nil or empty, locksmith operates in single-package mode.
	WorkspaceMembers map[string][]byte

	// NodeVersion, if set, skips package versions whose engines.node
	// constraint is incompatible with this Node.js version during resolution.
	// Format: semver string (e.g., "18.0.0"). When all candidate versions
	// are incompatible, the best version is used regardless (matches npm behavior).
	NodeVersion string
}
