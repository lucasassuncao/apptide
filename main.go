package main

import (
	"github.com/lucasassuncao/apptide/cmd"
	"github.com/lucasassuncao/apptide/internal/updater"
)

func main() {
	// Remove any leftover .old binary from a previous self-update.
	updater.CleanOldBinary()

	cmd.Execute()
}
