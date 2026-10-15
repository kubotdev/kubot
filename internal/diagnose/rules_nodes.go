package diagnose

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

type NodePressureRule struct{}

func (NodePressureRule) Name() string { return "node_pressure" }

func (NodePressureRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, n := range s.Nodes {
		var pressures []string
		for _, c := range n.Status.Conditions {
			switch c.Type {
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
				if c.Status == corev1.ConditionTrue {
					pressures = append(pressures, string(c.Type))
				}
			}
		}
		if len(pressures) == 0 {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("node/%s", n.Name),
			Namespace: "",
			Reason:    "node_pressure",
			Message:   fmt.Sprintf("Node under pressure: %s", joinList(pressures)),
			Evidence: map[string]any{
				"pressures": pressures,
			},
			Recommendation: "Pods on this node risk eviction and OOMKills; drain or add capacity, and check what's consuming the pressured resource.",
		})
	}
	return out
}

const NotReadyGrace = 5 * time.Minute

type NodeNotReadyRule struct{}

func (NodeNotReadyRule) Name() string { return "node_notready" }

func (NodeNotReadyRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, n := range s.Nodes {
		var ready *corev1.NodeCondition
		for i := range n.Status.Conditions {
			if n.Status.Conditions[i].Type == corev1.NodeReady {
				ready = &n.Status.Conditions[i]
				break
			}
		}
		if ready == nil || ready.Status == corev1.ConditionTrue {
			continue
		}
		if since := time.Since(ready.LastTransitionTime.Time); since <= NotReadyGrace {
			continue
		}
		ev := map[string]any{
			"status": string(ready.Status),
		}
		if ready.Reason != "" {
			ev["reason"] = ready.Reason
		}
		if ready.Message != "" {
			ev["message"] = truncate(ready.Message, 300)
		}
		out = append(out, model.Finding{
			Severity:  "critical",
			Resource:  fmt.Sprintf("node/%s", n.Name),
			Namespace: "",
			Reason:    "node_notready",
			Message:   "Node is NotReady — its capacity is gone",
			Evidence:  ev,
			Recommendation: "Check kubelet and node health (systemd, disk, network to the API); " +
				"workloads already rescheduling elsewhere will show up as their own findings.",
		})
	}
	return out
}
