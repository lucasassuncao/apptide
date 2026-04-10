package installer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// GitHub downloads and installs packages from GitHub Releases.
type GitHub struct {
	token      string
	installDir string
	client     *http.Client
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func NewGitHub(token, installDir string) *GitHub {
	return &GitHub{
		token:      token,
		installDir: installDir,
		client:     &http.Client{},
	}
}

func (g *GitHub) Name() string      { return SourceGitHub }
func (g *GitHub) IsAvailable() bool { return true } // no external dependency

// binaryBaseName returns the base name used for the installed binary or directory.
// It uses github.binary_name when set, otherwise falls back to the lowercased package name.
func binaryBaseName(pkg config.Package) string {
	if pkg.GitHub != nil && pkg.GitHub.BinaryName != "" {
		return strings.ToLower(pkg.GitHub.BinaryName)
	}
	return binaryBaseName(pkg)
}

// Check looks for the binary or directory that Install would have created.
func (g *GitHub) Check(pkg config.Package) (bool, string) {
	dir := g.installDir
	if pkg.GitHub != nil && pkg.GitHub.InstallDir != "" {
		dir = pkg.GitHub.InstallDir
	}
	// Single binary case: <name>.exe
	if _, err := os.Stat(filepath.Join(dir, binaryBaseName(pkg)+".exe")); err == nil {
		return true, ""
	}
	// Extracted directory case (extractAll)
	if info, err := os.Stat(filepath.Join(dir, binaryBaseName(pkg))); err == nil && info.IsDir() {
		return true, ""
	}
	return false, ""
}

func (g *GitHub) Install(ctx context.Context, pkg config.Package) error {
	if pkg.GitHub == nil || pkg.GitHub.Repo == "" {
		return fmt.Errorf("missing 'github.repo' for package %q", pkg.Name)
	}

	release, err := g.fetchRelease(ctx, pkg.GitHub.Repo, pkg.Version)
	if err != nil {
		return err
	}

	asset := selectAsset(release.Assets, pkg)
	if asset == nil {
		return fmt.Errorf("no suitable Windows asset found in %s @ %s", pkg.GitHub.Repo, release.TagName)
	}

	tmp, err := g.downloadAsset(ctx, asset)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	dir := g.installDir
	if pkg.GitHub.InstallDir != "" {
		dir = pkg.GitHub.InstallDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating install dir %q: %w", dir, err)
	}

	return g.installFile(ctx, tmp, asset.Name, dir, pkg)
}

func (g *GitHub) Uninstall(ctx context.Context, pkg config.Package) error {
	return fmt.Errorf("uninstall is not supported for github source")
}

// fetchRelease returns the latest or a specific tagged release from GitHub API.
// For versioned lookups it automatically retries with and without a "v" prefix
// so that both "1.2.3" and "v1.2.3" resolve regardless of the repo's tag convention.
func (g *GitHub) fetchRelease(ctx context.Context, repo, version string) (*ghRelease, error) {
	if version == "" || strings.EqualFold(version, "latest") {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
		return g.fetchReleaseURL(ctx, url, "latest", repo)
	}

	// Build the list of tags to try: exact first, then the v-toggled variant.
	tags := []string{version}
	if strings.HasPrefix(version, "v") {
		tags = append(tags, version[1:]) // "v1.2.3" → also try "1.2.3"
	} else {
		tags = append(tags, "v"+version) // "1.2.3"  → also try "v1.2.3"
	}

	for i, tag := range tags {
		url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
		rel, err := g.fetchReleaseURL(ctx, url, tag, repo)
		if err == nil {
			return rel, nil
		}
		// Only retry on 404; propagate rate-limit and other errors immediately.
		isNotFound := strings.Contains(err.Error(), "not found")
		if !isNotFound || i == len(tags)-1 {
			return nil, fmt.Errorf("release %q not found for %s (tried: %s)", version, repo, strings.Join(tags, ", "))
		}
	}
	return nil, fmt.Errorf("release %q not found for %s", version, repo)
}

// fetchReleaseURL performs a single GitHub releases API request.
func (g *GitHub) fetchReleaseURL(ctx context.Context, url, tag, repo string) (*ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "github.com/lucasassuncao/apptide/1.0")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("release %q not found for %s", tag, repo)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return nil, fmt.Errorf("github api rate-limited (set GITHUB_TOKEN to increase limits)")
	default:
		return nil, fmt.Errorf("github api returned %d for %s", resp.StatusCode, repo)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release response: %w", err)
	}
	return &rel, nil
}

// downloadAsset fetches the asset and saves it to a temp file.
func (g *GitHub) downloadAsset(ctx context.Context, asset *ghAsset) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "github.com/lucasassuncao/apptide/1.0")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	ext := filepath.Ext(asset.Name)
	tmp, err := os.CreateTemp("", "apptide-*"+ext)
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("writing download: %w", err)
	}
	return tmp.Name(), nil
}

