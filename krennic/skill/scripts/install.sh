#!/bin/sh
# Krennic installer for Linux (systemd --user) and macOS (launchd LaunchAgent).
# Idempotent: safe to re-run. Installs the binary to ~/.local/bin and registers
# a per-user background service (least privilege — runs as the logged-in user).
set -eu

# Resolve the physical scripts dir first (pwd -P follows symlinks), THEN go up —
# so this works even when the skill dir is symlinked into ~/.claude/skills/krennic.
SCRIPTS_DIR="$(cd "$(dirname "$0")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/../.." && pwd -P)"
OS="$(uname -s)"
ARCH="$(uname -m)"
BINDIR="$HOME/.local/bin"
BIN="$BINDIR/krennic"

echo "== Krennic install ($OS/$ARCH) =="
mkdir -p "$BINDIR"

# 1. Locate or build the binary.
case "$OS" in
  Linux) GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  *) echo "Nepodporovaný OS: $OS (použij install.ps1 na Windows)"; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) GOARCH="" ;;
esac

PREBUILT="$REPO_ROOT/dist/krennic-${GOOS}-${GOARCH}"
if [ -x "$PREBUILT" ]; then
  echo "Používám předsestavenou binárku: $PREBUILT"
  cp "$PREBUILT" "$BIN"
elif command -v go >/dev/null 2>&1; then
  echo "Sestavuji ze zdrojů..."
  ( cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/krennic )
else
  echo "Nenalezena binárka ani Go toolchain. Sestav 'make build' na build stroji a zkopíruj dist/."
  exit 1
fi
chmod +x "$BIN"
echo "Nainstalováno: $BIN"

# 2. Register the service.
if [ "$OS" = "Linux" ]; then
  UNIT_DIR="$HOME/.config/systemd/user"
  mkdir -p "$UNIT_DIR"
  sed "s#__BIN__#$BIN#g" "$REPO_ROOT/skill/service-templates/krennic.service.tmpl" > "$UNIT_DIR/krennic.service"
  systemctl --user daemon-reload
  systemctl --user enable --now krennic.service
  # Keep the service running without an active login session.
  loginctl enable-linger "$USER" 2>/dev/null || true
  echo "Služba systemd --user 'krennic' spuštěna. Log: journalctl --user -u krennic -f"
elif [ "$OS" = "Darwin" ]; then
  PLIST="$HOME/Library/LaunchAgents/com.acme.krennic.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  sed -e "s#__BIN__#$BIN#g" -e "s#__HOME__#$HOME#g" "$REPO_ROOT/skill/service-templates/com.acme.krennic.plist.tmpl" > "$PLIST"
  launchctl bootout "gui/$(id -u)/com.acme.krennic" 2>/dev/null || true
  launchctl bootstrap "gui/$(id -u)" "$PLIST"
  echo "LaunchAgent 'com.acme.krennic' spuštěn. Log: ~/Library/Logs/krennic.log"
fi

echo ""
echo "Dále:"
echo "  $BIN init-config          # vytvoř config a uprav watch_roots"
echo "  $BIN keys set anthropic   # nastav API klíč (nebo použij provider=claude-cli)"
echo "  $BIN doctor               # ověř prostředí"
echo "  Dashboard: http://127.0.0.1:7373"
