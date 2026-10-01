package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lucasassuncao/apptide/internal/config"
)

// The confirmation must not grow past the screen: a whole-config run can queue
// dozens of applications, and a dialog taller than the terminal loses its own
// buttons.
func TestConfirmDialogFitsTheScreen(t *testing.T) {
	var apps []config.Application
	for i := 0; i < 60; i++ {
		apps = append(apps, config.Application{
			Name:    fmt.Sprintf("app-%02d", i),
			Source:  config.Sources{config.SourceWinget},
			Package: config.Packages{Winget: &config.WingetSpec{ID: fmt.Sprintf("Vendor.App%02d", i)}},
		})
	}
	m := newTestModel(t, apps)
	m.resize(100, 30)

	m.runConfig()
	view := m.render()

	if got := strings.Count(view, "\n") + 1; got > m.height {
		t.Errorf("view is %d lines with the dialog open, screen is %d", got, m.height)
	}
	if !strings.Contains(view, "and") || !strings.Contains(view, "more") {
		t.Error("the dialog does not say that entries were elided")
	}
	if !strings.Contains(view, "enter") {
		t.Error("the dialog lost its own key hints")
	}
	// Sized to the body, the queue is elided once, by the dialog, and never
	// clipped again by the box around it.
	if strings.Contains(view, "more lines, resize") {
		t.Errorf("the dialog overflowed the body:\n%s", view)
	}
}
