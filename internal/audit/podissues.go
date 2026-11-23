package audit

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PodIssues lists pods that are neither Running nor Succeeded, PLUS pods
// whose containers are stuck in a waiting state that means business
// (CrashLoopBackOff, ImagePullBackOff). A pod phase alone lies: it stays
// "Running" while the container restarts in circles.
type PodIssues struct{}

// NewPodIssues returns the check instance.
func NewPodIssues() PodIssues { return PodIssues{} }

// Name implements Check.
func (PodIssues) Name() string { return "Problem Pods" }

// blockingWaitingReasons are container waiting states that mean the
// workload is effectively broken, independent of the pod phase.
var blockingWaitingReasons = map[string]bool{
	"CrashLoopBackOff":           true,
	"ImagePullBackOff":           true,
	"ErrImagePull":               true,
	"CreateContainerConfigError": true,
}

// Run implements Check.
func (PodIssues) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Problem Pods (phase or container state)"}

	pods, err := c.PodsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	var problems []string
	for _, p := range pods.Items {
		// 1. Classic: pod phase is neither Running nor Succeeded
		if p.Status.Phase != "Running" && p.Status.Phase != "Succeeded" {
			problems = append(problems, fmt.Sprintf("%s/%s [%s]", p.Meta.Namespace, p.Meta.Name, p.Status.Phase))
			continue
		}
		// 2. Phase says Running, but a container is stuck in a blocking
		//    waiting state (CrashLoopBackOff & friends)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil && blockingWaitingReasons[cs.State.Waiting.Reason] {
				problems = append(problems, fmt.Sprintf("%s/%s [%s: %s]",
					p.Meta.Namespace, p.Meta.Name, cs.Name, cs.State.Waiting.Reason))
			}
		}
	}

	if len(problems) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "all pods Running or Succeeded, no blocking container states", Status: StatusOK})
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

	// Score criterion: same dual logic as the lines above
	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "No Problem Pods",
		Passed:    len(problems) == 0,
		Weight:    2,
		Detail:    fmt.Sprintf("%d pods broken by phase or container state", len(problems)),
	})

	return res
}
