package tui

import "github.com/lucasassuncao/bezel/theme"

// The theme takes every colour from the terminal's own palette, so the
// interface matches whatever scheme the user already runs. Every style the
// rows and the detail draw with is a slot of it, so a theme swap reaches them.
var th = theme.Resolve(theme.ThemeTerminal, true)

var (
	stDim    = th.Dim
	stBold   = th.Bold
	stGreen  = th.Success
	stYellow = th.Warning
	stRed    = th.Danger
	stCyan   = th.Info
	stAccent = th.Accent
	stCursor = th.Cursor
	stKey    = th.Key
)
