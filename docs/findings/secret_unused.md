# secret_unused

A Secret is referenced by nothing — no env, no volume, no ingress TLS, no
pull secret, in any pod, workload template, or ServiceAccount.

Severity: note

## What kubot saw

No `secretRef`, `secretKeyRef`, `secret` volume (including projected
sources), ingress `tls.secretName`, pod `imagePullSecrets`, or
ServiceAccount pull-secret match. Evidence carries the age and the type.
Helm release-storage secrets and legacy service-account tokens are never
flagged — those are somebody's state, not garbage.

## Why it matters

Every stored credential is breach surface. Rotated passwords, abandoned
registry tokens, and experiment leftovers accumulate because nothing ever
tells anyone they're dead. Unused is not broken — note — but secrets are
the one garbage category with security weight.

## Verify it yourself

```sh
kubectl get secret <name> -o jsonpath='{.type}'
kubectl get pods --all-namespaces -o jsonpath='{..secretRef.name} {..secretKeyRef.name}'
```

## Fix

Verify no invisible referrer first — external-secrets / vault CSI (which
consume without in-cluster references), CI jobs that apply-then-use,
pull secrets attached via ServiceAccount rather than pod — then delete.
Rotate rather than just delete if the credential may have leaked.

## When to ignore

Pre-provisioned secrets for a workload deploying next week, or
break-glass credentials kept deliberately:

```toml
[[ignore]]
finding = "secret_unused"
object = "production/dr-breakglass-*"
reason = "kept deliberately, rotated quarterly"
```

## What kubot can't see

The invisible referrers above, plus anything outside RBAC reach. A secret
kubot can't see referenced is *probably* unused — the catalogue lists
every exception known, but deletion deserves one human glance.
