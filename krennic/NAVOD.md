# Krennic – uživatelský návod

Krennic hlídá kód. Běží potichu na pozadí na počítači každého vývojáře a pokaždé,
když někdo uloží soubor, nechá umělou inteligenci (Claude / Gemini) přečíst tu
změnu a říct, jestli je v pořádku. Volitelně se všechny změny sbíhají na jedno
místo (hub), kde je přehledně vidět **kdo, co, kde a kdy** změnil.

Návod má tři části:
- **Část A** – nastavení na počítači vývojáře (dělá se u každého).
- **Část B** – týmový přehled na jednom místě (dělá se jednou pro celou firmu).
- **Část C** – automatické sloučení přes GitHub bez ruční kontroly.

---

# Část A · Na počítači vývojáře

## Co to dělá (v kostce)

Když vývojář uloží soubor, Krennic:
1. **si sám všimne změny** (hned, nikdo nic nespouští),
2. **pošle jen tu změněnou část kódu** umělé inteligenci,
3. AI **řekne, jestli je změna OK**, nebo kde je chyba/riziko,
4. výsledek **ukáže v přehledu** na tom počítači.

Nic neblokuje ani nezdržuje – jen radí. Hesla a tajné soubory (`.env`, klíče,
certifikáty) se **nikdy neodešlou**.

## Co potřebuješ

- `git` (na počítači vývojáře obvykle už je),
- přístup k AI – **buď** klíč od Claude nebo Gemini, **nebo** nainstalovaný
  nástroj `claude` (pak žádný klíč netřeba).

## Nejrychlejší cesta: přes Claude Code (doporučeno)

Aby Claude Code na daném počítači věděl, co „krennic" je, **zaregistruj skill** –
jednou, jedním příkazem (počítač už musí mít složku `krennic`):

macOS / Linux:
```
krennic/skill/scripts/register-skill.sh
```
Windows (PowerShell):
```
krennic\skill\scripts\register-skill.ps1
```

Tím Claude Code pozná `/krennic`. Potom stačí ve Visual Studiu otevřít terminál,
spustit Claude Code a říct:

> „nainstaluj a spusť krennic"

Claude sám sestaví program, nainstaluje ho na pozadí a nastaví – jen se zeptá na
**klíč k AI** a **složku s projekty**. Kdo chce instalovat ručně bez Clauda,
pokračuje sekcemi níže.

## Instalace (jednou, pár minut)

V terminálu ve složce `krennic`:

```
make build
```
Vytvoří program `dist/krennic`.

```
skill/scripts/install.sh
```
Nastaví, aby se Krennic spouštěl sám na pozadí (na Windows: `install.ps1`).
Od teď běží pořád, i po restartu počítače.

## První nastavení (4 kroky)

**1) Vytvoř soubor s nastavením:**
```
krennic init-config
```
Příkaz **vypíše přesnou cestu** k souboru `config.toml`. Bývá tady:
- macOS: `~/Library/Application Support/Krennic/config.toml`
- Linux: `~/.config/krennic/config.toml`
- Windows: `%APPDATA%\Krennic\config.toml`

Otevři ho a uprav řádek `watch_roots` – ať míří na složku s projekty vývojáře:
```
watch_roots = ["~/projekty"]
```

**2) Zadej přístup k AI** (jedna z možností):
- Máš klíč od Claude? Ulož ho bezpečně (nikde se nezobrazí):
  ```
  krennic keys set anthropic
  ```
  (Gemini obdobně: `krennic keys set gemini`.)
- Nemáš klíč, ale máš nástroj `claude`? V `config.toml` v sekcích
  `[ai.triage]` a `[ai.review]` změň `provider` na `"claude-cli"`. Žádný klíč pak
  netřeba.

**3) Ověř, že je vše připravené:**
```
krennic doctor
```
Vypíše seznam s ✓ (v pořádku) / ✗ (chybí). Když jsou důležité věci ✓, hotovo.

**4) Zapni GitHub status pro automatické PR kontroly:**
V `config.toml` nastav:
```
[status]
enabled  = true
provider = "github"
identity = "status-token"

[issues]
enabled  = true
provider = "github"
identity = "status-token"
```
Potom ulož GitHub token:
```
krennic keys set status-token
```
Token musí umět zapisovat commit statusy a issues do repozitáře. Díky tomu
Krennic po kontrole pošle na GitHub výsledek `krennic/ai-review` pro aktuální
commit. Když najde blokující chybu, založí GitHub issue.

> Po každé změně `config.toml` restartuj službu, nebo prostě spusť `krennic run`.

