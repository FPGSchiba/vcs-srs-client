# Radio card — variant reference

**Status:** agreed 2026-09-30. This is the visual reference for the Comms radio
card redesign. The implementation spec that cites it is
[`docs/superpowers/specs/2026-09-30-vcs-client-comms-radio-redesign-design.md`](../../docs/superpowers/specs/2026-09-30-vcs-client-comms-radio-redesign-design.md).

This document is self-contained on purpose: the mockups it describes were worked
out interactively and would otherwise be lost. Everything needed to rebuild the
design is here — sizes, row breakdowns, state treatments, and the tokens used.

Related: [`project/lib/radio.jsx`](./project/lib/radio.jsx) is the original
prototype (`RadioWidget` + `RadioStrip`); [`project/styles.css`](./project/styles.css)
is the canonical token source.

---

## 1. The roster

Four variants. All four carry the radio name, the talker name and the VU meter.

| Variant | Size | Orientation | LCD | PTT | Chips | `MHZ` unit |
|---|---|---|---|---|---|---|
| **VERTICAL** | 280×168 | column | 30px | 96×28 | `ON` · `INTERCOM` | yes |
| **HORIZONTAL** | 360×115 | row | 22px | 64×64 | `ON` | yes |
| **NARROW-H** | 300×68 | row | 21px | 32×30 | frame only | — |
| **NARROW-V** | 150×136 | column | 18px | fill×26 | frame only | — |

For scale: the card being replaced is **516×180, 14px frequency, 160×48 PTT**.
Every variant is smaller and every frequency is larger.

> **Heights corrected 2026-09-30, after implementation.** The heights first agreed here
> (166 / 110 / 68 / 124) were derived from the row tables in §2 by addition, and three of
> them were too small for the components as actually built — the frame's 1px border was
> never budgeted, and several rows measure larger than the table assumed. The real
> components were then measured in headless Chrome:
>
> | Variant | Agreed | Measured need | Shipped |
> |---|---|---|---|
> | VERTICAL | 166 | 167.6 | **168** |
> | HORIZONTAL | 110 | 114.6 | **115** |
> | NARROW-H | 68 | 56.2 | **68** (no overflow; unchanged) |
> | NARROW-V | 124 | 135.4 | **136** |
>
> NARROW-V is the significant one: its LCD measures 34px against the 30 in §2, its header
> 16.8 against 14, and its status row 15.4 against 12, so 124 was never achievable. Under
> the agreed heights every row had `flex-shrink: 1`, so the shortfall was absorbed silently
> by compressing the LCD — which has `overflow: hidden` — and clipping the frequency digits.
> The §2 row tables below are kept as the agreed DESIGN INTENT and have not been rewritten;
> where they disagree with these totals, the totals are what ships.
>
> Consequence worth knowing: at 150×136, NARROW-V's area (20400) exactly equals NARROW-H's
> (300×68), so the keyboard resize order in `RadioBlock.tsx` needs an explicit tiebreak.
>
> Open for the user: accept these heights, or hold the agreed footprint and shrink the
> internals instead — the latter means a smaller frequency, which cuts against the
> redesign's first goal.

---

## 2. Layouts

### VERTICAL · 280×166

```
┌─────────────────────────────────┐  pad 14
│ R01  Fleet Common         [ON]  │  header    20
│                                 │  gap        9
│      ┌───────────────────┐      │
│      │  118.5̲00      MHZ │      │  LCD       46   (30px digits)
│      └───────────────────┘      │  gap        9
│ [INTERCOM]         ┌──────────┐ │  PTT row   28   (chip left, PTT right)
│                    │  ◉ PTT   │ │  gap        9
│                    └──────────┘ │
│ ───────────────────────────────  │  divider
│ ● ▶ Dabble  ▮▮▮▮▯▯              │  status    20
└─────────────────────────────────┘  pad 12   =  166
```

The status row sits on a `border-top` divider. Dot, talker and VU are one group
on the left; the flexible spacer is **after** the meter.

### HORIZONTAL · 360×110

```
┌──────────────────────────────────────────────┐  pad 14
│ R01  Fleet Common          [ON]     ┌──────┐ │  header  18
│                                     │      │ │  gap      8
│ ┌──────────────────┐                │  ◉   │ │  LCD     34   (22px)
│ │ 118.5̲00     MHZ  │                │ PTT  │ │  gap      8
│ └──────────────────┘                │      │ │
│ ● ▶ Dabble  ▮▮▮▯▯                   └──────┘ │  status  16
└──────────────────────────────────────────────┘  pad 12  =  110
                                       64×64
```

The square PTT is centred vertically beside the column and sets the card's
minimum height. The LCD is left-aligned (`align-self: flex-start`), not centred.

### NARROW-H · 300×68

```
┌────────────────────────────────────────────┐  pad 9
│ ┌────────────┐  R01  Fleet Common    ┌───┐ │
│ │  118.5̲00   │  ● ▶ Dabble  ▮▮▮▯     │ ◉ │ │  two stacked lines
│ └────────────┘                       └───┘ │
└────────────────────────────────────────────┘  pad 9  =  68
   LCD 21px      name over talker+VU    32×30
```

LCD anchors the left, the two text lines take the middle, the PTT anchors the
right. No chips; no `MHZ` suffix.

### NARROW-V · 150×124

