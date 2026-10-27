#!/usr/bin/env bash
# Fails unless the active Go toolchain is exactly the one go.mod's `toolchain`
# directive pins. Portable: shared verbatim between vcs-srs-server and
# vcs-srs-client; it holds no repo-specific value.
#
# Usage: bash .github/scripts/assert-go-toolchain.sh [path/to/go.mod]
set -euo pipefail

mod="${1:-./go.mod}"
# tr -d '\r': jq and go on Windows (Git Bash) emit CRLF.
want="$(go mod edit -json "$mod" | jq -r '.Toolchain // empty' | tr -d '\r')"
if [ -z "$want" ]; then
  echo "::error title=No toolchain directive::$mod has no toolchain directive; pin a patched Go release (e.g. go mod edit -toolchain=go1.27.3)"
  exit 1
fi
have="$(go env GOVERSION | tr -d '\r')"
if [ "$have" != "$want" ]; then
  echo "::error title=Go toolchain mismatch::want $want, got $have"
  exit 1
fi
echo "Go toolchain OK: $have"
