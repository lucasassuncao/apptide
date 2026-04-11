package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// WingetConfig holds fields specific to the winget source.
type WingetConfig struct {
	ID     string   `yaml:"id"`     // package identifier (e.g. "Publisher.App")
	Args   []string `yaml:"args"`   // extra CLI arguments passed to winget
	Scope  string   `yaml:"scope"`  // installation scope: "machine" or "user" (--scope)
	Locale string   `yaml:"locale"` // installer locale (e.g. "pt-BR") (--locale)
}

// ChocolateyConfig holds fields specific to the chocolatey source.
type ChocolateyConfig struct {
	ID            string   `yaml:"id"`             // package identifier (e.g. "googlechrome")
	Args          []string `yaml:"args"`           // extra CLI arguments passed to choco
	PackageParams string   `yaml:"package_params"` // parameters forwarded to the package script (--package-parameters)
}

// ScoopConfig holds fields specific to the scoop source.
type ScoopConfig struct {
	ID     string   `yaml:"id"`     // package identifier (e.g. "vim")
	Args   []string `yaml:"args"`   // extra CLI arguments passed to scoop
	Bucket string   `yaml:"bucket"` // scoop bucket that provides the package (e.g. "extras")
}

// GitHubConfig holds fields specific to the github source.
type GitHubConfig struct {
	Repo         string   `yaml:"repo"`          // "owner/repo"
	AssetPattern string   `yaml:"asset_pattern"` // glob to match a specific release asset (e.g. "*windows_amd64*.zip")
	RunInstaller bool     `yaml:"run_installer"` // run the .exe/.msi instead of copying the binary
	InstallDir   string   `yaml:"install_dir"`   // override the default binary destination directory
	Args         []string `yaml:"args"`          // extra CLI arguments passed to the installer (when run_installer: true)
	BinaryName   string   `yaml:"binary_name"`   // explicit binary name to use instead of lowercased package name (e.g. "gh" for "GitHub CLI")
}

// Package represents a single software entry in the config file.
type Package struct {
	// Common fields
	Name        string `yaml:"name"`        // display name (required)
	Source      string `yaml:"source"`      // winget | chocolatey | scoop | github (required)
	Action      string `yaml:"action"`      // install | uninstall | skip  (default: install)
	Version     string `yaml:"version"`     // specific version or "latest" (default: latest)
	Description string `yaml:"description"` // informational only
	NoUpgrade   bool   `yaml:"no_upgrade"`  // skip upgrade when already installed
	InfoURL     string `yaml:"info_url"`    // project homepage / docs link

	// PreInstall is an optional shell command executed before install/uninstall.
	// Runs via: cmd /C <pre_install>
	// If it exits with a non-zero code the package action is aborted.
	PreInstall string `yaml:"pre_install"`

	// PostInstall is an optional shell command executed after a successful install/uninstall.
	// Runs via: cmd /C <post_install>
	// A non-zero exit code is reported as a warning but does not mark the package as failed.
	PostInstall string `yaml:"post_install"`

	// Source-specific blocks — at most one will be set per package.
	Winget     *WingetConfig     `yaml:"winget"`
	Chocolatey *ChocolateyConfig `yaml:"chocolatey"`
	Scoop      *ScoopConfig      `yaml:"scoop"`
	GitHub     *GitHubConfig     `yaml:"github"`
}

// Config maps category names to their package lists.
type Config map[string][]Package

// LoadWithImports reads a YAML config file and recursively resolves any `import:` entries,
// merging all packages into a single Config. Import paths are relative to the file that
// declares them. Circular imports are detected and reported as errors.
func LoadWithImports(path string) (Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", path, err)
	}
	visited := make(map[string]bool)
	return loadRecursive(absPath, visited)
}

func loadRecursive(absPath string, visited map[string]bool) (Config, error) {
	if visited[absPath] {
		return nil, fmt.Errorf("circular import detected: %q", absPath)
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", absPath, err)
	}

	imports, merged, err := parseFile(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", absPath, err)
	}

	// Resolve and merge each imported file.
	baseDir := filepath.Dir(absPath)
	for _, imp := range imports {
		impAbs, err := filepath.Abs(filepath.Join(baseDir, imp))
		if err != nil {
			return nil, fmt.Errorf("resolving import %q in %q: %w", imp, absPath, err)
		}
		child, err := loadRecursive(impAbs, visited)
		if err != nil {
			return nil, fmt.Errorf("importing %q: %w", imp, err)
		}
		for cat, pkgs := range child {
			merged[cat] = append(merged[cat], pkgs...)
		}
	}

	return merged, nil
}

// parseFile decodes a YAML config file using yaml.Node so that the top-level `import:`
// key ([]string) and category keys ([]Package) can coexist without type conflicts.
// The yaml:",inline" approach fails because yaml.v3 feeds all keys — including `import:` —
// into the inline map and then tries to unmarshal file-path strings as Package structs.
func parseFile(data []byte) (imports []string, cfg Config, err error) {
	cfg = make(Config)

	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return imports, cfg, err
	}
	// Empty file.
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return imports, cfg, nil
	}

	root := doc.Content[0] // document root is always a MappingNode
	if root.Kind != yaml.MappingNode {
		return imports, cfg, fmt.Errorf("expected a YAML mapping at the top level, got %v", root.Kind)
	}

	// MappingNode children alternate: key, value, key, value, ...
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]

		if key == "import" {
			if decErr := val.Decode(&imports); decErr != nil {
				return imports, cfg, fmt.Errorf("decoding import list: %w", decErr)
			}
		} else {
			var pkgs []Package
			if decErr := val.Decode(&pkgs); decErr != nil {
				return imports, cfg, fmt.Errorf("decoding category %q: %w", key, decErr)
			}
			cfg[key] = pkgs
		}
	}
	return imports, cfg, nil
}

// Categories returns the sorted list of category names.
func (c Config) Categories() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
