# pod_host_access

A pod breaks container isolation: a privileged container, or shared host
namespaces (PID, network, IPC).

Severity: warning

## What kubot saw

`privileged: true` on one or more containers, or `hostPID` / `hostNetwork` /
`hostIPC` on the pod spec. Evidence names each break and, for privileged,
every container.

## Why it matters

These are never legitimate by accident. A privileged container has full host
access — a single app exploit becomes host compromise without needing a
second escape. `hostNetwork` exposes the pod to all host traffic;
`hostPID` leaks every host process into the container. Unlike running as
root (common, often harmless), isolation breaks are always worth a look.

## Verify it yourself

```sh
kubectl get pod <name> -o jsonpath='{.spec.containers[*].securityContext}'
kubectl get pod <name> -o jsonpath='{.spec.hostPID} {.spec.hostNetwork} {.spec.hostIPC}'
```

## Fix

Drop `privileged` and grant Linux capabilities instead
(`capabilities.add: ["NET_BIND_SERVICE"]` covers the classic `:80`
listener). Remove host namespaces; confine the pod with a NetworkPolicy
and `runAsNonRoot`.

## When to ignore

Only for pods that genuinely need the host: CNI / CSI / monitoring
Daemons (which usually live in `kube-system`, where kubot already stays
quiet). Anywhere else, rarely muted:

```toml
[[ignore]]
finding = "pod_host_access"
object = "monitoring/node-exporter-*"
reason = "needs host PID and network by design"
```

## What kubot can't see

Whether the *cluster* confines the blast radius anyway (PodSecurity
admission, OPA/Gatekeeper policies) — kubot reads the pod, not the policy
engine.
