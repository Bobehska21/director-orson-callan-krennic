# Týmový hub — centrální audit

Hub je volitelná centrální vrstva, která z jednotlivých lokálních agentů dělá
jeden týmový přehled: **kdo / co / kde / kdy** změnil a jak to AI vyhodnotila.

## Datový tok
```
Agent (na PC vývojáře)                     Hub (jeden firemní stroj)
  analýza hotová → BuildReport               POST /api/report  → append-only audit
     → uložit do outboxu (SQLite)               (hash-řetězeno, idempotentní)
     → outboxSender doručuje s retry   ──►    dashboard + /api/feed + /api/verify
```

## Report (co se posílá)
Atributovaný záznam, ne kód: `report_id` (= change_id, idempotence),
`developer` (git jméno + e-mail, OS uživatel, stroj), repo, branch, head_sha,
shadow_ref, seznam **souborů**, počty řádků, jazyky, `redacted_paths`, a výsledek
AI (relevance, kategorie, verdikt, počet nálezů). Definice: `internal/audit`.

## Nezfalšovatelnost (hash chain)
Každý záznam má `entry_hash = sha256(prev_hash ‖ payload)` a `prev_hash`
předchozího. Jakákoli změna obsahu nebo smazání/přeuspořádání záznamu rozbije
navazování — `krennic audit verify` (nebo `GET /api/verify`) najde první
narušený `seq`. Ověřeno testy `TestVerifyDetectsTampering` a
`TestVerifyDetectsDeletion`.

## Doručení bez ztráty (outbox)
Report se nejdřív uloží do lokálního **outboxu** (SQLite, `internal/store`).
Sender ho doručuje na hub s opakováním; smaže až po úspěchu. Výpadek hubu tedy
nezpůsobí ztrátu auditního záznamu — doručí se po obnově.

## Bezpečnost
- Sdílený **bearer token** (keychain `hub-token`, nebo `KRENNIC_HUB_TOKEN`, nebo
  `--token`). Bez tokenu hub varuje a přijímá kohokoli (jen pro test).
- Hub přijímá jen popis změny — **žádný zdrojový kód ani tajemství** (redakce
  probíhá už na agentovi před odesláním).
- Server-side `received_at` je nezávislý na čase agenta.

## Provoz
```bash
krennic hub --addr :8787 --db /var/lib/krennic/hub.db   # server
krennic team [--user X] [--repo Y]                       # přehled z CLI
krennic audit verify                                     # kontrola integrity
```
Dashboard: `http://<stroj>:8787` — živá tabulka celého týmu + stav řetězce.
