package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via:
//
//	go build -ldflags "-X github.com/lucasassuncao/apptide/cmd.Version=v1.0.0"
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the apptide version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Printf("apptide %s\n", Version)
		},
	}
}
