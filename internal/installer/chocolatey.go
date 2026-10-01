package installer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Chocolatey installs packages via the Chocolatey package manager (choco).
type Chocolatey struct{ force bool }

func NewChocolatey(force bool) *Chocolatey { return &Chocolatey{force: force} }

func (c *Chocolatey) Name() string { return string(config.SourceChocolatey) }

func (c *Chocolatey) IsAvailable() bool {
	_, err := exec.LookPath("choco")
	return err == nil
}

// spec returns the chocolatey block, or ErrNotOffered when the application
// does not declare one.
func (c *Chocolatey) spec(app config.Application) (*config.ChocolateySpec, error) {
	if app.Package.Chocolatey == nil || app.Package.Chocolatey.ID == "" {
		return nil, fmt.Errorf("%w: no 'package.chocolatey.id' for %q", ErrNotOffered, app.Name)
	}
	return app.Package.Chocolatey, nil
}

// commonArgs builds the flags shared by install and upgrade.
func (c *Chocolatey) commonArgs(app config.Application, spec *config.ChocolateySpec) []string {
	var args []string
	if app.Version != "" && !strings.EqualFold(app.Version, "latest") {
		args = append(args, "--version", app.Version)
	}
	if c.force {
		args = append(args, "--force")
	}
	if spec.PackageParams != "" {
		// Parameters consumed by the chocolatey package script.
		args = append(args, "--package-parameters", spec.PackageParams)
	}
	if spec.InstallArgs != "" {
		// Arguments forwarded to the native installer — a different channel.
		args = append(args, "--install-arguments", spec.InstallArgs)
	}
	if spec.Feed != "" {
		args = append(args, "--source", spec.Feed)
	}
	if spec.AllowDowngrade {
		args = append(args, "--allow-downgrade")
	}
	return args
}

func (c *Chocolatey) Install(ctx context.Context, app config.Application) error {
	spec, err := c.spec(app)
	if err != nil {
		return err
	}

	verb := "install"
	if c.isInstalled(app) {
		if app.SkipUpgrade {
			return ErrAlreadyInstalled
		}
		verb = "upgrade"
	}

	return runChoco(ctx, c.args(verb, app, spec)...)
}

// args is the full choco argv for verb ("install" or "upgrade"). Kept separate
// from Install so the command line can be asserted without running choco.
func (c *Chocolatey) args(verb string, app config.Application, spec *config.ChocolateySpec) []string {
	args := []string{verb, spec.ID, "--yes", "--no-progress"}
	args = append(args, c.commonArgs(app, spec)...)
	// spec.Args go last so a user-supplied flag can override what we chose.
	return append(args, spec.Args...)
}

// uninstallArgs is the full choco argv for a removal.
func (c *Chocolatey) uninstallArgs(spec *config.ChocolateySpec) []string {
	return []string{"uninstall", spec.ID, "--yes"}
}

func (c *Chocolatey) Uninstall(ctx context.Context, app config.Application) error {
	spec, err := c.spec(app)
	if err != nil {
		return err
	}
	if !c.isInstalled(app) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "choco", c.uninstallArgs(spec)...)
}

// checkArgs is the full choco argv for a local lookup.
//
// --exact is required: without it `choco list git` also matches git-lfs, and
// the first matching line decides the reported version. --limit-output gives
// "id|version" lines with no headers, and --local-only must NOT be passed:
// Chocolatey 2.x removed it from the list command and rejects the whole
// invocation, which made every lookup report "not installed".
func chocoCheckArgs(id string) []string {
	return []string{"list", id, "--exact", "--limit-output"}
}

func (c *Chocolatey) Check(app config.Application) (bool, string) {
	spec, err := c.spec(app)
	if err != nil {
		return false, ""
	}
	out, err := exec.Command("choco", chocoCheckArgs(spec.ID)...).Output() //#nosec G204 -- fixed program; the id is a chocolatey package identifier
	if err != nil {
		return false, ""
	}
	return parseChocoList(string(out), spec.ID)
}

// parseChocoList reads the "id|version" lines of `choco list --limit-output`
// and returns the entry matching id. A package that is absent yields no line
// at all, and choco still exits 0.
func parseChocoList(out, id string) (bool, string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, version, _ := strings.Cut(line, "|")
		if strings.EqualFold(strings.TrimSpace(name), id) {
			return true, strings.TrimSpace(version)
		}
	}
	return false, ""
}

func (c *Chocolatey) isInstalled(app config.Application) bool {
	installed, _ := c.Check(app)
	return installed
}

// runChoco runs a choco command with captured output so we can:
//  1. Strip the non-administrator warning block.
//  2. Detect "already installed" / "already up to date" and return ErrAlreadyInstalled.
func runChoco(ctx context.Context, args ...string) error {
	out, err := run(ctx, "choco", args...)

	filtered := filterChocoOutput(out)
	lower := strings.ToLower(filtered)

	// Known limitation: a genuine failure whose log happens to contain this
	// phrase is read as a no-op. Narrowing it to successful exits needs
	// chocolatey's exit code for an already-installed package confirmed on a
	// real machine, which is why it is left as it was.
	if strings.Contains(lower, "already installed") || strings.Contains(lower, "already up to date") {
		return ErrAlreadyInstalled
	}

	// err already carries the tail of the output via CommandError.
	return err
}

// filterChocoOutput strips the non-admin warning block:
//
//	Chocolatey detected you are not running from an elevated command shell
//	...
//	Do you want to continue?([Y]es/[N]o):
func filterChocoOutput(text string) string {
	const start = "Chocolatey detected you are not running from an elevated command shell"
	const end = "Do you want to continue?([Y]es/[N]o):"

	for {
		si := strings.Index(text, start)
		if si == -1 {
			break
		}
		ei := strings.Index(text[si:], end)
		if ei == -1 {
			text = strings.TrimRight(text[:si], "\r\n ")
			break
		}
		cut := si + ei + len(end)
		for cut < len(text) && (text[cut] == '\r' || text[cut] == '\n' || text[cut] == ' ') {
			cut++
		}
		text = text[:si] + text[cut:]
	}

	return strings.TrimSpace(text)
}
