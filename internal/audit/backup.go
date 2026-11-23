package audit

import (
	"context"
	"fmt"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// Backup verifies that disaster-recovery tooling is deployed - currently
// Velero, checked as a Deployment in the "velero" namespace. Volume-level
// backup validation (schedules, last success) needs the Velero API and
// stays on the roadmap.
type Backup struct{}

// NewBackup returns the check instance.
func NewBackup() Backup { return Backup{} }

// Name implements Check.
func (Backup) Name() string { return "Backup Tooling" }

// Run implements Check.
func (Backup) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Backup Tooling (Velero)"}

	deploys, err := c.DeploymentsIn(ctx, "velero")
	if err != nil {
		// namespace/deployments unreachable - treat as not deployed
		res.Status = StatusWarn
		res.Lines = append(res.Lines, Line{Key: "Velero", Value: "not found (no velero namespace or API error)", Status: StatusWarn})
		res.Scores = append(res.Scores, ScoreImpact{
			CheckName: "Backup Tooling",
			Passed:    false,
			Weight:    2,
			Detail:    "Velero not detected",
		})
		return res
	}

	veleroFound := false
	for _, d := range deploys.Items {
		if d.Meta.Name == "velero" {
			veleroFound = true
			break
		}
	}

	if veleroFound {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Velero", Value: "deployment found in namespace velero", Status: StatusOK})
	} else {
		res.Status = StatusWarn
		res.Lines = append(res.Lines, Line{Key: "Velero", Value: "namespace exists, but no velero deployment", Status: StatusWarn})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "Backup Tooling",
		Passed:    veleroFound,
		Weight:    2,
		Detail:    fmt.Sprintf("velero deployment present: %v", veleroFound),
	})

	return res
}
