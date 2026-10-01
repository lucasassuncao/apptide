package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/lucasassuncao/apptide/internal/config"
	"github.com/lucasassuncao/bezel/draw"
	"github.com/lucasassuncao/bezel/table"
)

func TestFilterNarrowsRowsAndKeepsCursorValid(t *testing.T) {
	m := newTestModel(t, sampleApps())

	m.setCursor(2)
	m.filter.SetValue("lazy")
	m.setCursor(m.cursor())

	rows := m.visibleRows()
	if len(rows) != 1 || rows[0].name() != "Lazygit" {
		t.Fatalf("filter returned %d rows: %+v", len(rows), rows)
	}
	// One group header plus the single match.
	if got := len(m.visibleItems()); got != 2 {
		t.Fatalf("visibleItems() = %d, want header + 1 row", got)
	}
	if m.cursor() != 1 {
		t.Errorf("cursor = %d, want clamped to the last item", m.cursor())
	}
}

func TestGroupingBuildsHeaders(t *testing.T) {
	apps := append(sampleApps(), config.Application{
		Name: "jq", Category: "CLITools",
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "jqlang.jq"}},
	})
	m := newTestModel(t, apps)

	items := m.visibleItems()
	var headers []string
	for _, it := range items {
		if it.header {
			headers = append(headers, it.group)
		}
	}
	if len(headers) != 2 || headers[0] != "Development" || headers[1] != "CLITools" {
		t.Fatalf("headers = %v, want [Development CLITools] in config order", headers)
	}
	if len(items) != len(apps)+2 {
		t.Errorf("items = %d, want %d rows plus 2 headers", len(items), len(apps))
	}
	if items[0].total != 3 {
		t.Errorf("Development header counts %d apps, want 3", items[0].total)
	}
}

func TestCollapseHidesRowsAndFilterReopensThem(t *testing.T) {
	m := newTestModel(t, sampleApps())

	m.setCursor(0) // the Development header
	m.foldLeft()

	if got := len(m.visibleItems()); got != 1 {
		t.Fatalf("collapsed group shows %d items, want just the header", got)
	}

	// A filter must not hide its own match behind a folded group.
	m.filter.SetValue("lazy")
	if got := len(m.visibleItems()); got != 2 {
		t.Errorf("filtering a collapsed group shows %d items, want header + match", got)
	}
}

// The arrows are directional, the way every tree view behaves: left only ever
// closes, right only ever opens. A single arrow that toggles cannot be
// predicted without first reading the header's arrow.
func TestArrowsAreDirectional(t *testing.T) {
	m := newTestModel(t, sampleApps())
	m.setCursor(0) // header, open

	m.foldRight() // open already: steps into the group
	if m.cursor() != 1 {
		t.Fatalf("right on an open header put the cursor at %d, want the first row", m.cursor())
	}
	if len(m.visibleItems()) == 1 {
		t.Fatal("right closed the group; it must only ever open")
	}

	m.foldLeft() // inside the group: steps out to the header
	if m.cursor() != 0 {
		t.Fatalf("left from inside put the cursor at %d, want the header", m.cursor())
	}
	if len(m.visibleItems()) == 1 {
		t.Fatal("left closed the group while stepping out; it should take two presses")
	}

	m.foldLeft() // on the header: closes
	if got := len(m.visibleItems()); got != 1 {
		t.Fatalf("second left shows %d items, want only the header", got)
	}

	m.foldLeft() // already closed: nothing left to do, must not reopen
	if got := len(m.visibleItems()); got != 1 {
		t.Fatalf("left on a closed group reopened it (%d items)", got)
	}

	m.foldRight() // closed: opens
	if len(m.visibleItems()) == 1 {
		t.Fatal("right did not reopen the closed group")
	}
}

func TestEnterTogglesTheGroup(t *testing.T) {
	m := newTestModel(t, sampleApps())

	m.setCursor(2) // a row inside the group
	m.toggleCollapseAtCursor()

	if got := len(m.visibleItems()); got != 1 {
		t.Fatalf("enter left %d items, want the group closed", got)
	}
	// The cursor was on a row that is now hidden, so it must have moved.
	if m.cursor() != 0 {
		t.Errorf("cursor = %d, want the header that now stands for the group", m.cursor())
	}
	if _, ok := m.currentRow(); ok {
		t.Error("currentRow returned a row while the cursor is on a header")
	}

	m.toggleCollapseAtCursor()
	if len(m.visibleItems()) == 1 {
		t.Error("enter did not reopen the group")
	}
}

