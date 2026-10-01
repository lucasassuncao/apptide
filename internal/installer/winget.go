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

func (w *Winget) Name() string { return string(config.SourceWinget) }

func (w *Winget) IsAvailable() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

// spec returns the winget block, or ErrNotOffered when the application does
// not declare one — which lets the caller fall back to the next source.
func (w *Winget) spec(app config.Application) (*config.WingetSpec, error) {
	if app.Package.Winget == nil || app.Package.Winget.ID == "" {
		return nil, fmt.Errorf("%w: no 'package.winget.id' for %q", ErrNotOffered, app.Name)
	}
	return app.Package.Winget, nil
}

func (w *Winget) Install(ctx context.Context, app config.Application) error {
	if _, err := w.spec(app); err != nil {
		return err
	}
	if w.isInstalled(app) {
		if app.SkipUpgrade {
			return ErrAlreadyInstalled
		}
		return w.upgrade(ctx, app)
	}
	return w.install(ctx, app)
}

func (w *Winget) Uninstall(ctx context.Context, app config.Application) error {
	spec, err := w.spec(app)
	if err != nil {
		return err
	}
	if !w.isInstalled(app) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "winget", w.uninstallArgs(spec)...)
}

func (w *Winget) Check(app config.Application) (bool, string) {
	spec, err := w.spec(app)
	if err != nil {
		return false, ""
	}
	out, err := exec.Command("winget", "list", "--id", spec.ID, "--exact").CombinedOutput() //#nosec G204 -- fixed program; the id is a winget package identifier
	if err != nil {
		return false, ""
	}
	idLower := strings.ToLower(spec.ID)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), idLower) {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if strings.EqualFold(f, spec.ID) && i+1 < len(fields) {
				return true, fields[i+1]
			}
		}
		return true, ""
	}
	return false, ""
}

func (w *Winget) isInstalled(app config.Application) bool {
	installed, _ := w.Check(app)
	return installed
}

// commonArgs are the flags shared by install and upgrade.
func (w *Winget) commonArgs(app config.Application, spec *config.WingetSpec) []string {
	var args []string
	if app.Version != "" && !strings.EqualFold(app.Version, "latest") {
		args = append(args, "--version", app.Version)
	}
	if spec.Scope != "" {
		args = append(args, "--scope", spec.Scope)
	}
	if spec.Locale != "" {
		args = append(args, "--locale", spec.Locale)
	}
	if spec.Feed != "" {
		args = append(args, "--source", spec.Feed)
	}
	if spec.Override != "" {
		args = append(args, "--override", spec.Override)
	}
	return args
}

// installArgs is the full winget argv for an install. Kept separate from
// install so the command line can be asserted without running winget.
func (w *Winget) installArgs(app config.Application, spec *config.WingetSpec) []string {
	args := []string{
		"install", "--id", spec.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	args = append(args, w.commonArgs(app, spec)...)
	if app.SkipUpgrade {
		args = append(args, "--no-upgrade")
	}
	if w.force {
		args = append(args, "--force")
	}
	// spec.Args go last so a user-supplied flag can override what we chose.
	return append(args, spec.Args...)
}

// upgradeArgs is the full winget argv for an upgrade.
func (w *Winget) upgradeArgs(app config.Application, spec *config.WingetSpec) []string {
	args := []string{
		"upgrade", "--id", spec.ID, "--exact",
		"--silent", "--accept-source-agreements", "--accept-package-agreements",
	}
	args = append(args, w.commonArgs(app, spec)...)
	return append(args, spec.Args...)
}

func (w *Winget) install(ctx context.Context, app config.Application) error {
	return runCtx(ctx, "winget", w.installArgs(app, app.Package.Winget)...)
}

func (w *Winget) upgrade(ctx context.Context, app config.Application) error {
	// With --force, use install --force instead of upgrade so winget reinstalls
	// even when already at the latest version.
	if w.force {
		return w.install(ctx, app)
	}
	return runWingetUpgrade(ctx, w.upgradeArgs(app, app.Package.Winget)...)
}

// uninstallArgs is the full winget argv for a removal.
func (w *Winget) uninstallArgs(spec *config.WingetSpec) []string {
	return []string{
		"uninstall", "--id", spec.ID, "--exact",
		"--silent", "--accept-source-agreements",
	}
}

// wingetUpToDateCodes are winget exit codes that mean "already at latest version".
//
//	0x8a15002b  APPINSTALLER_ERROR_UPDATE_NOT_AVAILABLE: no applicable upgrade found
//	0x8a150077  APPINSTALLER_ERROR_UPDATE_NOT_AVAILABLE: source-level variant
//
// Keyed by int, not uint32: ExitCode returns an int, and converting a negative
// one wraps into a value that could collide with a code in this table.
var wingetUpToDateCodes = map[int]bool{
	0x8a15002b: true,
	0x8a150077: true,
}

func runWingetUpgrade(ctx context.Context, args ...string) error {
	err := runCtx(ctx, "winget", args...)
	if err == nil {
		return nil
	}

	// The exit code is authoritative here, so this one needs no output
	// matching: winget says "no upgrade available" with a code of its own.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && wingetUpToDateCodes[exitErr.ExitCode()] {
		return ErrAlreadyInstalled
	}

	return err
}
