# pdb_no_match

A PodDisruptionBudget whose selector matches no pods — it protects
nothing.

Severity: note

## What kubot saw

The PDB's label selector matched zero pods in its namespace. Either the
workload was renamed and the budget left behind, or the selector was
wrong from the start — the same stale-selector class as
`service_no_endpoints`.

## Why it matters

A guarding-nothing budget is confusion waiting to happen: during the next
drain, someone reads it, believes the workload is protected, and learns
otherwise. Delete or fix — both are one line.

## Verify it yourself

```sh
kubectl get pdb <name> -o jsonpath='{.spec.selector}'
kubectl get pods -l <selector>
```

## Fix

Point the selector at the live workload labels, or delete the PDB.

## When to ignore

Briefly, during a rename in flight. Otherwise never — mute nothing here,
fix the selector.

## What kubot can't see

Whether a matching workload is *about* to appear (a deploy racing the
check). Re-run after the rollout settles.
