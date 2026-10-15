package diagnose

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kubotdev/kubot/internal/k8s"
)

func crashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-api-7d8f9c6f8b-x2k4m", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: "Running",
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "api",
					RestartCount: 14,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off 5m0s restarting failed container"},
					},
					LastTerminationState: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error"},
					},
				},
			},
		},
	}
}

func TestCrashLoop(t *testing.T) {
	s := &k8s.Snapshot{Pods: []corev1.Pod{crashPod()}}
	f := PodCrashLoopRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "pod_crashloop_backoff" || f[0].Severity != "critical" {
		t.Fatalf("expected crashloop finding, got %+v", f)
	}
}

func TestOOMKilled(t *testing.T) {
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = nil
	p.Status.ContainerStatuses[0].State.Terminated = &corev1.ContainerStateTerminated{ExitCode: 137, Reason: "OOMKilled"}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	f := PodOOMKilledRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "pod_oom_killed" {
		t.Fatalf("expected oom finding, got %+v", f)
	}
	if f[0].Evidence["exit_code"] != int32(137) && f[0].Evidence["exit_code"] != 137 {
		t.Fatalf("expected exit 137 evidence, got %+v", f[0].Evidence)
	}
}

func TestImagePull(t *testing.T) {
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "Back-off pulling image"}
	p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodImagePullRule{}).Run(s)
	if len(got) != 1 {
		t.Fatalf("expected imagepull, got %+v", got)
	}
}

func TestPending(t *testing.T) {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-0", Namespace: "default"},
		Status:     corev1.PodStatus{Phase: "Pending"},
	}
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "worker-0", Namespace: "default"},
		Reason:         "FailedScheduling",
		Message:        "0/3 nodes are available: 3 Insufficient cpu.",
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	f := PodPendingRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "pod_pending" {
		t.Fatalf("expected pending, got %+v", f)
	}
}

func TestPendingSkipsScheduledPod(t *testing.T) {
	// Assigned to a node (image pulling): not a scheduling problem.
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pulling-0", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status:     corev1.PodStatus{Phase: "Pending"},
	}
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "pulling-0", Namespace: "default"},
		Reason:         "Scheduled",
		Message:        "Successfully assigned default/pulling-0 to node-1",
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	if got := (PodPendingRule{}).Run(s); len(got) != 0 {
		t.Fatalf("scheduled pod must not be a pending finding, got %+v", got)
	}
}

func TestCrashLoopBackoffWindow(t *testing.T) {
	// Between backoffs: Error state, 3 restarts, recent non-zero last exit.
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = &corev1.ContainerStateWaiting{Reason: "Error", Message: "some error"}
	p.Status.ContainerStatuses[0].RestartCount = 3
	p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", FinishedAt: metav1.Now()},
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodCrashLoopRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_crashloop_backoff" {
		t.Fatalf("expected crashloop in backoff window, got %+v", got)
	}
}

func TestCrashLoopTerminatedWindow(t *testing.T) {
	// Crash landed, kubelet hasn't restarted yet: Terminated exit 1, 3 restarts.
	p := crashPod()
	p.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", FinishedAt: metav1.Now()},
	}
	p.Status.ContainerStatuses[0].RestartCount = 3
	p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 1, Reason: "Error", FinishedAt: metav1.Now()},
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodCrashLoopRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_crashloop_backoff" {
		t.Fatalf("expected crashloop in terminated window, got %+v", got)
	}
}

func TestCrashLoopStaleRestartsSilent(t *testing.T) {
	// 3 restarts from an hour ago, running fine now: history, not a loop.
	p := crashPod()
	p.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Now().Add(-30 * time.Minute))},
	}
	p.Status.ContainerStatuses[0].Ready = true
	p.Status.ContainerStatuses[0].RestartCount = 3
	old := metav1.NewTime(time.Now().Add(-time.Hour))
	p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{ExitCode: 255, Reason: "Unknown", FinishedAt: old},
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	if got := (PodCrashLoopRule{}).Run(s); len(got) != 0 {
		t.Fatalf("stale restarts must be silent, got %+v", got)
	}
}

func TestMissingResourcesSkipsMirrorPods(t *testing.T) {
	p := resPod()
	p.OwnerReferences = []metav1.OwnerReference{{Kind: "Node", Name: "node-1"}}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	if got := (PodMissingResourcesRule{}).Run(s); len(got) != 0 {
		t.Fatalf("mirror pod must be silent, got %+v", got)
	}
}

func TestMissingResourcesSkipsKubeSystem(t *testing.T) {
	p := resPod()
	p.Namespace = "kube-system"
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	if got := (PodMissingResourcesRule{}).Run(s); len(got) != 0 {
		t.Fatalf("kube-system must be silent, got %+v", got)
	}
}

func TestProbeReadyPodIgnored(t *testing.T) {
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = nil
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: p.Name, Namespace: "default"},
		Type:           "Warning",
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: connection refused",
		Count:          12,
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	if got := (PodProbeFailingRule{}).Run(s); len(got) != 0 {
		t.Fatalf("ready pod must be silent, got %+v", got)
	}
}

func TestProbeTransientIgnored(t *testing.T) {
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = nil
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: p.Name, Namespace: "default"},
		Type:           "Warning",
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: connection refused",
		Count:          1,
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	if got := (PodProbeFailingRule{}).Run(s); len(got) != 0 {
		t.Fatalf("transient probe blip must be silent, got %+v", got)
	}
}

