package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/lucasassuncao/apptide/internal/archive"
	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/ghrelease"
)

// GitHub downloads and installs packages from GitHub Releases.
type GitHub struct {
	installDir string
	client     *ghrelease.Client
}

func NewGitHub(token, installDir string) *GitHub {
	return &GitHub{
		installDir: installDir,
		client:     ghrelease.NewClient(token),
	}
}

func (g *GitHub) Name() string      { return string(config.SourceGitHub) }
func (g *GitHub) IsAvailable() bool { return true } // no external dependency

// binaryBaseName returns the base name used for the installed binary or directory.
// It uses github.binary_name when set, otherwise falls back to the lowercased package name.
func binaryBaseName(app config.Application) string {
	if app.Package.GitHub != nil && app.Package.GitHub.BinaryName != "" {
		return strings.ToLower(app.Package.GitHub.BinaryName)
	}
	return strings.ToLower(app.Name)
}

// targetDir is where this application's files go.
func (g *GitHub) targetDir(app config.Application) string {
	if app.Package.GitHub != nil && app.Package.GitHub.InstallDir != "" {
		return app.Package.GitHub.InstallDir
	}
	return g.installDir
}

// marker records what was installed, next to the files themselves.
//
// GitHub releases have no package database to ask, so without this apptide
// could see that a binary exists but never which release it came from — which
// is why this source had no version, no upgrade and no uninstall. The file
// lives beside the binary rather than in the state file so that it survives a
// lost state, and so a directory copied to another machine stays self-describing.
type marker struct {
	Version     string `json:"version"`
	Repo        string `json:"repo"`
	Asset       string `json:"asset,omitempty"`
	InstalledAt string `json:"installed_at"`
	// RunInstaller records that Windows owns the installed program, so
	// deleting files here would not uninstall anything.
	RunInstaller bool `json:"run_installer,omitempty"`
}

func markerPath(dir, base string) string {
	return filepath.Join(dir, ".apptide", base+".json")
}

