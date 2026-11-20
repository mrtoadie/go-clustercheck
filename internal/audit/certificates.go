package audit

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// certEntry is one parsed TLS certificate from a secret.
// Named type at package level - Go treats two identical-looking anonymous
// structs as different types, so this must not be declared inline.
type certEntry struct {
	ns       string
	name     string
	notAfter time.Time
	daysLeft int
}

// Certificates inspects every kubernetes.io/tls secret in the cluster and
// reports expiration state. Replaces the bash openssl/jq pipeline with
// pure crypto/x509 parsing.
//
// Classification:
//
//	FAIL  - already expired
//	WARN  - expires within 30 days
//	OK    - healthy certs exist, none critical
type Certificates struct {
	// WarnWindowDays is the threshold for the "expiring soon" bucket.
	WarnWindowDays int
}

// NewCertificates returns the check instance with defaults.
func NewCertificates() Certificates { return Certificates{WarnWindowDays: 30} }

// Name implements Check.
func (Certificates) Name() string { return "Certificates & TLS" }

// Run implements Check.
func (ch Certificates) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Certificates & TLS"}

	secrets, err := c.SecretsAll(ctx)
	if err != nil {
		res.Err = err
		return res
	}

	now := time.Now()
	window := time.Duration(ch.WarnWindowDays) * 24 * time.Hour

	var expired, expiring, healthy []certEntry
	totalTLS := 0

	for _, s := range secrets.Items {
		if s.Type != "kubernetes.io/tls" {
			continue
		}
		totalTLS++

		crt, ok := s.Data["tls.crt"]
		if !ok || len(crt) == 0 {
			expiring = append(expiring, certEntry{ns: s.Meta.Namespace, name: s.Meta.Name + " (no tls.crt)", daysLeft: -1})
			continue
		}

		// A tls.crt may contain a full chain. The SERVER presents the leaf
		// first, so the first certificate determines when clients break.
		leaf := parseLeaf(crt)
		if leaf == nil {
			expiring = append(expiring, certEntry{ns: s.Meta.Namespace, name: s.Meta.Name + " (unparsable)", daysLeft: -1})
			continue
		}

		daysLeft := int(leaf.NotAfter.Sub(now).Hours() / 24)
		info := certEntry{ns: s.Meta.Namespace, name: s.Meta.Name, notAfter: leaf.NotAfter, daysLeft: daysLeft}

		switch {
		case daysLeft <= 0:
			expired = append(expired, info)
		case leaf.NotAfter.Sub(now) < window:
			expiring = append(expiring, info)
		default:
			healthy = append(healthy, info)
		}
	}

	// Summary line
	res.Lines = append(res.Lines, Line{
		Key: "TLS Secrets",
		Value: fmt.Sprintf("%d total | %d expired | %d expiring <%dd | %d healthy",
			totalTLS, len(expired), len(expiring), ch.WarnWindowDays, len(healthy)),
		Status: summaryStatus(len(expired), len(expiring)),
	})

	// Expired first - these break clients today
	if len(expired) > 0 {
		res.Status = StatusFail
		res.Lines = append(res.Lines, Line{Key: "Expired", Value: formatCerts(expired), Status: StatusFail})
	}

	// Then the soon-to-break
	if len(expiring) > 0 {
		if res.Status != StatusFail {
			res.Status = StatusWarn
		}
		res.Lines = append(res.Lines, Line{
			Key:    fmt.Sprintf("Expiring <%dd", ch.WarnWindowDays),
			Value:  formatCerts(expiring),
			Status: StatusWarn,
		})
	}

	if res.Status == StatusWarn || res.Status == StatusFail {
		return res
	}

	// Nothing critical - status OK with the nearest expiry as bonus info
	res.Status = StatusOK
	if len(healthy) > 0 {
		sort.Slice(healthy, func(i, j int) bool { return healthy[i].notAfter.Before(healthy[j].notAfter) })
		nearest := healthy[0]
		res.Lines = append(res.Lines, Line{
			Key:    "Next Expiry",
			Value:  fmt.Sprintf("%s/%s in %d days (%s)", nearest.ns, nearest.name, nearest.daysLeft, nearest.notAfter.Format("2006-01-02")),
			Status: StatusOK,
		})
	} else if totalTLS == 0 {
		res.Lines = append(res.Lines, Line{
			Key:    "Note",
			Value:  "no kubernetes.io/tls secrets found (plain HTTP or external termination?)",
			Status: StatusInfo,
		})
	}

	return res
}

// parseLeaf extracts the first (leaf) certificate from a PEM block.
func parseLeaf(pemBytes []byte) *x509.Certificate {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return cert
}

func summaryStatus(expired, expiring int) Status {
	if expired > 0 {
		return StatusFail
	}
	if expiring > 0 {
		return StatusWarn
	}
	return StatusOK
}

// formatCerts renders entries sorted by urgency (lowest days left first),
// capped at 8 names to keep output readable.
func formatCerts(entries []certEntry) string {
	sort.Slice(entries, func(i, j int) bool { return entries[i].daysLeft < entries[j].daysLeft })
	limit := len(entries)
	if limit > 8 {
		limit = 8
	}
	var parts []string
	for _, e := range entries[:limit] {
		parts = append(parts, fmt.Sprintf("%s/%s (%dd)", e.ns, e.name, e.daysLeft))
	}
	suffix := ""
	if len(entries) > limit {
		suffix = fmt.Sprintf(" +%d more", len(entries)-limit)
	}
	return strings.Join(parts, ", ") + suffix
}
