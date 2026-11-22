#!/usr/bin/env bash
set -euo pipefail

BIN="${1:-$HOME/.local/bin/lerd-desktop}"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
APPS_DIR="$DATA_HOME/applications"
ICON_DIR="$DATA_HOME/icons/hicolor/512x512/apps"
ICON_SRC="$(cd "$(dirname "$0")/../src-tauri/icons" && pwd)/icon.png"

mkdir -p "$APPS_DIR" "$ICON_DIR"
cp "$ICON_SRC" "$ICON_DIR/sh.lerd.Desktop.png"

cat > "$APPS_DIR/sh.lerd.Desktop.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=Lerd
GenericName=Local PHP development environment
Comment=Native desktop app for Lerd
Exec=$BIN %u
Icon=sh.lerd.Desktop
Terminal=false
Categories=Development;
Keywords=php;laravel;podman;lerd;
MimeType=x-scheme-handler/lerd;
StartupWMClass=lerd-desktop
StartupNotify=true
EOF

if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APPS_DIR" >/dev/null 2>&1 || true
fi
if command -v xdg-mime >/dev/null 2>&1; then
  xdg-mime default sh.lerd.Desktop.desktop x-scheme-handler/lerd >/dev/null 2>&1 || true
fi
# Disable the standalone lerd-tray daemon so the desktop app's own tray is the
# only Lerd icon in the system tray.
if command -v lerd >/dev/null 2>&1; then
  lerd tray off >/dev/null 2>&1 || true
elif [ -x "$HOME/.local/bin/lerd" ]; then
  "$HOME/.local/bin/lerd" tray off >/dev/null 2>&1 || true
fi
echo "Registered $APPS_DIR/sh.lerd.Desktop.desktop (lerd:// handler)"