func TestProbeFailing(t *testing.T) {
	p := crashPod()
	p.Status.ContainerStatuses[0].State.Waiting = nil
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: p.Name, Namespace: "default"},
		Type:           "Warning",
		Reason:         "Unhealthy",
		Message:        "Readiness probe failed: HTTP probe failed with statuscode: 500",
		Count:          12,
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	f := PodProbeFailingRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "pod_probe_failing" {
		t.Fatalf("expected probe failing, got %+v", f)
	}
}

func TestDeploymentUnavailable(t *testing.T) {
	rep := int32(2)
	d := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-api", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &rep},
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, UnavailableReplicas: 1},
	}
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}}
	f := DeploymentUnavailableRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "deployment_unavailable" {
		t.Fatalf("expected deployment_unavailable, got %+v", f)
	}
}

func TestServiceNoEndpoints(t *testing.T) {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "shop"}},
	}
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "other-1", Namespace: "default", Labels: map[string]string{"app": "other"}},
	}
	s := &k8s.Snapshot{Services: []corev1.Service{svc}, Pods: []corev1.Pod{p}}
	f := ServiceNoEndpointsRule{}.Run(s)
	if len(f) != 1 || f[0].Reason != "service_no_endpoints" {
		t.Fatalf("expected service_no_endpoints, got %+v", f)
	}
}

func TestEngineSortDeterministic(t *testing.T) {
	s := &k8s.Snapshot{
		Pods: []corev1.Pod{crashPod()},
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "missing"}},
		}},
	}
	e := NewEngine()
	a, b := e.Run(s), e.Run(s)
	if len(a) != len(b) {
		t.Fatal("non-deterministic length")
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			t.Fatalf("non-deterministic finding %d: %+v vs %+v", i, a[i], b[i])
		}
	}
	if len(a) > 0 && a[0].Severity != "critical" {
		t.Fatalf("expected critical first, got %+v", a[0])
	}
}

func TestFilterByWorkload(t *testing.T) {
	s := &k8s.Snapshot{Pods: []corev1.Pod{crashPod()}}
	e := NewEngine()
	all := e.Run(s)
	filtered := FilterByWorkload(all, "payments-api", "", s)
	if len(filtered) == 0 {
		t.Fatal("expected workload match")
	}
	none := FilterByWorkload(all, "something-else-entirely", "", s)
	if len(none) != 0 {
		t.Fatalf("expected no match, got %+v", none)
	}
}

func TestFilterByWorkload_suffixedNames(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "payments-deployment-abc12-xyz", Namespace: "default",
			Labels:          map[string]string{"app": "payments"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "payments-deployment-abc12"}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-service", Namespace: "default"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "payments"}},
	}
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "payments-deployment", Namespace: "default"},
		Status:     appsv1.DeploymentStatus{UnavailableReplicas: 1},
	}
	s := &k8s.Snapshot{
		Pods:        []corev1.Pod{pod},
		Services:    []corev1.Service{svc},
		Deployments: []appsv1.Deployment{dep},
	}
	e := NewEngine()
	all := e.Run(s)
	// Querying the service must pull the differently-named pods+deployment
	// through the selector link, not just by name.
	got := FilterByWorkload(all, "payments-service", "", s)
	names := map[string]bool{}
	for _, f := range got {
		names[f.Resource] = true
	}
	for _, want := range []string{"service/payments-service", "deployment/payments-deployment", "pod/payments-deployment-abc12-xyz"} {
		if !names[want] {
			t.Fatalf("missing %s in %+v", want, names)
		}
	}
	// And the reverse: bare stem still matches everything by name.
	if got := FilterByWorkload(all, "payments", "", s); len(got) != len(all) {
		t.Fatalf("stem query got %d of %d", len(got), len(all))
	}
}

func resPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "app-1", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "app",
			Image: "busybox:1.36",
		}}},
		Status: corev1.PodStatus{Phase: "Running"},
	}
}

func TestMissingResources(t *testing.T) {
	s := &k8s.Snapshot{Pods: []corev1.Pod{resPod()}}
	got := (PodMissingResourcesRule{}).Run(s)
	if len(got) != 1 || got[0].Severity != "note" {
		t.Fatalf("expected one note, got %+v", got)
	}
}

func TestLowLimit(t *testing.T) {
	p := resPod()
	p.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("16Mi"),
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodLowLimitRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_low_limit" {
		t.Fatalf("expected low-limit warning, got %+v", got)
	}
	p.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory] = resource.MustParse("256Mi")
	if got := (PodLowLimitRule{}).Run(s); len(got) != 0 {
		t.Fatalf("sane limit must be silent, got %+v", got)
	}
}

func TestNearLimit(t *testing.T) {
	p := resPod()
	p.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("100Mi"),
	}
	s := &k8s.Snapshot{
		Pods: []corev1.Pod{p},
		Usage: []k8s.ContainerUsage{
			{Namespace: "default", Pod: "app-1", Container: "app", MemoryBytes: 90 * 1024 * 1024},
		},
	}
	got := (PodNearLimitRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_near_limit" {
		t.Fatalf("expected near-limit warning, got %+v", got)
	}
	s.Usage[0].MemoryBytes = 50 * 1024 * 1024
	if got := (PodNearLimitRule{}).Run(s); len(got) != 0 {
		t.Fatalf("half usage must be silent, got %+v", got)
	}
}