## Přehled na počítači

Otevři v prohlížeči:
```
http://127.0.0.1:7373
```
Uvidíš živě všechny změny, jak je AI hodnotí, kolik to stálo a kolik čeká fronta.

## Rychlý test, že to opravdu funguje

1. `krennic status` → má vypsat **Stav: RUNNING**.
2. V hlídané složce (z `watch_roots`) **něco změň a ulož** – třeba přidej řádek
   do libovolného souboru.
3. Za pár sekund `krennic recent` → objeví se **nová změna s hodnocením**
   (triage: `trivial` / `minor` / `notable` / `risky`, u vážnějších i verdikt).
4. Otevři `http://127.0.0.1:7373` – ta změna je i v tabulce.

Když se změna objeví, **funguje to**.

Kontrola bezpečnosti (nepovinné): přidej do složky soubor `.env` s nějakým
„heslem". V detailu (`krennic show <ID>`) musí být vidět, že byl **vynechán
(redacted)** – heslo se nikam neodešle.

## Každodenní příkazy

Vývojář **nedělá nic navíc** – jen programuje. Z terminálu se hodí:

| Chci… | Napíšu |
|---|---|
| Stav (běží? fronta? útrata dnes) | `krennic status` |
| Poslední hodnocené změny | `krennic recent` |
| Detail jedné změny (ID vezmeš z `recent`) | `krennic show <ID>` |
| Na chvíli vypnout (např. porada) | `krennic pause 1h` (jde i `30m`, `2h`) |
| Zase zapnout | `krennic resume` |
| Uklidit staré záznamy (výchozí 30 dní) | `krennic gc` |

## Kolik to stojí a jak to hlídat

Krennic šetří automaticky:
- Nejdřív se změnu podívá **levná rychlá AI**. Drobnost = konec, nic dalšího.
- **Dražší důkladná AI** se pustí, jen když je změna vážná (bezpečnost, logika,
  chybějící testy, nebo velká změna).
- V `config.toml` nastavíš **denní strop** `daily_usd` (např. `5.0`). Po jeho
  vyčerpání jede zbytek dne jen ta levná část.

Útratu vidíš v přehledu i přes `krennic status`.

## Soukromí – co opouští počítač

**Odchází k AI:** jen změněné řádky kódu + pár údajů (projekt, počet řádků).

**Nikdy neodchází:** celý projekt, soubory s hesly/tajemstvími (`.env`, `*.pem`,
`*.key`, `secrets/` apod.) a tajné hodnoty v kódu (Krennic je začerní). Co bylo
vynecháno, uvidíš u dané změny jako „redacted".

## Když něco nefunguje

- **„Agent neběží"** → spusť `krennic run` nebo zkontroluj instalaci.
- **„provider not configured" / „no AI providers"** → nemáš přístup k AI. Udělej
  `krennic keys set anthropic`, nebo přepni `provider` na `"claude-cli"`. Pokud to
  hlásí **jen služba na pozadí** (ruční `krennic run` přitom funguje), jde o PATH –
  služba musí vidět `claude` CLI. Šablony služby (`skill/service-templates/`) už
  mají `~/.local/bin` v PATH; stačí přeinstalovat přes `install.sh`.
- **Nic se nehodnotí** → `krennic doctor` a zkontroluj `watch_roots`.
- **PR nejde sloučit, čeká na `krennic/ai-review`** → na počítači autora změny
  neběží Krennic, nemá zapnutý `[status] enabled = true`, nebo chybí
  `status-token`.
- **Moc utrácí** → sniž `daily_usd` v `config.toml`.

---

# Část B · Týmový přehled (kdo co kde změnil)

Aby ses díval na **jedno místo** a viděl všechny změny všech vývojářů – s jménem,
e-mailem, počítačem, projektem, větví a konkrétními soubory – zapneš **hub**
(centrální sběrné místo).

**Jak to funguje:** každý Krennic po vyhodnocení změny pošle na hub krátké
hlášení (kdo, co, kde, kdy, jak to AI ohodnotila). Hub to ukládá do neměnného
seznamu.
- **Nic se neztratí:** když je hub zrovna nedostupný, Krennic si hlášení schová a
  pošle ho, jakmile hub zase běží.
- **Nedá se falšovat:** hub každý záznam „zapečetí" navázáním na předchozí.
  Kdyby chtěl někdo potají něco přepsat nebo smazat, `krennic audit verify` to
  okamžitě odhalí.
- **Soukromí platí i tady:** na hub jde jen popis změny – **ne kód** a nikdy ne
  hesla/tajné soubory.

