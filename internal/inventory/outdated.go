package inventory

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/lucasassuncao/apptide/internal/config"
)

// Upgrade is one package with a newer version available.
type Upgrade struct {
	ID        string
	Current   string
	Available string
	Pinned    bool
}

// Upgrades is what every reachable manager reports as outdated.
type Upgrades struct {
	bySource map[config.Source]map[string]Upgrade
	errs     map[config.Source]error
}

// Outdated asks every manager what it could upgrade.
//
// This is slower than Installed — winget and scoop go to the network — so it is
// loaded separately from the installed snapshot and the UI stays usable while
// it runs.
func Outdated(ctx context.Context) *Upgrades {
	bySource, errs := queryAll(ctx, map[config.Source]func(context.Context) ([]Upgrade, error){
		config.SourceWinget:     wingetOutdated,
		config.SourceScoop:      scoopOutdated,
		config.SourceChocolatey: chocolateyOutdated,
	}, func(u Upgrade) string { return u.ID })

	return &Upgrades{bySource: bySource, errs: errs}
}

// Lookup reports whether src has an upgrade for id.
func (u *Upgrades) Lookup(src config.Source, id string) (Upgrade, bool) {
	index, ok := u.bySource[src.Normalize()]
	if !ok {
		return Upgrade{}, false
	}
	up, ok := index[strings.ToLower(id)]
	return up, ok
}

// All returns every upgrade src offers.
func (u *Upgrades) All(src config.Source) []Upgrade {
	index := u.bySource[src.Normalize()]
	out := make([]Upgrade, 0, len(index))
	for _, up := range index {
		out = append(out, up)
	}
	return out
}

// Sources lists the managers that answered, in canonical order.
func (u *Upgrades) Sources() []config.Source {
	var out []config.Source
	for _, src := range config.AllSources() {
		if _, ok := u.bySource[src.Normalize()]; ok {
			out = append(out, src)
		}
	}
	return out
}

// Err returns why src could not be queried, if it could not.
func (u *Upgrades) Err(src config.Source) error { return u.errs[src.Normalize()] }

// Total counts every upgrade found.
func (u *Upgrades) Total() int {
	n := 0
	for _, index := range u.bySource {
		n += len(index)
	}
	return n
}

// ── chocolatey ───────────────────────────────────────────────────────────────

// chocolateyOutdated reads `choco outdated -r`, which is the one machine
// readable format of the three: "id|current|available|pinned".
func chocolateyOutdated(ctx context.Context) ([]Upgrade, error) {
	if _, err := exec.LookPath("choco"); err != nil {
		return nil, fmt.Errorf("choco %w", ErrManagerMissing)
	}

	out, err := exec.CommandContext(ctx, "choco", "outdated", "-r").Output()
	if err != nil {
		return nil, fmt.Errorf("choco outdated: %w", err)
	}

	var ups []Upgrade
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 3 || parts[0] == "" {
			continue
		}
		ups = append(ups, Upgrade{
			ID:        parts[0],
			Current:   parts[1],
			Available: parts[2],
			Pinned:    len(parts) > 3 && strings.EqualFold(parts[3], "true"),
		})
	}
	return ups, nil
}

// ── winget ───────────────────────────────────────────────────────────────────

// wingetOutdated parses the table `winget upgrade` prints.
//
// There is no machine-readable form, and the column titles are localized, so
// the columns are located by where the header words start rather than by what
// they say. Splitting on whitespace cannot work: names contain spaces and
// "Unknown" versions leave cells that shift the fields.
func wingetOutdated(ctx context.Context) ([]Upgrade, error) {
	if _, err := exec.LookPath("winget"); err != nil {
		return nil, fmt.Errorf("winget %w", ErrManagerMissing)
	}

	// See wingetMu: winget cannot run two of its own commands at once.
	wingetMu.Lock()
	defer wingetMu.Unlock()

	out, err := exec.CommandContext(ctx, "winget", "upgrade",
		"--include-unknown", "--accept-source-agreements").Output()
	if err != nil {
		// winget exits non-zero when nothing is upgradable.
		if len(out) == 0 {
			return nil, fmt.Errorf("winget upgrade: %w", err)
		}
	}

	lines := splitLines(string(out))
	headerIdx := findHeaderAbove(lines, '-')
	if headerIdx < 0 {
		return nil, nil // no table means nothing to upgrade
	}

	starts := columnStarts(lines[headerIdx])
	if len(starts) < 4 {
		return nil, fmt.Errorf("unexpected winget table layout")
	}

	var ups []Upgrade
	for _, line := range lines[headerIdx+2:] {
		if strings.TrimSpace(line) == "" {
			break // the table ends; anything after is a summary
		}
		cells := sliceColumns(line, starts)
		if len(cells) < 4 {
			continue
		}
		id, current, available := cells[1], cells[2], cells[3]
		// A trailing summary line ("N upgrades available.") has no id column.
		if id == "" || strings.ContainsAny(id, " ") || available == "" {
			continue
		}
		ups = append(ups, Upgrade{ID: id, Current: current, Available: available})
	}
	return ups, nil
}

