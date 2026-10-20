#!/bin/sh
# kubot skill installer. Downloads the kubernetes-diagnostics SKILL.md into your
# Claude skills directory so an agent knows how to use kubot well.
#
#   curl -fsSL https://raw.githubusercontent.com/kubotdev/kubot/main/skills/install.sh | sh
#
# Env:
#   KUBOT_SKILL      skill name to install (default: kubernetes-diagnostics)
#   KUBOT_SKILL_DIR  target skills dir    (default: ~/.claude/skills)
set -eu

REPO="kubotdev/kubot"
SKILL="${KUBOT_SKILL:-kubernetes-diagnostics}"
SKILL_DIR="${KUBOT_SKILL_DIR:-$HOME/.claude/skills}"

say() { printf 'kubot-skill: %s\n' "$1" >&2; }
die() { say "error: $1"; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"

src="https://raw.githubusercontent.com/$REPO/main/skills/$SKILL/SKILL.md"
dest="$SKILL_DIR/$SKILL"

say "installing '$SKILL' skill to $dest"
mkdir -p "$dest" || die "cannot create $dest"
curl -fsSL "$src" -o "$dest/SKILL.md" || die "download failed ($src)"

# Sanity check: a real skill starts with YAML frontmatter.
head -n1 "$dest/SKILL.md" | grep -q '^---' || die "downloaded file is not a SKILL.md"

say "installed $dest/SKILL.md"
say "start a new agent session to pick it up. Pair it with the tools:"
say "  claude mcp add kubot -- kubot mcp"
