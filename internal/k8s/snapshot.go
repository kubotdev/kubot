package k8s

import (
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
)

type Snapshot struct {
	Context         string
	Pods            []corev1.Pod
	Deployments     []appsv1.Deployment
	ReplicaSets     []appsv1.ReplicaSet
	StatefulSets    []appsv1.StatefulSet
	DaemonSets      []appsv1.DaemonSet
	Services        []corev1.Service
	EndpointSlices  []discoveryv1.EndpointSlice
	Ingresses       []networkingv1.Ingress
	IngressClasses  []networkingv1.IngressClass
	Jobs            []batchv1.Job
	CronJobs        []batchv1.CronJob
	Secrets         []corev1.Secret
	Events          []corev1.Event
	Nodes           []corev1.Node
	Usage           []ContainerUsage
	PVCs            []corev1.PersistentVolumeClaim
	HPAs            []autoscalingv1.HorizontalPodAutoscaler
	PDBs            []policyv1.PodDisruptionBudget
	ConfigMaps      []corev1.ConfigMap
	ServiceAccounts []corev1.ServiceAccount

	// Collectors that failed without aborting the run (e.g. events RBAC denied).
	Degraded []string
}

func (s *Snapshot) EventsForPod(namespace, podName string) []corev1.Event {
	var out []corev1.Event
	for _, e := range s.Events {
		if e.InvolvedObject.Kind == "Pod" &&
			e.InvolvedObject.Name == podName &&
			e.InvolvedObject.Namespace == namespace {
			out = append(out, e)
		}
	}
	// Newest first.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].LastTimestamp.Time.After(out[j-1].LastTimestamp.Time); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (s *Snapshot) ReadyEndpoints(namespace, svcName string) (ready, total int) {
	for _, es := range s.EndpointSlices {
		if es.Namespace != namespace {
			continue
		}
		svc, ok := es.Labels[discoveryv1.LabelServiceName]
		if !ok || svc != svcName {
			continue
		}
		for _, ep := range es.Endpoints {
			total++
			if ep.Conditions.Ready == nil || *ep.Conditions.Ready {
				ready++
			}
		}
	}
	return ready, total
}

// Memory usage for one container. False without metrics-server.
func (s *Snapshot) UsageOf(namespace, pod, container string) (int64, bool) {
	for _, u := range s.Usage {
		if u.Namespace == namespace && u.Pod == pod && u.Container == container {
			return u.MemoryBytes, true
		}
	}
	return 0, false
}

// Pod names keyed by workload name (deployment, statefulset, …).
func (s *Snapshot) PodsByTopOwner() map[string][]string {
	m := map[string][]string{}
	for _, p := range s.Pods {
		owner := TopOwnerName(&p)
		m[owner] = append(m[owner], p.Name)
	}
	return m
}

// TopOwnerName guesses the workload owning a pod
func TopOwnerName(p *corev1.Pod) string {
	for _, o := range p.OwnerReferences {
		if o.Kind == "ReplicaSet" {
			if d := stripHashSuffix(o.Name); d != "" {
				return d
			}
			return o.Name
		}
		if o.Kind == "StatefulSet" || o.Kind == "DaemonSet" || o.Kind == "Job" {
			return o.Name
		}
	}
	return p.Name
}

func stripHashSuffix(rs string) string {
	i := lastDash(rs)
	if i <= 0 || len(rs)-i-1 < 5 {
		return rs
	}
	for _, c := range rs[i+1:] {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			continue
		}
		return rs
	}
	return rs[:i]
}

func lastDash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '-' {
			return i
		}
	}
	return -1
}
