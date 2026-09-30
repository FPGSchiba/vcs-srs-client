# Comms radio redesign — variant roster, frequency editing, state language

**Date:** 2026-09-30
**Status:** design approved; implementation plan not yet written
**Branch:** `feat/phase-7-3-local-persistence` (extends it — see §8)
**Supersedes in part:** [`2026-09-30-vcs-client-phase-7-3-local-persistence-design.md`](./2026-09-30-vcs-client-phase-7-3-local-persistence-design.md) §6
**Visual reference:** [`design/vcs/radio-variants.md`](../../../design/vcs/radio-variants.md) — layouts, row breakdowns, state treatments and tokens. Self-contained; read it alongside this spec.

The Comms panel is the most-used surface in the client. After Phase 7.3 shipped
the resizable grid, the panel in practice reads as a settings form rather than
an instrument: the frequency is 14px, the PTT is a 160×48 slab, the name is a
permanent bordered text input, and three layout bugs make the whole thing look
broken.

This document specifies a redesign of the radio card and the panel around it.

---

## 1. What is wrong today

From a screenshot of the running client, plus reading the shipped components.

### 1.1 Three layout bugs

1. **Resize handles float detached** below and to the right of each card, outside
   the block they resize.
2. **Blocks overflow the panel.** `profile.DefaultBlockW` is 516 while the Comms
   popout defaults to 540 wide; minus `GRID_PAD` (12 each side) the inner width is
   516 exactly, so every card sits flush against the clip edge and any border or
   shadow is cut.
3. **The chrome buttons overlap.** SAVE / REVERT / RESET and the close control
   collide with each other and with the "COMMUNICATIONS" title.

### 1.2 The port dropped most of the designed card

`design/vcs/project/lib/radio.jsx` — the prototype this UI is meant to match —
contains a `RadioWidget` with a `size` prop (`lg`/`md`/`sm`) and a separate
`RadioStrip` compact variant. The shipped `RadioCard` is a single md-only card
that lost:

- **Per-digit wheel editing.** The prototype bumps an individual digit
  (`atoms.jsx:133-138`). Our own ported CSS still carries `cursor: ns-resize` and
  a hover glow on `.lcd-digit` (`components.css:1023-1029`) — the styling for the
  interaction is already in the tree, unwired.
- **Size variants.** LCD at 36/26/22px, padding 14/10, PTT 220×64 vs 160×48.
- **The name as a label** that becomes an input on double-click. Ours is a
  permanent bordered `<input>`, which is the single largest reason the card reads
  as a form.
- **Corner brackets and a gradient background** — the tactical framing.
- **A status row**: traffic dot, last talker, VU meter.

Much of this specification is therefore *restoration* rather than invention, which
is also what CLAUDE.md asks for: the real UI is meant to visually match the
prototype.

---

## 2. The variant roster

Four variants, agreed by working through mockups at real pixel size.

| Variant | Size | LCD | PTT | Name | Talker + VU | Chips | MHZ unit |
|---|---|---|---|---|---|---|---|
| **VERTICAL** | 280×166 | 30px | 96×28 | yes | yes | ON · INTERCOM | yes |
| **HORIZONTAL** | 360×110 | 22px | 64×64 | yes | yes | ON | yes |
| **NARROW-H** | 300×68 | 21px | 32×30 | yes | yes | frame only | — |
| **NARROW-V** | 150×124 | 18px | full×26 | yes | yes | frame only | — |

For comparison, today's card is **516×180 with a 14px frequency and a 160×48
PTT**. Every variant is smaller and every frequency is larger.

### 2.1 Why these heights

Each is a fixed-row stack with one spacing value and no leftover space. The
vertical worked out as:

```
padding top      14
header row       20   R01 · name · ON chip
gap               9
LCD              46   30px digits
gap               9
PTT row          28   INTERCOM chip + 96×28 PTT
gap               9
status row       20   divider + dot + talker + VU
padding bottom   12
                ───
total           166
```

An earlier draft was 300×260. The excess was not spacing but **leftover**: the
status row was pinned with `margin-top: auto`, so all unused height collected
into one 39px gap. Removing it — not shrinking any element — gave 166.

The other three:

```
HORIZONTAL 360×110   pad 14 + [hdr 18 · 8 · LCD 34 · 8 · status 16 = 84] + pad 12
                     PTT 64×64 centred beside the column
NARROW-H   300×68    pad 9 + two stacked lines + pad 9; LCD sits left, PTT right
NARROW-V   150×124   pad 10 + [rid+name 14 · 7 · LCD 30 · 7 · talker+VU 12 · 7 · PTT 26] + pad 10
```

`NARROW-V` is 150 wide rather than 130 so "Fleet Common" fits beside `R01`
without truncating. Its radio name is **left-aligned** directly after `R01`, so
the two read as one label.

