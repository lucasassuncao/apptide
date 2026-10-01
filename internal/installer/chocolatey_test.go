package installer

import (
	"errors"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

func chocoApp(spec *config.ChocolateySpec) config.Application {
	return config.Application{
		Name:    "Test App",
		Source:  config.Sources{config.SourceChocolatey},
		Package: config.Packages{Chocolatey: spec},
	}
}

func TestChocolateyArgsBase(t *testing.T) {
	c := NewChocolatey(false)
	spec := &config.ChocolateySpec{ID: "some-package"}

	for _, verb := range []string{"install", "upgrade"} {
		got := c.args(verb, chocoApp(spec), spec)
		want := []string{verb, "some-package", "--yes", "--no-progress"}
		if !argsEqual(got, want) {
			t.Errorf("args(%q)\n got: %v\nwant: %v", verb, got, want)
		}
	}
}

func TestChocolateySpecFlagsReachCommandLine(t *testing.T) {
	tests := []struct {
		name string
		spec config.ChocolateySpec
		want []string
	}{
		{
			"package_params goes to the package script",
			config.ChocolateySpec{ID: "p", PackageParams: "/Dir:C:\\tools"},
			[]string{"--package-parameters", "/Dir:C:\\tools"},
		},
		{
			"install_args goes to the native installer",
			config.ChocolateySpec{ID: "p", InstallArgs: "/quiet /norestart"},
			[]string{"--install-arguments", "/quiet /norestart"},
		},
		{
			"feed",
			config.ChocolateySpec{ID: "p", Feed: "https://nuget.example/api/v2"},
			[]string{"--source", "https://nuget.example/api/v2"},
		},
		{
			"allow_downgrade",
			config.ChocolateySpec{ID: "p", AllowDowngrade: true},
			[]string{"--allow-downgrade"},
		},
	}

	c := NewChocolatey(false)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.spec
			for _, verb := range []string{"install", "upgrade"} {
				got := c.args(verb, chocoApp(&spec), &spec)
				if !containsSeq(got, tt.want) {
					t.Errorf("%s argv missing %v\n got: %v", verb, tt.want, got)
				}
			}
		})
	}
}

// package_params and install_args are two distinct chocolatey channels — one
// reaches the package script, the other the installer it wraps. Setting one
// must never emit the other's flag.
func TestChocolateyParamChannelsAreIndependent(t *testing.T) {
	c := NewChocolatey(false)

	onlyParams := &config.ChocolateySpec{ID: "p", PackageParams: "/X"}
	if got := c.args("install", chocoApp(onlyParams), onlyParams); hasFlag(got, "--install-arguments") {
		t.Errorf("package_params alone must not emit --install-arguments: %v", got)
	}

	onlyInstall := &config.ChocolateySpec{ID: "p", InstallArgs: "/Y"}
	if got := c.args("install", chocoApp(onlyInstall), onlyInstall); hasFlag(got, "--package-parameters") {
		t.Errorf("install_args alone must not emit --package-parameters: %v", got)
	}

	both := &config.ChocolateySpec{ID: "p", PackageParams: "/X", InstallArgs: "/Y"}
	got := c.args("install", chocoApp(both), both)
	if !containsSeq(got, []string{"--package-parameters", "/X"}) ||
		!containsSeq(got, []string{"--install-arguments", "/Y"}) {
		t.Errorf("both channels should reach the argv: %v", got)
	}
}

func TestChocolateyAllSpecFlagsTogether(t *testing.T) {
	c := NewChocolatey(true)
	spec := &config.ChocolateySpec{
		ID:             "some-package",
		PackageParams:  "/Dir:C:\\tools",
		InstallArgs:    "/quiet",
		Feed:           "internal",
		AllowDowngrade: true,
		Args:           []string{"--ignore-checksums"},
	}
	app := chocoApp(spec)
	app.Version = "3.1.0"

	got := c.args("install", app, spec)
	for _, want := range [][]string{
		{"--version", "3.1.0"},
		{"--force"},
		{"--package-parameters", "/Dir:C:\\tools"},
		{"--install-arguments", "/quiet"},
		{"--source", "internal"},
		{"--allow-downgrade"},
	} {
		if !containsSeq(got, want) {
			t.Errorf("argv missing %v\n got: %v", want, got)
		}
	}
	if got[len(got)-1] != "--ignore-checksums" {
		t.Errorf("spec.Args should be last, got tail %q (full: %v)", got[len(got)-1], got)
	}
}

func TestChocolateyVersionLatestIsNotPinned(t *testing.T) {
	c := NewChocolatey(false)
	spec := &config.ChocolateySpec{ID: "p"}

	for _, version := range []string{"", "latest", "Latest"} {
		app := chocoApp(spec)
		app.Version = version
		if got := c.args("install", app, spec); hasFlag(got, "--version") {
			t.Errorf("version %q should not produce --version, got: %v", version, got)
		}
	}
}

