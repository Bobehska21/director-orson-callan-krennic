# Datový kontrakt

Jeden interní kontrakt (`internal/model`) sdílejí všichni producenti i všechny
provider adaptéry — proto jsou providery zaměnitelné.

## ChangeEvent (produkuje `change.Builder`)
```json
{
  "schema_version": "1.0",
  "change_id": "uuid",
  "trace_id": "uuid",
  "created_at": "RFC3339",
  "developer": { "user_slug": "alice", "git_name": "Alice", "machine": "alice-mbp" },
  "repo": {
    "name": "payments-svc", "branch": "feature/x", "head_sha": "…",
    "shadow_ref": "refs/ai/alice/payments-svc/feature-x", "shadow_sha": "…",
    "remote": "git@github.com:acme/payments-svc.git",
    "local_path": "/Users/alice/code/payments-svc"
  },
  "summary": {
    "files_changed": 3, "lines_added": 47, "lines_removed": 12,
    "languages": ["go","sql"], "redacted_paths": [".env.local"], "truncated": false
  },
  "diff": {
    "format": "unified", "context_lines": 3,
    "hunks": [ { "path": "…", "language": "go", "change_type": "modified",
                 "function_context": "func …", "patch": "@@ … @@\n…" } ]
  },
  "routing_hints": { "force_stage": "", "budget_tier": "normal" },
  "content_hash": "sha256(…)"
}
```
`local_path` je lokální bookkeeping — nikdy se nevkládá do AI promptu; renderer
promptu (`ai/prompts.go`) posílá jen vybraná pole.

## TriageResult (stage 1)
```json
{ "relevance": "trivial|minor|notable|risky", "categories": ["security","logic",…],
  "escalate": true, "reason": "…", "confidence": 0.82,
  "provider": "anthropic", "model": "…", "tokens": {"in":1200,"out":90}, "cost_usd": 0.0007 }
```

## Eskalace do stage 2 (deterministicky, `ai/ai.go`)
Eskaluj právě když:
- `routing_hints.force_stage == "review"`, NEBO
- `relevance ∈ {notable, risky}`, NEBO `escalate == true`, NEBO
- kategorie ∈ `escalate_categories`, NEBO
- `lines_added+lines_removed > escalate_line_threshold` a změna není pure-style.

`relevance == trivial` se vždy potlačí (hlavní kontrola šumu a nákladů).

## ReviewResult (stage 2)
```json
{ "verdict": "pass|comment|request-changes", "summary": "…",
  "findings": [ { "path":"…","line":44,"severity":"high","type":"security",
                  "message":"…","suggestion":"…","confidence":0.78 } ],
  "provider":"…","model":"…","tokens":{"in":8400,"out":620},"cost_usd":0.041 }
```

## Record (perzistováno, zobrazeno v UI)
`{ event, triage?, review?, status: pending|analyzing|done|failed, error?, updated_at }`
