package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/ghrelease"
)

func githubApp(name string, spec *config.GitHubSpec) config.Application {
	return config.Application{
		Name:    name,
		Source:  config.Sources{config.SourceGitHub},
		Package: config.Packages{GitHub: spec},
	}
}

func TestMsiexecArgs(t *testing.T) {
	got := msiexecArgs(`C:\tmp\app.msi`, &config.GitHubSpec{ID: "owner/repo"})
	want := []string{"/i", `C:\tmp\app.msi`, "/quiet", "/norestart"}
	if !argsEqual(got, want) {
		t.Errorf("msiexecArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestMsiexecArgsAppendsSpecArgsLast(t *testing.T) {
	got := msiexecArgs(`C:\tmp\app.msi`, &config.GitHubSpec{
		ID:   "owner/repo",
		Args: []string{"ALLUSERS=1", "/log", `C:\tmp\install.log`},
	})
	want := []string{
		"/i", `C:\tmp\app.msi`, "/quiet", "/norestart",
		"ALLUSERS=1", "/log", `C:\tmp\install.log`,
	}
	if !argsEqual(got, want) {
		t.Errorf("msiexecArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestBinaryBaseName(t *testing.T) {
	tests := []struct {
		name string
		app  config.Application
		want string
	}{
		{
			"falls back to the lowercased application name",
			githubApp("Lazygit", &config.GitHubSpec{ID: "jesseduffield/lazygit"}),
			"lazygit",
		},
		{
			"binary_name wins over the name",
			githubApp("GitHub CLI", &config.GitHubSpec{ID: "cli/cli", BinaryName: "gh"}),
			"gh",
		},
		{
			"binary_name is lowercased too",
			githubApp("GitHub CLI", &config.GitHubSpec{ID: "cli/cli", BinaryName: "GH"}),
			"gh",
		},
		{
			"no github block at all",
			config.Application{Name: "Some App"},
			"some app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := binaryBaseName(tt.app); got != tt.want {
				t.Errorf("binaryBaseName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTargetDirPrefersSpecInstallDir(t *testing.T) {
	g := NewGitHub("", `C:\default\bin`)

	if got := g.targetDir(githubApp("App", &config.GitHubSpec{ID: "o/r"})); got != `C:\default\bin` {
		t.Errorf("without install_dir = %q, want the default", got)
	}

	spec := &config.GitHubSpec{ID: "o/r", InstallDir: `C:\custom`}
	if got := g.targetDir(githubApp("App", spec)); got != `C:\custom` {
		t.Errorf("with install_dir = %q, want C:\\custom", got)
	}
}

func TestMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if _, ok := readMarker(dir, "lazygit"); ok {
		t.Fatal("readMarker should report false before anything is written")
	}

	want := marker{
		Version:      "v0.40.2",
		Repo:         "jesseduffield/lazygit",
		Asset:        "lazygit_Windows_x86_64.zip",
		InstalledAt:  "2026-07-29T00:00:00Z",
		RunInstaller: true,
	}
	if err := writeMarker(dir, "lazygit", want); err != nil {
		t.Fatalf("writeMarker: %v", err)
	}

	got, ok := readMarker(dir, "lazygit")
	if !ok {
		t.Fatal("readMarker reported false after a successful write")
	}
	if got != want {
		t.Errorf("marker round trip\n got: %+v\nwant: %+v", got, want)
	}

	// The marker lives beside the binary, in a dot-directory.
	if _, err := filepath.Rel(dir, markerPath(dir, "lazygit")); err != nil {
		t.Errorf("marker path is not under the install dir: %v", err)
	}
}

func TestGitHubCheckFindsBinaryAndVersion(t *testing.T) {
	dir := t.TempDir()
	g := NewGitHub("", dir)
	app := githubApp("Lazygit", &config.GitHubSpec{ID: "jesseduffield/lazygit"})

	if installed, _ := g.Check(app); installed {
		t.Fatal("Check should report not installed on an empty dir")
	}

	// A binary with no marker: installed, but the release is unknown.
	writeTestFile(t, filepath.Join(dir, "lazygit.exe"))
	installed, version := g.Check(app)
	if !installed || version != "" {
		t.Errorf("binary without marker: got (%v, %q), want (true, \"\")", installed, version)
	}

	// With a marker, the release becomes visible.
	if err := writeMarker(dir, "lazygit", marker{Version: "v0.40.2"}); err != nil {
		t.Fatal(err)
	}
	if installed, version = g.Check(app); !installed || version != "v0.40.2" {
		t.Errorf("binary with marker: got (%v, %q), want (true, \"v0.40.2\")", installed, version)
	}
}

// With run_installer the program is registered with Windows and there are no
// files of ours to find — the marker is the only trace.
func TestGitHubCheckTrustsRunInstallerMarker(t *testing.T) {
	dir := t.TempDir()
	g := NewGitHub("", dir)
	app := githubApp("Some Installer", &config.GitHubSpec{ID: "o/r", RunInstaller: true})

	if err := writeMarker(dir, "some installer", marker{Version: "1.2.3", RunInstaller: true}); err != nil {
		t.Fatal(err)
	}

	installed, version := g.Check(app)
	if !installed || version != "1.2.3" {
		t.Errorf("got (%v, %q), want (true, \"1.2.3\")", installed, version)
	}
}

func TestGitHubCheckUsesBinaryName(t *testing.T) {
	dir := t.TempDir()
	g := NewGitHub("", dir)
	app := githubApp("GitHub CLI", &config.GitHubSpec{ID: "cli/cli", BinaryName: "gh"})

	// A file named after the application, not the binary, must not count.
	writeTestFile(t, filepath.Join(dir, "github cli.exe"))
	if installed, _ := g.Check(app); installed {
		t.Error("Check matched the application name instead of binary_name")
	}

	writeTestFile(t, filepath.Join(dir, "gh.exe"))
	if installed, _ := g.Check(app); !installed {
		t.Error("Check did not find the binary_name executable")
	}
}

func TestSelectAssetPatternWins(t *testing.T) {
	assets := []ghrelease.Asset{
		{Name: "app_windows_amd64.zip"},
		{Name: "app_windows_arm64.zip"},
		{Name: "app_windows_i386.zip"},
	}

	app := githubApp("App", &config.GitHubSpec{ID: "o/r", AssetPattern: "*i386*"})
	got := selectAsset(assets, app)
	if got == nil || got.Name != "app_windows_i386.zip" {
		t.Errorf("asset_pattern should win over scoring, got %v", got)
	}
}

// A pattern that matches nothing falls through to scoring rather than failing:
// the user gets an install, and validate is where a bad glob is surfaced.
func TestSelectAssetPatternNoMatchFallsBackToScoring(t *testing.T) {
	assets := []ghrelease.Asset{{Name: "app_windows_amd64.zip"}, {Name: "app_linux_amd64.tar.gz"}}

	app := githubApp("App", &config.GitHubSpec{ID: "o/r", AssetPattern: "*nothing*"})
	got := selectAsset(assets, app)
	if got == nil || got.Name != "app_windows_amd64.zip" {
		t.Errorf("expected the scored Windows asset, got %v", got)
	}
}

func TestSelectAssetPrefersWindowsAmd64Archive(t *testing.T) {
	assets := []ghrelease.Asset{
		{Name: "app_linux_amd64.tar.gz"},
		{Name: "app_darwin_arm64.tar.gz"},
		{Name: "app_windows_amd64.zip"},
		{Name: "app_windows_amd64.msi"},
		{Name: "checksums.txt"},
	}

	got := selectAsset(assets, githubApp("App", &config.GitHubSpec{ID: "o/r"}))
	if got == nil || got.Name != "app_windows_amd64.zip" {
		t.Errorf("expected the Windows amd64 zip, got %v", got)
	}
}

func TestSelectAssetReturnsNilWhenNothingIsUsable(t *testing.T) {
	assets := []ghrelease.Asset{
		{Name: "app_linux_amd64.tar.gz"},
		{Name: "app_darwin_arm64.zip"},
		{Name: "checksums.txt"},
		{Name: "app.sha256"},
	}

	if got := selectAsset(assets, githubApp("App", &config.GitHubSpec{ID: "o/r"})); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestScoreAssetExcludesNonWindowsAndMetadata(t *testing.T) {
	for _, name := range []string{
		"app_linux_amd64.tar.gz",
		"app_darwin_arm64.zip",
		"app_windows_arm64.zip",
		"app_android.apk",
		"app.deb", "app.rpm", "app.dmg", "app.pkg",
		"checksums.txt", "app.sha256", "app.sha512", "app.sig", "app.asc",
		"source.zip",
	} {
		if got := scoreAsset(name); got != 0 {
			t.Errorf("scoreAsset(%q) = %d, want 0 (excluded)", name, got)
		}
	}
}

func TestScoreAssetRanking(t *testing.T) {
	tests := []struct {
		higher string
		lower  string
		why    string
	}{
		{"app_windows_amd64.zip", "app_amd64.zip", "an explicit windows marker beats none"},
		{"app_windows_amd64.zip", "app_windows.zip", "an explicit arch marker beats none"},
		{"app_windows_amd64.zip", "app_windows_amd64.exe", "a portable archive beats an installer"},
		{"app_windows_amd64.tar.gz", "app_windows_amd64.msi", "a portable archive beats an msi"},
	}

	for _, tt := range tests {
		t.Run(tt.why, func(t *testing.T) {
			h, l := scoreAsset(tt.higher), scoreAsset(tt.lower)
			if h <= l {
				t.Errorf("scoreAsset(%q)=%d should beat scoreAsset(%q)=%d", tt.higher, h, tt.lower, l)
			}
		})
	}
}

// writeTestFile creates a placeholder file so Check has something to stat.
func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("binary"), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// An ARM machine used to get nothing at all from the github source: every
// arm64 asset was excluded by name before it could be scored.
func TestScoreAssetForArm64PrefersNativeThenEmulated(t *testing.T) {
	native := scoreAssetFor("app_windows_arm64.zip", "arm64")
	emulated := scoreAssetFor("app_windows_amd64.zip", "arm64")
	foreign := scoreAssetFor("app_windows_i386.zip", "arm64")

	if native == 0 {
		t.Fatal("the native arm64 asset was excluded")
	}
	if native <= emulated {
		t.Errorf("arm64 scored %d, amd64 scored %d; native must win", native, emulated)
	}
	if emulated == 0 {
		t.Error("amd64 should stay usable under emulation, not be excluded")
	}
	if foreign != 0 {
		t.Errorf("a 32-bit asset scored %d on arm64, want 0", foreign)
	}
}

func TestScoreAssetForAmd64RejectsOtherArchitectures(t *testing.T) {
	if got := scoreAssetFor("app_windows_arm64.zip", "amd64"); got != 0 {
		t.Errorf("arm64 asset scored %d on an amd64 machine, want 0", got)
	}
	if got := scoreAssetFor("app_windows_amd64.zip", "amd64"); got == 0 {
		t.Error("the native amd64 asset was excluded")
	}
}

// A portable archive is preferred over an installer for the same architecture.
func TestScoreAssetForPrefersArchivesOverInstallers(t *testing.T) {
	zip := scoreAssetFor("app_windows_amd64.zip", "amd64")
	msi := scoreAssetFor("app_windows_amd64.msi", "amd64")
	if zip <= msi {
		t.Errorf("zip scored %d, msi scored %d; the archive should win", zip, msi)
	}
}

// Ties must not depend on sort order: the same release has to resolve to the
// same asset on every run.
func TestSelectAssetIsStableAcrossRuns(t *testing.T) {
	assets := []ghrelease.Asset{
		{Name: "app_windows_amd64_a.zip"},
		{Name: "app_windows_amd64_b.zip"},
		{Name: "app_windows_amd64_c.zip"},
	}
	app := githubApp("App", &config.GitHubSpec{ID: "o/r"})

	first := selectAsset(assets, app)
	if first == nil {
		t.Fatal("nothing selected")
	}
	for i := 0; i < 20; i++ {
		again := selectAsset(assets, app)
		if again.Name != first.Name {
			t.Fatalf("selection changed between runs: %q then %q", first.Name, again.Name)
		}
	}
}

func TestSafeDestRejectsAnEscapingAssetName(t *testing.T) {
	dir := filepath.Join("C:", "dest")

	got, err := safeDest(dir, "../../evil.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Base() strips the path, so the write stays inside dir.
	if filepath.Dir(got) != dir {
		t.Errorf("safeDest escaped: %q", got)
	}
	if filepath.Base(got) != "evil.exe" {
		t.Errorf("unexpected file name %q", filepath.Base(got))
	}
}
