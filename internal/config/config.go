// Package config defines the apptide configuration schema and loads it from
// YAML, resolving `import:` chains and per-file defaults.
//
// A config file declares applications, not packages: one application may be
// obtainable from several package managers, listed in preference order. The
// order applies to the first installation only — once installed, an
// application is bound to the source that succeeded (see internal/state) and
// upgrades and removals always go through that one.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the config format this build writes and understands.
const SchemaVersion = 2

// ── Named types ──────────────────────────────────────────────────────────────

// Source identifies a package manager.
type Source string

const (
	SourceWinget     Source = "winget"
	SourceChocolatey Source = "chocolatey"
	SourceScoop      Source = "scoop"
	SourceGitHub     Source = "github"
)

// AllSources lists every supported source in a stable order.
func AllSources() []Source {
	return []Source{SourceWinget, SourceChocolatey, SourceScoop, SourceGitHub}
}

// Normalize maps aliases to the canonical source name.
func (s Source) Normalize() Source {
	switch strings.ToLower(string(s)) {
	case "choco", string(SourceChocolatey):
		return SourceChocolatey
	default:
		return Source(strings.ToLower(string(s)))
	}
}

// Valid reports whether s is a supported source.
func (s Source) Valid() bool {
	switch s.Normalize() {
	case SourceWinget, SourceChocolatey, SourceScoop, SourceGitHub:
		return true
	}
	return false
}

// Action is what apptide should do with an application.
type Action string

const (
	ActionInstall   Action = "install"
	ActionUninstall Action = "uninstall"
	ActionSkip      Action = "skip"
)

// Valid reports whether a is a supported action.
func (a Action) Valid() bool {
	switch Action(strings.ToLower(string(a))) {
	case ActionInstall, ActionUninstall, ActionSkip:
		return true
	}
	return false
}

// ── Sources: scalar or list ──────────────────────────────────────────────────

// Sources is the ordered list of sources that may install an application.
// Both YAML forms are accepted:
//
//	source: winget
//	source: [winget, scoop, github]
type Sources []Source

// UnmarshalYAML accepts a sequence, a single scalar, or a comma-separated
// scalar.
//
// The last form exists because `source: winget, scoop` is the obvious way to
// write a preference list, and YAML reads it as the single string
// "winget, scoop" — which would otherwise only fail at validation, after the
// user had already written it that way everywhere.
func (s *Sources) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		var out Sources
		for _, part := range strings.Split(n.Value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, Source(part).Normalize())
			}
		}
		*s = out
		return nil
	}
	var list []Source
	if err := n.Decode(&list); err != nil {
		return fmt.Errorf("decoding source list: %w", err)
	}
	for i := range list {
		list[i] = list[i].Normalize()
	}
	*s = list
	return nil
}

// MarshalYAML collapses a single source back to scalar form, so a file
// round-tripped through the editor keeps the shape the user wrote.
func (s Sources) MarshalYAML() (any, error) {
	if len(s) == 1 {
		return string(s[0]), nil
	}
	return []Source(s), nil
}

// First returns the preferred source for a fresh installation.
func (s Sources) First() (Source, bool) {
	if len(s) == 0 {
		return "", false
	}
	return s[0], true
}

// Contains reports whether src is an accepted source.
func (s Sources) Contains(src Source) bool {
	want := src.Normalize()
	for _, x := range s {
		if x.Normalize() == want {
			return true
		}
	}
	return false
}

func (s Sources) String() string {
	parts := make([]string, len(s))
	for i, x := range s {
		parts[i] = string(x)
	}
	return strings.Join(parts, ", ")
}

// ── Document ─────────────────────────────────────────────────────────────────

// Config is a loaded configuration. The top-level `import:` key is not a field:
// the loader resolves it before decoding, and the editor lists it in
// PassthroughKeys so it survives a save without being shown.
type Config struct {
	SchemaVersion int           `yaml:"schema_version"`
	Settings      *Settings     `yaml:"settings,omitempty"`
	Defaults      *Defaults     `yaml:"defaults,omitempty"`
	Applications  []Application `yaml:"applications"`
}

// Settings holds the run-wide options that previously existed only as command
// line flags, which meant repeating them on every invocation and left the
// editor and the browser with nothing to edit but applications.
//
// Only the settings of the file given on the command line are used; an
// imported file describes applications, not how the run is configured.
// A flag always wins over the file.
type Settings struct {
	// InstallDir is where github binaries land. Environment variables are
	// expanded, so a config can be shared between machines.
	InstallDir string `yaml:"install_dir,omitempty"`
	// AddToPath adds InstallDir to the user PATH after installing a binary.
	AddToPath bool `yaml:"add_to_path,omitempty"`
	// GitHubToken raises the API rate limit. Write it as ${GITHUB_TOKEN} and
	// keep the value out of the file.
	GitHubToken string `yaml:"github_token,omitempty"`
}

