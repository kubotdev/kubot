# pod_pending

A pod was never assigned to a node. (Assigned-but-stuck pods — image pulls,
volume mounts — belong to their own rules.)

Severity: warning

## What kubot saw

Pod in phase `Pending` with no `nodeName` (already-scheduled pods are
another rule's problem), plus the `FailedScheduling` event message, the
pod's CPU/memory requests, nodeSelector, cluster taint/cordon context,
and the named pressured and NotReady nodes behind the failure.

## Why it matters

Pending pods serve no traffic and consume no resources — they're pure
capacity or constraint mismatch. The scheduler message usually names it
(`Insufficient cpu`, unbound PVCs, no matching nodes).

## Verify it yourself

```sh
kubectl describe pod <pod>   # Events: the FailedScheduling detail
kubectl get nodes
kubectl describe nodes | grep -i taint
```

## Fix

Match the message: lower requests or add capacity (`Insufficient cpu`),
bind the PVC, relax nodeSelector/affinity, or add tolerations for the taint.

## When to ignore

If the pod is *supposed* to wait (e.g. a job queued behind a quota):

```toml
[[ignore]]
finding = "pod_pending"
object = "default/queued-*"
reason = "waits for nightly quota window"
```

## What kubot can't see

Cluster-autoscaler intent (it may be *about* to add a node) and quota objects
— check those if the message mentions them.
