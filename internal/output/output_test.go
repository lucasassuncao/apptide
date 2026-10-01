package output

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func TestSetSelectsJSONOnlyForJSON(t *testing.T) {
	t.Cleanup(func() { Set("table") })

	for _, c := range []struct {
		format string
		want   bool
	}{{"json", true}, {"table", false}, {"", false}, {"JSON", false}} {
		Set(c.format)
		if got := IsJSON(); got != c.want {
			t.Errorf("Set(%q): IsJSON() = %v, want %v", c.format, got, c.want)
		}
	}
}

func TestPrintJSONIndentsToStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	PrintJSON(map[string]int{"a": 1})
	w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	if want := "{\n  \"a\": 1\n}\n"; buf.String() != want {
		t.Errorf("PrintJSON wrote %q, want %q", buf.String(), want)
	}
}
