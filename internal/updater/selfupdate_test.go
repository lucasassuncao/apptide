package updater

import (
	"testing"

	"github.com/lucasassuncao/apptide/internal/ghrelease"
)

// releaseAssets is the asset list the project's own goreleaser config produces.
func releaseAssets() []ghrelease.Asset {
	return []ghrelease.Asset{
		{Name: "apptide_1.4.0_Windows_x86_64.exe"},
		{Name: "apptide_1.4.0_Windows_arm64.exe"},
		{Name: "apptide_1.4.0_checksums.txt"},
	}
}

// apptide ships windows/amd64 and windows/arm64, but the selector scored only
// amd64, so an ARM machine replaced its native binary with the emulated x64
// one on every self-update.
func TestSelectWindowsAssetPicksTheNativeBuild(t *testing.T) {
	tests := map[string]string{
		"amd64": "apptide_1.4.0_Windows_x86_64.exe",
		"arm64": "apptide_1.4.0_Windows_arm64.exe",
	}

	for goarch, want := range tests {
		t.Run(goarch, func(t *testing.T) {
			got := selectWindowsAssetFor(releaseAssets(), goarch)
			if got == nil {
				t.Fatalf("nothing selected for %s", goarch)
			}
			if got.Name != want {
				t.Errorf("selected %q, want %q", got.Name, want)
			}
		})
	}
}

func TestSelectWindowsAssetSkipsChecksums(t *testing.T) {
	assets := []ghrelease.Asset{{Name: "apptide_1.4.0_checksums.txt"}}
	if got := selectWindowsAssetFor(assets, "amd64"); got != nil {
		t.Errorf("selected the checksum file: %q", got.Name)
	}
}

// A release with no build for this machine must select nothing rather than
// hand back something that cannot run.
func TestSelectWindowsAssetReturnsNilWhenNothingFits(t *testing.T) {
	assets := []ghrelease.Asset{
		{Name: "apptide_1.4.0_Linux_x86_64"},
		{Name: "apptide_1.4.0_Darwin_arm64"},
	}
	if got := selectWindowsAssetFor(assets, "amd64"); got != nil {
		t.Errorf("selected %q, want nil", got.Name)
	}
}

// A release that predates the arm64 build still has to update an ARM machine,
// under emulation, rather than refuse.
func TestSelectWindowsAssetFallsBackToEmulationOnArm(t *testing.T) {
	assets := []ghrelease.Asset{{Name: "apptide_1.0.0_Windows_x86_64.exe"}}

	got := selectWindowsAssetFor(assets, "arm64")
	if got == nil {
		t.Fatal("an ARM machine was left with no update path at all")
	}
	if got.Name != "apptide_1.0.0_Windows_x86_64.exe" {
		t.Errorf("selected %q", got.Name)
	}
}

func TestSelectWindowsAssetIsStable(t *testing.T) {
	assets := releaseAssets()
	first := selectWindowsAssetFor(assets, "amd64")
	for i := 0; i < 20; i++ {
		again := selectWindowsAssetFor(assets, "amd64")
		if again.Name != first.Name {
			t.Fatalf("selection changed: %q then %q", first.Name, again.Name)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	for in, want := range map[string]string{
		"v1.2.3": "1.2.3",
		"1.2.3":  "1.2.3",
		"dev":    "dev",
		"":       "",
	} {
		if got := normalizeVersion(in); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}
