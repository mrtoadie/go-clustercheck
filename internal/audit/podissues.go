package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PodIssues lists pods that are neither Running nor Succeeded.
// Completed (Succeeded) job pods are healthy and deliberately excluded -
// the old bash grep version used to count them as errors.
type PodIssues struct{}

// NewPodIssues returns the check instance.
func NewPodIssues() PodIssues { return PodIssues{} }

// Name implements Check.
func (PodIssues) Name() string { return "Problem Pods" }

// Run implements Check.
func (PodIssues) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Problem Pods (not Running/Succeeded)"}

	pods, err := c.PodsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	var problems []string
	for _, p := range pods.Items {
		if p.Status.Phase != "Running" && p.Status.Phase != "Succeeded" {
			problems = append(problems, fmt.Sprintf("%s/%s [%s]", p.Meta.Namespace, p.Meta.Name, p.Status.Phase))
		}
	}

	if len(problems) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "all pods Running or Succeeded", Status: StatusOK})
	} else {
		res.Status = StatusFail
		limit := len(problems)
		if limit > 10 {
			limit = 10
		}
		res.Lines = append(res.Lines, Line{
			Key:    "Pods",
			Value:  fmt.Sprintf("%d problem pods (showing %d): %s", len(problems), limit, strings.Join(problems[:limit], ", ")),
			Status: StatusFail,
		})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "No Problem Pods",
		Passed:    len(problems) == 0,
		Detail:    fmt.Sprintf("%d pods not Running/Succeeded", len(problems)),
	})

	return res
}