// installFile routes to the appropriate strategy based on the asset's extension.
func (g *GitHub) installFile(ctx context.Context, src, assetName, dir string, pkg config.Package) error {
	lower := strings.ToLower(assetName)
	gh := pkg.GitHub // guaranteed non-nil by Install
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return g.extractTarGz(src, dir, pkg)
	case strings.HasSuffix(lower, ".7z"):
		return g.extract7z(ctx, src, dir, pkg)
	case strings.HasSuffix(lower, ".zip"):
		return g.extractZip(src, dir, pkg)
	case strings.HasSuffix(lower, ".exe"):
		if gh.RunInstaller {
			return runCtx(ctx, src, gh.Args...)
		}
		dest := filepath.Join(dir, binaryBaseName(pkg)+".exe")
		return copyFile(src, dest)
	case strings.HasSuffix(lower, ".msi"):
		if gh.RunInstaller {
			args := []string{"/i", src, "/quiet", "/norestart"}
			return runCtx(ctx, "msiexec.exe", append(args, gh.Args...)...)
		}
		dest := filepath.Join(dir, binaryBaseName(pkg)+".msi")
		return copyFile(src, dest)
	default:
		dest := filepath.Join(dir, assetName)
		return copyFile(src, dest)
	}
}

// extractZip finds the best .exe inside the archive and copies it to dir.
// If no .exe is found, it extracts the whole archive into a sub-directory.
func (g *GitHub) extractZip(src, dir string, pkg config.Package) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}
	defer r.Close()

	type candidate struct {
		f     *zip.File
		depth int
	}
	var exes []candidate
	for _, f := range r.File {
		if !f.FileInfo().IsDir() && strings.ToLower(filepath.Ext(f.Name)) == ".exe" {
			exes = append(exes, candidate{f, strings.Count(f.Name, "/")})
		}
	}

	if len(exes) == 0 {
		// No .exe — extract everything into a sub-directory named after the package.
		subDir := filepath.Join(dir, binaryBaseName(pkg))
		return extractAll(r, subDir)
	}

	// Prefer the shallowest exe; break ties by name similarity with pkg.Name.
	wantName := binaryBaseName(pkg) + ".exe"
	sort.Slice(exes, func(i, j int) bool {
		if exes[i].depth != exes[j].depth {
			return exes[i].depth < exes[j].depth
		}
		ni := strings.ToLower(filepath.Base(exes[i].f.Name))
		nj := strings.ToLower(filepath.Base(exes[j].f.Name))
		if ni == wantName {
			return true
		}
		if nj == wantName {
			return false
		}
		return ni < nj
	})

	best := exes[0].f
	dest := filepath.Join(dir, binaryBaseName(pkg)+".exe")

	rc, err := best.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

// extractAll dumps the full zip contents into destDir, stripping the top-level folder.
func extractAll(r *zip.ReadCloser, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		// Strip the first path component (common zip root folder).
		rel := filepath.ToSlash(f.Name)
		if idx := strings.Index(rel, "/"); idx >= 0 {
			rel = rel[idx+1:]
		}
		if rel == "" {
			continue
		}
		dest := filepath.Join(destDir, filepath.FromSlash(rel))

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// extractTarGz finds the best .exe inside a .tar.gz archive and copies it to dir.
// If no .exe is found the full archive is extracted into a sub-directory.
func (g *GitHub) extractTarGz(src, dir string, pkg config.Package) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening tar.gz: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	type tarCandidate struct {
		header *tar.Header
		depth  int
	}
	// First pass: collect all entries and find exe candidates.
	var entries []tarEntry
	var exes []tarCandidate

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			entries = append(entries, tarEntry{header: hdr})
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("reading tar entry %s: %w", hdr.Name, err)
		}
		entries = append(entries, tarEntry{header: hdr, content: data})
		if strings.ToLower(filepath.Ext(hdr.Name)) == ".exe" {
			exes = append(exes, tarCandidate{hdr, strings.Count(hdr.Name, "/")})
		}
	}

	if len(exes) == 0 {
		// No .exe — extract everything into a sub-directory.
		subDir := filepath.Join(dir, binaryBaseName(pkg))
		return extractTarEntries(entries, subDir)
	}

	wantName := binaryBaseName(pkg) + ".exe"
	sort.Slice(exes, func(i, j int) bool {
		if exes[i].depth != exes[j].depth {
			return exes[i].depth < exes[j].depth
		}
		ni := strings.ToLower(filepath.Base(exes[i].header.Name))
		nj := strings.ToLower(filepath.Base(exes[j].header.Name))
		if ni == wantName {
			return true
		}
		if nj == wantName {
			return false
		}
		return ni < nj
	})

	bestName := exes[0].header.Name
	dest := filepath.Join(dir, binaryBaseName(pkg)+".exe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		if e.header.Name == bestName {
			out, err := os.Create(dest)
			if err != nil {
				return err
			}
			_, err = out.Write(e.content)
			out.Close()
			return err
		}
	}
	return fmt.Errorf("exe entry not found in tar: %s", bestName)
}