```
┌──────────────────┐  pad 10
│ R01  Fleet Comm… │  rid + name   14   ← name LEFT-ALIGNED after R01
│                  │  gap           7
│ ┌──────────────┐ │
│ │   118.5̲00    │ │  LCD          30   (18px, centred)
│ └──────────────┘ │  gap           7
│ ● ▶ Dabble ▮▮▮▯  │  talker + VU  12
│                  │  gap           7
│ ┌──────────────┐ │
│ │    ◉ PTT     │ │  PTT          26   (full width)
│ └──────────────┘ │
└──────────────────┘  pad 10  =  124
```

150 wide rather than 130 so "Fleet Common" fits beside `R01` without truncating.

---

## 3. Why the heights are what they are

Every variant is a fixed-row stack with **one spacing value** and no leftover
space. This matters: an earlier vertical draft was 300×260, and the excess was
not spacing but *leftover* — the status row was pinned with `margin-top: auto`,
so all unused height collected into a single 39px gap. Removing that, without
shrinking any element, gave 166.

The rule to preserve: **a card's height is the sum of its rows, never a number
chosen first and filled afterwards.**

---

## 4. State language

Carried by the frame, so it reads identically at every size.

| State | Treatment |
|---|---|
| **Selected** | Blue border `--ac-primary` + blue corner brackets |
| **Receiving** | Green dot (glow), talker name green, green VU |
| **Transmitting** | Blue frame glow, keyed PTT, talker line `▲ you` + blue VU |
| **Intercom** | Amber left edge (3px), amber corner brackets, amber `R-id` |
| **Disabled** | Drained border/background, desaturated LCD, PTT at 35%, diagonal hatch, talker line replaced by `OFF` |

Two deliberate choices:

- **Intercom uses a different colour channel from selection**, so a selected
  intercom radio shows both without the two competing. This is also why the
  narrow variants can drop the `ICOM` chip without losing the information — the
  frame already carries it.
- **Transmitting is visually distinct from receiving.** Your own transmission
  (`▲ you`, blue) must never look like someone else's (`▶ Dabble`, green).

### Corner brackets

10px L-shapes at three corners — top-left (opacity .7), top-right (.5),
bottom-left (.5). From the prototype. They take the state colour.

---

## 5. Tokens used

All from [`project/styles.css`](./project/styles.css) — no invented tokens.

```
--bg-2  #0a141f   card gradient end        --ac-primary #60a5fa  selected / transmitting
--bg-3  #0e1a28   card gradient start      --ac-lcd     #7af0a4  LCD digits
--bd-1  #15273a   dividers                 --ac-ok      #4ade80  receiving
--bd-2  #1d3147   default border           --ac-warn    #f5a524  intercom
--tx-0  #eaf2fb   radio name               --ac-alert   #ef4f4f  (unused here)
--tx-2  #8ba0b6   secondary text           --bg-lcd     #060e0a  LCD background
--tx-3  #5b748b   labels                   --ff-mono    JetBrains Mono
--tx-4  #3b556e   disabled / idle dot
```

Card background is `linear-gradient(180deg, var(--bg-3), var(--bg-2))`.
The LCD keeps its existing `.lcd-screen` inset shadow and scanline overlay.

---

## 6. Frequency editing

Hover a digit → it highlights → wheel changes **that decade only**.

```
1 1 8 . 5 0 0
│ │ │   │ │ └─ 1 kHz
│ │ │   │ └─── 10 kHz
│ │ │   └───── 100 kHz
│ │ └───────── 1 MHz
│ └─────────── 10 MHz
└───────────── 100 MHz
```

Carries propagate — a digit passing 9 wraps and bumps the digit to its left.
Click bumps up, shift-click bumps down (from the prototype).

**The value is integer kHz throughout.** The server compares advertised
frequencies with exact `float32` equality, so the client keeps one canonical
integer and derives the wire float only at the edge. Per-digit editing changes
*which integer* is added or subtracted — never the representation.

`.lcd-digit` in `components.css` already ships `cursor: ns-resize` and a hover
glow. The styling for this interaction exists; only the wiring is missing.

---

## 7. What the narrow variants drop

Only the `ON`/`INTERCOM` **chips** and the `MHZ` unit suffix.

They keep the radio name, the talker name and the VU meter. Intercom survives via
the frame (§4).

---

## 8. Descriptor shape

The table in §1 is the registry. Adding a **size** of an existing shape is a
descriptor entry and nothing else; adding a **shape** is one new shell.

```ts
interface RadioVariant {
  id: string;
  label: string;
  w: number; h: number;
  orientation: "column" | "row";
  lcdPx: number;
  ptt: { w: number | "fill"; h: number };
  shows: {
    chips: "full" | "enabled-only" | "none";
    unit: boolean;
    statusDivider: boolean;
  };
}
```

---

## 9. Things decided along the way, recorded so they are not re-litigated

- **The name is a label, not a text input.** Double-click to edit. The permanent
  bordered `<input>` is the single largest reason the shipped card reads as a
  settings form.
- **The PTT is an indicator, not a control.** Real push-to-talk runs through the
  configured keybind; making the button live needs backend surface that does not
  exist.
- **The talker line is one group.** Dot, name and meter together, with the
  flexible spacer *after* the meter — not `flex: 1` on the name, which pushes the
  meter to the far edge where it reads as unrelated.
- **`no traffic` is honest.** There is no live per-radio RX feed yet. The talker
  line and VU render empty until one exists — no simulated activity, no
  placeholder names.
- **Narrow-V name is left-aligned**, not centred. With `R01` holding the left, a
  centred name sits off true centre and reads as two floating items rather than
  one label.
