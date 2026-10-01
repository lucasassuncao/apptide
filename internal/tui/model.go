// Package tui is the interactive browser for an apptide configuration:
// what is declared, what the machine actually has, and where the two disagree.
//
// It is a reader and an applier, not an editor — the config file itself is
// edited by `apptide edit`, which owns the file and its undo history. Keeping
// a single writer avoids two code paths racing to rewrite the same YAML.
//
// Everything on screen is the bezel shell's: the header, the tab strip, the
// two panels, the status row, the legend and the dialogs. What is here is the
// data behind the rows and what each key means.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/installer"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/lucasassuncao/apptide/internal/state"
	"github.com/lucasassuncao/bezel/overlay"
	"github.com/lucasassuncao/bezel/shell"
	"github.com/lucasassuncao/bezel/tree"
)

type tab int

const (
	tabConfig tab = iota
	tabInstalled
	tabUpgradable
	tabDrift
	tabActivity
)

var tabNames = map[tab]string{
	tabConfig:     "Config",
	tabInstalled:  "Installed",
	tabUpgradable: "Upgradable",
	tabDrift:      "Drift",
	tabActivity:   "Activity",
}

var tabOrder = []tab{tabConfig, tabInstalled, tabUpgradable, tabDrift, tabActivity}

// The keys the list answers to. Vim's letters beside the arrows.
var (
	keyUp    = key.NewBinding(key.WithKeys("up", "k"))
	keyDown  = key.NewBinding(key.WithKeys("down", "j"))
	keyLeft  = key.NewBinding(key.WithKeys("left", "h"))
	keyRight = key.NewBinding(key.WithKeys("right", "l"))
)

// Options configures the browser.
type Options struct {
	ConfigPath  string
	StatePath   string
	InstallDir  string
	GitHubToken string
}

// Model is the bubbletea model for the browser.
type Model struct {
	opts     Options
	instOpts installer.Options

	cfg        *config.Config
	st         *state.State
	snap       *inventory.Snapshot
	upg        *inventory.Upgrades
	upgLoading bool
	probe      runner.Prober

	// sh is the chrome. tab mirrors its active tab for the code that reads it.
	sh  shell.Shell
	tab tab

	rows  map[tab][]row
	lists map[tab]tree.Model[item]
	// detailOffset scrolls the detail pane; reset whenever the cursor moves.
	detailOffset int
	// collapsed folds a group away, per tab and group name.
	collapsed map[tab]map[string]bool

	filter    textinput.Model
	filtering bool
	width     int
	height    int
	loading   bool

	// pending changes and their execution
	running bool
	queue   []queueItem
	dryRun  bool
	events  chan applyEvent
	// activity is the log the Activity tab shows, newest last.
	activity []string
	cancel   context.CancelFunc

	// pendingAdopt is the application the adopt dialog is asking about;
	// pendingForget the binding the forget dialog is; dialog names which of
	// the shell's overlays is up, for the crash note and the tests.
	pendingAdopt  adoptCtx
	pendingForget string
	dialog        string

	// quitRequested records a first ctrl+c during a run, so the second one
	// can stop it instead of repeating the same message.
	quitRequested bool
	quitting      bool
}

// New builds the model. The config is read eagerly so a broken file fails on
// the command line instead of inside an alt-screen the user then has to escape.
func New(opts Options) (*Model, error) {
	cfg, err := config.LoadWithImports(opts.ConfigPath)
	if err != nil {
		return nil, err
	}

	statePath := opts.StatePath
	if statePath == "" {
		statePath = state.DefaultPath()
	}
	st, _, err := state.LoadOrReset(statePath)
	if err != nil {
		return nil, err
	}

	// The config's settings block fills in what the command line left empty.
	installDir, token := opts.InstallDir, opts.GitHubToken
	if s := cfg.Settings; s != nil {
		if installDir == "" {
			installDir = s.InstallDir
		}
		if token == "" {
			token = s.GitHubToken
		}
	}

	m := newModel(opts, cfg, st)
	m.instOpts = installer.Options{GitHubToken: token, DefaultInstallDir: installDir}
	m.loading = true
	return m, nil
}

