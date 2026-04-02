package cmd

import (
	"os"

	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/spf13/cobra"
)

var (
	category    string
	source      string
	dryRun      bool
	installDir  string
	addToPath   bool
	githubToken string
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install (or upgrade) packages defined in the config file",
	Example: `  apptide install
  apptide install --category Development
  apptide install --source winget
  apptide install --dry-run`,
	RunE: func(cmd *cobra.Command, args []string) error {
		r := runner.New(runner.Options{
			ConfigPath:  configPath,
			Category:    category,
			Source:      source,
			DryRun:      dryRun,
			InstallDir:  installDir,
			AddToPath:   addToPath,
			GitHubToken: githubToken,
		})
		return r.Run()
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().StringVarP(&category, "category", "C", "", "process only this category")
	installCmd.Flags().StringVarP(&source, "source", "s", "", "process only this source (winget, chocolatey, scoop, github, third_party)")
	installCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "simulate actions without executing anything")
	installCmd.Flags().StringVar(&installDir, "install-dir", "", `default dir for github/third_party binaries (default: %LOCALAPPDATA%\apptide\bin)`)
	installCmd.Flags().BoolVar(&addToPath, "add-to-path", false, "add the install-dir to the user PATH if not already present")
	installCmd.Flags().StringVar(&githubToken, "github-token", os.Getenv("GITHUB_TOKEN"), "GitHub API token (defaults to $GITHUB_TOKEN)")
}
