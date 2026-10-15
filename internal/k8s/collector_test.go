package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

// No live cluster needed.
func TestCollect_fakeClient(t *testing.T) {
	cs := fake.NewClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "s1", Namespace: "default"}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "default"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "default"}},
		&appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default"}},
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "agent", Namespace: "default"}},
		&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: "shop", Namespace: "default"}},
		&networkingv1.IngressClass{ObjectMeta: metav1.ObjectMeta{Name: "nginx"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "shop-tls", Namespace: "default"}},
		&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup", Namespace: "default"}},
		&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "nightly", Namespace: "default"}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app-cfg", Namespace: "default"}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"}},
	)
	snap, err := Collect(context.Background(), cs, "", 10*time.Second)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Pods) != 1 || len(snap.Services) != 1 || len(snap.Events) != 1 || len(snap.Nodes) != 1 || len(snap.PVCs) != 1 {
		t.Fatalf("incomplete snapshot: %+v", snap)
	}
	if len(snap.StatefulSets) != 1 || len(snap.DaemonSets) != 1 {
		t.Fatalf("missing workloads: sts=%d ds=%d", len(snap.StatefulSets), len(snap.DaemonSets))
	}
	if len(snap.Ingresses) != 1 || len(snap.IngressClasses) != 1 || len(snap.Secrets) != 1 {
		t.Fatalf("missing networking: ing=%d class=%d secrets=%d", len(snap.Ingresses), len(snap.IngressClasses), len(snap.Secrets))
	}
	if len(snap.Jobs) != 1 || len(snap.CronJobs) != 1 {
		t.Fatalf("missing batch: jobs=%d crons=%d", len(snap.Jobs), len(snap.CronJobs))
	}
	if len(snap.ConfigMaps) != 1 || len(snap.ServiceAccounts) != 1 {
		t.Fatalf("missing refs: configmaps=%d sas=%d", len(snap.ConfigMaps), len(snap.ServiceAccounts))
	}
	if len(snap.Degraded) != 0 {
		t.Fatalf("unexpected degradations: %v", snap.Degraded)
	}
}

// -n filters namespaced resources; nodes (cluster-scoped) still come back.
func TestCollect_namespaceScope(t *testing.T) {
	cs := fake.NewClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "a"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p2", Namespace: "b"}},
	)
	snap, err := Collect(context.Background(), cs, "a", 10*time.Second)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(snap.Pods) != 1 || snap.Pods[0].Name != "p1" {
		t.Fatalf("namespace filter broken: %+v", snap.Pods)
	}
}

func TestResolveContextName_explicit(t *testing.T) {
	if got := ResolveContextName(Options{Context: "my-ctx"}); got != "my-ctx" {
		t.Fatalf("got %q", got)
	}
}

func TestCollect_unreachableClusterErrors(t *testing.T) {
	cs := fake.NewClientset()
	cs.PrependReactor("list", "*", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	})
	if _, err := Collect(context.Background(), cs, "", 10*time.Second); err == nil {
		t.Fatal("total collection failure must error, never read as healthy")
	}
}

func TestCollect_partialFailureDegrades(t *testing.T) {
	cs := fake.NewClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "default"}},
	)
	cs.PrependReactor("list", "events", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	snap, err := Collect(context.Background(), cs, "", 10*time.Second)
	if err != nil {
		t.Fatalf("partial failure must not error: %v", err)
	}
	if len(snap.Pods) != 1 || len(snap.Degraded) == 0 {
		t.Fatalf("expected pods plus degraded notes: %+v", snap)
	}
}
