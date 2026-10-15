# cronjob_failing

A CronJob's latest run failed with no success since.

Severity: warning

## What kubot saw

The newest terminally-failed job owned by the CronJob, plus the schedule.
A later success clears it — recovery is part of the check, not an
afterthought.

## Why it matters

Scheduled work silently not happening is worse than a loud crash: backups
not taken, reports not sent, cleanup not run. Nobody watches cron output.

## Verify it yourself

```sh
kubectl get jobs -l <selector>   # the failed runs
kubectl logs job/<failed-run>
```

## Fix

Same as a failed job (see `job_failed`), plus check the schedule itself —
wrong cron expression, wrong timezone assumption, or a `startingDeadline`
that expired before the run began.

## When to ignore

CronJobs paused by suspension (flagged separately as `cronjob_suspended`)
or schedules under maintenance:

```toml
[[ignore]]
finding = "cronjob_failing"
object = "default/nightly-report"
reason = "paused upstream until migration finishes"
```

## What kubot can't see

Missed schedules that never created a job (controller down, deadline
missed) — no failed job object exists to point at. `kubectl get cronjob`
shows `LAST SCHEDULE` age for that.
