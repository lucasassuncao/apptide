package runner

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
)

// ── Row types ─────────────────────────────────────────────────────────────────

type rowStatus uint8

const (
	statusPending rowStatus = iota
	statusOK
	statusUpToDate
	statusAlreadyInstalled
	statusSkipped
	statusFailed
)

type tableRow struct {
	pkg         config.Package
	category    string
	status      rowStatus
	detail      string
	newVersion  string
	prevVersion string
}

// ── Bubbletea model ───────────────────────────────────────────────────────────

type model struct {
	rows              []tableRow
	current           int
	instOpts          installer.Options
	dryRun            bool
	nTotal            int
	nOK               int
	nSkip             int
	nFailed           int
	installedBinaries bool
	quitting          bool
	ctx               context.Context //nolint:containedctx
	cancel            context.CancelFunc
}

func (m model) Init() tea.Cmd {
	if len(m.rows) == 0 {
		return tea.Quit
	}
	calcWidths(m.rows)
	return tea.Sequence(
		tea.Println(renderHeader(m.dryRun)),
		runRow(m.ctx, 0, m.rows[0], m.instOpts, m.dryRun),
	)
}

type pkgResult struct {
	index           int
	status          rowStatus
	detail          string
	installedBinary bool
	newVersion      string
	prevVersion     string
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
		m.cancel()
		m.quitting = true
		return m, tea.Quit
	}

	res, ok := msg.(pkgResult)
	if !ok {
		return m, nil
	}

	m.rows[res.index].status = res.status
	m.rows[res.index].detail = res.detail
	m.rows[res.index].newVersion = res.newVersion
	m.rows[res.index].prevVersion = res.prevVersion
	m.nTotal++
	switch res.status {
	case statusOK, statusUpToDate, statusAlreadyInstalled:
		m.nOK++
	case statusSkipped:
		m.nSkip++
	case statusFailed:
		m.nFailed++
	}
	if res.installedBinary {
		m.installedBinaries = true
	}
	m.current++

	// Always print the row for this result FIRST, then proceed
	printCmd := tea.Println(renderRow(m.rows[res.index]))

	// Check if all rows are processed
	if m.current >= len(m.rows) {
		m.quitting = true
		// Use Sequence to ensure print happens before summary and quit
		return m, tea.Sequence(printCmd, tea.Println(renderSummary(m)), tea.Quit)
	} else {
		// Use Sequence: print result first, THEN process next row
		return m, tea.Sequence(printCmd, runRow(m.ctx, m.current, m.rows[m.current], m.instOpts, m.dryRun))
	}
}

func (m model) View() string {
	if m.quitting || m.current >= len(m.rows) {
		return ""
	}
	r := m.rows[m.current]
	src := strings.ToLower(r.pkg.Source)
	action := strings.ToLower(r.pkg.Action)
	var verb string
	switch {
	case action == "skip":
		verb = lgGray.Render("skip")
	case m.dryRun:
		verb = lgYellow.Render("dry-run")
	case action == "uninstall":
		verb = lgCyan.Render("uninstalling...")
	case src == installer.SourceGitHub || src == installer.SourceThirdParty:
		verb = lgCyan.Render("downloading...")
	default:
		verb = lgCyan.Render("installing...")
	}
	return fmtRow(r.pkg.Name, r.category, displayMethod(r.pkg.Source), verb) + "\n"
}

// ── tea.Cmd ───────────────────────────────────────────────────────────────────

func runRow(ctx context.Context, idx int, r tableRow, opts installer.Options, dryRun bool) tea.Cmd {
	return func() tea.Msg {
		return doInstall(ctx, idx, r, opts, dryRun)
	}
}

func doInstall(ctx context.Context, idx int, r tableRow, opts installer.Options, dryRun bool) pkgResult {
	action := strings.ToLower(r.pkg.Action)
	if action == "" {
		action = "install"
	}
	if action == "skip" {
		return pkgResult{index: idx, status: statusSkipped}
	}

	inst, err := installer.Resolve(r.pkg.Source, opts)
	if err != nil {
		return pkgResult{index: idx, status: statusFailed, detail: err.Error()}
	}
	if !inst.IsAvailable() {
		return pkgResult{index: idx, status: statusFailed, detail: inst.Name() + " not found in PATH"}
	}
	if dryRun {
		return pkgResult{index: idx, status: statusOK}
	}

	// Capture the version currently installed (may be empty if not detectable).
	_, prevVersion := inst.Check(r.pkg)

	// Run pre_install hook — failure aborts the package action.
	if r.pkg.PreInstall != "" {
		if herr := runHook(r.pkg.PreInstall); herr != nil {
			return pkgResult{
				index: idx, status: statusFailed,
				detail: "pre-hook: " + herr.Error(),
			}
		}
	}

	if action == "uninstall" {
		err = inst.Uninstall(ctx, r.pkg)
	} else {
		err = inst.Install(ctx, r.pkg)
	}

	src := strings.ToLower(r.pkg.Source)
	isBinary := src == installer.SourceGitHub || src == installer.SourceThirdParty

	if err == nil {
		_, newVersion := inst.Check(r.pkg)
		// Run post_install hook — failure is a warning, not a hard failure.
		if r.pkg.PostInstall != "" {
			if herr := runHook(r.pkg.PostInstall); herr != nil {
				return pkgResult{
					index: idx, status: statusOK,
					detail:          "post-hook: " + herr.Error(),
					installedBinary: isBinary,
					newVersion:      newVersion,
					prevVersion:     prevVersion,
				}
			}
		}
		return pkgResult{
			index: idx, status: statusOK, installedBinary: isBinary,
			newVersion:  newVersion,
			prevVersion: prevVersion,
		}
	}
	if errors.Is(err, installer.ErrAlreadyInstalled) {
		if action == "uninstall" || r.pkg.NoUpgrade {
			return pkgResult{index: idx, status: statusAlreadyInstalled, prevVersion: prevVersion}
		}
		return pkgResult{index: idx, status: statusUpToDate, prevVersion: prevVersion}
	}
	if errors.Is(err, context.Canceled) {
		return pkgResult{index: idx, status: statusFailed, detail: "cancelled"}
	}
	return pkgResult{index: idx, status: statusFailed, detail: err.Error()}
}

