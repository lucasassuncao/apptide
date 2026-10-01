package cmd

import (
	"fmt"
	"sort"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/yedit/presets"
)

// AppTideBlockPresets feeds the preset picker inside the block editors (p on a
// block): one preset set per top-level block.
//
// Every preset is a Go value of the schema type, marshaled on demand, so a
// preset cannot name a field the config does not have — a hand-written YAML
// snippet could, and would only fail once a user inserted it.
var AppTideBlockPresets = presets.Combine(
	presets.ForField("settings", settingsPresets()),
	presets.ForField("defaults", defaultsPresets()),
	presets.ForField("applications", applicationPresets()),
)

// AppTideDocPresets is the whole-document picker on the root list (p), backed
// by the same templates `apptide init --template` writes, so the picker and
// init can never offer different starting points.
var AppTideDocPresets presets.Source = docPresets{}

// docPresets implements presets.Source over the init templates. Whole-document
// sources use the empty field name: PresetYAML("", name) returns the full YAML.
type docPresets struct{}

func (docPresets) ListFields() []string { return []string{""} }

func (docPresets) ListPresets(field string) []string {
	if field != "" {
		return nil
	}
	return initTemplateNames()
}

func (docPresets) PresetYAML(field, name string) (string, error) {
	if field != "" {
		return "", fmt.Errorf("docPresets: unknown field %q", field)
	}
	render, ok := initTemplates()[name]
	if !ok {
		return "", fmt.Errorf("docPresets: unknown template %q", name)
	}
	return render(), nil
}

// ── settings ─────────────────────────────────────────────────────────────────

func settingsPresets() map[string]config.Settings {
	return map[string]config.Settings{
		// The default location, spelled out: a value is easier to change than
		// to discover.
		"default-bin-dir": {
			InstallDir: "${LOCALAPPDATA}/apptide/bin",
			AddToPath:  true,
		},
		// One directory for every portable tool, for machines that already
		// have such a place on PATH.
		"custom-bin-dir": {
			InstallDir: "C:/Tools/bin",
			AddToPath:  true,
		},
		// The token is read from the environment, so the file stays shareable.
		"github-token": {
			GitHubToken: "${GITHUB_TOKEN}", //#nosec G101 -- the literal placeholder, not a token: the value is expanded from the environment at load time
		},
	}
}

// ── defaults ─────────────────────────────────────────────────────────────────

func defaultsPresets() map[string]config.Defaults {
	return map[string]config.Defaults{
		"winget-only": {
			Source: config.Sources{config.SourceWinget},
			Action: config.ActionInstall,
		},
		// Preference list: scoop picks up what winget does not carry.
		"winget-then-scoop": {
			Category: "Development",
			Source:   config.Sources{config.SourceWinget, config.SourceScoop},
			Action:   config.ActionInstall,
		},
		"cli-tools": {
			Category: "CLITools",
			Source:   config.Sources{config.SourceWinget, config.SourceScoop, config.SourceGitHub},
			Tags:     []string{"cli"},
		},
		// For a file whose whole job is removing software.
		"removal-list": {
			Action: config.ActionUninstall,
		},
	}
}

// ── applications ─────────────────────────────────────────────────────────────

