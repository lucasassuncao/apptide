package config

import (
	"github.com/lucasassuncao/yedit/metadata"
	"github.com/lucasassuncao/yedit/spec"
)

// This file is the single source of field metadata: it drives the hint panel
// in `apptide edit` and, through the FromMetadata validator family, the rules
// enforced on save. Documentation and validation therefore cannot drift apart.
//
// Each type declares only its own direct fields; nested types that implement
// metadata.Provider are composed automatically by metadata.New.

// NewMetadata builds the metadata tree for the config schema.
func NewMetadata() (spec.MetadataSource, error) {
	return metadata.New(Config{})
}

func (Config) Metadata() map[string]any {
	return map[string]any{
		"schema_version": meta{
			Description: "Version of this file's layout. Always 2 for configs written by this apptide.",
			Type:        "int", Required: true, Default: "2",
		},
		"defaults": meta{
			Description: "Values applied to every application in THIS file that omits them. " +
				"Not inherited by imported files. A value set on the application always wins.",
			Type: "object",
		},
		"applications": meta{
			Description: "The applications this config manages.",
			Type:        "[]object", Required: true, MinCount: 1,
		},
		"settings": meta{
			Description: "Run-wide options. Only the settings of the file passed to --config " +
				"are used, and a command-line flag always wins over them.",
			Type: "object",
		},
	}
}

func (Settings) Metadata() map[string]any {
	return map[string]any{
		"install_dir": meta{
			Description: `Where github binaries are placed. Default: %LOCALAPPDATA%\apptide\bin. ` +
				"${VAR} is expanded from the environment.",
			Type: "string", Example: `install_dir: "${LOCALAPPDATA}/apptide/bin"`,
		},
		"add_to_path": meta{
			Description: "Add install_dir to the user PATH after installing a binary. " +
				"Takes effect in new terminals only.",
			Type: "bool", Default: "false",
		},
		"github_token": meta{
			Description: "Token used for the GitHub API, which raises the rate limit. " +
				"Write it as ${GITHUB_TOKEN} so the value stays out of the file.",
			Type: "string", Example: `github_token: "${GITHUB_TOKEN}"`,
		},
	}
}

func (Defaults) Metadata() map[string]any {
	return map[string]any{
		"category": meta{
			Description: "Category for applications in this file that declare none.",
			Type:        "string", Example: `category: CLITools`,
		},
		"source": meta{
			Description: "Source preference list for applications that declare none.",
			Type:        "string | []string",
		},
		"action": meta{
			Description: "Action for applications that declare none.",
			Type:        "string", Default: "install",
			OneOf: []string{"install", "uninstall", "skip"},
		},
		"tags": meta{
			Description: "Tags added to every application in this file.",
			Type:        "[]string",
		},
	}
}

