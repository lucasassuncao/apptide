package main

import (
	"os"
	"testing"
)

// --version runs the whole startup path, cleanup included, and returns
// without exiting the process.
func TestMainRunsTheRootCommand(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	os.Args = []string{"apptide", "--version"}

	main()
}
