# Git transport — stínové snapshoty

Cíl: zachytit **working tree včetně untracked souborů** do pushnutelného refu
**bez dotčení branche, indexu, working tree a stashe** vývojáře a bez checkoutu.

## Univerzální cesta (funguje na všech verzích Git, i 2.39)
`internal/gitxport.CreateShadowSnapshot` používá izolovaný temp index:

```
TMPIDX=$(mktemp -d)/index
GIT_INDEX_FILE=$TMPIDX git add -A            # naplní PRÁZDNÝ temp index celým stromem
# → denied cesty se z temp indexu odeberou (git rm --cached), secrets nikdy nevstoupí
TREE=$(GIT_INDEX_FILE=$TMPIDX git write-tree)
# no-op, pokud TREE == HEAD^{tree}
SNAP=$(git commit-tree "$TREE" -p HEAD -m "krennic wip …")   # commit-tree nepotřebuje index
git update-ref refs/ai/<user>/<repo>/<branch> "$SNAP"
git push --force ai-remote refs/ai/…:refs/ai/…               # se shadow SSH identitou
```

Klíčové: prázdný `GIT_INDEX_FILE` znamená, že `write-tree` reprezentuje celý
pracovní strom, ale `$GIT_DIR/index` vývojáře se **nikdy** nezmění. Ověřeno
testem `TestSnapshotNeverMutatesGitState` (status/HEAD/index/stash beze změny).

## Volitelná optimalizace (Git ≥ 2.51)
Kde je k dispozici, lze místo commit-tree použít `git stash create` +
`git stash export --to-ref`. Design detekuje `git version` a drží obě cesty za
`GitTransport` rozhraním. **Nezávisí** na 2.51 — univerzální cesta je default.

## Pojmenování a úklid
- Ref: `refs/ai/<user>/<repo>/<branch>` (`/` v branchi → `-`).
- Force-push přepisuje jeden ref na branch → žádná akumulace.
- `krennic gc` maže refy branchí, které už lokálně neexistují.

## Identity a bezpečnost
- Dedikovaný remote `ai-remote` + shadow SSH klíč (`ssh_key_path`) nebo
  fine-grained token — nekoliduje s osobními git credentials.
- Redakce se aplikuje PŘED zápisem do temp indexu i před diffem.
- Server by měl vyloučit `refs/ai/**` z CI a z výpisu branchí.
