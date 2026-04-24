<p align="center">
  <img src="locksmith.png" alt="locksmith" width="200" />
</p>

<h1 align="center">locksmith</h1>

<p align="center">A Go library that generates valid lockfiles from package spec files. Given a <code>package.json</code>, locksmith resolves all dependencies against the npm registry and produces a lockfile that the target package manager accepts without modification.</p>

## Install

### Binary (recommended)

Download the latest release from [GitHub Releases](https://github.com/jumoel/locksmith/releases):

```bash
# macOS (Apple Silicon)
curl -L https://github.com/jumoel/locksmith/releases/latest/download/locksmith-darwin-arm64 -o locksmith
chmod +x locksmith

# macOS (Intel)
curl -L https://github.com/jumoel/locksmith/releases/latest/download/locksmith-darwin-amd64 -o locksmith
chmod +x locksmith

# Linux (x86_64)
curl -L https://github.com/jumoel/locksmith/releases/latest/download/locksmith-linux-amd64 -o locksmith
chmod +x locksmith

# Linux (ARM64)
curl -L https://github.com/jumoel/locksmith/releases/latest/download/locksmith-linux-arm64 -o locksmith
chmod +x locksmith

# Windows (x86_64) - PowerShell
Invoke-WebRequest -Uri https://github.com/jumoel/locksmith/releases/latest/download/locksmith-windows-amd64.exe -OutFile locksmith.exe
```

### From source

```bash
go install github.com/jumoel/locksmith/cmd/locksmith@latest
```

## Usage

### As a library

```go
import "github.com/jumoel/locksmith"

// Single package
result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
    SpecFile:     packageJSON,
    OutputFormat: locksmith.FormatPackageLockV3,
})

// Workspace/monorepo
result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
    SpecFile:     rootPackageJSON,
    OutputFormat: locksmith.FormatPnpmLockV9,
    WorkspaceMembers: map[string][]byte{
        "packages/lib-a": libAPackageJSON,
        "packages/lib-b": libBPackageJSON,
    },
})
// result.Lockfile contains the generated lockfile bytes
```

### As a CLI

```
locksmith generate \
  --spec package.json \
  --format package-lock-v3 \
  --output package-lock.json
```

Optional flags: `--cutoff 2025-01-01` (only resolve versions published before this date), `--registry https://registry.npmjs.org` (override registry URL).

## Supported formats

| Format | Flag | PM versions | Correctness | Acceptance | Notes |
|---|---|---|---|---|---|
| package-lock.json v1 | `package-lock-v1` | npm 5-6 | npm 5, 6 | npm 6-11 (ci) | |
| package-lock.json v2 | `package-lock-v2` | npm 7-8 | npm 7, 8 | npm 7-11 (ci) | |
| package-lock.json v3 | `package-lock-v3` | npm 9+ | npm 7-11 | npm 7-11 (ci) | 17+ fixtures byte-identical |
| npm-shrinkwrap.json | `npm-shrinkwrap` | npm 2-11 | npm 2-6 | npm 2, 5-11 | v1 format for max compat |
| pnpm-lock.yaml v5.1 | `pnpm-lock-v4` | pnpm 4 | pnpm 4 | pnpm 4 | |
| pnpm-lock.yaml v5.2-5.4 | `pnpm-lock-v5` | pnpm 5-7 | pnpm 5, 7 | pnpm 5, 6, 7 | |
| pnpm-lock.yaml v6.0 | `pnpm-lock-v6` | pnpm 8 | pnpm 8 | pnpm 8 | |
| pnpm-lock.yaml v9.0 | `pnpm-lock-v9` | pnpm 9-10 | pnpm 9, 10 | pnpm 9, 10 | |
| yarn.lock classic | `yarn-classic` | yarn 1 | yarn 1 | yarn 1 | |
| yarn.lock berry v4 | `yarn-berry-v4` | yarn 2 | yarn 2 | yarn 2 (install) | Checksums omitted* |
| yarn.lock berry v5 | `yarn-berry-v5` | yarn 3.1 | yarn 3.1 | yarn 3.1 (install) | Checksums omitted* |
| yarn.lock berry v6 | `yarn-berry-v6` | yarn 3.2+ | yarn 3 | yarn 3 (immutable) | |
| yarn.lock berry v8 | `yarn-berry-v8` | yarn 4 | yarn 4 | yarn 4 (immutable) | |
| bun.lock | `bun-lock` | bun 1.2+ | bun | bun | |

*\*Yarn berry v4/v5 checksums: yarn 2/3.1 compute checksums by re-packing tarballs into their internal ZIP cache format and hashing that. This hash can't be derived from registry data alone. Lockfiles are generated without checksums; yarn fills them on first `yarn install`. Yarn 3.2+ and 4 don't validate checksums.*

**Not implemented**: pnpm lockfile v1-v3 (pnpm 1-3, requires Node 4-10, zero active usage), yarn berry v1-v3 (pre-release development artifacts), yarn berry v7 (no yarn version ever produced this metadata version).

## Architecture

```
ecosystem/           Shared interfaces, types, and resolution engine
  resolve_engine.go  Single dependency resolver parameterized by ResolverPolicy
  nodeindex.go       O(1) package name lookups for dedup and peer checks
  workspace.go       WorkspaceIndex for workspace: protocol resolution
  types.go           Graph, Node, Edge, ProjectSpec, WorkspaceMember
  deps.go            Dependency grouping helpers

npm/                 npm-specific: registry client, spec parser, hoisting, v1/v2/v3 formatters
pnpm/                pnpm-specific: v5.1/v5.4/v6/v9 formatters
yarn/                yarn-specific: classic and berry v4-v8 formatters
bun/                 bun-specific: bun.lock formatter
internal/semver/     npm-compatible semver range resolution
internal/orderedjson Deterministic JSON serialization
internal/maputil/    Shared map utilities

cmd/locksmith/         CLI entry point
testharness/         Integration tests, Docker setup, 49 fixtures
```

### Resolution engine

All package managers share a single `ecosystem.Resolve()` function. Behavioral differences are captured in `ResolverPolicy`:

| Policy | npm | pnpm | yarn classic | yarn berry | bun |
|---|---|---|---|---|---|
| CrossTreeDedup | true | true | false | false | true |
| AutoInstallPeers | true | true | false | true | true |
| StorePeerMetaOnNode | true | false | false | false | false |
| ResolveWorkspaceByName | true | false | true | false | false |

Each PM's resolver is a thin wrapper (~60-80 lines) that configures the policy and collects PM-specific metadata via the `OnNodeResolved` callback. npm additionally runs a BFS hoisting/placement phase after resolution.

### Features

- **npm-pick-manifest**: prefers the `latest` dist-tag version over higher semver matches
- **Cross-tree deduplication**: reuses existing resolved versions across the dependency tree
- **Peer dependency auto-install**: respects per-PM-version behavior (npm 7+, pnpm 8+, yarn berry)
- **npm alias resolution**: handles `npm:package@constraint` syntax
- **Non-registry deps**: handles `file:`, `git+`, `github:`, tarball URL, and `workspace:` specifiers with PM-correct lockfile entries
- **Workspace/monorepo support**: resolves workspace members and cross-workspace deps (`workspace:*`, `workspace:^`), generates multi-importer lockfiles for pnpm, bun, and yarn berry
- **Cutoff date filtering**: only resolves versions published before a given date
- **Version overrides**: npm `overrides`, pnpm `pnpm.overrides`, and yarn `resolutions` - force specific versions for transitive dependencies
- **pnpm extensions**: `pnpm.packageExtensions` to inject missing deps into packages, `pnpm.peerDependencyRules` to control peer dep resolution (ignoreMissing, allowedVersions)
- **Per-PM-version PolicyOverride**: callers can specify exact resolution behavior for any PM version

## Testing

### Correctness matrix

900+ tests across 24 package manager versions and 56 fixtures, comparing resolved versions against what each real package manager produces (via Docker):

| PM versions tested | Fixtures |
|---|---|
| npm 2, 3, 4, 5, 6, 7, 8, 9, 10, 11 | minimal, transitive, diamond, react 15-19, next 12-13, typescript 4-5, express, arborist edge cases, and more |
| pnpm 4, 5, 7, 8, 9, 10 | Same fixtures |
| yarn 1, 3, 4 | Same fixtures |
| bun | Same fixtures |

### Running tests

```bash
# Unit tests (fast, no network)
go test -short ./...

# Generation tests against real npm registry
go test ./testharness/

# Full Docker correctness matrix (requires Docker)
go test -tags integration -timeout 25m ./testharness/
```

### CI matrix splitting

Tests are structured for CI parallelization:

```yaml
strategy:
  matrix:
    format: [package-lock-v1, package-lock-v3, pnpm-lock-v9, yarn-classic, bun-lock]
steps:
  - run: go test -tags integration -run "TestCorrectness/${{ matrix.format }}" ./testharness/
```

## Test fixtures

56 package.json fixtures covering:

- **Core patterns**: minimal, transitive, diamond, multi-dep, dev-deps, pinned, scoped, zero-deps
- **Framework versions**: React 15-19, Next.js 12-15, TypeScript 4-5
- **Large packages**: express, webpack, styled-components, eslint
- **Edge cases**: conflicting version ranges, optional missing deps, circular peer deps, deprecated packages, platform-specific deps, aliased deps, non-registry deps, bundled deps
- **Overrides**: npm overrides, yarn resolutions forcing transitive dep versions
- **pnpm extensions**: packageExtensions injecting deps, peerDependencyRules controlling peer resolution
- **Workspaces**: simple monorepo, cross-workspace deps with `workspace:*`, `workspace:^`, and npm-style semver ranges
- **Arborist test suite**: dedupe, dev-deps, peer-cycle, optional-missing, peer-optional (using real @isaacs/ test packages)
- **Package managers as deps**: npm 6/10, pnpm 8/9, yarn classic

## Releasing

Create a new release with the version bump script:

```bash
./scripts/release.sh patch    # v0.0.1 -> v0.0.2
./scripts/release.sh minor    # v0.0.2 -> v0.1.0
./scripts/release.sh major    # v0.1.0 -> v1.0.0
```

This creates an annotated git tag and pushes it. GitHub Actions builds binaries for linux (amd64/arm64), macOS (amd64/arm64), and Windows (amd64), generates sha256 checksums, and publishes a release with auto-generated notes.
