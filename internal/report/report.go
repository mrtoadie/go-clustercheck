// Package report renders audit results. Stage 1 ships only the terminal
// renderer; the markdown renderer joins in stage 4 on the same Result data.
package report

import (
	"fmt"
	"os"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
)

// ANSI escape codes. A future stage can swap these for empty strings
// when stdout is not a TTY.
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorCyan   = "\033[0;36m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[0;33m"
	colorRed    = "\033[0;31m"
	colorDim    = "\033[2m"
)

func statusColor(s audit.Status) string {
	switch s {
	case audit.StatusOK:
		return colorGreen
	case audit.StatusWarn:
		return colorYellow
	case audit.StatusFail:
		return colorRed
	default:
		return colorDim
	}
}

func statusGlyph(s audit.Status) string {
	switch s {
	case audit.StatusOK:
		return "✓"
	case audit.StatusWarn:
		return "⚠"
	case audit.StatusFail:
		return "✗"
	default:
		return "·"
	}
}

// RenderTerminal writes all results in the familiar bash-script layout.
func RenderTerminal(results []audit.Result, version string) {
	w := os.Stdout

	fmt.Fprintf(w, "\n%s| go-clustercheck (v%s)%s\n\n", colorBold+colorCyan, version, colorReset)

	for _, res := range results {
		fmt.Fprintf(w, "%s| %s%s\n", colorBold+colorCyan, res.Title, colorReset)

		if res.Err != nil {
			fmt.Fprintf(w, "  %s✗ check failed: %v%s\n\n", colorRed, res.Err, colorReset)
			continue
		}

		for _, line := range res.Lines {
			fmt.Fprintf(w, "  %s%s %-14s%s %s\n",
				statusColor(line.Status), statusGlyph(line.Status), line.Key, colorReset, line.Value)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "%s%d check(s) executed.%s\n", colorDim, len(results), colorReset)
}