func (Application) Metadata() map[string]any {
	return map[string]any{
		"name": meta{
			Description: "Display name, shown in install/verify output. Also the key used to " +
				"record which source installed this application, so renaming it forgets that binding.",
			Type: "string", Required: true, Example: `name: "VS Code"`,
		},
		"description": meta{
			Description: "Free-form note. Informational only — never used to install anything.",
			Type:        "string",
		},
		"category": meta{
			Description: "Group used by `apptide install --category`. Empty means Uncategorized.",
			Type:        "string", Example: `category: Development`,
		},
		"info_url": meta{
			Description: "Project homepage. Informational only.",
			Type:        "string", Formats: []string{spec.FormatURL.Label()},
		},
		"action": meta{
			Description: "What to do with this application. `skip` keeps the declaration without acting on it.",
			Type:        "string", Default: "install",
			OneOf: []string{"install", "uninstall", "skip"},
		},
		"tags": meta{
			Description: "Free-form labels for grouping across categories.",
			Type:        "[]string", Unique: true,
		},
		"version": meta{
			Description: "Exact version, or `latest`. Honoured by winget and chocolatey (--version) " +
				"and by github (it becomes the release tag). Scoop cannot install a specific " +
				"version and will install latest instead.",
			Type: "string", Default: "latest", Example: `version: "20.11.1"`,
		},
		"skip_upgrade": meta{
			Description: "Install when missing, but never upgrade afterwards. No effect when github " +
				"is the only source: that source has no upgrade path.",
			Type: "bool", Default: "false",
		},
		"optional": meta{
			Description: "A failure here is reported but does not fail the run. Use it for packages " +
				"known to be unreliable, so one of them cannot break a pipeline of sixty.",
			Type: "bool", Default: "false",
		},
		"requires_admin": meta{
			Description: "This application needs administrator rights. Without it apptide guesses " +
				"from the source and its options, and the guess is wrong in both directions.",
			Type: "bool", Default: "false",
		},
		"source": meta{
			Description: "Accepted sources in preference order. Accepts a single value (`source: winget`) " +
				"or a list (`source: [winget, scoop]`), where later entries are fallbacks used only " +
				"when an earlier source does not offer the package at all. The order applies to the " +
				"FIRST install only: afterwards the application stays bound to the source that " +
				"installed it, for upgrades and removal alike.",
			Type: "string | []string", Required: true,
			Example: "source: [winget, scoop, github]",
		},
		"hooks": meta{
			Description: "Shell commands run around the action.",
			Type:        "object",
		},
		"package": meta{
			Description: "Per-manager coordinates. Every source listed above needs its block here.",
			Type:        "object", Required: true,
		},
	}
}

func (Hooks) Metadata() map[string]any {
	return map[string]any{
		"pre_install": meta{
			Description: "Command run via `cmd /C` before the action. A non-zero exit aborts this application.",
			Type:        "string",
		},
		"post_install": meta{
			Description: "Command run via `cmd /C` after a successful install or upgrade. " +
				"A non-zero exit is reported as a warning only.",
			Type: "string", Example: `post_install: "code --install-extension golang.go"`,
		},
		"pre_uninstall": meta{
			Description: "Command run before a removal. A non-zero exit aborts it. " +
				"The install hooks never run on a removal, and vice versa.",
			Type: "string",
		},
		"post_uninstall": meta{
			Description: "Command run after a successful removal — where leftover configuration " +
				"is cleaned up. A non-zero exit is a warning only.",
			Type: "string", Example: `post_uninstall: "rmdir /s /q %APPDATA%\\App"`,
		},
	}
}

func (Packages) Metadata() map[string]any {
	return map[string]any{
		"winget":     meta{Description: "Coordinates for the Windows Package Manager."},
		"chocolatey": meta{Description: "Coordinates for Chocolatey."},
		"scoop":      meta{Description: "Coordinates for Scoop."},
		"github":     meta{Description: "Coordinates for a GitHub releases download."},
	}
}

func (WingetSpec) Metadata() map[string]any {
	return map[string]any{
		"id": meta{
			Description: "winget package identifier, matched exactly.",
			Type:        "string", Required: true, Example: `id: "Microsoft.VisualStudioCode"`,
		},
		"args": meta{
			Description: "Extra arguments appended to `winget install` / `winget upgrade`.",
			Type:        "[]string",
		},
		"scope": meta{
			Description: "Install for the whole machine or the current user (--scope).",
			Type:        "string", OneOf: []string{"machine", "user"},
		},
		"locale": meta{
			Description: "Installer locale (--locale).",
			Type:        "string", Example: `locale: "pt-BR"`,
		},
		"feed": meta{
			Description: "Which winget source to resolve the id against (--source): the community " +
				"repo, the Microsoft Store, or a private feed.",
			Type: "string", Example: `feed: msstore`,
		},
		"override": meta{
			Description: "String passed verbatim to the underlying installer (--override), replacing " +
				"the arguments winget would choose. Use only when a package needs installer-specific flags.",
			Type: "string", Example: `override: "/VERYSILENT /NORESTART"`,
		},
	}
}

