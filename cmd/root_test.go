package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTokenPrefersTheFlag(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-env")

	if got := resolveToken("from-flag"); got != "from-flag" {
		t.Errorf("resolveToken(flag) = %q, want from-flag", got)
	}
	if got := resolveToken(""); got != "from-env" {
		t.Errorf("resolveToken(\"\") = %q, want from-env", got)
	}
}

// configHome isolates the search path: a temp working directory and a temp
// %APPDATA%, so a config on the machine running the tests cannot leak in.
func configHome(t *testing.T) (cwd, appData string) {
	t.Helper()
	cwd, appData = t.TempDir(), t.TempDir()
	t.Chdir(cwd)
	t.Setenv("APPDATA", appData)
	return cwd, appData
}

func TestResolveConfigPathTakesTheFirstThatExists(t *testing.T) {
	cwd, appData := configHome(t)

	inAppData := filepath.Join(appData, "apptide", "packages.yaml")
	if err := os.MkdirAll(filepath.Dir(inAppData), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inAppData, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveConfigPath(); got != inAppData {
		t.Errorf("with only %%APPDATA%%: got %q, want %q", got, inAppData)
	}

	inCwd := filepath.Join(cwd, "packages.yaml")
	if err := os.WriteFile(inCwd, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := resolveConfigPath(); got != inCwd {
		t.Errorf("with both: got %q, want the working directory's %q", got, inCwd)
	}
}

// With no config anywhere the error names the preferred location, not the
// binary's directory.
func TestResolveConfigPathFallsBackToThePreferredLocation(t *testing.T) {
	cwd, _ := configHome(t)

	if got, want := resolveConfigPath(), filepath.Join(cwd, "packages.yaml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
