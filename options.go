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
	FormatPnpmLockV5 OutputFormat = "pnpm-lock-v5"
	FormatPnpmLockV6 OutputFormat = "pnpm-lock-v6"
	FormatPnpmLockV9 OutputFormat = "pnpm-lock-v9"

	// yarn lockfile formats.
	FormatYarnClassic OutputFormat = "yarn-classic"
	FormatYarnBerryV5 OutputFormat = "yarn-berry-v5"
	FormatYarnBerryV6 OutputFormat = "yarn-berry-v6"
	FormatYarnBerryV7 OutputFormat = "yarn-berry-v7"
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
		FormatPnpmLockV5,
		FormatPnpmLockV6,
		FormatPnpmLockV9,
		FormatYarnClassic,
		FormatYarnBerryV5,
		FormatYarnBerryV6,
		FormatYarnBerryV7,
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
}
