package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/lucasassuncao/bezel/draw"
	"github.com/lucasassuncao/bezel/table"
	"github.com/lucasassuncao/bezel/theme"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/state"
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
	statusConflict
)

type tableRow struct {
	app         config.Application
	category    string
	status      rowStatus
	detail      string
	newVersion  string
	prevVersion string
	// source is the manager that actually handled this row; empty until resolved.
	source config.Source
	// dryRun marks a row that was simulated, so the label can say "would
	// install" rather than claiming the work happened.
	dryRun bool
	// viaFallback is set when source is not the first preference, so the row
	// can say so instead of silently reporting a different manager.
	viaFallback bool
}

// ── Bubbletea model ───────────────────────────────────────────────────────────

type model struct {
	rows     []tableRow
	current  int
	instOpts installer.Options
	dryRun   bool
	state    *state.State
	nTotal   int
	nOK      int
	nSkip    int
	nFailed  int
	// nRequired counts failures that should fail the run; an application
	// marked optional is reported but does not set the exit code.
	nFailedRequired   int
	installedBinaries bool
	quitting          bool
	probe             Prober
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
		runRow(m.ctx, 0, m.rows[0], m.instOpts, m.dryRun, m.state, m.probe),
	)
}

type pkgResult struct {
	index           int
	status          rowStatus
	detail          string
	installedBinary bool
	newVersion      string
	prevVersion     string
	source          config.Source
	dryRun          bool
	viaFallback     bool
}

// applyResult copies a finished result onto its row. Shared by the TUI and the
// JSON path so both report exactly the same fields.
func applyResult(r *tableRow, res pkgResult) {
	r.status = res.status
	r.detail = res.detail
	r.newVersion = res.newVersion
	r.prevVersion = res.prevVersion
	r.source = res.source
	r.viaFallback = res.viaFallback
	r.dryRun = res.dryRun
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
		m.cancel()
		m.quitting = true
		return m, tea.Quit
	}

	res, ok := msg.(pkgResult)
	if !ok {
		return m, nil
	}

	applyResult(&m.rows[res.index], res)
	m.nTotal++
	switch res.status {
	case statusOK, statusUpToDate, statusAlreadyInstalled:
		m.nOK++
	case statusSkipped:
		m.nSkip++
	case statusFailed, statusConflict:
		m.nFailed++
		if !m.rows[res.index].app.Optional {
			m.nFailedRequired++
		}
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
		return m, tea.Sequence(printCmd, runRow(m.ctx, m.current, m.rows[m.current], m.instOpts, m.dryRun, m.state, m.probe))
	}
}

func (m model) View() tea.View {
	return tea.NewView(m.render())
}

func (m model) render() string {
	if m.quitting || m.current >= len(m.rows) {
		return ""
	}
	r := m.rows[m.current]
	action := r.app.EffectiveAction()
	first, _ := r.app.Source.First()
	var verb string
	switch {
	case action == config.ActionSkip:
		verb = lgGray.Render("skip")
	case m.dryRun:
		verb = lgYellow.Render("dry-run")
	case action == config.ActionUninstall:
		verb = lgCyan.Render("uninstalling...")
	case first == config.SourceGitHub:
		verb = lgCyan.Render("downloading...")
	default:
		verb = lgCyan.Render("installing...")
	}
	return fmtRow(r.app.Name, r.category, displayMethod(first), verb) + "\n"
}

// ── tea.Cmd ───────────────────────────────────────────────────────────────────

func runRow(ctx context.Context, idx int, r tableRow, opts installer.Options, dryRun bool, st *state.State, probe Prober) tea.Cmd {
	return func() tea.Msg {
		return doInstall(ctx, idx, r, opts, dryRun, st, probe)
	}
}

