// Package report renders audit results in two formats from one data
// source: terminal (colors, auto-disabled when piped) and markdown.
package report

import (
	"fmt"
	"io"
	"os"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
	"github.com/mrtoadie/go-clustercheck/internal/score"
)

// useColor is true only when stdout is a TTY.
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

// RenderTerminal writes all results in the familiar bash-script layout,
// followed by the readiness score footer (if a score report is given).
func RenderTerminal(results []audit.Result, version string, scoreRep *score.Report) {
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

	RenderScoreFooter(w, scoreRep)
}

// RenderScoreFooter prints the readiness score as the report footer.
func RenderScoreFooter(w io.Writer, rep *score.Report) {
	if rep == nil || rep.Total == 0 {
		return
	}
	color := codeDim
	switch rep.Grade() {
	case "green":
		color = codeGreen
	case "yellow":
		color = codeYellow
	default:
		color = codeRed
	}
	fmt.Fprintf(w, "\n%s\n", paint(color, rep.Line()))
	fmt.Fprintf(w, "%s\n", paint(codeDim, "──────────────────────────────"))
	for _, e := range rep.Entries {
		glyph := "✗"
		c := codeRed
		if e.Passed {
			glyph = "✓"
			c = codeGreen
		}
		fmt.Fprintf(w, "  %s %s: %s\n", paint(c, glyph), e.CheckName, e.Detail)
	}
}
