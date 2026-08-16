#!/usr/bin/env bash
# Installs CerclBackup CLI + GUI for the current user.
#
# Run from the extracted release tarball directory:
#   ./install.sh
set -euo pipefail

BIN_DIR="$HOME/.local/bin"
ICON_DIR="$HOME/.local/share/icons/hicolor/1024x1024/apps"
DESKTOP_DIR="$HOME/.local/share/applications"

SRC_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$BIN_DIR" "$ICON_DIR" "$DESKTOP_DIR"
install -m 755 "$SRC_DIR/cerclbackup" "$BIN_DIR/cerclbackup"
install -m 755 "$SRC_DIR/cerclbackup-gui" "$BIN_DIR/cerclbackup-gui"
install -m 644 "$SRC_DIR/cerclbackup.png" "$ICON_DIR/cerclbackup.png"
install -m 644 "$SRC_DIR/cerclbackup.desktop" "$DESKTOP_DIR/cerclbackup.desktop"

update-desktop-database "$DESKTOP_DIR" 2>/dev/null || true
gtk-update-icon-cache "$HOME/.local/share/icons/hicolor" 2>/dev/null || true

echo "Installed to $BIN_DIR (add it to PATH if it isn't already)."
echo "CerclBackup should now appear in your application launcher."
