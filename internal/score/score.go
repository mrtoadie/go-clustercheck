// Package score aggregates check results into a production readiness
// score. Criteria carry explicit weights: fundamental health (nodes,
// crashloops) outweighs hardening items (policies, hygiene).
//
// It is intentionally dumb: checks decide pass/fail (they own the
// context), the score package counts weighted points and renders.
package score

import (
	"fmt"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
)

// Report is the aggregated score output.
type Report struct {
	PassedPoints int // weighted points earned
	TotalPoints  int // weighted points possible
	Criteria     int // number of scored criteria
	Percent      int
	Entries      []audit.ScoreImpact
}

// weight normalizes an unset weight to 1.
func weight(si audit.ScoreImpact) int {
	if si.Weight <= 0 {
		return 1
	}
	return si.Weight
}

// Aggregate collects all scored criteria from the results.
func Aggregate(results []audit.Result) Report {
	rep := Report{}
	for _, res := range results {
		for _, si := range res.Scores {
			if strings.TrimSpace(si.CheckName) == "" {
				continue
			}
			rep.Criteria++
			rep.TotalPoints += weight(si)
			if si.Passed {
				rep.PassedPoints += weight(si)
			}
			rep.Entries = append(rep.Entries, si)
		}
	}
	if rep.TotalPoints == 0 {
		return rep
	}
	rep.Percent = rep.PassedPoints * 100 / rep.TotalPoints
	return rep
}

// Grade returns a color code per score band.
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
	return fmt.Sprintf("Readiness Score: %d%% (%d/%d weighted points, %d criteria) [grade: %s]",
		r.Percent, r.PassedPoints, r.TotalPoints, r.Criteria, r.Grade())
}
