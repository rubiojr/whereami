#!/bin/sh
# Keep Qt's private include path local to opt-in RHI builds. Changing global
# CGO_CXXFLAGS would unnecessarily invalidate every MIQT package in GOCACHE.
set -eu
version=$(pkg-config --modversion Qt6Gui)
headers=$(pkg-config --variable=includedir Qt6Gui)
rhi_include=${QT_RHI_INCLUDE:-"$headers/QtGui/$version/QtGui"}
if [ ! -f "$rhi_include/rhi/qrhi.h" ]; then
    printf 'Qt %s RHI headers missing at %s; install the matching private-devel SDK or set QT_RHI_INCLUDE\n' "$version" "$rhi_include" >&2
    exit 1
fi
export CPLUS_INCLUDE_PATH="$rhi_include${CPLUS_INCLUDE_PATH:+:$CPLUS_INCLUDE_PATH}"
exec "$@"
