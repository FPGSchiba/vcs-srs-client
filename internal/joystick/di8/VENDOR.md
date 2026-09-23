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
- `device_windows.go`: `deviceVtbl` completed through `IDirectInputDevice8`'s
  real vtable index 25, and a `Poll()` method added. Upstream's `deviceVtbl`
  stopped at `Initialize` (index 17) -- it never bound `CreateEffect`,
  `EnumEffects`, `GetEffectInfo`, `GetForceFeedbackState`,
  `SendForceFeedbackCommand`, `EnumCreatedEffectObjects`, `Escape` (indices
  18-24, unused by this package but structurally required as filler so later
  offsets land correctly) or `Poll` (index 25). This is not a new feature
  grafted onto di8: `Poll` is part of the same `IDirectInputDevice8` COM
  interface the package already binds, just an incomplete binding of it --
  upstream evidently never needed polling-model devices. Per MSDN, a
  polling-model device that is never polled silently never returns fresh
  data from `GetDeviceState`, which without this fix would look like "buttons
  never register" on hardware, not a build or acquire failure. Order was
  verified against Wine's `dinput.h`, not recalled from memory; every uintptr
  field between `Initialize` and `Poll` must stay present, in this order, or
  `Poll` calls into the wrong vtable slot.

**If upstream re-vendoring is ever done, this vtable completion and the
`Poll()` method must be reapplied by hand** -- a fresh copy of
`device_windows.go` from upstream will silently drop `Poll` again, and the
loss will not show up as a build error.

Do not edit to add features that are not already part of the DirectInput8
interface this package binds. Completing an existing, partially-bound COM
interface (as above) is not the same thing as adding new surface, and is in
scope; anything upstream doesn't touch at all stays out. If upstream fixes
something, re-vendor and note it here -- and re-check this vtable completion
still applies.
