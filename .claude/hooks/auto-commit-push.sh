#!/usr/bin/env bash
# Stop hook: po dokončení práce automaticky nahraje svoje změny a stáhne kolegovy.
# Pořadí: commit → pull --rebase (vezmi kolegovy nové změny) → push (nahraj svoje).
# Rebase = moje commity se přehrají nad kolegovými → nikdy to nespadne a nic nepřepíše.
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

# 1) Commitni svoje změny. Do těla commitu přidej seznam změněných souborů +
#    diffstat, aby další vývojář / Claude viděl, čeho přesně se změna týkala
#    (a předešlo se kolizím – ví, čeho se nedotýkat).
git add -A
changed="$(git diff --cached --stat 2>/dev/null)"
git commit -q \
  -m "auto-commit: ${ts}" \
  -m "Změněné soubory:
${changed}" \
  -m "Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>" 2>/dev/null

# 2) + 3) Synchronizace s remote: nejdřív stáhni kolegovy změny (rebase), pak nahraj svoje.
sync="jen lokálně (chybí remote)"
if git remote get-url origin >/dev/null 2>&1; then
  if git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
    if git pull --rebase --quiet 2>/dev/null; then
      if git push -q 2>/dev/null; then
        sync="sync OK → origin/${branch} (stáhnuto + nahráno)"
      else
        sync="⚠️ stáhnuto, ale push selhal"
      fi
    else
      git rebase --abort 2>/dev/null || true
      sync="⚠️ konflikt při stahování — nic nepřepsáno, vyřeš ručně (git status)"
    fi
  else
    git push -q -u origin "${branch}" 2>/dev/null \
      && sync="nahráno → origin/${branch} (upstream nastaven)"
  fi
fi

short="$(git rev-parse --short HEAD 2>/dev/null)"
printf '{"systemMessage": "🔄 Auto-sync %s — %s"}' "${short}" "${sync}"
exit 0
