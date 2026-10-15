package diagnose

import (
	"fmt"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

// Fires when the autoscaler cannot help anymore: it wants more
// replicas than allowed, or sits at max while the workload stays unhealthy.
type HPAAtMaxRule struct{}

func (HPAAtMaxRule) Name() string { return "hpa_at_max" }

func (HPAAtMaxRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, h := range s.HPAs {
		max := h.Spec.MaxReplicas
		if max < 1 {
			continue
		}
		current := h.Status.CurrentReplicas
		desired := h.Status.DesiredReplicas
		why := ""
		if desired > max {
			why = fmt.Sprintf("wants %d replicas but max is %d", desired, max)
		} else if current >= max && deploymentUnavailable(s, h.Namespace, h.Spec.ScaleTargetRef.Name) {
			why = fmt.Sprintf("at max (%d) and %s is still unhealthy", max, h.Spec.ScaleTargetRef.Name)
		}
		if why == "" {
			continue
		}
		min := int32(0)
		if h.Spec.MinReplicas != nil {
			min = *h.Spec.MinReplicas
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("horizontalpodautoscaler/%s", h.Name),
			Namespace: h.Namespace,
			Reason:    "hpa_at_max",
			Message:   fmt.Sprintf("Autoscaler %s — %s", h.Name, why),
			Evidence: map[string]any{
				"target":  h.Spec.ScaleTargetRef.Name,
				"min":     min,
				"max":     max,
				"current": current,
				"desired": desired,
			},
			Recommendation: "Raise maxReplicas if the nodes have headroom, or fix the underlying " +
				"unhealthiness the autoscaler is papering over — scaling broken pods only multiplies them.",
		})
	}
	return out
}

func deploymentUnavailable(s *k8s.Snapshot, ns, name string) bool {
	for _, d := range s.Deployments {
		if d.Namespace != ns || d.Name != name {
			continue
		}
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		return d.Status.ReadyReplicas < desired
	}
	for _, st := range s.StatefulSets {
		if st.Namespace != ns || st.Name != name {
			continue
		}
		desired := int32(1)
		if st.Spec.Replicas != nil {
			desired = *st.Spec.Replicas
		}
		return st.Status.ReadyReplicas < desired
	}
	return false
}

type HPANoopRule struct{}

func (HPANoopRule) Name() string { return "hpa_noop" }

func (HPANoopRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, h := range s.HPAs {
		min := int32(0)
		if h.Spec.MinReplicas != nil {
			min = *h.Spec.MinReplicas
		}
		msg := ""
		if min == h.Spec.MaxReplicas {
			msg = fmt.Sprintf("minReplicas == maxReplicas (%d) — the autoscaler can never scale", min)
		} else if !workloadExists(s, h.Namespace, h.Spec.ScaleTargetRef.Name) {
			msg = fmt.Sprintf("target %s %q does not exist", h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name)
		}
		if msg == "" {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("horizontalpodautoscaler/%s", h.Name),
			Namespace: h.Namespace,
			Reason:    "hpa_noop",
			Message:   msg,
			Recommendation: "Delete the HPA or point it at a live workload — " +
				"dead autoscalers hide in plain sight.",
		})
	}
	return out
}

func workloadExists(s *k8s.Snapshot, ns, name string) bool {
	for _, d := range s.Deployments {
		if d.Namespace == ns && d.Name == name {
			return true
		}
	}
	for _, st := range s.StatefulSets {
		if st.Namespace == ns && st.Name == name {
			return true
		}
	}
	return false
}

// Fires when a PodDisruptionBudget forbids every voluntary eviction
type PDBBlockingRule struct{}

func (PDBBlockingRule) Name() string { return "pdb_blocking" }

func (PDBBlockingRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, pdb := range s.PDBs {
		matched := podsMatchingPDB(s, &pdb)
		if len(matched) == 0 {
			continue
		}
		minAvail := resolveMinAvailable(&pdb, len(matched))
		if minAvail < len(matched) {
			continue
		}
		raw := ""
		if pdb.Spec.MinAvailable != nil {
			raw = pdb.Spec.MinAvailable.String()
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("poddisruptionbudget/%s", pdb.Name),
			Namespace: pdb.Namespace,
			Reason:    "pdb_blocking",
			Message:   fmt.Sprintf("PDB allows zero voluntary evictions (minAvailable %s covers all %d matching pods)", raw, len(matched)),
			Evidence: map[string]any{
				"min_available": raw,
				"matching_pods": len(matched),
			},
			Recommendation: "Lower minAvailable (or set maxUnavailable: 1) so drains and rollouts can proceed — " +
				"an un-evictable workload blocks every node operation beneath it.",
		})
	}
	return out
}

type PDBNoMatchRule struct{}

func (PDBNoMatchRule) Name() string { return "pdb_no_match" }

func (PDBNoMatchRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, pdb := range s.PDBs {
		if pdb.Spec.Selector == nil {
			continue
		}
		if len(podsMatchingPDB(s, &pdb)) > 0 {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("poddisruptionbudget/%s", pdb.Name),
			Namespace: pdb.Namespace,
			Reason:    "pdb_no_match",
			Message:   "PDB selector matches no pods — it protects nothing",
			Evidence: map[string]any{
				"selector": pdb.Spec.Selector.String(),
			},
			Recommendation: "Point the selector at the live workload labels, or delete the PDB — " +
				"a guarding-nothing budget is confusion waiting to happen.",
		})
	}
	return out
}

func podsMatchingPDB(s *k8s.Snapshot, pdb *policyv1.PodDisruptionBudget) []string {
	if pdb.Spec.Selector == nil {
		return nil
	}
	sel, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
	if err != nil {
		return nil
	}
	var out []string
	for _, p := range s.Pods {
		if p.Namespace != pdb.Namespace {
			continue
		}
		if sel.Matches(labels.Set(p.Labels)) {
			out = append(out, p.Name)
		}
	}
	return out
}

func resolveMinAvailable(pdb *policyv1.PodDisruptionBudget, matching int) int {
	if pdb.Spec.MinAvailable == nil {
		return 0
	}
	n, err := intstr.GetScaledValueFromIntOrPercent(pdb.Spec.MinAvailable, matching, true)
	if err != nil {
		return 0
	}
	return n
}