// newModel wires the parts New and the tests share.
func newModel(opts Options, cfg *config.Config, st *state.State) *Model {
	fi := textinput.New()
	fi.Prompt = "/ "
	fi.Placeholder = "filter by name, category, source or tag"
	fi.CharLimit = 64

	m := &Model{
		opts:      opts,
		cfg:       cfg,
		st:        st,
		filter:    fi,
		rows:      map[tab][]row{},
		lists:     map[tab]tree.Model[item]{},
		collapsed: map[tab]map[string]bool{},
	}
	m.sh = m.newShell()
	return m
}

// newShell builds the chrome: one tab per screen, the config path in the
// header, the write filter and the keys the shell handles itself.
func (m *Model) newShell() shell.Shell {
	tabs := make([]shell.Tab, len(tabOrder))
	for i, t := range tabOrder {
		tabs[i] = tabAdapter{m: m, t: t}
	}
	return shell.New(shell.Config{
		Layout:    m.layout(),
		Tabs:      tabs,
		Theme:     th,
		Title:     "apptide",
		Version:   m.opts.ConfigPath,
		Actions:   m.globalActions(),
		StatusTTL: statusLife,
	})
}

// statusLife is how long a copy's result stays on the status row. A variable
// so tests can shrink the timer they would otherwise wait out.
var statusLife = 5 * time.Second

// globalActions are live on every tab. The help is apptide's own panel, and
// quitting may be refused mid-run: both run in Update, which owns the shell.
func (m *Model) globalActions() []shell.Action {
	notFiltering := shell.When(func(shell.Context) bool { return !m.filtering })
	return []shell.Action{
		shell.Help(shell.RunWith(shell.Send(helpMsg{})), notFiltering),
		shell.ChangeTab(), shell.Move(),
		shell.Quit(shell.RunWith(shell.Send(quitMsg{}))),
	}
}

type (
	helpMsg struct{}
	quitMsg struct{}
)

type snapshotMsg struct{ snap *inventory.Snapshot }
type upgradesMsg struct{ upg *inventory.Upgrades }

func (m *Model) Init() tea.Cmd {
	load := tea.Batch(m.loadSnapshot(), m.loadUpgrades())
	return tea.Batch(load, m.syncBusy())
}

// syncBusy puts what is running on the status row and ends it when nothing
// is. Not the upgrades check: it is long, and the row's filter and source
// warnings must stay readable meanwhile; its own pane says it is checking.
func (m *Model) syncBusy() tea.Cmd {
	var text string
	switch {
	case m.running:
		text = "running…"
	case m.loading:
		text = "refreshing…"
	default:
		m.sh = m.sh.Idle()
		return nil
	}
	var cmd tea.Cmd
	m.sh, cmd = m.sh.Busy(text)
	return cmd
}

// loadSnapshot queries every manager once, off the UI goroutine.
func (m *Model) loadSnapshot() tea.Cmd {
	return func() tea.Msg {
		return snapshotMsg{snap: inventory.Installed(context.Background())}
	}
}

// loadUpgrades asks each manager what it could upgrade. It is a separate
// command from the snapshot because winget and scoop go to the network for it,
// and the rest of the UI should not wait.
func (m *Model) loadUpgrades() tea.Cmd {
	m.upgLoading = true
	return func() tea.Msg {
		return upgradesMsg{upg: inventory.Outdated(context.Background())}
	}
}

func (m *Model) rebuild() {
	m.probe = runner.NewInventoryProber(m.snap, m.instOpts)
	cfgRows := buildConfigRows(m.cfg, m.st, m.probe, m.snap, m.upg)
	m.rows[tabConfig] = cfgRows
	m.rows[tabDrift] = buildDriftRows(cfgRows)
	m.rows[tabInstalled] = buildInstalledRows(m.cfg, m.snap)
	m.rows[tabUpgradable] = buildUpgradableRows(m.cfg, m.upg)
}

// visibleRows applies the filter to the active tab.
func (m *Model) visibleRows() []row {
	return filterRows(m.rows[m.tab], m.filter.Value())
}

// Messages the dialogs answer with. Each carries what the user decided; the
// dialog itself is the shell's and is gone by the time these arrive.
type (
	applyMsg  struct{ dryRun bool }
	adoptMsg  struct{ choice int }
	forgetMsg struct{ key string }
	adoptCtx  struct {
		app     config.Application
		choices []config.Source
	}
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	if busy := m.syncBusy(); busy != nil {
		cmd = tea.Batch(cmd, busy)
	}
	if !m.sh.HasOverlay() {
		m.dialog = ""
	}
	m.relayout()
	return m, cmd
}

