# Plan: Bun Non-Registry Dependency Support

## Overview

The bun formatter doesn't produce correct lockfile entries for non-registry deps (git, file, tarball URL). The integration test skips `non-registry-deps` for bun.

## Target Entry Formats

### Registry dep (current, working):
```json
"ms": ["ms@2.1.3", "https://registry.npmjs.org/ms/-/ms-2.1.3.tgz", {}, "sha512-..."]
```
4-element array: `[resolvedSpec, tarballURL, metadata, integrity]`

### Git dep (`github:` shorthand):
```json
"git-pkg": ["is-odd@github:jonschlinkert/is-odd", "is-odd@git+ssh://git@github.com/jonschlinkert/is-odd.git#abc123"]
```
2-element array: `[realName@originalConstraint, realName@resolvedURL]`

### Git dep (`git+ssh://`):
```json
"git-ssh-pkg": ["is-even@git+ssh://git@github.com/jonschlinkert/is-even.git", "is-even@git+ssh://git@github.com/jonschlinkert/is-even.git#abc123"]
```
2-element array: element 0 without commit hash, element 1 with hash.

### File dep:
```json
"local-pkg": ["local-pkg@file:./local-pkg", ""]
```
2-element array: `[name@file:path, ""]`

### Tarball URL dep:
```json
"tarball-pkg": ["is-odd@https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz", "is-odd@https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz"]
```
2-element array: both elements are `realName@url`.

## Packages Map Key Formats

The resolve engine uses different key formats for non-registry deps. **Important**: for GitHub deps, `actualName` gets overridden from the GitHub package.json (resolve_engine.go:209), so the key uses the **real package name**, not the declared alias:
- Git deps (github:): `"is-odd@github:jonschlinkert/is-odd"` (real name from GitHub, not declared alias "git-pkg")
- Git deps (git+ssh://): `"git-ssh-pkg@git+ssh://..."` (declared name, since no GitHub API override)
- File deps: `"local-pkg@file:./local-pkg"` (name + constraint)
- Tarball deps (resolved): `"is-odd@3.0.1"` (resolved name + version, same as registry)

**The detection logic must use Node pointer comparison** (not key string matching) to reliably associate Packages map entries with root edges, since key formats are inconsistent.

## Changes to `bun/format.go`

### 1. Add `isNonRegistryConstraint` helper

```go
func isNonRegistryConstraint(constraint string) bool {
    prefixes := []string{"file:", "link:", "git+", "git://", "github:", "http://", "https://"}
    for _, p := range prefixes {
        if strings.HasPrefix(constraint, p) { return true }
    }
    return false
}
```

### 2. Add `nonRegInfo` type and detection in `buildPackagesFromGraph`

```go
type nonRegInfo struct {
    DeclaredName       string // e.g., "git-pkg"
    OriginalConstraint string // e.g., "github:jonschlinkert/is-odd"
}
```

Build detection map from root edges by pointer comparison:
```go
nonRegByNodePtr := make(map[*ecosystem.Node]nonRegInfo)
if result.Graph != nil && result.Graph.Root != nil {
    for _, edge := range result.Graph.Root.Dependencies {
        if edge.Target == nil { continue }
        if isNonRegistryConstraint(edge.Constraint) {
            nonRegByNodePtr[edge.Target] = nonRegInfo{
                DeclaredName:       edge.Name,
                OriginalConstraint: edge.Constraint,
            }
        }
    }
}

nonRegPkgs := make(map[string]nonRegInfo)
for key, pkg := range result.Packages {
    if info, ok := nonRegByNodePtr[pkg.Node]; ok {
        nonRegPkgs[key] = info
    }
}
```

### 3. Add `buildNonRegistryPackageEntry` function

```go
func buildNonRegistryPackageEntry(pkg *ResolvedPackage, declaredName string, originalConstraint string) []interface{} {
    node := pkg.Node
    url := node.TarballURL

    if strings.HasPrefix(url, "file:") {
        return []interface{}{
            fmt.Sprintf("%s@%s", node.Name, url),
            "",
        }
    }

    if strings.HasPrefix(url, "git+") {
        spec := fmt.Sprintf("%s@%s", node.Name, originalConstraint)
        resolved := fmt.Sprintf("%s@%s", node.Name, url)
        return []interface{}{spec, resolved}
    }

    if strings.HasPrefix(originalConstraint, "https://") {
        spec := fmt.Sprintf("%s@%s", node.Name, originalConstraint)
        return []interface{}{spec, spec}
    }

    // Fallback: standard format
    return nil // will use buildPackageEntry
}
```

### 4. Modify entry-building loop

In `buildPackagesFromGraph`, where entries are converted to `orderedjson.Map`:

```go
for _, e := range entries {
    var value interface{}
    if nrInfo, isNonReg := nonRegPkgs[e.key]; isNonReg {
        value = buildNonRegistryPackageEntry(e.pkg, nrInfo.DeclaredName, nrInfo.OriginalConstraint)
    }
    if value == nil {
        value = buildPackageEntry(e.pkg, e.bundled)
    }
    packages = append(packages, orderedjson.Entry{Key: e.displayKey, Value: value})
}
```

The `displayKey` for non-registry deps should be the declared name from package.json, not the resolved name.

### 5. Handle package key for non-registry deps

Non-registry deps should use `declaredName` as the key in the packages section (e.g., `"git-pkg"`, `"tarball-pkg"`). The existing alias map logic handles cases where `edge.Name != edge.Target.Name`. Need to verify this works for all non-registry dep types.

## Test Cases

### `bun/format_test.go` (new file, or additions)

1. **TestBuildNonRegistryPackageEntry_GitDep** - 2-element array, correct spec and resolved URL
2. **TestBuildNonRegistryPackageEntry_FileDep** - 2-element array, `["name@file:path", ""]`
3. **TestBuildNonRegistryPackageEntry_TarballDep** - 2-element array, both elements identical
4. **TestBuildPackagesFromGraph_NonRegistry** - mock ResolveResult with non-registry deps, verify packages map
5. **TestFormatFromResult_NonRegistryDeps** - end-to-end JSONC output verification

## Integration Test

Remove skip in `testharness/integration_test.go:135`:
```go
// Change from:
bunSkips := map[string]string{
    "zero-deps":        "bun deletes lockfile for empty projects",
    "non-registry-deps": "bun non-registry dep format not yet implemented",
}
// To:
bunSkips := map[string]string{
    "zero-deps": "bun deletes lockfile for empty projects",
}
```

## Potential Challenges

- **Tarball dep identity overlap**: Tarball URL dep resolves to same `name@version` as registry dep. Formatter must emit two distinct entries (the alias handling covers this).
- **Git+SSH in Docker**: Integration test container may lack SSH keys. May need HTTPS-only git deps.
- **Bun format stability**: Exact format may vary between bun versions.
- **Transitive deps of git packages**: These are normal registry deps - already handled correctly.
