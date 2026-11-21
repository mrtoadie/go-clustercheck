// Package audit defines the check interface and result types.
//
// Design principle: checks produce DATA, rendering happens separately
// (internal/report), and scoring is a pure aggregation (internal/score).
package audit

import (
	"context"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// Status classifies a check outcome.
type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
	StatusInfo
)

// Line is one key/value detail row within a check result.
type Line struct {
	Key    string
	Value  string
	Status Status // coloring hint for the terminal renderer
}

// ScoreImpact describes how a result contributes to the production
// readiness score. Passed/Failed is decided HERE by the check (which has
// full context), the score package only sums up and renders.
type ScoreImpact struct {
	CheckName string // human-readable check title, e.g. "All Nodes Ready"
	Passed    bool   // whether this particular criterion is satisfied
	Detail    string // short justification shown in the breakdown
	Weight    int    // relative importance; unset/0 is treated as 1
}

// Result is the structured outcome of a single check.
type Result struct {
	Title  string
	Status Status
	Lines  []Line
	Err    error
	Scored bool          // participates in the readiness score
	Scores []ScoreImpact // one entry per scored criterion (a check may contribute several)
}

// Check is one audit function.
type Check interface {
	Name() string
	Run(ctx context.Context, c *kube.Client) Result
}
