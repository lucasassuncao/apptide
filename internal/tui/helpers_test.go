package tui

import (
	"os"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/apptide/internal/state"
	"github.com/lucasassuncao/bezel/bezeltest"
	"github.com/lucasassuncao/bezel/legend"
	"github.com/lucasassuncao/bezel/shell"
)

// What the tests read of the model, named the way the old fields were.

type modeT = string

const (
	modeBrowse  modeT = "browse"
	modeFilter  modeT = "filter"
	modeConfirm modeT = "confirm"
	modeAdopt   modeT = "adopt"
	modeForget  modeT = "forget"
	modeRunning modeT = "running"
	modeHelp    modeT = "help"
)

func (m *Model) mode() modeT { return m.modeName() }

func (m *Model) statusText() string { s, _ := m.status(); return s }
func (m *Model) statusIsOK() bool   { _, ok := m.status(); return ok }

// forgetKey is what the forget dialog asks about, "" when it is not up.
func (m *Model) forgetKey() string {
	if m.dialog != "forget" {
		return ""
	}
	return m.pendingForget
}

func (m *Model) forgetEntry() state.Entry {
	e, _ := m.st.Get(m.forgetKey())
	return e
}

func (m *Model) resize(w, h int) { m.Update(tea.WindowSizeMsg{Width: w, Height: h}) }

// legendText is the tab's own keys as text.
func (m *Model) legendText() string {
	var keys []legend.Entry
	for _, a := range (tabAdapter{m: m, t: m.tab}).Actions(m.sh.Context()) {
		keys = append(keys, a.Entry())
	}
	return legend.HintLine(keys, legend.Style{})
}

// TestMain shrinks the status timer, which drain would otherwise wait out.
func TestMain(m *testing.M) {
	statusLife = time.Millisecond
	os.Exit(m.Run())
}

// newTestModel builds a model without touching the machine: an empty inventory
// snapshot stands in for the package managers.
func newTestModel(t *testing.T, apps []config.Application) *Model {
	t.Helper()
	m := newModel(Options{ConfigPath: "packages.yaml"},
		&config.Config{SchemaVersion: 2, Applications: apps},
		state.New(t.TempDir()+"/state.json"))
	m.snap = &inventory.Snapshot{}
	m.rebuild()
	m.resize(120, 40)
	return m
}

// press sends a key through the real Update path, so the mode dispatch is
// exercised rather than the handler being called directly.
func press(t *testing.T, m *Model, key string) *Model {
	t.Helper()
	return deliver(t, m, bezeltest.Key(key))
}

// deliver folds a message in and runs what it answered with: a dialog
// answers with the choice and its own close, both messages the model acts on.
func deliver(t *testing.T, m *Model, msg tea.Msg) *Model {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", next)
	}
	for _, out := range drain(cmd) {
		got = deliver(t, got, out)
	}
	return got
}

// drain runs a command down to its messages, batches included, skipping the
// timers that would only re-arm themselves.
func drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if shell.IsStatusExpiry(msg) {
		return nil // a timer, like the spinner's below
	}
	switch m := msg.(type) {
	case nil, spinner.TickMsg:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, drain(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func sampleApps() []config.Application {
	return []config.Application{
		{
			Name: "Git", Category: "Development", Description: "Version control",
			Source:  config.Sources{config.SourceWinget},
			Package: config.Packages{Winget: &config.WingetSpec{ID: "Git.Git"}},
		},
		{
			Name: "Lazygit", Category: "Development",
			Source: config.Sources{config.SourceWinget, config.SourceScoop, config.SourceGitHub},
			Package: config.Packages{
				Winget: &config.WingetSpec{ID: "JesseDuffield.lazygit"},
				Scoop:  &config.ScoopSpec{ID: "lazygit"},
				GitHub: &config.GitHubSpec{ID: "jesseduffield/lazygit"},
			},
		},
		{
			Name: "Docker", Category: "Development", Action: config.ActionSkip,
			Source:  config.Sources{config.SourceWinget},
			Package: config.Packages{Winget: &config.WingetSpec{ID: "Docker.DockerDesktop"}},
		},
	}
}
