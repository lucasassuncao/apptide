package cmd

import (
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

const (
	doctorReset  = "\033[0m"
	doctorBold   = "\033[1m"
	doctorRed    = "\033[31m"
	doctorGreen  = "\033[32m"
	doctorYellow = "\033[33m"
	doctorGray   = "\033[90m"
)

var doctorInstallMissing bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check whether all supported package managers are installed and working",
	Example: `  apptide doctor
  apptide doctor --install-missing`,
	Run: func(cmd *cobra.Command, args []string) {
		ok := runDoctor()
		if !ok {
			os.Exit(1)
		}
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

	fmt.Printf("\n%s%s[Package Managers]%s\n", doctorBold, doctorYellow, doctorReset)

	for _, m := range managers {
		path, err := exec.LookPath(m.binary)
		if err != nil {
			allOK = false
			fmt.Printf("  %s✗%s  %-14s %snot found%s\n", doctorRed, doctorReset, m.name, doctorGray, doctorReset)
			printMissingHint(m, &allOK)
			continue
		}

		version := getVersion(path, m.versionArg, m.versionPattern)
		fmt.Printf("  %s✓%s  %-14s %s%s%s  %s%s%s\n",
			doctorGreen, doctorReset,
			m.name,
			doctorGreen, version, doctorReset,
			doctorGray, path, doctorReset,
		)
	}

	// ── Self-update repo ────────────────────────────────────────────────────
	fmt.Printf("\n%s%s[Self-update]%s\n", doctorBold, doctorYellow, doctorReset)
	if repo := DefaultRepo; repo != "" {
		fmt.Printf("  %s✓%s  repo  %s\n", doctorGreen, doctorReset, repo)
	} else {
		fmt.Printf("  %s-%s  %srepo not set%s — self-update requires --repo flag\n",
			doctorGray, doctorReset, doctorGray, doctorReset)
	}

	fmt.Println()
	if allOK {
		fmt.Printf("%s✓ Everything looks good!%s\n\n", doctorGreen, doctorReset)
	} else {
		fmt.Printf("%s⚠ Some issues found. See suggestions above.%s\n\n", doctorYellow, doctorReset)
	}
	return allOK
}

func printMissingHint(m managerInfo, allOK *bool) {
	if !doctorInstallMissing {
		fmt.Printf("       %s↳ %s%s\n\n", doctorGray, m.installHow, doctorReset)
		return
	}
	if m.installArgs == nil {
		fmt.Printf("       %s↳ cannot auto-install: %s%s\n\n", doctorGray, m.installHow, doctorReset)
		return
	}
	fmt.Printf("       %s↳ installing %s…%s\n", doctorGray, m.name, doctorReset)
	if err := runInstall(m); err != nil {
		fmt.Printf("       %s✗ install failed: %v%s\n\n", doctorRed, err, doctorReset)
	} else {
		fmt.Printf("       %s✓ installed successfully%s\n\n", doctorGreen, doctorReset)
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

	_, statErr := os.Stat(binDir)
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
		UpdaterRepo: os.Getenv("UPDATER_REPO"),
		AllOK:       allOK,
	})
	return allOK
}

// runInstall executes the install command for a package manager, streaming output to stdout.
func runInstall(m managerInfo) error {
	cmd := exec.Command(m.installArgs[0], m.installArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// getVersion runs `binary versionArg` and returns the version string.
// If pattern is non-empty, it extracts the first capture group from the output.
// Otherwise, it returns the first non-empty line.
func getVersion(binary, arg, pattern string) string {
	out, err := exec.Command(binary, arg).Output()
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