// runHook executes a shell command via cmd /C and returns any error.
func runHook(command string) error {
	return exec.Command("cmd", "/C", command).Run()
}

// ── Lipgloss styles ───────────────────────────────────────────────────────────

var (
	lgGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	lgRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	lgGray   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	lgCyan   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	lgYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	lgBold   = lipgloss.NewStyle().Bold(true)
)

// Default column widths; overridden dynamically by calcWidths.
const (
	wCat    = 18
	wMethod = 12
	wStatus = 50
)

// wApp is set at render time by calcWidths based on actual package names.
var wApp = 32

// calcWidths sets wApp to fit the longest package name in rows, with a minimum of 20.
func calcWidths(rows []tableRow) {
	w := 20
	for _, r := range rows {
		if l := len(r.pkg.Name); l > w {
			w = l
		}
	}
	wApp = w + 2
}

// ── Rendering ─────────────────────────────────────────────────────────────────

// pad pads s to n visual characters (safe for plain strings only).
func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// fmtRow formats one table row. app/cat/method must be plain (no ANSI),
// status may be styled.
func fmtRow(app, cat, method, status string) string {
	return fmt.Sprintf("  %-*s  %-*s  %-*s  %s",
		wApp, app, wCat, cat, wMethod, method, status)
}

func renderHeader(dryRun bool) string {
	// Pad then bold, so spacing is based on plain-string widths.
	h := fmt.Sprintf("\n  %s  %s  %s  %s",
		lgBold.Render(pad("Application", wApp)),
		lgBold.Render(pad("Category", wCat)),
		lgBold.Render(pad("Method", wMethod)),
		lgBold.Render(pad("Status", wStatus)),
	)
	sep := lgGray.Render(strings.Repeat("─", wApp+wCat+wMethod+wStatus+8))
	out := h + "\n" + sep
	if dryRun {
		out += "\n" + lgYellow.Render("[dry-run — no changes will be made]")
	}
	return out
}

func renderRow(r tableRow) string {
	return fmtRow(r.pkg.Name, r.category, displayMethod(r.pkg.Source), statusLabel(r))
}

func statusLabel(r tableRow) string {
	switch r.status {
	case statusOK:
		var s string
		switch strings.ToLower(r.pkg.Action) {
		case "uninstall":
			s = lgGreen.Render("uninstalled")
		default:
			s = lgGreen.Render("installed")
			if r.newVersion != "" {
				s += " " + lgGreen.Render(r.newVersion)
				if r.prevVersion != "" && r.prevVersion != r.newVersion {
					s += " " + lgGray.Render("(previous "+r.prevVersion+")")
				}
			}
		}
		if r.detail != "" {
			s += "  " + lgYellow.Render(r.detail)
		}
		return s
	case statusUpToDate:
		s := lgGreen.Render("up to date")
		if r.prevVersion != "" {
			s += " " + lgGray.Render("("+r.prevVersion+")")
		}
		return s
	case statusAlreadyInstalled:
		if strings.ToLower(r.pkg.Action) == "uninstall" {
			return lgGray.Render("already uninstalled")
		}
		return lgGray.Render("already installed")
	case statusSkipped:
		return lgGray.Render("skip")
	case statusFailed:
		msg := r.detail
		if msg == "" {
			msg = "unknown error"
		}
		return lgRed.Render("failed: " + msg)
	default:
		return lgGray.Render("pending")
	}
}

func renderSummary(m model) string {
	sep := lgGray.Render(strings.Repeat("─", wApp+wCat+wMethod+8))
	line := fmt.Sprintf("total %-4d  %s  %s  %s",
		m.nTotal,
		lgGreen.Render(fmt.Sprintf("ok %d", m.nOK)),
		lgGray.Render(fmt.Sprintf("skip %d", m.nSkip)),
		lgRed.Render(fmt.Sprintf("failed %d", m.nFailed)),
	)
	return "\n" + sep + "\n" + line
}

func displayMethod(source string) string {
	switch strings.ToLower(source) {
	case installer.SourceWinget:
		return "winget"
	case installer.SourceScoop:
		return "scoop"
	case installer.SourceChocolatey:
		return "choco"
	case installer.SourceGitHub:
		return "github"
	case installer.SourceThirdParty:
		return "third-party"
	default:
		return source
	}
}
