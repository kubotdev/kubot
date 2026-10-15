# configmap_unused

A ConfigMap is referenced by nothing — no `envFrom`, no `valueFrom`, no
volume (including projected), in any pod or workload template.

Severity: note

## What kubot saw

No `configMapRef`, `configMapKeyRef`, or `configMap` volume match in any
live pod or any deployment / statefulset / daemonset / job / cronjob
template, with projected volume sources included. Evidence carries the
age.

## Why it matters

Stale config is how the wrong values come back from the dead: somebody
re-applies an old manifest, the ancient ConfigMap still sits there, and
the app boots with last year's feature flags. Unused is not broken —
note, not warning — but config rots faster than code.

## Verify it yourself

```sh
kubectl get configmap <name> -o jsonpath='{.data}'   # is it even meaningful?
kubectl get pods --all-namespaces -o jsonpath='{..configMapRef.name} {..configMapKeyRef.name}'
```

## Fix

Check no external referrer (CI-applied manifests, GitOps staging of a
future release), then delete it. `kube-system` is always skipped —
system namespaces own their lifecycle.

## What kubot can't see

Referrers outside the cluster's view: manifests applied by CI but not yet
deployed, Helm values files pointing at a name, operators that read
ConfigMaps directly through their own clients. Confirm before deleting.
