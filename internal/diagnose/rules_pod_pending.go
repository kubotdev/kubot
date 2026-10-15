package diagnose

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

type PodImagePullRule struct{}

func (PodImagePullRule) Name() string { return "pod_imagepull_backoff" }

func (PodImagePullRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, pod := range s.Pods {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting == nil {
				continue
			}
			r := cs.State.Waiting.Reason
			if r != "ImagePullBackOff" && r != "ErrImagePull" {
				continue
			}
			image := ""
			for _, c := range pod.Spec.Containers {
				if c.Name == cs.Name {
					image = c.Image
				}
			}
			out = append(out, model.Finding{
				Severity:  "critical",
				Resource:  fmt.Sprintf("pod/%s", pod.Name),
				Namespace: pod.Namespace,
				Reason:    "pod_imagepull_backoff",
				Message:   fmt.Sprintf("Container %q cannot pull image %q (%s)", cs.Name, image, r),
				Evidence: map[string]any{
					"container": cs.Name,
					"image":     image,
					"reason":    r,
					"message":   cs.State.Waiting.Message,
				},
				Recommendation: "Verify the image tag exists and registry credentials (imagePullSecrets) are correct.",
			})
		}
		// Init containers waiting on image pull too.
		for _, cs := range pod.Status.InitContainerStatuses {
			if cs.State.Waiting == nil {
				continue
			}
			r := cs.State.Waiting.Reason
			if r != "ImagePullBackOff" && r != "ErrImagePull" {
				continue
			}
			out = append(out, model.Finding{
				Severity:  "critical",
				Resource:  fmt.Sprintf("pod/%s", pod.Name),
				Namespace: pod.Namespace,
				Reason:    "pod_imagepull_backoff",
				Message:   fmt.Sprintf("Init container %q cannot pull image (%s)", cs.Name, r),
				Evidence: map[string]any{
					"container":     cs.Name,
					"initContainer": true,
					"reason":        r,
					"message":       cs.State.Waiting.Message,
				},
				Recommendation: "Verify the image tag exists and registry credentials (imagePullSecrets) are correct.",
			})
		}
	}
	return out
}

type PodPendingRule struct{}

func (PodPendingRule) Name() string { return "pod_pending" }

func (PodPendingRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, pod := range s.Pods {
		if string(pod.Status.Phase) != "Pending" {
			continue
		}
		if pod.Spec.NodeName != "" {
			continue
		}
		schedMsg := ""
		schedReason := ""
		for _, e := range s.EventsForPod(pod.Namespace, pod.Name) {
			if e.Reason == "FailedScheduling" {
				schedMsg = e.Message
				schedReason = e.Reason
				break
			}
		}
		var cpuReq, memReq string
		for _, c := range pod.Spec.Containers {
			if q, ok := c.Resources.Requests["cpu"]; ok && cpuReq == "" {
				cpuReq = q.String()
			}
			if q, ok := c.Resources.Requests["memory"]; ok && memReq == "" {
				memReq = q.String()
			}
		}
		sev := "warning"
		msg := fmt.Sprintf("Pod is Pending and unscheduled%s", reasonSuffix(schedReason))
		ev := map[string]any{
			"phase": "Pending",
		}
		if schedReason != "" {
			ev["scheduler_reason"] = schedReason
		}
		if schedMsg != "" {
			ev["scheduler_message"] = truncate(schedMsg, 300)
		}
		if cpuReq != "" {
			ev["cpu_request"] = cpuReq
		}
		if memReq != "" {
			ev["memory_request"] = memReq
		}
		if pod.Spec.NodeSelector != nil {
			ev["nodeSelector"] = pod.Spec.NodeSelector
		}
		unsched, taints := clusterScheduling(s)
		if unsched > 0 {
			ev["unschedulable_nodes"] = unsched
		}
		if len(taints) > 0 {
			ev["node_taints"] = taints
		}
		if pressured, notready := nodeHealthContext(s); len(pressured) > 0 || len(notready) > 0 {
			if len(pressured) > 0 {
				ev["pressured_nodes"] = pressured
			}
			if len(notready) > 0 {
				ev["notready_nodes"] = notready
			}
		}
		out = append(out, model.Finding{
			Severity:  sev,
			Resource:  fmt.Sprintf("pod/%s", pod.Name),
			Namespace: pod.Namespace,
			Reason:    "pod_pending",
			Message:   msg,
			Evidence:  ev,
			Recommendation: "Describe the pod events for FailedScheduling detail; check requests vs node capacity, " +
				"nodeSelector/affinity/tolerations, and unschedulable nodes.",
		})
	}
	return out
}

func reasonSuffix(r string) string {
	if r == "" {
		return ""
	}
	return ": " + r
}

func nodeHealthContext(s *k8s.Snapshot) (pressured, notready []string) {
	for _, n := range s.Nodes {
		for _, c := range n.Status.Conditions {
			switch c.Type {
			case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
				if c.Status == corev1.ConditionTrue {
					pressured = append(pressured, fmt.Sprintf("%s (%s)", n.Name, c.Type))
				}
			case corev1.NodeReady:
				if c.Status != corev1.ConditionTrue {
					r := string(c.Status)
					if c.Reason != "" {
						r = c.Reason
					}
					notready = append(notready, fmt.Sprintf("%s (%s)", n.Name, r))
				}
			}
		}
	}
	return pressured, notready
}

// Cluster-wide scheduling context: cordoned nodes and distinct taint keys
// the pending pod would need to tolerate.
func clusterScheduling(s *k8s.Snapshot) (int, []string) {
	unsched := 0
	seen := map[string]bool{}
	var taints []string
	for _, n := range s.Nodes {
		if n.Spec.Unschedulable {
			unsched++
		}
		for _, t := range n.Spec.Taints {
			if !seen[t.Key] {
				seen[t.Key] = true
				taints = append(taints, t.Key)
			}
		}
	}
	return unsched, taints
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