func TestDeploymentStalled(t *testing.T) {
	rep := int32(2)
	d := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "stuck", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &rep},
		Status: appsv1.DeploymentStatus{
			Replicas: 2, UpdatedReplicas: 1, ReadyReplicas: 1,
			Conditions: []appsv1.DeploymentCondition{{
				Type:    appsv1.DeploymentProgressing,
				Reason:  "ProgressDeadlineExceeded",
				Message: "ReplicaSet has timed out progressing",
			}},
		},
	}
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}}
	got := (DeploymentStalledRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "deployment_rollout_stalled" {
		t.Fatalf("expected stalled rollout, got %+v", got)
	}
}

func TestReplicaSetLeftover(t *testing.T) {
	one := int32(1)
	now := metav1.Now()
	old := metav1.NewTime(now.Add(-time.Hour))
	rs := func(name string, at metav1.Time) appsv1.ReplicaSet {
		return appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "web-" + name, Namespace: "default",
				CreationTimestamp: at,
				OwnerReferences:   []metav1.OwnerReference{{Kind: "Deployment", Name: "web"}},
			},
			Spec: appsv1.ReplicaSetSpec{Replicas: &one},
		}
	}
	s := &k8s.Snapshot{ReplicaSets: []appsv1.ReplicaSet{rs("aaa", old), rs("bbb", now)}}
	got := (ReplicaSetLeftoverRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "replicaset_leftover" {
		t.Fatalf("expected leftover note, got %+v", got)
	}
}

func TestPVCPending(t *testing.T) {
	class := "does-not-exist"
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "default"},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &class,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			}},
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	s := &k8s.Snapshot{PVCs: []corev1.PersistentVolumeClaim{pvc}}
	got := (PVCPendingRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pvc_pending" {
		t.Fatalf("expected pending pvc, got %+v", got)
	}
}

func TestPodMountFailure(t *testing.T) {
	p := crashPod()
	ev := corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: "default"},
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: p.Name, Namespace: "default"},
		Type:           "Warning",
		Reason:         "FailedMount",
		Message:        "Unable to attach or mount volumes: unmounted volumes=[data]",
		Count:          5,
	}
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}, Events: []corev1.Event{ev}}
	got := (PodMountFailureRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_mount_failure" {
		t.Fatalf("expected mount failure, got %+v", got)
	}
}

func TestNodePressure(t *testing.T) {
	n := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
		}},
	}
	s := &k8s.Snapshot{Nodes: []corev1.Node{n}}
	got := (NodePressureRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "node_pressure" {
		t.Fatalf("expected node pressure, got %+v", got)
	}
}

func TestDeploymentResourceRisk(t *testing.T) {
	p1 := resPod()
	p1.Name = "shop-7d8f9c6f8b-x1"
	p1.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "shop-7d8f9c6f8b"}}
	s := &k8s.Snapshot{
		Pods: []corev1.Pod{p1},
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"},
		}},
	}
	got := (DeploymentResourceRiskRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "deployment_resource_risk" {
		t.Fatalf("expected risk note, got %+v", got)
	}
}

func TestClusterScheduling(t *testing.T) {
	s := &k8s.Snapshot{Nodes: []corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "n1"},
			Spec: corev1.NodeSpec{
				Unschedulable: true,
				Taints:        []corev1.Taint{{Key: "gpu"}},
			},
		},
	}}
	n, taints := clusterScheduling(s)
	if n != 1 || len(taints) != 1 || taints[0] != "gpu" {
		t.Fatalf("got %d %v", n, taints)
	}
}

func TestStatefulSetUnavailable(t *testing.T) {
	rep := int32(2)
	s := &k8s.Snapshot{StatefulSets: []appsv1.StatefulSet{{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"},
		Spec:       appsv1.StatefulSetSpec{Replicas: &rep},
		Status:     appsv1.StatefulSetStatus{ReadyReplicas: 1},
	}}}
	got := (StatefulSetUnavailableRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "statefulset_unavailable" {
		t.Fatalf("expected unavailable statefulset, got %+v", got)
	}
	s.StatefulSets[0].Status.ReadyReplicas = 2
	if got := (StatefulSetUnavailableRule{}).Run(s); len(got) != 0 {
		t.Fatalf("ready statefulset must be silent, got %+v", got)
	}
}

func TestDaemonSetUnavailable(t *testing.T) {
	s := &k8s.Snapshot{DaemonSets: []appsv1.DaemonSet{{
		ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"},
		Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 1, NumberReady: 0, NumberUnavailable: 1},
	}}}
	got := (DaemonSetUnavailableRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "daemonset_unavailable" {
		t.Fatalf("expected unavailable daemonset, got %+v", got)
	}
	s.DaemonSets[0].Status.NumberUnavailable = 0
	s.DaemonSets[0].Status.NumberReady = 1
	if got := (DaemonSetUnavailableRule{}).Run(s); len(got) != 0 {
		t.Fatalf("ready daemonset must be silent, got %+v", got)
	}
}

func ingressFixture() *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "shop-svc",
									Port: networkingv1.ServiceBackendPort{Number: 80},
								},
							},
						}},
					},
				},
			}},
		},
	}
}

