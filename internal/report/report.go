// Package report renders audit results in two formats from one data
// source: terminal (colors, auto-disabled when piped) and markdown.
package report

import (
	"fmt"
	"os"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
)

// useColor is true only when stdout is a TTY. Piping into files or tools
// then produces clean output without escape sequences - same behavior
// as ls, grep and friends. No sed acrobatics needed anymore.
var useColor bool

func init() {
	fi, err := os.Stdout.Stat()
	useColor = err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

// ANSI escape codes
const (
	codeReset  = "\033[0m"
	codeBold   = "\033[1m"
	codeCyan   = "\033[0;36m"
	codeGreen  = "\033[0;32m"
	codeYellow = "\033[0;33m"
	codeRed    = "\033[0;31m"
	codeDim    = "\033[2m"
)

// paint wraps a code only when colors are enabled.
func paint(code, s string) string {
	if !useColor {
		return s
	}
	return code + s + codeReset
}

func statusColorCode(s audit.Status) string {
	switch s {
	case audit.StatusOK:
		return codeGreen
	case audit.StatusWarn:
		return codeYellow
	case audit.StatusFail:
		return codeRed
	default:
		return codeDim
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

	fmt.Fprintf(w, "\n%s\n\n", paint(codeBold+codeCyan, "| go-clustercheck ("+version+")"))

	for _, res := range results {
		fmt.Fprintf(w, "%s\n", paint(codeBold+codeCyan, "| "+res.Title))

		if res.Err != nil {
			fmt.Fprintf(w, "  %s\n\n", paint(codeRed, "✗ check failed: "+res.Err.Error()))
			continue
		}

		for _, line := range res.Lines {
			glyph := paint(statusColorCode(line.Status), statusGlyph(line.Status))
			key := paint(statusColorCode(line.Status), fmt.Sprintf("%-14s", line.Key))
			fmt.Fprintf(w, "  %s %s %s\n", glyph, key, line.Value)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "%s\n", paint(codeDim, fmt.Sprintf("%d check(s) executed.", len(results))))
}
