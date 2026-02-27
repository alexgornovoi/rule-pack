# rulepack

`rulepack` is a CLI for reusing AI agent rules across projects.
It lets you pull rules from git or local sources, lock them for reproducibility, build tool-specific outputs, and save reusable local profile snapshots.

[![Release](https://img.shields.io/github/v/release/alexgornovoi/rule-pack)](https://github.com/alexgornovoi/rule-pack/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/alexgornovoi/rule-pack)](https://github.com/alexgornovoi/rule-pack/blob/main/go.mod)

[Releases](https://github.com/alexgornovoi/rule-pack/releases) · [Issues](https://github.com/alexgornovoi/rule-pack/issues)

## Table of Contents

- [Why Rulepack](#why-rulepack)
- [Install](#install)
- [Basic Usage](#basic-usage)
- [Commands](#commands)
- [Advanced Usage](#advanced-usage)
- [Build From Source (Go)](#build-from-source-go)
- [Output modes](#output-modes)
- [Dependency resolution details](#dependency-resolution-details)
- [Output behavior](#output-behavior)
- [File reference](#file-reference)
- [Contributors](#contributors)

## Why Rulepack

- Reuse one shared rules library across many projects.
- Keep outputs deterministic with lockfiles.
- Build rules for Cursor, Copilot, and Codex from the same source.
- Save the current stack as a local profile and reuse it later, even if original sources disappear.

Typical use cases:

- Team-level rule pack in git, consumed by many repos.
- Personal local rule folders while iterating on prompts/rules.
- Snapshotting a "known-good" rules setup as a profile before starting a new project.

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

## Basic Usage

Start a new project, add rules, lock dependencies, and build outputs:

```bash
rulepack init --name my-project
rulepack deps add https://github.com/your-org/your-rules.git
rulepack deps install
rulepack build
```

### Example A: Python-only rules

```bash
rulepack init --name my-python-project
rulepack deps add https://github.com/your-org/your-rules.git --export python
rulepack deps install
rulepack build
```

### Example B: Python + ML rules from one repo

```bash
rulepack init --name my-ml-project
rulepack deps add https://github.com/your-org/your-rules.git --export python
rulepack deps add https://github.com/your-org/your-rules.git --export ml
rulepack deps install
rulepack build
```

`https://github.com/your-org/your-rules.git` is an example source. Replace it with your own rules repository URL.

If `--export` is omitted, Rulepack uses `exports.default` if present. If no `exports.default` exists, Rulepack implicitly selects all modules (`include: ["**"]`).

Optional variants:

```bash
# Pin a ref directly (commit/tag/branch)
rulepack deps add https://github.com/your-org/your-rules.git --ref v1.2.3

# Use a local rules repo
rulepack deps add --local ../my-rules --export python
rulepack deps install
rulepack build

# Optional: build only one target
rulepack build --target codex
```

## Commands

Global flags:

- `--json`: emit machine-readable JSON output. Default: `false` (human output).
- `--no-color`: disable ANSI colors in human output. Default: `false` (color enabled).

### `init`

Creates a starter `rulepack.json` with default targets.

Flags:

- `--name <name>`: set project/rulepack name.
  Default: current directory basename.
- `--template <template>`: scaffold template files. Supported value: `rulepack`.
  Default: empty (no template scaffold files).

### `deps add [git-url]`

Adds or replaces a dependency in `rulepack.json`.
Supported forms:

- `rulepack deps add <git-url>`
- `rulepack deps add --local <path>`

Flags:

- `--export <name>`: export name to select from dependency pack.
  Default: empty (Rulepack uses export fallback resolution).
- `--version <constraint>`: semver tag constraint.
  Default: empty.
- `--ref <ref>`: commit/tag/branch reference.
  Default: empty.
- `--local <path>`: local rulepack directory path.
  Default: empty (git mode).
- `--yes`: confirm risky dependency replacement without prompting.
  Default: `false`.

Notes:

- `--version` and `--ref` are mutually exclusive.
- `--version` and `--ref` are git-only flags and cannot be used with `--local`.
- If `--export` is not set: use `exports.default` when present; otherwise all modules (`include: ["**"]`).
- If neither `--version` nor `--ref` is set, dependency resolution uses `HEAD` during `rulepack deps install`.
- If `rulepack.json` is missing, `deps add` auto-initializes a default config first.
- Replacement (`add` against existing dependency URI/path) requires `--yes` in non-interactive or `--json` mode.

### `deps list`

Lists configured dependencies and lock status.

Flags:

- `--yes`: confirm dependency removal without prompting.
  Default: `false`.

### `deps remove <dep-selector> [dep-selector...]`

Removes one or more dependencies from `rulepack.json`.
Selectors accept:

- 1-based index (`1`)
- exact dependency reference (`uri`, `path`, or `profile id`)

Flags:

- none

Notes:

- Alias: `rulepack deps uninstall ...`
- Non-interactive or `--json` mode requires `--yes`.
- After removal, run:

```bash
rulepack deps install
rulepack build
```

### `deps install`

Resolves dependencies and writes `rulepack.lock.json`.

Flags:

- none

### `deps outdated`

Checks whether git dependencies have newer resolvable revisions.

Flags:

- none

### `build`

Builds target outputs from locked dependencies.

Flags:

- `--target <target>`: `cursor|copilot|codex|all`.
  Default: `all`.
- `--yes`: confirm unmanaged cursor overwrite collisions without prompting.
  Default: `false`.

Notes:

- If unmanaged cursor output collisions are detected, non-interactive or `--json` mode requires `--yes`.

### `doctor`

Runs diagnostics for config, lockfile, git client, and profile store.

Flags:

- none

### `profile save`

Saves dependencies as a reusable local profile snapshot.

Flags:

- `--alias <name>`: required in non-interactive mode; interactive prompts if omitted.
  Default: empty.
- `--dep <selector>`: save only one dependency instead of all.
  Default: empty (save all dependencies).
- `--switch`: replace project dependencies with the saved profile dependency.
  Default: `false` (leave project dependencies unchanged).

### `profile list`

Lists saved profiles from global store.

Flags:

- none

### `profile show <profile-id-or-alias>`

Shows metadata/details for one saved profile.

Flags:

- none

### `profile use <profile-id-or-alias>`

Adds/updates project dependency to use a saved profile.
This does not block adding other dependencies later; profile and non-profile dependencies can be composed.

Flags:

- none

### `profile remove <profile-id-or-alias>`

Deletes one saved profile.

Flags:

- `--yes`: skip interactive confirmation.
  Default: `false`.
- `--all`: remove all saved profiles (takes no positional arg).
  Default: `false`.

Notes:

- Alias: `rulepack profile delete ...`
- Non-interactive mode requires `--yes`.

### `profile diff <profile-id-or-alias>`

Compares profile snapshot modules with current source state.

Flags:

- `--rule <pattern>` (repeatable): limit comparison to specific module IDs/patterns.
  Default: none (compare all modules).

### `profile refresh <profile-id-or-alias>`

Refreshes profile snapshot from current source state.

Flags:

- `--new-id`: write refreshed snapshot to a new profile ID.
  Default: `false` (refresh in place).
- `--rule <pattern>` (repeatable): refresh only selected module IDs/patterns.
  Default: none (refresh all modules).
- `--dry-run`: preview without writing profile files.
  Default: `false` (writes snapshot updates).
- `--yes`: confirm risky in-place refresh updates without prompting.
  Default: `false`.

Notes:

- In-place refreshes with module diffs require `--yes` in non-interactive or `--json` mode.

## Advanced Usage

### 1) Save a profile once and reuse it everywhere

```bash
# in project A
rulepack profile save --alias python-a

# in project B
rulepack init --name project-b
rulepack profile use python-a
rulepack deps install
rulepack build
```

### 2) Use local rules while authoring

```bash
rulepack init --name my-project --template rulepack
rulepack deps install
rulepack build
```

### 3) Refresh a saved profile from source changes

```bash
# default: update existing profile in place
rulepack profile refresh python-a

# create a new profile id instead of updating in place
rulepack profile refresh python-a --new-id

# refresh only specific rules/modules
rulepack profile refresh python-a --rule python.* --rule ml.safety
```

### 4) Check for upstream updates before reinstalling

```bash
rulepack deps outdated
```

### 5) Preview what changed in a saved profile source

```bash
rulepack profile diff python-a
rulepack profile diff python-a --rule python.* --rule ml.*
```

## Build From Source (Go)

Clone and build this repo:

```bash
git clone https://github.com/alexgornovoi/rule-pack.git
cd rule-pack
go build -o bin/rulepack ./cmd/rulepack
./bin/rulepack --help
```

Run without building (development only):

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
rulepack deps install --json
rulepack build --no-color
```

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

## License

This project is licensed under the MIT License.
See `LICENSE` for details.