// applicationPresets returns one-entry lists: the picker either replaces the
// block with the selection (enter) or appends it to the existing list (a), so
// a preset describes a single application.
//
// Names are distinct across presets because the name is the key apptide
// records the installed source under: appending two presets that shared one
// would produce a config that no longer validates.
func applicationPresets() map[string][]config.Application {
	return map[string][]config.Application{
		"winget-app": {{
			Name:        "Visual Studio Code",
			Description: "Code editor",
			Category:    "Development",
			Source:      config.Sources{config.SourceWinget},
			Package: config.Packages{
				Winget: &config.WingetSpec{
					ID:    "Microsoft.VisualStudioCode",
					Scope: "user",
				},
			},
		}},

		"chocolatey-app": {{
			Name:        "Firefox",
			Description: "Web browser",
			Category:    "Browsers",
			Source:      config.Sources{config.SourceChocolatey},
			Package: config.Packages{
				Chocolatey: &config.ChocolateySpec{
					ID: "firefox",
					// Consumed by the chocolatey package script, not by the
					// native installer — install_args is that other channel.
					PackageParams: "/l:pt-BR /NoTaskbarShortcut",
				},
			},
		}},

		"scoop-app": {{
			Name:        "ripgrep",
			Description: "Recursive line search",
			Category:    "CLITools",
			Source:      config.Sources{config.SourceScoop},
			Package: config.Packages{
				Scoop: &config.ScoopSpec{ID: "ripgrep", Bucket: "main"},
			},
		}},

		// A release asset copied into install_dir. asset_pattern removes the
		// guesswork when a repository publishes several Windows archives.
		"github-binary": {{
			Name:        "Lazygit",
			Description: "Terminal UI for git",
			Category:    "Development",
			Source:      config.Sources{config.SourceGitHub},
			Package: config.Packages{
				GitHub: &config.GitHubSpec{
					ID:           "jesseduffield/lazygit",
					AssetPattern: "*Windows_x86_64.zip",
					BinaryName:   "lazygit",
				},
			},
		}},

		// A release asset that is an installer: it is executed instead of
		// copied, and args reach that installer.
		"github-installer": {{
			Name:        "PowerShell",
			Description: "PowerShell 7",
			Category:    "Development",
			Source:      config.Sources{config.SourceGitHub},
			Package: config.Packages{
				GitHub: &config.GitHubSpec{
					ID:           "PowerShell/PowerShell",
					AssetPattern: "*win-x64.msi",
					RunInstaller: true,
					Args:         []string{"/quiet", "/norestart"},
				},
			},
		}},

		// Preference list with a block per source: the first source that
		// offers the package installs it, and the binding is kept from then on.
		"multi-source-fallback": {{
			Name:        "fzf",
			Description: "Fuzzy finder",
			Category:    "CLITools",
			Source:      config.Sources{config.SourceWinget, config.SourceScoop, config.SourceGitHub},
			Package: config.Packages{
				Winget: &config.WingetSpec{ID: "junegunn.fzf"},
				Scoop:  &config.ScoopSpec{ID: "fzf"},
				GitHub: &config.GitHubSpec{
					ID:           "junegunn/fzf",
					AssetPattern: "*windows_amd64.zip",
				},
			},
		}},

		// Pinned to an exact version and never upgraded afterwards.
		"pinned-version": {{
			Name:        "Node.js",
			Description: "JavaScript runtime, pinned to the version this project builds with",
			Category:    "Development",
			Version:     "20.11.1",
			SkipUpgrade: true,
			Source:      config.Sources{config.SourceWinget},
			Package: config.Packages{
				Winget: &config.WingetSpec{ID: "OpenJS.NodeJS"},
			},
		}},

		"with-hooks": {{
			Name:        "Neovim",
			Description: "Text editor",
			Category:    "Development",
			Source:      config.Sources{config.SourceWinget},
			Hooks: &config.Hooks{
				PreInstall:    `if not exist "%LOCALAPPDATA%\nvim" mkdir "%LOCALAPPDATA%\nvim"`,
				PostInstall:   "nvim --version",
				PostUninstall: `rmdir /s /q "%LOCALAPPDATA%\nvim-data"`,
			},
			Package: config.Packages{
				Winget: &config.WingetSpec{ID: "Neovim.Neovim"},
			},
		}},

		// Removal is declarative too: the entry stays in the file, describing
		// software that must not be on the machine.
		"uninstall": {{
			Name:     "OneDrive",
			Category: "Cleanup",
			Action:   config.ActionUninstall,
			Source:   config.Sources{config.SourceWinget},
			Package: config.Packages{
				Winget: &config.WingetSpec{ID: "Microsoft.OneDrive"},
			},
		}},

		// optional keeps one flaky package from failing a whole run;
		// requires_admin states the elevation need instead of leaving it to
		// apptide's guess.
		"optional-and-admin": {{
			Name:          "Docker Desktop",
			Description:   "Containers",
			Category:      "Development",
			Tags:          []string{"containers"},
			Optional:      true,
			RequiresAdmin: true,
			Source:        config.Sources{config.SourceWinget},
			Package: config.Packages{
				Winget: &config.WingetSpec{
					ID:    "Docker.DockerDesktop",
					Scope: "machine",
				},
			},
		}},
	}
}

// sortedNames returns the keys of m in a stable order, so a picker lists the
// same entries in the same order on every run.
func sortedNames[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
