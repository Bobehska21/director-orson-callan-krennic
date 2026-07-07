#!/usr/bin/env bash
# UserPromptSubmit hook for AI coding agents.
# Before every human request, fetch the upstream branch. If the working tree is
# clean, fast-forward automatically so the agent starts from the newest GitHub
# state. If local work exists, do not modify it; return context for the agent.
set -u

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -z "$repo_root" ] && exit 0
cd "$repo_root" || exit 0

# Need a remote and an upstream branch.
git remote get-url origin >/dev/null 2>&1 || exit 0
git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1 || exit 0

git fetch --quiet 2>/dev/null || exit 0

behind="$(git rev-list --count HEAD..@{u} 2>/dev/null || echo 0)"
[ "${behind:-0}" -eq 0 ] && exit 0

files="$(git diff --stat HEAD..@{u} 2>/dev/null)"
branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"

if [ -z "$(git status --porcelain 2>/dev/null)" ]; then
  if git merge --ff-only '@{u}' --quiet 2>/dev/null; then
    action="Working tree was clean, so the hook fast-forwarded to the latest upstream commit."
  else
    action="Fast-forward failed. The agent must inspect git status before editing."
  fi
else
  action="Local changes exist, so the hook did not modify the tree. The agent must avoid conflicting files and the stop hook will rebase after committing."
fi

ctx="Upstream has ${behind} new commit(s) on branch ${branch}. Changed files:
${files}
${action}
Before editing, account for these upstream changes. Do not overwrite local user work."

ctx_json="$(printf '%s' "$ctx" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null)"
[ -z "$ctx_json" ] && ctx_json='"Upstream changed before this task."'

printf '{"systemMessage": "Upstream has %s new commit(s); repository sync was checked before work.", "hookSpecificOutput": {"hookEventName": "UserPromptSubmit", "additionalContext": %s}}' \
  "${behind}" "${ctx_json}"
exit 0