func TestIngressNoBackends_missingService(t *testing.T) {
	s := &k8s.Snapshot{Ingresses: []networkingv1.Ingress{*ingressFixture()}}
	got := (IngressNoBackendsRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "ingress_no_backends" {
		t.Fatalf("expected missing backend, got %+v", got)
	}
}

func TestIngressNoBackends_readyServiceSilent(t *testing.T) {
	ready := true
	s := &k8s.Snapshot{
		Ingresses: []networkingv1.Ingress{*ingressFixture()},
		Services: []corev1.Service{{
			ObjectMeta: metav1.ObjectMeta{Name: "shop-svc", Namespace: "default"},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "shop"}},
		}},
		EndpointSlices: []discoveryv1.EndpointSlice{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shop-1", Namespace: "default",
				Labels: map[string]string{discoveryv1.LabelServiceName: "shop-svc"},
			},
			Endpoints: []discoveryv1.Endpoint{{Conditions: discoveryv1.EndpointConditions{Ready: &ready}}},
		}},
	}
	if got := (IngressNoBackendsRule{}).Run(s); len(got) != 0 {
		t.Fatalf("ready backend must be silent, got %+v", got)
	}
}

func TestIngressUnknownClass(t *testing.T) {
	class := "does-not-exist"
	ing := ingressFixture()
	ing.Spec.IngressClassName = &class
	s := &k8s.Snapshot{Ingresses: []networkingv1.Ingress{*ing}}
	got := (IngressClassRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "ingress_unknown_class" {
		t.Fatalf("expected unknown class, got %+v", got)
	}
	ing.Spec.IngressClassName = nil
	s2 := &k8s.Snapshot{
		Ingresses: []networkingv1.Ingress{*ing},
		IngressClasses: []networkingv1.IngressClass{{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "nginx",
				Annotations: map[string]string{"ingressclass.kubernetes.io/is-default-class": "true"},
			},
		}},
	}
	if got := (IngressClassRule{}).Run(s2); len(got) != 0 {
		t.Fatalf("default class present must be silent, got %+v", got)
	}
}

func TestIngressTLSSecretMissing(t *testing.T) {
	ing := ingressFixture()
	ing.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{"shop.example.com"}, SecretName: "shop-tls"}}
	s := &k8s.Snapshot{Ingresses: []networkingv1.Ingress{*ing}}
	got := (IngressTLSSecretRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "ingress_tls_secret_missing" {
		t.Fatalf("expected missing tls secret, got %+v", got)
	}
	s.Secrets = []corev1.Secret{{ObjectMeta: metav1.ObjectMeta{Name: "shop-tls", Namespace: "default"}}}
	if got := (IngressTLSSecretRule{}).Run(s); len(got) != 0 {
		t.Fatalf("present secret must be silent, got %+v", got)
	}
}

func failedJob() batchv1.Job {
	backoff := int32(2)
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default"},
		Spec:       batchv1.JobSpec{BackoffLimit: &backoff},
		Status:     batchv1.JobStatus{Failed: 2},
	}
}

func TestJobFailed(t *testing.T) {
	s := &k8s.Snapshot{Jobs: []batchv1.Job{failedJob()}}
	got := (JobFailedRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "job_failed" {
		t.Fatalf("expected failed job, got %+v", got)
	}
	active := failedJob()
	active.Status.Active = 1
	s.Jobs = []batchv1.Job{active}
	if got := (JobFailedRule{}).Run(s); len(got) != 0 {
		t.Fatalf("active job must be silent, got %+v", got)
	}
}

func TestCronJobFailing(t *testing.T) {
	j := failedJob()
	j.OwnerReferences = []metav1.OwnerReference{{Kind: "CronJob", Name: "nightly"}}
	cj := batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 2 * * *"},
	}
	s := &k8s.Snapshot{Jobs: []batchv1.Job{j}, CronJobs: []batchv1.CronJob{cj}}
	got := (CronJobFailingRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "cronjob_failing" {
		t.Fatalf("expected failing cronjob, got %+v", got)
	}
	// A later success clears it.
	ok := cj
	now := metav1.Now()
	ok.Status.LastSuccessfulTime = &now
	s.CronJobs = []batchv1.CronJob{ok}
	if got := (CronJobFailingRule{}).Run(s); len(got) != 0 {
		t.Fatalf("recovered cronjob must be silent, got %+v", got)
	}
}

func TestFilterCronJobPullsJobAndPods(t *testing.T) {
	j := failedJob()
	j.OwnerReferences = []metav1.OwnerReference{{Kind: "CronJob", Name: "nightly"}}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "backup-abc", Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Job", Name: "backup"}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "x"}}},
	}
	cj := batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"}}
	s := &k8s.Snapshot{
		Jobs:     []batchv1.Job{j},
		CronJobs: []batchv1.CronJob{cj},
		Pods:     []corev1.Pod{pod},
	}
	e := NewEngine()
	all := e.Run(s)
	got := FilterByWorkload(all, "nightly", "", s)
	names := map[string]bool{}
	for _, f := range got {
		names[f.Resource] = true
	}
	for _, want := range []string{"cronjob/nightly", "job/backup", "pod/backup-abc"} {
		if !names[want] {
			t.Fatalf("missing %s in %+v", want, names)
		}
	}
}

