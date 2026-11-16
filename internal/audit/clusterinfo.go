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
	nodes, err := c.Nodes(ctx)
	if err != nil {
		res.Lines = append(res.Lines, Line{Key: "Nodes", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		ready := 0
		for _, n := range nodes.Items {
			for _, cond := range n.Status.Conditions {
				if cond.Type == "Ready" && cond.Status == "True" {
					ready++
					break
				}
			}
		}
		status := StatusOK
		if ready != len(nodes.Items) {
			status = StatusWarn
		}
		res.Lines = append(res.Lines, Line{
			Key:    "Nodes",
			Value:  fmt.Sprintf("%d total / %d ready", len(nodes.Items), ready),
			Status: status,
		})
	}

	// Pod count
	if pods, err := c.PodsAll(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "Pods", Value: fmt.Sprintf("%d", len(pods.Items)), Status: StatusOK})
	}

	return res
}
