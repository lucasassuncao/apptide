# 🌊 AppTide

<!-- markdownlint-disable MD033 -->
<p align="center">
  <img src="docs/apptide.png" alt="AppTide logo" width="360">
</p>
<!-- markdownlint-enable MD033 -->

![made with Go](https://img.shields.io/badge/made_with-Go-blue?logo=go) ![type CLI](https://img.shields.io/badge/type-CLI-green) ![platform Windows](https://img.shields.io/badge/platform-Windows-blue) ![license MIT](https://img.shields.io/badge/license-MIT-lightgrey)

A unified package manager CLI for Windows that orchestrates installations, updates, and lifecycle management across multiple package sources with a single declarative configuration.

## Overview

AppTide streamlines the management of your Windows development environment and applications. Instead of managing winget, chocolatey, scoop, and GitHub releases separately, AppTide lets you define your entire software stack in a single YAML file and operate on it through an intuitive command-line interface.

## Key Features

* **Multi-Source Support**: Seamlessly install and update packages from winget, chocolatey, scoop, GitHub releases, and direct URLs
* **Declarative Configuration**: Define your entire software suite once in `packages.yaml`, version control it, and replicate it across machines
* **Config Imports**: Split your configuration across multiple files and compose them with `import:` — supports relative paths and circular-import detection
* **Organized by Category**: Group applications into logical categories (Development, Browsers, DevOps, CLITools, etc.) for clarity and maintainability
* **Flexible Actions**: Fine-grained control with install, uninstall, and skip actions per package
* **Lifecycle Hooks**: Run shell commands before (`pre_install`) and after (`post_install`) each package action
* **Self-Updating**: AppTide can automatically detect and apply its own updates without manual intervention
* **Validation & Verification**: Built-in health checks to ensure configurations are valid and systems are in the desired state
* **Structured Output**: All commands support `--output json` for scripting and CI/CD integration
* **Rich CLI**: Intuitive commands with helpful documentation and diagnostic output

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

Create a `packages.yaml` file in your project or home directory:

```yaml
Development:
  - name: "VS Code"
    source: winget
    id: "Microsoft.VisualStudioCode"
    description: "The open-source AI code editor"
    info_url: "https://code.visualstudio.com/"
    action: install

  - name: "Git"
    source: winget
    id: "Git.Git"
    description: "Distributed version control system"
    info_url: "https://git-scm.com/"
    action: install

CLITools:
  - name: "jq"
    source: winget
    id: "jqlang.jq"
    description: "Command-line JSON processor"
    action: install
```

### 2. Run Commands

List all packages in your configuration:

```bash
apptide list
```

Validate your configuration:

```bash
apptide validate
```

Install all packages:

```bash
apptide install
```

Verify the system state:

```bash
apptide verify
```

Check system health:

```bash
apptide doctor
```

## Commands

| Command | Description |
|---------|-------------|
| `list` | Display all configured packages and their metadata |
| `install` | Install or update all packages marked with `action: install` |
| `doctor` | Diagnose system health and check package manager availability |
| `validate` | Verify YAML configuration syntax and completeness |
| `verify` | Check which packages are installed and detect version mismatches |
| `export` | Export current system state as a packages.yaml template |
| `selfupdate` | Download and apply the latest AppTide release |
| `version` | Print the current AppTide version |

## Configuration Format

Each package entry supports the following fields:

### Common Fields

* `name` (required): Display name for the package
* `source` (required): Package source — `winget`, `chocolatey`, `scoop`, `github`, or `third_party`
* `action` (optional, default: `install`): Action to perform — `install`, `uninstall`, or `skip`
* `description` (optional): Human-readable description
* `info_url` (optional): Project homepage or documentation link
* `pre_install` (optional): Shell command executed **before** the package action — non-zero exit aborts the install
* `post_install` (optional): Shell command executed **after** a successful install/uninstall — non-zero exit is reported as a warning but does not mark the package as failed

### winget / chocolatey / scoop

* `id` (required): Package identifier in the package manager
* `version` (optional, default: `latest`): Specific version or "latest"
* `no_upgrade` (optional): If true, skip upgrade when already installed
* `args` (optional): Extra CLI arguments to pass to the installer

### github

* `repo` (required): Repository identifier as "owner/repo"
* `version` (optional, default: `latest`): Release tag or "latest"
* `asset_pattern` (optional): Glob pattern to select specific assets (e.g., `*windows_amd64*.zip`)
* `run_installer` (optional): If true, execute .exe/.msi files; otherwise copy binary
* `install_dir` (optional): Override default binary installation directory

### third_party

* `url` (required): Direct download URL for the file
* `run_installer` (optional): If true, execute the downloaded installer
* `args` (optional): Arguments passed to the installer
* `install_dir` (optional): Override default binary directory

## Example Configuration

```yaml
Browsers:
  - name: "Chrome"
    source: winget
    id: "Google.Chrome"
    description: "Google Chrome web browser"
    info_url: "https://www.google.com/chrome/"
    action: install

Development:
  - name: "GoLang"
    source: winget
    id: "GoLang.Go"
    description: "Go programming language"
    action: install

  - name: "Lazygit"
    source: github
    repo: "jesseduffield/lazygit"
    description: "Terminal UI for git"
    version: "v0.40.2"
    action: install

CLITools:
  - name: "jq"
    source: winget
    id: "jqlang.jq"
    description: "Command-line JSON processor"
    action: install

  - name: "FFmpeg"
    source: winget
    id: "Gyan.FFmpeg"
    description: "Record, convert and stream audio and video"
    action: skip
```

## Advanced Usage

### Splitting Configuration with Imports

You can split your configuration across multiple files and compose them in a main `packages.yaml` using the top-level `import:` field. Paths are resolved relative to the file that declares them, so imports work regardless of where you invoke AppTide from.

```yaml
import:
  - ./configs/gaming.yaml
  - ./configs/work.yaml
  - ../envs/prd/packages.yaml

Development:
  - name: "Git"
    source: winget
    id: "Git.Git"
    action: install
```

* Imported files can themselves contain `import:` entries (recursive)
* Categories that appear in multiple files are merged — packages from both files are included
* Circular imports are detected and reported as an error

Pass any config file to any command with `--config`:

```bash
apptide install --config ./envs/prd/packages.yaml
apptide list    --config ./configs/gaming.yaml
```

### Lifecycle Hooks

Run shell commands before or after each package action using `pre_install` and `post_install`. Both hooks run via `cmd /C`.

```yaml
Development:
  - name: "Go"
    source: winget
    id: "GoLang.Go"
    pre_install: "echo Installing Go..."
    post_install: "go env -w GOPATH=C:/go"
    action: install

  - name: "Node.js"
    source: winget
    id: "OpenJS.NodeJS"
    post_install: "npm install -g yarn"
    action: install
```

**Behaviour:**

* `pre_install` failure (non-zero exit) → package action is **aborted**, row shown as `failed`
* `post_install` failure → package shown as `ok` with a **warning** detail
* Neither hook runs in `--dry-run` mode or when a package is already up to date

### Structured JSON Output

All commands support `--output json` (short: `-o json`) for scripting and CI/CD pipelines. In JSON mode, TUI output is suppressed and only valid JSON is written to stdout; errors go to stderr.

```bash
# List all packages as JSON
apptide list --output json

# Verify and pipe results to jq
apptide verify --output json | jq '[.[] | select(.status == "not_found")]'

# Doctor check in JSON (useful in CI)
apptide doctor --output json
```

`list` output shape:

```json
[{ "category": "...", "name": "...", "source": "...", "action": "...", "version": "...", "description": "..." }]
```

`verify` output shape:

```json
[{ "name": "...", "category": "...", "source": "...", "action": "...", "installed": true, "current_version": "...", "status": "installed" }]
```

`doctor` output shape:

```json
{
  "managers": [{ "name": "winget", "available": true, "version": "...", "path": "..." }],
  "apptide": { "install_dir": "...", "install_dir_exists": true, "in_path": false },
  "updater_repo": "",
  "all_ok": false
}
```

### Self-Updating

AppTide can detect and install its own updates:

```bash
apptide selfupdate
```

The application automatically cleans up old binaries after self-updates.

### Exporting System State

Generate a `packages.yaml` template from your current system installations:

```bash
apptide export > packages.yaml
```

### Filtering by Category

The `install` and `verify` commands accept a `--category` flag to operate on a single category:

```bash
apptide install --category Development
apptide verify  --category CLITools
```

## Project Structure

```plain
apptide/
├── main.go              # Entry point
├── cmd/                 # CLI commands (install, list, validate, etc.)
├── configs/             # Per-category package files imported by packages.yaml
├── internal/
│   ├── config/          # Configuration parsing, validation, and import resolution
│   ├── installer/       # Package manager integrations
│   ├── output/          # Structured output helpers (JSON / table)
│   ├── pathutil/        # Path resolution utilities
│   ├── runner/          # Command execution, TUI, and verification logic
│   └── updater/         # Self-update logic and binary management
├── packages.yaml        # Main configuration — imports from configs/
├── go.mod               # Go module definition
└── go.sum               # Dependency checksums
```

## System Requirements

* Windows 10 or later
* Administrator privileges for installing/uninstalling software
* One or more package managers installed:
  * winget (Windows Package Manager)
  * Chocolatey (optional)
  * Scoop (optional)

## Dependencies

AppTide uses the following Go libraries:

* `github.com/spf13/cobra` — Command-line interface framework
* `gopkg.in/yaml.v3` — YAML parsing and serialization
* `github.com/charmbracelet/bubbletea` — Terminal UI framework
* `github.com/charmbracelet/lipgloss` — Terminal styling

## Contributing

Contributions are welcome. Please feel free to submit pull requests or open issues for bugs and feature requests.

## License

This project is open source and available under the MIT License.

## Support

For issues, questions, or suggestions, please open an issue on the project repository.
