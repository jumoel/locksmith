package locksmith

import "time"

// OutputFormat identifies the lockfile format to generate.
type OutputFormat string

const (
	FormatPackageLockV1 OutputFormat = "package-lock-v1"
	FormatPackageLockV2 OutputFormat = "package-lock-v2"
	FormatPackageLockV3 OutputFormat = "package-lock-v3"
	FormatNpmShrinkwrap OutputFormat = "npm-shrinkwrap"
	FormatPnpmLockV5    OutputFormat = "pnpm-lock-v5"
	FormatPnpmLockV6    OutputFormat = "pnpm-lock-v6"
	FormatPnpmLockV9    OutputFormat = "pnpm-lock-v9"
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
}
