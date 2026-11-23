package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// DaemonSets verifies that every DaemonSet is actually running on all
// nodes it wants: desiredNumberScheduled vs currentNumberScheduled vs
// numberAvailable. A gap here is the silent killer - the incident that
// spawned this check was a PSA label blocking node-exporter creation on
// the control-plane node while DESIRED=4 said everything was planned.
type DaemonSets struct{}

// NewDaemonSets returns the check instance.
func NewDaemonSets() DaemonSets { return DaemonSets{} }

// Name implements Check.
func (DaemonSets) Name() string { return "DaemonSet Completeness" }

// Run implements Check.
func (DaemonSets) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "DaemonSet Completeness"}

	dss, err := c.DaemonSetsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	incomplete := 0
	var details []string
	for _, ds := range dss.Items {
		// desired = 0 means the DaemonSet is scaled off (or waiting for
		// its first node match) - not a completeness failure
		if ds.Status.DesiredNumberScheduled == 0 {
			continue
		}
		if ds.Status.CurrentNumberScheduled < ds.Status.DesiredNumberScheduled ||
			ds.Status.NumberAvailable < ds.Status.DesiredNumberScheduled {
			incomplete++
			if len(details) < 8 {
				details = append(details, fmt.Sprintf("%s/%s: %d desired, %d current, %d available",
					ds.Meta.Namespace, ds.Meta.Name,
					ds.Status.DesiredNumberScheduled,
					ds.Status.CurrentNumberScheduled,
					ds.Status.NumberAvailable))
			}
		}
	}

	res.Lines = append(res.Lines, Line{
		Key:    "DaemonSets",
		Value:  fmt.Sprintf("%d total", len(dss.Items)),
		Status: StatusInfo,
	})

	if incomplete == 0 {
		res.Status = StatusOK
		if len(dss.Items) > 0 {
			res.Lines = append(res.Lines, Line{
				Key:    "Completeness",
				Value:  "all DaemonSets complete (current = desired = available)",
				Status: StatusOK,
			})
		}
	} else {
		res.Status = StatusWarn
		sort.Strings(details)
		res.Lines = append(res.Lines, Line{
			Key:    "Incomplete",
			Value:  fmt.Sprintf("%d DaemonSets with missing pods: %s", incomplete, strings.Join(details, ", ")),
			Status: StatusWarn,
		})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "DaemonSet Completeness",
		Passed:    incomplete == 0 && len(dss.Items) > 0,
		Weight:    2,
		Detail:    fmt.Sprintf("%d incomplete of %d DaemonSets", incomplete, len(dss.Items)),
	})

	return res
}
