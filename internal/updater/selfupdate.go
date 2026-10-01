// Package updater implements the self-update mechanism for apptide.
package updater

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lucasassuncao/apptide/internal/ghrelease"
	"github.com/lucasassuncao/bezel/theme"
)

// th is the terminal palette the rest of apptide prints with.
var th = theme.Resolve(theme.ThemeTerminal, true)

// SelfUpdate downloads the latest release of apptide from GitHub and
// replaces the current binary. The old binary is kept as <name>.old until the
// next run, when it is cleaned up automatically.
//
// repo must be in "owner/repo" format, e.g. "lucasassuncao/apptide".
// currentVersion is the running binary's version (e.g. "1.2.3" or "v1.2.3");
// the update is skipped when it matches the latest release tag.
func SelfUpdate(ctx context.Context, repo, token, currentVersion string) error {
	if repo == "" {
		return fmt.Errorf("--repo is required (e.g. --repo lucasassuncao/apptide)")
	}

	// Clean up any leftover .old binary from a previous update.
	cleanOldBinary()

	client := ghrelease.NewClient(token)

	fmt.Printf("Checking latest release of %s...\n", repo)

	rel, err := client.Latest(ctx, repo)
	if err != nil {
		return err
	}

	// Normalise both versions to a bare "X.Y.Z" form before comparing.
	if normalizeVersion(currentVersion) == normalizeVersion(rel.TagName) {
		fmt.Printf("Already up to date (%s).\n", rel.TagName)
		return nil
	}

	asset := selectWindowsAsset(rel.Assets)
	if asset == nil {
		return fmt.Errorf("no Windows %s binary found in release %s", runtime.GOARCH, rel.TagName)
	}

	fmt.Printf("Found %s → %s (%.1f MB)\n", th.Success.Render(rel.TagName), asset.Name, float64(asset.Size)/1e6)

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
	if err := client.Download(ctx, *asset, tmpPath); err != nil {
		return err
	}

	// Verify before the swap. This is the one download whose contents become
	// the program the user runs next, so an unverified byte here is worse than
	// anywhere else in apptide.
	if err := verifyDownload(ctx, client, tmpPath, *asset, rel.Assets); err != nil {
		os.Remove(tmpPath)
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

	fmt.Printf("%s  (old binary saved as %s.old)\n",
		th.Success.Render("✓ Updated to "+rel.TagName), filepath.Base(exePath))
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

// normalizeVersion strips a leading "v" so that "v1.2.3" and "1.2.3" compare equal.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// verifyDownload checks the new binary against the release's checksum file.
//
// A release without checksums is accepted with a warning rather than refused:
// older apptide releases published none, and refusing to update away from a
// buggy version would be worse than the risk. Releases built by the current
// goreleaser config always carry one.
func verifyDownload(ctx context.Context, c *ghrelease.Client, path string, asset ghrelease.Asset, all []ghrelease.Asset) error {
	want, ok := c.ChecksumFor(ctx, all, asset.Name)
	if !ok {
		fmt.Printf("%s release publishes no checksums; cannot verify the download\n", th.Warning.Render("warn:"))
		return nil
	}
	if err := ghrelease.VerifySHA256(path, want); err != nil {
		return fmt.Errorf("refusing to install %s: %w", asset.Name, err)
	}
	fmt.Printf("%s checksum verified\n", th.Success.Render("✓"))
	return nil
}

// selectWindowsAsset picks the best apptide binary for this machine.
//
// The architecture is not hard-coded: apptide ships windows/amd64 and
// windows/arm64, and scoring only amd64 meant an ARM machine updated itself
// into the emulated x64 build every time.
func selectWindowsAsset(assets []ghrelease.Asset) *ghrelease.Asset {
	return selectWindowsAssetFor(assets, runtime.GOARCH)
}

func selectWindowsAssetFor(assets []ghrelease.Asset, goarch string) *ghrelease.Asset {
	var best *ghrelease.Asset
	bestScore := 0

	for i := range assets {
		score := ghrelease.ScoreBinary(assets[i].Name, goarch)
		if score == 0 {
			continue
		}
		// Strictly greater: the first asset of a tied score wins, so the same
		// release always resolves to the same binary.
		if score > bestScore {
			best, bestScore = &assets[i], score
		}
	}
	return best
}
