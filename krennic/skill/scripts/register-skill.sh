#!/bin/sh
# Zaregistruje Krennic skill pro Claude Code (macOS / Linux).
# Vytvoří odkaz ~/.claude/skills/krennic -> tato složka krennic/skill,
# takže Claude Code pozná /krennic a umí „nainstaluj krennic".
set -eu

# Fyzická cesta k této skill složce (funguje i přes symlink).
SKILL_DIR="$(cd "$(dirname "$0")/.." && pwd -P)"
DEST="$HOME/.claude/skills/krennic"

mkdir -p "$HOME/.claude/skills"
rm -rf "$DEST"
ln -s "$SKILL_DIR" "$DEST"

echo "✓ Skill zaregistrován:"
echo "    $DEST  ->  $SKILL_DIR"
echo ""
echo "Teď v Claude Code funguje /krennic — nebo mu řekni: „nainstaluj a spusť krennic“."
