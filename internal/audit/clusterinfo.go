package audit

import (
	"context"
	"fmt"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// ClusterInfo mirrors the CLUSTER INFOS section of the original bash
// script: API version and basic object counts.
type ClusterInfo struct{}

// NewClusterInfo returns the check instance.
func NewClusterInfo() ClusterInfo { return ClusterInfo{} }

// Name implements Check.
func (ClusterInfo) Name() string { return "Cluster Info" }

// Run implements Check. Partial failures degrade the result instead of
// aborting: each subsection collects its own error as a WARN line.
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

	// Node count incl. Ready condition
	nodes, nodeErr := c.Nodes(ctx)
	if nodeErr != nil {
		res.Lines = append(res.Lines, Line{Key: "Nodes", Value: fmt.Sprintf("error: %v", nodeErr), Status: StatusWarn})
	}
	ready := 0
	for _, n := range nodes.Items {
		for _, cond := range n.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				ready++
				break
			}
		}
	}
	if nodeErr == nil {
		lineStatus := StatusOK
		if ready != len(nodes.Items) {
			lineStatus = StatusWarn
		}
		res.Lines = append(res.Lines, Line{Key: "Nodes", Value: fmt.Sprintf("%d total / %d ready", len(nodes.Items), ready), Status: lineStatus})
	}

	// Pod count
	if pods, err := c.PodsAll(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("%d", len(pods.Items)), Status: StatusOK})
	}

	// Score criterion: every node reports Ready
	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "All Nodes Ready",
		Passed:    nodeErr == nil && len(nodes.Items) > 0 && ready == len(nodes.Items),
		Detail:    fmt.Sprintf("%d/%d ready", ready, len(nodes.Items)),
	})

	return res
}
