// Package audit defines the check interface and result types.
//
// Design principle: checks produce DATA, rendering happens separately
// (internal/report). That is what enables the terminal and markdown
// outputs to share one source of truth later.
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
	StatusInfo // informational - neither good nor bad
)

// Line is one key/value detail row within a check result.
type Line struct {
	Key    string
	Value  string
	Status Status // coloring hint for the terminal renderer
}

// Result is the structured outcome of a single check.
type Result struct {
	Title  string
	Status Status // overall status
	Lines  []Line // detail rows
	Err    error  // non-nil if the check could not execute at all
	Scored bool   // whether this check contributes to the readiness score (stage 4)
}

// Check is one audit function. Each stage of the Bash-to-Go migration
// adds implementations of this interface.
type Check interface {
	Name() string
	Run(ctx context.Context, c *kube.Client) Result
}
