# Krennic

Lokální AI code-review agent pro tým vývojářů. Na PC každého vývojáře běží démon,
který **zachytí každou změnu souboru**, po debounce okně sestaví hunk-level diff,
publikuje **stínový Git ref** (`refs/ai/<user>/<repo>/<branch>`) — bez dotčení
lidské branche, indexu či stashe — a nechá změnu přečíst AI (Claude / Gemini /
lokální `claude` CLI) ve dvou stupních: levný **triage** → hlubší **review**.

## Proč stínové refy a ne push na feature branch
Pushovat každé uložení do běžné branche by zaneslo historii, zvýšilo konflikty a
spouštělo CI. Krennic místo toho publikuje pracovní stav do odděleného namespace
`refs/ai/**` přes `git commit-tree` z izolovaného temp indexu — pracovní stav se
přenese věrně (včetně untracked souborů), ale git stav vývojáře zůstane netknutý.
Viz [`docs/git-transport.md`](docs/git-transport.md).

## Rychlý start
```bash
make build                       # nebo: go build -o dist/krennic ./cmd/krennic
./dist/krennic init-config      # vytvoř config, uprav watch_roots
./dist/krennic keys set anthropic   # API klíč (nebo v configu provider="claude-cli")
./dist/krennic doctor           # ověř git, keychain, providery, repozitáře
./dist/krennic run              # spusť démona (dashboard http://127.0.0.1:7373)
```
Změň soubor v hlídaném repu → `./dist/krennic recent` ukáže analýzu.

## Instalace jako služba (per-OS)
```bash
skill/scripts/install.sh         # Linux systemd --user / macOS launchd
skill/scripts/install.ps1        # Windows SCM
```
> Šablony služby (`skill/service-templates/`) mají v `PATH` i `~/.local/bin`, aby
> služba na pozadí našla lokální `claude` CLI — jinak by hlásila „no AI providers".

## CLI
| Příkaz | Popis |
|---|---|
| `run` | spustí démona |
| `status` / `recent` / `show <id>` | stav, poslední změny, detail |
| `pause 1h` / `resume` | dočasně vypnout |
| `keys set/list/del` | tajemství v OS keychainu |
| `doctor` | diagnostika prostředí |
| `gc --days 30` | úklid záznamů a stínových refů |
| `init-config` | výchozí config |

## Klíčové vlastnosti
- **Multi-provider AI** za jedním rozhraním: Anthropic Messages API, Gemini API,
  lokální Claude Code CLI (headless). Zaměnitelné bez zásahu do zbytku systému.
- **Dvoustupňové routování** — `trivial` se potlačí, eskaluje se jen relevantní
  (security/logic/test-gap nebo velké změny). Denní **budget gate**.
- **Redakce** — secrets (`.env*`, `*.pem`, …) nikdy neopustí stroj (deny-list +
  maskování tokenů), transparentně v `redacted_paths`.
- **Durabilita** — SQLite fronta přežije reboot; přerušená práce se dokončí.
- **Observabilita** — `/metrics` (Prometheus), dashboard, logy s `trace_id`.
- **Bezpečnost** — oddělené identity (shadow-write, status, AI), tajemství jen v
  OS keychainu.

## Vývoj
```bash
make test        # unit + e2e testy (vč. důkazu, že snapshot nemění git stav)
make vet
make dist        # cross-compile pro linux/darwin/windows × amd64/arm64
```

## Skill
Instalaci a provoz řídí Claude Code Skill v [`skill/SKILL.md`](skill/SKILL.md).
Zkopíruj/symlinkni složku `skill/` do `~/.claude/skills/krennic/` pro použití
jako `/krennic`.

## Automatická git synchronizace (tento repo)
Doplňkové Claude Code hooky (`.claude/hooks/`) po každé dokončené práci provedou
`commit → pull --rebase → push` — nahrají tvoje změny a stáhnou kolegovy, v pořadí
bezpečném proti konfliktům; do commitu přidají diffstat. Navíc **před prací**
upozorní, když kolega mezitím pushnul, a **před pushem** spustí `go build` (rozbitý
kód se kolegovi nepošle). Není součást krennicu (ten změny jen hodnotí); aktivace je
osobní přes `.claude/settings.local.json`. Detaily v [`NAVOD.md`](NAVOD.md) → *Část C*.

## Dokumentace
- [`docs/architecture.md`](docs/architecture.md) — komponenty a datový tok
- [`docs/git-transport.md`](docs/git-transport.md) — stínové snapshoty
- [`docs/data-contract.md`](docs/data-contract.md) — schémata a routing
- [`skill/reference/data-handling-policy.md`](skill/reference/data-handling-policy.md) — co opouští stroj
