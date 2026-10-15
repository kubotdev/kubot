package k8s

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func Collect(ctx context.Context, cs kubernetes.Interface, namespace string, timeout time.Duration) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	snap := &Snapshot{}
	listOpts := metav1.ListOptions{Limit: 500}

	failed, attempted := 0, 0
	var firstErr error
	collect := func(name string, fn func() error) {
		attempted++
		if err := fn(); err != nil {
			if ctx.Err() != nil {
				return
			}
			failed++
			if firstErr == nil {
				firstErr = err
			}
			snap.Degraded = append(snap.Degraded, name+": "+err.Error())
		}
	}

	collect("pods", func() error { return collectPods(ctx, listOpts, snap, cs, namespace) })
	collect("deployments", func() error { return collectDeployments(ctx, listOpts, snap, cs, namespace) })
	collect("replicasets", func() error { return collectReplicaSets(ctx, listOpts, snap, cs, namespace) })
	collect("statefulsets", func() error { return collectStatefulSets(ctx, listOpts, snap, cs, namespace) })
	collect("daemonsets", func() error { return collectDaemonSets(ctx, listOpts, snap, cs, namespace) })
	collect("services", func() error { return collectServices(ctx, listOpts, snap, cs, namespace) })
	collect("ingresses", func() error { return collectIngresses(ctx, listOpts, snap, cs, namespace) })
	collect("ingressclasses", func() error { return collectIngressClasses(ctx, listOpts, snap, cs) })
	collect("secrets", func() error { return collectSecrets(ctx, listOpts, snap, cs, namespace) })
	collect("jobs", func() error { return collectJobs(ctx, listOpts, snap, cs, namespace) })
	collect("cronjobs", func() error { return collectCronJobs(ctx, listOpts, snap, cs, namespace) })
	collect("endpointslices", func() error { return collectEndpointSlices(ctx, listOpts, snap, cs, namespace) })
	collect("events", func() error { return collectEvents(ctx, listOpts, snap, cs, namespace) })
	collect("pvcs", func() error { return collectPVCs(ctx, listOpts, snap, cs, namespace) })
	collect("nodes", func() error { return collectNodes(ctx, listOpts, snap, cs, namespace) })
	collect("hpas", func() error { return collectHPAs(ctx, listOpts, snap, cs, namespace) })
	collect("pdbs", func() error { return collectPDBs(ctx, listOpts, snap, cs, namespace) })
	collect("configmaps", func() error { return collectConfigMaps(ctx, listOpts, snap, cs, namespace) })
	collect("serviceaccounts", func() error { return collectServiceAccounts(ctx, listOpts, snap, cs, namespace) })

	if ctx.Err() == context.DeadlineExceeded {
		snap.Degraded = append(snap.Degraded, "collection timed out; results partial")
	}
	if failed == attempted {
		return nil, fmt.Errorf("cannot reach cluster: %w", firstErr)
	}
	return snap, nil
}

func collectPods(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().Pods(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Pods = res.Items
	return nil
}

func collectDeployments(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.AppsV1().Deployments(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Deployments = res.Items
	return nil
}

func collectReplicaSets(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.AppsV1().ReplicaSets(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.ReplicaSets = res.Items
	return nil
}

func collectStatefulSets(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.AppsV1().StatefulSets(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.StatefulSets = res.Items
	return nil
}

func collectDaemonSets(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.AppsV1().DaemonSets(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.DaemonSets = res.Items
	return nil
}

func collectServices(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().Services(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Services = res.Items
	return nil
}

func collectEndpointSlices(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.DiscoveryV1().EndpointSlices(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.EndpointSlices = res.Items
	return nil
}

func collectIngresses(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.NetworkingV1().Ingresses(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Ingresses = res.Items
	return nil
}

func collectJobs(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.BatchV1().Jobs(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Jobs = res.Items
	return nil
}

func collectCronJobs(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.BatchV1().CronJobs(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.CronJobs = res.Items
	return nil
}

func collectIngressClasses(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface) error {
	// Cluster-scoped; ignore the namespace filter like nodes.
	res, err := cs.NetworkingV1().IngressClasses().List(ctx, opts)
	if err != nil {
		return err
	}
	snap.IngressClasses = res.Items
	return nil
}

func collectSecrets(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().Secrets(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Secrets = res.Items
	return nil
}

func collectEvents(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Events = res.Items
	return nil
}

func collectPVCs(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().PersistentVolumeClaims(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.PVCs = res.Items
	return nil
}

func collectNodes(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, _ string) error {
	res, err := cs.CoreV1().Nodes().List(ctx, opts)
	if err != nil {
		return err
	}
	snap.Nodes = res.Items
	return nil
}

func collectHPAs(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.AutoscalingV1().HorizontalPodAutoscalers(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.HPAs = res.Items
	return nil
}

func collectPDBs(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.PolicyV1().PodDisruptionBudgets(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.PDBs = res.Items
	return nil
}

func collectConfigMaps(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().ConfigMaps(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.ConfigMaps = res.Items
	return nil
}

func collectServiceAccounts(ctx context.Context, opts metav1.ListOptions, snap *Snapshot, cs kubernetes.Interface, ns string) error {
	res, err := cs.CoreV1().ServiceAccounts(ns).List(ctx, opts)
	if err != nil {
		return err
	}
	snap.ServiceAccounts = res.Items
	return nil
}
