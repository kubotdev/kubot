# Commands

Global flags (every command): `--kubeconfig`, `--context`, `-n/--namespace`
(default: all namespaces), `--timeout`, `--no-color`, `--config`
(see [Configuration](configuration.md)).

| Command | What it does |
|---|---|
| `inspect [workload]` | full health report; `--json`, `--format=text\|json`, `--fail-on=…`, `--full` for evidence per finding |
| `why <workload>` | the diagnosis filtered to one workload, always in full detail; suggests names on typos |
| `events` | warning events as a table, most repeated first |
| `resources` | per-container requests/limits/usage table plus resource findings |
| `networking` | service, probe, and ingress findings |
| `logs <workload\|pod>` | recent container logs per pod (`--previous`, `--tail`, `--container`) |
| `check` | CI gate: one line per issue plus the exit code |
| `ask "<question>"` | plain-language answer grounded in the findings (see [AI layer](ai.md)) |
| `explain [workload]` | the report plus an AI reading of it |
| `mcp` | MCP server on stdio (see [MCP](mcp.md)) |

## Exit codes

A scriptable contract: `0` clean · `1` warning · `2` critical · `3`
connection/execution failure · `64` bad flags. `inspect` exits 0 by
default; `--fail-on=critical|warn|info|none` opts into gating, and `check`
exists so pipelines don't have to remember flags. Suppressed findings never
move the code.
