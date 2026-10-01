package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Scoop installs packages via the Scoop package manager.
type Scoop struct{ force bool }

func NewScoop(force bool) *Scoop { return &Scoop{force: force} }

func (s *Scoop) Name() string { return string(config.SourceScoop) }

func (s *Scoop) IsAvailable() bool {
	_, err := exec.LookPath("scoop")
	return err == nil
}

// spec returns the scoop block, or ErrNotOffered when the application does not
// declare one.
func (s *Scoop) spec(app config.Application) (*config.ScoopSpec, error) {
	if app.Package.Scoop == nil || app.Package.Scoop.ID == "" {
		return nil, fmt.Errorf("%w: no 'package.scoop.id' for %q", ErrNotOffered, app.Name)
	}
	return app.Package.Scoop, nil
}

// scoopFlags builds the trailing flags shared by install and update.
func scoopFlags(spec *config.ScoopSpec) []string {
	var args []string
	if spec.Global {
		args = append(args, "--global")
	}
	if spec.Arch != "" {
		args = append(args, "--arch", spec.Arch)
	}
	// spec.Args go last so a user-supplied flag can override what we chose.
	return append(args, spec.Args...)
}

// scoopArgs is the full scoop argv for verb ("install" or "update"). Kept
// separate from Install so the command line can be asserted without running
// scoop.
func scoopArgs(verb string, spec *config.ScoopSpec) []string {
	return append([]string{verb, spec.ID}, scoopFlags(spec)...)
}

// scoopUninstallArgs is the full scoop argv for a removal. Only --global
// carries over: scoop rejects --arch here, and spec.Args are install flags.
func scoopUninstallArgs(spec *config.ScoopSpec) []string {
	args := []string{"uninstall", spec.ID}
	if spec.Global {
		args = append(args, "--global")
	}
	return args
}

func (s *Scoop) Install(ctx context.Context, app config.Application) error {
	spec, err := s.spec(app)
	if err != nil {
		return err
	}

	if spec.Bucket != "" {
		// Ensure the bucket is available; scoop is idempotent for already-added buckets.
		if err := runCtx(ctx, "scoop", "bucket", "add", spec.Bucket); err != nil {
			return fmt.Errorf("adding scoop bucket %q: %w", spec.Bucket, err)
		}
	}

	if s.isInstalled(app) {
		if app.SkipUpgrade {
			return ErrAlreadyInstalled
		}
		if s.force {
			// Scoop has no --force flag; uninstall then reinstall.
			_ = runCtx(ctx, "scoop", scoopUninstallArgs(spec)...)
			return runScoop(ctx, scoopArgs("install", spec)...)
		}
		return runScoop(ctx, scoopArgs("update", spec)...)
	}
	return runScoop(ctx, scoopArgs("install", spec)...)
}

func (s *Scoop) Uninstall(ctx context.Context, app config.Application) error {
	spec, err := s.spec(app)
	if err != nil {
		return err
	}
	if !s.isInstalled(app) {
		return ErrAlreadyInstalled
	}
	return runCtx(ctx, "scoop", scoopUninstallArgs(spec)...)
}

// Check asks scoop what it has, via the JSON of `scoop export`.
//
// The human table from `scoop list` cannot be parsed by splitting on
// whitespace: an app whose install failed has an empty Version and Source, so
// the fields shift left and the Updated timestamp is read as the version.
//
// An app scoop itself marks as failed is reported as not installed, because
// that is what it is — the entry is a record of the attempt, not of a working
// program.
func (s *Scoop) Check(app config.Application) (bool, string) {
	spec, err := s.spec(app)
	if err != nil {
		return false, ""
	}
	out, err := exec.Command("scoop", "export").Output()
	if err != nil {
		return false, ""
	}

	var export struct {
		Apps []struct {
			Name    string `json:"Name"`
			Version string `json:"Version"`
			Info    string `json:"Info"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(out, &export); err != nil {
		return false, ""
	}

	for _, a := range export.Apps {
		if !strings.EqualFold(a.Name, spec.ID) {
			continue
		}
		if strings.Contains(strings.ToLower(a.Info), "failed") {
			return false, ""
		}
		return true, a.Version
	}
	return false, ""
}

func (s *Scoop) isInstalled(app config.Application) bool {
	installed, _ := s.Check(app)
	return installed
}

// runScoop runs a scoop command with captured output so we can:
//  1. Strip the noisy self-update block ("Updating Scoop..." … "Scoop was updated successfully!")
//  2. Detect "already at latest version" and return ErrAlreadyInstalled.
func runScoop(ctx context.Context, args ...string) error {
	out, err := run(ctx, "scoop", args...)

	filtered := filterScoopOutput(out)

	lower := strings.ToLower(filtered)
	if strings.Contains(lower, "latest version)") ||
		strings.Contains(lower, "latest versions for all apps are installed") {
		return ErrAlreadyInstalled
	}

	// err already carries the tail of the output via CommandError.
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