// The counts belong next to the name, and the word has to match what the tab
// is listing: on the machine tabs every row is installed already, so counting
// installs would just repeat the total.
func TestGroupHeaderCounts(t *testing.T) {
	m := newTestModel(t, sampleApps())
	it := item{header: true, group: "Development", total: 3, installed: 1}

	// The config tabs are the config: every row there is managed by definition,
	// so the group has nothing to add that the columns do not already say.
	if got := ansi.Strip(m.renderHeaderLine(it, false)); got != " ▾ Development" {
		t.Errorf("config header = %q, want just the name", got)
	}

	m.setTab(tabInstalled)
	line := ansi.Strip(m.renderHeaderLine(it, false))
	if !strings.Contains(line, "Development (3 apps · 1 managed by apptide)") {
		t.Errorf("installed header = %q", line)
	}
	// The counts sit next to the name, not pushed to the far edge.
	if strings.Contains(line, "          ") {
		t.Errorf("counts are pushed away from the name: %q", line)
	}

	if got := m.groupCounts(item{total: 1, installed: 1}); got != "1 app · 1 managed by apptide" {
		t.Errorf("singular = %q", got)
	}
}

// Columns are sized to their contents, not to the terminal: a config of short
// names must not leave a 40-column gap before the Source title just because the
// window is wide.
func TestColumnsAreSizedToContent(t *testing.T) {
	m := newTestModel(t, sampleApps())

	_, _, wide := m.columnWidths(400)
	if wide[0] != len("Lazygit") {
		t.Errorf("name column is %d wide; the longest name is %q", wide[0], "Lazygit")
	}
	if wide[2] != len("Version Declared") {
		t.Errorf("declared column is %d wide, want the title width", wide[2])
	}

	// A long name widens the column, up to the cap.
	long := append(sampleApps(), config.Application{
		Name: strings.Repeat("x", 200), Category: "Development",
		Source:  config.Sources{config.SourceWinget},
		Package: config.Packages{Winget: &config.WingetSpec{ID: "x"}},
	})
	m2 := newTestModel(t, long)
	if _, _, got := m2.columnWidths(400); got[0] != maxNameCols {
		t.Errorf("name column is %d wide for a 200-character name, want the %d cap", got[0], maxNameCols)
	}
}

// TestHeaderLayout pins where each title sits.
//
// Name is at the left edge, level with the group headers, because the names
// are indented under their category and a title following that indent would
// float in the middle of the row. Source and Version have no hierarchy, so
// they do sit directly above their values.
func TestHeaderLayout(t *testing.T) {
	m := newTestModel(t, sampleApps())
	cols, specs, widths := m.columnWidths(120)

	header := ansi.Strip(strings.SplitN(m.renderList(120), "\n", 2)[0])
	row := ansi.Strip(m.renderRow(m.rows[tabConfig][0], false, specs, widths))
	groupLine := ansi.Strip(m.renderHeaderLine(item{header: true, group: "Development", total: 3}, false))

	at := func(s string, col int) string { return string([]rune(s)[col:]) }

	if !strings.HasPrefix(at(header, 1), "Name") {
		t.Errorf("Name title is not at the left edge: %q", header)
	}
	// Level with the group header's fold arrow.
	if arrowAt := strings.IndexAny(groupLine, "▾▸"); arrowAt != 1 {
		t.Errorf("group arrow is at column %d, want 1 so Name lines up with it", arrowAt)
	}

	srcCol := rowPrefix + widths[0] + table.Gap
	if !strings.HasPrefix(at(header, srcCol), "Source") {
		t.Errorf("Source title is not at column %d: %q", srcCol, header)
	}
	if got := at(row, srcCol); !strings.HasPrefix(got, "winget") {
		t.Errorf("source value is not at column %d: %q", srcCol, got)
	}

	verCol := srcCol + widths[1] + table.Gap
	// Declared comes before installed: it is the intent the installed value is
	// compared against.
	if !strings.HasPrefix(at(header, verCol), "Version Declared") {
		t.Errorf("third column is not Version Declared: %q", header)
	}
	if got := at(row, verCol); !strings.HasPrefix(got, "none (latest)") {
		t.Errorf("declared value is not at column %d: %q", verCol, got)
	}

	installedCol := verCol + widths[2] + table.Gap
	if !strings.HasPrefix(at(header, installedCol), "Version Installed") {
		t.Errorf("fourth column is not Version Installed: %q", header)
	}
	if len(cols) != 2+len(specs) {
		t.Errorf("%d columns for %d specs", len(cols), len(specs))
	}
}

func TestTruncateKeepsVisibleWidth(t *testing.T) {
	cases := []struct {
		in    string
		width int
	}{
		{"a short string", 20},
		{"a considerably longer string that must be cut", 12},
		{stGreen.Render("styled text that is long enough to cut"), 10},
	}
	for _, c := range cases {
		out := draw.Fit(c.in, c.width)
		if got := draw.Width(out); got != c.width {
			t.Errorf("fit(%q, %d) is %d columns wide", c.in, c.width, got)
		}
	}
}
