# rulepack

`rulepack` is a Go CLI that composes instruction modules from Git-based rule packs and builds target-specific outputs for tools like Cursor, GitHub Copilot, and Codex.

[![Release](https://img.shields.io/github/v/release/alexgornovoi/rule-pack)](https://github.com/alexgornovoi/rule-pack/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/alexgornovoi/rule-pack)](https://github.com/alexgornovoi/rule-pack/blob/main/go.mod)

[Releases](https://github.com/alexgornovoi/rule-pack/releases) · [Issues](https://github.com/alexgornovoi/rule-pack/issues)

## Table of Contents

- [What it does](#what-it-does)
- [Install](#install)
- [Build From Source (Go)](#build-from-source-go)
- [Output modes](#output-modes)
- [Quick start](#quick-start)
- [Example user flows](#example-user-flows)
- [Commands](#commands)
- [Dependency resolution details](#dependency-resolution-details)
- [Output behavior](#output-behavior)
- [File reference](#file-reference)
- [Contributors](#contributors)

## What it does

- Resolves dependencies from Git URLs.
- Resolves dependencies from local filesystem rule packs.
- Pins each dependency in `rulepack.lock.json` (Git commit or local content hash).
- Loads `rulepack.json` from each dependency and expands selected modules.
- Applies local overrides (currently priority overrides).
- Produces deterministic, sorted output for configured targets.

## Install

### Homebrew (macOS/Linux)

```bash
brew tap alexgornovoi/homebrew-tap
brew install --cask rulepack
```

Or without tapping first:

```bash
brew install --cask alexgornovoi/homebrew-tap/rulepack
```

### Ubuntu PPA

```bash
sudo add-apt-repository ppa:alexgornovoi/rulepack
sudo apt update
sudo apt install rulepack
```

## Build From Source (Go)

Clone and build this repo:

```bash
git clone https://github.com/alexgornovoi/rule-pack.git
cd rule-pack
go build -o bin/rulepack ./cmd/rulepack
./bin/rulepack --help
```

Run without building:

```bash
go run ./cmd/rulepack --help
```

## Output modes

- Default: human-readable output with sections and tables.
- `--json`: machine-readable output for automation/scripts.
- `--no-color`: disable ANSI colors in human mode.

Examples:

```bash
rulepack profile list
rulepack install --json
rulepack build --no-color
```

## Quick start

Initialize a project:

```bash
go run ./cmd/rulepack init --name my-rules

# or scaffold a built-in "rules for writing rules" local pack
go run ./cmd/rulepack init --name my-rules --template rulepack
```

Add a dependency:

```bash
# Track a semver range
go run ./cmd/rulepack add https://github.com/org/rulepack.git --version "^1.2.0"

# Or pin a ref directly (commit/tag/branch)
go run ./cmd/rulepack add https://github.com/org/rulepack.git --ref v1.2.3
```

Resolve and lock:

```bash
go run ./cmd/rulepack install
```

Build outputs:

```bash
# all targets (default)
go run ./cmd/rulepack build

# single target
go run ./cmd/rulepack build --target codex
```

## Example user flows

### 1) Start a new project with shared rules

```bash
cd personal-website
# optional: rulepack init --name personal-website
rulepack add https://github.com/person-a/rules.git --export python
rulepack install
rulepack build
```

### 2) Save a profile once and reuse it everywhere

```bash
# in project A
rulepack profile save --dep 1 --alias python-a

# in project B
rulepack init --name project-b
rulepack profile use python-a
rulepack install
rulepack build
```

### 3) Use local rules while authoring

```bash
rulepack init --name my-project --template rulepack
rulepack install
rulepack build
```

### 4) Refresh a saved profile from source changes

```bash
# default: update existing profile in place
rulepack profile refresh python-a

# create a new profile id instead of updating in place
rulepack profile refresh python-a --new-id

# refresh only specific rules/modules
rulepack profile refresh python-a --rule python.* --rule ml.safety
```

### 5) Check for upstream updates before reinstalling

```bash
rulepack outdated
```

### 6) Preview what changed in a saved profile source

```bash
rulepack profile diff python-a
rulepack profile diff python-a --rule python.* --rule ml.*
```

## Commands

### `deps list`

List dependencies currently configured in `rulepack.json`, including lock status:

```bash
rulepack deps list
```

### `doctor`

Run quick diagnostics for project and environment health:

```bash
rulepack doctor
```

### `init`

Creates a starter `rulepack.json` with default targets:

- `cursor` -> `.cursor/rules` (`perModule: true`, extension `.mdc`)
- `copilot` -> `.github/copilot-instructions.md`
- `codex` -> `.codex/rules.md`

Flags:

- `--name <name>`: set rulepack name (defaults to current directory name).
- `--template rulepack`: scaffold `.rulepack/packs/rule-authoring` and add it as a local dependency.

### `add <git-url>`

Adds or replaces a dependency by URI in `rulepack.json`.
If `rulepack.json` does not exist in the current directory, `add` creates a default one automatically first.

Flags:

- `--export <name>`: choose a named export from `rulepack.json`.
- `--version <constraint>`: semver constraint (uses repo tags).
- `--ref <ref>`: commit/tag/branch.

`--version` and `--ref` are mutually exclusive.

### `remove <dep-selector> [dep-selector...]`

Removes one or more dependencies from `rulepack.json`.
Selectors can be 1-based index (`1`) or exact dependency reference (`uri`, `path`, or `profile id`).
This command updates only `rulepack.json`; run `rulepack install` afterward to refresh `rulepack.lock.json`.

```bash
rulepack remove 1
rulepack remove https://github.com/org/rules.git
rulepack remove 1 b4f97d30f0aa__python__2f9baf1a
```

`remove` also supports alias `uninstall`, and is available under `deps` as `rulepack deps remove ...` (or `rulepack deps uninstall ...`).

### `deps remove <dep-selector> [dep-selector...]`

Equivalent to top-level `remove`, but namespaced under `deps`:

```bash
rulepack deps remove 2
rulepack deps uninstall https://github.com/org/rules.git
```

### Local dependencies

You can also define dependencies directly in `rulepack.json` with `source: "local"` and a `path`:

```json
{
  "dependencies": [
    {
      "source": "local",
      "path": "../my-local-pack",
      "export": "default"
    }
  ]
}
```

Rules:

- `local` dependencies require `path`.
- `local` dependencies do not allow `uri`, `version`, or `ref`.
- Relative `path` values are resolved from the directory containing `rulepack.json`.
- `rulepack install` stores a `contentHash` in `rulepack.lock.json`.
- `rulepack build` recomputes the hash and fails with `local dependency changed; run rulepack install` if local content drifted.

### Apply modes (extensible targeting)

Modules in a source pack can define target-agnostic apply metadata and target-specific overrides.  
Cursor currently supports mapping of these modes into `.mdc` frontmatter:

- `always`
- `never`
- `agent`
- `glob`
- `manual`

Example module entry in a source `rulepack.json`:

```json
{
  "id": "python.ml",
  "path": "modules/python/ml.md",
  "priority": 120,
  "apply": {
    "default": { "mode": "agent", "description": "Apply when ML patterns are relevant" },
    "targets": {
      "cursor": { "mode": "glob", "globs": ["**/*.py"], "description": "Python files" }
    }
  }
}
```

### `install`

For each dependency:

- Mirrors/fetches the repository in cache.
- Resolves commit from `ref`, `version`, or `HEAD`.
- Validates expansion by reading `rulepack.json` and selected module files.
- Writes `rulepack.lock.json` with resolved commit and metadata.

### `build`

- Requires both `rulepack.json` and `rulepack.lock.json`.
- Requires lockfile order/URIs to match dependency list exactly.
- Expands modules at locked commits.
- Applies overrides, checks duplicate IDs, sorts by priority then ID.
- Writes target output(s).

Flag:

- `--target cursor|copilot|codex|all` (default `all`)

### `outdated`

Checks each dependency against the latest resolvable revision:

- For `git` dependencies: compares lockfile commit vs current resolved commit (`ref`, `version`, or `HEAD`).
- For `local` and `profile` dependencies: reported as `n/a` (no upstream check).

```bash
rulepack outdated
rulepack outdated --json
```

### `profile save`

Save one installed dependency as a globally reusable snapshot profile:

```bash
rulepack profile save --dep 1 --alias python-a
```

By default, this also switches the selected dependency to `source: "profile"` and refreshes lockfile.

### `profile list`

List globally saved profiles:

```bash
rulepack profile list
```

### `profile show`

Inspect one saved profile in detail:

```bash
rulepack profile show python-a
```

Sample human table (shape):

```text
Saved Profiles

Profiles
| Profile ID                 | Alias    | Source                         | Export | Modules | Created              |
|---------------------------|----------|--------------------------------|--------|---------|----------------------|
| b4f97d30f0aa__python__... | python-a | https://github.com/person/a... | python | 12      | 2026-02-27T10:00:00Z |
```

### `profile use`

Add/update current project to use a saved profile by ID or alias:

```bash
rulepack profile use python-a
```

### `profile refresh`

Refresh a saved profile from its original source.

Default behavior updates the existing profile in place:

```bash
rulepack profile refresh python-a
```

Create a new profile ID instead:

```bash
rulepack profile refresh python-a --new-id
```

Refresh only specific rules/modules by ID or pattern:

```bash
rulepack profile refresh python-a --rule python.* --rule ml.safety
```

Preview refresh changes without writing profile files:

```bash
rulepack profile refresh python-a --rule python.* --dry-run
```

### `profile diff`

Compare a saved profile snapshot to its source without writing anything:

```bash
rulepack profile diff python-a
rulepack profile diff python-a --rule python.* --rule ml.*
```

Saved profiles are stored globally under `~/.rulepack/profiles/<profile-id>`, so they can be reused across projects.
If two different repos both have a `python` export, they save as distinct profile IDs because source identity is part of profile ID generation.

Sample JSON output:

```json
{
  "command": "install",
  "result": {
    "lockFile": "rulepack.lock.json",
    "resolved": [
      {
        "index": 1,
        "source": "git",
        "ref": "https://github.com/person-a/rules.git",
        "export": "python",
        "resolved": "^1.2.0",
        "hash": "ab12cd34ef56"
      }
    ],
    "counts": {
      "git": 1,
      "local": 0,
      "profile": 0
    }
  }
}
```

### `profile` CI flow

```bash
rulepack install --json
rulepack build --json
```

Use JSON mode in automation; use default human mode for local development.

## Dependency resolution details

- Source types currently supported: `git`, `local`, `profile`.
- If `ref` is set, it resolves `ref^{commit}`.
- If `version` is set, tags are parsed as semver (leading `v` allowed), then highest matching version is selected.
- If neither is set, resolves `HEAD`.
- For `local`, the CLI loads the local pack directly from disk and pins `path` + `contentHash` in the lockfile.
- For `profile`, the CLI loads from `~/.rulepack/profiles/<profile-id>` and pins profile `contentHash`.

Repositories are cached under your user cache directory in a `rulepack` folder.

## Output behavior

- Module content is normalized to LF newlines.
- Merge order is deterministic: priority ascending, then module ID.
- Duplicate module IDs after composition are rejected.
- Cursor per-module output includes provenance headers plus one file per module.
- Copilot/Codex outputs are merged files without provenance headers.

## File reference

For full JSON schema and rule pack format details, see:

- `docs/rulepack-spec.md`

For a repository-local dogfood setup, see:

- `examples/README.md`

## Contributors

Contributions are welcome.

- Open an issue first for bugs, UX pain points, or feature proposals.
- Send a PR with a clear description of user impact and behavior changes.
- Include or update tests for CLI behavior changes (`human` + `--json` modes).
- For automation-focused changes, prefer deterministic outputs and document JSON shape updates.
