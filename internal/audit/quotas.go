package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// Quotas reports ResourceQuota coverage. Quotas protect node capacity
// from runaway deployments - one per workload namespace is the baseline.
type Quotas struct{}

// NewQuotas returns the check instance.
func NewQuotas() Quotas { return Quotas{} }

// Name implements Check.
func (Quotas) Name() string { return "Resource Quotas" }

// Run implements Check.
func (Quotas) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Resource Quotas"}

	quotas, err := c.ResourceQuotasAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	nsSet := map[string]bool{}
	var nss []string
	for _, q := range quotas.Items {
		if !nsSet[q.Meta.Namespace] {
			nsSet[q.Meta.Namespace] = true
			nss = append(nss, q.Meta.Namespace)
		}
	}
	sort.Strings(nss)

	res.Status = StatusInfo
	res.Lines = append(res.Lines, Line{
		Key:    "Quotas",
		Value:  fmt.Sprintf("%d across %d namespaces", len(quotas.Items), len(nss)),
		Status: StatusInfo,
	})
	if len(nss) > 0 {
		res.Lines = append(res.Lines, Line{
			Key:    "Covered",
			Value:  strings.Join(nss, ", "),
			Status: StatusInfo,
		})
	}

	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "Resource Quotas",
		Passed:    len(quotas.Items) > 0,
		Weight:    1,
		Detail:    fmt.Sprintf("%d quotas in %d namespaces", len(quotas.Items), len(nss)),
	})

	return res
}
