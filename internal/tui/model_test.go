package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/state"
	"github.com/lucasassuncao/bezel/bezeltest"
	"github.com/lucasassuncao/bezel/shell"
)

// screen is the whole frame as plain text.
func screen(m *Model) string {
	return ansi.Strip(m.View().Content)
}

// Whatever is running is named on the status row, in priority order, and the
// config's counts return once nothing is.
func TestStatusRowNamesWhatIsRunning(t *testing.T) {
	m := newTestModel(t, []config.Application{{Name: "Git", Category: "Development"}})
	counts := ansi.Strip(m.counts())

	// The upgrades check goes to the network on every start and after every
	// install: it must not hide an active filter or a broken source meanwhile.
	m.upgLoading = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	require(t, screen(m), counts)

	m.loading = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	require(t, screen(m), "refreshing…")

	m.running = true
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	require(t, screen(m), "running…")

	m.running, m.upgLoading = false, false
	m.Update(snapshotMsg{snap: &inventory.Snapshot{}})
	out := screen(m)
	for _, busy := range []string{"running…", "refreshing…"} {
		if strings.Contains(out, busy) {
			t.Errorf("status still says %q with nothing running", busy)
		}
	}
	require(t, out, counts)
}

// y hands the copy to the shell, off the event loop: the status reports it
// once the write lands, and the text is the config entries for the selection.
func TestYankCopiesTheSelectionThroughTheShell(t *testing.T) {
	var got string
	m := newTestModel(t, nil)
	m.sh = m.sh.WithClipboard(func(s string) error { got = s; return nil })
	m.setTab(tabInstalled)
	m.rows[tabInstalled] = []row{{entry: inventory.Entry{ID: "Git.Git"}, entrySrc: config.SourceWinget, selected: true}}

	m = deliver(t, m, bezeltest.Key("y"))

	require(t, got, "id: Git.Git")
	if !m.statusIsOK() || !strings.Contains(m.statusText(), "1 config entry copied") {
		t.Errorf("status = %q (ok=%v), want the copy confirmed", m.statusText(), m.statusIsOK())
	}
}

func TestYankReportsAClipboardFailure(t *testing.T) {
	m := newTestModel(t, nil)
	m.sh = m.sh.WithClipboard(func(string) error { return errors.New("xclip not found") })
	m.setTab(tabInstalled)
	m.rows[tabInstalled] = []row{{entry: inventory.Entry{ID: "Git.Git"}, entrySrc: config.SourceWinget, selected: true}}

	m = deliver(t, m, bezeltest.Key("y"))

	if m.statusIsOK() || m.statusText() != "could not reach the clipboard: xclip not found" {
		t.Errorf("status = %q (ok=%v), want the failure named", m.statusText(), m.statusIsOK())
	}
}

// tea v2 names the space bar "space", not " ": matching " " left the key the
// legend advertises doing nothing in a real terminal.
func TestSpaceBarSelects(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.setCursor(0) // the group header, which selects the whole group

	m = deliver(t, m, bezeltest.Key("space"))

	for _, r := range m.rows[tabConfig] {
		if !r.selected {
			t.Fatalf("%s was not selected by the space bar", r.name())
		}
	}
}

func require(t *testing.T, out, want string) {
	t.Helper()
	if !strings.Contains(out, want) {
		t.Fatalf("screen does not show %q:\n%s", want, out)
	}
}

// snapshotWith stands in for one manager reporting the given packages.
func snapshotWith(src config.Source, entries ...inventory.Entry) *inventory.Snapshot {
	return inventory.NewSnapshot(map[config.Source][]inventory.Entry{src: entries})
}

// focusRow moves the cursor off the group header onto the first application
// row. Rows are grouped by category, so position zero is a header, where the
// row-level keys deliberately do nothing.
func focusRow(t *testing.T, m *Model) *Model {
	t.Helper()
	for i := 0; i < len(m.visibleItems()); i++ {
		if _, ok := m.currentRow(); ok {
			return m
		}
		m = press(t, m, "j")
	}
	t.Fatalf("no application row on tab %s", tabNames[m.tab])
	return nil
}

// gitOnly is a single-application config, enough to put one row on the cursor.
func gitOnly() []config.Application {
	return []config.Application{{
		Name: "Git", Category: "Development",
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "Git.Git"}},
	}}
}

