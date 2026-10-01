// Package inventory takes one snapshot of everything the package managers have
// installed, so callers can answer "is this installed, and at what version?"
// without paying a process launch per application.
//
// installer.Check shells out once per application, which is fine for a
// sequential install run but not for a browsing UI: 60 applications across
// three managers would mean minutes of waiting before the first frame. Each
// manager here is queried exactly once, in parallel.
package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/lucasassuncao/apptide/internal/config"
)

// ErrManagerMissing means the package manager is not installed on this
// machine. It is not a failure: most machines have only some of the three, and
// the UI stays quiet about the ones that are simply absent.
var ErrManagerMissing = errors.New("not installed")

// wingetMu serializes winget invocations.
//
// winget does not tolerate two of its own commands at once: running
// `winget export` and `winget upgrade` together makes one of them fail, either
// with an empty error or with 0x8a150001. The failure is intermittent and
// silent — the affected source simply reports nothing — so a run would show
// three upgrades instead of eighteen with no indication why.
//
// The other managers have no such problem and still run in parallel.
var wingetMu sync.Mutex

// Entry is one installed package as the manager reports it.
type Entry struct {
	ID      string
	Version string
	// Failed is set when the manager itself records the install as broken.
	// Scoop keeps such apps in its list with an empty version and an
	// "Install failed" note; reporting those as installed would tell the user
	// everything is fine while the binary is missing.
	Failed bool
}

// Snapshot holds the installed packages of every reachable manager.
// The zero value is not usable; call Installed.
type Snapshot struct {
	bySource map[config.Source]map[string]Entry
	errs     map[config.Source]error
}

// queryAll runs every loader in parallel and indexes what they return by
// lowercased id.
//
// A manager that is missing or fails does not fail the query: its error is
// kept per source, so the UI can say "scoop not in PATH" instead of showing
// every scoop application as missing.
func queryAll[T any](
	ctx context.Context,
	loaders map[config.Source]func(context.Context) ([]T, error),
	id func(T) string,
) (map[config.Source]map[string]T, map[config.Source]error) {
	type result struct {
		source config.Source
		items  []T
		err    error
	}

	results := make(chan result, len(loaders))
	var wg sync.WaitGroup
	for src, load := range loaders {
		wg.Add(1)
		go func(src config.Source, load func(context.Context) ([]T, error)) {
			defer wg.Done()
			items, err := load(ctx)
			results <- result{source: src, items: items, err: err}
		}(src, load)
	}
	wg.Wait()
	close(results)

	bySource := make(map[config.Source]map[string]T, len(loaders))
	errs := make(map[config.Source]error, len(loaders))
	for r := range results {
		if r.err != nil {
			errs[r.source] = r.err
			continue
		}
		index := make(map[string]T, len(r.items))
		for _, item := range r.items {
			index[strings.ToLower(id(item))] = item
		}
		bySource[r.source] = index
	}
	return bySource, errs
}

// Installed queries every manager in parallel and returns the combined result.
func Installed(ctx context.Context) *Snapshot {
	bySource, errs := queryAll(ctx, map[config.Source]func(context.Context) ([]Entry, error){
		config.SourceWinget:     wingetInstalled,
		config.SourceScoop:      scoopInstalled,
		config.SourceChocolatey: chocolateyInstalled,
	}, func(e Entry) string { return e.ID })

	return &Snapshot{bySource: bySource, errs: errs}
}

// NewSnapshot builds a snapshot from listings already in hand, keyed by source.
//
// Installed is the normal way to get one. This exists for callers that got the
// listing elsewhere — chiefly tests, which must not launch a package manager to
// exercise the code that reads a snapshot. A source present in entries counts as
// reachable, even with an empty list.
func NewSnapshot(entries map[config.Source][]Entry) *Snapshot {
	snap := &Snapshot{
		bySource: make(map[config.Source]map[string]Entry, len(entries)),
		errs:     make(map[config.Source]error),
	}
	for src, list := range entries {
		index := make(map[string]Entry, len(list))
		for _, e := range list {
			index[strings.ToLower(e.ID)] = e
		}
		snap.bySource[src.Normalize()] = index
	}
	return snap
}

// Lookup reports whether src has id installed, and at which version.
func (s *Snapshot) Lookup(src config.Source, id string) (Entry, bool) {
	index, ok := s.bySource[src.Normalize()]
	if !ok {
		return Entry{}, false
	}
	e, ok := index[strings.ToLower(id)]
	return e, ok
}

