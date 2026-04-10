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
type Winget struct{ force bool }

func NewWinget(force bool) *Winget { return &Winget{force: force} }

func (w *Winget) Name() string { return SourceWinget }

func (w *Winget) IsAvailable() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

func (w *Winget) Install(ctx context.Context, pkg config.Package) error {
	if pkg.Winget == nil || pkg.Winget.ID == "" {
		return fmt.Errorf("missing 'winget.id' for package %q", pkg.Name)
	}

	if w.isInstalled(pkg) {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		return w.upgrade(ctx, pkg)
	}
	return w.install(ctx, pkg)
}

func (w *Winget) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.Winget == nil || pkg.Winget.ID == "" {
		return fmt.Errorf("missing 'winget.id' for package %q", pkg.Name)
	}
	if !w.isInstalled(pkg) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "winget", "uninstall",
		"--id", pkg.Winget.ID, "--exact",
		"--silent", "--accept-source-agreements",
	)
}

func (w *Winget) Check(pkg config.Package) (bool, string) {
	if pkg.Winget == nil || pkg.Winget.ID == "" {
		return false, ""
	}
	id := pkg.Winget.ID
	out, err := exec.Command("winget", "list", "--id", id, "--exact").CombinedOutput()
	if err != nil {
		return false, ""
	}
	idLower := strings.ToLower(id)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), idLower) {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if strings.EqualFold(f, id) && i+1 < len(fields) {
				return true, fields[i+1]
			}
		}
		return true, ""
	}
	return false, ""
}

func (w *Winget) isInstalled(pkg config.Package) bool {
	installed, _ := w.Check(pkg)
	return installed
}

func (w *Winget) install(ctx context.Context, pkg config.Package) error {
	args := []string{
		"install", "--id", pkg.Winget.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	if pkg.NoUpgrade {
		args = append(args, "--no-upgrade")
	}
	if w.force {
		args = append(args, "--force")
	}
	if pkg.Winget.Scope != "" {
		args = append(args, "--scope", pkg.Winget.Scope)
	}
	if pkg.Winget.Locale != "" {
		args = append(args, "--locale", pkg.Winget.Locale)
	}
	return runCtx(ctx, "winget", append(args, pkg.Winget.Args...)...)
}

func (w *Winget) upgrade(ctx context.Context, pkg config.Package) error {
	// With --force, use install --force instead of upgrade so winget reinstalls
	// even when already at the latest version.
	if w.force {
		return w.install(ctx, pkg)
	}
	args := []string{
		"upgrade", "--id", pkg.Winget.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	if pkg.Winget.Scope != "" {
		args = append(args, "--scope", pkg.Winget.Scope)
	}
	if pkg.Winget.Locale != "" {
		args = append(args, "--locale", pkg.Winget.Locale)
	}
	return runWingetUpgrade(ctx, append(args, pkg.Winget.Args...)...)
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
