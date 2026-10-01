package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipFile is one entry to write into a test archive.
type zipFile struct {
	name string
	body string
}

func writeZip(t *testing.T, files []zipFile) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for _, entry := range files {
		// Create is used rather than CreateHeader so the names go in verbatim,
		// which is the point: a real attacker writes the name they want.
		out, err := w.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTarGz(t *testing.T, files []zipFile) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, entry := range files {
		hdr := &tar.Header{
			Name:     entry.name,
			Mode:     0o644,
			Size:     int64(len(entry.body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// ── Path traversal ───────────────────────────────────────────────────────────

// A zip whose entries climb out of the destination must be refused, not
// written. This is the regression test for zip slip: before the guard, the
// entry below landed wherever its ".." led.
func TestExtractZipRefusesPathTraversal(t *testing.T) {
	// The first component is stripped as an archive root, so the escape needs
	// one more level than it looks.
	src := writeZip(t, []zipFile{
		{"root/../../escaped.txt", "pwned"},
	})

	dest := t.TempDir()
	target := filepath.Join(dest, "app")

	err := Extract(context.Background(), src, "test.zip", dest, "app")
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected ErrUnsafePath, got %v", err)
	}

	// Nothing may have been written above the destination.
	outside := filepath.Join(filepath.Dir(dest), "escaped.txt")
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Errorf("file was written outside the destination: %s", outside)
	}
	if _, statErr := os.Stat(filepath.Join(target, "..", "escaped.txt")); statErr == nil {
		t.Error("file escaped into the destination's parent")
	}
}

func TestExtractTarRefusesPathTraversal(t *testing.T) {
	src := writeTarGz(t, []zipFile{
		{"root/../../escaped.txt", "pwned"},
	})

	dest := t.TempDir()
	err := Extract(context.Background(), src, "test.tar.gz", dest, "app")
	if !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("expected ErrUnsafePath, got %v", err)
	}

	outside := filepath.Join(filepath.Dir(dest), "escaped.txt")
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Errorf("file was written outside the destination: %s", outside)
	}
}

func TestSafeJoin(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "tmp", "dest")

	tests := []struct {
		name string
		rel  string
		ok   bool
	}{
		{"plain file", "bin/tool.exe", true},
		{"current dir", ".", true},
		{"parent escape", "../evil", false},
		{"nested escape", "a/b/../../../evil", false},
		{"absolute path", string(filepath.Separator) + "evil", false},
		{"sibling prefix", "../dest-evil/x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeJoin(base, tt.rel)
			if tt.ok && err != nil {
				t.Errorf("safeJoin(%q) = %v, want no error", tt.rel, err)
			}
			if !tt.ok && err == nil {
				t.Errorf("safeJoin(%q) accepted a path that escapes", tt.rel)
			}
		})
	}
}

// ── Normal extraction ────────────────────────────────────────────────────────

func TestExtractZipPicksTheShallowestExe(t *testing.T) {
	src := writeZip(t, []zipFile{
		{"pkg/docs/readme.md", "docs"},
		{"pkg/helpers/tool-helper.exe", "helper"},
		{"pkg/tool.exe", "main binary"},
	})

	dest := t.TempDir()
	if err := Extract(context.Background(), src, "test.zip", dest, "tool"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "tool.exe"))
	if err != nil {
		t.Fatalf("expected tool.exe in %s: %v", dest, err)
	}
	if string(got) != "main binary" {
		t.Errorf("extracted the wrong exe: %q", got)
	}
}

// Two executables at the same depth: the one named after the package wins, so
// the choice does not depend on map or sort order.
func TestExtractZipPrefersTheMatchingName(t *testing.T) {
	src := writeZip(t, []zipFile{
		{"pkg/aaa-updater.exe", "updater"},
		{"pkg/tool.exe", "main binary"},
	})

	dest := t.TempDir()
	if err := Extract(context.Background(), src, "test.zip", dest, "tool"); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(filepath.Join(dest, "tool.exe"))
	if string(got) != "main binary" {
		t.Errorf("expected the name-matched exe, got %q", got)
	}
}

// With no executable, the whole archive is kept under its own directory and
// the wrapping folder is stripped.
func TestExtractZipWithoutExeKeepsEverything(t *testing.T) {
	src := writeZip(t, []zipFile{
		{"pkg/share/data.txt", "data"},
		{"pkg/run.ps1", "script"},
	})

	dest := t.TempDir()
	if err := Extract(context.Background(), src, "test.zip", dest, "tool"); err != nil {
		t.Fatal(err)
	}

	for rel, want := range map[string]string{
		"tool/share/data.txt": "data",
		"tool/run.ps1":        "script",
	} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("missing %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}

func TestExtractTarGzPicksTheExe(t *testing.T) {
	src := writeTarGz(t, []zipFile{
		{"pkg/readme.md", "docs"},
		{"pkg/tool.exe", "main binary"},
	})

	dest := t.TempDir()
	if err := Extract(context.Background(), src, "test.tar.gz", dest, "tool"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dest, "tool.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "main binary" {
		t.Errorf("extracted %q", got)
	}
}

// A tar entry that is a symlink must not become an empty regular file, which
// is what writing every non-directory entry produced.
func TestExtractTarSkipsSymlinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, hdr := range []*tar.Header{
		{Name: "pkg/real.txt", Mode: 0o644, Size: 4, Typeflag: tar.TypeReg},
		{Name: "pkg/link.txt", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
	} {
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte("real")); err != nil {
				t.Fatal(err)
			}
		}
	}
	tw.Close()
	gz.Close()
	f.Close()

	dest := t.TempDir()
	if err := Extract(context.Background(), path, "links.tar.gz", dest, "tool"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dest, "tool", "link.txt")); err == nil {
		t.Error("symlink entry was materialised as a file")
	}
	if _, err := os.Stat(filepath.Join(dest, "tool", "real.txt")); err != nil {
		t.Errorf("regular entry missing: %v", err)
	}
}

func TestIsArchive(t *testing.T) {
	for name, want := range map[string]bool{
		"app.zip":       true,
		"app.tar.gz":    true,
		"app.TGZ":       true,
		"app.7z":        true,
		"app.exe":       false,
		"app.msi":       false,
		"checksums.txt": false,
	} {
		if got := IsArchive(name); got != want {
			t.Errorf("IsArchive(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestPickExeIsDeterministicForIdenticalNames(t *testing.T) {
	// Same basename at the same depth used to make the comparator answer
	// "less" in both directions, leaving the winner to the sort.
	candidates := []exeCandidate{
		{path: "b/tool.exe", depth: 1},
		{path: "a/tool.exe", depth: 1},
	}

	first, ok := pickExe(candidates, "tool.exe")
	if !ok {
		t.Fatal("no candidate picked")
	}
	for i := 0; i < 20; i++ {
		again, _ := pickExe(candidates, "tool.exe")
		if again.path != first.path {
			t.Fatalf("pick changed between runs: %q then %q", first.path, again.path)
		}
	}
	if !strings.HasSuffix(first.path, "tool.exe") {
		t.Errorf("unexpected pick %q", first.path)
	}
}
