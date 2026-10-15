# hpa_noop

A HorizontalPodAutoscaler that can never scale: min equals max, or its
target workload doesn't exist.

Severity: note

## What kubot saw

`minReplicas == maxReplicas` (a fixed replica count wearing an autoscaler's
clothes), or a `scaleTargetRef` naming a deployment/statefulset that isn't
there — usually a renamed workload with a leftover HPA.

## Why it matters

Dead autoscalers hide in plain sight: dashboards show an HPA, humans assume
elasticity exists, and the 3am spike discovers it doesn't. The missing
target variant is the same stale-reference class as `service_no_endpoints`.

## Verify it yourself

```sh
kubectl get hpa <name> -o jsonpath='{.spec}'
kubectl get deployment <target>   # or statefulset
```

## Fix

Delete the HPA (a fixed count needs no autoscaler) or repoint it at the
live workload name.

## When to ignore

A deliberately pinned workload during an incident freeze — mute it with
the thaw date, not forever:

```toml
[[ignore]]
finding = "hpa_noop"
object = "production/payments-api"
reason = "pinned for freeze week, restore after"
expires = "2026-11-01"
```

## What kubot can't see

Whether the pinning is intentional policy or forgotten config — that's a
human question, which is why this is a note.
