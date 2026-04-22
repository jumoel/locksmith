# Plan: npm overrides / pnpm overrides / yarn resolutions

## Overview

These fields force-resolve specific transitive dependency versions. The spec parser already reads `overrides` as `json.RawMessage` but never uses it. All three PM mechanisms do the same core thing - replace a constraint during resolution - but differ in selector syntax.

## Common Override Representation

### New file: `ecosystem/overrides.go`

```go
type OverrideRule struct {
    Package                string          // dep name to override
    Version                string          // replacement version/constraint
    ParentPattern          string          // restricts to deps required by this parent (empty = global)
    PackageVersionSelector string          // restricts to specific original constraint (pnpm: "foo@^2")
    Children               []OverrideRule  // nested overrides (npm only)
}

type OverrideSet struct {
    Rules []OverrideRule
}
```

Add `Overrides *OverrideSet` to `ecosystem.ProjectSpec`.

### New file: `ecosystem/override_match.go`

```go
type ResolutionContext struct {
    Ancestors []string // package names from root to current requester
}

func (os *OverrideSet) FindOverride(pkg string, originalConstraint string, ctx ResolutionContext) (string, bool)
```

Matching modes:
- **Global** (no parent, no version selector): always applies
- **Parent-scoped**: check if parent appears in Ancestors
  - npm nested: walk Children tree matching ancestor chain
  - pnpm `bar>foo`: bar appears anywhere in Ancestors
  - yarn `bar/**/foo`: bar anywhere; `bar/foo`: bar is immediate parent
- **Version-selector** (pnpm): check if original constraint matches selector pattern

## PM-Specific Parsing

### New file: `npm/overrides.go`

```go
func ParseNpmOverrides(raw json.RawMessage, rootDeps map[string]string) (*ecosystem.OverrideSet, error)
```

- Recursively walk nested JSON object
- String values = direct override, object values = nested scoping
- Replace `$dependency` references using `rootDeps` map

### New file: `npm/overrides_pnpm.go`

```go
func ParsePnpmOverrides(raw json.RawMessage) (*ecosystem.OverrideSet, error)
```

- Flat `map[string]string`, but keys have selectors:
  - `"foo": "1.0.0"` - global
  - `"foo@^2": "2.0.0"` - version selector
  - `"bar>foo": "1.0.0"` - parent selector

### New file: `npm/overrides_yarn.go`

```go
func ParseYarnResolutions(raw json.RawMessage) (*ecosystem.OverrideSet, error)
```

- Flat `map[string]string`, glob-like patterns:
  - `"foo"` or `"**/foo"` - global
  - `"bar/**/foo"` - subtree
  - `"bar/foo"` - direct parent only

## Spec Parser Changes

### `npm/spec.go`

Add fields to `packageJSON`:
```go
Pnpm        *pnpmField     `json:"pnpm,omitempty"`
Resolutions json.RawMessage `json:"resolutions,omitempty"`
```

Add `ParseFull` method returning raw override fields alongside ProjectSpec:
```go
type ParseResult struct {
    Spec            *ecosystem.ProjectSpec
    NpmOverrides    json.RawMessage
    PnpmOverrides   json.RawMessage
    YarnResolutions json.RawMessage
}

func (p *SpecParser) ParseFull(data []byte) (*ParseResult, error)
```

Existing `Parse` method unchanged for backward compatibility.

## Resolution Engine Integration

### `ecosystem/resolve_engine.go`

Add to `resolverState`:
```go
overrides *OverrideSet
ancestry  []string // current resolution chain
```

In `Resolve()`: read `project.Overrides` into state.

In `resolveDep()`, before constraint parsing:
```go
if s.overrides != nil {
    ctx := ResolutionContext{Ancestors: s.ancestry}
    if overrideVersion, ok := s.overrides.FindOverride(actualName, actualConstraint, ctx); ok {
        actualConstraint = overrideVersion
    }
}
```

Ancestry tracking around recursive calls:
```go
s.ancestry = append(s.ancestry, actualName)
// ... resolve children ...
s.ancestry = s.ancestry[:len(s.ancestry)-1]
```

Applied at three recursion points: regular deps, optional deps, peer deps.

Override check placed AFTER alias resolution but BEFORE non-registry specifier check.

## `locksmith.go` Plumbing

Each `generate*` function:
1. Call `parser.ParseFull()` instead of `parser.Parse()`
2. Call PM-specific override parser
3. Attach result to `spec.Overrides`

npm:
```go
rootDeps := make(map[string]string)
for _, dep := range spec.Dependencies { rootDeps[dep.Name] = dep.Constraint }
overrides, _ := npm.ParseNpmOverrides(result.NpmOverrides, rootDeps)
spec.Overrides = overrides
```

**Note on bun**: The claim that bun uses npm's `overrides` format is an assumption based on bun reading package.json (which contains the `overrides` field). The bun codebase has no override-related code. Verify against real bun behavior before implementing bun override support.

## Test Plan

### Override parsing tests

- `npm/overrides_test.go`: flat, nested, `$dependency`, deeply nested, empty, nil
- `npm/overrides_pnpm_test.go`: flat, version selector, parent selector, combined, empty
- `npm/overrides_yarn_test.go`: flat, `**` glob, parent glob, direct parent, empty

### Override matching tests

- `ecosystem/override_match_test.go`: global match, no match, parent match, nested parent, version selector match/no-match, empty set, nil set

### Resolution integration tests

- In `npm/resolve_test.go`: flat global, nested, `$dependency`
- In `pnpm/resolve_test.go`: flat global, version selector, parent selector
- In yarn tests: flat global, parent glob

### Test fixtures

- `npm/testdata/overrides_npm_flat.json`
- `npm/testdata/overrides_npm_nested.json`
- `npm/testdata/overrides_pnpm.json`
- `npm/testdata/overrides_yarn.json`

## Implementation Sequence

1. **Step 1**: Define common types in `ecosystem/overrides.go` + `ecosystem/override_match.go` + tests
2. **Step 2**: Implement PM-specific parsers + tests
3. **Step 3**: Extend spec parser with `ParseFull` (existing `Parse` unchanged)
4. **Step 4**: Integrate into resolution engine (ancestry tracking + override check)
5. **Step 5**: Plumb through `locksmith.go` generate functions

## Key Challenges

- **Dedup interaction**: Overrides apply before version selection, so deduped nodes already have the override applied. Correct behavior.
- **Cycle detection**: Override check happens before cycle detection. No interaction.
- **npm aliases**: Override matches against `actualName` (post-alias), which is correct.
- **Non-registry deps**: Override check placed before non-registry check, but only applies to registry specifiers.
- **pnpm version selector matching**: Pragmatic first implementation: string prefix matching on original constraint.
