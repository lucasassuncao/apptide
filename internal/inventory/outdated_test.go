package inventory

import "testing"

// The three managers print three different shapes, all with empty cells that
// break whitespace splitting. These fixtures are copied from real output.

func TestColumnStartsFindsLocalizedHeaders(t *testing.T) {
	header := "Nome                          ID                 Versão      Disponível  Origem"
	starts := columnStarts(header)

	if len(starts) != 5 {
		t.Fatalf("found %d columns in %q", len(starts), header)
	}
	if got := string([]rune(header)[starts[1] : starts[1]+2]); got != "ID" {
		t.Errorf("second column starts at %q", got)
	}
}

func TestSliceColumnsHandlesEmptyCells(t *testing.T) {
	// scoop status: adb has no versions, only a note.
	starts := dashGroupStarts("---- ----------------- -------------- -------------------- ----")
	line := "adb                                                        Install failed"

	cells := sliceColumns(line, starts)
	if len(cells) != 5 {
		t.Fatalf("got %d cells, want 5", len(cells))
	}
	if cells[0] != "adb" {
		t.Errorf("name = %q", cells[0])
	}
	if cells[1] != "" || cells[2] != "" {
		t.Errorf("version cells should be empty, got %q and %q", cells[1], cells[2])
	}
}

func TestSliceColumnsReadsAFullScoopRow(t *testing.T) {
	starts := dashGroupStarts("---- ----------------- -------------- -------------------- ----")
	cells := sliceColumns("vim  9.2.0782          9.2.0870", starts)

	if cells[0] != "vim" || cells[1] != "9.2.0782" || cells[2] != "9.2.0870" {
		t.Errorf("cells = %q", cells)
	}
}

func TestFindDashesRow(t *testing.T) {
	lines := []string{
		"Scoop is up to date.",
		"",
		"Name Installed Version Latest Version",
		"---- ----------------- --------------",
		"vim  9.2.0782          9.2.0870",
	}
	if got := findDashesRow(lines); got != 3 {
		t.Errorf("findDashesRow = %d, want 3", got)
	}
}

func TestFindHeaderAboveSolidDashRow(t *testing.T) {
	lines := []string{
		"Nome      ID        Versão",
		"--------------------------",
		"Git       Git.Git   2.54.0",
	}
	if got := findHeaderAbove(lines, '-'); got != 0 {
		t.Errorf("findHeaderAbove = %d, want 0", got)
	}
}