// ── scoop ────────────────────────────────────────────────────────────────────

// scoopOutdated parses `scoop status`, whose dashes row marks the column
// boundaries exactly — an app with a failed install leaves both version cells
// empty, so whitespace splitting would read its note as a version.
func scoopOutdated(ctx context.Context) ([]Upgrade, error) {
	if _, err := exec.LookPath("scoop"); err != nil {
		return nil, fmt.Errorf("scoop %w", ErrManagerMissing)
	}

	out, err := exec.CommandContext(ctx, "scoop", "status").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("scoop status: %w", err)
	}

	lines := splitLines(ansi.Strip(string(out)))
	dashesIdx := findDashesRow(lines)
	if dashesIdx < 1 {
		return nil, nil // "Everything is up to date" and nothing else
	}

	starts := dashGroupStarts(lines[dashesIdx])
	if len(starts) < 3 {
		return nil, nil
	}

	var ups []Upgrade
	for _, line := range lines[dashesIdx+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells := sliceColumns(line, starts)
		if len(cells) < 3 {
			continue
		}
		name, current, latest := cells[0], cells[1], cells[2]
		if name == "" || latest == "" || current == "" || current == latest {
			continue
		}
		ups = append(ups, Upgrade{ID: name, Current: current, Available: latest})
	}
	return ups, nil
}

// ── table helpers ────────────────────────────────────────────────────────────

func splitLines(s string) []string {
	return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// findHeaderAbove returns the index of the line above the first row made
// entirely of the given rune.
func findHeaderAbove(lines []string, dash rune) int {
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) < 8 || strings.TrimLeft(t, string(dash)) != "" {
			continue
		}
		if i == 0 {
			return -1
		}
		return i - 1
	}
	return -1
}

// findDashesRow returns the index of a row made of dash groups.
func findDashesRow(lines []string) int {
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) < 4 {
			continue
		}
		if strings.Trim(t, "- ") == "" {
			return i
		}
	}
	return -1
}

// columnStarts locates each column by where its header word begins, splitting
// on runs of two or more spaces. Offsets are in runes, since localized headers
// contain multibyte characters.
func columnStarts(header string) []int {
	runes := []rune(header)
	var starts []int
	inGap := true
	for i, r := range runes {
		if r == ' ' {
			// Two spaces in a row end a column.
			if i+1 < len(runes) && runes[i+1] == ' ' {
				inGap = true
			}
			continue
		}
		if inGap {
			starts = append(starts, i)
			inGap = false
		}
	}
	return starts
}

// dashGroupStarts locates each column by where its run of dashes begins.
func dashGroupStarts(row string) []int {
	runes := []rune(row)
	var starts []int
	inGap := true
	for i, r := range runes {
		switch r {
		case '-':
			if inGap {
				starts = append(starts, i)
				inGap = false
			}
		default:
			inGap = true
		}
	}
	return starts
}

// sliceColumns cuts a data line at the given rune offsets.
func sliceColumns(line string, starts []int) []string {
	runes := []rune(line)
	cells := make([]string, 0, len(starts))
	for i, start := range starts {
		if start >= len(runes) {
			cells = append(cells, "")
			continue
		}
		end := len(runes)
		if i+1 < len(starts) && starts[i+1] < end {
			end = starts[i+1]
		}
		cells = append(cells, strings.TrimSpace(string(runes[start:end])))
	}
	return cells
}