// modeName is what is in front of the user besides a tab: a dialog, the
// filter, a run in progress, or nothing ("browse").
func (m *Model) modeName() string {
	switch {
	case m.dialog != "":
		return m.dialog
	case m.filtering:
		return "filter"
	case m.running:
		return "running"
	}
	return "browse"
}

// openDialog pushes one of apptide's dialogs and remembers which.
func (m *Model) openDialog(name string, o overlay.Overlay) {
	m.dialog = name
	m.sh = m.sh.Push(o)
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.sh, _, _ = m.sh.Update(msg)
		return nil

	case snapshotMsg:
		m.snap = msg.snap
		m.loading = false
		m.rebuild()
		return nil

	case upgradesMsg:
		m.upg = msg.upg
		m.upgLoading = false
		if m.snap != nil {
			m.rebuild()
		}
		return nil

	case applyEvent:
		return m.handleApplyEvent(msg)

	case applyMsg:
		m.dryRun = msg.dryRun
		return m.runQueue()
	case adoptMsg:
		m.adopt(msg.choice)
		return nil
	case forgetMsg:
		m.forget(msg.key)
		return nil
	case helpMsg:
		m.openDialog("help", m.helpOverlay())
		return nil
	case quitMsg:
		return m.quit()

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// The shell's own: a status expiring, a dialog closing.
	if sh, handled, cmd := m.sh.Update(msg); handled {
		m.sh = sh
		return cmd
	}
	return nil
}

func (m *Model) handleApplyEvent(ev applyEvent) tea.Cmd {
	if ev.finished {
		m.running = false
		m.queue = nil
		m.quitRequested = false
		m.activity = append(m.activity, stDim.Render("— done —"), "")
		// The machine changed, so the snapshot is stale.
		m.loading = true
		return tea.Batch(m.loadSnapshot(), m.loadUpgrades())
	}
	if line := ev.render(); line != "" {
		m.activity = append(m.activity, line)
	}
	return waitForEvent(m.events)
}

// handleKey routes a keystroke: the filter and the dialogs answer first, then
// the shell's tab keys, then movement, then the actions.
func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.sh.HasOverlay() {
		var cmd tea.Cmd
		m.sh, _, cmd = m.sh.Update(msg)
		return cmd
	}

	if msg.String() == "ctrl+c" {
		return m.quit()
	}

	// The shell runs help, the tab keys and quit; the rest are apptide's.
	if sh, handled, cmd := m.sh.Update(msg); handled {
		m.sh = sh
		if t := tabOrder[m.sh.ActiveTab()]; t != m.tab {
			m.setTab(t)
		}
		return cmd
	}

	// Movement and folding first, so what follows reads as a list of
	// actions rather than a list of actions plus every way to move a cursor.
	if m.handleNavKey(msg) {
		return nil
	}

	switch msg.String() {
	case "/":
		return m.openFilter()

	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
		}
		m.sh = m.sh.ClearStatus()
		return nil

	case "space": // tea v2 names the space bar; " " never matches
		m.toggleSelect()
		return nil

	case "R":
		m.loading = true
		m.sh = m.sh.ClearStatus()
		return tea.Batch(m.loadSnapshot(), m.loadUpgrades())

	case "i":
		m.stage(config.ActionInstall)
	case "r":
		m.stage(config.ActionUninstall)
	case "a":
		m.openAdopt()
	case "f":
		m.openForget()
	case "x":
		m.runConfig()
	case "y":
		return m.yank()
	case "A":
		m.addToConfig()

	// Shift-J/K scroll the detail pane, which can be taller than the panel.
	case "J":
		m.detailOffset++
	case "K":
		if m.detailOffset > 0 {
			m.detailOffset--
		}
	}
	return nil
}

