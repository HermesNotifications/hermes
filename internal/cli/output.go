package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func printRow(w io.Writer, cols ...any) {
	for i, col := range cols {
		if i > 0 {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprint(w, col)
	}
	fmt.Fprintln(w)
}
