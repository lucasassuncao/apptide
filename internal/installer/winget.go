package installer

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Winget installs packages via the Windows Package Manager (winget).
type Winget struct{}

func NewWinget() *Winget { return &Winget{} }

func (w *Winget) Name() string { return "winget" }

func (w *Winget) IsAvailable() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

func (w *Winget) Install(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for winget package %q", pkg.Name)
	}

	if w.isInstalled(pkg.ID) {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		return w.upgrade(ctx, pkg)
	}
	return w.install(ctx, pkg)
}

func (w *Winget) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for winget package %q", pkg.Name)
	}
	return runCtx(ctx, "winget", "uninstall",
		"--id", pkg.ID, "--exact",
		"--silent", "--accept-source-agreements",
	)
}

func (w *Winget) Check(pkg config.Package) (bool, string) {
	if pkg.ID == "" {
		return false, ""
	}
	out, err := exec.Command("winget", "list", "--id", pkg.ID, "--exact").CombinedOutput()
	if err != nil {
		return false, ""
	}
	idLower := strings.ToLower(pkg.ID)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), idLower) {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if strings.EqualFold(f, pkg.ID) && i+1 < len(fields) {
				return true, fields[i+1]
			}
		}
		return true, ""
	}
	return false, ""
}

func (w *Winget) isInstalled(id string) bool {
	installed, _ := w.Check(config.Package{ID: id})
	return installed
}

func (w *Winget) install(ctx context.Context, pkg config.Package) error {
	args := []string{
		"install", "--id", pkg.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	if pkg.NoUpgrade {
		args = append(args, "--no-upgrade")
	}
	return runCtx(ctx, "winget", append(args, pkg.Args...)...)
}

func (w *Winget) upgrade(ctx context.Context, pkg config.Package) error {
	args := []string{
		"upgrade", "--id", pkg.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	return runWingetUpgrade(ctx, append(args, pkg.Args...)...)
}

// wingetUpToDateCodes are winget exit codes that mean "already at latest version".
//
//	0x8a15002b  APPINSTALLER_ERROR_UPDATE_NOT_AVAILABLE — no applicable upgrade found
//	0x8a150077  APPINSTALLER_ERROR_UPDATE_NOT_AVAILABLE — source-level variant
var wingetUpToDateCodes = map[uint32]bool{
	0x8a15002b: true,
	0x8a150077: true,
}

func runWingetUpgrade(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "winget", args...)
	_, err := cmd.CombinedOutput()

	if err == nil {
		return nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if wingetUpToDateCodes[uint32(exitErr.ExitCode())] {
			return ErrAlreadyInstalled
		}
	}

	return err
}
