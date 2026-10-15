# pdb_blocking

A PodDisruptionBudget forbids every voluntary eviction: `minAvailable`
covers all matching pods, so node drains and rollouts hang.

Severity: warning

## What kubot saw

The PDB's selector matched N pods and the resolved `minAvailable` (plain
number, or percentage-ceiled) is ≥ N. Zero evictions allowed: every drain,
upgrade, or rollout touching those nodes blocks indefinitely.

## Why it matters

This is the finding that pages during a node upgrade, not the one
that causes it. An over-strict PDB — often copy-pasted `minAvailable: 3`
onto a 3-replica workload, or 100% — turns routine maintenance into a
standoff between the drain and the budget.

## Verify it yourself

```sh
kubectl get pdb <name> -o jsonpath='{.spec} {.status}'
kubectl get pods -l <selector>   # count them against minAvailable
```

## Fix

Lower `minAvailable` (or set `maxUnavailable: 1`): one voluntary
disruption at a time keeps availability while letting operations proceed.

## When to ignore

Singletons that genuinely cannot move (a single-writer database): then the
block is the intent, and the note belongs in config:

```toml
[[ignore]]
finding = "pdb_blocking"
object = "production/postgres-*"
reason = "single writer; drains are manual and scheduled"
```

## What kubot can't see

Involuntary disruptions (node crashes, kubelet evictions) bypass PDBs
entirely — a budget is a politeness contract with the cluster, not armor.
