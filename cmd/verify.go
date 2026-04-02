package cmd

import (
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Check which packages from the config are installed (no changes made)",
	Example: `  apptide verify
  apptide verify --category Development
  apptide verify --source github`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runner.Verify(runner.VerifyOptions{
			ConfigPath:  configPath,
			Category:    category,
			Source:      source,
			InstallDir:  installDir,
			GitHubToken: resolveToken(githubToken),
		})
	},
}

func init() {
	rootCmd.AddCommand(verifyCmd)
	// Bind the same package-level vars used by install.go.
	verifyCmd.Flags().StringVarP(&category, "category", "C", "", "check only this category")
	verifyCmd.Flags().StringVarP(&source, "source", "s", "", "check only this source")
	verifyCmd.Flags().StringVar(&installDir, "install-dir", "", `default dir for github/third_party binaries`)
	verifyCmd.Flags().StringVar(&githubToken, "github-token", "", "GitHub API token (defaults to $GITHUB_TOKEN)")
}
