package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// NetworkPolicies counts cluster-wide policies and lists namespaces
// without any NetworkPolicy (excluding default and kube-system).
// One API call replaces the bash script's per-namespace kubectl loop.
type NetworkPolicies struct{}

// NewNetworkPolicies returns the check instance.
func NewNetworkPolicies() NetworkPolicies { return NetworkPolicies{} }

// Name implements Check.
func (NetworkPolicies) Name() string { return "Network Policies" }

// Run implements Check.
func (NetworkPolicies) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Network Policies"}

	nsList, err := c.Namespaces(ctx)
	if err != nil {
		res.Err = err
		return res
	}
	nps, err := c.NetworkPoliciesAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	covered := map[string]bool{}
	for _, np := range nps.Items {
		covered[np.Meta.Namespace] = true
	}

	var unprotected []string
	for _, ns := range nsList.Items {
		if ns.Meta.Name == "default" || ns.Meta.Name == "kube-system" {
			continue
		}
		if !covered[ns.Meta.Name] {
			unprotected = append(unprotected, ns.Meta.Name)
		}
	}

	res.Lines = append(res.Lines, Line{
		Key:    "Policies",
		Value:  fmt.Sprintf("%d across %d namespaces", len(nps.Items), len(nsList.Items)),
		Status: StatusInfo,
	})

	if len(unprotected) == 0 {
		res.Status = StatusOK
		res.Lines = append(res.Lines, Line{Key: "Unprotected", Value: "none (except default/kube-system)", Status: StatusOK})
	} else {
		res.Status = StatusWarn
		sort.Strings(unprotected)
		res.Lines = append(res.Lines, Line{
			Key:    "Without NP",
			Value:  strings.Join(unprotected, ", "),
			Status: StatusWarn,
		})
	}

	return res
}
