# Rulepack Spec

This document describes the current JSON shapes and runtime behavior implemented by this repository.

## `rulepack.json`

Top-level schema:

```json
{
  "specVersion": "0.1",
  "name": "my-rulepack",
  "dependencies": [],
  "overrides": [],
  "targets": {}
}
```

### Fields

- `specVersion` (string, required): currently validated as non-empty.
- `name` (string): human-readable rulepack name.
- `dependencies` (array):
  - `source` (string): `"git"`, `"local"`, or `"profile"`. If omitted, treated as `"git"` for compatibility.
  - `uri` (string, required for git): Git clone URL.
  - `path` (string, required for local): local filesystem path to a rule pack directory.
  - `profile` (string, required for profile): saved profile ID from global profile storage.
  - `version` (string, optional): semver constraint against tags.
  - `ref` (string, optional): commit/tag/branch ref.
  - `export` (string, optional): named export from dependency `rulepack.json`.
- `overrides` (array):
  - `id` (string, required): module ID to override.
  - `priority` (number, optional): replacement priority.
- `targets` (object map):
  - Key is target name (`cursor`, `copilot`, `codex`).
  - Value:
    - `outDir` (string, optional)
    - `outFile` (string, optional)
    - `perModule` (bool, optional; used by cursor renderer)
    - `ext` (string, optional; used by cursor renderer)

### Target defaults from `rulepack init`

- `cursor`: `outDir=.cursor/rules`, `perModule=true`, `ext=.mdc`
- `copilot`: `outFile=.github/copilot-instructions.md`
- `codex`: `outFile=.codex/rules.md`

## `rulepack.lock.json`

Written by `rulepack install`.

```json
{
  "lockVersion": "0.1",
  "resolved": [
    {
      "source": "git",
      "uri": "https://github.com/org/repo.git",
      "requested": "^1.2.0",
      "resolvedVersion": "1.3.4",
      "commit": "abcdef1234...",
      "export": "default"
    },
    {
      "source": "local",
      "path": "../my-local-pack",
      "commit": "local",
      "contentHash": "2f9baf...",
      "export": "default"
    },
    {
      "source": "profile",
      "profile": "b4f97d30f0aa__python__2f9baf1a",
      "commit": "profile",
      "contentHash": "2f9baf...",
      "export": "default"
    }
  ]
}
```

### Fields

- `lockVersion` (string): current value is `0.1`.
- `resolved` (array):
  - `source` (string): `git` or `local`. Missing values are treated as `git` for compatibility.
  - `uri` (string): dependency URI.
  - `path` (string, optional): local dependency path (stored relative to the directory containing `rulepack.json`, with `/` separators).
  - `profile` (string, optional): saved profile ID for profile source.
  - `requested` (string): request used to resolve (`ref`, `version`, or `HEAD`).
  - `resolvedVersion` (string, optional): populated for semver resolution.
  - `commit` (string): resolved commit SHA.
  - `contentHash` (string, optional): deterministic hash for local dependency content.
  - `export` (string, optional): copied from dependency.

### Lock/build consistency checks

At build time:

- `len(dependencies)` must equal `len(lock.resolved)`.
- Each index `i` must have matching `dependencies[i].uri == lock.resolved[i].uri`.

If either check fails, `build` errors with a lockfile mismatch.

## Dependency and Git resolution behavior

Given one dependency:

1. Mirror repo in cache (or fetch if cached).
2. Resolve commit:
   - If `ref` is set: resolve `<ref>^{commit}`.
   - Else if `version` is set:
     - list tags
     - parse semver tags (leading `v` is stripped)
     - select highest version matching constraint
     - resolve selected tag to commit
   - Else: resolve `HEAD`.
3. Load `rulepack.json` at resolved commit.
4. Expand selected modules and read each module file at that commit.

For local dependencies:

1. Resolve `path` from the directory containing `rulepack.json`.
2. Load local `rulepack.json`.
3. Expand selected modules and read module files from disk.
4. Compute a deterministic `contentHash` from pack metadata, selected module metadata, and normalized module content.
5. Store `path` + `contentHash` in lockfile.

For profile dependencies:

1. Resolve profile ID from global store (`~/.rulepack/profiles/<id>`).
2. Load profile snapshot `rulepack.json`.
3. Expand selected modules and hash content.
4. Store `profile` + `contentHash` in lockfile.

## Global profile storage

Saved profiles live in:

- `~/.rulepack/profiles/<profile-id>/`

