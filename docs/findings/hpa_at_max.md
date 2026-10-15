# hpa_at_max

The autoscaler cannot help anymore: it wants more replicas than allowed, or
sits at max while the workload stays unhealthy.

Severity: warning

## What kubot saw

`status.desiredReplicas` above `spec.maxReplicas`, or `currentReplicas` at
max beside an unhealthy backing workload. Evidence carries min/max/current/
desired and the target name. An HPA at max with a healthy workload stays
silent — headroom gone is not an incident.

## Why it matters

Either the ceiling is too low for real load, or (worse) the autoscaler is
papering over broken pods: scaling CrashLooping replicas only multiplies
the crash count and the bill.

## Verify it yourself

```sh
kubectl get hpa <name>   # TARGETS column shows current/desired vs thresholds
kubectl top pods -l <selector>
```

## Fix

If the nodes have headroom and load is real, raise `maxReplicas`. If the
pods are unhealthy, fix that first — then watch desired fall back under
max on its own.

## When to ignore

Short traffic spikes that resolve before you finish reading this. Mute
only a deliberately capped workload:

```toml
[[ignore]]
finding = "hpa_at_max"
object = "production/batch-*"
reason = "capped by design; queue drains, no latency SLO"
```

## What kubot can't see

*Why* load rose (traffic spike vs leak vs hot loop) — metrics show the
shape, the cause lives in the app. Without metrics-server the HPA status
itself may be stale; kubot says when usage is unavailable.
