// Package updater implements the self-update mechanism for apptide.
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	reset  = "\033[0m"
	green  = "\033[32m"
	yellow = "\033[33m"
	gray   = "\033[90m"
)

// SelfUpdate downloads the latest release of apptide from GitHub and
// replaces the current binary. The old binary is kept as <name>.old until the
// next run, when it is cleaned up automatically.
//
// repo must be in "owner/repo" format, e.g. "lucas/apptide".
func SelfUpdate(repo, token string) error {
	if repo == "" {
		return fmt.Errorf("--repo is required (e.g. --repo lucas/apptide)")
	}

	// Clean up any leftover .old binary from a previous update.
	cleanOldBinary()

	fmt.Printf("Checking latest release of %s...\n", repo)

	rel, err := fetchLatestRelease(repo, token)
	if err != nil {
		return err
	}

	asset := selectWindowsAsset(rel.Assets)
	if asset == nil {
		return fmt.Errorf("no Windows amd64 binary found in release %s", rel.TagName)
	}

	fmt.Printf("Found %s%s%s → %s (%.1f MB)\n", green, rel.TagName, reset, asset.Name, float64(asset.Size)/1e6)

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine current executable path: %w", err)
	}
	// Resolve any symlinks so we operate on the real file.
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	fmt.Printf("Downloading new binary...\n")
	tmpPath := exePath + ".new"
	if err := download(asset.BrowserDownloadURL, tmpPath, token); err != nil {
		return err
	}

	// On Windows, we cannot delete the running binary but we can rename it.
	oldPath := exePath + ".old"
	os.Remove(oldPath) // remove a possible stale .old

	fmt.Printf("Replacing binary...\n")
	if err := os.Rename(exePath, oldPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming current binary: %w", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		// Try to roll back.
		os.Rename(oldPath, exePath) //nolint:errcheck
		os.Remove(tmpPath)
		return fmt.Errorf("installing new binary: %w", err)
	}

	fmt.Printf("%s✓ Updated to %s%s  (old binary saved as %s.old)\n",
		green, rel.TagName, reset, filepath.Base(exePath))
	return nil
}

// CleanOldBinary removes a <exe>.old file left by a previous self-update.
// Call this from main() at startup.
func CleanOldBinary() {
	cleanOldBinary()
}

func cleanOldBinary() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	if _, err := os.Stat(old); err == nil {
		os.Remove(old)
	}
}

// ── GitHub API ────────────────────────────────────────────────────────────────

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func fetchLatestRelease(repo, token string) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "github.com/lucasassuncao/apptide/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found for %s", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &rel, nil
}

// selectWindowsAsset picks the best asset for Windows amd64.
// Prefers .exe directly; falls back to any non-excluded file with Windows indicators.
func selectWindowsAsset(assets []ghAsset) *ghAsset {
	// Exclusion suffixes
	skip := []string{".sha256", ".sha512", ".sig", ".asc", "checksums", ".txt", "linux", "darwin", "macos"}

	type scored struct {
		a     *ghAsset
		score int
	}
	var candidates []scored

	for i := range assets {
		lower := strings.ToLower(assets[i].Name)
		excluded := false
		for _, s := range skip {
			if strings.Contains(lower, s) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}

		score := 0
		for _, w := range []string{"windows", "win64", "win32"} {
			if strings.Contains(lower, w) {
				score += 5
				break
			}
		}
		for _, a := range []string{"amd64", "x86_64", "x64"} {
			if strings.Contains(lower, a) {
				score += 3
				break
			}
		}
		if filepath.Ext(lower) == ".exe" {
			score += 2
		}
		candidates = append(candidates, scored{&assets[i], score})
	}

	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.score > best.score {
			best = c
		}
	}
	return best.a
}

// download fetches url into destPath.
func download(url, destPath, token string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "github.com/lucasassuncao/apptide/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("creating temp binary: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("writing binary: %w", err)
	}
	return nil
}
