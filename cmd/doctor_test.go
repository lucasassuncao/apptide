package cmd

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetVersion(t *testing.T) {
	if got := getVersion(filepath.Join(t.TempDir(), "missing"), "--version", ""); got != "unknown" {
		t.Errorf("missing binary = %q, want unknown", got)
	}

	// The go binary is on hand wherever the tests run, and prints its version
	// on one line: "go version go1.27.0 windows/amd64".
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not in PATH")
	}
	if got := getVersion(goBin, "version", ""); !strings.HasPrefix(got, "go version ") {
		t.Errorf("first line = %q, want the go version line", got)
	}
	if got := getVersion(goBin, "version", `go(\d+\.\d+)`); strings.ContainsAny(got, " go") {
		t.Errorf("pattern = %q, want only the captured version", got)
	}
}

// With no manager on the machine, every one is reported missing and
// installable, and the run is not OK.
func TestDoctorJSONWithNoManagers(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	binDir := filepath.Join(t.TempDir(), "bin")

	var ok bool
	out := captureCmdStdout(t, func() { ok = runDoctorJSON(binDir) })
	if ok {
		t.Error("doctor passed with no package manager")
	}

	var got struct {
		Managers []struct {
			Name            string `json:"name"`
			Available       bool   `json:"available"`
			AutoInstallable bool   `json:"auto_installable"`
		} `json:"managers"`
		Apptide struct {
			InstallDir string `json:"install_dir"`
			Exists     bool   `json:"install_dir_exists"`
		} `json:"apptide"`
		AllOK bool `json:"all_ok"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\nraw: %s", err, out)
	}
	if len(got.Managers) != len(managers) {
		t.Fatalf("got %d managers, want %d", len(got.Managers), len(managers))
	}
	for _, m := range got.Managers {
		if m.Available || !m.AutoInstallable {
			t.Errorf("%s = available %v, auto_installable %v", m.Name, m.Available, m.AutoInstallable)
		}
	}
	if got.Apptide.InstallDir != binDir || got.Apptide.Exists {
		t.Errorf("apptide = %+v, want %q, not existing", got.Apptide, binDir)
	}
	if got.AllOK {
		t.Error("all_ok = true")
	}
}
