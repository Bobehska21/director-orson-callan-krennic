#!/usr/bin/env bash
# UserPromptSubmit hook: před začátkem práce tiše fetchne a upozorní, když kolega
# mezitím pushnul nové commity — ať Claude i ty vidíte, čeho se (ne)dotýkat,
# a předejde se kolizím. Čistý strom → bezpečně fast-forward stáhne; jinak jen varuje.
set -u

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -z "$repo_root" ] && exit 0
cd "$repo_root" || exit 0

# Musí existovat remote + upstream.
git remote get-url origin >/dev/null 2>&1 || exit 0
git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1 || exit 0

# Tiše zjisti stav remote.
git fetch --quiet 2>/dev/null || exit 0

behind="$(git rev-list --count HEAD..@{u} 2>/dev/null || echo 0)"
[ "${behind:-0}" -eq 0 ] && exit 0   # kolega nic nového nepushnul → ticho

files="$(git diff --stat HEAD..@{u} 2>/dev/null)"
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"

if [ -z "$(git status --porcelain 2>/dev/null)" ]; then
  if git merge --ff-only '@{u}' --quiet 2>/dev/null; then
    action="Pracovní strom byl čistý → automaticky staženo (fast-forward)."
  else
    action="Nešlo fast-forwardnout — stáhni ručně (git pull)."
  fi
else
  action="Máš rozdělané změny → NEstahuju automaticky (po dokončení práce to srovná rebase v Stop hooku). Pozor na soubory výše."
fi

ctx="POZOR – kolega mezitím pushnul ${behind} commit(ů) do větve ${branch}. Změněné soubory:
${files}
${action}
Zvaž, jestli se tvůj úkol netýká stejných souborů, ať nevznikne kolize."

# systemMessage = uživateli; additionalContext = vloží se Claudovi do kontextu.
ctx_json="$(printf '%s' "$ctx" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null)"
[ -z "$ctx_json" ] && ctx_json='"kolega pushnul nové změny"'

printf '{"systemMessage": "⚠️ Kolega pushnul %s commit(ů) — viz změněné soubory.", "hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": %s}}' \
  "${behind}" "${ctx_json}"
exit 0