// handleNavKey moves the cursor, switches tabs by number and folds groups.
// It reports whether it consumed the key.
//
// Left and right fold; they are directional, not a toggle: left only ever
// closes (or steps out of) a group and right only ever opens (or steps into)
// one. An arrow that does both leaves the user unable to predict a press
// without first reading the arrow on the header.
func (m *Model) handleNavKey(msg tea.KeyPressMsg) bool {
	switch k := msg.String(); k {
	case "1", "2", "3", "4", "5":
		if i := int(k[0] - '1'); i < len(tabOrder) {
			m.setTab(tabOrder[i])
		}
	case "l", "right":
		m.foldRight()
	case "h", "left":
		m.foldLeft()
	case "enter":
		m.toggleCollapseAtCursor()
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "pgdown", "ctrl+f":
		m.move(m.listHeight())
	case "pgup", "ctrl+b":
		m.move(-m.listHeight())
	case "g", "home":
		m.setCursor(0)
	case "G", "end":
		m.setCursor(len(m.list().Visible()) - 1)
	default:
		return false
	}
	return true
}

// setTab shows a tab, the shell's strip following.
func (m *Model) setTab(t tab) {
	m.tab = t
	for i, o := range tabOrder {
		if o == t {
			m.sh = m.sh.SetTab(i)
		}
	}
}

func (m *Model) quit() tea.Cmd {
	if m.running {
		if m.quitRequested {
			// The second press has to actually do it: let the running command
			// finish rather than leaving a package half-installed.
			if m.cancel != nil {
				m.cancel()
			}
			m.quitting = true
			return tea.Quit
		}
		m.quitRequested = true
		m.warn("still applying; press ctrl+c again to stop after the current package")
		return nil
	}
	m.quitting = true
	return tea.Quit
}

// openFilter puts the filter on the status row; the shell hands it the keys.
func (m *Model) openFilter() tea.Cmd {
	m.filtering = true
	m.filter.Focus()
	m.sh = m.sh.SetInput(&m.filter)
	return textinput.Blink
}

func (m *Model) closeFilter() {
	m.filtering = false
	m.filter.Blur()
	m.sh = m.sh.SetInput(nil)
	m.setCursor(m.cursor())
}

func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.closeFilter()
		return nil
	case "enter":
		m.closeFilter()
		return nil
	}
	var cmd tea.Cmd
	m.sh, _, cmd = m.sh.Update(msg)
	m.setCursor(0)
	return cmd
}

// Status. Nothing here expires: a message stays until esc or the next one,
// since most of them say why a key did nothing.
func (m *Model) info(text string) { m.sh, _ = m.sh.SetStatus(text, shell.OK, 0) }
func (m *Model) warn(text string) { m.sh, _ = m.sh.SetStatus(text, shell.Error, 0) }

// status is the message on the status row, and whether it reports success.
func (m *Model) status() (string, bool) { return m.sh.Status(), m.sh.StatusLevel() == shell.OK }

// toggleSelect marks the row under the cursor, or the whole group when the
// cursor is on a header.
func (m *Model) toggleSelect() {
	if m.tab == tabActivity {
		return
	}
	it, ok := m.currentItem()
	if !ok {
		return
	}
	if it.header {
		m.selectGroup(it, !m.groupAllSelected(it))
		return
	}
	// Rows are values, so flip the flag on the backing slice.
	for i := range m.rows[m.tab] {
		if m.rows[m.tab][i].name() == it.row.name() {
			m.rows[m.tab][i].selected = !m.rows[m.tab][i].selected
			return
		}
	}
}

// staged returns the rows an action applies to: everything selected, or the
// row under the cursor when nothing is selected.
func (m *Model) staged() []row {
	var out []row
	for _, r := range m.rows[m.tab] {
		if r.selected {
			out = append(out, r)
		}
	}
	if len(out) > 0 {
		return out
	}
	if r, ok := m.currentRow(); ok {
		return []row{r}
	}
	return nil
}

// stage builds the queue and asks for confirmation. Nothing runs until the
// user confirms: an install queue is a change to the machine, and it should be
// visible in full before it starts.
func (m *Model) stage(action config.Action) {
	if m.tab == tabInstalled || m.tab == tabActivity {
		m.warn("this tab is read-only — switch to Config, Upgradable or Drift")
		return
	}

	rows := m.staged()
	if len(rows) == 0 {
		return
	}

	m.queue = nil
	var undeclared int
	for _, r := range rows {
		// The Upgradable tab also lists packages apptide does not manage;
		// upgrading those would be acting outside the config.
		if r.app.Name == "" {
			undeclared++
			continue
		}
		if r.res.Kind == runner.ResolutionConflict {
			m.warn(r.name() + " has a source conflict — press a to set its source first")
			return
		}
		if r.res.Kind == runner.ResolutionNoSource {
			continue
		}

		verb := "install"
		switch {
		case action == config.ActionUninstall:
			verb = "remove"
		case m.tab == tabUpgradable, r.state == stateInstalled:
			verb = "upgrade"
		}
		app := r.app
		app.Action = action
		m.queue = append(m.queue, queueItem{app: app, res: r.res, action: action, verb: verb})
	}

	if len(m.queue) == 0 {
		if undeclared > 0 {
			m.warn("not in the config — press A on the Installed tab to declare it first")
		}
		return
	}
	m.openDialog("confirm", m.confirmOverlay(""))
}