func (ChocolateySpec) Metadata() map[string]any {
	return map[string]any{
		"id": meta{
			Description: "chocolatey package identifier.",
			Type:        "string", Required: true, Example: `id: "firefox"`,
		},
		"args": meta{
			Description: "Extra arguments appended to `choco install` / `choco upgrade`.",
			Type:        "[]string",
		},
		"package_params": meta{
			Description: "Parameters consumed by the chocolatey package script (--package-parameters). " +
				"Distinct from install_args, which reaches the native installer.",
			Type: "string", Example: `package_params: "/l:pt-BR /NoTaskbarShortcut"`,
		},
		"install_args": meta{
			Description: "Arguments forwarded to the native installer (--install-arguments). " +
				"Distinct from package_params, which the chocolatey script consumes.",
			Type: "string", Example: `install_args: "/DIR=C:\\Tools"`,
		},
		"feed": meta{
			Description: "Package feed to install from (--source), for internal or private repositories.",
			Type:        "string",
		},
		"allow_downgrade": meta{
			Description: "Permit installing an older version than the one present (--allow-downgrade).",
			Type:        "bool", Default: "false",
		},
	}
}

func (ScoopSpec) Metadata() map[string]any {
	return map[string]any{
		"id": meta{
			Description: "scoop app name.",
			Type:        "string", Required: true, Example: `id: "ripgrep"`,
		},
		"args": meta{
			Description: "Extra arguments appended to `scoop install` / `scoop update`.",
			Type:        "[]string",
		},
		"bucket": meta{
			Description: "Bucket that provides the app. Added automatically before installing when missing.",
			Type:        "string", Example: `bucket: "extras"`,
		},
		"global": meta{
			Description: "Install for all users (--global). Requires an elevated shell.",
			Type:        "bool", Default: "false",
		},
		"arch": meta{
			Description: "Architecture to install (--arch), when the manifest offers more than one.",
			Type:        "string", OneOf: []string{"64bit", "32bit", "arm64"},
		},
	}
}

func (GitHubSpec) Metadata() map[string]any {
	return map[string]any{
		"id": meta{
			Description: "Repository in owner/repo form. This source downloads a release asset directly; " +
				"it cannot upgrade or uninstall.",
			Type: "string", Required: true,
			Pattern: `^[\w.\-]+/[\w.\-]+$`,
			Example: `id: "jesseduffield/lazygit"`,
		},
		"args": meta{
			Description: "Arguments for the DOWNLOADED installer (or msiexec) — not for a package " +
				"manager, unlike args in the other sources. Ignored unless run_installer is true.",
			Type: "[]string", Example: `args: ["/quiet", "/norestart"]`,
		},
		"asset_pattern": meta{
			Description: "Glob selecting a specific release asset. Without it apptide scores the " +
				"assets and picks the best Windows match, which can guess wrong on unusual naming.",
			Type: "string", Example: `asset_pattern: "*windows_amd64*.zip"`,
		},
		"run_installer": meta{
			Description: "Execute the downloaded .exe/.msi instead of copying the binary into install_dir.",
			Type:        "bool", Default: "false",
		},
		"install_dir": meta{
			Description: `Directory for the extracted binary. Default: %LOCALAPPDATA%\apptide\bin.`,
			Type:        "string",
		},
		"binary_name": meta{
			Description: "Binary name when it differs from the lowercased application name.",
			Type:        "string", Example: `binary_name: "gh"    # for name: "GitHub CLI"`,
		},
		"checksum": meta{
			Description: "Expected SHA-256 of the downloaded asset, optionally prefixed with sha256:. " +
				"Pins the exact bytes, which the release's own checksum file cannot do. " +
				"Only worth setting together with a pinned version.",
			Type:    "string",
			Pattern: `^(sha256:)?[0-9a-fA-F]{64}$`,
			Example: `checksum: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`,
		},
		"prerelease": meta{
			Description: "Accept pre-release tags. GitHub's \"latest release\" excludes them, so a " +
				"repository that only publishes pre-releases resolves to nothing without this.",
			Type: "bool", Default: "false",
		},
	}
}