func rolloutSnap(now metav1.Time, sets ...appsv1.ReplicaSet) *k8s.Snapshot {
	d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "checkout-api", Namespace: "production"}}
	return &k8s.Snapshot{Deployments: []appsv1.Deployment{d}, ReplicaSets: sets}
}

func rolloutRS(name, image string, revision int, at metav1.Time) appsv1.ReplicaSet {
	rs := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "production",
			CreationTimestamp: at,
			Annotations:       map[string]string{"deployment.kubernetes.io/revision": strconv.Itoa(revision)},
			OwnerReferences:   []metav1.OwnerReference{{Kind: "Deployment", Name: "checkout-api"}},
		},
	}
	rs.Spec.Template.Spec.Containers = []corev1.Container{{Name: "api", Image: image}}
	return rs
}

func TestRolloutRecent_imageChange(t *testing.T) {
	now := metav1.Now()
	s := rolloutSnap(now,
		rolloutRS("checkout-api-7d8f9c", "api:v2.8.0", 11, metav1.NewTime(now.Add(-2*time.Hour))),
		rolloutRS("checkout-api-9a1b2c", "api:v2.8.1", 12, metav1.NewTime(now.Add(-9*time.Minute))),
	)
	got := (DeploymentRolloutRecentRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "deployment_rollout_recent" || got[0].Severity != "note" {
		t.Fatalf("expected recent-rollout note, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "v2.8.0") || !strings.Contains(got[0].Message, "v2.8.1") || !strings.Contains(got[0].Message, "→") {
		t.Fatalf("message must show the image change, got %q", got[0].Message)
	}
	if got[0].Evidence["previous_images"] == nil || got[0].Evidence["revision"] != 12 {
		t.Fatalf("evidence must carry previous images and revision, got %+v", got[0].Evidence)
	}
}

func TestRolloutRecent_oldRolloutSilent(t *testing.T) {
	now := metav1.Now()
	s := rolloutSnap(now,
		rolloutRS("checkout-api-7d8f9c", "api:v2.8.0", 11, metav1.NewTime(now.Add(-3*time.Hour))),
		rolloutRS("checkout-api-9a1b2c", "api:v2.8.1", 12, metav1.NewTime(now.Add(-2*time.Hour))),
	)
	if got := (DeploymentRolloutRecentRule{}).Run(s); len(got) != 0 {
		t.Fatalf("old rollouts are history, got %+v", got)
	}
}

func TestRolloutRecent_firstRollout(t *testing.T) {
	now := metav1.Now()
	s := rolloutSnap(now,
		rolloutRS("checkout-api-7d8f9c", "api:v2.8.0", 1, metav1.NewTime(now.Add(-5*time.Minute))),
	)
	got := (DeploymentRolloutRecentRule{}).Run(s)
	if len(got) != 1 || !strings.Contains(got[0].Message, "first rollout") {
		t.Fatalf("expected first-rollout note, got %+v", got)
	}
}

func TestRolloutRecent_sameImageRedeploy(t *testing.T) {
	now := metav1.Now()
	s := rolloutSnap(now,
		rolloutRS("checkout-api-7d8f9c", "api:v2.8.0", 11, metav1.NewTime(now.Add(-2*time.Hour))),
		rolloutRS("checkout-api-9a1b2c", "api:v2.8.0", 12, metav1.NewTime(now.Add(-9*time.Minute))),
	)
	got := (DeploymentRolloutRecentRule{}).Run(s)
	if len(got) != 1 || strings.Contains(got[0].Message, "→") {
		t.Fatalf("same-image redeploy must not claim a change, got %+v", got)
	}
}

func TestRolloutRecent_filteredByWorkload(t *testing.T) {
	now := metav1.Now()
	s := rolloutSnap(now,
		rolloutRS("checkout-api-9a1b2c", "api:v2.8.1", 12, metav1.NewTime(now.Add(-9*time.Minute))),
	)
	all := NewEngine().Run(s)
	got := FilterByWorkload(all, "checkout-api", "production", s)
	found := false
	for _, f := range got {
		if f.Reason == "deployment_rollout_recent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("why must include the rollout note, got %+v", all)
	}
}

func secPod(ns, name string, sc *corev1.SecurityContext, hostPID, hostNet bool) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: corev1.PodSpec{
			HostPID: hostPID, HostNetwork: hostNet,
			Containers: []corev1.Container{{Name: "app", SecurityContext: sc}},
		},
	}
}

func boolPtr(b bool) *bool { return &b }

func TestPodHostAccess_privileged(t *testing.T) {
	p := secPod("default", "agent-0", &corev1.SecurityContext{Privileged: boolPtr(true)}, false, false)
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodHostAccessRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_host_access" || got[0].Severity != "warning" {
		t.Fatalf("expected host-access warning, got %+v", got)
	}
}

func TestPodHostAccess_hostNamespaces(t *testing.T) {
	p := secPod("default", "agent-0", nil, true, true)
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	got := (PodHostAccessRule{}).Run(s)
	if len(got) != 1 {
		t.Fatalf("expected host-namespace warning, got %+v", got)
	}
	ev, _ := got[0].Evidence["breaks"].([]string)
	if len(ev) != 2 {
		t.Fatalf("must name both breaks, got %+v", got[0].Evidence)
	}
}

