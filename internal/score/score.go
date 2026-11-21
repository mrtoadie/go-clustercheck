// Package score aggregates check results into a production readiness
// score, mirroring the 13 criteria of the original bash script.
//
// It is intentionally dumb: checks decide pass/fail (they own the
// context), the score package counts, computes the percentage and
// buckets it into a color grade.
package score

import (
	"fmt"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
)

// Report is the aggregated score output.
type Report struct {
	Passed  int
	Total   int
	Percent int
	Entries []audit.ScoreImpact
}

// Aggregate collects all scored criteria from the results.
func Aggregate(results []audit.Result) Report {
	rep := Report{}
	for _, res := range results {
		for _, si := range res.Scores {
			if strings.TrimSpace(si.CheckName) == "" {
				continue
			}
			rep.Total++
			if si.Passed {
				rep.Passed++
			}
			rep.Entries = append(rep.Entries, si)
		}
	}
	if rep.Total == 0 {
		return rep
	}
	rep.Percent = rep.Passed * 100 / rep.Total
	return rep
}

// Grade returns a color code per score band (mirrors the fixed bash
// bug: bands match integers like 92 properly now).
func (r Report) Grade() string {
	switch {
	case r.Percent >= 90:
		return "green"
	case r.Percent >= 70:
		return "yellow"
	default:
		return "red"
	}
}

// Line is the single-line summary for the terminal footer.
func (r Report) Line() string {
	return fmt.Sprintf("Readiness Score: %d%% (%d/%d criteria passed) [grade: %s]",
		r.Percent, r.Passed, r.Total, r.Grade())
}
