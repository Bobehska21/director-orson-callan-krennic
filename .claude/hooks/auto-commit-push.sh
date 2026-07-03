#!/usr/bin/env bash
# Stop hook: po dokončení práce automaticky commitne a pushne změny.
# No-op, když v pracovním stromu nejsou žádné změny (např. jen konverzace).
set -u

# Přečti stdin hooku — pokud jsme uvnitř dalšího Stop hooku, skonči (proti smyčce).
input="$(cat 2>/dev/null || true)"
if printf '%s' "$input" | grep -q '"stop_hook_active"[[:space:]]*:[[:space:]]*true'; then
  exit 0
fi

# Najdi kořen git repozitáře podle aktuálního adresáře session.
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -z "$repo_root" ] && exit 0
cd "$repo_root" || exit 0

# Žádné změny → nic nedělej.
if [ -z "$(git status --porcelain 2>/dev/null)" ]; then
  exit 0
fi

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
ts="$(date '+%Y-%m-%d %H:%M:%S')"

git add -A
git commit -q \
  -m "auto-commit: ${ts}" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" 2>/dev/null

pushed="nepushnuto (chybí remote/upstream)"
if git remote get-url origin >/dev/null 2>&1; then
  if git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
    git push -q 2>/dev/null && pushed="pushnuto → origin/${branch}"
  else
    git push -q -u origin "${branch}" 2>/dev/null && pushed="pushnuto → origin/${branch} (upstream nastaven)"
  fi
fi

short="$(git rev-parse --short HEAD 2>/dev/null)"
printf '{"systemMessage": "🔄 Auto-commit %s — %s"}' "${short}" "${pushed}"
exit 0
