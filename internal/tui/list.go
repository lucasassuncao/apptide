package tui

import (
	"fmt"
	"strings"

	"github.com/lucasassuncao/bezel/draw"
	"github.com/lucasassuncao/bezel/table"
	"github.com/lucasassuncao/bezel/tree"
)

// The list is grouped rather than flat: apt has 96k packages and no grouping
// to offer, but an apptide config is a few dozen applications that already
// carry a category. Grouping also removes the Category column, which otherwise
// repeats the same value on every consecutive row.
//
// The grouping is a bezel tree one level deep - a header is a parent, an
// application is a leaf under it - so folding, the cursor and the scroll are
// the tree's. What is decided here is what the rows are and how they read.

// item is one line of the list: either a group header or an application.
type item struct {
	header bool

	// Header fields.
	group     string
	total     int
	installed int
	collapsed bool

	row row
}

// TreeInfo is what the bezel tree reads: headers fold, rows sit under them.
func (it item) TreeInfo() tree.Info {
	if it.header {
		return tree.Info{Depth: 0, Leaf: false, Expanded: !it.collapsed}
	}
	return tree.Info{Depth: 1, Leaf: true}
}

// Expand is the copy the tree stores when a header opens or closes.
func (it item) Expand(open bool) item {
	it.collapsed = !open
	return it
}

// groupKey is what a row is grouped under, per tab: category for the
// config-driven tabs, source for the machine listing.
func (m *Model) groupKey(r row) string {
	if m.tab == tabInstalled {
		return string(r.entrySrc)
	}
	return r.category
}

// visibleItems is what the list shows: headers plus the rows of the open
// groups, in order.
func (m *Model) visibleItems() []item {
	t := m.list()
	var out []item
	for _, i := range t.Visible() {
		out = append(out, t.Nodes[i])
	}
	return out
}

// allItems flattens the filtered rows into headers plus their rows,
// preserving the order categories first appear in the config. The tree hides
// what is under a collapsed header; this lists everything.
//
// While a filter is active collapsed groups are expanded: hiding a match the
// user just searched for would look like the filter is broken.
func (m *Model) allItems() []item {
	rows := m.visibleRows()
	filtering := strings.TrimSpace(m.filter.Value()) != ""

	var order []string
	byGroup := map[string][]row{}
	for _, r := range rows {
		key := m.groupKey(r)
		if _, seen := byGroup[key]; !seen {
			order = append(order, key)
		}
		byGroup[key] = append(byGroup[key], r)
	}

	var out []item
	for _, key := range order {
		group := byGroup[key]

		installed := 0
		for _, r := range group {
			if r.state == stateInstalled || r.state == stateTracked {
				installed++
			}
		}

		out = append(out, item{
			header:    true,
			group:     key,
			total:     len(group),
			installed: installed,
			collapsed: m.collapsed[m.tab][key] && !filtering,
		})
		for _, r := range group {
			out = append(out, item{row: r})
		}
	}
	return out
}

// list is the tree of the active tab, its nodes refreshed from the rows.
// Cursor and scroll survive the refresh; the cursor is clamped to what is
// left, which changes as the filter narrows and groups fold.
func (m *Model) list() tree.Model[item] {
	t, ok := m.lists[m.tab]
	if !ok {
		t = tree.New([]item(nil), 0).WithKeys(tree.Keys{Up: keyUp, Down: keyDown, Left: keyLeft, Right: keyRight})
		t.EmptyMessage = "  nothing to show"
	}
	t.Nodes = m.allItems()
	t.Height = m.listHeight()
	return t.ClampCursor().ClampOffset()
}

// setList stores the tree back and mirrors what folding changed into the
// per-tab map the rows are rebuilt from.
func (m *Model) setList(t tree.Model[item]) {
	m.lists[m.tab] = t
	// A filter forces every group open; what it shows says nothing about
	// what the user folded.
	if strings.TrimSpace(m.filter.Value()) != "" {
		return
	}
	for _, n := range t.Nodes {
		if n.header {
			m.setCollapsed(n.group, n.collapsed)
		}
	}
}

// currentItem returns the line under the cursor.
func (m *Model) currentItem() (item, bool) {
	t := m.list()
	idx := t.CurrentIdx()
	if idx < 0 {
		return item{}, false
	}
	return t.Nodes[idx], true
}