// Available reports whether src could be queried at all.
func (s *Snapshot) Available(src config.Source) bool {
	_, ok := s.bySource[src.Normalize()]
	return ok
}

// Err returns why src could not be queried, if it could not.
func (s *Snapshot) Err(src config.Source) error { return s.errs[src.Normalize()] }

// Sources lists the managers that answered, in canonical order.
func (s *Snapshot) Sources() []config.Source {
	var out []config.Source
	for _, src := range config.AllSources() {
		if s.Available(src) {
			out = append(out, src)
		}
	}
	return out
}

// All returns every package src reports as installed.
func (s *Snapshot) All(src config.Source) []Entry {
	index := s.bySource[src.Normalize()]
	out := make([]Entry, 0, len(index))
	for _, e := range index {
		out = append(out, e)
	}
	return out
}

// Count returns how many packages src reports as installed.
func (s *Snapshot) Count(src config.Source) int { return len(s.bySource[src.Normalize()]) }

// ── winget ───────────────────────────────────────────────────────────────────

type wingetExportFile struct {
	Sources []struct {
		Packages []struct {
			PackageIdentifier string `json:"PackageIdentifier"`
			Version           string `json:"Version"`
		} `json:"Packages"`
	} `json:"Sources"`
}

// wingetInstalled uses `winget export`, whose JSON is stable, rather than
// parsing the localized column layout of `winget list`.
func wingetInstalled(ctx context.Context) ([]Entry, error) {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil, fmt.Errorf("winget %w", ErrManagerMissing)
	}

	wingetMu.Lock()
	defer wingetMu.Unlock()

	tmp, err := os.CreateTemp("", "apptide-winget-*.json")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	// --include-versions is required: without it winget writes identifiers
	// only, and every package looks like it has no version installed.
	//#nosec G204 -- fixed program and flags; the only variable is our own temp file
	out, err := exec.CommandContext(ctx, "winget", "export",
		"--output", tmp.Name(), "--include-versions",
		"--accept-source-agreements").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("winget export: %s", strings.TrimSpace(string(out)))
	}

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return nil, err
	}

	var export wingetExportFile
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("parsing winget export JSON: %w", err)
	}

	var result []Entry
	for _, src := range export.Sources {
		for _, pkg := range src.Packages {
			result = append(result, Entry{ID: pkg.PackageIdentifier, Version: pkg.Version})
		}
	}
	return result, nil
}

// ── scoop ────────────────────────────────────────────────────────────────────

type scoopExportFile struct {
	Apps []struct {
		Name    string `json:"Name"`
		Version string `json:"Version"`
		Info    string `json:"Info"`
	} `json:"apps"`
}

// ScoopInstalled lists what scoop has, including apps whose install failed —
// those are flagged rather than dropped, so the UI can show the breakage
// instead of silently pretending the app is absent.
func ScoopInstalled(ctx context.Context) ([]Entry, error) { return scoopInstalled(ctx) }

func scoopInstalled(ctx context.Context) ([]Entry, error) {
	if _, err := exec.LookPath("scoop"); err != nil {
		return nil, fmt.Errorf("scoop %w", ErrManagerMissing)
	}

	out, err := exec.CommandContext(ctx, "scoop", "export").Output()
	if err != nil {
		return nil, fmt.Errorf("scoop export: %w", err)
	}

	var export scoopExportFile
	if err := json.Unmarshal(out, &export); err != nil {
		return nil, fmt.Errorf("parsing scoop export JSON: %w", err)
	}

	result := make([]Entry, 0, len(export.Apps))
	for _, app := range export.Apps {
		result = append(result, Entry{
			ID:      app.Name,
			Version: app.Version,
			Failed:  strings.Contains(strings.ToLower(app.Info), "failed"),
		})
	}
	return result, nil
}

// ── chocolatey ───────────────────────────────────────────────────────────────

func chocolateyInstalled(ctx context.Context) ([]Entry, error) {
	if _, err := exec.LookPath("choco"); err != nil {
		return nil, fmt.Errorf("choco %w", ErrManagerMissing)
	}

	// --limit-output: pipe-separated "id|version" lines, no headers.
	// --local-only is deliberately absent: Chocolatey 2.x lists local packages
	// by default and removed the flag from this command.
	out, err := exec.CommandContext(ctx, "choco", "list", "--limit-output").Output()
	if err != nil {
		return nil, fmt.Errorf("choco list: %w", err)
	}

	var result []Entry
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		e := Entry{ID: parts[0]}
		if len(parts) == 2 {
			e.Version = parts[1]
		}
		result = append(result, e)
	}
	return result, nil
}
