// Package ghrelease talks to the GitHub Releases API and decides which asset
// of a release belongs on this machine.
//
// It exists because the installer and the self-updater each grew their own
// copy of the same three things: the API calls, the download, and the rules
// for reading a platform out of an asset name. The copies drifted — one of
// them excluded every arm64 asset, the other scored only amd64 — so a machine
// could install the wrong architecture in two different ways.
package ghrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// userAgent identifies apptide to the GitHub API, which requires one.
const userAgent = "github.com/lucasassuncao/apptide"

// requestTimeout bounds a single API call or download. Without it a server
// that accepts the connection and then goes quiet hangs the command forever.
const requestTimeout = 5 * time.Minute

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is a published GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Client performs authenticated GitHub API requests.
type Client struct {
	token string
	http  *http.Client
}

// NewClient returns a client that sends token as a bearer credential when one
// is given. An empty token means unauthenticated, which GitHub rate-limits to
// 60 requests an hour per address.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: requestTimeout},
	}
}

// NotFoundError reports a release or repository the API does not have. It is
// distinguished from other failures so a caller can retry a different tag
// without swallowing a rate limit as "not found".
type NotFoundError struct{ What string }

func (e *NotFoundError) Error() string { return fmt.Sprintf("release %q not found", e.What) }

// Latest returns the newest non-prerelease release.
func (c *Client) Latest(ctx context.Context, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	return c.release(ctx, url, "latest")
}

// Tag returns the release published under an exact tag.
func (c *Client) Tag(ctx context.Context, repo, tag string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	return c.release(ctx, url, tag)
}

// TagFuzzy returns the release for tag, retrying with the "v" prefix toggled.
// Repositories disagree on whether tags carry it, and a config should not have
// to know which convention a project picked.
func (c *Client) TagFuzzy(ctx context.Context, repo, tag string) (*Release, error) {
	tags := []string{tag}
	if strings.HasPrefix(tag, "v") {
		tags = append(tags, tag[1:])
	} else {
		tags = append(tags, "v"+tag)
	}

	for i, t := range tags {
		rel, err := c.Tag(ctx, repo, t)
		if err == nil {
			return rel, nil
		}
		// Only a missing tag justifies trying the other spelling; a rate limit
		// or a network error must surface as itself.
		var notFound *NotFoundError
		if !errors.As(err, &notFound) {
			return nil, err
		}
		if i == len(tags)-1 {
			return nil, fmt.Errorf("release %q not found for %s (tried: %s)", tag, repo, strings.Join(tags, ", "))
		}
	}
	return nil, &NotFoundError{What: tag}
}

// Newest returns the most recent published release, pre-release or not.
//
// The "latest release" endpoint deliberately excludes pre-releases, so a
// repository that only publishes those resolves to nothing at all.
func (c *Client) Newest(ctx context.Context, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", repo)
	body, err := c.get(ctx, url, repo)
	if err != nil {
		return nil, err
	}

	var releases []Release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("decoding release list for %s: %w", repo, err)
	}
	for i := range releases {
		if !releases[i].Draft {
			return &releases[i], nil
		}
	}
	return nil, fmt.Errorf("no published release found for %s", repo)
}

func (c *Client) release(ctx context.Context, url, what string) (*Release, error) {
	body, err := c.get(ctx, url, what)
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decoding release response: %w", err)
	}
	return &rel, nil
}

// get performs one API request and returns the body.
func (c *Client) get(ctx context.Context, url, what string) ([]byte, error) {
	req, err := c.request(ctx, url)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, &NotFoundError{What: what}
	case http.StatusForbidden, http.StatusTooManyRequests:
		// A 403 is also how GitHub answers a bad token or a private repository,
		// and telling those users to set GITHUB_TOKEN sends them in circles.
		// The remaining-requests header is what separates the two.
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("github api rate-limited (set GITHUB_TOKEN to increase limits)")
		}
		if c.token != "" {
			return nil, fmt.Errorf("github api denied access to %s: check the token's scope", what)
		}
		return nil, fmt.Errorf("github api denied access to %s: set GITHUB_TOKEN if the repository is private", what)
	default:
		return nil, fmt.Errorf("github api returned %d for %s", resp.StatusCode, what)
	}

	return io.ReadAll(resp.Body)
}

func (c *Client) request(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

// Download writes an asset to destPath, creating parent directories.
func (c *Client) Download(ctx context.Context, asset Asset, destPath string) error {
	req, err := c.request(ctx, asset.URL)
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download of %s returned HTTP %d", asset.Name, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //#nosec G302 G304 -- a downloaded binary must be executable; destPath is chosen by the caller, not the release
	if err != nil {
		return fmt.Errorf("creating %q: %w", destPath, err)
	}

	written, err := io.Copy(f, resp.Body)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(destPath)
		return fmt.Errorf("writing %s: %w", asset.Name, err)
	}

	// The release says how big the file is; a short read means a truncated
	// download, which would otherwise be installed as if it were complete.
	if asset.Size > 0 && written != asset.Size {
		os.Remove(destPath)
		return fmt.Errorf("download of %s is %d bytes, expected %d", asset.Name, written, asset.Size)
	}
	return nil
}

// DownloadTemp writes an asset to a temp file and returns its path. The caller
// removes it.
func (c *Client) DownloadTemp(ctx context.Context, asset Asset) (string, error) {
	tmp, err := os.CreateTemp("", "apptide-*"+filepath.Ext(asset.Name))
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	path := tmp.Name()
	tmp.Close()

	if err := c.Download(ctx, asset, path); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// ── Integrity ────────────────────────────────────────────────────────────────

// SHA256 returns the hex-encoded SHA-256 of a file.
func SHA256(path string) (string, error) {
	f, err := os.Open(path) //#nosec G304 -- hashing a file apptide just downloaded to its own temp path
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA256 checks a downloaded file against an expected digest. A "sha256:"
// prefix is accepted so a config can name the algorithm it is using.
func VerifySHA256(path, want string) error {
	want = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(want, "sha256:")))
	if want == "" {
		return nil
	}
	got, err := SHA256(path)
	if err != nil {
		return fmt.Errorf("hashing %q: %w", path, err)
	}
	if got != want {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", want, got)
	}
	return nil
}

// ChecksumAsset returns the release asset that holds checksums, if the release
// publishes one. goreleaser and most release tooling ship exactly one.
func ChecksumAsset(assets []Asset) (Asset, bool) {
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, "checksums") || strings.Contains(lower, "checksum") ||
			strings.HasSuffix(lower, ".sha256") {
			return a, true
		}
	}
	return Asset{}, false
}

// ParseChecksums reads the "<digest>  <filename>" lines that sha256sum and
// goreleaser produce, and returns the digest for name.
func ParseChecksums(body, name string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// The filename may carry a "*" marker for binary mode.
		file := strings.TrimPrefix(fields[len(fields)-1], "*")
		if strings.EqualFold(filepath.Base(file), name) {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}

// ChecksumFor downloads the release's checksum file and returns the digest
// recorded for asset. Missing checksums are not an error: most repositories
// publish none, and refusing to install from those would make the github
// source useless.
func (c *Client) ChecksumFor(ctx context.Context, assets []Asset, name string) (string, bool) {
	sums, ok := ChecksumAsset(assets)
	if !ok {
		return "", false
	}
	req, err := c.request(ctx, sums.URL)
	if err != nil {
		return "", false
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	// A checksum list is a few kilobytes; the cap is there so a mislabelled
	// asset cannot pull a gigabyte into memory.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", false
	}
	return ParseChecksums(string(body), name)
}
