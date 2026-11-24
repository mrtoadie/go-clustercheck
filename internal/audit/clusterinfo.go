package audit

import (
	"context"
	"fmt"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// ClusterInfo mirrors the CLUSTER INFO section: API version, object counts.
// Every subquery degrades to a WARN line on failure instead of crashing -
// the cluster being half-unreachable is a report, not a panic.
type ClusterInfo struct{}

// NewClusterInfo returns the check instance.
func NewClusterInfo() ClusterInfo { return ClusterInfo{} }

// Name implements Check.
func (ClusterInfo) Name() string { return "Cluster Info" }

// Run implements Check.
func (ClusterInfo) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{
		Title:  "Cluster Info",
		Status: StatusOK,
		Scored: false,
	}

	// API version
	v, err := c.Version(ctx)
	if err != nil {
		res.Lines = append(res.Lines, Line{Key: "K8s Version", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "K8s Version", Value: v.GitVersion, Status: StatusOK})
	}

	// Namespace count
	if ns, err := c.Namespaces(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "Namespaces", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "Namespaces", Value: fmt.Sprintf("%d", len(ns.Items)), Status: StatusOK})
	}

	// Node count incl. Ready condition.
	// NOTE: ready is declared OUTSIDE the error branch so the score
	// block below can reference it in both cases.
	nodes, nodeErr := c.Nodes(ctx)
	ready := 0
	total := 0
	if nodeErr != nil {
		res.Lines = append(res.Lines, Line{Key: "Nodes", Value: fmt.Sprintf("error: %v", nodeErr), Status: StatusWarn})
	} else {
		total = len(nodes.Items)
		for _, n := range nodes.Items {
			for _, cond := range n.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready++
					break
				}
			}
		}
		lineStatus := StatusOK
		if ready != total {
			lineStatus = StatusWarn
		}
		res.Lines = append(res.Lines, Line{Key: "Nodes", Value: fmt.Sprintf("%d total / %d ready", total, ready), Status: lineStatus})
	}

	// Pod count
	if pods, err := c.PodsAll(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("%d", len(pods.Items)), Status: StatusOK})
	}

	// Score criterion: every node reports Ready. Unknown state (fetch
	// error) counts as failed - silence is not a pass.
	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "All Nodes Ready",
		Passed:    nodeErr == nil && total > 0 && ready == total,
		Weight:    3,
		Detail:    fmt.Sprintf("%d/%d ready", ready, total),
	})

	return res
}