func doInstall(ctx context.Context, idx int, r tableRow, opts installer.Options, dryRun bool, st *state.State, probe Prober) pkgResult {
	app := r.app
	action := app.EffectiveAction()
	if action == config.ActionSkip {
		return pkgResult{index: idx, status: statusSkipped}
	}

	res := ResolveSource(app, st, probe)
	switch res.Kind {
	case ResolutionNoSource:
		return pkgResult{index: idx, status: statusFailed, detail: "no source declared"}
	case ResolutionConflict:
		names := make([]string, len(res.Conflicts))
		for i, s := range res.Conflicts {
			names[i] = string(s)
		}
		return pkgResult{
			index: idx, status: statusConflict, source: res.Source,
			detail: "installed via " + strings.Join(names, " and ") +
				" — run 'apptide adopt " + app.Name + " --source <one>'",
		}
	}

	first, _ := app.Source.First()
	fallback := func(used config.Source) bool { return used != "" && used != first }

	if dryRun {
		return pkgResult{index: idx, status: statusOK, source: res.Source, viaFallback: fallback(res.Source), dryRun: true}
	}

	// Version currently installed, via the source that owns the app.
	prevVersion := currentVersion(app, res.Source, opts)

	// pre_install hook — failure aborts the action.
	if pre := app.Pre(action); pre != "" {
		if herr := RunHook(ctx, pre); herr != nil {
			return pkgResult{index: idx, status: statusFailed, detail: "pre-hook: " + herr.Error()}
		}
	}

	var out Outcome
	if action == config.ActionUninstall {
		out = RunUninstall(ctx, app, res, opts)
	} else {
		out = RunInstall(ctx, app, res, opts)
	}

	isBinary := out.Source == config.SourceGitHub
	result := pkgResult{
		index:       idx,
		source:      out.Source,
		viaFallback: fallback(out.Source),
		prevVersion: prevVersion,
	}

	switch {
	case out.Err == nil:
		result.status = statusOK
		result.installedBinary = isBinary
		result.newVersion = currentVersion(app, out.Source, opts)
		recordState(st, app, out, result.newVersion, action, dryRun)

		// post_install hook — failure is a warning, not a hard failure.
		// Hooks named for install must not fire on a removal.
		if post := app.Post(action); post != "" {
			if herr := RunHook(ctx, post); herr != nil {
				result.detail = "post-hook: " + herr.Error()
			}
		}
	case errors.Is(out.Err, installer.ErrAlreadyInstalled):
		if action == config.ActionUninstall || app.SkipUpgrade {
			result.status = statusAlreadyInstalled
		} else {
			result.status = statusUpToDate
		}
		recordState(st, app, out, prevVersion, action, dryRun)
	case errors.Is(out.Err, context.Canceled):
		result.status = statusFailed
		result.detail = "cancelled"
	default:
		result.status = statusFailed
		result.detail = out.Err.Error()
	}

	return result
}

// recordState persists the binding unless this is a dry run.
func recordState(st *state.State, app config.Application, out Outcome, version string, action config.Action, dryRun bool) {
	if dryRun {
		return
	}
	RecordState(st, app, out, version, action)
}

// currentVersion asks src what version of app is installed, if any.
func currentVersion(app config.Application, src config.Source, opts installer.Options) string {
	if src == "" {
		return ""
	}
	inst, err := installer.Resolve(src, opts)
	if err != nil || !inst.IsAvailable() {
		return ""
	}
	_, version := inst.Check(app)
	return version
}

// RunHook executes a lifecycle hook via cmd /C.
//
// It takes a context so that Ctrl+C stops a hook too: without one, cancelling
// a run left a pre_install script running with nothing watching it. The output
// is attached to the error, because an exit code alone says nothing about what
// the script did.
func RunHook(ctx context.Context, command string) error {
	var out hookOutput
	cmd := exec.CommandContext(ctx, "cmd", "/C", command) //#nosec G204 -- running a shell command from the config is the hook feature itself
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		if text := out.String(); text != "" {
			return fmt.Errorf("%w: %s", err, text)
		}
		return err
	}
	return nil
}

// hookOutput keeps the last 2 KiB a hook printed, for its error message.
type hookOutput struct{ buf bytes.Buffer }

func (h *hookOutput) Write(p []byte) (int, error) {
	h.buf.Write(p)
	if excess := h.buf.Len() - 2048; excess > 0 {
		h.buf.Next(excess)
	}
	return len(p), nil
}