### 2.2 What the narrow variants drop, and what they do not

They drop the `ON` / `INTERCOM` **chips** and the `MHZ` unit suffix. They do
**not** drop the radio name, the talker name, or the VU meter — all four variants
carry all three.

Intercom is not lost when the chip goes: it is carried by the **frame** (§5.2),
which scales to every size for free.

---

## 3. Component architecture

The explicit requirement is that new variants are cheap to add. So the four
variants are **two layout shells driven by a descriptor registry**, not four
components.

```
frontend/src/windows/comms/
  variants.ts          the registry — one descriptor per variant
  RadioCard.tsx        resolves the descriptor, renders the matching shell
  RadioColumn.tsx      shell for orientation "column"  (VERTICAL, NARROW-V)
  RadioRow.tsx         shell for orientation "row"     (HORIZONTAL, NARROW-H)
  RadioFrame.tsx       border, corner brackets, intercom edge, state treatment
  FreqLcd.tsx          per-digit scroll editing, sized from the descriptor
  TalkerLine.tsx       dot + name + VU as one grouped unit
  PttIndicator.tsx     sized from the descriptor
```

### 3.1 The descriptor

```ts
export interface RadioVariant {
  id: "vertical" | "horizontal" | "narrow-h" | "narrow-v" | string;
  label: string;                    // shown in the resize affordance
  w: number;
  h: number;
  orientation: "column" | "row";
  lcdPx: number;
  ptt: { w: number | "fill"; h: number };
  shows: {
    chips: "full" | "enabled-only" | "none";
    unit: boolean;                  // the "MHZ" suffix
    statusDivider: boolean;         // the border-top above the status row
  };
}
```

The table in §2 *is* this registry. Nothing in the shells hard-codes a size.

### 3.2 Adding a variant

- **A new size of an existing shape** — one descriptor entry. No new component. It
  joins the snap set automatically (§4).
- **A structurally new shape** (a circular dial, a two-column grid) — one new
  shell plus its descriptor. The frame, LCD, talker line and PTT are shared
  primitives that size themselves from the descriptor, so they need no change.

This is the property to protect in review: if adding a size requires touching
anything but `variants.ts`, the abstraction has leaked.

---

## 4. Snap model

Dragging a card's corner **snaps to the nearest descriptor**, by Euclidean
distance in (w, h). Both axes count, so dragging wide-and-short lands on
NARROW-H while wide-and-tall lands on HORIZONTAL. There is no free resize and no
in-between size, which is what stops a variant being stretched into a shape it
was not designed for — the failure that produced the dead air in §2.1.

A short transition animates the size change so the snap reads as intentional.

### 4.1 What the profile stores

`profile.Block` carries **`variantId`** instead of `{w, h}`:

```json
{ "radio_id": 1, "variant": "vertical" }
```

This makes the format self-describing and lets a future variant change its own
dimensions without invalidating saved profiles.

**No migration.** The `{w, h}` block format never left local development, so
there is no compatibility path to write. A block with **no** `variant` field
defaults to `vertical` — a default for a missing field, in the same way
`profile.Reconcile` already appends a default block for a radio that has none.

---

## 5. Interaction and state

### 5.1 Frequency editing

Hovering a digit highlights it; the wheel changes **that decade only**.
`118.5̲00` steps by 0.1 MHz; the last digit by 1 kHz. Carries propagate — a digit
passing 9 wraps and bumps the digit to its left. Click bumps up, shift-click
bumps down, matching the prototype.

**The value stays an integer kHz throughout.** `LcdFreq` already works this way
deliberately: the server decides whether to relay a transmission by comparing
advertised frequencies with **exact `float32` equality**
(`vcs-srs-server/state/server.go:211`), so the client keeps one canonical integer
representation and derives the wire float only at the edge. Per-digit editing
changes *which integer* is added or subtracted; it changes nothing about the
representation. The existing 24-bit clamp still applies.

### 5.2 State language

Five states, each carried by the frame so they read identically at every size:

| State | Treatment |
|---|---|
| **Selected** | Blue border + blue corner brackets |
| **Receiving** | Green traffic dot, talker name in green, green VU |
| **Transmitting** | Blue frame glow, keyed PTT, talker line shows `▲ you` with a blue VU |
| **Intercom** | Amber left edge (3px), amber corner brackets, amber `R-id` |
| **Disabled** | Drained border and background, desaturated LCD, 35% PTT, diagonal hatch |

Two deliberate choices. **Intercom uses a different colour channel from
selection**, so a selected intercom radio shows both without the two competing.
And **transmitting is visually distinct from receiving** — your own transmission
(`▲ you`, blue) never looks like someone else's (`▶ Dabble`, green).

