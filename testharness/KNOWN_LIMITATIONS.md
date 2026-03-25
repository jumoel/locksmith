# Known Limitations - Skipped Integration Tests

This documents test fixtures that are skipped in integration tests because they
exercise package manager behaviors that locksmith does not (yet) replicate.

## Skipped for all package managers

### non-registry-deps
`file:` and `git+` dependency specifiers reference local paths or git repos that
don't exist inside the Docker test runner. Locksmith creates placeholder nodes
for these but they can't be installed.

### aliased-dep
npm alias syntax (`"alias-name": "npm:real-name@^1.0.0"`) is resolved correctly
during generation, but the alias name is not preserved in all lockfile outputs.
The lockfile uses the real package name instead of the alias.

### zero-deps
Some package managers delete or reject lockfiles for projects with no
dependencies. Not a locksmith issue - just incompatible PM behavior.

### arborist-optional-missing
The fixture declares an optional dependency on a package that doesn't exist on
the npm registry (`@isaacs/this-does-not-exist-at-all`). Each package manager
handles this differently:
- pnpm 4/5: expects the specifier in the lockfile even if unresolved
- pnpm 9: expects it in the importer section
- yarn classic: tries to resolve from registry even in frozen mode
- npm: tolerates missing optional deps

Locksmith omits unresolvable optional deps from the lockfile, which is correct
for most PMs but not all.

### arborist-peer-cycle
The fixture has packages with circular peer dependencies
(`peer-dep-cycle-a -> b -> c -> (peer) a`). Each package manager resolves these
cycles differently. Locksmith's resolver handles cycles but the lockfile output
may not match what a specific PM version expects.

## Skipped for yarn berry only

### typescript-4, typescript-5
Yarn berry (v2+) applies internal compatibility patches to certain packages
(notably TypeScript) via the `patch:` protocol. These patches are specific to
each yarn version and maintained in yarn's built-in plugin database. Locksmith
cannot generate these patch entries because they require knowledge of yarn's
internal patch registry.

The generated lockfile is missing entries like:
```
"typescript@patch:typescript@^5.0.0#~builtin<compat/typescript>":
  resolution: "typescript@patch:typescript@npm%3A5.9.3#~builtin<compat/typescript>::version=5.9.3&hash=5786d5"
```

## Skipped for yarn@3.1 correctness

### yarn@3.1-v5 correctness
Yarn 3.1 (`yarn-berry-v5` format) uses its own checksum format that differs
from registry-provided integrity hashes. Locksmith cannot compute yarn 3.1's
cache-specific checksums, so the `SkipChecksum` flag is set for v4/v5 formats.
However, the correctness test compares locksmith output with yarn-generated
output, and the missing checksums cause a diff.
