#!/bin/sh
# Post-removal script for whereami RPM package

# Update desktop database
if command -v update-desktop-database > /dev/null 2>&1; then
  update-desktop-database /usr/share/applications 2>/dev/null || true
fi

# Update icon cache
if command -v gtk-update-icon-cache > /dev/null 2>&1; then
  gtk-update-icon-cache -q /usr/share/icons/hicolor 2>/dev/null || true
fi

exit 0
