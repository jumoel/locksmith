# Plan: Yarn Berry Git Dependency Entries

## Overview

The yarn berry formatter doesn't produce correct lockfile entries for non-registry deps (git, file, tarball URL). The integration test explicitly skips `non-registry-deps` for all berry versions. Yarn classic handles these fine.

## Current State

The `non-registry-deps` fixture:
```json
{
  "dependencies": {
    "ms": "^2.1.0",
    "local-pkg": "file:./local-pkg",
    "git-pkg": "github:jonschlinkert/is-odd",
    "git-ssh-pkg": "git+ssh://git@github.com/jonschlinkert/is-even.git",
    "tarball-pkg": "https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz"
  }
}
```

## Target Lockfile Format

### Git dep (`github:` shorthand):
```
"git-pkg@github:jonschlinkert/is-odd":
  version: 3.0.1
  resolution: "is-odd@https://github.com/jonschlinkert/is-odd.git#commit=abc123"
  dependencies:
    is-number: "npm:^7.0.0"
  languageName: node
  linkType: hard
```

### Git dep (`git+ssh://`):
```
"git-ssh-pkg@git+ssh://git@github.com/jonschlinkert/is-even.git":
  version: 1.1.0
  resolution: "is-even@https://github.com/jonschlinkert/is-even.git#commit=abc123"
  languageName: node
  linkType: hard
```

### File dep:
```
"local-pkg@file:./local-pkg":
  version: 0.0.0
  resolution: "local-pkg@portal:./local-pkg::locator=locksmith-test-non-registry-deps%40workspace%3A."
  languageName: node
  linkType: soft
```

### Tarball URL dep (resolves to known npm package):
```
"tarball-pkg@https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz":
  version: 3.0.1
  resolution: "is-odd@npm:3.0.1"
  dependencies:
    is-number: "npm:^7.0.0"
  checksum: sha512-...
  languageName: node
  linkType: hard
```

## Changes

### 1. Add `ProjectName` to `berryConfig`

In `yarn/format_berry.go`, extend `berryConfig` (currently has 6 fields: MetadataVersion, CacheKey, ChecksumPrefix, IncludeRoot, SkipChecksum, RootDepsNpmPrefix):
```go
type berryConfig struct {
    // existing fields...
    ProjectName string // for portal: locator suffix
}
```

Set from `formatBerryWithConfig`:
```go
cfg.ProjectName = project.Name
```

### 2. Fix `buildConstraintMap` (lines 279-335)

**Problem**: For tarball URL deps, `targetKey` is computed as `"tarball-pkg@https://..."` but the Packages map uses `"is-odd@3.0.1"` (the resolved registry key).

**Fix**: When constraint is an `https://...tgz` URL, use `edge.Target.Name + "@" + edge.Target.Version` as targetKey:

```go
if isNonRegistryBerryConstraint(constraint) {
    if strings.HasPrefix(constraint, "https://") && strings.HasSuffix(constraint, ".tgz") {
        targetKey = edge.Target.Name + "@" + edge.Target.Version
    } else {
        targetKey = edge.Name + "@" + constraint
    }
}
```

### 3. Update `writeEntryBody` signature

Change to accept `berryConfig`:
```go
func writeEntryBody(b *strings.Builder, pkg *ResolvedPackage, constraintKey string, cfg berryConfig)
```

### 4. Fix resolution logic in `writeEntryBody`

Reorder conditions and add npm registry URL detection:

```go
url := node.TarballURL
if isNpmRegistryURL(url) {
    // Tarball URL dep resolved to npm package
    b.WriteString(fmt.Sprintf("  resolution: \"%s@npm:%s\"\n", node.Name, node.Version))
} else if strings.HasPrefix(url, "git+ssh://") || strings.HasPrefix(url, "git+https://") {
    // Git dep: convert git+ssh to https, append #commit=hash
    cleanURL := strings.Replace(url, "git+ssh://git@", "https://", 1)
    cleanURL = strings.Replace(cleanURL, "git+https://", "https://", 1)
    if idx := strings.Index(cleanURL, "#"); idx > 0 {
        hash := cleanURL[idx+1:]
        cleanURL = cleanURL[:idx] + "#commit=" + hash
    }
    b.WriteString(fmt.Sprintf("  resolution: \"%s@%s\"\n", node.Name, cleanURL))
} else if strings.HasPrefix(url, "file:") {
    // File dep: portal: with locator suffix
    portalPath := strings.TrimPrefix(url, "file:")
    encoded := urlEncodeProjectName(cfg.ProjectName)
    b.WriteString(fmt.Sprintf("  resolution: \"%s@portal:%s::locator=%s%%40workspace%%3A.\"\n",
        node.Name, portalPath, encoded))
} else {
    // Fallback: use npm resolution
    b.WriteString(fmt.Sprintf("  resolution: \"%s@npm:%s\"\n", node.Name, node.Version))
}
```

### 5. Dynamic linkType

```go
if strings.HasPrefix(node.TarballURL, "file:") {
    b.WriteString("  languageName: node\n")
    b.WriteString("  linkType: soft\n")
} else {
    b.WriteString("  languageName: node\n")
    b.WriteString("  linkType: hard\n")
}
```

### 6. Helper: `isNpmRegistryURL`

```go
func isNpmRegistryURL(url string) bool {
    return (strings.Contains(url, "registry.npmjs.org") ||
            strings.Contains(url, "registry.yarnpkg.com")) &&
           strings.HasSuffix(url, ".tgz")
}
```

### 7. Helper: `urlEncodeProjectName`

```go
func urlEncodeProjectName(name string) string {
    // Percent-encode @ as %40, / as %2F
    name = strings.ReplaceAll(name, "@", "%40")
    name = strings.ReplaceAll(name, "/", "%2F")
    return name
}
```

## Test Cases

### `yarn/format_berry_test.go`

1. **TestBerryGitDepEntry** - github: shorthand, verify constraint key, HTTPS resolution with `#commit=`, deps with `npm:` prefix, no checksum, `linkType: hard`
2. **TestBerryGitSSHDepEntry** - git+ssh:// style, verify HTTPS conversion with `#commit=`
3. **TestBerryFileDepEntry** - file: dep, verify portal: resolution with locator suffix, `linkType: soft`
4. **TestBerryTarballDepEntry** - tarball URL, verify `npm:version` resolution, checksum present, `linkType: hard`
5. **TestBerryMixedNonRegistryDeps** - all dep types together

Each test constructs `ResolveResult` directly (no registry calls) and calls the formatter.

## Integration Test

Remove skip in `testharness/integration_test.go:159-161`:
```go
// DELETE:
if fixture == "non-registry-deps" && vc.PMName == "yarn" && vc.PMVersion != "1" {
    t.Skip("yarn berry git dep entry format not yet implemented")
}
```

**Caveat**: Docker container may lack SSH keys for `git+ssh://` deps. May need to keep a narrower skip or use HTTPS-only git deps in the fixture.

## Potential Challenges

- **Portal locator suffix encoding**: Must match yarn berry's exact percent-encoding
- **Git dep without commit hash**: If GitHub API fails, TarballURL has empty hash. Omit `#commit=` suffix.
- **Transitive deps of git packages**: Already handled - they're normal registry deps
- **Yarn version differences**: All berry versions use `#commit=` format (verified)
