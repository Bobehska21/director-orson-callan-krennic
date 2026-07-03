# Architektura

Krennic je local-first, event-driven agent běžící na PC každého vývojáře.

```
[FS Watcher] → [Debouncer] → [Change Builder: diff + redakce + snapshot]
   → [Durable Queue (SQLite)] ─┬→ [Shadow Publisher: update-ref + push]
                               └→ [AI Gateway] → [Triage] →(eskalace)→ [Deep Review]
   → [Result Store] → [CLI / Dashboard]  → [Status Publisher (opt-in)]
[Telemetry: metrics + strukturované logy s trace_id]
```

## Tři oddělené roviny událostí
1. **Edit-time** — `internal/watcher` (fsnotify). Reaguje na každé uložení; toto
   Git hook neumí.
2. **Git-time** — stínové refy publikované do `refs/ai/**` (viz `git-transport.md`).
3. **AI analýza** — `internal/ai`, dvoustupňová (triage → review).

Roviny jsou oddělené durable frontou, takže selhání jedné (např. push) neblokuje
druhou (analýza běží i bez pushe a naopak).

## Balíčky (`internal/`)
| Balíček | Odpovědnost |
|---|---|
| `model` | interní datový kontrakt (ChangeEvent, TriageResult, ReviewResult) |
| `config` | TOML config + validace; jen názvy klíčů, ne tajemství |
| `secrets` | OS keychain wrapper (go-keyring) |
| `redact` | deny-globy cest + maskování secret tokenů |
| `gitxport` | git plumbing: stav repo, diff, stínový snapshot + push |
| `change` | sestavení ChangeEventu (diff + redakce + snapshot + content hash) |
| `store` | SQLite: durable fronta + result store + dedup + cost accounting |
| `ai` | Provider interface + Gateway (routing) + adaptéry anthropic/gemini/claude-cli |
| `watcher` | fsnotify watcher + auto-discovery repozitářů |
| `debounce` | trailing debounce s max-wait stropem |
| `status` | publikace commit statusů (GitHub/GitLab) |
| `telemetry` | metrics registry (Prometheus text) + slog |
| `agent` | orchestrace: wiring, workeři, dashboard/control HTTP |

## Odolnost a provoz
- **Durabilita:** fronta v SQLite; `ResetInflight` po startu vrátí přerušenou
  práci do `pending`.
- **Dedup:** content-hash okno (formatter revert = no-op).
- **Backpressure:** bounded worker pool (`ai_workers`), token-cost budget gate.
- **Pause:** globální pauza přes CLI/HTTP (`/api/pause`).
- **Observabilita:** `/metrics` (Prometheus), dashboard na `127.0.0.1`, každý
  krok logován s `change_id`/`trace_id`.

## Poznámka k volbě lehké telemetrie
`internal/telemetry` používá vlastní lehký metrics registry + `slog` místo plného
OpenTelemetry SDK, aby binárka zůstala bez těžkých závislostí a fungovala i bez
collectoru. Snapshot API je navržené tak, aby šel OTLP exporter přidat proti
`Snapshot()` bez zásahu do call-site (viz `otlp_endpoint` v configu).
