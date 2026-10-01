package cmd

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/lucasassuncao/apptide/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Browse the config and the machine side by side",
	Long: `Open an interactive browser over the configuration and the installed packages.

Five tabs:
  Config      every declared application, with the source that manages it
  Installed   what the machine actually has, and whether the config tracks it
  Upgradable  packages with a newer version available
  Drift       only the applications where config and machine disagree
  Activity    progress and results of what you apply

Select with space, then i to install or r to remove — nothing runs before a
confirmation listing every change. The config file itself is edited with
'apptide edit'; this command reads it and acts on the machine.

Two keys act on apptide's own record rather than on the machine: a sets which
source owns an application, and f forgets it — the software stays, only the
record goes. See 'apptide adopt --list' for the same record on the command
line.`,
	Example: `  apptide tui
  apptide tui --config other.yaml`,
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := tui.New(tui.Options{
			ConfigPath:  configPath,
			InstallDir:  installDir,
			GitHubToken: resolveToken(githubToken),
		})
		if err != nil {
			return err
		}

		p := tea.NewProgram(m) // the model asks for the alt screen in its View
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("tui: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	tuiCmd.Flags().StringVar(&installDir, "install-dir", "", `default dir for github binaries (default: %LOCALAPPDATA%\apptide\bin)`)
	tuiCmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub API token (or set $GITHUB_TOKEN)")
}