func (h *hookOutput) String() string {
	var kept []string
	for _, line := range strings.Split(strings.ReplaceAll(h.buf.String(), "\r", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	if len(kept) > 4 {
		kept = kept[len(kept)-4:]
	}
	return strings.Join(kept, "; ")
}

// ── Styles ────────────────────────────────────────────────────────────────────

// The same terminal theme the browser draws with, so a run printed inline
// reads like the rows it came from.
var (
	th       = theme.Resolve(theme.ThemeTerminal, true)
	lgGreen  = th.Success
	lgRed    = th.Danger
	lgGray   = th.Dim
	lgCyan   = th.Info
	lgYellow = th.Warning
	lgBold   = th.Bold
)

// The columns before the status. Rows print as each install finishes, so the
// widths are fixed up front by calcWidths, from every row's cells.
var tableCols = []table.Column{{Title: "Application"}, {Title: "Category"}, {Title: "Method"}}

var widths = table.Fit(tableCols, nil, math.MaxInt)

// statusWidth is how far the rules reach past the columns: statuses vary.
const statusWidth = 50

// calcWidths sizes the columns by what is drawn, not by bytes, counting every
// source a row may fall back to.
func calcWidths(rows []tableRow) {
	cells := make([][]string, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, []string{r.app.Name, r.category, displayMethod("")})
		for _, src := range r.app.Source {
			cells = append(cells, []string{r.app.Name, r.category, displayMethod(src)})
		}
	}
	widths = table.Fit(tableCols, cells, math.MaxInt)
}

// ── Rendering ─────────────────────────────────────────────────────────────────

// fmtRow formats one table row; the status runs as long as it needs.
func fmtRow(app, cat, method, status string) string {
	return "  " + table.Row(append(slices.Clone(widths), draw.Width(status)), []string{app, cat, method, status})
}

// tableHeader is the titles in bold and a rule under them, for install and verify.
func tableHeader() string {
	titles := table.Row(append(slices.Clone(widths), statusWidth), []string{"Application", "Category", "Method", "Status"})
	return "\n  " + lgBold.Render(titles) + "\n" + rule(statusWidth)
}

// rule is a line under the columns and extra cells past them.
func rule(extra int) string {
	w := 2 + extra
	for _, cw := range widths {
		w += cw + table.Gap
	}
	return lgGray.Render(strings.Repeat("─", w))
}

func renderHeader(dryRun bool) string {
	out := tableHeader()
	if dryRun {
		out += "\n" + lgYellow.Render("[dry-run: no changes will be made]")
	}
	return out
}

func renderRow(r tableRow) string {
	src := r.source
	if src == "" {
		src, _ = r.app.Source.First()
	}
	return fmtRow(r.app.Name, r.category, displayMethod(src), statusLabel(r))
}

func statusLabel(r tableRow) string {
	switch r.status {
	case statusOK:
		var s string
		switch {
		case r.dryRun && r.app.EffectiveAction() == config.ActionUninstall:
			// A dry run must not claim work it did not do. The JSON output
			// already said "would_uninstall" while the table said "uninstalled".
			return lgYellow.Render("would uninstall")
		case r.dryRun:
			return lgYellow.Render("would install")
		case r.app.EffectiveAction() == config.ActionUninstall:
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
		if r.viaFallback {
			s += " " + lgYellow.Render("via "+string(r.source))
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
		if r.viaFallback {
			s += " " + lgGray.Render("via "+string(r.source))
		}
		return s
	case statusAlreadyInstalled:
		if r.app.EffectiveAction() == config.ActionUninstall {
			return lgGray.Render("already uninstalled")
		}
		return lgGray.Render("already installed")
	case statusSkipped:
		return lgGray.Render("skip")
	case statusConflict:
		return lgYellow.Render("conflict: " + r.detail)
	case statusFailed:
		msg := r.detail
		if msg == "" {
			msg = "unknown error"
		}
		if r.app.Optional {
			return lgYellow.Render("failed (optional): " + msg)
		}
		return lgRed.Render("failed: " + msg)
	default:
		return lgGray.Render("pending")
	}
}

func renderSummary(m model) string {
	sep := rule(0)
	line := fmt.Sprintf("total %-4d  %s  %s  %s",
		m.nTotal,
		lgGreen.Render(fmt.Sprintf("ok %d", m.nOK)),
		lgGray.Render(fmt.Sprintf("skip %d", m.nSkip)),
		failedSummary(m),
	)
	return "\n" + sep + "\n" + line
}

func displayMethod(source config.Source) string {
	switch source.Normalize() {
	case config.SourceChocolatey:
		return "choco"
	case "":
		return "—"
	default:
		return string(source)
	}
}

// failedSummary reports required failures, and optional ones separately so a
// non-zero exit code always matches the number shown in red.
func failedSummary(m model) string {
	out := lgRed.Render(fmt.Sprintf("failed %d", m.nFailedRequired))
	if optional := m.nFailed - m.nFailedRequired; optional > 0 {
		out += "  " + lgYellow.Render(fmt.Sprintf("optional %d", optional))
	}
	return out
}
