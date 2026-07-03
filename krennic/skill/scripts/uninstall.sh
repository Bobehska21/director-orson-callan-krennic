#!/bin/sh
# Removes the Krennic service and binary. Keychain secrets are left intact —
# remove them explicitly with `krennic keys del <name>`.
set -eu

OS="$(uname -s)"
BIN="$HOME/.local/bin/krennic"

echo "== Krennic uninstall ($OS) =="
if [ "$OS" = "Linux" ]; then
  systemctl --user disable --now krennic.service 2>/dev/null || true
  rm -f "$HOME/.config/systemd/user/krennic.service"
  systemctl --user daemon-reload 2>/dev/null || true
elif [ "$OS" = "Darwin" ]; then
  launchctl bootout "gui/$(id -u)/com.acme.krennic" 2>/dev/null || true
  rm -f "$HOME/Library/LaunchAgents/com.acme.krennic.plist"
fi
rm -f "$BIN"
echo "Odinstalováno. Tajemství v keychainu zůstala (smaž přes 'krennic keys del')."
