package installer

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Scoop installs packages via the Scoop package manager.
type Scoop struct{ force bool }

func NewScoop(force bool) *Scoop { return &Scoop{force: force} }

func (s *Scoop) Name() string { return SourceScoop }

func (s *Scoop) IsAvailable() bool {
	_, err := exec.LookPath("scoop")
	return err == nil
}

func (s *Scoop) Install(ctx context.Context, pkg config.Package) error {
	if pkg.Scoop == nil || pkg.Scoop.ID == "" {
		return fmt.Errorf("missing 'scoop.id' for package %q", pkg.Name)
	}

	if pkg.Scoop.Bucket != "" {
		// Ensure the bucket is available; scoop is idempotent for already-added buckets.
		if err := runCtx(ctx, "scoop", "bucket", "add", pkg.Scoop.Bucket); err != nil {
			return fmt.Errorf("adding scoop bucket %q: %w", pkg.Scoop.Bucket, err)
		}
	}

	if s.isInstalled(pkg) {
		if pkg.NoUpgrade {
			return ErrAlreadyInstalled
		}
		if s.force {
			// Scoop has no --force flag; uninstall then reinstall.
			_ = runCtx(ctx, "scoop", "uninstall", pkg.Scoop.ID)
			return runScoop(ctx, append([]string{"install", pkg.Scoop.ID}, pkg.Scoop.Args...)...)
		}
		return runScoop(ctx, append([]string{"update", pkg.Scoop.ID}, pkg.Scoop.Args...)...)
	}
	return runScoop(ctx, append([]string{"install", pkg.Scoop.ID}, pkg.Scoop.Args...)...)
}

func (s *Scoop) Uninstall(ctx context.Context, pkg config.Package) error {
	if pkg.Scoop == nil || pkg.Scoop.ID == "" {
		return fmt.Errorf("missing 'scoop.id' for package %q", pkg.Name)
	}
	if !s.isInstalled(pkg) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "scoop", "uninstall", pkg.Scoop.ID)
}

func (s *Scoop) Check(pkg config.Package) (bool, string) {
	if pkg.Scoop == nil || pkg.Scoop.ID == "" {
		return false, ""
	}
	out, err := exec.Command("scoop", "list").CombinedOutput()
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.EqualFold(fields[0], pkg.Scoop.ID) {
			if len(fields) > 1 {
				return true, fields[1]
			}
			return true, ""
		}
	}
	return false, ""
}

func (s *Scoop) isInstalled(pkg config.Package) bool {
	installed, _ := s.Check(pkg)
	return installed
}

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
