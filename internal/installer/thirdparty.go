package installer

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucasassuncao/apptide/internal/config"
)

// ThirdParty downloads and installs standalone packages from arbitrary URLs.
type ThirdParty struct {
	installDir string
	client     *http.Client
}

func NewThirdParty(installDir string) *ThirdParty {
	return &ThirdParty{
		installDir: installDir,
		client:     &http.Client{},
	}
}

func (t *ThirdParty) Name() string      { return SourceThirdParty }
func (t *ThirdParty) IsAvailable() bool { return true }

// Check looks for any file matching <name>.* in the install directory.
func (t *ThirdParty) Check(pkg config.Package) (bool, string) {
	dir := t.installDir
	if pkg.ThirdParty != nil && pkg.ThirdParty.InstallDir != "" {
		dir = pkg.ThirdParty.InstallDir
	}
	matches, err := filepath.Glob(filepath.Join(dir, strings.ToLower(pkg.Name)+".*"))
	if err != nil || len(matches) == 0 {
		return false, ""
	}
	return true, ""
}

func (t *ThirdParty) Install(ctx context.Context, pkg config.Package) error {
	if pkg.ThirdParty == nil || pkg.ThirdParty.URL == "" {
		return fmt.Errorf("missing 'third_party.url' for package %q", pkg.Name)
	}
	tp := pkg.ThirdParty

	tmp, err := t.download(ctx, tp.URL)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	if tp.Checksum != "" {
		if err := verifyChecksum(tmp, tp.Checksum); err != nil {
			return err
		}
	}

	dir := t.installDir
	if tp.InstallDir != "" {
		dir = tp.InstallDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating install dir: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(tmp))

	if tp.RunInstaller {
		switch ext {
		case ".msi":
			args := []string{"/i", tmp, "/quiet", "/norestart"}
			return runCtx(ctx, "msiexec.exe", append(args, tp.Args...)...)
		default:
			return runCtx(ctx, tmp, tp.Args...)
		}
	}

	dest := filepath.Join(dir, strings.ToLower(pkg.Name)+ext)
	return copyFile(tmp, dest)
}

func (t *ThirdParty) Uninstall(ctx context.Context, pkg config.Package) error {
	return fmt.Errorf("uninstall is not supported for third_party source")
}

// verifyChecksum checks the file at path against a "algo:hexdigest" string.
// Supported algorithms: sha256, sha512.
func verifyChecksum(path, checksum string) error {
	parts := strings.SplitN(checksum, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid checksum format %q — expected algo:hexdigest (e.g. sha256:abc123...)", checksum)
	}
	algo, expected := parts[0], strings.ToLower(parts[1])

	var h hash.Hash
	switch strings.ToLower(algo) {
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return fmt.Errorf("unsupported checksum algorithm %q — supported: sha256, sha512", algo)
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file for checksum: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("computing checksum: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch: expected %s:%s, got %s", algo, expected, actual)
	}
	return nil
}

func (t *ThirdParty) download(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("building request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	ext := filepath.Ext(url)
	if ext == "" || len(ext) > 5 {
		ext = ".bin"
	}
	tmp, err := os.CreateTemp("", "apptide-*"+ext)
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("writing download: %w", err)
	}
	return tmp.Name(), nil
}
