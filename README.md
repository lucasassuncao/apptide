<!-- markdownlint-disable MD033 -->
<p align="center">
  <img src="docs/apptide.png" alt="AppTide logo" width="360">
</p>
<!-- markdownlint-enable MD033 -->

🌊 AppTide is a unified package manager CLI for Windows that orchestrates installations, updates, and lifecycle management across multiple package sources with a single declarative configuration.

## Overview

AppTide streamlines the management of your Windows development environment and applications. Instead of managing winget, chocolatey, scoop, and GitHub releases separately, AppTide lets you describe your entire software stack in a single YAML file and operate on it through a command-line interface, an interactive browser, and a config editor.

A config file declares **applications**, not packages. One application may be obtainable from several package managers, listed in preference order — the order applies to the *first* installation only. Once installed, the application is bound to the source that succeeded, and upgrades and removals always go through that same manager.

## Key Features

* **Multi-Source Support**: install and upgrade applications from winget, chocolatey, scoop, and GitHub releases
* **Source Preference Lists**: declare `source: [winget, scoop, github]` — later entries are fallbacks used only when an earlier manager does not offer the package
* **Ownership Tracking**: a local state file records which source installed each application, so upgrades and removals never go through the wrong manager
* **Declarative Configuration**: define your stack once in `packages.yaml`, version control it, and replicate it across machines
* **Config Imports**: split the configuration across files and compose them with `import:` — relative paths, per-file defaults, circular-import detection
* **Interactive Browser** (`apptide tui`): config, installed packages, available upgrades, and drift side by side — install and remove from the same screen
* **Interactive Editor** (`apptide edit`): edit the YAML in a two-panel TUI with per-field hints and validation on save
* **Tags & Categories**: group applications and filter runs with `--category` or `--tags`
* **Lifecycle Hooks**: separate `pre_install` / `post_install` and `pre_uninstall` / `post_uninstall` commands
* **Validation & Verification**: errors *and* warnings for fields a given source silently ignores
* **Structured Output**: `list`, `verify`, `doctor`, `install` and `adopt --list` support `--output json` for scripting and CI
* **Self-Updating**: AppTide can detect and apply its own updates

## Installation

Pre-built binaries are available for Windows. Extract the binary to a location in your PATH:

```
# Download AppTide from releases
# Extract apptide.exe to your Windows PATH or a custom directory
```

Or build from source:

```bash
git clone https://github.com/lucasassuncao/apptide.git
cd apptide
go build -ldflags "-X github.com/lucasassuncao/apptide/cmd.Version=v1.0.0" -o apptide.exe .
```

## Getting Started

### 1. Create Your Configuration

Generate a starting point:

```bash
apptide init                 # minimal template with a field reference
apptide init -t example      # one application per source type
apptide init -i              # interactive wizard
```

Or write `packages.yaml` by hand:

```yaml
schema_version: 2

applications:
  - name: "VS Code"
    category: Development
    source: winget
    description: "The open-source AI code editor"
    info_url: "https://code.visualstudio.com/"
    package:
      winget:
        id: "Microsoft.VisualStudioCode"

  - name: "Git"
    category: Development
    source: winget
    description: "Distributed version control system"
    package:
      winget:
        id: "Git.Git"

  - name: "jq"
    category: CLITools
    source: winget
    description: "Command-line JSON processor"
    package:
      winget:
        id: "jqlang.jq"
```

