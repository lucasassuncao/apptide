package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/apptide/internal/inventory"
	"github.com/lucasassuncao/bezel/draw"
	"github.com/lucasassuncao/bezel/layout"
	"github.com/lucasassuncao/bezel/shell"
)

// Layout constants. The detail pane is capped so a wide terminal grows the
// list rather than the description column.
const (
	minWidth      = 60
	maxDetailCols = 72
)

// Pane names, shared by the layout, the panes and the tests.
const (
	paneList     = "list"
	paneDetail   = "detail"
	paneActivity = "activity"
)

// layout is this frame's tree: the list beside the detail, or the activity
// log alone.
func (m *Model) layout() layout.Node {
	if m.tab == tabActivity {
		return layout.Fill(paneActivity)
	}
	return layout.Columns(layout.Fill(paneList), layout.Fixed(paneDetail, layout.Lines(m.detailWidth())))
}

// relayout hands the shell this frame's tree; called after every Update.
func (m *Model) relayout() { m.sh = m.sh.SetLayout(m.layout()) }

// listHeight is the rows the list has under its column header.
func (m *Model) listHeight() int {
	return max(3, draw.InnerRect(m.sh.Rect(paneList)).H-1)
}

// detailWidth splits the screen between the list and the detail pane.
//
// A third of the screen is the starting point, but the list has a ceiling: its
// name column is capped, so past a certain width it only gains blank space.
// Everything beyond what the list can use goes to the detail pane, where long
// values — a source line with a version and a binding marker, a homepage — were
// being truncated while half the screen sat empty.
func (m *Model) detailWidth() int {
	w := m.width / 3

	listNeeds := 8 + maxNameCols + 16 + 14 // chrome + name + source + version
	if slack := m.width - listNeeds; slack > w {
		w = slack
	}

	w = min(w, maxDetailCols, m.width/2)
	return max(w, 24)
}

func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m *Model) render() string {
	if m.quitting {
		return ""
	}
	if m.width < minWidth || m.height < 12 {
		return fmt.Sprintf("terminal too small: need at least %dx12\n", minWidth)
	}
	if m.loading && m.snap == nil {
		return fmt.Sprintf("\n  %s reading installed packages from winget, scoop and chocolatey…\n",
			m.sh.Spinner())
	}
	return m.sh.View(m.panes())
}

// panes is what goes inside the shell's panels this frame.
func (m *Model) panes() map[string]shell.Pane {
	if m.tab == tabActivity {
		title := "Activity"
		if m.running {
			title = "Activity  " + m.sh.Spinner() + " running"
		}
		return map[string]shell.Pane{paneActivity: {Title: title, Body: func(r layout.Rect) string {
			lines := m.activity
			if len(lines) == 0 {
				lines = []string{stDim.Render("no activity yet — select applications on the Config tab and press i")}
			}
			return strings.Join(draw.Tail(lines, r.H), "\n")
		}}}
	}

	list := shell.Pane{Title: m.listTitle(), Body: func(r layout.Rect) string {
		if m.tab == tabUpgradable && m.upgLoading && len(m.rows[tabUpgradable]) == 0 {
			return "\n  " + m.sh.Spinner() + stDim.Render(" asking winget, scoop and chocolatey what is outdated…")
		}
		return m.renderList(r.W)
	}}
	title, body := m.detailPanel()
	return map[string]shell.Pane{
		paneList:   list,
		paneDetail: {Title: title, Body: func(layout.Rect) string { return strings.Join(body, "\n") }},
	}
}

func (m *Model) listTitle() string {
	rows := m.visibleRows()
	total := len(m.rows[m.tab])
	if len(rows) != total {
		return fmt.Sprintf("%s  %d/%d", tabNames[m.tab], len(rows), total)
	}
	return fmt.Sprintf("%s  %d", tabNames[m.tab], total)
}

// detailPanel is the detail pane's title and lines, scrolled by detailOffset.
//
// Long descriptions used to be cut with no sign that anything followed; the
// title carries the position when there is more than fits.
func (m *Model) detailPanel() (string, []string) {
	r, ok := m.currentRow()
	if !ok {
		m.detailOffset = 0
		return "Detail", []string{stDim.Render("nothing selected")}
	}

	inner := draw.InnerRect(m.sh.Rect(paneDetail))
	body := renderDetail(r, m.st, m.snap, inner.W)
	visible := inner.H

	title := "Detail"
	if len(body) > visible {
		hidden := len(body) - visible
		m.detailOffset = min(max(m.detailOffset, 0), hidden)
		body = body[m.detailOffset : m.detailOffset+visible]

		title = fmt.Sprintf("Detail  %d↓", hidden-m.detailOffset)
		switch {
		case m.detailOffset == hidden:
			title = fmt.Sprintf("Detail  %d↑", m.detailOffset)
		case m.detailOffset > 0:
			title = fmt.Sprintf("Detail  %d↑ %d↓", m.detailOffset, hidden-m.detailOffset)
		}
	} else {
		m.detailOffset = 0
	}
	return title, body
}

// sourceIssues reports managers that are present but failed to answer.
//
// A failed query used to be invisible: the source simply contributed no rows,
// so "Upgradable 18" silently became "Upgradable 3" and nothing on screen said
// which manager had dropped out. Managers that are merely not installed are not
// issues and stay quiet.
func (m *Model) sourceIssues() string {
	var parts []string
	report := func(src config.Source, err error) {
		if err == nil || errors.Is(err, inventory.ErrManagerMissing) {
			return
		}
		parts = append(parts, string(src)+": "+err.Error())
	}

	for _, src := range config.AllSources() {
		if m.snap != nil {
			report(src, m.snap.Err(src))
		}
		if m.upg != nil && (m.tab == tabUpgradable || m.tab == tabConfig) {
			report(src, m.upg.Err(src))
		}
	}
	return strings.Join(parts, " · ")
}
