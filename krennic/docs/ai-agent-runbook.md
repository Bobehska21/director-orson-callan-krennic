# Krennic runbook for AI coding tools

This file is for Codex, Claude Code, Cursor, Cline, Windsurf, and similar AI
coding tools. Follow it when installing, repairing, testing, or changing Krennic
on a developer PC.

## Goal

Krennic must run continuously in the background on each developer PC. It should:

1. watch the developer's project roots,
2. create safe shadow snapshots under `refs/ai/**`,
3. run AI triage/review,
4. show results locally,
5. optionally report team hub events,
6. optionally publish GitHub status `krennic/ai-review` for PR merge gates.

Do not stop after a build succeeds. A correct setup is proven only by a running
service and a real saved-file test.

## Universal agent workflow

Use this workflow in every Git project, regardless of language, framework, or
folder layout:

1. Before acting on a human request, run `git fetch` for the current upstream.
2. If the worktree is clean and the branch can fast-forward, update it before
   editing.
3. If local changes already exist, do not overwrite them. Read `git status` and
   the upstream diff, then work around the existing changes.
4. Make the requested change.
5. Let Krennic observe saved files in the watched repository. If Krennic should
   be active but is not running, fix that before considering the task complete.
6. Run the validation commands that the project exposes. Prefer `make test`,
   `make vet`, `make build` when present; otherwise use detected package-manager
   commands such as `npm run test`, `npm run lint`, `npm run build`, `go test
   ./...`, `go vet ./...`, `go build ./...`, `cargo test`, `cargo build`, or
   `pytest`.
7. When a small subtask is complete, run `krennic done --message "<summary>"`.
8. Let Krennic create the short branch, validate, push, open the PR, and enable
   auto-merge through GitHub branch protection.
9. Do not hand-roll commits, pushes, or direct merges to `main` unless the user
   explicitly disables team sync.

Never claim a task is complete if local changes were left uncommitted,
unvalidated, or unpushed without saying so explicitly.

## Team sync

Krennic's `[team_sync]` mode replaces repository hook scripts. The daemon
periodically fetches the configured main branch, reports pending updates in
`krennic status` and the dashboard, and never mutates a dirty working tree.

Use:

```bash
krennic done --message "short summary"
krennic sync
```

`krennic done` is the explicit "I am finished" signal. It creates a short branch,
commits local changes, runs detected validation commands, pushes the branch,
opens a PR, and enables GitHub auto-merge. `krennic sync` only fast-forwards the
configured main branch when the current tree is clean.

The repository also includes short entrypoint files for common agents:
`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.cursorrules`, `.clinerules`,
`.windsurfrules`, and `.github/copilot-instructions.md`. They all point back to
this runbook so each tool follows the same workflow.

## First checks

Run these before making assumptions:

```bash
pwd
git status --short --branch
command -v krennic || true
krennic status || true
krennic doctor || true
```

If `krennic` is not found, use `~/.local/bin/krennic` when present:

```bash
~/.local/bin/krennic status || true
~/.local/bin/krennic doctor || true
```

On macOS, service logs are usually:

```bash
tail -n 200 ~/Library/Logs/krennic.log
launchctl print gui/$(id -u)/com.acme.krennic
```

On Linux:

```bash
systemctl --user status krennic
journalctl --user -u krennic -n 200 --no-pager
```

On Windows PowerShell:

```powershell
sc.exe query Krennic
krennic status
krennic doctor
```

## Install or reinstall

From the repository root:

```bash
cd krennic
make build
skill/scripts/install.sh
```

On Windows PowerShell:

```powershell
cd krennic
go build -o dist\krennic.exe .\cmd\krennic
.\skill\scripts\install.ps1
```

The installer copies the binary to `~/.local/bin/krennic` on macOS/Linux and
registers a per-user service. Re-run it after changing service templates or when
the background service cannot find `claude`.

## Config

Create config if missing:

```bash
krennic init-config
```

Typical config paths:

- macOS: `~/Library/Application Support/Krennic/config.toml`
- Linux: `~/.config/krennic/config.toml`
- Windows: `%APPDATA%\Krennic\config.toml`

Minimum expected settings:

```toml
[agent]
watch_roots = ["~/code"]
dashboard_addr = "127.0.0.1:7373"
head_poll_ms = 5000

[redaction]
deny = [".env*", "*.pem", "*.key", "id_rsa*", "secrets/**"]
scan_regex = true
```

For GitHub merge gates:

```toml
[status]
enabled  = true
provider = "github"
identity = "status-token"

[issues]
enabled  = true
provider = "github"
identity = "status-token"

[team_sync]
enabled           = true
main_branch       = "main"
fetch_interval_ms = 300000
branch_prefix     = "krennic/done"
provider          = "github"
identity          = "status-token"
auto_merge        = true
```

After editing config, restart the service.