## Spuštění hubu (jednou, na jednom firemním počítači/serveru)

```
krennic keys set hub-token       # zadáš společné heslo (token) pro celý tým
krennic hub                      # spustí sběrné místo (port 8787)
```
Přehled celého týmu pak najdeš v prohlížeči na adrese toho počítače, např.
`http://ten-pocitac:8787`. Tento počítač musí být pro ostatní dostupný v síti.

## Napojení každého vývojáře na hub

V jeho `config.toml` doplň sekci `[hub]`:
```
[hub]
url            = "http://ten-pocitac:8787"
token_identity = "hub-token"
```
A na jeho počítači zadej **stejný token**:
```
krennic keys set hub-token
```
Restartuj službu (nebo `krennic run`). Od té chvíle se jeho změny objevují
v týmovém přehledu.

## Příkazy pro tým

| Chci… | Napíšu |
|---|---|
| Přehled: kdo co kde změnil | `krennic team` |
| Jen změny jednoho člověka | `krennic team --user alice` |
| Jen změny v jednom projektu | `krennic team --repo platby` |
| Ověřit, že s auditem nikdo nemanipuloval | `krennic audit verify` |

---

# Část C · Automatické sloučení přes GitHub

Do `main` se neposílá přímo. Každý pracuje ve své větvi a otevře Pull Request.
GitHub ho pustí do `main` jen když projdou automatické kontroly:

- `test`
- `vet`
- `build`
- `krennic/ai-review`

Ruční schvalování není potřeba. Krennic na počítači autora změny musí běžet,
zkontrolovat změnu a poslat výsledek `krennic/ai-review` na GitHub pro poslední
commit v PR. Když Krennic najde vážný problém, PR se nesloučí, dokud se kód
neopraví a Krennic nepošle nový zelený stav. Pokud je zapnuté `[issues]`, zároveň
založí GitHub issue s labely `backend` nebo `frontend` podle změněných souborů.
Jakmile další review na stejné větvi projde, Krennic tu issue automaticky zavře.

Jednoduché pravidlo pro každého vývojáře:

```
1. pracuj ve vlastní větvi
2. otevři Pull Request
3. počkej na zelené automatické kontroly
4. slouč do main
```

---

# Část D · Automatická synchronizace kódu s kolegou (git)

> Pozn.: Tohle **není součást krennicu** – je to doplňkový pracovní postup přes
> Claude Code hooky (`.claude/hooks/` v kořeni repa). Krennic změny jen
> **hodnotí**; přenos kódu dělá git.

Aby si vývojáři nepřepisovali práci a měli pořád aktuální kód, po **každé
dokončené práci** se automaticky provede:

```
1. commit          # nahraje tvoje změny do commitu
2. pull --rebase   # stáhne kolegovy nejnovější změny (tvoje se přehrají navrch)
3. push            # nahraje na GitHub
```

- Pořadí je zvolené tak, aby to **nikdy nespadlo a nic nepřepsalo**.
- Když jsi jen povídal a nic neměnil → **neudělá se nic** (no-op).
- Do zprávy commitu se přidá **seznam změněných souborů + diffstat**, aby další
  vývojář / Claude viděl, čeho přesně se změna týkala (méně kolizí).
- Při skutečném konfliktu ve stejných řádcích → **nic se nepřepíše**, jen se
  zobrazí upozornění a konflikt vyřešíš ručně (`git status`).

Navíc dva ochranné hooky:

- **Před prací** (`pre-work-fetch.sh`, UserPromptSubmit) — před každým příkazem
  tiše ověří remote a když kolega mezitím pushnul, **upozorní** (a když máš čistý
  strom, bezpečně stáhne). Vidíš, čeho se nedotýkat, dřív než začneš.
- **Před pushem** (brána v `auto-commit-push.sh`) — než se kód pošle kolegovi,
  spustí `go build ./...`. Když se **nepřeloží, nepushne** (commitne jen lokálně) —
  ať kolegovi nepřistane rozbitý kód. Opraví se a nahraje při příštím sync.

Aktivace je osobní (v `.claude/settings.local.json`, který je gitignored), takže
se nikomu nevnucuje. Skript je ale ve verzi repa, takže ho má každý po naklonování
k dispozici. Zapnutí / vypnutí / přehled: příkaz `/hooks` v Claude Code.

---

# Odinstalace

```
skill/scripts/uninstall.sh
```
(Na Windows `uninstall.ps1`.) Odstraní program i jeho spouštění na pozadí. Uložené
klíče/tokeny smažeš zvlášť příkazem `krennic keys del <název>`.