Disabled replaces the talker line with an `OFF` marker, since a disabled radio
cannot receive.

### 5.3 The talker line

Dot, name and VU meter are one grouped unit: the name sizes to its content and
the flexible spacer sits *after* the meter. In the shipped card the name has
`flex: 1`, which pushes the meter to the far edge where it reads as unrelated.

### 5.4 PTT

The PTT is a transmit **indicator**, not a control — real push-to-talk runs
through the configured keybind, and making the button live would need backend
binding surface that does not exist. It stays a disabled element with the native
"does nothing" affordance, and shrinks to the sizes in §2.

---

## 6. The three bugs

- **Resize handle** moves onto the card's own corner, inside the block bounds.
- **Overflow**: no variant is wider than 360, and the default becomes VERTICAL at
  280 — comfortably inside the popout's 516px inner width, with room for two
  NARROW-V or one of anything else per row.
- **Chrome**: SAVE / REVERT / RESET and the close control get a real layout rather
  than overlapping.

---

## 7. Data this design needs that does not exist yet

**The talker name and VU meter have no live per-radio feed.** Phase 7.3 added RX
transmission detection for the *history log* — `voice.Options.OnRX` fires once per
*completed* transmission, which is the wrong shape for a live indicator. There is
no per-radio "someone is talking right now" state on the frontend.

The components are therefore built with the data plumbed through and rendering
`no traffic`. They become live the day a per-radio RX feed lands, with no further
component change. **This is not faked** — no simulated VU activity, no placeholder
talker names. A card that is not receiving shows that it is not receiving.

Wiring that feed is out of scope here (§10) and wants its own design: it needs a
live `voice:rx_state` style event that the history pipeline deliberately does not
provide.

---

## 8. What this supersedes in Phase 7.3

Phase 7.3's §6 specified a free-resize `flex-wrap` grid with per-radio pixel
sizing, and Task 15 implemented it. This design replaces the **resize model**
while keeping the grid:

- `SetCommsLayout` still owns block layout; `captureCommsWindowSize` still owns
  the window size.
- `profile.Block.{w,h}` becomes `profile.Block.variant` (§4.1).
- `shared/layout.ts`'s `place()` wrap rule stays, and keeps serving both the real
  grid and the Profiles screen's `LayoutPreview` — variants change what a block
  measures, not how blocks flow.
- `MIN_BLOCK_W`/`MIN_BLOCK_H` become redundant: a block is always exactly one
  descriptor's size. They are removed rather than left as dead constants, along
  with the Go-side `profile.MinBlockW`/`MinBlockH` clamps in `Reconcile` and
  `SetCommsLayout`.

The Phase 7.3 branch grows rather than closing. That is deliberate: the resize
model it shipped is three commits old and unmerged, so changing it now costs less
than shipping it and revising later.

---

## 9. Testing

- **The registry is the contract.** A test asserts every descriptor renders in its
  declared shell at its declared size, driven by iterating `variants.ts` — so a
  new descriptor is covered the moment it is added, and a descriptor whose shell
  cannot render it fails immediately.
- **Snap resolution**: a table of (dragged w, h) → expected variant, including the
  midpoints between adjacent stops.
- **Per-digit editing**: each digit position steps the correct decade; carries
  propagate; the 24-bit clamp holds; the committed value is integer kHz.
- **State language**: each of the five states renders its distinguishing marker,
  and receiving vs transmitting are visually distinct (different marker, different
  colour class).
- **`no traffic` is the honest default** — a test asserts that with no RX feed the
  talker line shows no name and an empty meter, so a future regression cannot
  quietly invent activity.
- **StrictMode**: both window roots render inside it, so any effect attaching
  pointer listeners needs a StrictMode test plus a real-unmount control.
- **`typescript:S1082`**: every non-button element carrying a pointer handler —
  the resize handle, the digit targets — needs `role`, `tabIndex` and keyboard
  operation in the same commit.

---

## 10. Out of scope

- **A live per-radio RX feed** (§7). Needed for the talker line and VU to show
  anything; wants its own design.
- **Making the PTT clickable** (§5.4).
- **Volume and balance knobs.** The prototype has them; there is no backend
  per-radio volume, so they would be inert controls — the exact defect this
  project keeps catching.
- **The keybind chips** (`PTT` / `SEL`) in the prototype's status row.
- **Encryption pills.** PROTO_GAPS #1; not on the wire.
- **Changing the grid's flow or wrap rule** (§8).

---

## 11. Open question, recorded not closed

Nothing in this design has been seen in a real Wails webview. The mockups were
rendered in a browser at the same pixel sizes using the same tokens, which is
close but not identical — font metrics for `JetBrains Mono` and the LCD glow
effects are the two most likely to differ. The first task that renders a real
card should be looked at before the rest are built.
