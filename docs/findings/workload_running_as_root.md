# workload_running_as_root

Every pod behind a workload runs as root — no `runAsNonRoot` in any
container or pod security context.

Severity: note

## What kubot saw

All pods owned by one workload (deployment, statefulset, daemonset — or a
bare pod judged alone) run with uid 0: nothing sets `runAsNonRoot: true`
or a non-zero `runAsUser`, and nothing overrides the image default.

## Why it matters

Root in a container is the default, not an incident — which is why this is
a note, aggregated to one per workload instead of one per pod. It still
matters: an app exploit (RCE) as root plus a kernel escape path is host
compromise; the same exploit as a powerless user stays in its box. This
finding is the cheap half of that hardening.

## Verify it yourself

```sh
kubectl get pod <name> -o jsonpath='{.spec.containers[*].securityContext} {.spec.securityContext}'
```

## Fix

Set `runAsNonRoot: true` (and a non-zero `runAsUser`) at pod level so every
container inherits it — unless the image needs root: databases
initializing data dirs, sub-1024 ports. Then keep root and document why.

## When to ignore

When root is load-bearing. Mute per workload, never blanket:

```toml
[[ignore]]
finding = "workload_running_as_root"
object = "production/postgres-*"
reason = "PGDATA init requires root; drops to postgres at runtime"
```

## What kubot can't see

`seccompProfile`, `allowPrivilegeEscalation`, and `readOnlyRootFilesystem`
are deliberately unchecked: their safe values are near-universal defaults
(or cluster-enforced), so flagging them would be noise. kubot also can't
see kubelet flags, so it can't tell a cluster that enforces a seccomp
default from one that enforces nothing — unactionable advice stays out.
