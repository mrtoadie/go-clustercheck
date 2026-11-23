package audit

import (
	"context"
	"fmt"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PDB reports PodDisruptionBudgets - the availability guardrails for
// voluntary disruptions (node drains, upgrades).
type PDB struct{}

// NewPDB returns the check instance.
func NewPDB() PDB { return PDB{} }

// Name implements Check.
func (PDB) Name() string { return "Disruption Budgets" }

// Run implements Check.
func (PDB) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Pod Disruption Budgets"}

	pdbs, err := c.PodDisruptionBudgetsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	blocked := 0
	var blockedNames []string
	for _, pdb := range pdbs.Items {
		if pdb.Status.DesiredHealthy > 0 && pdb.Status.DisruptionsAllowed <= 0 {
			blocked++
			if len(blockedNames) < 5 {
				blockedNames = append(blockedNames, fmt.Sprintf("%s/%s", pdb.Meta.Namespace, pdb.Meta.Name))
			}
		}
	}

	res.Status = StatusOK
	res.Lines = append(res.Lines, Line{
		Key:    "PDBs",
		Value:  fmt.Sprintf("%d configured", len(pdbs.Items)),
		Status: StatusInfo,
	})
	if blocked > 0 {
		res.Status = StatusWarn
		res.Lines = append(res.Lines, Line{
			Key:    "Blocking",
			Value:  fmt.Sprintf("%d PDBs allow zero disruptions: %v", blocked, blockedNames),
			Status: StatusWarn,
		})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "Disruption Budgets",
		Passed:    len(pdbs.Items) >= 2,
		Weight:    1,
		Detail:    fmt.Sprintf("%d PDBs, %d currently blocking", len(pdbs.Items), blocked),
	})

	return res
}
