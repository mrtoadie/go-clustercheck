package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// lbGracePeriod is how long a LoadBalancer service may lack an ingress IP
// before we consider it stuck (fresh services may still be provisioning).
const lbGracePeriod = 10 * time.Minute

// Services inspects every Service for common readiness issues:
//
//   - selector matches no ready pods ("dead service", traffic black-holes)
//   - LoadBalancer services that never received an ingress IP
//
// Deliberately NOT scored yet: dead services can be intentional or
// timing artifacts. Report-only for the first weeks, then decide on
// weighting based on real findings.
type Services struct{}

// NewServices returns the check instance.
func NewServices() Services { return Services{} }

// Name implements Check.
func (Services) Name() string { return "Services & Endpoints" }

// Run implements Check.
func (Services) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Services & Endpoints"}

	svcList, err := c.ServicesAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	epList, err := c.EndpointsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	// Index endpoints by ns/name so each service is a single map lookup.
	ready := make(map[string]bool, len(epList.Items))
	for _, ep := range epList.Items {
		ready[ep.Meta.Namespace+"/"+ep.Meta.Name] = hasReadyAddress(ep)
	}

	var dead, lbPending []string
	total := 0

	for _, svc := range svcList.Items {
		// Skip control-plane-managed services (most notably "kubernetes").
		if svc.Meta.Name == "kubernetes" && svc.Meta.Namespace == "default" {
			continue
		}
		total++

		switch svc.Spec.Type {
		case "ExternalName":
			// Points at a DNS name - no endpoints expected.
		case "LoadBalancer":
			if since := pendingSince(svc); since > lbGracePeriod {
				lbPending = append(lbPending,
					fmt.Sprintf("%s/%s (no ingress IP after %s)",
						svc.Meta.Namespace, svc.Meta.Name, since.Round(time.Second)))
			}
		default:
			if ready[svc.Meta.Namespace+"/"+svc.Meta.Name] {
				continue
			}
			// Selector-less services have hand-managed endpoints - we
			// cannot judge intent, so skip them.
			if len(svc.Spec.Selector) == 0 {
				continue
			}
			dead = append(dead,
				fmt.Sprintf("%s/%s (selector: %s)",
					svc.Meta.Namespace, svc.Meta.Name, selectorString(svc.Spec.Selector)))
		}
	}

	findings := len(dead) + len(lbPending)
	res.Status = statusFor(findings)

	res.Lines = append(res.Lines, Line{
		Key:    "Services",
		Value:  fmt.Sprintf("%d inspected | %d dead | %d LB pending", total, len(dead), len(lbPending)),
		Status: statusFor(findings),
	})

	if len(dead) > 0 {
		res.Lines = append(res.Lines, Line{Key: "No Endpoints", Value: formatNames(dead), Status: StatusWarn})
	}
	if len(lbPending) > 0 {
		res.Lines = append(res.Lines, Line{Key: "LB Pending", Value: formatNames(lbPending), Status: StatusWarn})
	}
	if total == 0 {
		res.Lines = append(res.Lines, Line{Key: "Note", Value: "no services found", Status: StatusInfo})
	}

	return res
}

// pendingSince returns how long a LoadBalancer service has gone without
// an ingress IP (zero if it has one, or if the timestamp is unparsable -
// we prefer silence over a false alarm).
func pendingSince(svc kube.Service) time.Duration {
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" || ing.Hostname != "" {
			return 0
		}
	}
	created, err := time.Parse(time.RFC3339, svc.Meta.CreationTimestamp)
	if err != nil {
		return 0
	}
	return time.Since(created)
}

// hasReadyAddress reports whether any endpoint subset has ready addresses.
func hasReadyAddress(ep kube.Endpoints) bool {
	for _, subset := range ep.Subsets {
		if len(subset.Addresses) > 0 {
			return true
		}
	}
	return false
}

// selectorString renders a selector map as k=v,k=v for readable findings.
func selectorString(m map[string]string) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

// statusFor maps a problem count to a result status.
func statusFor(problems int) Status {
	if problems > 0 {
		return StatusWarn
	}
	return StatusOK
}

// formatNames joins findings, capped at 8 entries to keep output readable
// (same convention as formatCerts in certificates.go).
func formatNames(items []string) string {
	limit := len(items)
	if limit > 8 {
		limit = 8
	}
	out := strings.Join(items[:limit], ", ")
	if len(items) > limit {
		out += fmt.Sprintf(" +%d more", len(items)-limit)
	}
	return out
}
