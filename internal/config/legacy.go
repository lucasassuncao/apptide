package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// This file converts the v1 config layout — a top-level map of category name
// to package list — into v2 applications. Reading v1 keeps existing configs
// working after the schema change; apptide never writes it back.

// legacyPackage mirrors the v1 Package struct.
type legacyPackage struct {
	Name        string `yaml:"name"`
	Source      string `yaml:"source"`
	Action      string `yaml:"action"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
	NoUpgrade   bool   `yaml:"no_upgrade"`
	InfoURL     string `yaml:"info_url"`
	PreInstall  string `yaml:"pre_install"`
	PostInstall string `yaml:"post_install"`

	// ID is the pre-0135a20 flat identifier, kept so that configs written
	// before the source-specific blocks still resolve instead of silently
	// installing nothing.
	ID string `yaml:"id"`

	Winget     *legacyWinget     `yaml:"winget"`
	Chocolatey *legacyChocolatey `yaml:"chocolatey"`
	Scoop      *legacyScoop      `yaml:"scoop"`
	GitHub     *legacyGitHub     `yaml:"github"`
}

type legacyWinget struct {
	ID     string   `yaml:"id"`
	Args   []string `yaml:"args"`
	Scope  string   `yaml:"scope"`
	Locale string   `yaml:"locale"`
}

type legacyChocolatey struct {
	ID            string   `yaml:"id"`
	Args          []string `yaml:"args"`
	PackageParams string   `yaml:"package_params"`
}

type legacyScoop struct {
	ID     string   `yaml:"id"`
	Args   []string `yaml:"args"`
	Bucket string   `yaml:"bucket"`
}

type legacyGitHub struct {
	Repo         string   `yaml:"repo"`
	AssetPattern string   `yaml:"asset_pattern"`
	RunInstaller bool     `yaml:"run_installer"`
	InstallDir   string   `yaml:"install_dir"`
	Args         []string `yaml:"args"`
	BinaryName   string   `yaml:"binary_name"`
}

// convertLegacy turns alternating key/value nodes (category name, package
// sequence) into applications, preserving the order the categories appear in.
func convertLegacy(nodes []*yaml.Node) ([]Application, error) {
	var apps []Application

	for i := 0; i+1 < len(nodes); i += 2 {
		category := nodes[i].Value

		var pkgs []legacyPackage
		if err := nodes[i+1].Decode(&pkgs); err != nil {
			return nil, fmt.Errorf("decoding legacy category %q: %w", category, err)
		}
		for _, p := range pkgs {
			apps = append(apps, p.toApplication(category))
		}
	}
	return apps, nil
}

func (p legacyPackage) toApplication(category string) Application {
	src := Source(p.Source).Normalize()

	app := Application{
		Name:        p.Name,
		Description: p.Description,
		Category:    category,
		InfoURL:     p.InfoURL,
		Action:      Action(p.Action),
		Version:     p.Version,
		SkipUpgrade: p.NoUpgrade,
		Source:      Sources{src},
	}

	if p.PreInstall != "" || p.PostInstall != "" {
		app.Hooks = &Hooks{PreInstall: p.PreInstall, PostInstall: p.PostInstall}
	}

	if p.Winget != nil {
		app.Package.Winget = &WingetSpec{
			ID: p.Winget.ID, Args: p.Winget.Args,
			Scope: p.Winget.Scope, Locale: p.Winget.Locale,
		}
	}
	if p.Chocolatey != nil {
		app.Package.Chocolatey = &ChocolateySpec{
			ID: p.Chocolatey.ID, Args: p.Chocolatey.Args,
			PackageParams: p.Chocolatey.PackageParams,
		}
	}
	if p.Scoop != nil {
		app.Package.Scoop = &ScoopSpec{
			ID: p.Scoop.ID, Args: p.Scoop.Args, Bucket: p.Scoop.Bucket,
		}
	}
	if p.GitHub != nil {
		app.Package.GitHub = &GitHubSpec{
			ID: p.GitHub.Repo, Args: p.GitHub.Args,
			AssetPattern: p.GitHub.AssetPattern,
			RunInstaller: p.GitHub.RunInstaller,
			InstallDir:   p.GitHub.InstallDir,
			BinaryName:   p.GitHub.BinaryName,
		}
	}

	// Flat `id:` with no block at all — adopt it for the declared source.
	if p.ID != "" {
		if _, ok := app.Package.ID(src); !ok {
			switch src {
			case SourceWinget:
				app.Package.Winget = &WingetSpec{ID: p.ID}
			case SourceChocolatey:
				app.Package.Chocolatey = &ChocolateySpec{ID: p.ID}
			case SourceScoop:
				app.Package.Scoop = &ScoopSpec{ID: p.ID}
			case SourceGitHub:
				app.Package.GitHub = &GitHubSpec{ID: p.ID}
			}
		}
	}

	return app
}
