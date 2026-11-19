package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PodRestarts flags containers with more than 5 restarts.
// Note: restartCount accumulates over the pod's whole lifetime - on a
// cluster that is powered down nightly, older pods rack up counts that
// are statistics, not incidents.
type PodRestarts struct{}

// NewPodRestarts returns the check instance.
func NewPodRestarts() PodRestarts { return PodRestarts{} }

// Name implements Check.
func (PodRestarts) Name() string { return "Pod Stability (Restarts)" }

// Run implements Check.
func (PodRestarts) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Pod Stability (Restarts > 5)"}

	pods, err := c.PodsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	type entry struct {
		ns, pod, container string
		count              int
	}
	var entries []entry
	for _, p := range pods.Items {
		for _, cs := range p.Status.ContainerStatuses {
			if cs.RestartCount > 5 {
				entries = append(entries, entry{p.Meta.Namespace, p.Meta.Name, cs.Name, cs.RestartCount})
			}
		}
	}

	if len(entries) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "no containers with excessive restarts", Status: StatusOK})
		return res
	}

	res.Status = StatusWarn
	sort.Slice(entries, func(i, j int) bool { return entries[i].count > entries[j].count })
	limit := len(entries)
	if limit > 10 {
		limit = 10
	}
	var parts []string
	for _, e := range entries[:limit] {
		parts = append(parts, fmt.Sprintf("%s/%s (%s: %d)", e.ns, e.pod, e.container, e.count))
	}
	res.Lines = append(res.Lines, Line{
		Key:    "Containers",
		Value:  fmt.Sprintf("%d above threshold (top %d): %s", len(entries), limit, strings.Join(parts, ", ")),
		Status: StatusWarn,
	})

	return res
}
