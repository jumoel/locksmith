# Plan: Fill Testing Gaps

## Overview

Five packages have zero test coverage. This plan adds unit tests in priority order by risk.

## Priority 1: `bun/resolve_test.go` and `bun/format_test.go`

The only PM with both resolver and formatter completely untested.

### `bun/resolve_test.go`

Define a bun-specific `mockRegistry` (matching the npm/pnpm pattern) with an extended `addVersionMeta` that accepts a full `ecosystem.VersionMetadata` for bun-specific metadata fields (PeerDeps, PeerDepsMeta, OptionalDeps, Bin, HasInstallScript).

Test cases:
1. **TestBunResolve_SingleDep** - resolves to highest matching version, packages map populated
2. **TestBunResolve_TransitiveDeps** - A -> B -> C chain, verify `DepInfo` structs (Constraint, ResolvedName, ResolvedVersion)
3. **TestBunResolve_PeerDepsSkipped** - peer dep edges NOT in `ResolvedPackage.Dependencies`, but `PeerDeps` populated from metadata
4. **TestBunResolve_OptionalDepsSkipped** - optional dep edges NOT in `Dependencies`, but `OptionalDeps` populated
5. **TestBunResolve_PeerDepsMeta** - mixed optional/required peers, verify `PeerDepsMeta` propagation
6. **TestBunResolve_BinMetadata** - verify `Bin` propagation from VersionMetadata
7. **TestBunResolve_HasInstallScript** - verify `HasInstallScript` propagation
8. **TestBunResolve_CrossTreeDedup** - A and B both depend on C@^1.0.0, only one C entry
9. **TestBunResolve_OptionalDepFails** - missing optional dep doesn't fail resolution
10. **TestBunResolve_CutoffDate** - cutoff filtering works

### `bun/format_test.go`

1. **TestBunLockFormatter_SimpleProject** - valid JSON output (after stripping trailing commas), correct top-level keys
2. **TestBunLockFormatter_DevAndOptionalDeps** - workspace entry has correct dep group sections
3. **TestBunLockFormatter_PackageEntryStructure** - 4-element array `[resolvedSpec, "", metadata, integrity]`
4. **TestBunLockFormatter_TrailingCommas** - test `addTrailingCommas` pure function directly
5. **TestBunLockFormatter_PeerDepsInMetadata** - metadata object has `peerDependencies` and `optionalPeers`
6. **TestBunLockFormatter_BinInMetadata** - metadata object has `bin` field
7. **TestBunLockFormatter_OsCpuMetadata** - `os`/`cpu` fields, single-element arrays become scalars
8. **TestBunLockFormatter_NormalizeBunCPU** - known values pass through, unknown become "none"
9. **TestBunLockFormatter_MultiVersionPathKeys** - different versions of same dep get bare vs path keys
10. **TestBunLockFormatter_DeterministicOutput** - format twice, byte-identical
11. **TestBunLockFormatter_FormatInterfaceReturnsError** - `Format()` returns expected error
12. **TestBunLockFormatter_SingleOrSlice** - single-element returns string, multi returns slice

## Priority 2: `internal/orderedjson/orderedjson_test.go`

Used by every formatter. Bug here silently corrupts all lockfile output.

1. **TestMap_MarshalJSON_PreservesOrder** - keys in insertion order, not sorted
2. **TestMap_MarshalJSON_NoHTMLEscaping** - `<`, `>`, `&` not escaped (critical for engine constraints)
3. **TestMap_MarshalJSON_EmptyMap** - produces `{}`
4. **TestMap_MarshalJSON_NestedMap** - nested Map values serialize correctly
5. **TestMap_MarshalJSON_MixedValueTypes** - string, int, bool, slice, nested Map
6. **TestMap_MarshalJSON_SpecialCharacters** - quotes, backslashes, unicode
7. **TestFromStringMap** - alphabetically sorted, empty map, single entry
8. **TestFromStringMapSorted** - keys sorted, correct Map values
9. **TestMap_MarshalJSON_UsedByEncoder** - works with `json.NewEncoder` (not just `json.Marshal`)

