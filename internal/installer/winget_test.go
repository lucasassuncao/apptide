package installer

import (
	"errors"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

// argsEqual compares two argv slices, treating nil and empty as equal.
func argsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// containsSeq reports whether want appears in got as a contiguous run. Used to
// assert that a flag and its value stay adjacent and in order.
func containsSeq(got, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(got); i++ {
		if argsEqual(got[i:i+len(want)], want) {
			return true
		}
	}
	return false
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func wingetApp(spec *config.WingetSpec) config.Application {
	return config.Application{
		Name:    "Test App",
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: spec},
	}
}

func TestWingetInstallArgsBase(t *testing.T) {
	w := NewWinget(false)
	spec := &config.WingetSpec{ID: "Publisher.App"}

	got := w.installArgs(wingetApp(spec), spec)
	want := []string{
		"install", "--id", "Publisher.App", "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if !argsEqual(got, want) {
		t.Errorf("installArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestWingetSpecFlagsReachCommandLine(t *testing.T) {
	tests := []struct {
		name string
		spec config.WingetSpec
		want []string // contiguous sequence expected in the argv
	}{
		{"scope", config.WingetSpec{ID: "P.A", Scope: "machine"}, []string{"--scope", "machine"}},
		{"locale", config.WingetSpec{ID: "P.A", Locale: "pt-BR"}, []string{"--locale", "pt-BR"}},
		{"feed", config.WingetSpec{ID: "P.A", Feed: "msstore"}, []string{"--source", "msstore"}},
		{"override", config.WingetSpec{ID: "P.A", Override: "/S /D=C:\\App"}, []string{"--override", "/S /D=C:\\App"}},
	}

	w := NewWinget(false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			for _, verb := range []string{"install", "upgrade"} {
				var got []string
				if verb == "install" {
					got = w.installArgs(wingetApp(&spec), &spec)
				} else {
					got = w.upgradeArgs(wingetApp(&spec), &spec)
				}
				if !containsSeq(got, tt.want) {
					t.Errorf("%s argv missing %v\n got: %v", verb, tt.want, got)
				}
			}
		})
	}
}

func TestWingetAllSpecFlagsTogether(t *testing.T) {
	w := NewWinget(false)
	spec := &config.WingetSpec{
		ID:       "Publisher.App",
		Scope:    "user",
		Locale:   "en-US",
		Feed:     "winget",
		Override: "/quiet",
		Args:     []string{"--custom", "value"},
	}
	app := wingetApp(spec)
	app.Version = "1.2.3"

	got := w.installArgs(app, spec)
	for _, want := range [][]string{
		{"--version", "1.2.3"},
		{"--scope", "user"},
		{"--locale", "en-US"},
		{"--source", "winget"},
		{"--override", "/quiet"},
	} {
		if !containsSeq(got, want) {
			t.Errorf("argv missing %v\n got: %v", want, got)
		}
	}
	// spec.Args must come last so a user flag can override ours.
	if !argsEqual(got[len(got)-2:], []string{"--custom", "value"}) {
		t.Errorf("spec.Args should be last, got tail %v (full: %v)", got[len(got)-2:], got)
	}
}

func TestWingetVersionLatestIsNotPinned(t *testing.T) {
	w := NewWinget(false)
	spec := &config.WingetSpec{ID: "P.A"}

	for _, version := range []string{"", "latest", "LATEST"} {
		app := wingetApp(spec)
		app.Version = version
		if got := w.installArgs(app, spec); hasFlag(got, "--version") {
			t.Errorf("version %q should not produce --version, got: %v", version, got)
		}
	}

	app := wingetApp(spec)
	app.Version = "2.0.0"
	if got := w.installArgs(app, spec); !containsSeq(got, []string{"--version", "2.0.0"}) {
		t.Errorf("pinned version missing from argv: %v", got)
	}
}

func TestWingetSkipUpgradeAddsNoUpgradeToInstallOnly(t *testing.T) {
	w := NewWinget(false)
	spec := &config.WingetSpec{ID: "P.A"}
	app := wingetApp(spec)
	app.SkipUpgrade = true

	if got := w.installArgs(app, spec); !hasFlag(got, "--no-upgrade") {
		t.Errorf("skip_upgrade should add --no-upgrade to install: %v", got)
	}
	// upgrade is never reached with skip_upgrade set, and --no-upgrade is not a
	// valid flag for `winget upgrade` anyway.
	if got := w.upgradeArgs(app, spec); hasFlag(got, "--no-upgrade") {
		t.Errorf("upgrade argv must not carry --no-upgrade: %v", got)
	}
}

func TestWingetForceAddsForceToInstallOnly(t *testing.T) {
	spec := &config.WingetSpec{ID: "P.A"}
	app := wingetApp(spec)

	if got := NewWinget(true).installArgs(app, spec); !hasFlag(got, "--force") {
		t.Errorf("force should add --force: %v", got)
	}
	if got := NewWinget(false).installArgs(app, spec); hasFlag(got, "--force") {
		t.Errorf("no force should not add --force: %v", got)
	}
	// `winget upgrade --force` does not reinstall an up-to-date package, so
	// upgrade() routes through install() instead of adding the flag here.
	if got := NewWinget(true).upgradeArgs(app, spec); hasFlag(got, "--force") {
		t.Errorf("upgrade argv must not carry --force: %v", got)
	}
}

func TestWingetUpgradeArgsUseUpgradeVerb(t *testing.T) {
	w := NewWinget(false)
	spec := &config.WingetSpec{ID: "Publisher.App"}

	got := w.upgradeArgs(wingetApp(spec), spec)
	want := []string{
		"upgrade", "--id", "Publisher.App", "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if !argsEqual(got, want) {
		t.Errorf("upgradeArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestWingetUninstallArgs(t *testing.T) {
	w := NewWinget(false)
	got := w.uninstallArgs(&config.WingetSpec{ID: "Publisher.App", Scope: "machine"})
	want := []string{
		"uninstall", "--id", "Publisher.App", "--exact",
		"--silent", "--accept-source-agreements",
	}
	if !argsEqual(got, want) {
		t.Errorf("uninstallArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestWingetSpecMissingIsNotOffered(t *testing.T) {
	w := NewWinget(false)

	for _, tt := range []struct {
		name string
		app  config.Application
	}{
		{"no block", config.Application{Name: "X"}},
		{"empty id", wingetApp(&config.WingetSpec{})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := w.spec(tt.app); !errors.Is(err, ErrNotOffered) {
				t.Errorf("want ErrNotOffered, got %v", err)
			}
		})
	}
}
