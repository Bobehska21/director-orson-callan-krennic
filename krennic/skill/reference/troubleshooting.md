# Krennic — troubleshooting

## `krennic status` hlásí, že agent neběží
- Zkontroluj službu: Linux `systemctl --user status krennic`, macOS
  `launchctl print gui/$(id -u)/com.acme.krennic`, Windows `sc.exe query Krennic`.
- Ruční spuštění s logem: `krennic run --debug`.
- Ověř `dashboard_addr` v configu; CLI se připojuje na stejnou adresu.

## Žádné změny se neanalyzují
- `krennic doctor` → jsou nalezené repozitáře? Uprav `watch_roots`.
- Watcher ignoruje `.git`, `node_modules`, `dist`, `build`, … a redigované cesty.
- Debounce: po uložení počkej `debounce_ms` (default 800 ms).
- Jednořádkové/whitespace změny mohou být triage vyhodnoceny jako `trivial`
  (bez eskalace) — to je záměr (kontrola šumu).

## AI selhává: „provider … not configured"
- Nastav klíč: `krennic keys set anthropic` (nebo `gemini`), případně přepni
  na `provider = "claude-cli"` a ověř `claude` na PATH.
- `krennic keys list` ukáže, co je nastavené.

## Shadow push selhává
- `krennic doctor` ukáže ai-remote URL. Prázdné = použije se origin repozitáře;
  pokud origin není, push se přeskočí (analýza běží dál).
- Pokud konkrétní repo nesmí používat lidský `origin` pro Krennic shadow push,
  nastav mu v configu vlastní blok `[[repos]]` s `path` a `remote_url`. Per-repo
  hodnota má přednost před globálním `[git_transport].remote_url`.
- Ověř přístup shadow identity: `GIT_SSH_COMMAND="ssh -i <klíč>" git ls-remote <url>`.
- Server musí povolit push do `refs/ai/**` pro shadow identitu.

## Vysoké náklady / rate limity
- `[budget] daily_usd` omezí denní útratu (po překročení jen triage).
- Zpřísni eskalaci: zvyš `escalate_line_threshold`, zúž `escalate_categories`.
- Použij levnější triage model / `provider = "claude-cli"`.

## Reset stavu
- Fronta a výsledky jsou v SQLite vedle configu (`krennic.db`). Zastav službu,
  smaž soubor pro čistý start (ztratíš historii, ne kód).
