package audit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mrtoadie/go-clustercheck/internal/kube"
)

// Storage mirrors the STORAGE STATUS section of the bash script:
// StorageClasses (incl. default), PV phases, released PV leftovers and
// pending PVCs.
type Storage struct{}

// NewStorage returns the check instance.
func NewStorage() Storage { return Storage{} }

// Name implements Check.
func (Storage) Name() string { return "Storage Status" }

// Run implements Check.
func (Storage) Run(ctx context.Context, c *kube.Client) Result {
	res := Result{Title: "Storage Status", Status: StatusOK}

	// StorageClasses
	if scs, err := c.StorageClasses(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "StorageClasses", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		res.Lines = append(res.Lines, Line{Key: "StorageClasses", Value: fmt.Sprintf("%d", len(scs.Items)), Status: StatusOK})
	}

	// Default StorageClass - the flag lives in annotations, fetched via a
	// dedicated ad-hoc decode (the shared StorageClassList stays lean).
	res.Lines = append(res.Lines, defaultStorageClassLines(ctx, c)...)

	// PersistentVolumes by phase
	pvs, err := c.PersistentVolumes(ctx)
	pvFailed, pvReleased := 0, 0

	if err != nil {
		res.Lines = append(res.Lines, Line{Key: "PVs", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		phases := map[string]int{}
		for _, pv := range pvs.Items {
			phases[pv.Status.Phase]++
		}
		pvFailed = phases["Failed"]
		pvReleased = phases["Released"]
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("%d total", len(pvs.Items)))
		for _, phase := range sortedKeys(phases) {
			sb.WriteString(fmt.Sprintf(" | %s=%d", phase, phases[phase]))
		}
		lineStatus := StatusOK
		if phases["Failed"] > 0 {
			lineStatus = StatusWarn
		}
		res.Lines = append(res.Lines, Line{Key: "PVs", Value: sb.String(), Status: lineStatus})

		// Released PVs are orphaned leftovers - typically from deleted
		// namespaces with Retain-style reclaim. Worth naming explicitly.
		if released := phases["Released"]; released > 0 {
			var names []string
			for _, pv := range pvs.Items {
				if pv.Status.Phase == "Released" && len(names) < 5 {
					if pv.Spec.ClaimRef != nil {
						names = append(names, fmt.Sprintf("%s (ex %s/%s)", pv.Meta.Name, pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name))
					} else {
						names = append(names, pv.Meta.Name)
					}
				}
			}
			res.Lines = append(res.Lines, Line{
				Key:    "Released PVs",
				Value:  fmt.Sprintf("%d - %s", released, strings.Join(names, ", ")),
				Status: StatusWarn,
			})
		}
	}

	// PVCs and pending ones
	if pvcs, err := c.PersistentVolumeClaimsAll(ctx); err != nil {
		res.Lines = append(res.Lines, Line{Key: "PVCs", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn})
	} else {
		pending := 0
		for _, pvc := range pvcs.Items {
			if pvc.Status.Phase == "Pending" {
				pending++
			}
		}
		lineStatus := StatusOK
		if pending > 0 {
			lineStatus = StatusWarn
		}
		res.Lines = append(res.Lines, Line{
			Key:    "PVCs",
			Value:  fmt.Sprintf("%d total | %d pending", len(pvcs.Items), pending),
			Status: lineStatus,
		})
	}

	// Score criterion: no failed or orphaned PVs. Unknown state (fetch
	// error) counts as failed - silence is not a pass.
	res.Scores = append(res.Scores, ScoreImpact{
		CheckName: "PV Hygiene",
		Passed:    err == nil && pvFailed == 0 && pvReleased == 0,
		Detail:    fmt.Sprintf("failed=%d released=%d", pvFailed, pvReleased),
		Weight:    1,
	})

	return res
}

// defaultStorageClassLines reads storage class annotations directly,
// because annotations live outside the shared lean list struct.
func defaultStorageClassLines(ctx context.Context, c *kube.Client) []Line {
	raw := struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}{}
	if err := c.GetJSON(ctx, "/apis/storage.k8s.io/v1/storageclasses", &raw); err != nil {
		return []Line{{Key: "Default SC", Value: fmt.Sprintf("error: %v", err), Status: StatusWarn}}
	}
	var defaults []string
	for _, sc := range raw.Items {
		if v, ok := sc.Metadata.Annotations["storageclass.kubernetes.io/is-default-class"]; ok && v == "true" {
			defaults = append(defaults, sc.Metadata.Name)
		}
	}
	if len(defaults) == 0 {
		return []Line{{Key: "Default SC", Value: "none", Status: StatusWarn}}
	}
	sort.Strings(defaults)
	return []Line{{Key: "Default SC", Value: strings.Join(defaults, ", "), Status: StatusOK}}
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
