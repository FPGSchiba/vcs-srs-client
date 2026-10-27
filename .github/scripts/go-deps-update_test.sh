#!/usr/bin/env bash
# Offline test for go-deps-update.sh --list. Portable: shared verbatim between
# vcs-srs-server and vcs-srs-client.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/go-deps-update.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export GOTOOLCHAIN=local
fail=0

check() { # name, got, want
  if [ "$2" = "$3" ]; then
    echo "PASS $1"
  else
    printf 'FAIL %s\n--- got ---\n%s\n--- want ---\n%s\n' "$1" "$2" "$3"; fail=1
  fi
}

mkdir -p "$tmp/full" "$tmp/empty"
cat > "$tmp/full/go.mod" <<'MOD'
module example.com/fixture

go 1.27.0

require (
	example.com/direct/a v1.0.0
	example.com/direct/b v1.0.0
	example.com/excluded v1.0.0
	example.com/excluded/v2 v2.0.0
)

require example.com/indirect v1.0.0 // indirect
MOD
printf 'module example.com/empty\n\ngo 1.27.0\n' > "$tmp/empty/go.mod"

got="$(cd "$tmp/full" && GO_DEPS_EXCLUDE="example.com/excluded" bash "$script" --list)"
check exclude-exact "$got" "$(printf 'example.com/direct/a\nexample.com/direct/b\nexample.com/excluded/v2')"

got="$(cd "$tmp/full" && GO_DEPS_EXCLUDE="" bash "$script" --list)"
check no-exclude "$got" "$(printf 'example.com/direct/a\nexample.com/direct/b\nexample.com/excluded\nexample.com/excluded/v2')"

set +e
got="$(cd "$tmp/empty" && GO_DEPS_EXCLUDE="" bash "$script" --list)"
rc=$?
set -e
check no-requires "$got|$rc" "|0"

exit "$fail"
