# deployment_rollout_recent

A deployment rolled out within the last hour — a new revision, possibly a new
image. This is the "what changed" timestamp to correlate fresh symptoms
against.

Severity: note

## What kubot saw

The deployment's newest ReplicaSet (by creation time, revision annotation as
tiebreak) is younger than one hour. Evidence carries the revision, the new
image list, the age, and — when a previous set exists — its revision and
images.

## Why it matters

Most production incidents start with a change, and the change is usually a
deploy. A rollout minutes old beside fresh CrashLoops, OOMKills, or pending
pods is the first correlation to check — before blaming the platform. Older
than an hour, a rollout is history: kubot goes silent rather than cry
"changed" forever.

## Verify it yourself

```sh
kubectl rollout history deployment/<name>
kubectl get replicasets -l <selector> --sort-by=.metadata.creationTimestamp
```

## Fix

Nothing to fix — this is context, not a problem. If symptoms started with
this rollout, `kubectl rollout undo deployment/<name>` (or fix forward),
then confirm with `kubot why <name>`.

## When to ignore

Mute it per deployment when you deploy constantly and the note is noise:

```toml
[[ignore]]
finding = "deployment_rollout_recent"
object = "production/frontend-*"
reason = "continuous delivery, several rollouts per hour"
```

## What kubot can't see

*Why* the new revision behaves differently — diff the image tags and the
app's own changelog. And kubot only sees rollouts younger than an hour;
for anything older, `kubectl rollout history` is the record.
