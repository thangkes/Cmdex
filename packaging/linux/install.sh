#!/bin/sh
# Installs Cmdex for the current user: the program in ~/.local/bin (where
# self-update can replace it) and a menu entry. No root needed.
set -e
here=$(cd "$(dirname "$0")" && pwd)
bin="$HOME/.local/bin"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/512x512/apps"
mkdir -p "$bin" "$apps" "$icons"
install -m 755 "$here/cmdex" "$bin/cmdex"
install -m 644 "$here/cmdex.png" "$icons/cmdex.png"
sed "s|^Exec=.*|Exec=$bin/cmdex|" "$here/cmdex.desktop" > "$apps/cmdex.desktop"
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" || true
echo "Cmdex installed: $bin/cmdex"
echo "It needs the WebKitGTK 4.1 runtime (Debian/Ubuntu: sudo apt install libwebkit2gtk-4.1-0)."
