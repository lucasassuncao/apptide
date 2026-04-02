package installer

import (
	"context"
	"fmt"
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
	dir := pkg.InstallDir
	if dir == "" {
		dir = t.installDir
	}
	matches, err := filepath.Glob(filepath.Join(dir, strings.ToLower(pkg.Name)+".*"))
	if err != nil || len(matches) == 0 {
		return false, ""
	}
	return true, ""
}

func (t *ThirdParty) Install(ctx context.Context, pkg config.Package) error {
	if pkg.URL == "" {
		return fmt.Errorf("missing 'url' for third_party package %q", pkg.Name)
	}

	tmp, err := t.download(ctx, pkg.URL)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)

	dir := pkg.InstallDir
	if dir == "" {
		dir = t.installDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating install dir: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(tmp))

	if pkg.RunInstaller {
		switch ext {
		case ".msi":
			args := []string{"/i", tmp, "/quiet", "/norestart"}
			return runCtx(ctx, "msiexec.exe", append(args, pkg.Args...)...)
		default:
			return runCtx(ctx, tmp, pkg.Args...)
		}
	}

	dest := filepath.Join(dir, strings.ToLower(pkg.Name)+ext)
	return copyFile(tmp, dest)
}

func (t *ThirdParty) Uninstall(ctx context.Context, pkg config.Package) error {
	return fmt.Errorf("uninstall is not supported for third_party source")
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
