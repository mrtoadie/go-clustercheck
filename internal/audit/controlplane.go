package audit

import (
	"context"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// ControlPlane inspects /readyz?verbose - the API server's own component
// health report. Missing entries (e.g. standalone scheduler/controller-
// manager checks absent on k3s) are informational, NOT failures: the
// endpoint only lists checks this API server actually runs.
type ControlPlane struct{}

// NewControlPlane returns the check instance.
func NewControlPlane() ControlPlane { return ControlPlane{} }

// Name implements Check.
func (ControlPlane) Name() string { return "Control Plane Health" }

// Run implements Check.
func (ControlPlane) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Control Plane Health (/readyz)"}

	readyz, err := c.RawText(ctx, "/readyz?verbose")
	if err != nil {
		res.Status = StatusFail
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "unreachable: " + err.Error(), Status: StatusFail})
		res.Scores = append(res.Scores, ScoreImpact{
			CheckName: "API Server Ready",
			Passed:    false,
			Weight:    3,
			Detail:    "readyz unreachable",
		})
		return res
	}

	// Aggregate: the last line of verbose output is "readyz check passed"
	passed := strings.Contains(readyz, "check passed")
	failing := []string{}
	for _, line := range strings.Split(readyz, "\n") {
		if strings.HasPrefix(line, "[-]") {
			failing = append(failing, strings.TrimSpace(line))
		}
	}

	switch {
	case passed && len(failing) == 0:
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "readyz check passed", Status: StatusOK})
	case passed:
		res.Status = StatusWarn
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "readyz passed, but individual checks failing:", Status: StatusWarn})
	default:
		res.Status = StatusFail
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "readyz check FAILED", Status: StatusFail})
	}

	if len(failing) > 0 {
		limit := len(failing)
		if limit > 5 {
			limit = 5
		}
		res.Lines = append(res.Lines, Line{
			Key:    "Failing",
			Value:  strings.Join(failing[:limit], ", "),
			Status: StatusFail,
		})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "API Server Ready",
		Passed:    passed && len(failing) == 0,
		Weight:    3,
		Detail:    "readyz check passed",
	})

	return res
}
