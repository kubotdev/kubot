# kubot skills

Agent **skills** that teach an AI assistant how to use kubot *well* — the
judgment layer on top of the tools: findings are facts, never write, carry
caveats, prioritize by time-to-incident, parse `--json`.

A skill pairs naturally with the kubot **MCP server** (`kubot mcp`): MCP gives
the agent the read-only findings, the skill gives it the playbook.

## `kubernetes-diagnostics`

Turns "is my cluster healthy" / "why is my deployment down" into a
prioritized, caveat-aware diagnosis backed by kubot's deterministic findings.

### Install

Works in **Claude Code, Cursor, and Codex** (and any Skills-aware agent). The
[`skills` CLI](https://github.com/vercel-labs/skills) detects which agents you
use and installs to the right place for each:

```bash
npx skills add kubotdev/kubot
```

Or the kubot one-liner (Claude Code / Skills-aware clients):

```bash
curl -fsSL https://raw.githubusercontent.com/kubotdev/kubot/main/skills/install.sh | sh
```

It drops the skill at `~/.claude/skills/kubernetes-diagnostics/SKILL.md`
(override the target with `KUBOT_SKILL_DIR`, e.g. `.claude/skills` for a
project-scoped install). Or fetch the raw file directly:

```bash
mkdir -p ~/.claude/skills/kubernetes-diagnostics
curl -fsSL https://raw.githubusercontent.com/kubotdev/kubot/main/skills/kubernetes-diagnostics/SKILL.md \
  -o ~/.claude/skills/kubernetes-diagnostics/SKILL.md
```

Pair it with the MCP server so the agent has both the findings and the playbook:

```bash
claude mcp add kubot -- kubot mcp
```

Then ask your agent *"is my cluster healthy?"* — it invokes the skill, calls
kubot's tools, and gives you a prioritized diagnosis with the caveats intact.

> The `SKILL.md` format is a small frontmatter block (`name`, `description`)
> followed by the instructions. `description` is what the agent matches on to
> decide the skill is relevant, so it names the situations kubot is for.