func TestChocolateyForceFlag(t *testing.T) {
	spec := &config.ChocolateySpec{ID: "p"}

	if got := NewChocolatey(true).args("install", chocoApp(spec), spec); !hasFlag(got, "--force") {
		t.Errorf("force should add --force: %v", got)
	}
	if got := NewChocolatey(false).args("install", chocoApp(spec), spec); hasFlag(got, "--force") {
		t.Errorf("no force should not add --force: %v", got)
	}
}

func TestChocolateyUninstallArgs(t *testing.T) {
	c := NewChocolatey(true)
	// Install-time flags must not leak into a removal.
	got := c.uninstallArgs(&config.ChocolateySpec{
		ID:            "some-package",
		Feed:          "internal",
		InstallArgs:   "/quiet",
		PackageParams: "/X",
		Args:          []string{"--ignore-checksums"},
	})
	want := []string{"uninstall", "some-package", "--yes"}
	if !argsEqual(got, want) {
		t.Errorf("uninstallArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestChocolateySpecMissingIsNotOffered(t *testing.T) {
	c := NewChocolatey(false)

	for _, tt := range []struct {
		name string
		app  config.Application
	}{
		{"no block", config.Application{Name: "X"}},
		{"empty id", chocoApp(&config.ChocolateySpec{})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.spec(tt.app); !errors.Is(err, ErrNotOffered) {
				t.Errorf("want ErrNotOffered, got %v", err)
			}
		})
	}
}

func TestFilterChocoOutputStripsNonAdminWarning(t *testing.T) {
	const block = `Chocolatey detected you are not running from an elevated command shell
(cmd/powershell).

 You may experience errors - many functions/packages
 require admin rights.

Do you want to continue?([Y]es/[N]o):`

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"warning before real output",
			block + "\nsome-package v1.0.0 already installed.",
			"some-package v1.0.0 already installed.",
		},
		{
			"two warnings",
			block + "\nfirst line\n" + block + "\nsecond line",
			"first line\nsecond line",
		},
		{
			"truncated warning with no terminator drops the tail",
			"real output\nChocolatey detected you are not running from an elevated command shell\n(cut off",
			"real output",
		},
		{
			"output without a warning is untouched",
			"Installing the following packages:\nsome-package",
			"Installing the following packages:\nsome-package",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := filterChocoOutput(tt.in); got != tt.want {
				t.Errorf("filterChocoOutput()\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

// runChoco reads the filtered output to decide ErrAlreadyInstalled; the phrases
// it looks for must survive filtering.
func TestChocoAlreadyInstalledPhrasesSurviveFiltering(t *testing.T) {
	for _, phrase := range []string{
		"some-package v1.0.0 already installed.",
		"some-package is already up to date.",
	} {
		got := strings.ToLower(filterChocoOutput(phrase))
		if !strings.Contains(got, "already installed") && !strings.Contains(got, "already up to date") {
			t.Errorf("phrase %q lost its marker after filtering: %q", phrase, got)
		}
	}
}

// Chocolatey 2.x removed --local-only from the list command and rejects the
// whole invocation when it is passed, which made every lookup report "not
// installed": no upgrades, and uninstall refusing to run.
func TestChocolateyCheckArgsAvoidRemovedFlag(t *testing.T) {
	args := chocoCheckArgs("git")

	for _, arg := range args {
		if arg == "--local-only" || arg == "-lo" {
			t.Fatalf("checkArgs still passes a removed flag: %v", args)
		}
	}

	// --exact matters just as much: without it "git" also matches git-lfs, and
	// the first matching line decided the reported version.
	var hasExact, hasLimit bool
	for _, arg := range args {
		switch arg {
		case "--exact":
			hasExact = true
		case "--limit-output":
			hasLimit = true
		}
	}
	if !hasExact {
		t.Errorf("checkArgs must pass --exact: %v", args)
	}
	if !hasLimit {
		t.Errorf("checkArgs must pass --limit-output: %v", args)
	}
}

func TestParseChocoList(t *testing.T) {
	// The shape of `choco list <id> --exact --limit-output`.
	const out = "chocolatey|2.7.3\n"

	installed, version := parseChocoList(out, "chocolatey")
	if !installed || version != "2.7.3" {
		t.Errorf("parseChocoList = (%v, %q), want (true, \"2.7.3\")", installed, version)
	}

	// An absent package produces no line at all, and choco still exits 0.
	if installed, _ := parseChocoList("", "missing"); installed {
		t.Error("an empty listing was read as installed")
	}
}

// Matching must be on the whole id, not a substring, so a package whose name
// merely contains the query is not mistaken for it.
func TestParseChocoListDoesNotMatchSubstrings(t *testing.T) {
	const out = "git-lfs|3.4.0\nchocolatey-core.extension|1.4.0\n"

	if installed, version := parseChocoList(out, "git"); installed {
		t.Errorf("git-lfs was reported as git (version %q)", version)
	}
	if installed, _ := parseChocoList(out, "chocolatey"); installed {
		t.Error("chocolatey-core.extension was reported as chocolatey")
	}
}

func TestParseChocoListIgnoresCase(t *testing.T) {
	if installed, version := parseChocoList("GoLang|1.26.1\n", "golang"); !installed || version != "1.26.1" {
		t.Errorf("case-insensitive lookup failed: (%v, %q)", installed, version)
	}
}