func readMarker(dir, base string) (marker, bool) {
	data, err := os.ReadFile(markerPath(dir, base))
	if err != nil {
		return marker{}, false
	}
	var m marker
	if err := json.Unmarshal(data, &m); err != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(dir, base string, m marker) error {
	path := markerPath(dir, base)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating marker dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// Check looks for the binary or directory that Install would have created, and
// reports the release it came from when the marker is present. An install made
// before markers existed still reports installed, just without a version.
func (g *GitHub) Check(app config.Application) (bool, string) {
	dir := g.targetDir(app)
	base := binaryBaseName(app)

	m, hasMarker := readMarker(dir, base)

	// run_installer hands the program to Windows; the marker is the only trace
	// apptide keeps of it.
	if hasMarker && m.RunInstaller {
		return true, m.Version
	}

	// Single binary case: <name>.exe
	if _, err := os.Stat(filepath.Join(dir, base+".exe")); err == nil {
		return true, m.Version
	}
	// Extracted directory case (extractAll)
	if info, err := os.Stat(filepath.Join(dir, base)); err == nil && info.IsDir() {
		return true, m.Version
	}
	return false, ""
}

func (g *GitHub) Install(ctx context.Context, app config.Application) error {
	spec := app.Package.GitHub
	if spec == nil || spec.ID == "" {
		return fmt.Errorf("%w: no 'package.github.id' for %q", ErrNotOffered, app.Name)
	}

	release, err := g.resolveRelease(ctx, spec, app.Version)
	if err != nil {
		return err
	}

	// Decide whether anything needs to happen before downloading.
	if installed, current := g.Check(app); installed {
		switch {
		case app.SkipUpgrade:
			return ErrAlreadyInstalled
		case current != "" && current == release.TagName:
			return ErrAlreadyInstalled
		case current == "":
			// Installed before markers existed: the release is unknown, so
			// reinstalling is the only way to learn it.
		}
	}

	asset := selectAsset(release.Assets, app)
	if asset == nil {
		return fmt.Errorf("no suitable Windows asset found in %s @ %s", spec.ID, release.TagName)
	}

	tmp, err := g.client.DownloadTemp(ctx, *asset)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	if err := g.verify(ctx, tmp, *asset, release.Assets, spec); err != nil {
		return err
	}

	dir := g.targetDir(app)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating install dir %q: %w", dir, err)
	}

	if err := g.installFile(ctx, tmp, asset.Name, dir, app); err != nil {
		return err
	}

	return writeMarker(dir, binaryBaseName(app), marker{
		Version:      release.TagName,
		Repo:         spec.ID,
		Asset:        asset.Name,
		InstalledAt:  time.Now().UTC().Format(time.RFC3339),
		RunInstaller: spec.RunInstaller,
	})
}

// Uninstall deletes what Install placed: the binary or extracted directory,
// plus the marker.
//
// With run_installer the program was handed to a Windows installer and is
// registered in Add/Remove Programs; deleting our files would leave it
// installed but untracked, so that case refuses with instructions instead.
func (g *GitHub) Uninstall(ctx context.Context, app config.Application) error {
	spec := app.Package.GitHub
	if spec == nil || spec.ID == "" {
		return fmt.Errorf("%w: no 'package.github.id' for %q", ErrNotOffered, app.Name)
	}

	dir := g.targetDir(app)
	base := binaryBaseName(app)
	m, _ := readMarker(dir, base)

	if spec.RunInstaller || m.RunInstaller {
		return fmt.Errorf(
			"%q was installed by its own installer — remove it from Windows Settings > Apps, then run 'apptide adopt %s --forget'",
			app.Name, app.Name)
	}

	removed := false
	for _, path := range []string{
		filepath.Join(dir, base+".exe"),
		filepath.Join(dir, base+".msi"),
		filepath.Join(dir, base),
	} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %q: %w", path, err)
		}
		removed = true
	}

	// The marker goes last: while it exists, a failed removal is still visible.
	if err := os.Remove(markerPath(dir, base)); err == nil {
		removed = true
	}

	if !removed {
		return ErrAlreadyInstalled // nothing was there; treated as a no-op
	}
	return nil
}

// resolveRelease picks the release to install, honouring prerelease.
func (g *GitHub) resolveRelease(ctx context.Context, spec *config.GitHubSpec, version string) (*ghrelease.Release, error) {
	pinned := version != "" && !strings.EqualFold(version, "latest")
	switch {
	case pinned:
		return g.client.TagFuzzy(ctx, spec.ID, version)
	case spec.Prerelease:
		return g.client.Newest(ctx, spec.ID)
	default:
		return g.client.Latest(ctx, spec.ID)
	}
}

// verify checks the download against a digest before anything is run or
// copied into place.
//
// A digest in the config is authoritative and a mismatch fails the install.
// Otherwise the release's own checksum file is used when it publishes one:
// that only proves the download matches what the release says, which still
// catches a truncated or tampered transfer.
func (g *GitHub) verify(ctx context.Context, path string, asset ghrelease.Asset, all []ghrelease.Asset, spec *config.GitHubSpec) error {
	if spec.Checksum != "" {
		if err := ghrelease.VerifySHA256(path, spec.Checksum); err != nil {
			return fmt.Errorf("verifying %s: %w", asset.Name, err)
		}
		return nil
	}

	want, ok := g.client.ChecksumFor(ctx, all, asset.Name)
	if !ok {
		// Most repositories publish no checksums; refusing those would make
		// the github source useless.
		return nil
	}
	if err := ghrelease.VerifySHA256(path, want); err != nil {
		return fmt.Errorf("verifying %s against the release checksums: %w", asset.Name, err)
	}
	return nil
}