func TestForgetDropsTheBinding(t *testing.T) {
	m := newTestModel(t, gitOnly())
	m.st.Bind("Git", state.Entry{InstalledVia: "winget", PackageID: "Git.Git", Version: "2.51.0"})
	m.rebuild()

	m = focusRow(t, m)
	m = press(t, m, "f")
	if m.mode() != modeForget {
		t.Fatalf("f did not open the forget dialog (mode = %v)", m.mode())
	}
	if m.forgetKey() != "git" {
		t.Errorf("forgetKey = %q, want git", m.forgetKey())
	}
	// The dialog shows what is about to go, so the choice is informed.
	if m.forgetEntry().PackageID != "Git.Git" {
		t.Errorf("forgetEntry = %+v", m.forgetEntry())
	}

	m = press(t, m, "enter")
	if m.mode() != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse after confirming", m.mode())
	}
	if _, ok := m.st.Get("Git"); ok {
		t.Error("the entry survived a confirmed forget")
	}
	if !m.statusIsOK() || !strings.Contains(m.statusText(), "no longer tracked") {
		t.Errorf("status = %q (ok=%v)", m.statusText(), m.statusIsOK())
	}
}

// Forgetting is confirmed, not applied on the keystroke: the software stays and
// only apptide's record goes, which is easy to mistake for an uninstall.
func TestForgetCancelKeepsTheBinding(t *testing.T) {
	for _, key := range []string{"esc", "n", "q"} {
		t.Run(key, func(t *testing.T) {
			m := newTestModel(t, gitOnly())
			m.st.Bind("Git", state.Entry{InstalledVia: "winget", PackageID: "Git.Git"})
			m.rebuild()

			m = focusRow(t, m)
			m = press(t, m, "f")
			if m.mode() != modeForget {
				t.Fatalf("f did not open the dialog")
			}

			m = press(t, m, key)
			if m.mode() != modeBrowse {
				t.Errorf("mode = %v, want modeBrowse after %q", m.mode(), key)
			}
			if _, ok := m.st.Get("Git"); !ok {
				t.Errorf("%q dropped the binding anyway", key)
			}
			if m.forgetKey() != "" {
				t.Errorf("forgetKey should be cleared, got %q", m.forgetKey())
			}
		})
	}
}

// Pressing f on something apptide does not track must say so rather than open a
// dialog with nothing in it.
func TestForgetOnUntrackedRowReportsInsteadOfOpening(t *testing.T) {
	m := newTestModel(t, gitOnly())

	m = focusRow(t, m)
	m = press(t, m, "f")
	if m.mode() != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m.mode())
	}
	if m.statusIsOK() || !strings.Contains(m.statusText(), "not tracked") {
		t.Errorf("status = %q (ok=%v)", m.statusText(), m.statusIsOK())
	}
}

// The state keys on the config name, so an application dropped from the config
// leaves an entry nothing else can name. That orphan is the strongest reason to
// have forget at all, and it is only reachable from the Installed tab.
func TestForgetFindsAnOrphanedEntryFromTheInstalledTab(t *testing.T) {
	m := newTestModel(t, nil) // the config no longer declares it
	m.st.Bind("Lazygit", state.Entry{InstalledVia: "scoop", PackageID: "lazygit", Version: "0.42.0"})
	m.snap = snapshotWith(config.SourceScoop, inventory.Entry{ID: "lazygit", Version: "0.42.0"})
	m.rebuild()

	m.setTab(tabInstalled)
	if len(m.rows[tabInstalled]) == 0 {
		t.Fatal("the Installed tab has no rows to act on")
	}

	m = focusRow(t, m)
	m = press(t, m, "f")
	if m.mode() != modeForget {
		t.Fatalf("f did not resolve the orphan (mode = %v, status = %q)", m.mode(), m.statusText())
	}
	if m.forgetKey() != "lazygit" {
		t.Errorf("forgetKey = %q, want lazygit", m.forgetKey())
	}

	m = press(t, m, "enter")
	if _, ok := m.st.Get("Lazygit"); ok {
		t.Error("the orphaned entry survived")
	}
}

// A package the machine has but apptide never installed has no entry to drop.
func TestForgetIgnoresAPackageApptideNeverInstalled(t *testing.T) {
	m := newTestModel(t, nil)
	m.snap = snapshotWith(config.SourceScoop, inventory.Entry{ID: "ripgrep", Version: "14.1.0"})
	m.rebuild()

	m.setTab(tabInstalled)
	m = focusRow(t, m)
	m = press(t, m, "f")

	if m.mode() != modeBrowse {
		t.Errorf("mode = %v, want modeBrowse", m.mode())
	}
	if !strings.Contains(m.statusText(), "ripgrep") {
		t.Errorf("the message should name the row, got %q", m.statusText())
	}
}

