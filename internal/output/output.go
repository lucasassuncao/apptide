package output

import (
	"encoding/json"
	"fmt"
	"os"
)

// format holds the value of the --output flag, set by cmd/root.go via Set().
var format = "table"

// Set stores the output format chosen by the user. Called from cmd/root.go.
func Set(f string) { format = f }

// IsJSON returns true when the user requested JSON output.
func IsJSON() bool { return format == "json" }

// PrintJSON encodes v as indented JSON and writes it to stdout.
// Any encoding error is written to stderr.
func PrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "json encode error: %v\n", err)
	}
}