// runConfig queues the whole configuration, which is what `apptide install`
// does from the command line — the point of having it here is not having to
// leave the browser to apply what you have just been looking at.
//
// Unlike i and r it takes the action from each application rather than from
// the key, so an entry declared `action: uninstall` is removed and one
// declared `action: skip` is left alone.
func (m *Model) runConfig() {
	m.queue = nil
	var skipped, conflicts, unusable int

	for _, r := range m.rows[tabConfig] {
		action := r.app.EffectiveAction()

		switch {
		case action == config.ActionSkip:
			skipped++
			continue
		case r.res.Kind == runner.ResolutionConflict:
			conflicts++
			continue
		case r.res.Kind == runner.ResolutionNoSource:
			unusable++
			continue
		}

		verb := "install"
		switch {
		case action == config.ActionUninstall:
			verb = "remove"
		case r.state == stateInstalled:
			verb = "upgrade"
		}
		m.queue = append(m.queue, queueItem{app: r.app, res: r.res, action: action, verb: verb})
	}

	if len(m.queue) == 0 {
		m.warn("nothing to do — every application is skipped or unusable")
		return
	}

	var notes []string
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf("%d skipped", skipped))
	}
	if conflicts > 0 {
		notes = append(notes, fmt.Sprintf("%d with a source conflict", conflicts))
	}
	if unusable > 0 {
		notes = append(notes, fmt.Sprintf("%d with no source", unusable))
	}
	m.openDialog("confirm", m.confirmOverlay(strings.Join(notes, ", ")))
}

func (m *Model) runQueue() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.events = make(chan applyEvent, 32)
	m.running = true
	m.setTab(tabActivity)

	header := fmt.Sprintf("applying %d change(s)", len(m.queue))
	if m.dryRun {
		header += "  " + stYellow.Render("[dry-run]")
	}
	m.activity = append(m.activity, stBold.Render(header))

	startApply(ctx, m.queue, m.instOpts, m.st, m.dryRun, m.events)
	return waitForEvent(m.events)
}

// yank copies the selected installed packages to the clipboard as config
// entries. It is what selection on the Installed tab is for: that tab cannot
// install or remove anything, so without this the checkboxes would do nothing.
func (m *Model) yank() tea.Cmd {
	if m.tab != tabInstalled {
		m.warn("y copies config entries — only on the Installed tab")
		return nil
	}

	rows := m.staged()
	if len(rows) == 0 {
		return nil
	}

	snippet := yamlSnippet(rows)
	if snippet == "" {
		return nil
	}

	noun := "entries"
	if len(rows) == 1 {
		noun = "entry"
	}
	done := fmt.Sprintf("%d config %s copied, paste them in apptide edit", len(rows), noun)
	return m.sh.Copy(snippet, done, "could not reach the clipboard")
}

// addToConfig appends the selected installed packages to the config file.
//
// The entry it writes is the minimum that installs correctly: name, source and
// package id. Category, version pinning and hooks are for `apptide edit`,
// which is the right tool for shaping a declaration — this key exists so the
// user does not have to retype an identifier that is already on screen.
func (m *Model) addToConfig() {
	if m.tab != tabInstalled {
		m.warn("A adds installed packages to the config — only on the Installed tab")
		return
	}

	rows := m.staged()
	if len(rows) == 0 {
		return
	}

	var apps []config.Application
	var skipped int
	for _, r := range rows {
		if r.entrySrc == "" {
			continue
		}
		if r.configName != "" {
			// Already declared: adding it again would install the same package
			// twice under two names.
			skipped++
			continue
		}
		apps = append(apps, applicationFromEntry(r))
	}

	if len(apps) == 0 {
		m.warn("nothing to add — every selected package is already in the config")
		return
	}

	if err := config.AppendApplications(m.opts.ConfigPath, apps); err != nil {
		m.warn(err.Error())
		return
	}

	// Re-read from disk so the rows reflect the file we just changed.
	cfg, err := config.LoadWithImports(m.opts.ConfigPath)
	if err != nil {
		m.warn("added, but the config no longer loads: " + err.Error())
		return
	}
	m.cfg = cfg
	m.clearSelection()
	m.rebuild()

	noun := "applications"
	if len(apps) == 1 {
		noun = "application"
	}
	msg := fmt.Sprintf("%d %s added to %s — refine them with apptide edit",
		len(apps), noun, m.opts.ConfigPath)
	if skipped > 0 {
		msg += fmt.Sprintf(" (%d already declared)", skipped)
	}
	m.info(msg)
}