If a watched repository's human `origin` is not the remote that should receive
Krennic shadow refs, configure an explicit per-repository `remote_url`. This is
required whenever the daemon would otherwise reuse `origin` and shadow push would
fail or go to the wrong place:

```toml
[[repos]]
path = "~/path/to/repository"
enabled = true
remote_url = "git@github.com:owner/repository.git"
```

Per-repo `remote_url` wins over `[git_transport].remote_url`; when both are
empty, Krennic falls back to the repository's `origin`.

## Secrets

Never write secrets into config files. Use keychain-backed identities:

```bash
krennic keys set anthropic
krennic keys set gemini
krennic keys set claude-oauth-token
krennic keys set status-token
krennic keys set hub-token
```

Use only the identities needed by the config. `status-token` needs permission to
write commit statuses. If issues are enabled, it must also write issues.

For `provider = "claude-cli"`, prefer a durable background token:

```bash
claude setup-token
krennic keys set claude-oauth-token
```

Then restart the service and run `krennic doctor`. A terminal-authenticated
Claude CLI is not enough if the launchd/systemd/Windows service cannot access
the same auth context.

## Restart commands

macOS:

```bash
launchctl kickstart -k gui/$(id -u)/com.acme.krennic
```

Linux:

```bash
systemctl --user restart krennic
```

Windows PowerShell:

```powershell
Restart-Service Krennic
```

If restart fails after binary or service template changes, rerun the installer.

## Verification checklist

Run after install, config change, provider change, token change, or service
repair:

```bash
krennic doctor
krennic status
```

Expected:

- `krennic doctor` does not report missing required provider/config pieces.
- `krennic status` prints `Stav: RUNNING`.
- `Repozitáře` is non-zero when `watch_roots` should contain repositories.
- The target repository appears in the repo list.
- Dashboard opens at `http://127.0.0.1:7373`.

Then do a real file-save test in a watched repository:

```bash
printf "\n# krennic smoke test %s\n" "$(date -u +%Y%m%dT%H%M%SZ)" >> KRENNIC_SMOKE_TEST.tmp
sleep 8
krennic recent
```

Confirm a new record appears. Remove the temporary file afterwards in the normal
working tree flow for that repository.

For GitHub merge gates, confirm the status is on the latest PR head SHA:

```bash
git rev-parse HEAD
gh pr checks --watch
```

The required context is `krennic/ai-review`. A status on an older commit or only
on `refs/ai/**` does not satisfy branch protection.

## Common failures

### `agent neběží?`

The CLI cannot reach the dashboard server. Check the service:

```bash
launchctl print gui/$(id -u)/com.acme.krennic
systemctl --user status krennic
sc.exe query Krennic
```

Then check logs and restart. Also verify `dashboard_addr` in config matches what
the CLI is using.

### No repositories watched

Run:

```bash
krennic doctor
krennic status
```

Fix `watch_roots` so it points to parent directories containing Git repositories,
not to unrelated folders. Restart after editing config.

### `provider not configured` or `no AI providers`

Either set an API key:

```bash
krennic keys set anthropic
```

or configure both `[ai.triage]` and `[ai.review]` to use:

```toml
provider = "claude-cli"
```

For background service use, also set `claude-oauth-token` and make sure the
service PATH can find `claude`.

### PR waits forever for `krennic/ai-review`

Check all of these:

- Krennic is running on the author's PC.
- The PR repository is under `watch_roots`.
- `[status] enabled = true`.
- `status-token` exists in keychain.
- The token can write commit statuses to this repository.
- `head_poll_ms` is enabled so new commits are rechecked.
- The status was published to the latest PR head SHA.

### `krennic done` refuses to run

Common causes:

- The working tree is clean; there is nothing to finish.
- The current branch is not based on the latest `origin/main`; finish or save
  work, sync/rebase manually, then retry.
- Validation failed; inspect the log path printed by the command.
- `team_sync.identity` token cannot create PRs or enable auto-merge.

### Shadow push uses the wrong remote

Krennic resolves remotes in this order:

1. `[[repos]].remote_url` for the matching repo path,
2. `[git_transport].remote_url`,
3. the repository's `origin`.

If any repository needs a different push target, add a `[[repos]]` block with
that repo's absolute or `~`-expanded `path` and explicit `remote_url`, then
restart the service. Do not change the human `origin` remote just to satisfy
Krennic.

### Manual run works but service does not

This usually means environment mismatch. Services may have a smaller PATH and no
interactive shell auth. Reinstall service templates, set `claude-oauth-token`,
restart the service, and inspect logs.

## Change safety

- Keep redaction defaults.
- Keep tokens in keychain only.
- Keep shadow transport under `refs/ai/**`.
- Avoid CI triggers for `refs/ai/**`.
- Before code changes, fetch upstream and fast-forward when the tree is clean.
- After code changes, run the strongest validation commands the project exposes.
  For this repository, run:

```bash
cd krennic
make test
make vet
make build
```

If one of these cannot run on the current PC, say exactly which command failed
and why.
