package installer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Scoop installs packages via the Scoop package manager.
type Scoop struct{}

func NewScoop() *Scoop { return &Scoop{} }

func (s *Scoop) Name() string { return SourceScoop }

func (s *Scoop) IsAvailable() bool {
	_, err := exec.LookPath("scoop")
	return err == nil
}

func (s *Scoop) Install(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for scoop package %q", pkg.Name)
	}

	if s.isInstalled(pkg.ID) {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		return runScoop(ctx, append([]string{"update", pkg.ID}, pkg.Args...)...)
	}
	return runScoop(ctx, append([]string{"install", pkg.ID}, pkg.Args...)...)
}

func (s *Scoop) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.ID == "" {
		return fmt.Errorf("missing 'id' for scoop package %q", pkg.Name)
	}
	if !s.isInstalled(pkg.ID) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "scoop", "uninstall", pkg.ID)
}

func (s *Scoop) Check(pkg config.Package) (bool, string) {
	if pkg.ID == "" {
		return false, ""
	}
	out, err := exec.Command("scoop", "list").CombinedOutput()
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], pkg.ID) {
			if len(fields) > 1 {
				return true, fields[1]
			}
			return true, ""
		}
	}
	return false, ""
}

func (s *Scoop) isInstalled(id string) bool { return checkInstalled(s, id) }

// runScoop runs a scoop command with captured output so we can:
//  1. Strip the noisy self-update block ("Updating Scoop..." … "Scoop was updated successfully!")
//  2. Detect "already at latest version" and return ErrAlreadyInstalled.
func runScoop(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "scoop", args...)
	out, err := cmd.CombinedOutput()

	filtered := filterScoopOutput(string(out))

	lower := strings.ToLower(filtered)
	if strings.Contains(lower, "latest version)") ||
		strings.Contains(lower, "latest versions for all apps are installed") {
		return ErrAlreadyInstalled
	}

	return err
}

// filterScoopOutput removes the scoop self-update block that scoop emits before
// every install/update command. The block looks like:
//
//	Updating Scoop...
//	Updating Buckets...
//	MethodInvocationException: ...   (optional permission error)
//	  Line |
//	  …
//	Scoop was updated successfully!
//
// Everything between "Updating Scoop..." and "Scoop was updated successfully!"
// (inclusive) is stripped. Multiple occurrences are handled.
func filterScoopOutput(text string) string {
	const start = "Updating Scoop..."
	const end = "Scoop was updated successfully!"

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
		for cut < len(text) && (text[cut] == '\r' || text[cut] == '\n') {
			cut++
		}
		text = text[:si] + text[cut:]
	}

	return strings.TrimSpace(text)
}