func TestPodHostAccess_cleanSilent(t *testing.T) {
	p := secPod("default", "agent-0", &corev1.SecurityContext{RunAsNonRoot: boolPtr(true)}, false, false)
	s := &k8s.Snapshot{Pods: []corev1.Pod{p}}
	if got := (PodHostAccessRule{}).Run(s); len(got) != 0 {
		t.Fatalf("clean pods stay silent, got %+v", got)
	}
}

func rootDeployment() (appsv1.Deployment, []corev1.Pod) {
	d := appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"}}
	mk := func(name string, sc *corev1.SecurityContext) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-7d8f9c"}},
			},
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", SecurityContext: sc}}},
		}
	}
	return d, []corev1.Pod{mk("web-7d8f9c-a", nil), mk("web-7d8f9c-b", nil)}
}

func TestWorkloadRoot_allRoot(t *testing.T) {
	d, pods := rootDeployment()
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}, Pods: pods}
	got := (WorkloadRootRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "workload_running_as_root" || got[0].Severity != "note" {
		t.Fatalf("expected root note, got %+v", got)
	}
	if got[0].Resource != "deployment/web" {
		t.Fatalf("resource must name the workload, got %q", got[0].Resource)
	}
}

func TestWorkloadRoot_partialSilent(t *testing.T) {
	d, pods := rootDeployment()
	pods[1].Spec.Containers[0].SecurityContext = &corev1.SecurityContext{RunAsNonRoot: boolPtr(true)}
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}, Pods: pods}
	if got := (WorkloadRootRule{}).Run(s); len(got) != 0 {
		t.Fatalf("partial hardening is progress, got %+v", got)
	}
}

func TestWorkloadRoot_podLevelOverride(t *testing.T) {
	tru := true
	d, pods := rootDeployment()
	for i := range pods {
		pods[i].Spec.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: &tru}
	}
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}, Pods: pods}
	if got := (WorkloadRootRule{}).Run(s); len(got) != 0 {
		t.Fatalf("pod-level runAsNonRoot must win, got %+v", got)
	}
}

func TestWorkloadRoot_skipsJobsAndSystem(t *testing.T) {
	d, pods := rootDeployment()
	jobPod := pods[0]
	jobPod.Name = "nightly-abc"
	jobPod.OwnerReferences = []metav1.OwnerReference{{Kind: "Job", Name: "nightly"}}
	sysPod := pods[1]
	sysPod.Name = "coredns-x"
	sysPod.Namespace = "kube-system"
	sysPod.OwnerReferences = nil
	s := &k8s.Snapshot{Deployments: []appsv1.Deployment{d}, Pods: []corev1.Pod{jobPod, sysPod}}
	if got := (WorkloadRootRule{}).Run(s); len(got) != 0 {
		t.Fatalf("job pods and kube-system stay silent, got %+v", got)
	}
}

func hpaFor(ns, name, target string, min, max, current, desired int32) autoscalingv1.HorizontalPodAutoscaler {
	h := autoscalingv1.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{Kind: "Deployment", Name: target},
			MinReplicas:    &min,
			MaxReplicas:    max,
		},
		Status: autoscalingv1.HorizontalPodAutoscalerStatus{CurrentReplicas: current, DesiredReplicas: desired},
	}
	return h
}

func unreadyDeploy(ns, name string) appsv1.Deployment {
	two := int32(2)
	return appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       appsv1.DeploymentSpec{Replicas: &two},
		Status:     appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 0},
	}
}

func TestHPAAtMax_desiredBeyondMax(t *testing.T) {
	s := &k8s.Snapshot{
		HPAs:        []autoscalingv1.HorizontalPodAutoscaler{hpaFor("default", "web", "web", 1, 3, 3, 5)},
		Deployments: []appsv1.Deployment{unreadyDeploy("default", "web")},
	}
	got := (HPAAtMaxRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "hpa_at_max" || got[0].Severity != "warning" {
		t.Fatalf("expected maxed warning, got %+v", got)
	}
}

func TestHPAAtMax_healthyMaxSilent(t *testing.T) {
	one := int32(1)
	s := &k8s.Snapshot{
		HPAs: []autoscalingv1.HorizontalPodAutoscaler{hpaFor("default", "web", "web", 1, 3, 3, 3)},
		Deployments: []appsv1.Deployment{{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: &one},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		}},
	}
	if got := (HPAAtMaxRule{}).Run(s); len(got) != 0 {
		t.Fatalf("maxed-but-healthy stays silent, got %+v", got)
	}
}

func TestHPANoop_minEqualsMax(t *testing.T) {
	s := &k8s.Snapshot{HPAs: []autoscalingv1.HorizontalPodAutoscaler{hpaFor("default", "web", "web", 2, 2, 2, 2)}}
	got := (HPANoopRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "hpa_noop" {
		t.Fatalf("expected noop note, got %+v", got)
	}
}

func TestHPANoop_missingTarget(t *testing.T) {
	s := &k8s.Snapshot{HPAs: []autoscalingv1.HorizontalPodAutoscaler{hpaFor("default", "web", "gone", 1, 3, 1, 1)}}
	got := (HPANoopRule{}).Run(s)
	if len(got) != 1 {
		t.Fatalf("expected missing-target note, got %+v", got)
	}
}

func pdbFor(ns, name string, minAvail *intstr.IntOrString, match map[string]string) policyv1.PodDisruptionBudget {
	var sel *metav1.LabelSelector
	if match != nil {
		sel = &metav1.LabelSelector{MatchLabels: match}
	}
	return policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       policyv1.PodDisruptionBudgetSpec{MinAvailable: minAvail, Selector: sel},
	}
}