// currentRow returns the application under the cursor, if the cursor is on one
// rather than on a group header.
func (m *Model) currentRow() (row, bool) {
	it, ok := m.currentItem()
	if !ok || it.header {
		return row{}, false
	}
	return it.row, true
}

// cursor is the cursor of the active tab, over the visible rows.
func (m *Model) cursor() int { return m.list().Cursor }

func (m *Model) setCursor(c int) {
	t := m.list()
	t.Cursor = c
	m.detailOffset = 0 // a new row means a new detail body
	m.setList(t.ClampCursor().ClampOffset())
}

func (m *Model) move(delta int) { m.setCursor(m.cursor() + delta) }

// owningHeader returns the header the cursor belongs to: itself when the
// cursor is on a header, otherwise the one above the current row.
func (m *Model) owningHeader() (int, item, bool) {
	t := m.list()
	vis := t.Visible()
	i := t.Cursor
	if i < 0 || i >= len(vis) {
		return 0, item{}, false
	}
	for i > 0 && !t.Nodes[vis[i]].header {
		i--
	}
	if !t.Nodes[vis[i]].header {
		return 0, item{}, false
	}
	return i, t.Nodes[vis[i]], true
}

func (m *Model) setCollapsed(key string, collapsed bool) {
	if m.collapsed == nil {
		m.collapsed = map[tab]map[string]bool{}
	}
	if m.collapsed[m.tab] == nil {
		m.collapsed[m.tab] = map[string]bool{}
	}
	m.collapsed[m.tab][key] = collapsed
}

// foldLeft implements the left arrow, following the convention every tree view
// uses: close the group, or step out to its header when it is already closed
// or the cursor is inside it. It never opens anything.
func (m *Model) foldLeft() {
	idx, header, ok := m.owningHeader()
	if !ok {
		return
	}
	// Inside an open group: step out to the header first, so a second press
	// closes it. Jumping straight to closed would skip a position the user
	// may have been aiming for.
	if m.cursor() != idx {
		m.setCursor(idx)
		return
	}
	if !header.collapsed {
		m.setCollapsed(header.group, true)
	}
	m.setCursor(idx)
}

// foldRight implements the right arrow: open the group, or step into it when
// it is already open. It never closes anything.
func (m *Model) foldRight() {
	idx, header, ok := m.owningHeader()
	if !ok {
		return
	}
	if m.cursor() != idx {
		return // already inside the group
	}
	if header.collapsed {
		m.setCollapsed(header.group, false)
		m.setCursor(idx)
		return
	}
	// Open already: move onto its first application, if it has one.
	if idx+1 < len(m.list().Visible()) {
		m.setCursor(idx + 1)
	}
}

// toggleCollapseAtCursor is what enter does: flip the group the cursor is in.
func (m *Model) toggleCollapseAtCursor() {
	idx, header, ok := m.owningHeader()
	if !ok {
		return
	}
	m.setCollapsed(header.group, !header.collapsed)
	// Closing hides the rows under it, so the cursor lands on the header.
	m.setCursor(idx)
}

// selectGroup marks or clears every row of the group under the cursor.
func (m *Model) selectGroup(header item, selected bool) {
	for i := range m.rows[m.tab] {
		if m.groupKey(m.rows[m.tab][i]) == header.group {
			m.rows[m.tab][i].selected = selected
		}
	}
}

// groupAllSelected reports whether every row of the group is already marked,
// so space can toggle rather than only ever select.
func (m *Model) groupAllSelected(header item) bool {
	found := false
	for _, r := range m.rows[m.tab] {
		if m.groupKey(r) != header.group {
			continue
		}
		found = true
		if !r.selected {
			return false
		}
	}
	return found
}

// Columns. The table sizes each to its widest value rather than to the
// terminal: fixed widths meant a config of "Chrome" and "VS Code" left a
// 40-column gap before the Source title.

// rowPrefix is the width of everything before the Name column: the cursor bar,
// one space of indent under the group header, the [ ] mark, the state glyph,
// and the single space after each.
const rowPrefix = 1 + 1 + 3 + 1 + 1 + 1

