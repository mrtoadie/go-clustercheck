// Package main is the entry point for the clustercheck CLI.
//
// Stage 4 scope: wires the readiness score into the output. Flags:
//
//	-noscore   skip the readiness score footer
//	-md        markdown renderer (score appended as its own section)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mrtoadie/go-clustercheck/internal/audit"
	"github.com/mrtoadie/go-clustercheck/internal/kube"
	"github.com/mrtoadie/go-clustercheck/internal/report"
	"github.com/mrtoadie/go-clustercheck/internal/score"
)

// version is injected at build time via -ldflags (see Makefile).
var version = "dev"

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG or ~/.kube/config)")
	md := flag.Bool("md", false, "render output as markdown instead of terminal format")
	noScore := flag.Bool("noscore", false, "skip the production readiness score")
	flag.Parse()

	client, err := kube.NewClient(*kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ failed to load kubeconfig: %v\n", err)
		os.Exit(1)
	}

	checks := []audit.Check{
		audit.NewClusterInfo(),
		audit.NewControlPlane(),
		audit.NewStorage(),
		audit.NewPodIssues(),
		audit.NewPodRestarts(),
		audit.NewCertificates(),
		audit.NewNetworkPolicies(),
		audit.NewPSA(),
		audit.NewPDB(),
		audit.NewQuotas(),
		audit.NewBackup(),
		audit.NewDaemonSets(),
		audit.NewServices(),
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

	// Aggregate the readiness score BEFORE rendering so both renderers
	// can consume it from the same data basis.
	// With -noscore we hand nil and the renderers skip the section.
	var scoreRep *score.Report
	if !*noScore {
		rep := score.Aggregate(results)
		scoreRep = &rep
	}

	if *md {
		report.RenderMarkdown(results, version, scoreRep)
	} else {
		report.RenderTerminal(results, version, scoreRep)
	}

	if failed {
		os.Exit(1)
	}
}
