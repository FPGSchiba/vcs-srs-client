#!/usr/bin/env bash
# Checks the Linux GUI deps are usable: the pkg-config modules Wails' cgo
# links against, and (XVFB=true) the binaries xvfb-run needs.
set -euo pipefail
pkg-config --exists --print-errors gtk4 webkitgtk-6.0 libsoup-3.0 gio-unix-2.0 x11
pkg-config --modversion gtk4 webkitgtk-6.0 libsoup-3.0
if [ "${XVFB:-false}" = "true" ]; then
  for bin in Xvfb xvfb-run xauth; do
    command -v "$bin" >/dev/null || { echo "::error::$bin not found"; exit 1; }
  done
fi
