# node_notready

A node stopped reporting Ready — its capacity is gone.

Severity: critical

## What kubot saw

The `Ready` condition `False` (or `Unknown`) for longer than 5 minutes,
with the kubelet reason and message as evidence. Brief flaps (kubelet
restarts) stay silent — only sustained NotReady fires.

## Why it matters

Vanishing capacity with no attribution is the worst kind: workloads
reschedule elsewhere (or, on a single-node cluster, simply die) while
nothing names the cause. A NotReady node is never intentional, unlike a
cordoned one — kubot deliberately does not flag cordons, which are routine
maintenance.

## Verify it yourself

```sh
kubectl get nodes
kubectl describe node <name>   # Conditions, kubelet messages
journalctl -u kubelet          # on the node itself
```

## Fix

Check kubelet and node health: disk pressure, network to the API server,
expired certificates, dead container runtime. The rescheduled workloads
show up as their own kubot findings — fix the node, not its refugees.

## When to ignore

Almost never. Mute only a node you know is gone for good and already
replaced:

```toml
[[ignore]]
finding = "node_notready"
object = "node/old-worker-3"
reason = "decommissioned, replacement joined"
```

## What kubot can't see

*Why* the kubelet stopped reporting — that answer lives on the node
(logs, systemd), not in the API.
