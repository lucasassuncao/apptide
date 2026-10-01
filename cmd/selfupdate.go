package cmd

import (
	"github.com/lucasassuncao/apptide/internal/updater"
	"github.com/spf13/cobra"
)

// DefaultRepo is set at build time via ldflags.
var DefaultRepo = ""

var (
	selfUpdateRepo  string
	selfUpdateToken string
)

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update",
	Short: "Update apptide itself to the latest GitHub release",
	Long: `Downloads the latest apptide release from GitHub and replaces the current binary.
The old binary is kept as apptide.exe.old until the next run.`,
	Example: `  apptide self-update
  apptide self-update --repo lucasassuncao/apptide`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// The token was hard-coded empty here, so self-update ran against the
		// 60-requests-an-hour anonymous limit even with GITHUB_TOKEN set.
		return updater.SelfUpdate(cmd.Context(), selfUpdateRepo, resolveToken(selfUpdateToken), Version)
	},
}

func init() {
	rootCmd.AddCommand(selfUpdateCmd)
	selfUpdateCmd.Flags().StringVar(&selfUpdateRepo, "repo", DefaultRepo,
		`GitHub repository in "owner/repo" format`)
	selfUpdateCmd.Flags().StringVar(&selfUpdateToken, "github-token", "",
		"GitHub token for the API (defaults to $GITHUB_TOKEN)")
}
