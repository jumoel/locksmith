# Plan: Fix Yarn Berry peerDependenciesMeta Accuracy

## Overview

The berry formatter hardcodes `optional: true` for ALL peer deps in the root workspace entry's `peerDependenciesMeta`. Only peers explicitly marked optional should get this flag. Required peers without `optional: true` cause yarn berry to suppress real peer dep warnings/errors.

## Current Bug

`yarn/format_berry.go:246-257`:
```go
// TODO: track actual peerDependenciesMeta from package.json
b.WriteString("  peerDependenciesMeta:\n")
for _, name := range peerNames {
    b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
    b.WriteString("      optional: true\n")  // <-- WRONG: hardcoded
}
```

## Root Cause

The data path is broken at two points:
1. `ecosystem.ProjectSpec` has no `PeerDepsMeta` field
2. `npm/spec.go` does not parse `peerDependenciesMeta` from package.json
3. `yarn/resolve.go` `ResolvedPackage` has no `PeerDepsMeta` field
4. The `OnNodeResolved` callback discards `meta.PeerDepsMeta`

## Changes

### Step 1: Add PeerDepsMeta to ProjectSpec

`ecosystem/types.go`:
```go
type ProjectSpec struct {
    Name         string
    Version      string
    Dependencies []DeclaredDep
    PeerDepsMeta map[string]PeerDepMeta // optional metadata for peer deps
}
```

`PeerDepMeta` already exists in `ecosystem/registry.go`.

### Step 2: Parse peerDependenciesMeta in spec parser

`npm/spec.go`:

Add to `packageJSON`:
```go
PeerDependenciesMeta map[string]peerDepMetaJSON `json:"peerDependenciesMeta,omitempty"`
```

```go
type peerDepMetaJSON struct {
    Optional bool `json:"optional"`
}
```

In `Parse()`, after optionalDependencies loop:
```go
if len(pkg.PeerDependenciesMeta) > 0 {
    spec.PeerDepsMeta = make(map[string]ecosystem.PeerDepMeta, len(pkg.PeerDependenciesMeta))
    for name, pm := range pkg.PeerDependenciesMeta {
        spec.PeerDepsMeta[name] = ecosystem.PeerDepMeta{Optional: pm.Optional}
    }
}
```

### Step 3: Add PeerDepsMeta to yarn ResolvedPackage

`yarn/resolve.go`:
```go
type ResolvedPackage struct {
    Node         *ecosystem.Node
    Dependencies map[string]string
    PeerDepsMeta map[string]ecosystem.PeerDepMeta // NEW
}
```

In `OnNodeResolved` callback:
```go
pkg := &ResolvedPackage{Node: node, Dependencies: resolvedDeps}
if len(meta.PeerDepsMeta) > 0 {
    pkg.PeerDepsMeta = meta.PeerDepsMeta
}
packages[key] = pkg
```

(Follows the same pattern as `bun/resolve.go:90-92`.)

### Step 4: Fix berry formatter

`yarn/format_berry.go`, replace lines 246-257:

```go
// peerDependenciesMeta: only mark peers that are actually optional.
var optionalPeerNames []string
for _, name := range peerNames {
    if pm, ok := project.PeerDepsMeta[name]; ok && pm.Optional {
        optionalPeerNames = append(optionalPeerNames, name)
    }
}
if len(optionalPeerNames) > 0 {
    b.WriteString("  peerDependenciesMeta:\n")
    for _, name := range optionalPeerNames {
        yamlName := name
        if strings.HasPrefix(name, "@") {
            yamlName = fmt.Sprintf("%q", name)
        }
        b.WriteString(fmt.Sprintf("    %s:\n", yamlName))
        b.WriteString("      optional: true\n")
    }
}
```

Behavioral changes:
- **Before**: All peers get `optional: true`. Section always emitted.
- **After**: Only peers with `optional: true` in `peerDependenciesMeta`. Section omitted if none are optional.

## Test Cases

### Spec parser test (`npm/spec_test.go`)

**TestSpecParser_Parse_PeerDepsMeta**:
```json
{
    "peerDependencies": {
        "react": "^18.0.0",
        "react-dom": "^18.0.0",
        "@types/react": "^18.0.0"
    },
    "peerDependenciesMeta": {
        "react-dom": { "optional": true },
        "@types/react": { "optional": true }
    }
}
```
- Assert `PeerDepsMeta` has 2 entries
- Assert `react` is NOT in PeerDepsMeta (required peer)
- Assert `react-dom` and `@types/react` are optional

Also test: absent/empty `peerDependenciesMeta` results in nil `PeerDepsMeta`.

### Update `npm/testdata/full.json`

Add a second peer dep + peerDependenciesMeta:
```json
"peerDependencies": { "react": "^18.0.0", "react-dom": "^18.0.0" },
"peerDependenciesMeta": { "react-dom": { "optional": true } }
```

Update `TestSpecParser_Parse_Full` expected count from 7 to 8.

### Berry formatter tests (`yarn/format_berry_test.go`)

1. **TestBerryPeerDepsMetaAccuracy** - 3 peers, 1 optional. Only the optional one in `peerDependenciesMeta`.
2. **TestBerryPeerDepsMetaAllRequired** - peers but no optional. `peerDependenciesMeta` section absent.
3. **TestBerryPeerDepsMetaAllOptional** - all peers optional. All appear in section.

## Edge Cases

- **No peerDependenciesMeta**: `project.PeerDepsMeta` nil. No section emitted. Correct.
- **Orphaned entries**: peerDependenciesMeta has `"foo"` but no matching peerDep. Safely ignored (formatter iterates `peerNames`, not `PeerDepsMeta` keys).
- **`optional: false`**: Equivalent to not being in PeerDepsMeta. Excluded. Correct.
- **Scoped packages**: YAML quoting already handles `@scope/pkg`.
- **All berry versions affected**: The fix is in shared `formatBerryWithConfig`, applies to v4/v5/v6/v8.

## Implementation Order

1. `ecosystem/types.go` - add `PeerDepsMeta` to `ProjectSpec`
2. `npm/spec.go` - parse `peerDependenciesMeta`
3. `npm/spec_test.go` + `npm/testdata/full.json` - test parser
4. `yarn/resolve.go` - add `PeerDepsMeta` to `ResolvedPackage`, save in callback
5. `yarn/format_berry.go` - use real metadata instead of hardcoding
6. `yarn/format_berry_test.go` - test formatter
