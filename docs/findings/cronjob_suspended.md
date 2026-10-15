# cronjob_suspended

A CronJob is suspended — the schedule runs nothing, and everything looks
clean precisely because of that.

Severity: warning

## What kubot saw

`spec.suspend: true` with the schedule and the last scheduled time as
evidence. No failed jobs, no missing runs in the failing sense — silence.

## Why it matters

Missed backups hide here. A pause nobody remembers (incident triage six
months ago, a migration that finished, a "temporarily" that stuck) reads
as healthy in every dashboard. The failure mode isn't an error — it's the
absence of the thing that was supposed to happen.

## Verify it yourself

```sh
kubectl get cronjob <name> -o jsonpath='{.spec.suspend} {.spec.schedule} {.status.lastScheduleTime}'
```

## Fix

Resume it (`suspend: false`) if the pause was temporary, or delete the
CronJob if the job is retired. A paused schedule should always have an
owner and a reason.

## When to ignore

Seasonal jobs parked on purpose (year-end reconciliation in March). Mute
with the resume date:

```toml
[[ignore]]
finding = "cronjob_suspended"
object = "production/year-end-*"
reason = "seasonal; resumes in December"
expires = "2026-12-01"
```

## What kubot can't see

*Why* it was suspended — git blame the manifest, not the cluster.