// Matching an orphan goes through source and package id together, so the same
// id under a different manager is not mistaken for it.
func TestForgetOrphanMatchRequiresTheSameSource(t *testing.T) {
	m := newTestModel(t, nil)
	m.st.Bind("Lazygit", state.Entry{InstalledVia: "winget", PackageID: "lazygit"})
	m.snap = snapshotWith(config.SourceScoop, inventory.Entry{ID: "lazygit"})
	m.rebuild()

	m.setTab(tabInstalled)
	m = focusRow(t, m)
	m = press(t, m, "f")

	if m.mode() == modeForget {
		t.Errorf("a scoop row matched a winget entry (forgetKey = %q)", m.forgetKey())
	}
}

func TestForgetDialogRendersWhatIsLost(t *testing.T) {
	m := newTestModel(t, gitOnly())
	m.st.Bind("Git", state.Entry{InstalledVia: "winget", PackageID: "Git.Git", Version: "2.51.0"})
	m.rebuild()

	m = focusRow(t, m)
	m = press(t, m, "f")
	out := m.render()

	for _, want := range []string{"Stop tracking git?", "winget", "Git.Git", "2.51.0", "Nothing is uninstalled"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog missing %q\ngot:\n%s", want, out)
		}
	}
	if strings.Contains(out, "%!") {
		t.Errorf("format verb leaked into the dialog:\n%s", out)
	}
}

// The footer advertises f only where it can do something.
func TestFooterAdvertisesForget(t *testing.T) {
	m := newTestModel(t, gitOnly())

	for _, tb := range []tab{tabConfig, tabDrift, tabInstalled} {
		m.setTab(tb)
		if !strings.Contains(m.legendText(), "forget") {
			t.Errorf("tab %s does not advertise forget", tabNames[tb])
		}
	}
	m.setTab(tabActivity)
	if strings.Contains(m.legendText(), "forget") {
		t.Error("the Activity tab should not advertise forget")
	}
}

func TestSpaceOnHeaderSelectsWholeGroup(t *testing.T) {
	m := newTestModel(t, sampleApps())

	m.setCursor(0) // header
	m.toggleSelect()

	for _, r := range m.rows[tabConfig] {
		if !r.selected {
			t.Fatalf("%s was not selected by the group toggle", r.name())
		}
	}

	m.toggleSelect() // toggling again clears it
	for _, r := range m.rows[tabConfig] {
		if r.selected {
			t.Fatalf("%s stayed selected after the second toggle", r.name())
		}
	}
}

func TestSkippedApplicationIsNotStaged(t *testing.T) {
	m := newTestModel(t, sampleApps())

	// Select every row, including the skipped one.
	for i := range m.rows[tabConfig] {
		m.rows[tabConfig][i].selected = true
	}
	if m.stage(config.ActionInstall); m.mode() != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", m.mode())
	}
	if len(m.queue) != 3 {
		t.Fatalf("queue has %d items, want 3", len(m.queue))
	}
}

// x queues the whole config, taking each application's own action rather than
// forcing one — that is what `apptide install` does, and the point of having
// it here is not leaving the browser to run it.
func TestRunConfigQueuesEverythingActionable(t *testing.T) {
	apps := append(sampleApps(), config.Application{
		Name: "McAfee", Category: "System", Action: config.ActionUninstall,
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "McAfee.Security"}},
	})
	m := newTestModel(t, apps)

	if m.runConfig(); m.mode() != modeConfirm {
		t.Fatalf("mode = %v, want a confirmation", m.mode())
	}

	// Docker is action: skip, so it stays out; the other three are queued.
	if len(m.queue) != 3 {
		t.Fatalf("queue has %d items, want 3", len(m.queue))
	}
	verbs := map[string]string{}
	for _, item := range m.queue {
		verbs[item.app.Name] = item.verb
		if item.app.Name == "Docker" {
			t.Error("an application declared action: skip was queued")
		}
	}
	if verbs["McAfee"] != "remove" {
		t.Errorf("McAfee queued as %q, want remove — the action comes from the file", verbs["McAfee"])
	}
	if verbs["Git"] != "install" {
		t.Errorf("Git queued as %q", verbs["Git"])
	}
	if !strings.Contains(ansi.Strip(m.render()), "not queued:") {
		t.Error("the dialog should say what was left out")
	}
}

func TestRunConfigWithNothingToDo(t *testing.T) {
	m := newTestModel(t, []config.Application{{
		Name: "Docker", Action: config.ActionSkip,
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "Docker.DockerDesktop"}},
	}})

	if m.runConfig(); m.mode() == modeConfirm {
		t.Error("a config with nothing actionable should not open a confirmation")
	}
	if m.statusIsOK() || m.statusText() == "" {
		t.Error("the user should be told why nothing happened")
	}
}