type tarEntry struct {
	header  *tar.Header
	content []byte
}

// extractTarEntries writes all entries to destDir, stripping the top-level folder.
func extractTarEntries(entries []tarEntry, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		rel := filepath.ToSlash(e.header.Name)
		if idx := strings.Index(rel, "/"); idx >= 0 {
			rel = rel[idx+1:]
		}
		if rel == "" {
			continue
		}
		dest := filepath.Join(destDir, filepath.FromSlash(rel))
		if e.header.Typeflag == tar.TypeDir {
			os.MkdirAll(dest, 0o755) //nolint:errcheck
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		_, err = out.Write(e.content)
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// extract7z extracts a .7z archive using the 7z.exe CLI tool.
// Falls back to 7za.exe if 7z.exe is not found.
func (g *GitHub) extract7z(ctx context.Context, src, dir string, pkg config.Package) error {
	sevenZip, err := find7z()
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "apptide-7z-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := runCtx(ctx, sevenZip, "x", src, "-o"+tmp, "-y"); err != nil {
		return fmt.Errorf("7z extraction failed: %w", err)
	}

	// Walk extracted dir to find exe candidates.
	type candidate struct {
		path  string
		depth int
	}
	var exes []candidate
	_ = filepath.Walk(tmp, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(p)) == ".exe" {
			rel, _ := filepath.Rel(tmp, p)
			exes = append(exes, candidate{p, strings.Count(rel, string(filepath.Separator))})
		}
		return nil
	})

	if len(exes) == 0 {
		// No .exe — copy the whole extracted dir.
		subDir := filepath.Join(dir, binaryBaseName(pkg))
		return copyDir(tmp, subDir)
	}

	wantName := binaryBaseName(pkg) + ".exe"
	sort.Slice(exes, func(i, j int) bool {
		if exes[i].depth != exes[j].depth {
			return exes[i].depth < exes[j].depth
		}
		ni := strings.ToLower(filepath.Base(exes[i].path))
		nj := strings.ToLower(filepath.Base(exes[j].path))
		if ni == wantName {
			return true
		}
		if nj == wantName {
			return false
		}
		return ni < nj
	})

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(dir, binaryBaseName(pkg)+".exe")
	return copyFile(exes[0].path, dest)
}

func find7z() (string, error) {
	for _, name := range []string{"7z", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("7z not found in PATH — install 7-Zip to extract .7z archives")
}

// copyDir recursively copies src directory to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

// selectAsset picks the best release asset for Windows amd64.
func selectAsset(assets []ghAsset, pkg config.Package) *ghAsset {
	// User-supplied glob takes priority.
	if pkg.GitHub != nil && pkg.GitHub.AssetPattern != "" {
		for i := range assets {
			if matched, _ := filepath.Match(pkg.GitHub.AssetPattern, assets[i].Name); matched {
				return &assets[i]
			}
		}
	}

	type scored struct {
		asset *ghAsset
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
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].asset
}

// scoreAsset returns a relevance score for a Windows amd64 asset.
// Returns 0 for assets that should be skipped entirely.
func scoreAsset(name string) int {
	lower := strings.ToLower(name)

	// Hard exclusions — checksums, signatures, non-Windows platforms.
	for _, skip := range []string{
		".sha256", ".sha512", ".sha1", ".md5",
		".sig", ".asc", ".txt",
		"checksums", "checksum",
		".deb", ".rpm", ".dmg", ".pkg", ".apk",
		"linux", "darwin", "macos", "android",
		"-arm", "_arm", "arm64", "aarch64",
		"source", "src",
	} {
		if strings.Contains(lower, skip) {
			return 0
		}
	}

	score := 1 // baseline: any non-excluded asset gets a chance

	// Windows platform indicators.
	for _, win := range []string{"windows", "win64", "win32", "_win_", "-win-", ".win."} {
		if strings.Contains(lower, win) {
			score += 5
			break
		}
	}

	// x86-64 architecture indicators.
	for _, arch := range []string{"x86_64", "amd64", "x64", "64bit", "64-bit"} {
		if strings.Contains(lower, arch) {
			score += 3
			break
		}
	}

	// Format preference: zip/tar.gz/7z (portable) > exe > msi.
	switch {
	case strings.HasSuffix(lower, ".zip"):
		score += 2
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		score += 2
	case strings.HasSuffix(lower, ".7z"):
		score += 2
	case strings.HasSuffix(lower, ".exe"):
		score += 1
	case strings.HasSuffix(lower, ".msi"):
		score += 1
	}

	return score
}
