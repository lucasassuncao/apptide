package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lucasassuncao/apptide/internal/output"
	"github.com/lucasassuncao/apptide/internal/pathutil"
	"github.com/spf13/cobra"
)

// managerInfo describes a supported package manager.
type managerInfo struct {
	name           string
	binary         string
	versionArg     string
	versionPattern string   // optional regexp to extract version from output
	installHow     string   // human-readable install instructions
	installArgs    []string // powershell args to auto-install; nil means not auto-installable
}

var managers = []managerInfo{
	{
		name:       "winget",
		binary:     "winget",
		versionArg: "--version",
		installHow: "Installs via .msixbundle from GitHub Releases (requires Add-AppxPackage).",
		installArgs: []string{
			"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
			"-Command",
			"$out = \"$env:TEMP\\AppInstaller.msixbundle\"; " +
				"Invoke-WebRequest -Uri https://github.com/microsoft/winget-cli/releases/latest/download/Microsoft.DesktopAppInstaller_8wekyb3d8bbwe.msixbundle -OutFile $out; " +
				"Add-AppxPackage -Path $out",
		},
	},
	{
		name:           "scoop",
		binary:         "scoop",
		versionArg:     "--version",
		versionPattern: `(\d+\.\d+\.\d+)`,
		installHow:     "Run in PowerShell:  irm get.scoop.sh | iex",
		installArgs: []string{
			"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
			"-Command", "irm get.scoop.sh | iex",
		},
	},
	{
		name:       "chocolatey",
		binary:     "choco",
		versionArg: "--version",
		installHow: "Run in PowerShell (admin): see https://chocolatey.org/install",
		installArgs: []string{
			"powershell", "-NoProfile", "-ExecutionPolicy", "Bypass",
			"-Command",
			"Set-ExecutionPolicy Bypass -Scope Process -Force; " +
				"iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))",
		},
	},
}

var doctorInstallMissing bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check whether all supported package managers are installed and working",
	Example: `  apptide doctor
  apptide doctor --install-missing`,
	// RunE, not Run with os.Exit: exiting from inside the command skips the
	// deferred cleanup every other path relies on.
	RunE: func(cmd *cobra.Command, args []string) error {
		if !runDoctor() {
			return errors.New("one or more checks failed")
		}
		return nil
	},
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorInstallMissing, "install-missing", false, "attempt to install any missing package managers")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor() bool {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	binDir := filepath.Join(local, "apptide", "bin")

	if output.IsJSON() {
		return runDoctorJSON(binDir)
	}
	return runDoctorTable()
}

func runDoctorTable() bool {
	allOK := true

	section := th.Warning.Bold(true)
	fmt.Println("\n" + section.Render("[Package Managers]"))

	for _, m := range managers {
		path, err := exec.LookPath(m.binary)
		if err != nil {
			allOK = false
			fmt.Printf("  %s  %-14s %s\n", th.Danger.Render("✗"), m.name, th.Dim.Render("not found"))
			printMissingHint(m, &allOK)
			continue
		}

		version := getVersion(path, m.versionArg, m.versionPattern)
		fmt.Printf("  %s  %-14s %s  %s\n",
			th.Success.Render("✓"), m.name, th.Success.Render(version), th.Dim.Render(path))
	}

	// ── Self-update repo ────────────────────────────────────────────────────
	fmt.Println("\n" + section.Render("[Self-update]"))
	if repo := DefaultRepo; repo != "" {
		fmt.Printf("  %s  repo  %s\n", th.Success.Render("✓"), repo)
	} else {
		fmt.Printf("  %s  %s — self-update requires --repo flag\n",
			th.Dim.Render("-"), th.Dim.Render("repo not set"))
	}

	fmt.Println()
	if allOK {
		fmt.Println(th.Success.Render("✓ Everything looks good!") + "\n")
	} else {
		fmt.Println(th.Warning.Render("⚠ Some issues found. See suggestions above.") + "\n")
	}
	return allOK
}

func printMissingHint(m managerInfo, allOK *bool) {
	if !doctorInstallMissing {
		fmt.Printf("       %s\n\n", th.Dim.Render("↳ "+m.installHow))
		return
	}
	if m.installArgs == nil {
		fmt.Printf("       %s\n\n", th.Dim.Render("↳ cannot auto-install: "+m.installHow))
		return
	}
	fmt.Printf("       %s\n", th.Dim.Render("↳ installing "+m.name+"…"))
	if err := runInstall(m); err != nil {
		fmt.Printf("       %s\n\n", th.Danger.Render(fmt.Sprintf("✗ install failed: %v", err)))
	} else {
		fmt.Printf("       %s\n\n", th.Success.Render("✓ installed successfully"))
		*allOK = true
	}
}

func runDoctorJSON(binDir string) bool {
	type managerResult struct {
		Name            string `json:"name"`
		Available       bool   `json:"available"`
		Version         string `json:"version,omitempty"`
		Path            string `json:"path,omitempty"`
		AutoInstallable bool   `json:"auto_installable,omitempty"`
	}
	type apptideResult struct {
		InstallDir      string `json:"install_dir"`
		InstallDirExist bool   `json:"install_dir_exists"`
		InPath          bool   `json:"in_path"`
	}
	type result struct {
		Managers    []managerResult `json:"managers"`
		Apptide     apptideResult   `json:"apptide"`
		UpdaterRepo string          `json:"updater_repo,omitempty"`
		AllOK       bool            `json:"all_ok"`
	}

	var mgrs []managerResult
	allOK := true
	for _, m := range managers {
		p, err := exec.LookPath(m.binary)
		if err != nil {
			allOK = false
			mgrs = append(mgrs, managerResult{Name: m.name, Available: false, AutoInstallable: m.installArgs != nil})
			continue
		}
		mgrs = append(mgrs, managerResult{
			Name:      m.name,
			Available: true,
			Version:   getVersion(p, m.versionArg, m.versionPattern),
			Path:      p,
		})
	}

	_, statErr := os.Stat(binDir) //#nosec G703 -- binDir is built from %LOCALAPPDATA%, and this only stats it
	inPath := pathutil.IsInUserPath(binDir)
	if !inPath {
		allOK = false
	}

	output.PrintJSON(result{
		Managers: mgrs,
		Apptide: apptideResult{
			InstallDir:      binDir,
			InstallDirExist: statErr == nil,
			InPath:          inPath,
		},
		// The same source the table reports. This read $UPDATER_REPO, a name
		// that appears nowhere else in apptide, so the field was always empty.
		UpdaterRepo: DefaultRepo,
		AllOK:       allOK,
	})
	return allOK
}

// runInstall executes the install command for a package manager, streaming output to stdout.
func runInstall(m managerInfo) error {
	cmd := exec.Command(m.installArgs[0], m.installArgs[1:]...) //#nosec G204 -- installArgs is a constant in this file, not user input
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getVersion runs `binary versionArg` and returns the version string.
// If pattern is non-empty, it extracts the first capture group from the output.
// Otherwise, it returns the first non-empty line.
func getVersion(binary, arg, pattern string) string {
	out, err := exec.Command(binary, arg).Output() //#nosec G204 -- binary comes from exec.LookPath, arg is a constant in this file
	if err != nil {
		return "unknown"
	}
	text := strings.TrimSpace(string(out))
	if pattern != "" {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(text); len(m) > 1 {
			return m[1]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "unknown"
}
