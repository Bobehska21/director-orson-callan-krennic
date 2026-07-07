---
name: krennic
description: Instaluje, nastaví a spravuje Krennic — lokálního AI code-review agenta, který na každém vývojářském PC zachytí každou změnu, publikuje ji jako stínový Git ref a nechá ji přečíst AI (Claude/Gemini). Použij, když uživatel chce nainstalovat/nakonfigurovat/spustit/diagnostikovat Krennic nebo měnit jeho politiku (repozitáře, redakce, providery, budget, publikaci statusů).
allowed-tools: Bash, Read
---

# Krennic — lokální AI code-review agent

Krennic je démon běžící na PC každého vývojáře. Sleduje změny souborů, po
krátkém debounce okně sestaví hunk-level diff, publikuje **stínový Git ref**
(`refs/ai/<user>/<repo>/<branch>`) — nikdy nezasahuje do lidské branche, indexu
ani stashe — a spustí dvoustupňovou AI analýzu (levný triage → hlubší review).
Výsledky jsou v lokálním CLI a dashboardu; volitelně se publikují jako commit
status.

## Governance — co opouští stroj (řekni to uživateli PŘED instalací)
- Do cloudové AI jdou **jen hunk-level diffy** změněných souborů, ne celý repo.
- Cesty na deny-listu (`.env*`, `*.pem`, `*.key`, `id_rsa*`, `secrets/**`) jsou
  vyloučené z diffu **i** ze stínového snapshotu; navíc se maskují secret-like tokeny.
- Stav pracovního stromu se publikuje do namespace `refs/ai/**` odděleného od
  `refs/heads/**`. Doporuč adminovi vyloučit `refs/ai/**` z CI a z výpisu branchí.
- Tajemství (API klíče, git shadow cred, status token) jsou **jen v OS keychainu**.

## Instalace (proveď v tomto pořadí)

1. **Ověř/sestav binárku.** Pokud existuje předsestavená binárka pro danou
   platformu v `dist/`, použij ji; jinak sestav ze zdrojů:
   ```bash
   cd krennic && make build        # nebo: go build -o dist/krennic ./cmd/krennic
   ```
   Detekuj OS/arch: `uname -sm` (Unix) / `$env:OS` (Windows).

2. **Spusť instalační skript** pro danou platformu — nakopíruje binárku a
   zaregistruje službu (systemd --user / launchd / Windows SCM):
   ```bash
   # Linux/macOS
   ${CLAUDE_SKILL_DIR}/scripts/install.sh
   # Windows (PowerShell)
   ${CLAUDE_SKILL_DIR}/scripts/install.ps1
   ```

3. **Vytvoř konfiguraci** (pokud ještě není):
   ```bash
   krennic init-config
   ```
   Pak uprav `watch_roots` na kořeny s repozitáři vývojáře. Šablona je i v
   `${CLAUDE_SKILL_DIR}/config.example.toml`.

4. **Nastav tajemství** (čte se ze stdin, nikdy z argv, ukládá do keychainu):
   ```bash
   krennic keys set anthropic       # Claude API klíč
   krennic keys set gemini          # (volitelné) Gemini API klíč
   krennic keys set git-shadow      # (volitelné) cred/heslo pro shadow push
   krennic keys set status-token    # (volitelné) token se scope repo:status
   ```
   Alternativa bez API klíče: nastav v configu `provider = "claude-cli"` a
   Krennic použije lokální `claude` CLI (subscription auth).

5. **Ověř prostředí a spusť:**
   ```bash
   krennic doctor        # git, keychain, providers, repozitáře
   krennic run           # ruční spuštění; služba běží na pozadí po instalaci
   ```
   Dashboard: `http://127.0.0.1:7373`.

## Běžná správa
- `krennic status` — stav, fronta, náklady dnes.
- `krennic sync` — stáhne pending `main`, jen když je worktree čistý.
- `krennic done --message "..."` — bezpečně dokončí podúlohu přes větev, validaci, PR a auto-merge.
- `krennic recent` — poslední analýzy.
- `krennic show <change_id>` — detail nálezů.
- `krennic pause 1h` / `krennic resume` — dočasně vypnout.
- `krennic gc --days 30` — úklid starých záznamů a stínových refů zaniklých branchí.
- `krennic keys list` — které klíče jsou nastavené.

## Týmový hub (centrální audit „kdo co kde změnil")
Volitelná centrální vrstva. Na jednom firemním stroji běží sběrné místo, kam
každý agent hlásí atributované změny; audit je append-only a hash-řetězený
(tamper-evident).

Nastavení hubu (jednou):
```bash
krennic keys set hub-token     # společný token pro celý tým
krennic hub                    # server na :8787, dashboard http://<stroj>:8787
```
Napojení agenta — v jeho configu v sekci `[hub]`:
```toml
url            = "http://<stroj-s-hubem>:8787"
token_identity = "hub-token"
```
a na jeho stroji `krennic keys set hub-token` (stejný token). Doručení je durable
(outbox), takže se žádné hlášení neztratí ani při výpadku hubu.

Kontrola z terminálu: `krennic team` (přehled), `krennic team --user X`,
`krennic audit verify` (ověří, že s auditem nikdo nemanipuloval). Na hub jde jen
popis změny (kdo/kde/soubory/verdikt), nikdy zdrojový kód ani tajemství.

## Změna politiky
Uprav config (`krennic init-config` ukáže cestu; typicky
`~/.config/krennic/config.toml`, na macOS `~/Library/Application Support/Krennic/`):
- `[ai.triage]` / `[ai.review]` — provider (`anthropic|gemini|claude-cli`) a model.
- `[ai.routing]` — kdy eskalovat na hlubší review.
- `[budget] daily_usd` — denní strop; po překročení jen triage.
- `[redaction] deny` — vyloučené cesty.
- `[status] enabled` — opt-in publikace commit statusů.
- `[team_sync] enabled` — periodický fetch hlavní větve a `krennic done`
  workflow pro krátkou větev, validaci, PR a auto-merge.
Po změně restartuj službu (viz `scripts/install.sh` výstup) nebo `krennic run`.

## Reference
- `${CLAUDE_SKILL_DIR}/reference/data-handling-policy.md` — co a kam odchází, identity, tokeny.
- `${CLAUDE_SKILL_DIR}/reference/troubleshooting.md` — časté problémy.
- `docs/architecture.md`, `docs/git-transport.md`, `docs/data-contract.md` — návrh.

## Odinstalace
```bash
${CLAUDE_SKILL_DIR}/scripts/uninstall.sh     # nebo uninstall.ps1
```
Smaže službu i binárku; keychain tajemství smaž ručně přes `krennic keys del <name>`.
