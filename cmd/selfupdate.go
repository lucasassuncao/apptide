package cmd

import (
	"os"

	"github.com/lucasassuncao/apptide/internal/updater"
	"github.com/spf13/cobra"
)

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
		return updater.SelfUpdate(selfUpdateRepo, githubToken)
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
	defaultRepo := os.Getenv("UPDATER_REPO")
	selfUpdateCmd.Flags().StringVar(&selfUpdateRepo, "repo", defaultRepo,
		`GitHub repository in "owner/repo" format (or set $UPDATER_REPO)`)
	selfUpdateCmd.Flags().StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"),
		"GitHub API token (defaults to $GITHUB_TOKEN)")
}
