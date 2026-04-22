# Plan: Workspace/Monorepo Support

## Overview

All four ecosystems support workspaces (npm 7+, pnpm, yarn, bun). Locksmith currently handles single-package projects only. This is the biggest blocker for real-world adoption.

## Type Changes

### `ecosystem/types.go`

```go
type WorkspaceMember struct {
    RelPath string       // relative path from root (e.g., "packages/foo")
    Spec    *ProjectSpec // parsed package.json for this member
}

type ProjectSpec struct {
    Name         string
    Version      string
    Dependencies []DeclaredDep
    Workspaces   []*WorkspaceMember // nil for single-package projects
}
```

### New file: `ecosystem/workspace.go`

```go
type WorkspaceIndex struct {
    byName map[string]*WorkspaceMember
}

func NewWorkspaceIndex(members []*WorkspaceMember) *WorkspaceIndex
func (wi *WorkspaceIndex) Resolve(name string) *WorkspaceMember
```

### `options.go`

```go
type GenerateOptions struct {
    // ... existing fields ...
    WorkspaceMembers  map[string][]byte // relPath -> raw package.json
    PnpmWorkspaceYaml []byte            // optional, for pnpm workspace discovery
}
```

### `ecosystem/resolver.go`

```go
type ResolveOptions struct {
    CutoffDate     *time.Time
    SpecDir        string
    WorkspaceIndex *WorkspaceIndex // for resolving workspace: protocol
}
```

## Spec Parser Changes

### `npm/spec.go`

- Add `Workspaces json.RawMessage` to `packageJSON` struct
- Add `ParseWorkspaceGlobs(raw json.RawMessage) []string` - handles both `["packages/*"]` and `{"packages": [...]}` forms
- Add `ParseWithWorkspaces(rootData []byte, memberData map[string][]byte) (*ProjectSpec, error)`

### New file: `pnpm/workspace_config.go`

- `ParsePnpmWorkspaceYaml(data []byte) ([]string, error)`

## Resolution Changes

### `ecosystem/resolve_engine.go`

Handle `workspace:` protocol before `isNonRegistrySpecifier`:

```go
if strings.HasPrefix(actualConstraint, "workspace:") {
    if s.opts.WorkspaceIndex != nil {
        member := s.opts.WorkspaceIndex.Resolve(actualName)
        if member != nil {
            // Create workspace node with member's version
            // Resolve member's own deps transitively
        }
    }
}
```

`workspace:` protocol patterns:
- `workspace:*` - any version
- `workspace:^` - caret range of member's version
- `workspace:~` - tilde range of member's version

Extend `Resolve()` to handle workspace members (don't need a separate function):
1. Check `len(project.Workspaces) > 0`
2. Build WorkspaceIndex, add to ResolveOptions
3. Add workspace member deps to the resolution queue alongside root deps
4. The `projectDeps` set includes deps from ALL members (for cross-tree dedup)

### PM-specific resolver changes

Each PM's `ResolveResult` needs workspace data:

- **npm**: `WorkspaceRoots map[string]*PlacedNode`
- **pnpm**: `Importers map[string]*ImporterInfo`
- **yarn**: `WorkspaceMembers map[string]*ProjectSpec`
- **bun**: `WorkspaceMembers map[string]*ProjectSpec`

## Formatter Changes

### npm (`npm/format_packagelock.go`)

packages section gets:
```json
"": { "name": "root", "workspaces": ["packages/*"] },
"packages/foo": { "name": "@scope/foo", "version": "1.0.0", "dependencies": {...} },
"node_modules/@scope/foo": { "resolved": "packages/foo", "link": true }
```

### pnpm (`pnpm/format.go`)

importers section gets entries per workspace member:
```yaml
importers:
  .:
    dependencies:
      shared-lib:
        specifier: workspace:*
        version: link:packages/shared-lib
  packages/shared-lib:
    dependencies:
      lodash:
        specifier: ^4.17.21
        version: 4.17.21
```

### yarn berry (`yarn/format_berry.go`)

Each member gets a workspace entry:
```
"@scope/foo@workspace:packages/foo":
  version: 0.0.0-use.local
  resolution: "@scope/foo@workspace:packages/foo"
  dependencies:
    lodash: "npm:^4.17.21"
  languageName: unknown
  linkType: soft
```

### yarn classic (`yarn/format_classic.go`)

Workspace members get:
```
"@scope/foo@0.0.0":
  version "0.0.0"
  resolved ""
```

### bun (`bun/format.go`)

workspaces section gets entries per member:
```jsonc
"workspaces": {
  "": { "name": "root", "dependencies": {...} },
  "packages/foo": { "name": "@scope/foo", "dependencies": {...} }
}
```

## CLI Changes

### `cmd/locksmith/main.go`

When `--spec` points to a package.json with `workspaces`:
1. Parse root to extract workspace globs
2. For pnpm formats, also check for `pnpm-workspace.yaml`
3. Expand globs to find member directories
4. Read each member's package.json
5. Populate `GenerateOptions.WorkspaceMembers`

## `locksmith.go` Orchestration

Each `generate*` function: if `WorkspaceMembers` non-nil, call `parser.ParseWithWorkspaces()` instead of `parser.Parse()`. The resolver and formatter handle workspaces via the extended ProjectSpec.

## Implementation Sequence

1. **Phase 1**: Core types and infrastructure (no behavior change, all tests pass)
2. **Phase 2**: Resolution engine workspace support + unit tests
3. **Phase 3**: PM-specific resolver changes + unit tests
4. **Phase 4**: Formatter changes (pnpm first, then bun, npm, yarn)
5. **Phase 5**: CLI workspace discovery
6. **Phase 6**: Integration tests with workspace fixtures

## Backward Compatibility

- All workspace fields default to nil
- Every codepath checks `len(spec.Workspaces) > 0` before entering workspace logic
- Formatter signatures unchanged (extended structs have zero-value defaults)
- Single-package projects produce identical output

## Key Challenges

- **Circular workspace deps**: Need workspace-level cycle detection
- **npm hoisting**: Workspace packages are symlinked, their deps hoisted to root
- **pnpm strict isolation**: Per-importer dependency lists, no hoisting
- **Test fixtures**: Need multi-package.json fixtures (extension of current structure)
- **`workspace:^`/`workspace:~`**: Need to resolve member version first, then construct constraint
