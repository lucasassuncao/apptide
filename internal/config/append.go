package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AppendApplications adds apps to the `applications:` block of the config at
// path and writes the file back.
//
// The file is edited as text rather than decoded and re-encoded: a round trip
// through the YAML marshaller would drop every comment and reflow the whole
// document. Here only the inserted lines are new; every other byte is copied
// through untouched.
//
// When several files are chained with `import:`, the entries land in the file
// given by path — the caller decides which one that is.
func AppendApplications(path string, apps []Application) error {
	if len(apps) == 0 {
		return nil
	}

	data, err := os.ReadFile(path) //#nosec G304 -- the config path is the user's own choice
	if err != nil {
		return fmt.Errorf("reading config %q: %w", path, err)
	}

	block, err := marshalApplications(apps)
	if err != nil {
		return err
	}

	updated := insertApplications(string(data), block)

	return writeAtomic(path, []byte(updated))
}

// marshalApplications renders apps as the body of an applications list,
// indented two spaces so it nests under the key.
func marshalApplications(apps []Application) ([]string, error) {
	// Two-space indent, matching what apptide writes everywhere else; the
	// package default of four would leave the appended entries looking foreign
	// next to the ones already in the file.
	var buf strings.Builder
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(apps); err != nil {
		return nil, fmt.Errorf("encoding applications: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding applications: %w", err)
	}

	var lines []string
	for _, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if l == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+l)
	}
	return lines, nil
}

// insertApplications splices block into the applications section of doc.
// It cannot fail: every shape of input has a defined outcome, including a
// document with no applications key at all.
func insertApplications(doc string, block []string) string {
	newline := "\n"
	if strings.Contains(doc, "\r\n") {
		newline = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")

	keyAt := -1
	for i, l := range lines {
		if l == "applications:" || strings.HasPrefix(l, "applications:") && !strings.HasPrefix(l, "applications:-") {
			if rest := strings.TrimSpace(strings.TrimPrefix(l, "applications:")); rest == "" || rest == "[]" {
				keyAt = i
				lines[i] = "applications:" // drop an empty [] so the list can grow
				break
			}
		}
	}

	// No applications block yet: start one at the end of the file.
	if keyAt < 0 {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, "", "applications:")
		lines = append(lines, block...)
		return strings.Join(lines, newline) + newline
	}

	// The block ends at the last indented line under the key; a line starting
	// in column zero is the next top-level key (or a comment that introduces
	// it), and the entries belong before it.
	last := keyAt
	for i := keyAt + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if l[0] == ' ' || l[0] == '\t' {
			last = i
			continue
		}
		break
	}

	insertAt := last + 1
	merged := make([]string, 0, len(lines)+len(block))
	merged = append(merged, lines[:insertAt]...)
	merged = append(merged, block...)
	merged = append(merged, lines[insertAt:]...)

	out := strings.Join(merged, newline)
	if !strings.HasSuffix(out, newline) {
		out += newline
	}
	return out
}

// writeAtomic replaces path via a temp file in the same directory, so an
// interrupted write cannot leave the user with a truncated config.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".apptide-*.yaml")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %q: %w", path, err)
	}
	return nil
}
