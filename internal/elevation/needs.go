package elevation

import "github.com/lucasassuncao/apptide/internal/config"

// Needed reports whether installing app through src will normally require
// administrator rights, and why.
//
// This is a prediction, not a fact: a chocolatey package can be portable, and a
// winget package can request elevation on its own. It is worth making anyway,
// because the alternative is a TUI that appears to hang while an invisible UAC
// dialog waits behind it.
func Needed(app config.Application, src config.Source) (bool, string) {
	// A declaration beats the guess in both directions: a portable chocolatey
	// package needs nothing, and a winget installer can demand elevation
	// without any of its options saying so.
	if app.RequiresAdmin {
		return true, "declared requires_admin"
	}

	switch src.Normalize() {
	case config.SourceChocolatey:
		// Chocolatey installs into %ProgramData% and almost always needs it.
		return true, "chocolatey installs machine-wide"

	case config.SourceWinget:
		if spec := app.Package.Winget; spec != nil && spec.Scope == "machine" {
			return true, "winget scope: machine"
		}

	case config.SourceScoop:
		if spec := app.Package.Scoop; spec != nil && spec.Global {
			return true, "scoop global install"
		}

	case config.SourceGitHub:
		if spec := app.Package.GitHub; spec != nil && spec.RunInstaller {
			return true, "runs an installer that may prompt"
		}
	}
	return false, ""
}
