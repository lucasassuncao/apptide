package cmd

import (
	"github.com/lucasassuncao/apptide/internal/updater"
	"github.com/spf13/cobra"
)

// DefaultRepo is set at build time via ldflags.
var DefaultRepo = ""

var selfUpdateRepo string

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update apptide itself to the latest GitHub release",
	Long: `Downloads the latest apptide release from GitHub and replaces the current binary.
The old binary is kept as apptide.exe.old until the next run.

The repository must be provided via --repo or the UPDATER_REPO environment variable.`,
	Example: `  apptide self-update --repo lucas/apptide
  UPDATER_REPO=lucas/apptide apptide self-update`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return updater.SelfUpdate(selfUpdateRepo, "")
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
	selfUpdateCmd.Flags().StringVar(&selfUpdateRepo, "repo", DefaultRepo,
		`GitHub repository in "owner/repo" format`)
}
