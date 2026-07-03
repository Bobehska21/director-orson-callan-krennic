# Krennic — politika nakládání s daty

## Co opouští vývojářský stroj
1. **Diffy do AI:** pouze hunk-level diffy změněných souborů (s omezeným
   kontextem), plus stručná metadata (repo, branch, počty řádků, jazyk). Nikdy
   ne celý repozitář.
2. **Stav pracovního stromu do Gitu:** jako commit v namespace `refs/ai/**`
   (stínové refy), oddělený od `refs/heads/**`. Publikuje se pod technickou
   identitou (shadow-write), ne pod osobní git identitou vývojáře.
3. **Commit statusy (opt-in):** pouze pass/fail/pending + krátký popis, přes
   identitu se scope `repo:status` (bez přístupu ke zdrojovému kódu).

## Redakce (co NIKDY neopustí stroj)
Cesty na deny-listu jsou vyloučené z diffu **i** ze stínového snapshotu:
```
.env*   *.pem   *.key   id_rsa*   secrets/**
```
Navíc `scan_regex = true` maskuje secret-like tokeny (AWS klíče, JWT, PEM
hlavičky, `api_key=…`, GitHub PAT, Slack tokeny) na zbývajících řádcích.
Vyloučené cesty se transparentně vypíšou v `redacted_paths` u každé změny.

## Tři oddělené identity (princip minimálních oprávnění)
| Identita | Účel | Scope |
|---|---|---|
| `git-shadow` | push stínových refů | pouze push do `refs/ai/**` (deploy key / fine-grained PAT) |
| `status-token` | publikace commit statusů | `repo:status` (GitHub) / status API (GitLab) |
| `anthropic` / `gemini` | AI inference | API klíč providera |

Všechna tajemství jsou **jen v OS keychainu** (macOS Keychain / Windows
Credential Manager+DPAPI / Linux Secret Service) pod službou `com.acme.krennic`.
Nikdy nejsou v configu ani na disku v plaintextu.

## Doporučení pro server (admin)
- Vyluč `refs/ai/**` ze spouštění CI a z výpisu branchí.
- Chraň `main`/release branche přes protected branches / rulesets; agent do nich
  nikdy nepushuje.
- Zvaž požadavek na signed commits pro rozlišení „člověk vs. agent".

## Retence
- Lokálně: záznamy a dedup se promazávají `krennic gc --days N` (default 30).
- Stínové refy: force-push přepisuje jeden ref na branch (neakumuluje);
  `gc` maže refy zaniklých branchí.
- U cloudových providerů se řiď jejich retenční politikou; pro citlivá repa zvaž
  enterprise/ZDR režim nebo `provider = "claude-cli"` (subscription).
