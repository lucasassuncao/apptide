// Package archive unpacks the release archives apptide downloads.
//
// Every writing path here goes through safeJoin. An archive is untrusted
// input: a release built by someone else, or a repository that has changed
// hands since the config was written. An entry named "../../startup/evil.exe"
// would otherwise be written wherever the path led, because filepath.Join
// cleans a path but does not keep it inside its base.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUnsafePath reports an archive entry that would be written outside the
// destination directory.
var ErrUnsafePath = errors.New("archive entry escapes the destination directory")

// maxEntrySize caps a single extracted file at 2 GiB. A decompression bomb
// otherwise fills the disk before anything notices.
const maxEntrySize = 2 << 30

// safeJoin resolves rel against base and fails if the result leaves base.
func safeJoin(base, rel string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))

	// filepath.IsAbs is not enough on Windows: it wants a volume, so a rooted
	// entry like "\evil" and a drive-relative one like "C:evil" both slip past
	// it. Neither belongs in an archive, so both are refused outright.
	rooted := filepath.IsAbs(clean) ||
		(clean != "" && os.IsPathSeparator(clean[0])) ||
		filepath.VolumeName(clean) != ""
	escapes := clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator))

	if rooted || escapes {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}

	dest := filepath.Join(base, clean)
	// Compare against base plus a separator so that "/tmp/x-evil" is not
	// accepted as living under "/tmp/x".
	if dest != base && !strings.HasPrefix(dest, base+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	return dest, nil
}

// stripRoot removes the single top-level folder archives usually wrap their
// contents in, so files land directly in the destination.
func stripRoot(name string) string {
	rel := filepath.ToSlash(name)
	if idx := strings.Index(rel, "/"); idx >= 0 {
		return rel[idx+1:]
	}
	return rel
}

// Extract unpacks src into dir, choosing what to write from the archive's
// contents: when it holds an executable, only that one is written, named
// binaryName + ".exe". Otherwise the whole archive goes into dir/binaryName.
//
// assetName decides the format, because the temp file the download landed in
// has a generated name.
func Extract(ctx context.Context, src, assetName, dir, binaryName string) error {
	lower := strings.ToLower(assetName)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(src, dir, binaryName)
	case strings.HasSuffix(lower, ".7z"):
		return extract7z(ctx, src, dir, binaryName)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(src, dir, binaryName)
	default:
		return fmt.Errorf("unsupported archive format: %s", assetName)
	}
}