func TestStageIsReadOnlyOnInstalledTab(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.setTab(tabInstalled)

	if m.stage(config.ActionInstall); m.mode() == modeConfirm {
		t.Error("staging was allowed on the read-only Installed tab")
	}
	if m.statusText() == "" {
		t.Error("no explanation shown when staging is refused")
	}
}

func TestAddToConfigAppendsAndRefreshes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packages.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 2\napplications:\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(t, nil)
	m.opts.ConfigPath = path
	m.setTab(tabInstalled)
	m.rows[tabInstalled] = []row{
		{entry: inventory.Entry{ID: "Google.Chrome"}, entrySrc: config.SourceWinget, selected: true},
		{entry: inventory.Entry{ID: "Mozilla.Firefox"}, entrySrc: config.SourceWinget, selected: true},
	}

	if m.addToConfig(); !m.statusIsOK() {
		t.Fatalf("addToConfig failed: %s", m.statusText())
	}

	cfg, err := config.LoadWithImports(path)
	if err != nil {
		out, _ := os.ReadFile(path)
		t.Fatalf("config does not parse after the append: %v\n%s", err, out)
	}
	if len(cfg.Applications) != 2 {
		t.Fatalf("config has %d applications, want 2", len(cfg.Applications))
	}
	if id, ok := cfg.Applications[0].Package.ID(config.SourceWinget); !ok || id != "Google.Chrome" {
		t.Errorf("first entry has id %q", id)
	}

	// The selection is consumed, so pressing A twice does not duplicate.
	for _, r := range m.rows[tabInstalled] {
		if r.selected {
			t.Error("selection survived the append")
		}
	}
}

func TestAddToConfigSkipsAlreadyDeclared(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packages.yaml")
	if err := os.WriteFile(path, []byte("applications:\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newTestModel(t, nil)
	m.opts.ConfigPath = path
	m.setTab(tabInstalled)
	m.rows[tabInstalled] = []row{{
		entry:      inventory.Entry{ID: "Google.Chrome"},
		entrySrc:   config.SourceWinget,
		configName: "Chrome", // already declared
		selected:   true,
	}}

	if m.addToConfig(); m.statusIsOK() {
		t.Error("an already-declared package was added again")
	}

	out, _ := os.ReadFile(path)
	if strings.Contains(string(out), "Google.Chrome") {
		t.Errorf("config was written anyway:\n%s", out)
	}
}

func TestAddToConfigIsRefusedOutsideInstalledTab(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.setTab(tabConfig)

	if m.addToConfig(); m.statusText() == "" || m.statusIsOK() {
		t.Error("A on the Config tab should explain why it does nothing")
	}
}

func TestYankIsRefusedOutsideInstalledTab(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.setTab(tabConfig)

	if m.yank(); m.statusText() == "" || m.statusIsOK() {
		t.Error("yank on the Config tab should explain why it does nothing")
	}
}

func TestTabNavigationWraps(t *testing.T) {
	m := newTestModel(t, sampleApps())

	// tab switches tabs; left/right are reserved for folding groups.
	m.setTab(tabOrder[len(tabOrder)-1])
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.tab != tabConfig {
		t.Errorf("tab = %v, want wrap to Config", m.tab)
	}

	m.handleKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if m.tab != tabConfig {
		t.Errorf("l changed the tab to %v; it should only fold groups", m.tab)
	}
}

// "?" opens the legend, and goes away while the filter takes it as text.
func TestLegendShowsHelpExceptWhileFiltering(t *testing.T) {
	m := newTestModel(t, sampleApps())
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "[?] help") {
		t.Fatalf("legend misses [?] help:\n%s", got)
	}
	m.openFilter()
	if got := ansi.Strip(m.View().Content); strings.Contains(got, "[?] help") {
		t.Errorf("legend offers [?] help while filtering:\n%s", got)
	}
}

// The legend reads like the other apps': help, tab, arrows and quit first,
// then this tab's own keys, none of them sharing a key.
func TestLegendPutsThePrebuiltKeysFirst(t *testing.T) {
	m := newTestModel(t, sampleApps())
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "[?] help  [tab] change tab  [↑/↓] move  [q] quit  [space] select") {
		t.Errorf("legend order:\n%s", got)
	}
	for _, tb := range tabOrder {
		if err := shell.Check(append(m.globalActions(), tabAdapter{m: m, t: tb}.Actions(m.sh.Context())...)...); err != nil {
			t.Errorf("%s: %v", tabNames[tb], err)
		}
	}
}