Without `--config`, every command looks for the config in a fixed order; see
[Where the config is read from](#where-the-config-is-read-from) below.

### 2. Run Commands

```bash
apptide list        # what the config declares
apptide validate    # errors and warnings in the config
apptide install     # install or upgrade everything marked action: install
apptide verify      # what the machine actually has (changes nothing)
apptide tui         # browse config and machine side by side
apptide doctor      # is each package manager present and working?
```

### Where the config is read from

With no `--config`, apptide takes the first of these that exists:

1. `./packages.yaml` in the current directory
2. `%APPDATA%\apptide\packages.yaml`
3. `<binary-dir>\conf\packages.yaml`

The third is the historical location and is kept for compatibility. It is a
poor home for a config: installed through scoop or winget, apptide lives in a
shim directory that is replaced on every upgrade, taking the file with it.

## Commands

| Command | Description |
|---------|-------------|
| `list` | Display all configured applications and their metadata |
| `list --categories` | List only category names, with application counts |
| `install` | Install or upgrade every application marked `action: install` |
| `verify` | Check which applications are installed and detect version mismatches |
| `validate` | Check the config for errors and for fields a source will ignore |
| `tui` | Interactive browser over the config and the machine |
| `edit` | Edit the configuration file in an interactive TUI |
| `adopt --list` | Show every application apptide tracks, and which source owns it |
| `adopt <app> --source <src>` | Record which source manages an application |
| `export` | Export currently installed packages as a `packages.yaml` |
| `init` | Create a new `packages.yaml` from a template or a wizard |
| `doctor` | Diagnose system health and package manager availability |
| `doctor --install-missing` | Attempt to install any missing package managers |
| `self-update` | Download and apply the latest AppTide release |
| `--version` | Print the current AppTide version |

Global flags: `--config, -c <path>` and `--output, -o table|json`.

## Configuration Format

A v2 config has up to four top-level keys plus `import:`:

```yaml
schema_version: 2   # the format this build writes and understands

settings:           # run-wide options; only read from the file named on the CLI
  install_dir: "${LOCALAPPDATA}\\apptide\\bin"
  add_to_path: true
  github_token: "${GITHUB_TOKEN}"

defaults:           # applied to every application in THIS file that omits the field
  category: Development
  source: winget
  action: install
  tags: [work]

applications:
  - name: ...
```

### `settings`

Run-wide options that would otherwise have to be repeated as flags on every invocation. `${VAR}` is expanded from the environment, so a config can name a path or a token without embedding either. A command-line flag always wins over the file, and settings are read only from the file passed to the command — an imported file describes applications, not how the run is configured.

* `install_dir`: where github binaries land (default: `%LOCALAPPDATA%\apptide\bin`)
* `add_to_path`: add `install_dir` to the user PATH after installing a binary
* `github_token`: raises the GitHub API rate limit

### `defaults`

Resolved at load time and file-scoped: an imported file never inherits the importer's defaults. Supports `category`, `source`, `action`, and `tags` (default tags are prepended to each application's own).

### Application fields

* `name` (required): display name — also the key under which AppTide records which source installed it
* `source` (required): one source, or a preference list. `winget`, `chocolatey`, `scoop`, `github`. Both forms work:

  ```yaml
  source: winget
  source: [winget, scoop, github]
  ```

