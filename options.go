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

	// CutoffExcludes lists package names that bypass CutoffDate. Sourced
	// from `pnpm-workspace.yaml minimumReleaseAgeExclude` and
	// `bunfig.toml install.minimumReleaseAgeExcludes`. Exact-name match
	// only per ticket #28.
	CutoffExcludes []string

	// RegistryURL overrides the default registry for the ecosystem.
	RegistryURL string

	// ScopeRegistries maps npm scopes to registry URLs.
	// Key is the scope with @ prefix (e.g., "@company"), value is the registry base URL.
	// Packages matching a scope use the scope-specific registry instead of RegistryURL.
	ScopeRegistries map[string]string

	// AuthCredentials maps a normalized registry URL to a Credential.
	// The credential's AuthHeader value is set on Authorization for each
	// request to that registry. Keys MUST be normalized via
	// internal/registryurl.Normalize; the registry client normalizes its
	// own per-request URLs the same way before lookup.
	//
	// Supported credential types: ecosystem.BearerCredential (most common,
	// e.g. .npmrc _authToken / yarnrc.yml npmAuthToken / bunfig.toml token)
	// and ecosystem.BasicCredential (HTTP Basic, e.g. .npmrc _auth /
	// username+_password / yarnrc.yml npmAuthIdent / bunfig.toml
	// username+password). Client cert auth is not yet supported.
	AuthCredentials map[string]ecosystem.Credential

	// PolicyOverride, if set, overrides the default ResolverPolicy for the
	// chosen format. Use this to match the behavior of a specific package
	// manager version (e.g., npm 5-6 which don't auto-install peers).
	PolicyOverride *ecosystem.ResolverPolicy

	// Platform, if set, filters out packages whose OS/CPU restrictions
	// are incompatible with the target platform. Format: "os/cpu" (e.g., "linux/x64").
	//
	// Deprecated: prefer SupportedArchitectures, which carries the same
	// information plus a libc axis and supports multi-arch lockfiles.
	// Platform is honored when SupportedArchitectures is the zero value;
	// otherwise SupportedArchitectures wins. The two fields are kept in
	// sync by the CLI's flag-handling code.
	Platform string

	// SupportedArchitectures filters out packages whose OS/CPU/Libc
	// restrictions don't intersect the configured axes. Sourced from
	// `.yarnrc.yml supportedArchitectures` and `pnpm-workspace.yaml
	// supportedArchitectures` per ticket #13.
	//
	// Zero value means "no filtering" (every node passes). The
	// `--platform os/cpu` CLI shorthand populates this with a single-entry
	// list per axis; richer multi-arch use comes from config files.
	SupportedArchitectures ecosystem.Architectures

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
	// are incompatible, behavior depends on EngineStrict.
	NodeVersion string

	// EngineStrict, when true, makes "no version compatible with NodeVersion"
	// a hard resolution error rather than falling back to the best-available
	// version. Sourced from .npmrc `engine-strict=true` per ticket #15.
	EngineStrict bool

	// Catalogs provides pnpm catalog definitions. Maps catalog name ("default"
	// for the unnamed catalog:) to package name -> version constraint.
	// Typically parsed from pnpm-workspace.yaml by the CLI.
	Catalogs map[string]map[string]string

	// PnpmPatchHashMD5 selects MD5+base32 patch hash encoding (pnpm 7-9)
	// instead of the default SHA256 hex (pnpm 10+). Only affects pnpm lockfile
	// generation when PatchedDependencies are present.
	PnpmPatchHashMD5 bool

	// YarnCompressionLevel mirrors `.yarnrc.yml`'s `compressionLevel` setting.
	// Yarn 4 (FormatYarnBerryV8) derives the lockfile's `cacheKey` suffix from
	// this value (see yarn.YarnBerryV8Formatter for the mapping). Required to
	// match `cacheKey` exactly under `yarn install --immutable`.
	YarnCompressionLevel string

	// TLSOptions controls TLS validation and certificate trust for registry
	// requests. Nil means default (system roots, strict validation).
	// Sourced from .npmrc ca/cafile/strict-ssl per ticket #17.
	TLSOptions *ecosystem.TLSOptions

	// OmitLockfileRegistryResolved suppresses the `resolved` field on
	// registry-tarball entries in npm package-lock output. Sourced from
	// `.npmrc omit-lockfile-registry-resolved=true`. file: and workspace
	// symlinks keep their resolved value (npm needs the path).
	// Currently honored by FormatPackageLockV3 only; V1/V2 follow-up tracked
	// separately.
	OmitLockfileRegistryResolved bool

	// MinifyPackageLock disables pretty-printing in npm package-lock output,
	// matching `.npmrc format-package-lock=false`. Default (zero value) is
	// pretty-print, matching npm's `format-package-lock=true` default.
	// Currently honored by FormatPackageLockV3 only; V1/V2 follow-up tracked
	// separately.
	MinifyPackageLock bool
}
