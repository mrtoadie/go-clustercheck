// Package main is the entry point for the clustercheck CLI.
//
// Stage 2.5 scope: adds the markdown renderer flag. Colors now disable
// themselves automatically when stdout is not a TTY.
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
	md := flag.Bool("md", false, "render output as markdown instead of terminal format")
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

	// One data source, two renderers - the whole point of the Go rewrite.
	if *md {
		report.RenderMarkdown(results, version)
	} else {
		report.RenderTerminal(results, version)
	}

	if failed {
		os.Exit(1)
	}
}