// IsArchive reports whether Extract knows the format of an asset name.
func IsArchive(assetName string) bool {
	lower := strings.ToLower(assetName)
	for _, ext := range []string{".tar.gz", ".tgz", ".7z", ".zip"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// ── Picking the executable ───────────────────────────────────────────────────

// exeCandidate is one .exe found inside an archive.
type exeCandidate struct {
	// path is the entry name inside the archive, or a real path for 7z.
	path string
	// depth is how deeply nested it is; a program's main binary sits shallower
	// than the helpers shipped beside it.
	depth int
}

// pickExe returns the entry most likely to be the program itself.
//
// Shallowest wins, then an exact match on the expected name, then
// alphabetical order so the choice is the same on every run.
func pickExe(candidates []exeCandidate, wantName string) (exeCandidate, bool) {
	if len(candidates) == 0 {
		return exeCandidate{}, false
	}

	want := strings.ToLower(wantName)
	rank := func(c exeCandidate) (int, int, string) {
		base := strings.ToLower(filepath.Base(filepath.FromSlash(c.path)))
		exact := 1
		if base == want {
			exact = 0
		}
		return c.depth, exact, base
	}

	// Stable and totally ordered: the previous comparator answered "less" in
	// both directions for two entries with the same basename, which left the
	// winner up to the sort implementation.
	sorted := make([]exeCandidate, len(candidates))
	copy(sorted, candidates)
	sort.SliceStable(sorted, func(i, j int) bool {
		di, ei, ni := rank(sorted[i])
		dj, ej, nj := rank(sorted[j])
		if di != dj {
			return di < dj
		}
		if ei != ej {
			return ei < ej
		}
		return ni < nj
	})
	return sorted[0], true
}

// copyLimited writes at most maxEntrySize bytes from r to path.
func copyLimited(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	out, err := os.Create(path) //#nosec G304 -- path is safeJoin'd against the destination above
	if err != nil {
		return err
	}
	written, err := io.Copy(out, io.LimitReader(r, maxEntrySize))
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	if written == maxEntrySize {
		os.Remove(path)
		return fmt.Errorf("archive entry %q exceeds the %d byte limit", filepath.Base(path), int64(maxEntrySize))
	}
	return nil
}

// ── zip ──────────────────────────────────────────────────────────────────────

func extractZip(src, dir, binaryName string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("opening zip: %w", err)
	}
	defer r.Close()

	var exes []exeCandidate
	for _, f := range r.File {
		if !f.FileInfo().IsDir() && strings.EqualFold(filepath.Ext(f.Name), ".exe") {
			exes = append(exes, exeCandidate{path: f.Name, depth: strings.Count(f.Name, "/")})
		}
	}

	best, ok := pickExe(exes, binaryName+".exe")
	if !ok {
		// No executable: keep the whole archive, under its own directory.
		return extractZipAll(r, filepath.Join(dir, binaryName))
	}

	for _, f := range r.File {
		if f.Name != best.path {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			return openErr
		}
		defer rc.Close()
		return copyLimited(filepath.Join(dir, binaryName+".exe"), rc)
	}
	return fmt.Errorf("exe entry not found in zip: %s", best.path)
}

func extractZipAll(r *zip.ReadCloser, destDir string) error {
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return err
	}
	for _, f := range r.File {
		rel := stripRoot(f.Name)
		if rel == "" {
			continue
		}
		dest, err := safeJoin(destDir, rel)
		if err != nil {
			return err
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o750); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = copyLimited(dest, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ── tar.gz ───────────────────────────────────────────────────────────────────

// extractTarGz reads the archive twice rather than holding it in memory.
//
// The first pass only needs entry names to decide what to keep; buffering
// every file to answer that question made a 500 MB release cost 500 MB of RAM,
// almost always to extract a single binary.
func extractTarGz(src, dir, binaryName string) error {
	var exes []exeCandidate
	err := walkTar(src, func(hdr *tar.Header, _ io.Reader) error {
		if hdr.Typeflag != tar.TypeDir && strings.EqualFold(filepath.Ext(hdr.Name), ".exe") {
			exes = append(exes, exeCandidate{path: hdr.Name, depth: strings.Count(hdr.Name, "/")})
		}
		return nil
	})
	if err != nil {
		return err
	}

	best, ok := pickExe(exes, binaryName+".exe")
	if !ok {
		return extractTarAll(src, filepath.Join(dir, binaryName))
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	found := false
	err = walkTar(src, func(hdr *tar.Header, r io.Reader) error {
		if hdr.Name != best.path {
			return nil
		}
		found = true
		return copyLimited(filepath.Join(dir, binaryName+".exe"), r)
	})
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("exe entry not found in tar: %s", best.path)
	}
	return nil
}

func extractTarAll(src, destDir string) error {
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return err
	}
	return walkTar(src, func(hdr *tar.Header, r io.Reader) error {
		rel := stripRoot(hdr.Name)
		if rel == "" {
			return nil
		}
		dest, err := safeJoin(destDir, rel)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(dest, 0o750)
		case tar.TypeReg:
			return copyLimited(dest, r)
		default:
			// Symlinks, hardlinks, devices and the rest are skipped: they were
			// silently written as empty files before, and a link is another way
			// to point at something outside the destination.
			return nil
		}
	})
}

// walkTar streams a .tar.gz, calling fn for each entry. The reader passed to
// fn is only valid for the duration of the call.
func walkTar(src string, fn func(*tar.Header, io.Reader) error) error {
	f, err := os.Open(src) //#nosec G304 -- src is the temp file apptide downloaded the asset to
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
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if err := fn(hdr, tr); err != nil {
			return err
		}
	}
}

// ── 7z ───────────────────────────────────────────────────────────────────────

func extract7z(ctx context.Context, src, dir, binaryName string) error {
	sevenZip, err := find7z()
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "apptide-7z-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// -o<dir> extracts there, -y answers every prompt, and 7z refuses entries
	// that would escape the output directory on its own.
	cmd := exec.CommandContext(ctx, sevenZip, "x", src, "-o"+tmp, "-y") //#nosec G204 -- sevenZip comes from exec.LookPath, the rest are literals
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("7z extraction failed: %w: %s", runErr, tail(string(out)))
	}

	var exes []exeCandidate
	_ = filepath.Walk(tmp, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // an unreadable entry is simply not a candidate
		}
		if strings.EqualFold(filepath.Ext(p), ".exe") {
			rel, _ := filepath.Rel(tmp, p)
			exes = append(exes, exeCandidate{path: p, depth: strings.Count(rel, string(filepath.Separator))})
		}
		return nil
	})

	best, ok := pickExe(exes, binaryName+".exe")
	if !ok {
		return copyDir(tmp, filepath.Join(dir, binaryName))
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	return CopyFile(best.path, filepath.Join(dir, binaryName+".exe"))
}

func find7z() (string, error) {
	for _, name := range []string{"7z", "7za"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("7z not found in PATH: install 7-Zip to extract .7z archives")
}

// tail returns the last few lines of command output, for an error message.
func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.TrimSpace(strings.Join(lines, "; "))
}

// ── File helpers ─────────────────────────────────────────────────────────────

// CopyFile copies src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(src) //#nosec G304 -- src comes from an archive already extracted to our temp dir
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst) //#nosec G304 -- dst is safeJoin'd against the destination by the caller
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

// copyDir recursively copies src into dst.
//
// Only regular files are copied. A .7z can carry symlinks, and 7z materialises
// them in the temp directory before this runs; following one would copy
// whatever it points at into the install directory. The zip and tar paths skip
// links for the same reason, and this one has to agree with them.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target, err := safeJoin(dst, rel)
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o750) //#nosec G122 -- target is safeJoin'd, only regular files are copied, and the source is a temp dir apptide created
		case info.Mode().IsRegular():
			return CopyFile(p, target)
		default:
			// Symlinks, devices and pipes.
			return nil
		}
	})
}
