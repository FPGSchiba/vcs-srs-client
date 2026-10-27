#!/usr/bin/env bash
# Offline test for assert-go-toolchain.sh. Portable: shared verbatim between
# vcs-srs-server and vcs-srs-client.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
script="$here/assert-go-toolchain.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
# Fixtures must not make the go command switch toolchains while we read GOVERSION.
export GOTOOLCHAIN=local
have="$(go env GOVERSION)"
fail=0

run_case() { # name, go.mod body, want-exit (0|nonzero), want-substring
  local name="$1" body="$2" want="$3" sub="$4" out rc
  mkdir -p "$tmp/$name"
  printf '%s\n' "$body" > "$tmp/$name/go.mod"
  set +e
  out="$(bash "$script" "$tmp/$name/go.mod" 2>&1)"
  rc=$?
  set -e
  if [ "$want" = 0 ] && [ "$rc" -ne 0 ]; then
    echo "FAIL $name: want exit 0, got $rc: $out"; fail=1; return
  fi
  if [ "$want" != 0 ] && [ "$rc" -eq 0 ]; then
    echo "FAIL $name: want non-zero exit, got 0: $out"; fail=1; return
  fi
  if [ -n "$sub" ] && [[ "$out" != *"$sub"* ]]; then
    echo "FAIL $name: output lacks '$sub': $out"; fail=1; return
  fi
  echo "PASS $name"
}

run_case no-directive $'module example.com/fixture\n\ngo 1.21' nonzero "No toolchain directive"
run_case mismatch $'module example.com/fixture\n\ngo 1.21\n\ntoolchain go1.0.1' nonzero "want go1.0.1, got $have"
run_case match "$(printf 'module example.com/fixture\n\ngo 1.21\n\ntoolchain %s' "$have")" 0 ""

exit "$fail"