// Defaults apply to every application in the same file that omits the field.
// They are resolved at load time, so consumers always see final values.
type Defaults struct {
	Category string   `yaml:"category,omitempty"`
	Source   Sources  `yaml:"source,omitempty"`
	Action   Action   `yaml:"action,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
}

// Application is one piece of software plus the coordinates each package
// manager knows it by.
type Application struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Category    string `yaml:"category,omitempty"`
	InfoURL     string `yaml:"info_url,omitempty"`

	Action Action   `yaml:"action,omitempty"`
	Tags   []string `yaml:"tags,omitempty"`

	// Version is honoured by winget and chocolatey (--version) and by github
	// (it becomes the release tag). Scoop has no per-version install, so a
	// pinned version together with scoop in Source is a validation warning.
	Version string `yaml:"version,omitempty"`

	// SkipUpgrade installs when missing but never upgrades afterwards. It has
	// no effect when github is the only source: that source has no upgrade path.
	SkipUpgrade bool `yaml:"skip_upgrade,omitempty"`

	// Optional keeps a failure from failing the run. A config of sixty
	// applications should not exit non-zero — breaking CI — because of one
	// package already known to be flaky.
	Optional bool `yaml:"optional,omitempty"`

	// RequiresAdmin states that this application needs elevation. apptide
	// otherwise guesses from the source and its options, and a guess is wrong
	// in both directions: a portable chocolatey package needs nothing, and a
	// winget installer can demand elevation without saying so.
	RequiresAdmin bool `yaml:"requires_admin,omitempty"`

	// Source lists acceptable sources in preference order. Later entries are
	// fallbacks used only when an earlier one does not offer the package.
	Source Sources `yaml:"source"`

	Hooks   *Hooks   `yaml:"hooks,omitempty"`
	Package Packages `yaml:"package"`
}

// Hooks are shell commands run around the action, via cmd /C.
//
// Install and uninstall have their own pair: a command named for installing
// must not run when the application is being removed, and someone who removes
// software regularly needs somewhere to clean up after it.
type Hooks struct {
	// PreInstall runs before an install or upgrade; a non-zero exit aborts it.
	PreInstall string `yaml:"pre_install,omitempty"`
	// PostInstall runs after a successful install; a non-zero exit is a warning.
	PostInstall string `yaml:"post_install,omitempty"`
	// PreUninstall runs before a removal; a non-zero exit aborts it.
	PreUninstall string `yaml:"pre_uninstall,omitempty"`
	// PostUninstall runs after a successful removal; a non-zero exit is a warning.
	PostUninstall string `yaml:"post_uninstall,omitempty"`
}

// Pre returns the command to run before action, or "" when there is none.
func (a Application) Pre(action Action) string {
	if a.Hooks == nil {
		return ""
	}
	if action == ActionUninstall {
		return a.Hooks.PreUninstall
	}
	return a.Hooks.PreInstall
}

// Post returns the command to run after action succeeds, or "" when there is none.
func (a Application) Post(action Action) string {
	if a.Hooks == nil {
		return ""
	}
	if action == ActionUninstall {
		return a.Hooks.PostUninstall
	}
	return a.Hooks.PostInstall
}

// EffectiveAction returns the action to perform, defaulting to install.
func (a Application) EffectiveAction() Action {
	if a.Action == "" {
		return ActionInstall
	}
	return Action(strings.ToLower(string(a.Action)))
}

// ── Per-source blocks ────────────────────────────────────────────────────────

// Packages holds the identifier and options each manager needs. Every source
// listed in Application.Source must have its block filled in; a block present
// for a source that is not listed is dead config and reported by validate.
type Packages struct {
	Winget     *WingetSpec     `yaml:"winget,omitempty"`
	Chocolatey *ChocolateySpec `yaml:"chocolatey,omitempty"`
	Scoop      *ScoopSpec      `yaml:"scoop,omitempty"`
	GitHub     *GitHubSpec     `yaml:"github,omitempty"`
}

// ID returns the identifier src knows this application by, and whether src is
// configured at all.
func (p Packages) ID(src Source) (string, bool) {
	switch src.Normalize() {
	case SourceWinget:
		if p.Winget != nil {
			return p.Winget.ID, true
		}
	case SourceChocolatey:
		if p.Chocolatey != nil {
			return p.Chocolatey.ID, true
		}
	case SourceScoop:
		if p.Scoop != nil {
			return p.Scoop.ID, true
		}
	case SourceGitHub:
		if p.GitHub != nil {
			return p.GitHub.ID, true
		}
	}
	return "", false
}

// Configured lists the sources that have a block in this document.
func (p Packages) Configured() []Source {
	var out []Source
	for _, s := range AllSources() {
		if _, ok := p.ID(s); ok {
			out = append(out, s)
		}
	}
	return out
}

// WingetSpec holds winget coordinates and flags.
type WingetSpec struct {
	ID       string   `yaml:"id"`                 // "Publisher.App"
	Args     []string `yaml:"args,omitempty"`     // appended to winget install/upgrade
	Scope    string   `yaml:"scope,omitempty"`    // --scope machine|user
	Locale   string   `yaml:"locale,omitempty"`   // --locale pt-BR
	Feed     string   `yaml:"feed,omitempty"`     // --source winget|msstore|<private feed>
	Override string   `yaml:"override,omitempty"` // --override, passed raw to the installer
}

// ChocolateySpec holds chocolatey coordinates and flags.
type ChocolateySpec struct {
	ID   string   `yaml:"id"`
	Args []string `yaml:"args,omitempty"` // appended to choco install/upgrade
	// PackageParams goes to the package script (--package-parameters);
	// InstallArgs goes to the native installer (--install-arguments).
	// Chocolatey treats these as two distinct channels.
	PackageParams  string `yaml:"package_params,omitempty"`
	InstallArgs    string `yaml:"install_args,omitempty"`
	Feed           string `yaml:"feed,omitempty"`            // --source <feed>
	AllowDowngrade bool   `yaml:"allow_downgrade,omitempty"` // --allow-downgrade
}

// ScoopSpec holds scoop coordinates and flags.
type ScoopSpec struct {
	ID     string   `yaml:"id"`
	Args   []string `yaml:"args,omitempty"`
	Bucket string   `yaml:"bucket,omitempty"` // added before install when missing
	Global bool     `yaml:"global,omitempty"` // scoop install --global
	Arch   string   `yaml:"arch,omitempty"`   // --arch 64bit|32bit|arm64
}

// GitHubSpec holds GitHub release coordinates and flags.
type GitHubSpec struct {
	ID string `yaml:"id"` // "owner/repo"

	// Args go to the downloaded installer (or msiexec), not to a package
	// manager — the one field whose meaning differs from the other sources.
	// Without RunInstaller it has no effect.
	Args []string `yaml:"args,omitempty"`

	AssetPattern string `yaml:"asset_pattern,omitempty"` // glob forcing a specific asset
	RunInstaller bool   `yaml:"run_installer,omitempty"` // run the .exe/.msi instead of copying
	InstallDir   string `yaml:"install_dir,omitempty"`   // override the default binary dir
	BinaryName   string `yaml:"binary_name,omitempty"`   // binary name when it differs from Name

	// Checksum is the expected SHA-256 of the downloaded asset, with an
	// optional "sha256:" prefix. It pins the exact bytes, which the release's
	// own checksum file cannot do: that file is published by whoever published
	// the asset. Only worth setting for a pinned version.
	Checksum string `yaml:"checksum,omitempty"`

	// Prerelease accepts pre-release tags. The GitHub "latest release" endpoint
	// excludes them, so a repository that only publishes pre-releases resolves
	// to nothing at all without this.
	Prerelease bool `yaml:"prerelease,omitempty"`
}

// ── Loading ──────────────────────────────────────────────────────────────────

// LoadWithImports reads path and recursively resolves `import:` entries,
// merging every file into one Config. Import paths are relative to the file
// that declares them, and circular imports are reported as errors.
//
// Defaults are file-scoped: each file's `defaults:` block is applied to that
// file's applications before merging, so an imported file never inherits the
// importer's defaults.
// ErrNotFound reports that the config file itself is missing, as opposed to
// being unreadable or malformed. Callers turn it into advice rather than a
// stack of wrapped I/O errors.
var ErrNotFound = errors.New("no configuration file")

func LoadWithImports(path string) (*Config, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", path, err)
	}
	if _, statErr := os.Stat(absPath); errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("%w at %s; run 'apptide init' to create one, or pass --config", ErrNotFound, absPath)
	}
	visited := make(map[string]bool)
	return loadRecursive(absPath, visited)
}

func loadRecursive(absPath string, visited map[string]bool) (*Config, error) {
	if visited[absPath] {
		return nil, fmt.Errorf("circular import detected: %q", absPath)
	}
	visited[absPath] = true

	data, err := os.ReadFile(absPath) //#nosec G304 G703 -- the config path is the user's own choice
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", absPath, err)
	}

	imports, cfg, err := parseFile(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", absPath, err)
	}

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
		cfg.Applications = append(cfg.Applications, child.Applications...)
	}

	return cfg, nil
}

// parseFile decodes one config file. It walks the document with yaml.Node so
// that `import:` can coexist with the rest of the schema, and so that the
// legacy v1 layout (a top-level map of category name to package list) can be
// detected and converted in place.
//
// Defaults are applied here and the block is then cleared: everything
// downstream reads final values, never file-local defaults.
func parseFile(data []byte) (imports []string, cfg *Config, err error) {
	cfg = &Config{SchemaVersion: SchemaVersion}

	var doc yaml.Node
	if err = yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, cfg, nil // empty file
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("expected a YAML mapping at the top level, got %v", root.Kind)
	}

	var legacy []*yaml.Node // key, value pairs that look like v1 categories

	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		val := root.Content[i+1]

		switch key {
		case "import":
			if decErr := val.Decode(&imports); decErr != nil {
				return nil, nil, fmt.Errorf("decoding import list: %w", decErr)
			}
		case "schema_version":
			if decErr := val.Decode(&cfg.SchemaVersion); decErr != nil {
				return nil, nil, fmt.Errorf("decoding schema_version: %w", decErr)
			}
		case "settings":
			if decErr := val.Decode(&cfg.Settings); decErr != nil {
				return nil, nil, fmt.Errorf("decoding settings: %w", decErr)
			}
		case "defaults":
			if decErr := val.Decode(&cfg.Defaults); decErr != nil {
				return nil, nil, fmt.Errorf("decoding defaults: %w", decErr)
			}
		case "applications":
			if decErr := val.Decode(&cfg.Applications); decErr != nil {
				return nil, nil, fmt.Errorf("decoding applications: %w", decErr)
			}
		default:
			// v1 files map a category name straight to a package sequence.
			if val.Kind == yaml.SequenceNode {
				legacy = append(legacy, root.Content[i], val)
				continue
			}
			return nil, nil, fmt.Errorf("unknown top-level key %q", key)
		}
	}

	if len(legacy) > 0 {
		if len(cfg.Applications) > 0 {
			return nil, nil, fmt.Errorf(
				"file mixes the v1 layout (category keys) with `applications:` — migrate it fully")
		}
		apps, convErr := convertLegacy(legacy)
		if convErr != nil {
			return nil, nil, convErr
		}
		cfg.Applications = apps
		cfg.SchemaVersion = 1
	}

	applyDefaults(cfg)
	expandSettings(cfg.Settings)
	return imports, cfg, nil
}

// expandSettings resolves ${VAR} against the environment, so a config can name
// a path or a token without embedding either.
func expandSettings(s *Settings) {
	if s == nil {
		return
	}
	s.InstallDir = os.ExpandEnv(s.InstallDir)
	s.GitHubToken = os.ExpandEnv(s.GitHubToken)
}

// applyDefaults folds the file-level defaults into every application and then
// clears the block, so no consumer has to remember to resolve them.
func applyDefaults(cfg *Config) {
	d := cfg.Defaults
	for i := range cfg.Applications {
		app := &cfg.Applications[i]
		if d != nil {
			if app.Category == "" {
				app.Category = d.Category
			}
			if len(app.Source) == 0 {
				app.Source = d.Source
			}
			if app.Action == "" {
				app.Action = d.Action
			}
			if len(d.Tags) > 0 {
				app.Tags = append(append([]string{}, d.Tags...), app.Tags...)
			}
		}
		if app.Action == "" {
			app.Action = ActionInstall
		}
		for j := range app.Source {
			app.Source[j] = app.Source[j].Normalize()
		}
	}
	cfg.Defaults = nil
}

// ── Views ────────────────────────────────────────────────────────────────────

// Categories returns the category names present, in first-seen order, with
// applications that declare no category grouped under "Uncategorized".
func (c *Config) Categories() []string {
	seen := make(map[string]bool, len(c.Applications))
	var out []string
	for _, app := range c.Applications {
		cat := app.CategoryOrDefault()
		if !seen[cat] {
			seen[cat] = true
			out = append(out, cat)
		}
	}
	return out
}

// UncategorizedLabel is the category shown for applications without one.
const UncategorizedLabel = "Uncategorized"

// CategoryOrDefault returns the application's category, or UncategorizedLabel.
func (a Application) CategoryOrDefault() string {
	if a.Category == "" {
		return UncategorizedLabel
	}
	return a.Category
}

// InCategory returns the applications belonging to cat.
func (c *Config) InCategory(cat string) []Application {
	var out []Application
	for _, app := range c.Applications {
		if strings.EqualFold(app.CategoryOrDefault(), cat) {
			out = append(out, app)
		}
	}
	return out
}

// HasCategory reports whether any application declares cat.
func (c *Config) HasCategory(cat string) bool {
	for _, app := range c.Applications {
		if strings.EqualFold(app.CategoryOrDefault(), cat) {
			return true
		}
	}
	return false
}

// FilterBySource returns the applications that accept src as one of their
// sources.
func (c *Config) FilterBySource(src Source) []Application {
	var out []Application
	for _, app := range c.Applications {
		if app.Source.Contains(src) {
			out = append(out, app)
		}
	}
	return out
}