## Priority 3: `locksmith_test.go`

Root `Generate()` is the public API. Platform filtering and orphan cleanup have PM-specific logic.

**Important**: `applyPlatformFilter` and `unreachableKeys` are **unexported** (lowercase). Tests must be in package `locksmith` (internal test file), not `locksmith_test` (external test). Use `httptest.NewServer` for `Generate()` tests (avoids changing public API).

Pure function tests (no network, internal package test):
1. **TestUnreachableKeys_NilGraph** - returns nil
2. **TestUnreachableKeys_NilRoot** - returns nil
3. **TestUnreachableKeys_AllReachable** - empty map
4. **TestUnreachableKeys_OrphanedNode** - returns orphaned key
5. **TestUnreachableKeys_TransitiveReachability** - Root -> A -> B -> C, D orphaned
6. **TestApplyPlatformFilter_EmptyPlatform** - returns nil, no error
7. **TestApplyPlatformFilter_InvalidPlatform** - returns error
8. **TestApplyPlatformFilter_ValidPlatform** - correct keys removed

Integration-level tests (mock HTTP server):
9. **TestGenerate_UnknownFormat** - returns error
10. **TestGenerate_WithMockServer** - single-dep, verify non-empty lockfile
11. **TestGenerate_AllFormatsDispatch** - table-driven over `AllFormats()`, all produce non-empty output

## Priority 4: `ecosystem/nodeindex_test.go`

Critical dedup data structure. Bug causes silent duplicate or missed resolution.

1. **TestNodeIndex_Add_HasName** - add node, HasName true; unknown name false
2. **TestNodeIndex_HasName_Empty** - new index returns false
3. **TestNodeIndex_FindSatisfying_SingleVersion** - returns matching node
4. **TestNodeIndex_FindSatisfying_NoMatch** - returns nil
5. **TestNodeIndex_FindSatisfying_MultipleVersions** - returns first satisfying (insertion order)
6. **TestNodeIndex_FindSatisfying_InvalidVersion** - skips unparseable versions
7. **TestNodeIndex_MultiplePackages** - different names don't cross-match
8. **TestNodeIndex_FindSatisfying_EmptyName** - returns nil when name not in index

## Priority 5: `cmd/locksmith/main_test.go`

CLI flag parsing and validation.

1. **TestRootCmd_HasSubcommands** - "generate" and "version" present
2. **TestVersionCmd** - output contains "locksmith"
3. **TestIsValidFormat** - all valid formats true, invalid false
4. **TestValidFormatsStr** - contains all format names
5. **TestGenerateCmd_RequiredFlags** - error mentions required flags
6. **TestGenerateCmd_InvalidFormat** - error mentions "unknown format"
7. **TestGenerateCmd_CutoffDateParsing_RFC3339** - valid RFC3339 accepted
8. **TestGenerateCmd_CutoffDateParsing_DateOnly** - YYYY-MM-DD accepted
9. **TestGenerateCmd_CutoffDateParsing_Invalid** - error mentions formats

## Implementation Sequencing

**Phase 1** (pure functions, zero coupling):
- `internal/orderedjson/orderedjson_test.go`
- `ecosystem/nodeindex_test.go`

**Phase 2** (bun coverage):
- `bun/resolve_test.go`
- `bun/format_test.go`

**Phase 3** (public API):
- `locksmith_test.go`

**Phase 4** (CLI):
- `cmd/locksmith/main_test.go`

## Mock Registry Decision

Do NOT refactor npm/pnpm mocks into a shared package. The bun mock needs additional metadata fields that the others don't support. Keep each package self-contained, matching the existing convention. The bun mock should accept full `ecosystem.VersionMetadata` via `addVersionMeta` plus convenience wrappers matching the npm/pnpm API for simple cases.
