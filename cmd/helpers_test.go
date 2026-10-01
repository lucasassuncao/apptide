package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// captureCmdStdout runs fn with os.Stdout replaced by a pipe and returns what it
// wrote. The commands print straight to os.Stdout, which is what these tests
// need to inspect.
func captureCmdStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	w.Close()
	return <-done
}

// useConfig writes body to a temp packages.yaml and points configPath at it
// until the test ends.
func useConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packages.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := configPath
	configPath = path
	t.Cleanup(func() { configPath = prev })
	return path
}

// sampleConfig declares applications in two categories, out of name order.
const sampleConfig = `schema_version: 2
applications:
  - name: "Neovim"
    category: Development
    source: [winget, scoop]
    tags: [editor]
    package:
      winget:
        id: Neovim.Neovim
      scoop:
        id: neovim
  - name: "Git"
    category: Development
    source: winget
    package:
      winget:
        id: Git.Git
  - name: "7-Zip"
    category: Utilities
    source: winget
    action: skip
    package:
      winget:
        id: 7zip.7zip
`
