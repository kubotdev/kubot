# pvc_unused

A PersistentVolumeClaim is mounted by nothing — no pod volume and no
workload template references it.

Severity: note

## What kubot saw

No `persistentVolumeClaim.claimName` match in any live pod or any
deployment / statefulset / daemonset / job / cronjob template. Evidence
carries the claim's age, phase, and requested storage. Templates count on
purpose: a claim mounted only by a weekly CronJob looks unused mid-week
if live pods are all you check.

## Why it matters

Orphaned claims bill storage silently — the classic is a StatefulSet
scaled down, its per-replica PVCs left behind forever. Unused is not
broken, which is why this is a note, but storage is the one resource that
charges you for forgetting.

## Verify it yourself

```sh
kubectl get pvc <name> -o jsonpath='{.status.phase} {.spec.volumeName}'
kubectl get pods --all-namespaces -o jsonpath='{..persistentVolumeClaim.claimName}' | tr ' ' '\n' | sort -u
```

## Fix

If nothing will ever mount it again, delete it (`kubectl delete pvc`).
Keep data-retention volumes — then mute with the reason, not silence:

```toml
[[ignore]]
finding = "pvc_unused"
object = "production/audit-archive-*"
reason = "seven-year retention, deleted by policy 2031"
```

## What kubot can't see

Whether the data on the volume still matters — kubot sees the claim,
never the bytes. And a claim with `Retain` policy keeps its volume after
deletion: dropping the PVC is step one, reclaiming the disk is step two.
