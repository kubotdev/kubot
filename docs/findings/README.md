# Findings catalogue

Every reason kubot can emit, what it means, and how to verify, fix, or mute
it. Finding ids are stable: `kubot inspect --json` reports them in `reason`.

| Reason | Severity | Meaning |
|---|---|---|
| [pod_crashloop_backoff](pod_crashloop_backoff.md) | critical | container keeps crashing (3+ recent restarts) |
| [pod_oom_killed](pod_oom_killed.md) | critical | container exceeded its memory limit (exit 137) |
| [pod_imagepull_backoff](pod_imagepull_backoff.md) | critical | image can't be pulled |
| [pod_pending](pod_pending.md) | warning | pod never scheduled |
| [pod_probe_failing](pod_probe_failing.md) | warning | readiness/liveness/startup probe failing |
| [deployment_unavailable](deployment_unavailable.md) | critical | deployment has unavailable replicas |
| [statefulset_unavailable](statefulset_unavailable.md) | critical | statefulset has unavailable replicas |
| [daemonset_unavailable](daemonset_unavailable.md) | critical | daemonset has unavailable pods |
| [service_no_endpoints](service_no_endpoints.md) | warning | service selects nothing ready |
| [pod_missing_resources](pod_missing_resources.md) | note | containers without limits/requests |
| [pod_low_limit](pod_low_limit.md) | warning | memory limit under 32Mi |
| [pod_near_limit](pod_near_limit.md) | warning | usage at 80%+ of memory limit |
| [deployment_resource_risk](deployment_resource_risk.md) | note | half+ pods limitless |
| [deployment_rollout_stalled](deployment_rollout_stalled.md) | warning | rollout past its deadline |
| [deployment_rollout_recent](deployment_rollout_recent.md) | note | rolled out within the last hour |
| [replicaset_leftover](replicaset_leftover.md) | note | old ReplicaSets still running |
| [pvc_pending](pvc_pending.md) | warning | claim unbound, pods blocked |
| [pod_mount_failure](pod_mount_failure.md) | warning | kubelet can't mount a volume |
| [node_pressure](node_pressure.md) | warning | memory/disk/PID pressure |
| [node_notready](node_notready.md) | critical | node stopped reporting Ready |
| [ingress_no_backends](ingress_no_backends.md) | warning | route to missing/unready service |
| [ingress_unknown_class](ingress_unknown_class.md) | warning | class nobody implements |
| [ingress_tls_secret_missing](ingress_tls_secret_missing.md) | warning | TLS secret doesn't exist |
| [job_failed](job_failed.md) | warning | job failed with no successes |
| [cronjob_failing](cronjob_failing.md) | warning | latest scheduled run failed |
| [pod_host_access](pod_host_access.md) | warning | privileged container or host namespaces |
| [workload_running_as_root](workload_running_as_root.md) | note | every pod runs as root |
| [hpa_at_max](hpa_at_max.md) | warning | autoscaler maxed, still unhealthy |
| [hpa_noop](hpa_noop.md) | note | min==max or target missing |
| [pdb_blocking](pdb_blocking.md) | warning | zero voluntary evictions allowed |
| [pdb_no_match](pdb_no_match.md) | note | PDB selector matches nothing |
| [cronjob_suspended](cronjob_suspended.md) | warning | schedule paused, runs nothing |
