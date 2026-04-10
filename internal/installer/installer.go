package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lucasassuncao/apptide/internal/config"
)

// ErrAlreadyInstalled is returned when a package is already present and no_upgrade is set.
var ErrAlreadyInstalled = errors.New("already installed")

// Source identifiers for all supported package sources.
const (
	SourceWinget     = "winget"
	SourceChocolatey = "chocolatey"
	SourceScoop      = "scoop"
	SourceGitHub     = "github"
	SourceThirdParty = "third_party"
)

// Installer handles install/uninstall for a specific package source.
type Installer interface {
	// Name returns the source identifier (e.g. "winget").
	Name() string
	// IsAvailable reports whether the underlying package manager is reachable.
	IsAvailable() bool
	// Install installs or upgrades the package.
	Install(ctx context.Context, pkg config.Package) error
	// Uninstall removes the package.
	Uninstall(ctx context.Context, pkg config.Package) error
	// Check reports whether the package is currently installed and its version (if detectable).
	Check(pkg config.Package) (installed bool, version string)
}

// Options configures source-specific settings passed to Resolve.
type Options struct {
	GitHubToken       string
	DefaultInstallDir string
	Force             bool
}

// Resolve returns the correct Installer for the given source name.
func Resolve(source string, opts Options) (Installer, error) {
	dir := opts.DefaultInstallDir
	if dir == "" {
		dir = defaultInstallDir()
	}

	switch source {
	case SourceWinget:
		return NewWinget(opts.Force), nil
	case SourceChocolatey, "choco":
		return NewChocolatey(opts.Force), nil
	case SourceScoop:
		return NewScoop(opts.Force), nil
	case SourceGitHub:
		return NewGitHub(opts.GitHubToken, dir), nil
	case SourceThirdParty:
		return NewThirdParty(dir), nil
	default:
		return nil, fmt.Errorf("unknown source %q — valid: winget, chocolatey, scoop, github, third_party", source)
	}
}

// DefaultInstallDir returns the default binary directory used when no install_dir is specified.
func DefaultInstallDir() string { return defaultInstallDir() }

func defaultInstallDir() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(local, "apptide", "bin")
}
