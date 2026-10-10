# Logs

`kubot logs` prints recent container logs for a workload's pods — the
evidence behind crash findings. Same names as `why` (deployment,
statefulset, daemonset, job, service, or pod name, with typo suggestions).

```sh
kubot logs billing-worker -n production
kubot logs checkout-api --previous -n production   # last terminated container: where a CrashLoopBackOff cause lives
kubot logs search-api --tail 50 --container api
```

Flags: `--previous`, `--tail N` (default 100 lines, `-1` for all),
`--container` (required when a pod runs more than one, like kubectl).
Every pod prints under a `--- namespace/pod (container …)` header so
multi-replica output stays greppable.

Read-only: pure GETs on `pods/log` — never exec, never attach. Log text is
evidence, not a stable interface: read it, don't parse it (no `--json`).

## RBAC

The stock read-only role often lacks log access. If the API answers
Forbidden, kubot names the missing grant instead of failing silently:

```yaml
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
```

`--previous` on a container with no terminated instance reports the API's
own "previous terminated container not found" error for that pod; other
pods still print. If every pod fails, the exit code is 3.
