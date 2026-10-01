package ghrelease

import "testing"

func TestArchOf(t *testing.T) {
	tests := map[string]string{
		"app_windows_amd64.zip":  "amd64",
		"app_windows_x86_64.zip": "amd64",
		"app-win64.exe":          "amd64",
		"app_windows_arm64.zip":  "arm64",
		"app_windows_aarch64.7z": "arm64",
		"app_win32.exe":          "386",
		"app_windows_i386.zip":   "386",
		"app-armv7.zip":          "arm",
		"app.zip":                "",
		"tool-setup.exe":         "",
	}

	for name, want := range tests {
		if got := ArchOf(name); got != want {
			t.Errorf("ArchOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// aarch64 contains neither "x64" nor a 32-bit token, but the ordering is what
// keeps it from being read as one.
func TestArchOfOrderingEdgeCases(t *testing.T) {
	if got := ArchOf("app_aarch64.zip"); got != "arm64" {
		t.Errorf("aarch64 read as %q", got)
	}
	if got := ArchOf("app_arm64_win64.zip"); got != "arm64" {
		t.Errorf("a name carrying both tokens resolved to %q, want arm64", got)
	}
}

func TestArchScore(t *testing.T) {
	tests := []struct {
		name    string
		asset   string
		goarch  string
		wantOK  bool
		wantPos bool // score should be above zero
	}{
		{"native amd64", "app_windows_amd64.zip", "amd64", true, true},
		{"native arm64", "app_windows_arm64.zip", "arm64", true, true},
		{"no arch named", "app.zip", "amd64", true, false},
		{"arm64 asset on amd64 machine", "app_windows_arm64.zip", "amd64", false, false},
		{"32-bit on amd64", "app_windows_i386.zip", "amd64", false, false},
		// Windows on ARM emulates x64, so this is usable but not preferred.
		{"amd64 asset on arm64 machine", "app_windows_amd64.zip", "arm64", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, ok := ArchScore(tt.asset, tt.goarch)
			if ok != tt.wantOK {
				t.Fatalf("ArchScore(%q, %q) ok = %v, want %v", tt.asset, tt.goarch, ok, tt.wantOK)
			}
			if (score > 0) != tt.wantPos {
				t.Errorf("score = %d, wantPositive = %v", score, tt.wantPos)
			}
		})
	}
}

// A native build must outrank the emulated one on an ARM machine. This is the
// regression test for self-update replacing an arm64 binary with the x64 one.
func TestScoreBinaryPrefersNativeArm64(t *testing.T) {
	native := ScoreBinary("apptide_1.0.0_Windows_arm64.exe", "arm64")
	emulated := ScoreBinary("apptide_1.0.0_Windows_x86_64.exe", "arm64")

	if native <= emulated {
		t.Errorf("arm64 build scored %d, x86_64 scored %d; the native build must win",
			native, emulated)
	}
	if emulated == 0 {
		t.Error("the x86_64 build should remain usable as a fallback, not be excluded")
	}
}

func TestScoreBinaryExcludesNonBinaries(t *testing.T) {
	for _, name := range []string{
		"checksums.txt",
		"apptide_1.0.0_Windows_x86_64.exe.sha256",
		"apptide_1.0.0_Linux_x86_64",
		"apptide_1.0.0_Darwin_arm64",
		"app.sig",
	} {
		if got := ScoreBinary(name, "amd64"); got != 0 {
			t.Errorf("ScoreBinary(%q) = %d, want 0", name, got)
		}
	}
}

func TestExcludedLeavesArchitectureAlone(t *testing.T) {
	// Architecture is scored, not excluded: an arm64 asset is the right answer
	// on an arm64 machine, so Excluded must not reject it.
	if Excluded("app_windows_arm64.zip") {
		t.Error("an arm64 asset must not be excluded outright")
	}
}
