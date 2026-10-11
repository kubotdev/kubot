# History & diff

Every inspect-family run (`inspect`, `why`, `events`, `resources`,
`networking`, `check`, `ask`, `explain`, and the MCP tools) quietly saves a
small digest of what it saw. `kubot diff` compares two of them: what got
better, what got worse, and what changed images.

```sh
kubot diff                 # newest run vs nearest run older than 24h
kubot diff --since 1h      # newest run vs nearest run older than 1h
kubot diff --json          # machine-readable
```

```
diff · kind-kubot-prod · production
2026-10-10 09:12 → 2026-10-10 16:40  ·  7h28m elapsed

RESOLVED (1)
● pod/checkout-api-85566fcb9f-5ph2n (production)
    Container "api" cannot pull image "hashicorp/http-echo:0.2.99" (ImagePullBackOff)

NEW (1)
● pod/billing-worker-5cbb5b98dc-zrr89 (production)
    Container "worker" was OOMKilled (exit 137)

CHANGED IMAGES (1)
● deployment/shopfront (production)
    nginx:1.27-alpine → nginx:1.28-alpine

4 unchanged.
```

## Semantics

- **Window-anchored, not consecutive.** `--since` picks the nearest stored
  run at least that old; the latest run is always the other side. Ten runs
  a day with `--since 24h` compares roughly first-vs-last; `--since 1h`
  compares roughly consecutive runs.
- **Honest about the window.** If the nearest older snapshot isn't near
  `--since`, the header says what was actually compared
  ("the nearest older snapshot is 31h back — comparing that").
- **Scope must match.** A `-n production` run never compares against an
  all-namespaces run — that would manufacture fake resolved/new findings.
  With no usable pair, `diff` errors plainly instead of inventing one
  (run `kubot inspect` twice first).
- **Identity, not text.** Findings compare by reason + resource; reworded
  human messages don't count as change. Image changes compare workload
  image lists (currently deployments).
- **Informational.** `diff` always exits 0 on a successful comparison —
  it's a report, not a gate. Use `check --fail-on` for pipelines.

## What's stored, and where

One JSON file per run under `$XDG_STATE_HOME/kubot/history/<context>/`
(`KUBOT_STATE_HOME` overrides the root; `~/.local/state` is the default):
timestamp, scope, finding identities with messages, and per-deployment
images + revisions + replica counts. No logs, no secrets, no metric values.
Pruned on every save (30 days / 500 files per context). Saving never fails
an inspection — a write error becomes a `Degraded` note. To purge
everything: delete the directory.