// applicationFromEntry builds the smallest declaration that installs a package.
func applicationFromEntry(r row) config.Application {
	app := config.Application{
		Name: r.entry.ID,
		// Explicit rather than omitted: the file then states the intent, and
		// the Declared column can tell "tracking latest" apart from "nothing
		// was said". Adopting must not pin the package to today's version.
		Version: "latest",
		Source:  config.Sources{r.entrySrc},
	}
	id := r.entry.ID
	switch r.entrySrc.Normalize() {
	case config.SourceWinget:
		app.Package.Winget = &config.WingetSpec{ID: id}
	case config.SourceChocolatey:
		app.Package.Chocolatey = &config.ChocolateySpec{ID: id}
	case config.SourceScoop:
		app.Package.Scoop = &config.ScoopSpec{ID: id}
	case config.SourceGitHub:
		app.Package.GitHub = &config.GitHubSpec{ID: id}
	}
	return app
}

// clearSelection unmarks every row of the active tab.
func (m *Model) clearSelection() {
	for i := range m.rows[m.tab] {
		m.rows[m.tab][i].selected = false
	}
}

// openAdopt asks which source should own the application under the cursor.
func (m *Model) openAdopt() {
	r, ok := m.currentRow()
	if !ok || r.app.Name == "" {
		return
	}
	choices := r.res.Conflicts
	if len(choices) == 0 {
		choices = r.app.Source
	}
	if len(choices) == 0 {
		return
	}
	m.openDialog("adopt", m.adoptOverlay(adoptCtx{app: r.app, choices: choices}))
}

// adopt records the source the adopt dialog chose.
func (m *Model) adopt(choice int) {
	ctx, ok := m.pendingAdopt, m.pendingAdopt.app.Name != ""
	if !ok || choice < 0 || choice >= len(ctx.choices) {
		return
	}
	src := ctx.choices[choice]
	if err := m.st.Adopt(ctx.app.Name, string(src)); err != nil {
		// No entry yet: record one so the choice sticks.
		id, _ := ctx.app.Package.ID(src)
		m.st.Bind(ctx.app.Name, state.Entry{InstalledVia: string(src), PackageID: id})
	}
	if err := saveState(m.st); err != nil {
		m.warn("could not save state: " + err.Error())
	} else {
		m.info(fmt.Sprintf("%s is now managed by %s", ctx.app.Name, src))
	}
	m.pendingAdopt = adoptCtx{}
	m.rebuild()
}

// forgetTarget resolves the state entry a row refers to, if any.
//
// Rows reach the state by different routes: the config-driven tabs know the
// application name the state keys on, while the Installed tab knows only what a
// package manager reported. The last case is the one worth handling — an
// application installed by apptide and later dropped from the config leaves an
// entry nothing else can name, which is precisely what wants forgetting.
func (m *Model) forgetTarget(r row) (key string, e state.Entry, ok bool) {
	for _, name := range []string{r.app.Name, r.configName} {
		if name == "" {
			continue
		}
		if e, found := m.st.Get(name); found {
			return state.Key(name), e, true
		}
	}

	// Nothing named it. Match the manager's own report against the recorded
	// package ids instead.
	if r.entry.ID == "" || r.entrySrc == "" {
		return "", state.Entry{}, false
	}
	for _, k := range m.st.Keys() {
		got, found := m.st.Get(k)
		if !found {
			continue
		}
		if config.Source(got.InstalledVia).Normalize() == r.entrySrc.Normalize() &&
			strings.EqualFold(got.PackageID, r.entry.ID) {
			return k, got, true
		}
	}
	return "", state.Entry{}, false
}

