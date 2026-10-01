package cmd

import (
	"path/filepath"
	"testing"
)

// A config that does not load fails before the program takes the terminal,
// so the error is printed on a normal screen.
func TestTUIFailsBeforeStartingOnABadConfig(t *testing.T) {
	prev := configPath
	t.Cleanup(func() { configPath = prev })
	configPath = filepath.Join(t.TempDir(), "missing.yaml")

	if err := tuiCmd.RunE(tuiCmd, nil); err == nil {
		t.Error("tui started without a config")
	}
}
