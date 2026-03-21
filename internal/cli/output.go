package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/fatih/color"
)

var (
	bold    = color.New(color.Bold).SprintFunc()
	header  = color.New(color.FgCyan, color.Bold).SprintFunc()
	label   = color.New(color.FgCyan).SprintFunc()
	dim     = color.New(color.Faint).SprintFunc()
	success = color.New(color.FgGreen).SprintFunc()
	warn    = color.New(color.FgYellow).SprintFunc()
)

// ansiRe matches ANSI escape sequences for stripping when calculating visible width.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleLen returns the display width of s, ignoring ANSI escape codes.
func visibleLen(s string) int {
	return utf8.RuneCountInString(ansiRe.ReplaceAllString(s, ""))
}

// padColor pads a colored string to width based on its visible length.
// This lets tabwriter-free column alignment work with ANSI codes.
func padColor(s string, width int) string {
	pad := width - visibleLen(s)
	if pad <= 0 {
		return s
	}
	return s + fmt.Sprintf("%*s", pad, "")
}

func colorStatus(s string) string {
	switch s {
	case "delivered", "read", "sent":
		return success(s)
	case "pending":
		return warn(s)
	case "archived":
		return dim(s)
	default:
		return s
	}
}

// fmtTime formats a timestamp string with a relative time suffix, e.g. "2026-03-21T17:48:28Z (5m ago)".
func fmtTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	ago := time.Since(t).Truncate(time.Second)
	var relative string
	switch {
	case ago < time.Minute:
		relative = fmt.Sprintf("%ds ago", int(ago.Seconds()))
	case ago < time.Hour:
		relative = fmt.Sprintf("%dm ago", int(ago.Minutes()))
	case ago < 24*time.Hour:
		relative = fmt.Sprintf("%dh ago", int(ago.Hours()))
	default:
		relative = fmt.Sprintf("%dd ago", int(ago.Hours()/24))
	}
	return fmt.Sprintf("%s %s", t.Local().Format("2006-01-02 15:04:05"), dim("("+relative+")"))
}

func colorSeverity(s string) string {
	switch s {
	case "info":
		return s
	case "warn", "warning":
		return warn(s)
	case "error":
		return color.RedString(s)
	default:
		return s
	}
}

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

// printTable prints rows with colored headers and proper alignment.
// It pre-computes column widths so ANSI codes don't affect alignment.
func printTable(w io.Writer, headers []string, rows [][]string) {
	// Compute max visible width per column.
	ncols := len(headers)
	widths := make([]int, ncols)
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i := 0; i < ncols && i < len(row); i++ {
			vl := visibleLen(row[i])
			if vl > widths[i] {
				widths[i] = vl
			}
		}
	}

	// Print header.
	for i, h := range headers {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, padColor(header(h), widths[i]))
	}
	fmt.Fprintln(w)

	// Print rows.
	for _, row := range rows {
		for i := 0; i < ncols && i < len(row); i++ {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			if i < ncols-1 {
				fmt.Fprint(w, padColor(row[i], widths[i]))
			} else {
				fmt.Fprint(w, row[i]) // last column: no padding
			}
		}
		fmt.Fprintln(w)
	}
}
