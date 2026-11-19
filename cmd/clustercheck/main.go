// Package main is the entry point for the clustercheck CLI.
//
// Stage 1 scope: bootstrap a Kubernetes API client from the local
// kubeconfig (no client-go dependency) and run the first check
// (ClusterInfo). Later stages add more checks, scoring and renderers.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
	"github.com/mrtoadie/go-clustercheck/internal/kube"
	"github.com/mrtoadie/go-clustercheck/internal/report"
)

// version is injected at build time via -ldflags (see Makefile).
var version = "dev"

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	flag.Parse()

	client, err := kube.NewClient(*kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ failed to load kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// The check registry. Each stage of the migration appends here -
	// nothing else in main needs to change.
	checks := []audit.Check{
		audit.NewClusterInfo(),
		audit.NewStorage(),
		audit.NewPodIssues(),
		audit.NewPodRestarts(),
		audit.NewNetworkPolicies(),
		audit.NewPSA(),
	}

	ctx := context.Background()

	results := make([]audit.Result, 0, len(checks))
	failed := false
	for _, c := range checks {
		res := c.Run(ctx, client)
		if res.Status == audit.StatusFail {
			failed = true
		}
		results = append(results, res)
	}

	report.RenderTerminal(results, version)

	if failed {
		os.Exit(1)
	}
}
