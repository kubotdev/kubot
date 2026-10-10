package main

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubotdev/kubot/internal/k8s"
)

func logTestSnap() *k8s.Snapshot {
	pod := func(ns, name string, owners ...metav1.OwnerReference) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, OwnerReferences: owners},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
		}
	}
	rs := func(name string) metav1.OwnerReference {
		return metav1.OwnerReference{Kind: "ReplicaSet", Name: name}
	}
	return &k8s.Snapshot{
		Pods: []corev1.Pod{
			pod("production", "checkout-api-7d8f9c-x2k4p", rs("checkout-api-7d8f9c")),
			pod("production", "checkout-api-7d8f9c-k8m2q", rs("checkout-api-7d8f9c")),
			pod("production", "lonely"),
		},
		Deployments: []appsv1.Deployment{
			{ObjectMeta: metav1.ObjectMeta{Name: "checkout-api", Namespace: "production"}},
		},
	}
}

func TestResolveLogPods_workload(t *testing.T) {
	pods, _ := resolveLogPods(logTestSnap(), "checkout-api")
	if len(pods) != 2 {
		t.Fatalf("want both replicas, got %d", len(pods))
	}
	if pods[0].Name >= pods[1].Name {
		t.Fatalf("pods must be sorted, got %s then %s", pods[0].Name, pods[1].Name)
	}
}

func TestResolveLogPods_exactPod(t *testing.T) {
	pods, _ := resolveLogPods(logTestSnap(), "lonely")
	if len(pods) != 1 || pods[0].Name != "lonely" {
		t.Fatalf("want the bare pod, got %+v", pods)
	}
}

func TestResolveLogPods_typoSuggests(t *testing.T) {
	pods, suggestions := resolveLogPods(logTestSnap(), "chekout-api")
	if len(pods) != 0 {
		t.Fatalf("want no pods, got %+v", pods)
	}
	found := false
	for _, s := range suggestions {
		if s == "checkout-api" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a checkout-api suggestion, got %v", suggestions)
	}
}

func TestLogReadiness_states(t *testing.T) {
	running := corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "api", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
	}}}
	if _, ok := logReadiness(&running, "api", false); !ok {
		t.Fatal("running container must read current logs")
	}
	if _, ok := logReadiness(&running, "api", true); ok {
		t.Fatal("running container with no terminated instance must refuse --previous")
	}
	waiting := corev1.Pod{Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "api", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
	}}}
	note, ok := logReadiness(&waiting, "api", false)
	if ok || !strings.Contains(note, "ImagePullBackOff") {
		t.Fatalf("waiting container must explain itself, got %q", note)
	}
	pending := corev1.Pod{}
	note, ok = logReadiness(&pending, "api", false)
	if ok || !strings.Contains(note, "no container instances") {
		t.Fatalf("status-less pod must say so, got %q", note)
	}
}

func TestLogContainer_selection(t *testing.T) {
	single := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}}}
	if got, err := logContainer(&single, ""); err != nil || got != "api" {
		t.Fatalf("single container must be implicit, got %q, %v", got, err)
	}
	multi := corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "b"}, {Name: "a"}}}}
	if _, err := logContainer(&multi, ""); err == nil {
		t.Fatal("multi-container pods must require --container")
	}
	if got, err := logContainer(&multi, "a"); err != nil || got != "a" {
		t.Fatalf("explicit container must win, got %q, %v", got, err)
	}
	if _, err := logContainer(&multi, "nope"); err == nil {
		t.Fatal("unknown container must fail")
	}
}
