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
	Example: `  apptide export                   # print to stdout
  apptide export --out backup.yaml # write to file`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runner.Export(runner.ExportOptions{
			Output: exportOutput,
		})
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
	// Not --output/-o: that is the global table|json flag, and shadowing it
	// here meant `apptide export -o json` quietly wrote a file named "json".
	exportCmd.Flags().StringVar(&exportOutput, "out", "", "write to this file instead of stdout")
}