* `package` (required): a block per source listed above — see [Per-source blocks](#per-source-blocks)
* `category` (optional): free-form group, used by `--category`. Applications without one are grouped under `Uncategorized`
* `action` (optional, default `install`): `install`, `uninstall`, or `skip`
* `version` (optional, default latest): honoured by winget and chocolatey (`--version`) and by github (it becomes the release tag). Scoop cannot pin versions — a pinned version together with scoop is a validation warning
* `description` (optional): informational only
* `info_url` (optional): project homepage
* `tags` (optional): free-form labels, used by `--tags`
* `skip_upgrade` (optional): install when missing, never upgrade afterwards. No effect when github is the only source — that source has no upgrade path
* `optional` (optional): a failure here does not fail the run. Sixty applications should not exit non-zero, and break CI, because of one package already known to be flaky
* `requires_admin` (optional): state that this application needs elevation. AppTide otherwise guesses from the source and its options, and the guess is wrong in both directions — a portable chocolatey package needs nothing, and a winget installer can demand elevation without saying so
* `hooks` (optional): see [Lifecycle Hooks](#lifecycle-hooks)

### Per-source blocks

Every source listed in `source:` must have its block filled in under `package:`. A block present for a source that is *not* listed is dead config, and `validate` reports it.

#### `package.winget`

* `id` (required): package identifier, e.g. `"Publisher.App"`
* `args`: extra arguments appended to `winget install`/`upgrade`
* `scope`: `machine` or `user` → `--scope`
* `locale`: installer locale, e.g. `pt-BR` → `--locale`
* `feed`: winget source to resolve against (`winget`, `msstore`, a private feed) → `--source`
* `override`: string passed verbatim to the underlying installer → `--override`

#### `package.chocolatey`

* `id` (required): package identifier, e.g. `"googlechrome"`
* `args`: extra arguments appended to `choco install`/`upgrade`
* `package_params`: parameters for the chocolatey package script → `--package-parameters`
* `install_args`: arguments for the native installer → `--install-arguments`
* `feed`: package feed to install from → `--source`
* `allow_downgrade`: permit installing an older version → `--allow-downgrade`

`package_params` and `install_args` are two distinct channels in chocolatey — the first reaches the package script, the second the installer it wraps.

#### `package.scoop`

* `id` (required): app name, e.g. `"vim"`
* `args`: extra arguments appended to `scoop install`/`update`
* `bucket`: bucket that provides the app — added automatically when missing
* `global`: install for all users (requires an elevated shell) → `--global`
* `arch`: `64bit`, `32bit`, or `arm64` → `--arch`

#### `package.github`

* `id` (required): `"owner/repo"`
* `asset_pattern`: glob forcing a specific asset, e.g. `"*windows_amd64*.zip"`
* `run_installer`: execute the `.exe`/`.msi` instead of copying the binary
* `install_dir`: override the default binary directory for this application
* `binary_name`: binary name when it differs from `name`, e.g. `gh` for `name: "GitHub CLI"`
* `prerelease`: accept pre-release tags. GitHub's "latest release" endpoint excludes them, so a repository that only publishes pre-releases resolves to nothing without this
* `checksum`: expected SHA-256 of the asset, with an optional `sha256:` prefix. It pins the exact bytes, which the release's own checksum file cannot do, and only makes sense alongside a pinned `version`
* `args`: arguments for the **downloaded installer**, not for a package manager — the one field whose meaning differs from the other sources. Ignored unless `run_installer: true`

Asset selection matches the machine's architecture: on Windows on ARM a native `arm64` asset wins, and an `x86_64` one is taken only as an emulated fallback. Downloads are checked against the release's checksum file when it publishes one, and against `checksum` when it is set.

## Example Configuration

```yaml
schema_version: 2

settings:
  install_dir: "${LOCALAPPDATA}\\apptide\\bin"
  add_to_path: true
  github_token: "${GITHUB_TOKEN}"

applications:
  - name: "Chrome"
    category: Browsers
    source: winget
    description: "Google Chrome web browser"
    info_url: "https://www.google.com/chrome/"
    package:
      winget:
        id: "Google.Chrome"

  - name: "GoLang"
    category: Development
    source: winget
    tags: [work]
    package:
      winget:
        id: "GoLang.Go"
        scope: machine

  # Preference list: winget first, then scoop, then a GitHub release.
  # Whichever succeeds is recorded and used for upgrades and removal.
  - name: "Lazygit"
    category: Development
    source: [winget, scoop, github]
    description: "Terminal UI for git"
    package:
      winget:
        id: "JesseDuffield.lazygit"
      scoop:
        id: "lazygit"
        bucket: extras
      github:
        id: "jesseduffield/lazygit"
        asset_pattern: "*Windows_x86_64.zip"

  - name: "Docker Desktop"
    category: DevOps
    source: chocolatey
    requires_admin: true
    optional: true
    package:
      chocolatey:
        id: "docker-desktop"
        install_args: "--quiet"

  - name: "jq"
    category: CLITools
    source: winget
    version: "1.7.1"
    skip_upgrade: true
    package:
      winget:
        id: "jqlang.jq"

  - name: "FFmpeg"
    category: CLITools
    source: winget
    action: skip
    package:
      winget:
        id: "Gyan.FFmpeg"
```

## Advanced Usage

### Source Ownership and `adopt`

The preference order in `source:` decides the *first* installation. From then on the application is bound to whichever source succeeded, recorded in `%LOCALAPPDATA%\apptide\state.json` — a small, human-readable JSON file you can inspect or attach to a bug report. Writes are atomic and serialized across processes.

AppTide never re-decides the binding on its own: doing so would mean uninstalling and reinstalling software because a third-party catalog changed. `adopt` is the explicit way to change it. It only rewrites the binding — nothing is installed or removed.

```bash
# Show what apptide tracks, and which source owns each application
apptide adopt --list
apptide adopt --list --output json

# Two managers report Neovim as installed — declare which one owns it
apptide adopt Neovim --source scoop

# Stop tracking an application entirely
apptide adopt Neovim --forget
```

`adopt --list` is the only view of the state file, and answers a different question from the other two listings:

| Command | Answers |
|---|---|
| `list` | what the config **declares** |
| `verify` | what the machine **has** |
| `adopt --list` | what apptide **believes it installed**, and through which source |

The application is named by its config `name:`. Names are normalized the way the state file keys them — lowercased, runs of whitespace joined by a hyphen — so `"VS Code"`, `"vs code"` and `vs-code` all reach the same entry. That normalized key is what `--list` prints.

A source not listed in the application's `source:` is rejected: binding there would make the next `install` fail with "not offered", far from the cause.

### The Interactive Browser

```bash
apptide tui
```

Five tabs:

| Tab | Contents |
|-----|----------|
| Config | every declared application, with the source that manages it |
| Installed | what the machine actually has, and whether the config tracks it |
| Upgradable | packages with a newer version available |
| Drift | only the applications where config and machine disagree |
| Activity | progress and results of what you apply |

Select rows with space, then act on them — nothing runs before a confirmation listing every change. The config file itself is edited with `apptide edit`; `tui` reads it and acts on the machine.

| Tab | Keys |
|---|---|
| Config, Drift | `i` install, `r` remove, `x` run the whole config |
| Installed | `A` add the selection to the config, `y` copy it as config entries |
| Upgradable | `i` upgrade |
| Activity | `R` refresh |

On every list, `←`/`→` fold and unfold a group, `/` filters, and `?` shows every key.

Two keys act on apptide's own record instead of on the machine:

| Key | Does |
|---|---|
| `a` | set which source owns the application — same as `apptide adopt <app> --source <src>` |
| `f` | forget it: drop apptide's record. **Nothing is uninstalled** — same as `apptide adopt <app> --forget` |

`f` also reaches entries no other view can name: an application installed by apptide and later removed from the config leaves a record keyed by a name the config no longer contains. Those show up on the Installed tab, where `f` matches them by source and package id.

### The Config Editor

```bash
apptide edit                        # edit the resolved config
apptide edit --theme grape          # pick a theme
apptide edit --list-themes          # browse the available themes
apptide edit --out work.yaml        # load one file, save to another
apptide edit --dump                 # record a session trace for a bug report
```

The left panel lists the top-level blocks; Enter opens the block editor, where each field gets a type-appropriate control and a hint panel explaining what it does. `Tab` changes pane, `Ctrl+S` saves, `Ctrl+U` undoes, `Ctrl+Y` redoes, `Esc` goes back a level and `q` quits; `?` lists every key. Saving validates the whole config first, against the rules declared with each field (required, allowed values, patterns) and the cross-field checks; `--no-validate-on-save` lifts that. `apptide validate` is a separate check on the loaded config, imports resolved, and also warns about fields a source ignores. The top-level `import:` and `schema_version:` keys are preserved byte-for-byte and are not offered as editable blocks: `import:` is resolved by the loader before the file is decoded, and `schema_version:` is written by AppTide — a file without it is read as the current version, and an older one is converted on read, so editing the number by hand cannot change what the file is.

`p` opens a picker, with a preview of what will be inserted:

| Where | What it offers |
|---|---|
| Root list | Whole-file templates — the same ones `apptide init --template` writes |
| Block editor | Ready-made blocks: a winget/chocolatey/scoop application, a github binary or installer, a source fallback list, a pinned version, hooks, an uninstall entry, `settings` and `defaults` variants |

`Enter` replaces the block with the selection; on a list block, `a` appends it instead, so applications can be stacked one preset at a time.

### Splitting Configuration with Imports

Split the configuration across files and compose them with the top-level `import:` key. Paths resolve relative to the file that declares them, so imports work regardless of where you invoke AppTide from.

```yaml
import:
  - ./configs/gaming.yaml
  - ./configs/work.yaml
  - ../envs/prd/packages.yaml

schema_version: 2

applications:
  - name: "Git"
    source: winget
    package:
      winget:
        id: "Git.Git"
```

* Imported files can themselves contain `import:` entries (recursive)
* Each file's `defaults:` block applies only to that file's applications
* `settings:` is read only from the file named on the command line
* Circular imports are detected and reported as an error

Pass any config file to any command with `--config`:

```bash
apptide install --config ./envs/prd/packages.yaml
apptide list    --config ./configs/gaming.yaml
```

### Lifecycle Hooks

Shell commands run around the action, via `cmd /C`. Install and uninstall have their own pair: a command named for installing must not run when the application is being removed.

```yaml
applications:
  - name: "Go"
    source: winget
    package:
      winget:
        id: "GoLang.Go"
    hooks:
      pre_install: "echo Installing Go..."
      post_install: "go env -w GOPATH=C:/go"

  - name: "Node.js"
    source: winget
    package:
      winget:
        id: "OpenJS.NodeJS"
    hooks:
      post_install: "npm install -g yarn"
      post_uninstall: "rmdir /s /q %APPDATA%\\npm"
```

**Behaviour:**

* `pre_install` / `pre_uninstall` failure (non-zero exit) → the action is **aborted**, row shown as `failed`
* `post_install` / `post_uninstall` failure → row shown as `ok` with a **warning** detail
* No hook runs in `--dry-run` mode or when a package is already up to date
* A hook's output is captured and attached to the failure, so the row says what went wrong rather than only `exit status 1`
* Ctrl+C cancels a running hook along with the rest of the run

> A config is executable content, not data. Hooks run arbitrary shell commands,
> and `import:` pulls in whatever the imported file declares, hooks included.
> Read a config you did not write before running it.

### Filtering a Run

`install` and `verify` accept `--category`, `--source`, and `--tags`:

```bash
apptide install --category Development
apptide install --tags work,cli
apptide verify  --source github
apptide install --dry-run           # simulate, execute nothing
```

`--tags` matches an application carrying **any** of the listed tags.

### Structured JSON Output

`list`, `verify`, `doctor`, `install` and `adopt --list` support `--output json` (short: `-o json`) for scripting and CI. In JSON mode, TUI output is suppressed and only valid JSON goes to stdout; errors, warnings, and progress go to stderr.

```bash
apptide list --output json
apptide verify --output json | jq '[.[] | select(.status == "not_found")]'
apptide doctor --output json
apptide install --output json | jq '.summary'
apptide install --dry-run --output json | jq -r '.applications[] | select(.status=="would_install") | .name'
```

`list` output shape:

```json
[{
  "category": "...", "name": "...", "sources": ["winget", "scoop"],
  "action": "install", "version": "...", "tags": ["..."], "description": "..."
}]
```

`verify` output shape:

```json
[{
  "name": "...", "category": "...", "sources": ["winget"],
  "resolved_source": "winget", "action": "install", "installed": true,
  "current_version": "...", "status": "installed", "detail": "..."
}]
```

`doctor` output shape:

```json
{
  "managers": [{ "name": "winget", "available": true, "version": "...", "path": "...", "auto_installable": true }],
  "apptide": { "install_dir": "...", "install_dir_exists": true, "in_path": false },
  "updater_repo": "",
  "all_ok": false
}
```

`install` is the one action command, so instead of a bare array it emits a document with the run-level facts a CI job needs:

```json
{
  "dry_run": false,
  "applications": [{
    "name": "Lazygit", "category": "Development",
    "sources": ["winget", "scoop"], "resolved_source": "scoop", "via_fallback": true,
    "action": "install", "status": "installed",
    "previous_version": "0.41.0", "version": "0.42.0",
    "optional": false, "detail": ""
  }],
  "summary": { "total": 1, "ok": 1, "skipped": 0, "failed": 0, "failed_required": 0 }
}
```

Row `status` values: `installed`, `uninstalled`, `up_to_date`, `already_installed`, `already_uninstalled`, `skip`, `conflict`, `failed`, and — in `--dry-run` only — `would_install` / `would_uninstall`. A dry run never reports work it did not do.

`summary.failed_required` excludes applications marked `optional` and is what the exit code follows: non-zero only when a required application failed.

### Validation

`validate` separates two kinds of problem. **Errors** stop a run: a missing `name`, an unknown source, a `package.<source>.id` missing for a source that is listed, a github `id` that is not `owner/repo`, `action: uninstall` on a github-only application. **Warnings** mean a field will be quietly ignored: a pinned `version` under scoop, `skip_upgrade` on a github-only application, `package.github.args` without `run_installer`, or a `package.<source>` block for a source absent from `source:`.

```bash
apptide validate
```

A config still written in the v1 layout (top-level category keys mapping to package lists) is read and converted in memory, with a warning. AppTide only ever *writes* v2.

### Elevation

Some operations need administrator rights — `scoop --global`, most chocolatey packages, machine-scope winget installs. AppTide predicts this per application and reports it up front, because an installer that raises a UAC prompt does it outside the alt-screen: from the TUI's point of view the interface simply freezes with no explanation. Set `requires_admin: true` when the prediction is wrong.

### Exporting System State

Generate a v2 `packages.yaml` from what is currently installed:

```bash
apptide export                       # to stdout
apptide export --out packages.yaml   # to a file
```

### Self-Updating

```bash
apptide self-update
apptide self-update --github-token <token>   # or set $GITHUB_TOKEN
```

The binary matching this machine's architecture is chosen, so an ARM install
stays on the native build. The download is verified against the release's
checksum file before the swap, and a release that publishes none is reported
as unverified rather than refused. Old binaries are cleaned up automatically
after the update.

## Project Structure

```plain
apptide/
├── main.go              # Entry point
├── cmd/                 # CLI commands (install, list, tui, edit, adopt, ...)
├── scripts/make/        # Makefile targets, one file per area
├── docs/                # GitHub Pages site
├── examples/            # Annotated example configuration
├── internal/
│   ├── archive/         # Zip/tar/7z extraction, with path-traversal guards
│   ├── inventory/       # One parallel snapshot of what every manager has installed
│   ├── config/          # Schema, YAML loading, imports, defaults, editor metadata
│   ├── elevation/       # Whether apptide is elevated, and what needs elevation
│   ├── ghrelease/       # GitHub Releases API, asset selection, checksums
│   ├── installer/       # Package manager integrations
│   ├── output/          # Structured output helpers (JSON / table)
│   ├── pathutil/        # Path resolution utilities
│   ├── runner/          # Install/verify/export execution, source resolution, TUI output
│   ├── state/           # Which source installed each application (state.json)
│   ├── tui/             # Interactive browser
│   └── updater/         # Self-update logic and binary management
├── go.mod               # Go module definition
└── go.sum               # Dependency checksums
```

## System Requirements

* Windows 10 or later
* Administrator privileges for installing/uninstalling most software
* One or more package managers installed:
  * winget (Windows Package Manager)
  * Chocolatey (optional)
  * Scoop (optional)

`apptide doctor --install-missing` can bootstrap the missing ones.

## Dependencies

AppTide uses the following Go libraries:

* `github.com/spf13/cobra` — Command-line interface framework
* `gopkg.in/yaml.v3` — YAML parsing and serialization
* `github.com/charmbracelet/bubbletea` / `bubbles` / `lipgloss` — Terminal UI
* `github.com/lucasassuncao/yedit` — YAML editor used by `apptide edit`
* `github.com/pterm/pterm` — Interactive prompts for `apptide init -i`

## Contributing

Contributions are welcome. Please feel free to submit pull requests or open issues for bugs and feature requests.

### Development

Targets live in `scripts/make/*.mk`, one file per area. `make help` lists them all.

```bash
make tools          # install the pinned tools into ./.gobin
make lint           # golangci-lint
make security       # gosec
make test           # gotestsum, with -race
make test-coverage  # the same, plus HTML and Cobertura reports
make docs           # regenerate the per-package README files
make govulncheck    # advisories reachable from this code
make vuln           # SBOM plus a grype scan
make all            # everything above, in the order CI runs it
```

Every tool is pinned in `scripts/make/tools.mk` and installed into `./.gobin`,
so a target runs the same version everywhere rather than whatever is on PATH.
`make clean-tools` forces a reinstall after a version bump.

#### If a test binary is blocked with "Access is denied"

```
<package>.test.exe: Access is denied.
go: unlinkat ...\b001: The process cannot access the file because it is
being used by another process.
```

That is an antivirus false positive on the race-instrumented test binary, not
a compilation error. Bitdefender reported `Gen:Variant.Application.Tedy.57526`
against `catalog.test.exe` and blocked the write while the linker was still
producing it, which is why `internal/catalog` is now `internal/inventory`.

The trigger is the file name, not the code: a three-line package named
`catalog` was blocked just the same, and the real package's source under any
other name linked and ran normally. Nothing in the package was at fault.

If it happens again for another name, add an exclusion for Go's build
directory rather than renaming. Defender takes one from an elevated
PowerShell:

```powershell
Add-MpPreference -ExclusionPath "$env:LOCALAPPDATA\go-build"
```

Bitdefender has no supported command line for it: Protection → Antivirus →
Settings → Manage exceptions, pointing at the directory `go env GOTMPDIR`
prints, or `%TEMP%` when that is empty.

CI is unaffected either way.

## License

This project is open source and available under the MIT License.

## Support

For issues, questions, or suggestions, please open an issue on the project repository.
