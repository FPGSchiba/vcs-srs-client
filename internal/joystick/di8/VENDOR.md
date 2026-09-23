# Vendored: github.com/gonutz/di8

Source: https://github.com/gonutz/di8 (MIT, see LICENSE)
Vendored at: 2026-09-18

Vendored rather than depended on because it is a single-author package with
no other known users, and it sits on the critical input path. What it wraps --
DirectInput8 -- has been frozen since 2005, so a thin syscall wrapper cannot
rot; owning the code outright removes the supply-chain risk without taking on
maintenance risk.

Local changes:
- Files without an OS suffix renamed to `*_windows.go` so the package cannot
  break the macOS and Linux builds.
- `go.mod` and `readme.md` not vendored.

Do not edit to add features. If upstream fixes something, re-vendor and note
it here.
