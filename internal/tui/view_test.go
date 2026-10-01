package tui

import (
	"strings"
	"testing"
)

func TestViewRendersEveryTab(t *testing.T) {
	m := newTestModel(t, sampleApps())

	for _, tb := range tabOrder {
		m.setTab(tb)
		out := m.render()
		if out == "" {
			t.Errorf("tab %s rendered nothing", tabNames[tb])
		}
		if strings.Contains(out, "%!") {
			t.Errorf("tab %s has a formatting bug: %s", tabNames[tb], out)
		}
	}
}

// TestViewSurvivesNarrowAndShortTerminals guards the layout arithmetic: the
// column widths are derived by subtraction, which is exactly where a small
// terminal produces a negative width and a panic.
func TestViewSurvivesNarrowAndShortTerminals(t *testing.T) {
	m := newTestModel(t, sampleApps())

	sizes := []struct{ w, h int }{
		{60, 12}, {61, 13}, {80, 24}, {200, 60}, {40, 10}, {120, 12},
	}
	for _, s := range sizes {
		m.resize(s.w, s.h)
		for _, tb := range tabOrder {
			m.setTab(tb)
			_ = m.render() // must not panic
		}
	}
}

// TestOverlayIsCompositedNotAppended guards the modal behaviour: a dialog must
// sit on top of the screen. Appending it below pushed the list out of view and
// made the screen scroll, which is what it used to do.
func TestOverlayIsCompositedNotAppended(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.resize(100, 30)

	base := strings.Count(m.render(), "\n")

	m.queue = []queueItem{{app: sampleApps()[0], verb: "install"}}
	m.openDialog("confirm", m.confirmOverlay(""))
	withDialog := m.render()

	if got := strings.Count(withDialog, "\n"); got > m.height {
		t.Errorf("view grew to %d lines with a dialog open (screen is %d)", got+1, m.height)
	}
	if !strings.Contains(withDialog, "Apply 1 change(s)?") {
		t.Fatal("dialog is not in the composited view")
	}

	// The dialog must not land on the first line: it is centred, and the
	// header has to stay readable behind it.
	lines := strings.Split(withDialog, "\n")
	for i, l := range lines {
		if strings.Contains(l, "Apply 1 change(s)?") {
			if i < 2 {
				t.Errorf("dialog starts at line %d, want it centred", i)
			}
			break
		}
	}
	_ = base
}

func TestDetailPaneTakesTheSlackOnWideTerminals(t *testing.T) {
	m := newTestModel(t, sampleApps())

	m.resize(120, m.height)
	normal := m.detailWidth()

	m.resize(250, m.height)
	wide := m.detailWidth()

	if wide <= normal {
		t.Errorf("detail pane stayed at %d columns on a 250-column screen (was %d at 120)", wide, normal)
	}
	if wide > maxDetailCols {
		t.Errorf("detail pane is %d columns, above the %d cap", wide, maxDetailCols)
	}
	if wide > m.width/2 {
		t.Errorf("detail pane took %d of %d columns — more than half the screen", wide, m.width)
	}

	// Narrow screens must still leave the list the bulk of the room.
	m.resize(80, m.height)
	if narrow := m.detailWidth(); narrow > m.width/2 {
		t.Errorf("detail pane took %d of 80 columns", narrow)
	}
}
