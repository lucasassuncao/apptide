// Package state persists which package source actually installed each
// application.
//
// The config file declares which sources are acceptable and in what order
// they should be tried; this package records what actually happened. Once an
// application is bound to a source, upgrades and removals always go through
// that same manager — the preference order in the config applies to the first
// installation only. Re-resolving it on every run would mean uninstalling and
// reinstalling an application because a third-party catalog changed, which is
// never something the user asked for.
//
// The state lives in a small, human-readable JSON file so it can be inspected,
// hand-edited, and attached to bug reports. Writes are atomic (temp file plus
// rename) and can be serialized across processes with Lock.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Version is the current on-disk schema version of the state file.
const Version = 1

// ErrNotFound is returned when an operation targets an application that has no
// entry in the state.
var ErrNotFound = errors.New("no state entry for application")

// CorruptError reports a state file that exists but cannot be decoded.
// Use LoadOrReset to quarantine it and continue with an empty state.
type CorruptError struct {
	Path string
	Err  error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("state file %q is corrupt: %v", e.Path, e.Err)
}

func (e *CorruptError) Unwrap() error { return e.Err }

// Result is the outcome of trying to install an application from one source.
type Result string

const (
	// ResultOK means the source installed the application successfully.
	ResultOK Result = "ok"
	// ResultNotFound means the source does not offer this package at all,
	// as opposed to offering it and failing to install it.
	ResultNotFound Result = "not_found"
	// ResultUnavailable means the package manager itself is missing or broken.
	ResultUnavailable Result = "unavailable"
	// ResultFailed means the package exists but the install failed.
	ResultFailed Result = "failed"
)

// Attempt records one source tried during installation. The list is kept so a
// later run can explain why the bound source is not the first preference
// instead of silently retrying a source that has already failed.
type Attempt struct {
	Source string `json:"source"`
	Result Result `json:"result"`
	Detail string `json:"detail,omitempty"`
}

// Entry is the recorded state of a single application.
type Entry struct {
	// InstalledVia is the source that owns this application: the one used for
	// upgrades, removal, and version checks.
	InstalledVia string `json:"installed_via"`
	// PackageID is the identifier InstalledVia knows the package by
	// (a winget/choco/scoop id, or "owner/repo" for github).
	PackageID string `json:"package_id,omitempty"`
	// Version is the version recorded at install time, when detectable.
	Version string `json:"version,omitempty"`
	// InstallDir and Binary are set for sources that place files directly
	// (github), enabling removal without a package manager.
	InstallDir string `json:"install_dir,omitempty"`
	Binary     string `json:"binary,omitempty"`

	InstalledAt time.Time `json:"installed_at"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`

	Attempts []Attempt `json:"attempts,omitempty"`
}

// State is an in-memory view of the state file. It is safe for concurrent use
// within a process; use Lock to serialize across processes.
type State struct {
	Version int              `json:"version"`
	Apps    map[string]Entry `json:"apps"`

	mu   sync.Mutex
	path string
}

// DefaultPath returns the standard state file location,
// %LOCALAPPDATA%\apptide\state.json.
func DefaultPath() string {
	local := os.Getenv("LOCALAPPDATA")
	if local == "" {
		local = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(local, "apptide", "state.json")
}

// New returns an empty state that will be written to path on Save.
func New(path string) *State {
	return &State{Version: Version, Apps: make(map[string]Entry), path: path}
}

// Key normalizes an application name into the key used in the state file:
// lowercased, trimmed, with internal whitespace collapsed to single hyphens.
// All lookups and mutations normalize their argument, so callers can pass the
// display name directly.
//
//	Key("VS Code")  == "vs-code"
//	Key("  GitHub   CLI ") == "github-cli"
func Key(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), "-")
}

// Load reads the state file at path.
//
// A missing file is not an error: it yields an empty state, since the first
// run of any installation legitimately has no state yet. A file that exists
// but cannot be decoded returns *CorruptError — losing the source bindings
// silently would let a later run adopt the wrong package manager.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path) //#nosec G304 -- the state file path is apptide's own, or the user's --state
	if errors.Is(err, os.ErrNotExist) {
		return New(path), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state %q: %w", path, err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, &CorruptError{Path: path, Err: err}
	}
	if s.Version > Version {
		return nil, fmt.Errorf(
			"state %q has schema version %d, this apptide understands up to %d — upgrade apptide",
			path, s.Version, Version)
	}
	if s.Apps == nil {
		s.Apps = make(map[string]Entry)
	}
	s.path = path
	s.Version = Version // older versions are migrated in place as they load
	return &s, nil
}

// LoadOrReset behaves like Load, but on a corrupt file it renames the bad file
// aside and returns an empty state plus the quarantine path. Detection can
// rebuild the bindings from the machine, so a damaged file should not stop the
// user from installing anything.
func LoadOrReset(path string) (s *State, quarantined string, err error) {
	s, err = Load(path)
	var corrupt *CorruptError
	if !errors.As(err, &corrupt) {
		return s, "", err
	}

	quarantined = fmt.Sprintf("%s.corrupt.%d", path, time.Now().Unix())
	if renameErr := os.Rename(path, quarantined); renameErr != nil {
		return nil, "", fmt.Errorf("quarantining corrupt state %q: %w", path, renameErr)
	}
	return New(path), quarantined, nil
}

// Path returns the file this state loads from and saves to.
func (s *State) Path() string { return s.path }

// Get returns the entry bound to name, if any.
func (s *State) Get(name string) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.Apps[Key(name)]
	return e, ok
}

// Bind records that name is now managed by the source in e.InstalledVia,
// replacing any previous binding. InstalledAt is preserved across re-binds so
// it keeps meaning "first installed by apptide"; UpdatedAt always advances.
func (s *State) Bind(name string, e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	key := Key(name)
	if prev, ok := s.Apps[key]; ok && !prev.InstalledAt.IsZero() {
		e.InstalledAt = prev.InstalledAt
	} else if e.InstalledAt.IsZero() {
		e.InstalledAt = now
	}
	e.UpdatedAt = now
	s.Apps[key] = e
}

// Adopt moves an existing application to a different source without touching
// the machine. It is the explicit counterpart to the rule that a binding is
// never migrated automatically: the caller is responsible for any reinstall.
func (s *State) Adopt(name, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := Key(name)
	e, ok := s.Apps[key]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	e.InstalledVia = source
	e.UpdatedAt = time.Now().UTC()
	s.Apps[key] = e
	return nil
}

// Remove drops the entry for name, reporting whether one existed.
func (s *State) Remove(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := Key(name)
	_, ok := s.Apps[key]
	delete(s.Apps, key)
	return ok
}

// Keys returns the recorded application keys in sorted order.
func (s *State) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.Apps))
	for k := range s.Apps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Len returns the number of recorded applications.
func (s *State) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Apps)
}

// Save writes the state atomically: a temp file in the same directory is
// written, flushed, and renamed over the target. A crash mid-write therefore
// leaves the previous state intact rather than a truncated file.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.path == "" {
		return errors.New("state has no path — use Load or New")
	}
	s.Version = Version

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("creating state dir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("creating temp state file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp state file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("flushing temp state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp state file: %w", err)
	}
	// os.Rename maps to MoveFileEx with MOVEFILE_REPLACE_EXISTING on Windows,
	// so this replaces an existing file atomically.
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replacing state %q: %w", s.path, err)
	}
	return nil
}
