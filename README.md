<p align="center">
  <img src="locksmith.png" alt="locksmith" width="200" />
</p>

<h1 align="center">locksmith</h1>

<p align="center">A Go library that generates valid lockfiles from package spec files. Given a <code>package.json</code>, locksmith resolves all dependencies against the npm registry and produces a lockfile that the target package manager accepts without modification.</p>

## Usage

### As a library

```go
import "github.com/jumoel/locksmith"

result, err := locksmith.Generate(ctx, locksmith.GenerateOptions{
    SpecFile:     packageJSON,
    OutputFormat: locksmith.FormatPackageLockV3,
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
  types.go           Graph, Node, Edge, ProjectSpec
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

Each PM's resolver is a thin wrapper (~60-80 lines) that configures the policy and collects PM-specific metadata via the `OnNodeResolved` callback. npm additionally runs a BFS hoisting/placement phase after resolution.

### Features

- **npm-pick-manifest**: prefers the `latest` dist-tag version over higher semver matches
- **Cross-tree deduplication**: reuses existing resolved versions across the dependency tree
- **Peer dependency auto-install**: respects per-PM-version behavior (npm 7+, pnpm 8+, yarn berry)
- **npm alias resolution**: handles `npm:package@constraint` syntax
- **Non-registry deps**: gracefully handles `file:`, `git+`, `github:`, `workspace:` specifiers
- **Cutoff date filtering**: only resolves versions published before a given date
- **Per-PM-version PolicyOverride**: callers can specify exact resolution behavior for any PM version

## Testing

### Correctness matrix

700+ tests across 24 package manager versions and 49 fixtures, comparing resolved versions against what each real package manager produces (via Docker):

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

49 package.json fixtures covering:

- **Core patterns**: minimal, transitive, diamond, multi-dep, dev-deps, pinned, scoped, zero-deps
- **Framework versions**: React 15-19, Next.js 12-15, TypeScript 4-5
- **Large packages**: express, webpack, styled-components, eslint
- **Edge cases**: conflicting version ranges, optional missing deps, circular peer deps, deprecated packages, platform-specific deps, aliased deps, non-registry deps, bundled deps
- **Arborist test suite**: dedupe, dev-deps, peer-cycle, optional-missing, peer-optional (using real @isaacs/ test packages)
- **Package managers as deps**: npm 6/10, pnpm 8/9, yarn classic
