package cmd

import (
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/spf13/cobra"
)

var exportOutput string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Generate a packages.yaml from all currently installed packages",
	Long: `Queries winget, scoop, and chocolatey for installed packages and writes
a packages.yaml that can be used with 'apptide install' to replicate the setup.`,
	Example: `  apptide export                      # print to stdout
  apptide export --output backup.yaml  # write to file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runner.Export(runner.ExportOptions{
			Output: exportOutput,
		})
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "write to this file instead of stdout")
}
