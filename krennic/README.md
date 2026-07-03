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

## Týmový merge model: Krennic místo ruční kontroly
`main` je chráněná stabilní větev. Vývojáři do ní neposílají změny přímo:
každá změna jde přes krátkou feature/fix větev a Pull Request. Ruční approval
není povinný; místo něj musí projít automatické kontroly:

- `test` — `make test`
- `vet` — `make vet`
- `build` — `make build`
- `krennic/ai-review` — verdikt Krennicu publikovaný jako GitHub commit status

GitHub branch protection je nastavený tak, že PR do `main` nejde sloučit, dokud
všechny tyto kontroly nejsou zelené. Krennic tedy není jen lokální rádce; v tomhle
repu je součást merge brány.

### Co musí dělat Krennic na každém PC
Na počítači každého vývojáře musí běžet Krennic démon se zapnutým publikováním
statusů:

```toml
[status]
enabled  = true
provider = "github"
identity = "status-token"
```

Token `status-token` se ukládá do OS keychainu:

```bash
krennic keys set status-token
```

Token musí mít oprávnění zapisovat commit statusy do tohoto repozitáře
(`repo:status` u classic PAT, případně fine-grained token s přístupem ke commit
statuses). Krennic po analýze změny zavolá GitHub Statuses API a nastaví kontext
`krennic/ai-review` na commit, který PR obsahuje. Pro branch protection je
důležité, aby tento status existoval na **posledním commitu PR větve**; status na
starším commitu nebo jen na stínovém refu `refs/ai/**` merge neodemkne.

Výsledek statusu:

- `success` — změna je podle Krennicu bezpečná pro merge, případně jen drobná.
- `failure` — hlubší review našlo problém s verdiktem `request-changes`; PR se
  nesloučí, dokud autor neopraví kód a Krennic nepublikuje nový zelený status.
- chybějící status — Krennic neběží, nesleduje dané repo, nemá zapnutý status
  publishing, nemá správný `status-token`, nebo ještě neposlal výsledek pro
  poslední commit PR; PR zůstane zablokované.

### Automatické GitHub issues pro chyby
Krennic umí při blokujícím AI review založit GitHub issue. Zapíná se samostatně:

```toml
[issues]
enabled  = true
provider = "github"
identity = "status-token"
```

Použitý token musí mít kromě commit statusů také právo zapisovat issues. Když
review skončí verdiktem `request-changes`, Krennic vytvoří issue se shrnutím,
nálezy, odkazem na commit/change ID a labely:

- `krennic`
- `bug`
- `backend`, pokud změněné soubory vypadají jako server/backend
- `frontend`, pokud změněné soubory vypadají jako UI/frontend
- `area-unknown`, když oblast nejde spolehlivě určit

Klasifikace je heuristická: bere v úvahu přípony souborů (`.go`, `.py`, `.sql`
pro backend; `.tsx`, `.jsx`, `.vue`, `.css`, `.html` pro frontend), jazyky a
cesty jako `internal/`, `cmd/`, `api/`, `web/`, `ui/`, `client/`. Když změna
zasáhne obě části, issue dostane oba labely.

Issue obsahuje skrytý Krennic marker s repo/branch identitou. Díky tomu Krennic
nevytváří duplicitní issues pro stejnou větev: při dalším `request-changes`
existující issue aktualizuje, a jakmile další review na stejné větvi skončí
verdiktem `pass` nebo `comment`, otevřenou issue automaticky zavře.

### Běžný tok práce
1. Vývojář si vytvoří větev z aktuálního `main`.
2. Pracuje normálně lokálně; Krennic průběžně sleduje uložené změny.
3. Krennic po debounce okně vytvoří stínový snapshot, spustí AI triage/review a
   publikuje `krennic/ai-review` na GitHub pro aktuální commit.
4. Pokud review vrátí `request-changes`, Krennic založí nebo aktualizuje GitHub
   issue.
5. Vývojář otevře Pull Request.
6. GitHub Actions spustí `test`, `vet` a `build`.
7. PR se může sloučit až po zeleném CI a zeleném `krennic/ai-review`.

Pokud PR po pushi čeká na `krennic/ai-review`, nejdřív ověř, že Krennic na PC
autora běží (`krennic status`), že daný repozitář patří do `watch_roots`, a že je
zapnutý `[status] enabled = true`. Stav musí být publikovaný pro nejnovější SHA
větve v PR.

Tento model má dvě důležité vlastnosti: `main` se nerozbije přímým pushem a nikdo
nemusí ručně kontrolovat každý PR. Pokud automatická AI kontrola neproběhne, merge
se zastaví místo toho, aby nekontrolovaná změna prošla do stabilní větve.

## Rychlý start
```bash
make build                       # nebo: go build -o dist/krennic ./cmd/krennic
./dist/krennic init-config      # vytvoř config, uprav watch_roots
./dist/krennic keys set anthropic   # API klíč (nebo v configu provider="claude-cli")
./dist/krennic keys set status-token # GitHub token pro kontext krennic/ai-review
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
- **Merge gate** — GitHub status `krennic/ai-review` může být povinný check pro
  PR, takže merge do `main` projde jen po automatické kontrole Krennicem.
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
