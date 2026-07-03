# Prace s repozitarem

`main` je stabilni integracni vetev. Bezna prace ma jit pres kratke feature/fix
vetve a Pull Request.

## Doporuceny postup

1. Vytvor vetev z aktualniho `main`.
2. Udelej malou, ucelenou zmenu.
3. Pred odeslanim spust kontroly v `krennic`:

```bash
cd krennic
make test
make vet
make build
```

4. Pushni vetev a otevri Pull Request proti `main`.
5. Slucuj az po zelenem CI a review.

## Pravidla

- Netlacit bezne zmeny primo do `main`.
- Drzet vetve kratke a pravidelne je aktualizovat z `main`.
- Nerozsirovat PR o nesouvisejici refaktoringy.
- Kdyz merge/rebase narazi na konflikt, vyresit ho ve feature vetvi pred mergem.
