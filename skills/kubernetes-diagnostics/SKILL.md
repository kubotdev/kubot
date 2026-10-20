---
name: kubernetes-diagnostics
description: Diagnose Kubernetes health and performance with the read-only kubot tool. Use for unhealthy clusters or namespaces, crashlooping or OOMKilled pods, image pull failures, failing probes, pending pods, resource pressure, service/endpoint problems, event storms, "why is my deployment down", and any k8s health, networking, or resource question.
---

# Kubernetes diagnostics with kubot

kubot is a **read-only** Kubernetes diagnostic tool. Its findings are computed
**deterministically** in Go from the cluster's own API state — never by a model.
It only ever performs GET/LIST. Treat its output as the source of truth. Your
job is to run the right command, then interpret, prioritize, and recommend —
never to invent diagnoses or apply changes.

## When to use this skill

- "why is my deployment/pod down", "is the cluster healthy", "what's failing?"
- CrashLoops, OOMKills, image pull errors, failing probes, pending pods
- "what's eating the resources", "why isn't traffic reaching the pods"
- "what changed since yesterday", CI cluster health gates
- Any Kubernetes health / networking / resource / events question

## How to run it

**If the kubot MCP server is connected**, call its tools: `inspect` (start
here), `why <workload>`. They return stable JSON. (The `events`, `resources`,
`networking`, `logs`, `diff`, `check`, `report`, and `ask` commands are
CLI-only for now — run them through the shell.)

**Otherwise run the CLI.** Global flags: `-n/--namespace` (default all),
`--context`, `--kubeconfig`, `--timeout`, `--no-color`.

| Question | Command |
|---|---|
| Open-ended / "is it healthy?" | `kubot inspect` (health score + worst-first findings) |
| One namespace | `kubot inspect -n production` |
| Full detail (evidence + recommendations) | `kubot inspect --full` |
| "Why is workload X down?" | `kubot why <workload> [-n ns]` (always full detail; typo suggestions) |
| "What has been failing?" | `kubot events` (warning events as a table, most repeated first) |
| "What's using resources / missing limits?" | `kubot resources` |
| "Why isn't traffic reaching pods?" | `kubot networking` (service, probe, ingress findings) |
| "Show me recent logs / the last crash" | `kubot logs <workload> [--previous] [--tail N]` |
| "What changed since yesterday?" | `kubot diff --since 24h` (stored snapshots, offline) |
| CI gate (exit-code contract) | `kubot check --fail-on=critical` |
| Full report for a human | `kubot report -o report.html` (self-contained page) |
| Plain-language reading | `kubot ask "…"` / `kubot explain [workload]` (AI layer — confirms first) |
| Machine-readable for parsing | `kubot inspect --json` (versioned contract, JSON Schema in `schema/`) |

SARIF/JUnit for pipelines: `kubot inspect --format=sarif` (GitHub Security
tab) · `--format=junit` (Jenkins/GitLab panes).

## First run — when something is missing

Don't stall and don't improvise; each gap has one fix:

- **`kubot` not on PATH:** install it —
  `curl -fsSL https://raw.githubusercontent.com/kubotdev/kubot/main/install.sh | sh`
  (or `brew install kubotdev/tap/kubot`, or `go install
  github.com/kubotdev/kubot/cmd/kubot@latest`). Run it through the normal
  permission prompt so the user sees the command.
- **`kubot: cannot reach cluster`:** loud by design — never a fake-OK. Check
  `--context` / `$KUBECONFIG`; after a Docker Desktop restart, kind's published
  port can go missing (`docker ps`, `kubectl cluster-info`).
- **`unavailable: pod usage unavailable`:** metrics-server isn't installed or
  isn't serving. Usage checks degrade with this visible note; everything else
  works. Install metrics-server to clear it.
- **Workload looks sick but the report is clean:** pass `-n` (kubot searches
  all namespaces, but names must be right — a typo gets a `Did you mean:`
  suggestion). `kube-system` and mirror pods are skipped where the advice
  would be unactionable.

## Rules — non-negotiable

1. **Findings are facts.** Every finding is computed in Go from API state. Do
   not invent a number, reason, or workload beyond kubot's report. If kubot
   didn't measure it, say what it would need to collect.
2. **kubot never writes — neither do you.** Recommend; don't apply. Hand the
   user the exact `kubectl` command (or, in a kubox environment, the re-sync
   steps) to run themselves.
3. **Carry every caveat.** Usage checks need metrics-server; suppressed
   findings stay visible with their written reason and never move the exit
   code or score.
4. **Prioritize by time-to-incident, not by finding count.** A 44-restart
   OOMKilled pod beats a leftover ReplicaSet. Say which to fix first and why.
5. **Exit codes are a scriptable contract:** `0` clean · `1` warning ·
   `2` critical · `3` connection failure · `64` usage error. `inspect`
   reports (default `--fail-on=none`); `check` gates.
6. **Parse `--json`, not the terminal output.** The human-readable report is
   not a stable interface; the versioned `--json` contract (`schema_version`,
   currently `1.0.0`, JSON Schema published in the repo's `schema/`) is.
7. **Hedge the AI layer.** `ask`/`explain` sends findings to your configured
   model — confirm first, name the provider. The findings stay deterministic;
   the model only interprets. Local endpoints send nothing off the box.
8. **Never loosen a check to make it pass.** Expected noise gets a
   `.kubot.toml` `[[ignore]]` rule with a written reason — suppression is
   visible, not hidden.

## Output shape

1. One-line verdict: healthy, or the count of critical/warning findings.
2. Worst-first, at most ~3 issues. For each: the resource + reason **with
   kubot's evidence** → a likely cause **only if the data supports one** → a
   concrete, safe next step, caveat inline.
3. Briefly name what's healthy and what was checked (`checked · pods ·
   deployments · …`), so the user knows the coverage.

## Reading the signals

- **`pod_oom_killed` (exit 137):** memory limit too low or a leak — evidence
  carries the limit and restart count. Cross-check `kubot resources`.
- **ImagePullBackOff:** the tag was never published or the registry auth is
  wrong — fix the tag, re-roll.
- **Failing probes:** kubot flags only *sustained* probe failures; Ready pods
  stay silent. A readiness failure blackholes traffic before a liveness kill.
- **service selector matches no pods:** endpoint emptiness — a traffic
  blackhole that looks "up" from the outside.
- **Pending pods:** read the scheduler context (quota, node pressure,
  unschedulable) — it's in the finding's evidence.
- **deployment risk / stalled rollouts:** what the next rollout will break;
  treat as pre-incident.

## Safety and privacy

kubot only ever reads (GET/LIST against the API server) — a read-only role is
sufficient and there is nothing a diagnosis can break. Nothing leaves the
machine except `kubot ask` / `kubot explain`, which send the findings to your
configured model and say so first, naming the provider and model. Local
endpoints (Ollama, vLLM) send nothing off the box.
