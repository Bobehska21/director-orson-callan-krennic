#!/bin/sh
# Convenience wrapper: runs `krennic doctor` with the default binary location.
set -eu
BIN="${KRENNIC_BIN:-$HOME/.local/bin/krennic}"
if [ ! -x "$BIN" ]; then
  BIN="$(command -v krennic || true)"
fi
if [ -z "$BIN" ]; then
  echo "krennic binárka nenalezena — spusť nejdřív install.sh"
  exit 1
fi
exec "$BIN" doctor "$@"