// openForget asks whether to drop the state binding for the row under the
// cursor. Forgetting is not uninstalling, so it is confirmed rather than
// applied on the keystroke: the software stays, and only apptide's record of
// owning it goes.
func (m *Model) openForget() {
	r, ok := m.currentRow()
	if !ok {
		return
	}
	key, entry, found := m.forgetTarget(r)
	if !found {
		m.warn(forgetNothingMsg(r))
		return
	}
	m.pendingForget = key
	m.openDialog("forget", m.forgetOverlay(key, entry))
}

// forgetNothingMsg names the row as the user sees it, so the message is about
// something on screen rather than an empty string.
func forgetNothingMsg(r row) string {
	label := r.app.Name
	if label == "" {
		label = r.configName
	}
	if label == "" {
		label = r.entry.ID
	}
	if label == "" {
		return "nothing to forget here"
	}
	return fmt.Sprintf("%s is not tracked — nothing to forget", label)
}

// forget drops the binding the forget dialog confirmed.
func (m *Model) forget(key string) {
	m.pendingForget = ""
	if !m.st.Remove(key) {
		// Removed by another process between opening the dialog and now.
		m.warn(fmt.Sprintf("%s was already untracked", key))
		return
	}
	if err := saveState(m.st); err != nil {
		m.warn("could not save state: " + err.Error())
		return
	}
	m.info(fmt.Sprintf("%s is no longer tracked", key))
	m.rebuild()
}

// counts summarises the config for the status row.
func (m *Model) counts() string {
	var installed, missing, conflict int
	for _, r := range m.rows[tabConfig] {
		switch r.state {
		case stateInstalled:
			installed++
		case stateMissing:
			missing++
		case stateConflict:
			conflict++
		}
	}
	parts := []string{
		stGreen.Render(fmt.Sprintf("%d installed", installed)),
		stDim.Render(fmt.Sprintf("%d missing", missing)),
	}
	if conflict > 0 {
		parts = append(parts, stRed.Render(fmt.Sprintf("%d conflict", conflict)))
	}
	return strings.Join(parts, stDim.Render(" · "))
}

// tabAdapter is one tab as the shell sees it: its name in the strip, with the
// count that means "there is something here to deal with", and the legend.
type tabAdapter struct {
	m *Model
	t tab
}

func (a tabAdapter) Name() string {
	name := tabNames[a.t]
	// A badge only earns space when it means "there is something here to
	// deal with": what could be upgraded, and what drifted.
	if a.t == tabUpgradable || a.t == tabDrift {
		if n := len(a.m.rows[a.t]); n > 0 {
			return fmt.Sprintf("%s %d", name, n)
		}
	}
	return name
}

// Status is what the config adds up to, or what is happening to it now.
func (a tabAdapter) Status(shell.Context) string {
	m := a.m
	status := m.counts()
	if issues := m.sourceIssues(); issues != "" {
		status += stDim.Render("  ·  ") + stRed.Render("⚠ "+issues)
	}
	if !m.filtering && m.filter.Value() != "" {
		status = stDim.Render("filter: ") + m.filter.Value() + stDim.Render("   esc to clear")
	}
	return status
}

// Actions is the keys that do something on this tab rather than one fixed
// list that is half inapplicable. apptide handles them itself, so the shell
// only prints them after help, tab, the arrows and quit.
func (a tabAdapter) Actions(shell.Context) []shell.Action {
	shown := func(label, desc string, keys ...string) shell.Action {
		if len(keys) == 0 {
			keys = []string{label}
		}
		return shell.Custom(label, desc, nil, shell.WithKey(keys...), shell.DisplayOnly())
	}
	sel, fold, filter := shown("space", "select"), shown("←→", "fold", "left", "right"), shown("/", "filter")
	switch a.t {
	case tabConfig, tabDrift:
		return []shell.Action{sel, fold, shown("i", "install"), shown("r", "remove"), shown("x", "run config"),
			shown("a", "set source"), shown("f", "forget"), filter}
	case tabInstalled:
		return []shell.Action{sel, shown("A", "add to apptide config"), shown("y", "copy"), shown("f", "forget"), fold, filter}
	case tabUpgradable:
		return []shell.Action{sel, shown("i", "upgrade"), fold, filter}
	case tabActivity:
		return []shell.Action{shown("R", "refresh")}
	}
	return nil
}