// installFile routes to the appropriate strategy based on the asset's extension.
func (g *GitHub) installFile(ctx context.Context, src, assetName, dir string, app config.Application) error {
	lower := strings.ToLower(assetName)
	gh := app.Package.GitHub // guaranteed non-nil by Install
	base := binaryBaseName(app)

	switch {
	case archive.IsArchive(assetName):
		return archive.Extract(ctx, src, assetName, dir, base)
	case strings.HasSuffix(lower, ".exe"):
		if gh.RunInstaller {
			return runCtx(ctx, src, gh.Args...)
		}
		return archive.CopyFile(src, filepath.Join(dir, base+".exe"))
	case strings.HasSuffix(lower, ".msi"):
		if gh.RunInstaller {
			return runCtx(ctx, "msiexec.exe", msiexecArgs(src, gh)...)
		}
		return archive.CopyFile(src, filepath.Join(dir, base+".msi"))
	default:
		// An unknown format is kept under its own name; the user asked for this
		// asset, so handing them the file is better than refusing it.
		dest, err := safeDest(dir, assetName)
		if err != nil {
			return err
		}
		return archive.CopyFile(src, dest)
	}
}

// safeDest keeps an asset name from steering the write out of dir. The name
// comes from the release, and GitHub does not forbid a slash in it.
func safeDest(dir, assetName string) (string, error) {
	clean := filepath.Base(filepath.FromSlash(assetName))
	if clean == "." || clean == string(filepath.Separator) || clean == "" {
		return "", fmt.Errorf("asset name %q is not usable as a file name", assetName)
	}
	return filepath.Join(dir, clean), nil
}

// msiexecArgs is the full msiexec argv for a downloaded .msi. Kept separate
// from installFile so the command line can be asserted without running msiexec.
func msiexecArgs(src string, gh *config.GitHubSpec) []string {
	// gh.Args go last so a user-supplied switch can override what we chose.
	return append([]string{"/i", src, "/quiet", "/norestart"}, gh.Args...)
}

// selectAsset picks the best release asset for this machine.
func selectAsset(assets []ghrelease.Asset, app config.Application) *ghrelease.Asset {
	// User-supplied glob takes priority.
	if app.Package.GitHub != nil && app.Package.GitHub.AssetPattern != "" {
		for i := range assets {
			if matched, _ := filepath.Match(app.Package.GitHub.AssetPattern, assets[i].Name); matched {
				return &assets[i]
			}
		}
	}

	type scored struct {
		asset *ghrelease.Asset
		score int
	}
	var candidates []scored
	for i := range assets {
		if s := scoreAsset(assets[i].Name); s > 0 {
			candidates = append(candidates, scored{&assets[i], s})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	// Stable: assets that tie on score keep the order the release lists them
	// in, so the same repository resolves to the same asset on every run.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].asset
}

// scoreAsset returns a relevance score for an asset on the machine apptide is
// running on. Returns 0 for assets that should be skipped entirely.
func scoreAsset(name string) int { return scoreAssetFor(name, runtime.GOARCH) }

// scoreAssetFor is scoreAsset with the target architecture spelled out, so the
// choice can be tested for a machine other than the one running the test.
//
// It differs from ghrelease.ScoreBinary in what it prefers: an installed
// application is better served by a portable archive than by an installer,
// whereas self-update needs a bare executable it can swap in.
func scoreAssetFor(name, goarch string) int {
	if ghrelease.Excluded(name) {
		return 0
	}
	archScore, ok := ghrelease.ArchScore(name, goarch)
	if !ok {
		return 0
	}

	// Baseline of 1: any asset that could run here gets a chance.
	score := 1 + ghrelease.WindowsScore(name) + archScore

	// Format preference: zip/tar.gz/7z (portable) > exe > msi.
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"),
		strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"),
		strings.HasSuffix(lower, ".7z"):
		score += 2
	case strings.HasSuffix(lower, ".exe"), strings.HasSuffix(lower, ".msi"):
		score++
	}

	return score
}
