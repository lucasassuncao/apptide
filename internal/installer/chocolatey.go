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

func (c *Chocolatey) Name() string { return SourceChocolatey }

func (c *Chocolatey) IsAvailable() bool {
	_, err := exec.LookPath("choco")
	return err == nil
}

func (c *Chocolatey) Install(ctx context.Context, pkg config.Package) error {
	if pkg.Chocolatey == nil || pkg.Chocolatey.ID == "" {
		return fmt.Errorf("missing 'chocolatey.id' for package %q", pkg.Name)
	}

	installed := c.isInstalled(pkg)

	if installed {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		args := []string{"upgrade", pkg.Chocolatey.ID, "--yes", "--no-progress"}
		if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
			args = append(args, "--version", pkg.Version)
		}
		if c.force {
			args = append(args, "--force")
		}
		if pkg.Chocolatey.PackageParams != "" {
			args = append(args, "--package-parameters", pkg.Chocolatey.PackageParams)
		}
		return runChoco(ctx, append(args, pkg.Chocolatey.Args...)...)
	}

	args := []string{"install", pkg.Chocolatey.ID, "--yes", "--no-progress"}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	if c.force {
		args = append(args, "--force")
	}
	if pkg.Chocolatey.PackageParams != "" {
		args = append(args, "--package-parameters", pkg.Chocolatey.PackageParams)
	}
	return runChoco(ctx, append(args, pkg.Chocolatey.Args...)...)
}

func (c *Chocolatey) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.Chocolatey == nil || pkg.Chocolatey.ID == "" {
		return fmt.Errorf("missing 'chocolatey.id' for package %q", pkg.Name)
	}
	if !c.isInstalled(pkg) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "choco", "uninstall", pkg.Chocolatey.ID, "--yes")
}

func (c *Chocolatey) Check(pkg config.Package) (bool, string) {
	if pkg.Chocolatey == nil || pkg.Chocolatey.ID == "" {
		return false, ""
	}
	id := pkg.Chocolatey.ID
	out, err := exec.Command("choco", "list", "--local-only", id).CombinedOutput()
	if err != nil {
		return false, ""
	}
	idLower := strings.ToLower(id)
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(strings.ToLower(line), idLower) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return true, fields[1]
		}
		return true, ""
	}
	return false, ""
}

func (c *Chocolatey) isInstalled(pkg config.Package) bool {
	installed, _ := c.Check(pkg)
	return installed
}

// runChoco runs a choco command with captured output so we can:
//  1. Strip the non-administrator warning block.
//  2. Detect "already installed" / "already up to date" and return ErrAlreadyInstalled.
func runChoco(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "choco", args...)
	out, err := cmd.CombinedOutput()

	filtered := filterChocoOutput(string(out))
	lower := strings.ToLower(filtered)

	if strings.Contains(lower, "already installed") || strings.Contains(lower, "already up to date") {
		return ErrAlreadyInstalled
	}

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
