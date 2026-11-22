package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PodRestarts flags containers with more than 5 restarts and classifies
// them by severity instead of dumping a raw count:
//
//   - FAIL: dense AND recent restarts -> active crashloop, look NOW
//   - WARN: sustained restart rate >= 1/day over lifetime -> chronic instability
//   - INFO: high historic count on a long-lived pod -> statistics, not incidents
//
// Crashloop discrimination: on clusters that are powered down nightly,
// EVERY pod restarts once per boot. A single fresh lastState is therefore
// a boot artifact, not an incident. A container only counts as ACTIVE if
// its restarts are BOTH recent (< 6h) and dense (>= 1/day) - real
// crashloops repeat quickly, boot restarts do not.
//
// Both thresholds are fields, so they can be tuned by callers or tests.
type PodRestarts struct {
	RestartThreshold int           // restart count above which a container is inspected (default 5)
	RecentWindow     time.Duration // how recent the last restart must be for ACTIVE (default 6h)
	DenseRate        float64       // restarts per day required for ACTIVE (default 1.0)
}

// NewPodRestarts returns the check instance with defaults.
func NewPodRestarts() PodRestarts {
	return PodRestarts{
		RestartThreshold: 5,
		RecentWindow:     6 * time.Hour,
		DenseRate:        1.0,
	}
}

// Name implements Check.
func (PodRestarts) Name() string { return "Pod Stability (Restarts)" }

type restartEntry struct {
	ns, pod, container string
	count              int
	ageDays            float64
	rate               float64 // restarts per day over lifetime
	lastRestart        time.Time
	recent             bool // dense AND recent -> suspected active crashloop
	hasLastRestart     bool // whether lastState.terminated.finishedAt was parsable
}

// Run implements Check.
func (ch PodRestarts) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Pod Stability (Restarts > 5)"}

	pods, err := c.PodsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	now := time.Now()
	var entries []restartEntry

	for _, p := range pods.Items {
		created, cerr := time.Parse(time.RFC3339, p.Meta.CreationTimestamp)
		if cerr != nil {
			created = now // unparsable age -> fall back to raw count view
		}
		ageDays := now.Sub(created).Hours() / 24
		if ageDays < 1 {
			ageDays = 1 // avoid division blowup on fresh pods
		}

		for _, cs := range p.Status.ContainerStatuses {
			if cs.RestartCount <= ch.RestartThreshold {
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
				if ts, terr := time.Parse(time.RFC3339, cs.LastState.Terminated.FinishedAt); terr == nil {
					e.lastRestart = ts
					e.hasLastRestart = true
					// Crashloop discrimination: dense AND recent.
					// A nightly power cycle restarts every pod once per
					// day - recent alone is a boot artifact (see struct doc).
					e.recent = now.Sub(ts) < ch.RecentWindow && e.rate >= ch.DenseRate
				}
			}
			entries = append(entries, e)
		}
	}

	// Score criterion: no actively crashlooping containers. The same
	// dense+recent rule as above decides - boot artifacts do not fail.
	active := 0
	for _, e := range entries {
		if e.recent {
			active++
		}
	}
	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "No Active Crashloops",
		Passed:    active == 0,
		Weight:    3,
		Detail:    fmt.Sprintf("%d crashlooping containers (dense + recent)", active),
	})

	if len(entries) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{
			Key:    "Result",
			Value:  "no containers with excessive restarts",
			Status: StatusOK,
		})
		return res
	}

	// Overall status: FAIL if actively crashlooping, WARN if there is
	// chronic instability at all, OK otherwise (historic noise only).
	sort.Slice(entries, func(i, j int) bool { return entries[i].count > entries[j].count })
	res.Status = StatusWarn
	if active > 0 {
		res.Status = StatusFail
	}
	// Everything above threshold but neither dense nor >= 1/day -> INFO
	if active == 0 {
		denseHistoric := 0
		for _, e := range entries {
			if e.rate >= 1.0 {
				denseHistoric++
			}
		}
		if denseHistoric == 0 {
			res.Status = StatusInfo
		}
	}

	const limit = 10
	shown := entries
	if len(shown) > limit {
		shown = shown[:limit]
	}

	var parts []string
	for _, e := range shown {
		line := fmt.Sprintf("%s/%s (%s: %d, %.1f/day)", e.ns, e.pod, e.container, e.count, e.rate)
		if e.recent {
			line += " ACTIVE"
		} else if e.hasLastRestart {
			ago := now.Sub(e.lastRestart).Truncate(time.Hour)
			line += fmt.Sprintf(" (last %s ago)", ago)
		}
		parts = append(parts, line)
	}

	res.Lines = append(res.Lines, Line{
		Key:    "Containers",
		Value:  fmt.Sprintf("%d above threshold, %d crashlooping (dense + recent, top %d shown)", len(entries), active, len(shown)),
		Status: res.Status,
	})
	res.Lines = append(res.Lines, Line{
		Key:    "Details",
		Value:  strings.Join(parts, ", "),
		Status: StatusInfo,
	})

	return res
}
