package diagnose

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"

	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

// Flags the isolation breaks that are never legitimate by
// accident: privileged containers and shared host namespaces.
type PodHostAccessRule struct{}

func (PodHostAccessRule) Name() string { return "pod_host_access" }

func (PodHostAccessRule) Run(s *k8s.Snapshot) []model.Finding {
	var out []model.Finding
	for _, pod := range s.Pods {
		if isMirrorPod(&pod) || pod.Namespace == "kube-system" {
			continue
		}
		var breaks []string
		var who []string
		for _, c := range pod.Spec.Containers {
			if c.SecurityContext != nil && c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
				if !containsStr(breaks, "privileged") {
					breaks = append(breaks, "privileged")
				}
				who = append(who, c.Name)
			}
		}
		host := map[string]bool{
			"hostPID": pod.Spec.HostPID, "hostNetwork": pod.Spec.HostNetwork,
			"hostIPC": pod.Spec.HostIPC,
		}
		for k, v := range host {
			if v && !containsStr(breaks, k) {
				breaks = append(breaks, k)
			}
		}
		if len(breaks) == 0 {
			continue
		}
		sort.Strings(breaks)
		sort.Strings(who)
		out = append(out, model.Finding{
			Severity:  "warning",
			Resource:  fmt.Sprintf("pod/%s", pod.Name),
			Namespace: pod.Namespace,
			Reason:    "pod_host_access",
			Message:   fmt.Sprintf("Pod breaks container isolation (%s)", joinStrings(breaks, ", ")),
			Evidence: map[string]any{
				"breaks":                breaks,
				"privileged_containers": who,
			},
			Recommendation: "Drop privileged (grant Linux capabilities instead) and host namespaces; " +
				"confine the pod with a NetworkPolicy and runAsNonRoot.",
		})
	}
	return out
}

// WorkloadRootRule is the aggregate for the mild, widespread case: every pod
// behind a workload runs as root. One note per workload instead of a note per pod
type WorkloadRootRule struct{}

func (WorkloadRootRule) Name() string { return "workload_running_as_root" }

func (WorkloadRootRule) Run(s *k8s.Snapshot) []model.Finding {
	kindOf := map[string]string{}
	for _, d := range s.Deployments {
		kindOf[d.Namespace+"/"+d.Name] = "deployment"
	}
	for _, st := range s.StatefulSets {
		if _, ok := kindOf[st.Namespace+"/"+st.Name]; !ok {
			kindOf[st.Namespace+"/"+st.Name] = "statefulset"
		}
	}
	for _, ds := range s.DaemonSets {
		if _, ok := kindOf[ds.Namespace+"/"+ds.Name]; !ok {
			kindOf[ds.Namespace+"/"+ds.Name] = "daemonset"
		}
	}
	type group struct {
		kind, ns, name string
		pods           map[string]bool
	}
	groups := map[string]*group{}
	order := []string{}
	add := func(kind, ns, name, pod string, allRoot bool) {
		key := kind + "/" + ns + "/" + name
		g, ok := groups[key]
		if !ok {
			g = &group{kind: kind, ns: ns, name: name, pods: map[string]bool{}}
			groups[key] = g
			order = append(order, key)
		}
		if prev, seen := g.pods[pod]; !seen {
			g.pods[pod] = allRoot
		} else {
			g.pods[pod] = prev && allRoot
		}
	}
	for _, pod := range s.Pods {
		if len(pod.Spec.Containers) == 0 {
			continue
		}
		if isMirrorPod(&pod) || pod.Namespace == "kube-system" {
			continue
		}
		if ownedByJob(&pod) {
			continue
		}
		allRoot := true
		for _, c := range pod.Spec.Containers {
			if !containerIsRoot(pod.Spec.SecurityContext, c) {
				allRoot = false
				break
			}
		}
		owner := k8s.TopOwnerName(&pod)
		if kind, ok := kindOf[pod.Namespace+"/"+owner]; ok {
			add(kind, pod.Namespace, owner, pod.Name, allRoot)
		} else {
			add("pod", pod.Namespace, pod.Name, pod.Name, allRoot)
		}
	}
	var out []model.Finding
	for _, key := range order {
		g := groups[key]
		rootPods := 0
		for _, r := range g.pods {
			if r {
				rootPods++
			}
		}
		if rootPods == 0 || rootPods != len(g.pods) {
			continue
		}
		out = append(out, model.Finding{
			Severity:  "note",
			Resource:  fmt.Sprintf("%s/%s", g.kind, g.name),
			Namespace: g.ns,
			Reason:    "workload_running_as_root",
			Message:   fmt.Sprintf("%d/%d pod(s) run as root — no runAsNonRoot in container or pod", rootPods, len(g.pods)),
			Evidence: map[string]any{
				"pods":      len(g.pods),
				"root_pods": rootPods,
			},
			Recommendation: "Set runAsNonRoot: true (and a non-zero runAsUser) unless the image needs root " +
				"(databases initializing data dirs, sub-1024 ports) — then document why, not just that.",
		})
	}
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func joinStrings(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func ownedByJob(pod *corev1.Pod) bool {
	for _, o := range pod.OwnerReferences {
		if o.Kind == "Job" {
			return true
		}
	}
	return false
}

func containerIsRoot(podSC *corev1.PodSecurityContext, c corev1.Container) bool {
	if c.SecurityContext != nil {
		if c.SecurityContext.RunAsNonRoot != nil {
			return !*c.SecurityContext.RunAsNonRoot
		}
		if c.SecurityContext.RunAsUser != nil {
			return *c.SecurityContext.RunAsUser == 0
		}
	}
	if podSC != nil {
		if podSC.RunAsNonRoot != nil {
			return !*podSC.RunAsNonRoot
		}
		if podSC.RunAsUser != nil {
			return *podSC.RunAsUser == 0
		}
	}
	return true
}
