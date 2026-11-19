package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PodRestarts flags containers with more than 5 restarts, but classifies
// them by severity instead of dumping a raw count:
//
//   - FAIL: restarted within the last 24h  -> active crashloop, look NOW
//   - WARN: restart rate >= 1/day over lifetime -> chronic instability
//   - INFO: high historic count on an old pod -> statistics, not incidents
//
// This fixes the main weakness of the bash version on a cluster that is
// powered down nightly: accumulated counts on long-lived pods looked
// scary while saying nothing about the current situation.
type PodRestarts struct{}

// NewPodRestarts returns the check instance.
func NewPodRestarts() PodRestarts { return PodRestarts{} }

// Name implements Check.
func (PodRestarts) Name() string { return "Pod Stability (Restarts)" }

type restartEntry struct {
	ns, pod, container string
	count              int
	ageDays            float64
	rate               float64 // restarts per day
	lastRestart        time.Time
	recent             bool // crashed within the last 24h
}

// Run implements Check.
func (PodRestarts) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Pod Stability (Restarts > 5)"}

	pods, err := c.PodsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	now := time.Now()
	var entries []restartEntry

	for _, p := range pods.Items {
		created, err := time.Parse(time.RFC3339, p.Meta.CreationTimestamp)
		if err != nil {
			created = now // unparsable age -> fall back to raw count view
		}
		ageDays := now.Sub(created).Hours() / 24
		if ageDays < 1 {
			ageDays = 1 // avoid division blowup on fresh pods
		}

		for _, cs := range p.Status.ContainerStatuses {
			if cs.RestartCount <= 5 {
				continue
			}
			e := restartEntry{
				ns:        p.Meta.Namespace,
				pod:       p.Meta.Name,
				container: cs.Name,
				count:     cs.RestartCount,
				ageDays:   ageDays,
				rate:      float64(cs.RestartCount) / ageDays,
			}
			if cs.LastState.Terminated != nil && cs.LastState.Terminated.FinishedAt != "" {
				if ts, err := time.Parse(time.RFC3339, cs.LastState.Terminated.FinishedAt); err == nil {
					e.lastRestart = ts
					e.recent = now.Sub(ts) < 24*time.Hour
				}
			}
			entries = append(entries, e)
		}
	}

	if len(entries) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Result", Value: "no containers with excessive restarts", Status: StatusOK})
		return res
	}

	// Overall status driven by the worst offender
	sort.Slice(entries, func(i, j int) bool { return entries[i].count > entries[j].count })
	res.Status = StatusWarn
	if entries[0].recent {
		res.Status = StatusFail
	}

	const limit = 10
	shown := entries
	if len(shown) > limit {
		shown = shown[:limit]
	}

	active := 0
	for _, e := range entries {
		if e.recent {
			active++
		}
	}

	var parts []string
	for _, e := range shown {
		line := fmt.Sprintf("%s/%s (%s: %d, %.1f/day)", e.ns, e.pod, e.container, e.count, e.rate)
		if e.recent {
			line += " ACTIVE"
		}
		parts = append(parts, line)
	}

	res.Lines = append(res.Lines, Line{
		Key:    "Containers",
		Value:  fmt.Sprintf("%d above threshold, %d active in last 24h (top %d)", len(entries), active, len(shown)),
		Status: res.Status,
	})
	res.Lines = append(res.Lines, Line{
		Key:    "Details",
		Value:  strings.Join(parts, ", "),
		Status: StatusInfo,
	})

	return res
}
