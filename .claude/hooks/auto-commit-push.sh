#!/usr/bin/env bash
# Stop hook for AI coding agents.
# After each completed human request: commit local changes, rebase onto the
# latest upstream branch, run the project's validation gate, and push only if the
# gate passes. No-op when there are no local changes.
set -u

input="$(cat 2>/dev/null || true)"
if printf '%s' "$input" | grep -q '"stop_hook_active"[[:space:]]*:[[:space:]]*true'; then
  exit 0
fi

# Najdi kořen git repozitáře podle aktuálního adresáře session.
repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -z "$repo_root" ] && exit 0
cd "$repo_root" || exit 0

if [ -z "$(git status --porcelain 2>/dev/null)" ]; then
  exit 0
fi

branch="$(git rev-parse --abbrev-ref HEAD 2>/dev/null)"
ts="$(date '+%Y-%m-%d %H:%M:%S')"

git add -A
changed="$(git diff --cached --stat 2>/dev/null)"
if [ -z "$changed" ]; then
  exit 0
fi

git commit -q \
  -m "ai-sync: ${ts}" \
  -m "Changed files:
${changed}" \
  -m "Created by an AI coding agent after completing a human request." 2>/dev/null || exit 0

run_gate() {
  gate_log="$(mktemp 2>/dev/null || printf '/tmp/krennic-ai-gate.log')"
  : >"$gate_log" 2>/dev/null || true

  run_cmd_in() {
    dir="$1"
    label="$2"
    shift 2
    printf '%s\n' "== ${dir}: ${label} ==" >>"$gate_log" 2>/dev/null || true
    ( cd "$dir" && "$@" ) >>"$gate_log" 2>&1
  }

  run_gate_dir() {
    dir="$1"

    # Prefer explicit project Makefile targets when present.
    if [ -f "$dir/Makefile" ] || [ -f "$dir/makefile" ]; then
      targets="$(cd "$dir" && make -qp 2>/dev/null | awk -F: '/^[A-Za-z0-9_.-]+:/ {print $1}' | sort -u)"
      for target in test vet build; do
        if printf '%s\n' "$targets" | grep -qx "$target"; then
          run_cmd_in "$dir" "make ${target}" make "$target" || return 1
        fi
      done
      return 0
    fi

    # Node projects: install must be handled separately; run scripts that exist.
    if [ -f "$dir/package.json" ] && command -v npm >/dev/null 2>&1 && command -v node >/dev/null 2>&1; then
      scripts="$(cd "$dir" && node -e "const p=require('./package.json'); console.log(Object.keys(p.scripts||{}).join('\n'))" 2>/dev/null || true)"
      for script in test lint build; do
        if printf '%s\n' "$scripts" | grep -qx "$script"; then
          run_cmd_in "$dir" "npm run ${script}" npm run "$script" || return 1
        fi
      done
      return 0
    fi

    if [ -f "$dir/go.mod" ] && command -v go >/dev/null 2>&1; then
      run_cmd_in "$dir" "go test ./..." go test ./... || return 1
      run_cmd_in "$dir" "go vet ./..." go vet ./... || return 1
      run_cmd_in "$dir" "go build ./..." go build ./... || return 1
      return 0
    fi

    if [ -f "$dir/Cargo.toml" ] && command -v cargo >/dev/null 2>&1; then
      run_cmd_in "$dir" "cargo test" cargo test || return 1
      run_cmd_in "$dir" "cargo build" cargo build || return 1
      return 0
    fi

    if { [ -f "$dir/pyproject.toml" ] || [ -f "$dir/pytest.ini" ] || [ -d "$dir/tests" ]; } && command -v pytest >/dev/null 2>&1; then
      run_cmd_in "$dir" "pytest" pytest || return 1
      return 0
    fi

    return 0
  }

  project_dirs="$(
    {
      printf '.\n'
      find . -maxdepth 3 \( -name .git -o -name node_modules -o -name dist -o -name build -o -name target \) -prune -o \
        \( -name Makefile -o -name makefile -o -name go.mod -o -name package.json -o -name Cargo.toml -o -name pyproject.toml -o -name pytest.ini \) \
        -print 2>/dev/null | sed 's#/[^/]*$##'
    } | sed 's#^\./##; s#^$#.#' | sort -u
  )"

  ran=0
  while IFS= read -r dir; do
    [ -z "$dir" ] && continue
    [ "$dir" = "." ] || [ -d "$dir" ] || continue
    if [ -f "$dir/Makefile" ] || [ -f "$dir/makefile" ] || [ -f "$dir/package.json" ] || [ -f "$dir/go.mod" ] || [ -f "$dir/Cargo.toml" ] || [ -f "$dir/pyproject.toml" ] || [ -f "$dir/pytest.ini" ] || [ -d "$dir/tests" ]; then
      ran=1
      run_gate_dir "$dir" || return 1
    fi
  done <<EOF
$project_dirs
EOF

  if [ "$ran" -eq 0 ]; then
    printf '%s\n' "== no validation commands detected ==" >>"$gate_log" 2>/dev/null || true
  fi

  return 0
}

sync="jen lokálně (chybí remote)"
if git remote get-url origin >/dev/null 2>&1; then
  if git rev-parse --abbrev-ref --symbolic-full-name '@{u}' >/dev/null 2>&1; then
    if git pull --rebase --quiet 2>/dev/null; then
      if run_gate; then
        if git push -q 2>/dev/null; then
          sync="sync OK: rebased, validated, pushed to origin/${branch}"
        else
          sync="rebased and validated, but push failed"
        fi
      else
        sync="validation failed; not pushed. Inspect git status and ${gate_log:-the validation log}."
      fi
    else
      git rebase --abort 2>/dev/null || true
      sync="rebase conflict; nothing was overwritten. Resolve manually with git status."
    fi
  else
    if run_gate; then
      git push -q -u origin "${branch}" 2>/dev/null \
        && sync="validated and pushed to origin/${branch}; upstream set"
    else
      sync="validation failed; not pushed. Inspect git status and ${gate_log:-the validation log}."
    fi
  fi
fi

short="$(git rev-parse --short HEAD 2>/dev/null)"
printf '{"systemMessage": "Auto-sync %s: %s"}' "${short}" "${sync}"
exit 0
