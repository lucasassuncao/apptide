package installer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Chocolatey installs packages via the Chocolatey package manager (choco).
type Chocolatey struct{}

func NewChocolatey() *Chocolatey { return &Chocolatey{} }

func (c *Chocolatey) Name() string { return "chocolatey" }

func (c *Chocolatey) IsAvailable() bool {
	_, err := exec.LookPath("choco")
	return err == nil
}

func (c *Chocolatey) Install(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for chocolatey package %q", pkg.Name)
	}

	installed := c.isInstalled(pkg.ID)

	if installed {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		args := []string{"upgrade", pkg.ID, "--yes", "--no-progress"}
		if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
			args = append(args, "--version", pkg.Version)
		}
		return runChoco(ctx, append(args, pkg.Args...)...)
	}

	args := []string{"install", pkg.ID, "--yes", "--no-progress"}
	if pkg.Version != "" && !strings.EqualFold(pkg.Version, "latest") {
		args = append(args, "--version", pkg.Version)
	}
	return runChoco(ctx, append(args, pkg.Args...)...)
}

func (c *Chocolatey) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for chocolatey package %q", pkg.Name)
	}
	return runCtx(ctx, "choco", "uninstall", pkg.ID, "--yes")
}

func (c *Chocolatey) Check(pkg config.Package) (bool, string) {
	if pkg.ID == "" {
		return false, ""
	}
	out, err := exec.Command("choco", "list", "--local-only", pkg.ID).CombinedOutput()
	if err != nil {
		return false, ""
	}
	idLower := strings.ToLower(pkg.ID)
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

func (c *Chocolatey) isInstalled(id string) bool {
	installed, _ := c.Check(config.Package{ID: id})
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