// Bounds for the content-derived column widths. Version columns have no
// ceiling: version strings vary from "1.0" to "1.2.93.667.g7b5cc0cd", and a
// fixed cap only guaranteed the long ones would be unreadable.
const (
	maxNameCols   = 46
	maxSourceCols = 20
	minNameCols   = 12
)

// tableColumns are the columns of the active tab: Name and Source, then the
// tab's own. There is no Category column: grouping already carries it.
func (m *Model) tableColumns() ([]table.Column, []colSpec) {
	specs := columnSpecs(m.tab)
	cols := []table.Column{
		{Title: "Name", Min: minNameCols, Max: maxNameCols, Flex: true},
		{Title: "Source", Max: maxSourceCols},
	}
	for _, s := range specs {
		cols = append(cols, table.Column{Title: s.title, Max: s.cap})
	}
	return cols, specs
}

// cells are one row's values in column order.
func cells(r row, specs []colSpec) []string {
	out := []string{r.name(), r.sourceLabel()}
	for _, s := range specs {
		out = append(out, s.value(r))
	}
	return out
}

// columnWidths fits the active tab's columns to width, less the row prefix.
func (m *Model) columnWidths(width int) ([]table.Column, []colSpec, []int) {
	cols, specs := m.tableColumns()
	all := make([][]string, 0, len(m.rows[m.tab]))
	for _, r := range m.rows[m.tab] {
		all = append(all, cells(r, specs))
	}
	return cols, specs, table.Fit(cols, all, width-rowPrefix)
}

// renderList draws the column header plus the visible window of items.
func (m *Model) renderList(width int) string {
	cols, specs, widths := m.columnWidths(width)
	t := m.list()

	// "Name" sits at the left edge, lined up with the group headers, rather
	// than above the name values: the names are indented under their
	// category, so a title following that indent would float in the row.
	// Its cell spans the prefix too, so the other titles land on their values.
	name := draw.Fit("Name", rowPrefix-1+widths[0])
	rest := make([]string, 0, len(cols)-1)
	for _, c := range cols[1:] {
		rest = append(rest, c.Title)
	}
	header := th.TableHeader.Render(" " + name + strings.Repeat(" ", table.Gap) + table.Row(widths[1:], rest))

	body := t.View(th, func(it item, _ int, selected bool) string {
		if it.header {
			return m.renderHeaderLine(it, selected)
		}
		return m.renderRow(it.row, selected, specs, widths)
	})
	return header + "\n" + body
}

func (m *Model) renderRow(r row, cursor bool, specs []colSpec, widths []int) string {
	mark := "[ ]"
	if r.selected {
		mark = stAccent.Render("[x]")
	}
	pointer := " "
	if cursor {
		pointer = stAccent.Render("▌")
	}

	c := cells(r, specs)
	if cursor {
		c[0] = stCursor.Render(c[0])
	}
	// One space of indent under the group header, so the hierarchy reads
	// without needing a separator line between groups.
	return fmt.Sprintf("%s %s %s %s", pointer, mark, r.state.glyph(), table.Row(widths, c))
}

// renderHeaderLine draws a group header.
//
// The counts sit in parentheses next to the name rather than flush right: on a
// wide terminal the two were separated by half a screen of blank, which made
// the reader travel to find out what the number belonged to.
func (m *Model) renderHeaderLine(it item, cursor bool) string {
	arrow := "▾"
	if it.collapsed {
		arrow = "▸"
	}
	name := it.group
	if name == "" {
		name = "Uncategorized"
	}

	line := fmt.Sprintf(" %s %s", arrow, stBold.Render(name))
	if cursor {
		line = stAccent.Render(fmt.Sprintf(" %s ", arrow)) + stCursor.Render(name)
	}
	if counts := m.groupCounts(it); counts != "" {
		line += " " + stDim.Render("("+counts+")")
	}
	return line
}

// groupCounts summarises a group, on the tabs where the summary says something.
//
// The machine tabs list packages apptide may or may not know about, so "how
// many of these does it manage" is the question the group answers. The config
// tabs are the config: everything there is managed by definition, and the row
// columns already report each application's state.
func (m *Model) groupCounts(it item) string {
	if m.tab != tabInstalled && m.tab != tabUpgradable {
		return ""
	}
	noun := "apps"
	if it.total == 1 {
		noun = "app"
	}
	return fmt.Sprintf("%d %s · %d managed by apptide", it.total, noun, it.installed)
}
