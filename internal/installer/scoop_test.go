package installer

import (
	"errors"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func scoopApp(spec *config.ScoopSpec) config.Application {
	return config.Application{
		Name:    "Test App",
		Source:  config.Sources{config.SourceScoop},
		Package: config.Packages{Scoop: spec},
	}
}

func TestScoopArgsBase(t *testing.T) {
	spec := &config.ScoopSpec{ID: "vim"}

	for _, verb := range []string{"install", "update"} {
		got := scoopArgs(verb, spec)
		want := []string{verb, "vim"}
		if !argsEqual(got, want) {
			t.Errorf("scoopArgs(%q)\n got: %v\nwant: %v", verb, got, want)
		}
	}
}

func TestScoopSpecFlagsReachCommandLine(t *testing.T) {
	tests := []struct {
		name string
		spec config.ScoopSpec
		want []string
	}{
		{"global", config.ScoopSpec{ID: "vim", Global: true}, []string{"--global"}},
		{"arch 64bit", config.ScoopSpec{ID: "vim", Arch: "64bit"}, []string{"--arch", "64bit"}},
		{"arch arm64", config.ScoopSpec{ID: "vim", Arch: "arm64"}, []string{"--arch", "arm64"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			for _, verb := range []string{"install", "update"} {
				got := scoopArgs(verb, &spec)
				if !containsSeq(got, tt.want) {
					t.Errorf("%s argv missing %v\n got: %v", verb, tt.want, got)
				}
			}
		})
	}
}

func TestScoopAllSpecFlagsTogether(t *testing.T) {
	spec := &config.ScoopSpec{
		ID:     "vim",
		Global: true,
		Arch:   "64bit",
		Bucket: "extras",
		Args:   []string{"--independent"},
	}

	got := scoopArgs("install", spec)
	want := []string{"install", "vim", "--global", "--arch", "64bit", "--independent"}
	if !argsEqual(got, want) {
		t.Errorf("scoopArgs()\n got: %v\nwant: %v", got, want)
	}
	// The bucket is added with a separate `scoop bucket add` call, never as a
	// flag on install.
	if hasFlag(got, "extras") || hasFlag(got, "--bucket") {
		t.Errorf("bucket must not appear in the install argv: %v", got)
	}
}

// Scoop cannot pin a version, so app.Version must never reach the command line
// — validate warns about it instead.
func TestScoopIgnoresVersion(t *testing.T) {
	spec := &config.ScoopSpec{ID: "vim"}
	app := scoopApp(spec)
	app.Version = "9.0.1"

	for _, verb := range []string{"install", "update"} {
		got := scoopArgs(verb, spec)
		if hasFlag(got, "--version") || hasFlag(got, "9.0.1") {
			t.Errorf("%s argv must not carry a version: %v", verb, got)
		}
	}
}

func TestScoopUninstallArgsCarryOnlyGlobal(t *testing.T) {
	// --arch is not valid for `scoop uninstall`, and spec.Args are install flags.
	got := scoopUninstallArgs(&config.ScoopSpec{
		ID:     "vim",
		Global: true,
		Arch:   "64bit",
		Args:   []string{"--independent"},
	})
	want := []string{"uninstall", "vim", "--global"}
	if !argsEqual(got, want) {
		t.Errorf("scoopUninstallArgs()\n got: %v\nwant: %v", got, want)
	}

	local := scoopUninstallArgs(&config.ScoopSpec{ID: "vim"})
	if !argsEqual(local, []string{"uninstall", "vim"}) {
		t.Errorf("non-global uninstall\n got: %v", local)
	}
}

func TestScoopSpecMissingIsNotOffered(t *testing.T) {
	s := NewScoop(false)

	for _, tt := range []struct {
		name string
		app  config.Application
	}{
		{"no block", config.Application{Name: "X"}},
		{"empty id", scoopApp(&config.ScoopSpec{})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.spec(tt.app); !errors.Is(err, ErrNotOffered) {
				t.Errorf("want ErrNotOffered, got %v", err)
			}
		})
	}
}

func TestFilterScoopOutputStripsSelfUpdateBlock(t *testing.T) {
	const block = `Updating Scoop...
Updating Buckets...
Scoop was updated successfully!`

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"self-update before real output",
			block + "\n'vim' (9.0.1) was installed successfully!",
			"'vim' (9.0.1) was installed successfully!",
		},
		{
			"two blocks",
			block + "\nfirst\n" + block + "\nsecond",
			"first\nsecond",
		},
		{
			"permission error inside the block is stripped too",
			"Updating Scoop...\nMethodInvocationException: access denied\n  Line |\nScoop was updated successfully!\nvim: 9.0.1",
			"vim: 9.0.1",
		},
		{
			"truncated block with no terminator drops the tail",
			"real output\nUpdating Scoop...\n(cut off",
			"real output",
		},
		{
			"output without a block is untouched",
			"'vim' (9.0.1) is already installed.",
			"'vim' (9.0.1) is already installed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterScoopOutput(tt.in); got != tt.want {
				t.Errorf("filterScoopOutput()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}