func labeledPod(ns, name string, lbls map[string]string) corev1.Pod {
	return corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls}}
}

func intOrStr(i int) *intstr.IntOrString {
	v := intstr.FromInt32(int32(i))
	return &v
}

func TestPDBBlocking_absolute(t *testing.T) {
	s := &k8s.Snapshot{
		PDBs: []policyv1.PodDisruptionBudget{pdbFor("default", "web", intOrStr(3), map[string]string{"app": "web"})},
		Pods: []corev1.Pod{
			labeledPod("default", "web-a", map[string]string{"app": "web"}),
			labeledPod("default", "web-b", map[string]string{"app": "web"}),
			labeledPod("default", "web-c", map[string]string{"app": "web"}),
		},
	}
	got := (PDBBlockingRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pdb_blocking" || got[0].Severity != "warning" {
		t.Fatalf("expected blocking warning, got %+v", got)
	}
}

func TestPDBBlocking_headroomSilent(t *testing.T) {
	s := &k8s.Snapshot{
		PDBs: []policyv1.PodDisruptionBudget{pdbFor("default", "web", intOrStr(2), map[string]string{"app": "web"})},
		Pods: []corev1.Pod{
			labeledPod("default", "web-a", map[string]string{"app": "web"}),
			labeledPod("default", "web-b", map[string]string{"app": "web"}),
			labeledPod("default", "web-c", map[string]string{"app": "web"}),
		},
	}
	if got := (PDBBlockingRule{}).Run(s); len(got) != 0 {
		t.Fatalf("headroom must stay silent, got %+v", got)
	}
}

func TestPDBNoMatch(t *testing.T) {
	s := &k8s.Snapshot{
		PDBs: []policyv1.PodDisruptionBudget{pdbFor("default", "web-v2", intOrStr(1), map[string]string{"app": "web-v2"})},
		Pods: []corev1.Pod{labeledPod("default", "web-a", map[string]string{"app": "web"})},
	}
	got := (PDBNoMatchRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pdb_no_match" {
		t.Fatalf("expected no-match note, got %+v", got)
	}
}

func TestCronJobSuspended(t *testing.T) {
	tru := true
	s := &k8s.Snapshot{CronJobs: []batchv1.CronJob{{
		ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"},
		Spec:       batchv1.CronJobSpec{Schedule: "0 2 * * *", Suspend: &tru},
	}}}
	got := (CronJobSuspendedRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "cronjob_suspended" || got[0].Severity != "warning" {
		t.Fatalf("expected suspended warning, got %+v", got)
	}
	s.CronJobs[0].Spec.Suspend = nil
	if got := (CronJobSuspendedRule{}).Run(s); len(got) != 0 {
		t.Fatalf("running schedules stay silent, got %+v", got)
	}
}

func unusedSnap() *k8s.Snapshot {
	usedCM := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app-cfg", Namespace: "default"}}
	staleCM := corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "old-cfg", Namespace: "default"}}
	usedSecret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "app-creds", Namespace: "default"}}
	staleSecret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "rotated-creds", Namespace: "default"}}
	helmSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sh.helm.release.v1.shop.v3", Namespace: "default"},
		Type:       "helm.sh/release.v1",
	}
	usedPVC := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "default"}}
	stalePVC := corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "orphan", Namespace: "default"}}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "web-0", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "app",
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-cfg"}}},
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-creds"}}},
			},
		}},
		}}
	pod.Spec.Volumes = []corev1.Volume{{
		Name: "data",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
		},
	}}
	return &k8s.Snapshot{
		Pods:       []corev1.Pod{pod},
		ConfigMaps: []corev1.ConfigMap{usedCM, staleCM},
		Secrets:    []corev1.Secret{usedSecret, staleSecret, helmSecret},
		PVCs:       []corev1.PersistentVolumeClaim{usedPVC, stalePVC},
	}
}

func TestConfigMapUnused(t *testing.T) {
	got := (ConfigMapUnusedRule{}).Run(unusedSnap())
	if len(got) != 1 || got[0].Resource != "configmap/old-cfg" || got[0].Severity != "note" {
		t.Fatalf("want only old-cfg, got %+v", got)
	}
}

func TestSecretUnused(t *testing.T) {
	got := (SecretUnusedRule{}).Run(unusedSnap())
	if len(got) != 1 || got[0].Resource != "secret/rotated-creds" {
		t.Fatalf("want only rotated-creds (helm storage excluded), got %+v", got)
	}
}

func TestPVCUnused(t *testing.T) {
	got := (PVCUnusedRule{}).Run(unusedSnap())
	if len(got) != 1 || got[0].Resource != "persistentvolumeclaim/orphan" {
		t.Fatalf("want only orphan, got %+v", got)
	}
}

