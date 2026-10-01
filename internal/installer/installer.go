package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lucasassuncao/apptide/internal/config"
)

// ErrAlreadyInstalled is returned when a package is already present and no
// action is needed.
var ErrAlreadyInstalled = errors.New("already installed")

// ErrNotOffered is returned when a source does not offer the application at
// all — a missing package id, or a repository/package the manager does not
// know.
//
// It is recorded distinctly from an install that was attempted and failed
// (see state.ResultNotFound against state.ResultFailed) so the attempt chain
// says which of the two happened. Both advance to the next source: listing
// several sources declares that any of them is acceptable. See RunInstall.
var ErrNotOffered = errors.New("not offered by this source")

// Installer handles install/uninstall for a specific package source.
type Installer interface {
	// Name returns the source identifier (e.g. "winget").
	Name() string
	// IsAvailable reports whether the underlying package manager is reachable.
	IsAvailable() bool
	// Install installs or upgrades the application.
	Install(ctx context.Context, app config.Application) error
	// Uninstall removes the application.
	Uninstall(ctx context.Context, app config.Application) error
	// Check reports whether the application is currently installed and its
	// version (if detectable).
	Check(app config.Application) (installed bool, version string)
}

// Options configures source-specific settings passed to Resolve.
type Options struct {
	GitHubToken       string
	DefaultInstallDir string
	Force             bool
}

// Resolve returns the correct Installer for the given source.
func Resolve(source config.Source, opts Options) (Installer, error) {
	dir := opts.DefaultInstallDir
	if dir == "" {
		dir = defaultInstallDir()
	}

	switch source.Normalize() {
	case config.SourceWinget:
		return NewWinget(opts.Force), nil
	case config.SourceChocolatey:
		return NewChocolatey(opts.Force), nil
	case config.SourceScoop:
		return NewScoop(opts.Force), nil
	case config.SourceGitHub:
		return NewGitHub(opts.GitHubToken, dir), nil
	default:
		return nil, fmt.Errorf("unknown source %q — valid: winget, chocolatey, scoop, github", source)
	}
}

// DefaultInstallDir returns the default binary directory used when no
// install_dir is specified.
func DefaultInstallDir() string { return defaultInstallDir() }

func defaultInstallDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(local, "apptide", "bin")
}
