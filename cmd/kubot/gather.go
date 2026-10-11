package main

import (
	"context"
	"os"

	"github.com/kubotdev/kubot/internal/config"
	"github.com/kubotdev/kubot/internal/diagnose"
	"github.com/kubotdev/kubot/internal/history"
	"github.com/kubotdev/kubot/internal/k8s"
	"github.com/kubotdev/kubot/internal/model"
)

// Shared pipeline every command and MCP tool runs: connect, collect, diagnose.
func gather(ctx context.Context) (model.Report, *k8s.Snapshot, error) {
	return gatherScoped(ctx, namespaceFlag)
}

func gatherScoped(ctx context.Context, namespace string) (model.Report, *k8s.Snapshot, error) {
	cs, err := k8s.NewClientset(k8s.Options{Kubeconfig: kubeconfigFlag, Context: contextFlag})
	if err != nil {
		return model.Report{}, nil, err
	}
	snap, err := k8s.Collect(ctx, cs, namespace, timeoutFlag)
	if err != nil {
		return model.Report{}, nil, err
	}
	if mc, err := k8s.NewMetricsClient(k8s.Options{Kubeconfig: kubeconfigFlag, Context: contextFlag}); err == nil {
		k8s.CollectMetrics(ctx, mc, namespace, snap)
	} else {
		snap.Degraded = append(snap.Degraded, "pod usage unavailable ("+err.Error()+")")
	}
	snap.Context = k8s.ResolveContextName(k8s.Options{Kubeconfig: kubeconfigFlag, Context: contextFlag})
	engine := diagnose.NewEngine()
	findings := engine.Run(snap)
	if findings == nil {
		findings = []model.Finding{}
	}
	if path := config.Discover(configFlag, os.Getenv("KUBOT_CONFIG")); path != "" {
		cfg, err := config.Load(path)
		if err != nil {
			return model.Report{}, nil, err
		}
		for i, f := range findings {
			if reason, ok := cfg.Match(f.Reason, f.Namespace, f.Resource); ok {
				findings[i].Suppressed = true
				findings[i].SuppressionReason = reason
			}
		}
	}
	rep := model.Report{
		SchemaVersion: model.SchemaVersion,
		Cluster:       model.ClusterInfo{Context: snap.Context, Namespace: namespace},
		Status:        model.OverallStatus(findings),
		Issues:        findings,
		Checked:       diagnose.CheckedSubsystems(),
		Degraded:      snap.Degraded,
	}
	// Every run leaves a digest for `kubot diff`
	if dir, err := history.Dir(); err != nil {
		rep.Degraded = append(rep.Degraded, "history not saved ("+err.Error()+")")
	} else if err := history.Save(dir, history.Fingerprint(snap.Context), history.FromSnapshot(snap, rep, snap.Context, namespace)); err != nil {
		rep.Degraded = append(rep.Degraded, "history not saved ("+err.Error()+")")
	}
	return rep, snap, nil
}
