package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/lucasassuncao/apptide/internal/elevation"
	"github.com/lucasassuncao/apptide/internal/runner"
	"github.com/lucasassuncao/apptide/internal/state"
	"github.com/lucasassuncao/bezel/overlay"
)

// The dialogs are the shell's overlays. Each one draws what it asks about and
// answers with a message; the model acts on the message once the dialog is
// gone, so no handler here reaches into the model.

// confirmOverlay lists the queue and asks to apply it. A whole-config run can
// queue dozens of applications; listing them all would push the dialog past
// the screen and lose its own buttons.
func (m *Model) confirmOverlay(note string) overlay.Overlay {
	tail := m.confirmNotes(note)
	// The body less the dialog around the list: border and padding, the title
	// and the buttons with a blank each, the "and N more" row, and the notes.
	visible := max(3, m.sh.Body().H-9-len(tail))
	shown := m.queue
	hidden := 0
	if len(shown) > visible {
		hidden = len(shown) - visible
		shown = shown[:visible]
	}

	body := make([]string, 0, len(shown)+len(tail)+1)
	for _, item := range shown {
		chain := item.app.Source.String()
		if item.res.Kind != runner.ResolutionFresh {
			chain = string(item.res.Source)
		}
		verb := item.verb
		switch verb {
		case "remove":
			verb = stRed.Render(fmt.Sprintf("%-8s", verb))
		case "upgrade":
			verb = stCyan.Render(fmt.Sprintf("%-8s", verb))
		default:
			verb = stGreen.Render(fmt.Sprintf("%-8s", verb))
		}
		body = append(body, fmt.Sprintf("  %s %-22s %s", verb, item.app.Name, stDim.Render(chain)))
	}
	if hidden > 0 {
		body = append(body, stDim.Render(fmt.Sprintf("  … and %d more", hidden)))
	}
	body = append(body, tail...)

	apply := func(dry bool) tea.Cmd { return func() tea.Msg { return applyMsg{dryRun: dry} } }
	return overlay.NewChoice(fmt.Sprintf("Apply %d change(s)?", len(m.queue)), body, []overlay.Option{
		overlay.Opt("enter/y", "apply", apply(false)),
		overlay.Opt("d", "dry-run", apply(true)),
		overlay.Opt("esc/n/q", "cancel", nil),
	}, th.Modal, th.Legend)
}

// confirmNotes are the lines under the queue: what was left out, and the
// elevation warning.
func (m *Model) confirmNotes(note string) []string {
	var out []string
	if note != "" {
		out = append(out, "", "  "+stDim.Render("not queued: "+note))
	}
	// A UAC prompt opens outside the alt-screen, so an unelevated run of a
	// package that needs admin looks like the UI has frozen. Say so first.
	if elevation.IsElevated() {
		return out
	}
	var reasons []string
	seen := map[string]bool{}
	for _, item := range m.queue {
		if need, why := elevation.Needed(item.app, item.res.Source); need && !seen[why] {
			seen[why] = true
			reasons = append(reasons, why)
		}
	}
	if len(reasons) > 0 {
		out = append(out, "",
			"  "+stYellow.Render("⚠ not running as administrator"),
			"  "+stDim.Render(strings.Join(reasons, ", ")),
			"  "+stDim.Render("Windows will prompt outside this window, or the install will fail."))
	}
	return out
}

// adoptOverlay asks which source manages an application. It only records the
// choice: nothing is installed or removed.
func (m *Model) adoptOverlay(ctx adoptCtx) overlay.Overlay {
	m.pendingAdopt = ctx
	rows := make([]string, len(ctx.choices))
	for i, src := range ctx.choices {
		id, _ := ctx.app.Package.ID(src)
		rows[i] = fmt.Sprintf("%-12s %s", string(src), stDim.Render(id))
	}
	notes := []string{
		stDim.Render("This only records which manager owns it."),
		stDim.Render("Nothing is installed or removed."),
	}
	pick := func(i int) tea.Cmd { return func() tea.Msg { return adoptMsg{choice: i} } }
	return overlay.NewPick("Which source manages "+ctx.app.Name+"?", rows, notes, pick, th.Modal, th.Legend)
}

// forgetOverlay shows what is about to be forgotten and asks. Forgetting is
// not uninstalling: the software stays, apptide stops claiming it owns it.
func (m *Model) forgetOverlay(key string, entry state.Entry) overlay.Overlay {
	field := func(label, value string) string {
		if value == "" {
			value = "—"
		}
		return fmt.Sprintf("  %s %s", stDim.Render(fmt.Sprintf("%-12s", label)), value)
	}
	body := []string{
		field("source", string(entry.InstalledVia)),
		field("package id", entry.PackageID),
		field("version", entry.Version),
		"",
		"  " + stYellow.Render("Nothing is uninstalled."),
		"  " + stDim.Render("The software stays; apptide stops claiming it owns it."),
		"  " + stDim.Render("The next install re-decides the source from source:."),
	}
	forget := func() tea.Msg { return forgetMsg{key: key} }
	return overlay.NewChoice("Stop tracking "+key+"?", body, []overlay.Option{
		overlay.Opt("enter/y", "forget", forget),
		overlay.Opt("esc/n/q", "cancel", nil),
	}, th.Modal, th.Legend)
}

func (m *Model) helpOverlay() overlay.Overlay {
	entry := func(k, desc string) string {
		return fmt.Sprintf("  %s %s", stKey.Render(fmt.Sprintf("%-12s", k)), stDim.Render(desc))
	}
	lines := []string{
		stBold.Render("  Navigation"),
		entry("j / k", "move · g / G first / last · pgup / pgdn page"),
		entry("←", "close a category, or step out of it"),
		entry("J / K", "scroll the detail pane"),
		entry("→", "open a category, or step into it · enter toggles"),
		entry("tab / 1-5", "switch tab"),
		entry("/", "filter · esc clears it · folded groups open while filtering"),
		entry("R", "re-read installed packages"),
		"",
		stBold.Render("  Actions"),
		entry("space", "select · on a category header, selects all of it"),
		entry("i", "install or upgrade · r remove"),
		entry("x", "run the whole config, like apptide install"),
		entry("a", "set source: choose which manager owns the application"),
		entry("f", "forget: drop apptide's record — uninstalls nothing"),
		entry("A", "Installed tab: add the selection to the config file"),
		entry("i", "Upgradable tab: upgrade the declared applications"),
		entry("y", "Installed tab: copy the selection as config entries"),
		"",
		"  " + stDim.Render("The Installed tab does not install or remove: it lists what the"),
		"  " + stDim.Render("machine has. Select there and press A to declare it, or y to copy."),
		"",
		stBold.Render("  Glyphs"),
		"  " + stGreen.Render("●") + stDim.Render(" installed   ") +
			stDim.Render("○") + stDim.Render(" missing   ") +
			stRed.Render("!") + stDim.Render(" conflict"),
		"  " + stDim.Render("–") + stDim.Render(" skip        ") +
			stYellow.Render("⚠") + stDim.Render(" unavailable   ") +
			stYellow.Render("+") + stDim.Render(" not in config"),
		"  " + stYellow.Render("⇠") + stDim.Render(" installed by a fallback source"),
		"  " + stYellow.Render("★") + stDim.Render(" version pinned or upgrades disabled"),
		"",
		"  " + stDim.Render("The config file is edited with ") + stKey.Render("apptide edit") +
			stDim.Render(", which owns the file."),
	}
	return overlay.NewText("Help", lines, th.Modal, th.Legend)
}