Directory contents:

- `profile.json`: metadata (`id`, `alias`, `sourceType`, `sourceRef`, `sourceExport`, `createdAt`, `contentHash`, `moduleCount`, `provenance`)
- `rulepack.json`: snapshot rule pack manifest
- `modules/`: snapshotted module files

## Rule pack format (`rulepack.json`)

```json
{
  "specVersion": "0.1",
  "name": "pack-name",
  "version": "1.2.3",
  "modules": [
    {
      "id": "general.style",
      "path": "modules/general/style.md",
      "priority": 100,
      "appliesTo": ["go", "backend"],
      "apply": {
        "default": {
          "mode": "agent",
          "description": "Apply when frontend component guidance is relevant"
        },
        "targets": {
          "cursor": {
            "mode": "glob",
            "globs": ["**/*.py"],
            "description": "Python files"
          }
        }
      }
    }
  ],
  "exports": {
    "default": {
      "include": ["**"],
      "appliesTo": []
    },
    "backend": {
      "include": ["backend.*"],
      "appliesTo": ["go", "python"]
    }
  }
}
```

### Required metadata

`specVersion`, `name`, and `version` must be non-empty.

### Export selection

- If dependency `export` is set, that named export must exist.
- If not set:
  - use `exports.default` if present,
  - otherwise implicit selector: `{"include":["**"]}`.

### Module selection

A module is selected when:

1. Its `id` matches any include pattern.
2. Applies-to filtering passes.

Include matching supports:

- `**` or `*` as wildcard-all.
- `path.Match`-style pattern matching.
- Prefix-star fallback (`foo*` matches IDs starting with `foo`).

Applies-to behavior:

- If selector `appliesTo` is empty, no applies-to filtering is applied.
- If selector `appliesTo` is non-empty:
  - modules with empty `appliesTo` are allowed,
  - modules with non-empty `appliesTo` must intersect selector set.

Selected modules are sorted by `priority`, then `id`.

### Target-agnostic apply metadata

Each module can define optional `apply` metadata:

- `apply.default`: fallback behavior for targets.
- `apply.targets.<target>`: target-specific override.

Supported modes:

- `always`: always apply (default when unspecified)
- `never`: never apply for that target
- `agent`: agent-decided using `description`
- `glob`: apply by `globs` patterns (requires non-empty `globs`)
- `manual`: only apply when explicitly invoked by the target workflow

Current Cursor renderer mapping:

- `always` -> `alwaysApply: true`
- `agent` -> `alwaysApply: false` + `description`
- `glob` -> `alwaysApply: false` + `globs`
- `manual` -> `alwaysApply: false` + manual description
- `never` -> module is omitted from cursor output

## Build composition behavior

After all dependencies are expanded:

1. Apply overrides by exact module `id`.
2. Reject duplicate module IDs.
3. Sort by `priority`, then `id`.
4. Render target outputs.

For local dependencies during `build`, the CLI recomputes `contentHash` and compares against lockfile. If it differs, build fails with:

- `local dependency changed; run rulepack install`

For profile dependencies during `build`, the CLI recomputes `contentHash` and compares against lockfile. If it differs, build fails with:

- `profile snapshot drift detected; run rulepack install`

## Render targets

### Cursor (`target=cursor`)

Defaults:

- `outDir=.cursor/rules`
- `ext=.mdc`

If `perModule=true`:

- One file per module, named:
  - `<priority-3-digit>-<sanitized-id><ext>`
- `id` sanitization:
  - `.` replaced with `_`
  - non `[a-zA-Z0-9._-]` replaced with `_`
- File content:
  - provenance header comment
  - blank line
  - module content

If `perModule=false`:

- Write single merged file (`outFile` or `<outDir>/rules<ext>`)
- Includes provenance headers before module content.

### Copilot (`target=copilot`)

- Writes merged output to configured `outFile`.
- No provenance headers.

### Codex (`target=codex`)

- Writes merged output to configured `outFile`.
- No provenance headers.

## Content normalization

For module content and rendered output:

- CRLF and CR converted to LF.
- Output is trimmed of trailing blank lines and ends with exactly one newline.

## CLI machine output

`rulepack` supports machine-readable output with `--json`.

Envelope shape:

```json
{
  "command": "install",
  "result": {}
}
```

Error shape:

```json
{
  "command": "error",
  "result": {
    "failedCommand": "install",
    "error": {
      "message": "..."
    }
  }
}
```
