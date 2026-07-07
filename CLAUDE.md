# Krennic AI Agent Rules

Read `AGENTS.md` first, then `krennic/docs/ai-agent-runbook.md`.

Before every coding task, check `krennic status`. If `[team_sync]` reports a
pending update and the worktree is clean, run `krennic sync`; if local changes
exist, preserve them and finish or reconcile them deliberately. Keep Krennic
running as a background service. After a small completed subtask, run
`krennic done --message "<summary>"`. It validates, creates a short branch,
pushes it, opens a PR, and enables auto-merge under the repository policy.
