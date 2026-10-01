//go:build windows

// Package elevation reports whether apptide is running with administrator
// rights, and which package operations are likely to need them.
//
// Knowing this up front matters most in the TUI: an installer that raises a
// UAC prompt does it outside the alt-screen, so from the user's point of view
// the interface simply freezes with no explanation.
package elevation

import "golang.org/x/sys/windows"

// IsElevated reports whether the current process token is elevated.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