func TestUnused_templateRefsCount(t *testing.T) {
	// A CronJob template referencing everything: no live pod needed.
	s := unusedSnap()
	s.Pods = nil
	tpl := corev1.PodSpec{
		Volumes: []corev1.Volume{{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"},
			},
		}},
		Containers: []corev1.Container{{
			Name: "app",
			EnvFrom: []corev1.EnvFromSource{
				{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-cfg"}}},
				{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-creds"}}},
			},
		}},
	}
	s.CronJobs = []batchv1.CronJob{{
		ObjectMeta: metav1.ObjectMeta{Name: "weekly", Namespace: "default"},
		Spec: batchv1.CronJobSpec{
			Schedule: "0 0 * * 0",
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: tpl}},
			},
		},
	}}
	if got := (PVCUnusedRule{}).Run(s); len(got) != 1 || got[0].Resource != "persistentvolumeclaim/orphan" {
		t.Fatalf("template refs must count, got %+v", got)
	}
	if got := (ConfigMapUnusedRule{}).Run(s); len(got) != 1 {
		t.Fatalf("template refs must count, got %+v", got)
	}
	if got := (SecretUnusedRule{}).Run(s); len(got) != 1 {
		t.Fatalf("template refs must count, got %+v", got)
	}
}

func TestUnused_ingressTLSCounts(t *testing.T) {
	s := unusedSnap()
	s.Secrets = append(s.Secrets, corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shop-tls", Namespace: "default"}})
	s.Ingresses = []networkingv1.Ingress{{
		ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"},
		Spec:       networkingv1.IngressSpec{TLS: []networkingv1.IngressTLS{{SecretName: "shop-tls"}}},
	}}
	for _, f := range (SecretUnusedRule{}).Run(s) {
		if f.Resource == "secret/shop-tls" {
			t.Fatalf("ingress TLS secret must count as used: %+v", f)
		}
	}
}

func TestUnused_kubeSystemSilent(t *testing.T) {
	s := &k8s.Snapshot{
		ConfigMaps: []corev1.ConfigMap{{ObjectMeta: metav1.ObjectMeta{Name: "sys", Namespace: "kube-system"}}},
		Secrets:    []corev1.Secret{{ObjectMeta: metav1.ObjectMeta{Name: "sys", Namespace: "kube-system"}}},
		PVCs:       []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "sys", Namespace: "kube-system"}}},
	}
	if got := (ConfigMapUnusedRule{}).Run(s); len(got) != 0 {
		t.Fatalf("kube-system stays silent, got %+v", got)
	}
	if got := (SecretUnusedRule{}).Run(s); len(got) != 0 {
		t.Fatalf("kube-system stays silent, got %+v", got)
	}
	if got := (PVCUnusedRule{}).Run(s); len(got) != 0 {
		t.Fatalf("kube-system stays silent, got %+v", got)
	}
}

func notReadyNode(name string, status corev1.ConditionStatus, ago time.Duration) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type:               corev1.NodeReady,
			Status:             status,
			Reason:             "KubeletNotReady",
			Message:            "kubelet is not ready",
			LastTransitionTime: metav1.NewTime(time.Now().Add(-ago)),
		}}},
	}
}

func TestNodeNotReady_fires(t *testing.T) {
	s := &k8s.Snapshot{Nodes: []corev1.Node{notReadyNode("n1", corev1.ConditionFalse, 10*time.Minute)}}
	got := (NodeNotReadyRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "node_notready" || got[0].Severity != "critical" {
		t.Fatalf("expected notready critical, got %+v", got)
	}
	if got[0].Resource != "node/n1" {
		t.Fatalf("resource must name the node, got %q", got[0].Resource)
	}
}

func TestNodeNotReady_graceAndHealthy(t *testing.T) {
	flapping := &k8s.Snapshot{Nodes: []corev1.Node{notReadyNode("n1", corev1.ConditionFalse, time.Minute)}}
	if got := (NodeNotReadyRule{}).Run(flapping); len(got) != 0 {
		t.Fatalf("brief flaps stay silent, got %+v", got)
	}
	healthy := &k8s.Snapshot{Nodes: []corev1.Node{notReadyNode("n1", corev1.ConditionTrue, time.Hour)}}
	if got := (NodeNotReadyRule{}).Run(healthy); len(got) != 0 {
		t.Fatalf("ready nodes stay silent, got %+v", got)
	}
}

func pendingPod(ns, name string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
}

func TestPodPending_nodeHealthContext(t *testing.T) {
	pressured := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "n1"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionTrue},
		}},
	}
	s := &k8s.Snapshot{
		Pods:  []corev1.Pod{pendingPod("default", "hungry")},
		Nodes: []corev1.Node{pressured, notReadyNode("n2", corev1.ConditionFalse, time.Hour)},
	}
	got := (PodPendingRule{}).Run(s)
	if len(got) != 1 || got[0].Reason != "pod_pending" {
		t.Fatalf("expected pending finding, got %+v", got)
	}
	if _, ok := got[0].Evidence["pressured_nodes"]; !ok {
		t.Fatalf("must name pressured nodes, got %+v", got[0].Evidence)
	}
	if _, ok := got[0].Evidence["notready_nodes"]; !ok {
		t.Fatalf("must name notready nodes, got %+v", got[0].Evidence)
	}
}
