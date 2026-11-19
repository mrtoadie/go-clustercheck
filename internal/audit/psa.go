package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// PSA reports Pod Security Admission labels per namespace.
// Only the enforce label is counted as "labeled" - warn/audit alone do
// not block anything.
type PSA struct{}

// NewPSA returns the check instance.
func NewPSA() PSA { return PSA{} }

// Name implements Check.
func (PSA) Name() string { return "Pod Security Admission" }

// Run implements Check.
func (PSA) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Pod Security Admission (PSA)"}

	nsList, err := c.Namespaces(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	var enforced []string
	for _, ns := range nsList.Items {
		// kube-* namespaces ship as privileged by design - expected, not a finding
		if v, ok := ns.Meta.Labels["pod-security.kubernetes.io/enforce"]; ok && v != "" {
			enforced = append(enforced, fmt.Sprintf("%s=%s", ns.Meta.Name, v))
		}
	}

	status := StatusWarn
	if len(enforced) >= 8 {
		status = StatusOK
	}
	res.Status = status

	sort.Strings(enforced)
	res.Lines = append(res.Lines, Line{
		Key:    "Enforce",
		Value:  fmt.Sprintf("%d/%d namespaces labeled", len(enforced), len(nsList.Items)),
		Status: StatusInfo,
	})
	if len(enforced) > 0 {
		res.Lines = append(res.Lines, Line{
			Key:    "Labeled",
			Value:  strings.Join(enforced, ", "),
			Status: StatusInfo,
		})
	}

	return res
}
