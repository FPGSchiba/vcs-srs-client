# Comms Radio Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the Comms radio card as four size variants driven by a descriptor registry, with a prominent per-digit-scrollable frequency, a clear state language, and a snap-to-variant resize model.

**Architecture:** Two layout shells (`RadioColumn`, `RadioRow`) render any descriptor from a registry in `variants.ts`. Shared primitives (`RadioFrame`, `FreqLcd`, `TalkerLine`, `PttIndicator`) size themselves from the descriptor. A block stores `variant` rather than `{w,h}`, so the snap model has no in-between sizes and adding a variant size is a data entry.

**Tech Stack:** React 18 + TypeScript + Zustand + Vite + `vitest`; Go 1.x (`internal/profile`, `internal/config`, `internal/app`) with `-tags purego`.

**Spec:** `docs/superpowers/specs/2026-09-30-vcs-client-comms-radio-redesign-design.md`
**Visual reference:** `design/vcs/radio-variants.md` — layouts, row breakdowns, state treatments, tokens. Read it alongside the spec; it carries the exact pixel values every task needs.

## Global Constraints

- **Every Go invocation carries `-tags purego`**, with `GOCACHE=$TMPDIR/vcs-gocache`.
- **Race runs need gcc on PATH:** `PATH="/c/msys64/ucrt64/bin:$PATH" CGO_ENABLED=1 GOCACHE=$TMPDIR/vcs-gocache go test -race -count=1 -tags purego ./...`. `internal/voice` needs `dangerouslyDisableSandbox` (it binds UDP sockets).
- **Typecheck with `(cd frontend && npx tsc --noEmit)`** — this exact form. The `npx --prefix frontend` form prints a help banner and exits 0 **without checking anything**.
- **Frontend suite baseline: 316/316 across 33 files.** Do not regress it.
- **`gopls` is unreliable in this repo** — 13 false positives in a previous phase, two of which would be hard compile errors if real. Never act on its diagnostics; `go build`/`go vet` is the only authority.
- **`gofmt -l` flags everything** because the tree is CRLF (`core.autocrlf=true`). Check your own files by copying them to a temp dir with LF endings and running `gofmt -l` there; fix anything dirty, preserving CRLF.
- **Frequencies stay integer kHz end to end.** The server decides whether to relay a transmission by comparing advertised frequencies with **exact `float32` equality** (`vcs-srs-server/state/server.go:211`). Per-digit editing changes *which integer* is added or subtracted, never the representation.
- **`typescript:S1082`:** any non-button element carrying a click/pointer handler needs `role="button"`, `tabIndex` and Enter/Space handling **in the same commit**.
- **Both window roots render inside `React.StrictMode`.** Any effect attaching pointer listeners needs idempotent cleanup, a StrictMode test **and** a real-unmount control.
- **No faked telemetry.** There is no live per-radio RX feed (spec §7). The talker line and VU render empty until one lands — no simulated activity, no placeholder talker names.
- **Only canonical tokens.** Colours come from `design/vcs/project/styles.css`. `--ac-danger` does not exist; the alert token is `--ac-alert`.
- **No `Co-Authored-By` trailers on commits.**

## Review Focus

Five input classes the spec implies but does not enumerate, each pinned to a test in the task that owns the code:

1. **A block whose `variant` names an id no descriptor defines** — a renamed descriptor, or a hand-edited profile. Expected: falls back to `vertical` and renders, rather than crashing or rendering a zero-size card. *(Task 1)*
2. **Per-digit carry at the top of the range.** Scrolling the 100 MHz digit up past the 24-bit kHz ceiling (16 777 215 kHz) must not wrap to a garbage frequency the server will silently refuse to relay, nor clamp to the ceiling and rewrite the digits the user never touched. Expected: the step is refused. *(Tasks 5 and 6)*
3. **A radio name longer than the card.** Especially NARROW-V at 150px wide. Expected: ellipsis, never pushing the LCD out of the card or clipping it. *(Tasks 8 and 9)*
4. **A drag landing exactly equidistant between two variants.** Expected: deterministic resolution — the same input always yields the same variant, independent of descriptor array order. *(Task 1)*
5. **A profile with no blocks.** `place([])` must not produce NaN geometry from the empty-array spread, and the Profiles preview must draw nothing rather than a degenerate rect. *(Tasks 4 and 12)*

---

## File Structure

**Created:**
- `frontend/src/windows/comms/variants.ts` — the descriptor registry, `nearestVariant`, `variantById`
- `frontend/src/windows/comms/RadioFrame.tsx` — border, corner brackets, intercom edge, state treatment
- `frontend/src/windows/comms/TalkerLine.tsx` — dot + name + VU as one group
- `frontend/src/windows/comms/PttIndicator.tsx` — transmit indicator, sized from the descriptor
- `frontend/src/windows/comms/RadioColumn.tsx` — shell for `orientation: "column"`
- `frontend/src/windows/comms/RadioRow.tsx` — shell for `orientation: "row"`
- `frontend/src/windows/comms/shells.tsx` — `RadioShellProps` and the orientation→shell map
- `frontend/src/shared/freq.ts` — integer-kHz arithmetic, digit decades, step-and-refuse
- Test files alongside each.

**Modified:**
- `frontend/src/shared/components/LcdFreq.tsx` — per-digit editing
- `frontend/src/windows/comms/RadioCard.tsx` — descriptor resolution, name-as-label
- `frontend/src/windows/comms/RadioBlock.tsx` — snap resize, handle on the card corner
- `frontend/src/windows/comms/CommsApp.tsx` — variant grid, chrome layout
- `frontend/src/shared/layout.ts` — `place()` from variants; `MIN_BLOCK_*`/`clampBlock` removed
- `frontend/src/shared/styles/components.css` — digit-button reset, `.lcd-unit`, chrome text buttons
- `internal/profile/profile.go` — `Block.Variant` replaces `W`/`H`; `MinBlockW/H`/`DefaultBlockW/H` removed
- `internal/config/config.go` — `LayoutBlock.Variant`
- `internal/app/profiles.go` — mapping + `SetCommsLayout` clamp removal
- `frontend/src/shared/api/client.ts` — `LayoutBlock` type

**Deliberately unchanged:** `frontend/src/windows/main/screens/Profiles.tsx`. Its
`LayoutPreview` already draws from `place(blocks, win)` and never touches a
block's `w`/`h` itself, so Task 4's change reaches it for free. Only its test
file gains cases.

---

## Task 1: The variant registry

**Files:**
- Create: `frontend/src/windows/comms/variants.ts`
- Test: `frontend/src/windows/comms/variants.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `export interface RadioVariant { id: string; label: string; w: number; h: number; orientation: "column" | "row"; lcdPx: number; ptt: { w: number | "fill"; h: number }; shows: { chips: "full" | "enabled-only" | "none"; unit: boolean; statusDivider: boolean } }`
  - `export const VARIANTS: readonly RadioVariant[]`
  - `export const DEFAULT_VARIANT_ID = "vertical"`
  - `export function variantById(id: string | undefined): RadioVariant`
  - `export function nearestVariant(w: number, h: number): RadioVariant`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/comms/variants.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { VARIANTS, variantById, nearestVariant, DEFAULT_VARIANT_ID } from "./variants";

describe("the registry", () => {
  it("defines the four agreed variants at their agreed sizes", () => {
    const byId = Object.fromEntries(VARIANTS.map((v) => [v.id, v]));
    expect(byId.vertical).toMatchObject({ w: 280, h: 166, orientation: "column", lcdPx: 30 });
    expect(byId.horizontal).toMatchObject({ w: 360, h: 110, orientation: "row", lcdPx: 22 });
    expect(byId["narrow-h"]).toMatchObject({ w: 300, h: 68, orientation: "row", lcdPx: 21 });
    expect(byId["narrow-v"]).toMatchObject({ w: 150, h: 124, orientation: "column", lcdPx: 18 });
  });

  it("gives every variant a unique id", () => {
    const ids = VARIANTS.map((v) => v.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("only uses orientations a shell exists for", () => {
    for (const v of VARIANTS) expect(["column", "row"]).toContain(v.orientation);
  });
});

describe("variantById", () => {
  it("resolves a known id", () => {
    expect(variantById("narrow-v").w).toBe(150);
  });

  // Review Focus #1.
  it("falls back to the default for an unknown id", () => {
    expect(variantById("does-not-exist").id).toBe(DEFAULT_VARIANT_ID);
  });

  it("falls back to the default for a missing id", () => {
    expect(variantById(undefined).id).toBe(DEFAULT_VARIANT_ID);
  });
});

describe("nearestVariant", () => {
  it("returns the exact variant for an exact size", () => {
    expect(nearestVariant(280, 166).id).toBe("vertical");
    expect(nearestVariant(360, 110).id).toBe("horizontal");
    expect(nearestVariant(300, 68).id).toBe("narrow-h");
    expect(nearestVariant(150, 124).id).toBe("narrow-v");
  });

  it("uses BOTH axes, so wide-and-short differs from wide-and-tall", () => {
    // 330x70 is close to narrow-h (300x68) on both axes.
    expect(nearestVariant(330, 70).id).toBe("narrow-h");
    // 330x115 is close to horizontal (360x110).
    expect(nearestVariant(330, 115).id).toBe("horizontal");
  });

  it("snaps a tiny drag to the smallest variant rather than returning nothing", () => {
    expect(nearestVariant(10, 10).id).toBe("narrow-v");
  });

  it("snaps an enormous drag to the largest rather than returning nothing", () => {
    expect(nearestVariant(5000, 5000).id).toBeDefined();
  });

  // Review Focus #4.
  it("is deterministic and order-independent for an equidistant drag", () => {
    // Find a point equidistant from two variants by construction: the midpoint
    // between vertical (280,166) and narrow-v (150,124).
    const mid = { w: (280 + 150) / 2, h: (166 + 124) / 2 };
    const first = nearestVariant(mid.w, mid.h).id;
    // Same input, repeated — must not vary.
    for (let i = 0; i < 20; i++) expect(nearestVariant(mid.w, mid.h).id).toBe(first);
    // And the tie-break must not depend on array order: reversing the registry
    // in a copy and re-resolving by the same rule yields the same winner.
    const dist = (v: { w: number; h: number }) => (v.w - mid.w) ** 2 + (v.h - mid.h) ** 2;
    const sorted = [...VARIANTS].sort((a, b) => dist(a) - dist(b) || a.id.localeCompare(b.id));
    expect(sorted[0].id).toBe(first);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/variants.test.ts)`
Expected: FAIL — cannot resolve `./variants`.

- [ ] **Step 3: Write the registry**

Create `frontend/src/windows/comms/variants.ts`:

```ts
/**
 * The radio-card variant registry.
 *
 * This file is the contract for the whole Comms card design. Adding a new SIZE
 * of an existing shape is an entry here and nothing else — it joins the snap
 * set automatically and the shells render it without change. Adding a new
 * SHAPE is one new shell plus an entry here.
 *
 * If adding a size ever requires touching a file other than this one, the
 * abstraction has leaked. See docs/superpowers/specs/2026-09-30-vcs-client-comms-radio-redesign-design.md §3.2
 * and design/vcs/radio-variants.md for the agreed layouts.
 */

export interface RadioVariant {
  id: string;
  /** Shown in the resize affordance. */
  label: string;
  w: number;
  h: number;
  orientation: "column" | "row";
  /** Frequency digit size in px. */
  lcdPx: number;
  /** "fill" means the PTT spans the card's content width. */
  ptt: { w: number | "fill"; h: number };
  shows: {
    /** "full" = ON + INTERCOM, "enabled-only" = ON, "none" = frame carries it. */
    chips: "full" | "enabled-only" | "none";
    /** The "MHZ" suffix after the digits. */
    unit: boolean;
    /** The border-top above the status row. */
    statusDivider: boolean;
  };
}

export const DEFAULT_VARIANT_ID = "vertical";

export const VARIANTS: readonly RadioVariant[] = [
  {
    id: "vertical",
    label: "Vertical",
    w: 280,
    h: 166,
    orientation: "column",
    lcdPx: 30,
    ptt: { w: 96, h: 28 },
    shows: { chips: "full", unit: true, statusDivider: true },
  },
  {
    id: "horizontal",
    label: "Horizontal",
    w: 360,
    h: 110,
    orientation: "row",
    lcdPx: 22,
    ptt: { w: 64, h: 64 },
    shows: { chips: "enabled-only", unit: true, statusDivider: false },
  },
  {
    id: "narrow-h",
    label: "Narrow (wide)",
    w: 300,
    h: 68,
    orientation: "row",
    lcdPx: 21,
    ptt: { w: 32, h: 30 },
    shows: { chips: "none", unit: false, statusDivider: false },
  },
  {
    id: "narrow-v",
    label: "Narrow (tall)",
    w: 150,
    h: 124,
    orientation: "column",
    lcdPx: 18,
    ptt: { w: "fill", h: 26 },
    shows: { chips: "none", unit: false, statusDivider: false },
  },
];

const DEFAULT = VARIANTS.find((v) => v.id === DEFAULT_VARIANT_ID) as RadioVariant;

/**
 * Resolves a stored variant id.
 *
 * An unknown or missing id falls back to the default rather than throwing: a
 * hand-edited profile, or one written before a descriptor was renamed, must
 * still render. This is a default for a missing field, not a migration —
 * the {w,h} block format never left local development.
 */
export function variantById(id: string | undefined): RadioVariant {
  if (!id) return DEFAULT;
  return VARIANTS.find((v) => v.id === id) ?? DEFAULT;
}

/**
 * Resolves a dragged size to the nearest variant by Euclidean distance in
 * (w, h), so both axes count: dragging wide-and-short lands on a narrow
 * strip while wide-and-tall lands on the horizontal card.
 *
 * Ties break on id, NOT on array order, so reordering the registry can never
 * silently change which variant a given drag produces.
 */
export function nearestVariant(w: number, h: number): RadioVariant {
  const dist = (v: RadioVariant) => (v.w - w) ** 2 + (v.h - h) ** 2;
  return [...VARIANTS].sort((a, b) => dist(a) - dist(b) || a.id.localeCompare(b.id))[0];
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/windows/comms/variants.test.ts)`
Expected: PASS, all cases.

- [ ] **Step 5: Typecheck and commit**

```bash
(cd frontend && npx tsc --noEmit)
git add frontend/src/windows/comms/variants.ts frontend/src/windows/comms/variants.test.ts
git commit -m "feat(comms): radio variant registry

Four descriptors with the agreed sizes. Adding a size is an entry here and
nothing else; ties in nearestVariant break on id rather than array order, so
reordering the registry cannot silently change which variant a drag produces."
```

---

## Task 2: Go — `profile.Block.Variant`

**Files:**
- Modify: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: Task 1's variant ids (`vertical`, `horizontal`, `narrow-h`, `narrow-v`) as strings. Go does not import the registry; the ids are a wire contract.
- Produces:
  - `type Block struct { RadioID uint32 \`json:"radio_id"\`; Variant string \`json:"variant"\` }`
  - `const DefaultVariant = "vertical"`
  - `MinBlockW`, `MinBlockH`, `DefaultBlockW`, `DefaultBlockH` **removed**.

- [ ] **Step 1: Write the failing tests**

Replace the block-geometry cases in `internal/profile/profile_test.go` with these, keeping every other test in the file unchanged:

```go
func TestReconcileAppendsMissingBlockWithDefaultVariant(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 2, Name: "b", FrequencyKHz: 122750},
		},
		Layout: Layout{Blocks: []Block{{RadioID: 1, Variant: "narrow-v"}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 2 {
		t.Fatalf("blocks = %+v, want one appended for radio 2", d.Layout.Blocks)
	}
	if got := d.Layout.Blocks[0]; got.Variant != "narrow-v" {
		t.Fatalf("existing block's variant must be preserved, got %+v", got)
	}
	if got := d.Layout.Blocks[1]; got.RadioID != 2 || got.Variant != DefaultVariant {
		t.Fatalf("appended block = %+v, want radio 2 at %q", got, DefaultVariant)
	}
}

func TestReconcileFillsEmptyVariant(t *testing.T) {
	// A hand-edited profile, or one written before a descriptor existed.
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: ""}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0].Variant; got != DefaultVariant {
		t.Fatalf("Variant = %q, want %q — an empty variant is a missing field, not an error", got, DefaultVariant)
	}
}

func TestReconcilePreservesAnUnknownVariant(t *testing.T) {
	// Go does not know the registry. A variant this build has never heard of
	// must survive a load/save cycle so a profile written by a NEWER client is
	// not silently downgraded — the frontend falls back at render time.
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: "dial-round"}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0].Variant; got != "dial-round" {
		t.Fatalf("Variant = %q, want the unknown id preserved", got)
	}
}

func TestReconcileDropsOrphanBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, Variant: "vertical"}, {RadioID: 9, Variant: "vertical"}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 1 || d.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want orphan dropped", d.Layout.Blocks)
	}
}

func TestBlockRoundTripsVariant(t *testing.T) {
	src := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: true}},
		Layout: Layout{
			Window: WindowSize{W: 540, H: 720},
			Blocks: []Block{{RadioID: 1, Variant: "narrow-h"}},
		},
	}
	b, err := Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(b), `"variant": "narrow-h"`) {
		t.Fatalf("block must serialise its variant:\n%s", b)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Layout.Blocks[0].Variant != "narrow-h" {
		t.Fatalf("blocks = %+v", got.Layout.Blocks)
	}
}
```

Delete `TestReconcileClampsDegenerateBlockSizes` — a block no longer has a size to clamp.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/`
Expected: FAIL — `Block` has no field `Variant`; `DefaultVariant` undefined.

- [ ] **Step 3: Change `Block` and `Reconcile`**

In `internal/profile/profile.go`, replace the `Block` type:

```go
// Block is one radio's tile in the Comms grid.
//
// It stores a VARIANT ID, not a size. The frontend's variant registry
// (frontend/src/windows/comms/variants.ts) owns the dimensions, so a variant
// can change its own size without invalidating saved profiles, and there is no
// in-between size a card could be stretched into.
//
// Go deliberately does not know the registry: an id this build has never seen
// is preserved through a load/save cycle so a profile written by a newer client
// is not silently downgraded. The frontend falls back to its default at render
// time.
type Block struct {
	RadioID uint32 `json:"radio_id"`
	Variant string `json:"variant"`
}

// DefaultVariant is what a block with no variant gets. It must match
// DEFAULT_VARIANT_ID in the frontend registry.
const DefaultVariant = "vertical"
```

Delete the `MinBlockW`, `MinBlockH`, `DefaultBlockW`, `DefaultBlockH` constants and their doc comment block.

Replace `Reconcile`'s block loop:

```go
	known := make(map[uint32]bool, len(d.Radios))
	for _, r := range d.Radios {
		known[r.ID] = true
	}
	have := make(map[uint32]bool, len(d.Layout.Blocks))
	kept := make([]Block, 0, len(d.Layout.Blocks))
	for _, b := range d.Layout.Blocks {
		if !known[b.RadioID] || have[b.RadioID] {
			continue // an orphan, or a second block for one radio
		}
		if b.Variant == "" {
			b.Variant = DefaultVariant
		}
		have[b.RadioID] = true
		kept = append(kept, b)
	}
	for _, r := range d.Radios {
		if !have[r.ID] {
			kept = append(kept, Block{RadioID: r.ID, Variant: DefaultVariant})
		}
	}
	d.Layout.Blocks = kept
```

Leave the window-size defaulting above it exactly as it is — `DefaultWindowW`/`DefaultWindowH` stay.

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -v`
Expected: PASS.

- [ ] **Step 5: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/profile/
git add internal/profile/
git commit -m "feat(profile): blocks store a variant id, not a size

The frontend registry owns dimensions, so a variant can change its own size
without invalidating saved profiles and there is no in-between size a card can
be stretched into. Go does not know the registry: an unknown id survives a
load/save cycle so a profile from a newer client is not downgraded.

MinBlockW/H and DefaultBlockW/H are removed rather than left as dead constants
— a block is always exactly one descriptor's size."
```

---

## Task 3: Go — config and app mapping

**Files:**
- Modify: `internal/config/config.go`, `internal/app/profiles.go`
- Test: `internal/config/config_test.go`, `internal/app/profiles_test.go`

**Interfaces:**
- Consumes: Task 2's `profile.Block{RadioID, Variant}`, `profile.DefaultVariant`.
- Produces: `config.LayoutBlock{ RadioID uint32 \`toml:"radio_id"\`; Variant string \`toml:"variant"\` }`.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, change the layout assertions inside `TestProfileFieldsRoundTrip`:

```go
	cfg.CommsLayout = CommsLayout{
		WindowW: 540,
		WindowH: 720,
		Blocks:  []LayoutBlock{{RadioID: 1, Variant: "vertical"}, {RadioID: 2, Variant: "narrow-h"}},
	}
```

and after reload:

```go
	if back.CommsLayout.WindowW != 540 || len(back.CommsLayout.Blocks) != 2 {
		t.Errorf("CommsLayout = %+v", back.CommsLayout)
	}
	if back.CommsLayout.Blocks[1].RadioID != 2 || back.CommsLayout.Blocks[1].Variant != "narrow-h" {
		t.Errorf("block order or variant lost: %+v", back.CommsLayout.Blocks)
	}
```

In `internal/app/profiles_test.go`, add:

```go
func TestSetCommsLayoutStoresVariantsVerbatim(t *testing.T) {
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, Variant: "narrow-v"}},
	}); err != nil {
		t.Fatalf("SetCommsLayout: %v", err)
	}
	got := a.GetCommsLayout()
	if len(got.Blocks) != 1 || got.Blocks[0].Variant != "narrow-v" {
		t.Fatalf("blocks = %+v, want the variant carried through unchanged", got.Blocks)
	}
}

func TestSetCommsLayoutPreservesAnUnknownVariant(t *testing.T) {
	// The Go side must not validate against a registry it does not have.
	a := newTestAppWithConfig(t, config.Default(), filepath.Join(t.TempDir(), "config.toml"))
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, Variant: "dial-round"}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := a.GetCommsLayout().Blocks[0].Variant; got != "dial-round" {
		t.Fatalf("Variant = %q, want it preserved", got)
	}
}
```

Adapt any other test in either file that constructs a `LayoutBlock`/`ProfileBlockDTO` with `W`/`H` — change the literal to `Variant`, keeping its assertion's intent. Do **not** weaken an assertion to make it compile: if a test asserted a block width carried to disk, assert the variant carries instead.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/ ./internal/app/`
Expected: FAIL — `LayoutBlock` has no field `Variant`.

- [ ] **Step 3: Change the types and mapping**

In `internal/config/config.go`:

```go
// LayoutBlock is one radio's tile. Order within CommsLayout.Blocks IS the flow
// order; there is deliberately no index field to keep consistent with the slice.
//
// Variant names a frontend descriptor (see variants.ts). This package does not
// validate it: an id written by a newer client must survive a load/save cycle.
type LayoutBlock struct {
	RadioID uint32 `toml:"radio_id"`
	Variant string `toml:"variant"`
}
```

In `internal/app/dto.go`:

```go
type ProfileBlockDTO struct {
	RadioID uint32 `json:"radio_id"`
	Variant string `json:"variant"`
}
```

In `internal/app/profiles.go`, update **every** site that builds a
`ProfileBlockDTO` or a `config.LayoutBlock` to carry `Variant` instead of
`W`/`H`, and **delete the clamping block in `SetCommsLayout`** — there is no
size to clamp. Find them all first, rather than trusting this list:

```bash
grep -n "ProfileBlockDTO{\|LayoutBlock{\|profile.Block{" internal/app/*.go internal/config/*.go
```

At the time of writing that is the profile-summary mapper (~line 286), the two
layout converters, `GetCommsLayout` (~line 766), `SetCommsLayout` (~line 791)
and `ResetLayout` (~line 505). The two converters become:

```go
func profileLayoutToConfig(l profile.Layout) config.CommsLayout {
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, Variant: b.Variant})
	}
	return config.CommsLayout{WindowW: l.Window.W, WindowH: l.Window.H, Blocks: blocks}
}

func configLayoutToProfile(l config.CommsLayout) profile.Layout {
	blocks := make([]profile.Block, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, profile.Block{RadioID: b.RadioID, Variant: b.Variant})
	}
	return profile.Layout{
		Window: profile.WindowSize{W: l.WindowW, H: l.WindowH},
		Blocks: blocks,
	}
}
```

and in `SetCommsLayout`, replace the clamping loop with a plain copy:

```go
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, Variant: b.Variant})
	}
```

Do the same in `GetCommsLayout` and in `ResetLayout` (which builds one block per radio — it now uses `profile.DefaultVariant`).

**Keep `SetCommsLayout`'s window-size behaviour exactly as it is**: it ignores `l.Window` and carries over the stored `WindowW`/`WindowH`, because `captureCommsWindowSize` is the sole authority. Keep the explicit `sb.mu.Unlock()` before `emitProfileState()` — a deferred unlock there deadlocks the app.

- [ ] **Step 4: Run tests to verify they pass**

```bash
GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/ ./internal/app/ -v
PATH="/c/msys64/ucrt64/bin:$PATH" CGO_ENABLED=1 GOCACHE=$TMPDIR/vcs-gocache go test -race -count=1 -tags purego ./internal/app/
```
Expected: PASS, including the whole pre-existing `internal/app` suite.

- [ ] **Step 5: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/config/ ./internal/app/
git add internal/config/ internal/app/
git commit -m "feat(config,app): carry block variant instead of width/height

Drops the MinBlockW/H clamp from SetCommsLayout — a block is now always exactly
one descriptor's size, so there is nothing to clamp. Neither package validates
the variant id: an id from a newer client survives a round trip.

SetCommsLayout keeps ignoring l.Window (captureCommsWindowSize is the sole
authority) and keeps its explicit unlock before emitProfileState."
```

---

## Task 4: `layout.ts` — flow from variants

**Files:**
- Modify: `frontend/src/shared/layout.ts`, `frontend/src/shared/api/client.ts`
- Test: `frontend/src/shared/layout.test.ts`

**Interfaces:**
- Consumes: Task 1's `variantById`.
- Produces:
  - `LayoutBlock` in `client.ts` becomes `{ radio_id: number; variant: string }`
  - `place(blocks: LayoutBlock[], win: LayoutWindow): { rects: Placed[]; w: number; h: number }` — unchanged signature, sizes resolved from the registry
  - `GRID_PAD`, `GRID_GAP` unchanged
  - `MIN_BLOCK_W`, `MIN_BLOCK_H`, `DEFAULT_BLOCK_W`, `DEFAULT_BLOCK_H`, `clampBlock` **removed**

- [ ] **Step 1: Write the failing tests**

Replace `frontend/src/shared/layout.test.ts`'s body with:

```ts
import { describe, expect, it } from "vitest";
import { place, GRID_PAD, GRID_GAP } from "./layout";
import { variantById } from "../windows/comms/variants";

const b = (radio_id: number, variant: string) => ({ radio_id, variant });

describe("place", () => {
  it("sizes each rect from its variant", () => {
    const { rects } = place([b(1, "vertical")], { w: 540, h: 720 });
    const v = variantById("vertical");
    expect(rects[0]).toMatchObject({ x: 0, y: 0, w: v.w, h: v.h });
  });

  it("flows left to right with GRID_GAP between", () => {
    const { rects } = place([b(1, "narrow-v"), b(2, "narrow-v")], { w: 540, h: 720 });
    const v = variantById("narrow-v");
    expect(rects[0].x).toBe(0);
    expect(rects[1].x).toBe(v.w + GRID_GAP);
    expect(rects[1].y).toBe(0);
  });

  it("wraps when the next block would overflow the content width", () => {
    // Inner width at 540 is 540 - 2*12 = 516. Two verticals (280 each) plus the
    // gap is 568, so the second must wrap.
    const { rects } = place([b(1, "vertical"), b(2, "vertical")], { w: 540, h: 720 });
    expect(rects[1].x).toBe(0);
    expect(rects[1].y).toBe(variantById("vertical").h + GRID_GAP);
  });

  it("reports the content width and total height", () => {
    const out = place([b(1, "vertical")], { w: 540, h: 720 });
    expect(out.w).toBe(540 - 2 * GRID_PAD);
    expect(out.h).toBe(variantById("vertical").h);
  });

  // Review Focus #5.
  it("handles an empty block list without NaN geometry", () => {
    const out = place([], { w: 540, h: 720 });
    expect(out.rects).toEqual([]);
    expect(Number.isFinite(out.w)).toBe(true);
    expect(Number.isFinite(out.h)).toBe(true);
    expect(out.h).toBe(0);
  });

  it("falls back to the default variant for an unknown id rather than a zero-size rect", () => {
    const { rects } = place([b(1, "dial-round")], { w: 540, h: 720 });
    const d = variantById("dial-round"); // resolves to the default
    expect(rects[0].w).toBe(d.w);
    expect(rects[0].w).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/shared/layout.test.ts)`
Expected: FAIL — `place` reads `b.w`, which no longer exists on `LayoutBlock`.

- [ ] **Step 3: Rewrite `layout.ts`**

```ts
import type { LayoutBlock, LayoutWindow } from "./api/client";
import { variantById } from "../windows/comms/variants";

// The Comms flow container's padding and inter-block gap, in window pixels.
// CommsApp renders with exactly these values and `place` assumes them, so the
// Profiles preview and the real grid cannot drift apart.
export const GRID_PAD = 12;
export const GRID_GAP = 8;

export interface Placed {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * Flows blocks left to right inside the window's content width, wrapping when
 * the next block would overflow. This is the rule the real Comms grid (a CSS
 * flex-wrap container with GRID_PAD padding, GRID_GAP gap, flex-shrink 0 items
 * and align-content flex-start) produces, and the Profiles preview draws with
 * the same function. Pure, so the geometry is testable without rendering.
 *
 * Each block's size comes from its VARIANT, not from stored pixels — see
 * windows/comms/variants.ts. An unknown id resolves to the default rather than
 * producing a zero-size rect.
 */
export function place(
  blocks: LayoutBlock[],
  win: LayoutWindow,
): { rects: Placed[]; w: number; h: number } {
  const sized = blocks.map((b) => {
    const v = variantById(b.variant);
    return { w: v.w, h: v.h };
  });
  const inner = Math.max(win.w - 2 * GRID_PAD, ...sized.map((s) => s.w), 1);
  const rects: Placed[] = [];
  let x = 0;
  let y = 0;
  let rowH = 0;
  for (const s of sized) {
    if (x > 0 && x + s.w > inner) {
      x = 0;
      y += rowH + GRID_GAP;
      rowH = 0;
    }
    rects.push({ x, y, w: s.w, h: s.h });
    x += s.w + GRID_GAP;
    rowH = Math.max(rowH, s.h);
  }
  return { rects, w: inner, h: y + rowH };
}
```

In `frontend/src/shared/api/client.ts`:

```ts
export interface LayoutBlock {
  radio_id: number;
  variant: string;
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
(cd frontend && npx vitest run src/shared/layout.test.ts)
(cd frontend && npx tsc --noEmit)
```
Expected: tests PASS. `tsc` will still report errors in `RadioBlock.tsx`, `CommsApp.tsx` and `Profiles.tsx`, which Tasks 10–11 fix — note them and move on.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/layout.ts frontend/src/shared/layout.test.ts frontend/src/shared/api/client.ts
git commit -m "refactor(layout): flow blocks from their variant, drop the size clamps

place() resolves each block's dimensions from the registry rather than stored
pixels. MIN_BLOCK_*/DEFAULT_BLOCK_*/clampBlock are removed: a block is always
exactly one descriptor's size, so a minimum is meaningless. An empty block list
and an unknown variant id both have tests."
```

---

## Task 5: Per-digit frequency arithmetic

**Files:**
- Create: `frontend/src/shared/freq.ts`
- Test: `frontend/src/shared/freq.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `MIN_KHZ = 0`, `MAX_KHZ = 16_777_215`
  - `export function clampKhz(khz: number): number`
  - `export function mhzToKhz(mhz: number): number`
  - `export function khzToMhz(khz: number): number`
  - `export interface FreqCell { char: string; place: number | null }`
  - `export function digitsOf(khz: number): FreqCell[]`
  - `export function stepDigit(khz: number, place: number, dir: 1 | -1): number`

Pure module, no React. Every frequency rule lives here so the component is only wiring.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/shared/freq.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { MAX_KHZ, clampKhz, digitsOf, khzToMhz, mhzToKhz, stepDigit } from "./freq";

const render = (khz: number) => digitsOf(khz).map((c) => c.char).join("");

describe("digitsOf", () => {
  it("formats a typical frequency with a three-digit MHz part", () => {
    expect(render(118_500)).toBe("118.500");
  });

  it("pads the MHz part so digit positions never shift", () => {
    expect(render(5_000)).toBe("005.000");
    expect(render(0)).toBe("000.000");
  });

  it("grows past three MHz digits rather than truncating", () => {
    expect(render(MAX_KHZ)).toBe("16777.215");
  });

  it("tags each digit with its kHz decade and the separator with null", () => {
    const cells = digitsOf(118_500);
    expect(cells.map((c) => c.place)).toEqual([5, 4, 3, null, 2, 1, 0]);
  });
});

describe("stepDigit", () => {
  it("changes only the targeted decade", () => {
    expect(stepDigit(118_500, 2, 1)).toBe(118_600); // 100 kHz up
    expect(stepDigit(118_500, 3, -1)).toBe(117_500); // 1 MHz down
    expect(stepDigit(118_500, 0, 1)).toBe(118_501); // 1 kHz up
  });

  it("carries into the decade to its left, as the design specifies", () => {
    expect(render(stepDigit(118_509, 0, 1))).toBe("118.510");
    expect(render(stepDigit(118_900, 2, 1))).toBe("119.000");
  });

  it("borrows downward the same way", () => {
    expect(render(stepDigit(119_000, 2, -1))).toBe("118.900");
  });

  // Review Focus #2: the ceiling must not silently rewrite untouched digits.
  it("refuses a step past the 24-bit ceiling instead of clamping", () => {
    expect(stepDigit(MAX_KHZ, 0, 1)).toBe(MAX_KHZ);
    // 16_677_215 + a 100 MHz-decade step exceeds MAX. Clamping would rewrite
    // six digits the user never touched, so the step is refused.
    expect(stepDigit(16_677_215, 5, 1)).toBe(16_677_215);
  });

  it("refuses a step below zero", () => {
    expect(stepDigit(0, 0, -1)).toBe(0);
    expect(stepDigit(500, 3, -1)).toBe(500);
  });

  it("allows a step that lands exactly on a bound", () => {
    expect(stepDigit(MAX_KHZ - 1, 0, 1)).toBe(MAX_KHZ);
    expect(stepDigit(1, 0, -1)).toBe(0);
  });
});

describe("clampKhz", () => {
  it("bounds both ends", () => {
    expect(clampKhz(-5)).toBe(0);
    expect(clampKhz(MAX_KHZ + 5)).toBe(MAX_KHZ);
    expect(clampKhz(118_500)).toBe(118_500);
  });
});

describe("the MHz edge conversions", () => {
  it("khzToMhz mirrors the Go KHz.MHz32 expression exactly", () => {
    // internal/voice/freq.go: float32(uint32(k)) / 1000.0
    expect(khzToMhz(118_500)).toBe(Math.fround(118_500 / 1000));
  });

  it("round-trips the frequencies real radios use", () => {
    for (const k of [0, 1, 118_500, 122_750, 251_000, 999_999, 1_000_000]) {
      expect(mhzToKhz(khzToMhz(k))).toBe(k);
    }
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/shared/freq.test.ts)`
Expected: FAIL — cannot resolve `./freq`.

- [ ] **Step 3: Write the module**

Create `frontend/src/shared/freq.ts`:

```ts
/**
 * Frequency arithmetic in integer kHz.
 *
 * The wire field is a 24-bit unsigned kHz integer (internal/voice/freq.go,
 * type KHz), and the SERVER decides whether to relay a transmission by
 * comparing advertised frequencies with exact float32 equality
 * (vcs-srs-server/state/server.go). So the client keeps ONE canonical integer
 * and derives the float only at the edge: per-digit editing changes which
 * integer is added or subtracted, never the representation.
 */

export const MIN_KHZ = 0;
export const MAX_KHZ = 16_777_215; // 2^24 - 1

/** How many MHz digits are always rendered, so a digit never changes column. */
const MHZ_PAD = 3;

export function clampKhz(khz: number): number {
  return Math.min(MAX_KHZ, Math.max(MIN_KHZ, Math.round(khz)));
}

export function mhzToKhz(mhz: number): number {
  return Math.round(mhz * 1000);
}

/**
 * Mirrors internal/voice/freq.go's KHz.MHz32 EXACTLY
 * (`float32(uint32(k)) / 1000.0`). Any other rounding here silently desyncs
 * from the value the server compares against.
 */
export function khzToMhz(khz: number): number {
  return Math.fround(khz / 1000);
}

/**
 * One rendered character. `place` is the power-of-ten kHz weight the character
 * carries (0 = 1 kHz, 3 = 1 MHz), or null for the decimal separator, which is
 * not a target.
 */
export interface FreqCell {
  char: string;
  place: number | null;
}

/**
 * Splits a kHz value into display cells. The MHz part is padded to MHZ_PAD so
 * a digit keeps its column as the value changes — otherwise the digit under
 * the cursor shifts out from under it mid-scroll.
 */
export function digitsOf(khz: number): FreqCell[] {
  const k = clampKhz(khz);
  const mhzPart = String(Math.floor(k / 1000)).padStart(MHZ_PAD, "0");
  const khzPart = String(k % 1000).padStart(3, "0");
  const cells: FreqCell[] = [];
  for (let i = 0; i < mhzPart.length; i++) {
    cells.push({ char: mhzPart[i], place: mhzPart.length - 1 - i + 3 });
  }
  cells.push({ char: ".", place: null });
  for (let i = 0; i < khzPart.length; i++) {
    cells.push({ char: khzPart[i], place: khzPart.length - 1 - i });
  }
  return cells;
}

/**
 * Steps one decade. Carries and borrows fall out of plain addition, which is
 * what "a digit passing 9 bumps the digit to its left" means.
 *
 * A step that would leave the wire range is REFUSED, not clamped: clamping the
 * 100 MHz digit to MAX_KHZ would rewrite six digits the user never touched and
 * leave the radio on a frequency nobody is listening to.
 */
export function stepDigit(khz: number, place: number, dir: 1 | -1): number {
  const next = clampKhz(khz) + dir * 10 ** place;
  if (next < MIN_KHZ || next > MAX_KHZ) return clampKhz(khz);
  return next;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/shared/freq.test.ts)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/freq.ts frontend/src/shared/freq.test.ts
git commit -m "feat(freq): integer-kHz digit arithmetic

digitsOf tags each character with its kHz decade so a digit can be targeted;
stepDigit adds or subtracts that decade, so carries fall out of arithmetic. A
step past either wire bound is refused rather than clamped — clamping would
rewrite digits the user never touched."
```

---

## Task 6: `LcdFreq` — per-digit editing

**Files:**
- Modify: `frontend/src/shared/components/LcdFreq.tsx`, `frontend/src/shared/styles/components.css`
- Test: `frontend/src/shared/components/LcdFreq.test.tsx`

**Interfaces:**
- Consumes: Task 5's `digitsOf`, `stepDigit`, `clampKhz`, `MAX_KHZ`.
- Produces: `interface LcdFreqProps { khz: number; digitPx?: number; unit?: boolean; className?: string; onChange?: (khz: number) => void }`
- **Breaking:** `value` (MHz) becomes `khz`, and `onChange` now receives kHz. Task 10 converts at the RadioCard boundary. The only current caller is `RadioCard`.

**The interaction contract:**
- Hover a digit and wheel → that decade steps. Click → up; shift-click → down.
- Each digit is a real `<button type="button" tabIndex={-1}>`, so click and Enter/Space are native and `typescript:S1082` does not apply.
- The LCD itself is the single tab stop (`role="spinbutton"`, `tabIndex={0}`). ArrowLeft/ArrowRight move a digit cursor, ArrowUp/ArrowDown step the digit under it. Seven tab stops per card would be unusable.
- Typed entry survives unchanged: digit keys build a draft, Enter commits, Escape and blur discard. **While a draft is open the per-digit handlers are inert** — a partially typed string has no stable decades — and the whole-LCD wheel keeps its existing ±1 kHz fallback.

- [ ] **Step 1: Write the failing tests**

Replace `frontend/src/shared/components/LcdFreq.test.tsx` with:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { LcdFreq } from "./LcdFreq";
import { MAX_KHZ } from "../freq";

const digits = () => screen.getAllByRole("button", { name: /digit/i });

describe("display", () => {
  it("renders the kHz value as padded digits", () => {
    render(<LcdFreq khz={118_500} />);
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("renders the MHZ unit only when asked", () => {
    const { rerender } = render(<LcdFreq khz={118_500} unit />);
    expect(screen.getByText("MHZ")).toBeInTheDocument();
    rerender(<LcdFreq khz={118_500} />);
    expect(screen.queryByText("MHZ")).toBeNull();
  });

  it("sizes the digits from digitPx", () => {
    render(<LcdFreq khz={118_500} digitPx={30} />);
    expect(screen.getByRole("group", { name: "frequency" })).toHaveStyle({ fontSize: "30px" });
  });

  it("is read-only with no onChange: no digit buttons, no spinbutton", () => {
    render(<LcdFreq khz={118_500} />);
    expect(screen.queryAllByRole("button")).toHaveLength(0);
    expect(screen.queryByRole("spinbutton")).toBeNull();
  });
});

describe("per-digit editing", () => {
  it("wheel over a digit steps that decade only", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    // Cells are "1","1","8",".","5","0","0"; digits()[3] is the "5" (100 kHz).
    fireEvent.wheel(digits()[3], { deltaY: -1 });
    expect(onChange).toHaveBeenCalledWith(118_600);
  });

  it("wheel down steps that decade down", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.wheel(digits()[2], { deltaY: 1 }); // the "8": 1 MHz decade
    expect(onChange).toHaveBeenCalledWith(117_500);
  });

  it("click bumps up and shift-click bumps down", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.click(digits()[5]); // last digit: 1 kHz
    expect(onChange).toHaveBeenLastCalledWith(118_501);
    fireEvent.click(digits()[5], { shiftKey: true });
    expect(onChange).toHaveBeenLastCalledWith(118_499);
  });

  it("does not select the card when a digit is clicked", () => {
    const onCardClick = vi.fn();
    render(
      <div onClick={onCardClick}>
        <LcdFreq khz={118_500} onChange={vi.fn()} />
      </div>,
    );
    fireEvent.click(digits()[5]);
    expect(onCardClick).not.toHaveBeenCalled();
  });

  // Review Focus #2, at the UI boundary.
  it("a refused step at the ceiling emits nothing rather than a rewritten value", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={MAX_KHZ} onChange={onChange} />);
    fireEvent.wheel(digits()[digits().length - 1], { deltaY: -1 });
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("keyboard", () => {
  it("is one tab stop carrying spinbutton semantics", () => {
    render(<LcdFreq khz={118_500} onChange={vi.fn()} />);
    const lcd = screen.getByRole("spinbutton", { name: "frequency" });
    expect(lcd).toHaveAttribute("tabindex", "0");
    for (const d of digits()) expect(d).toHaveAttribute("tabindex", "-1");
  });

  it("ArrowUp steps the digit under the cursor, which starts at 1 kHz", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.keyDown(screen.getByRole("spinbutton"), { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(118_501);
  });

  it("ArrowLeft moves the cursor one decade up before stepping", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "ArrowLeft" });
    fireEvent.keyDown(lcd, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(118_510);
  });

  it("ArrowLeft skips the separator", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    for (let i = 0; i < 3; i++) fireEvent.keyDown(lcd, { key: "ArrowLeft" });
    fireEvent.keyDown(lcd, { key: "ArrowUp" });
    expect(onChange).toHaveBeenCalledWith(119_500); // 1 MHz, not the ".".
  });

  it("marks the cursor digit so it is visible", () => {
    render(<LcdFreq khz={118_500} onChange={vi.fn()} />);
    fireEvent.keyDown(screen.getByRole("spinbutton"), { key: "ArrowLeft" });
    const cursored = digits().filter((d) => d.getAttribute("data-cursor") === "true");
    expect(cursored).toHaveLength(1);
    expect(cursored[0].textContent).toBe("0"); // the 10 kHz digit
  });
});

describe("typed entry still works", () => {
  it("builds a draft and commits on Enter", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    for (const k of ["1", "2", "3", "0", "0", "0"]) fireEvent.keyDown(lcd, { key: k });
    fireEvent.keyDown(lcd, { key: "Enter" });
    expect(onChange).toHaveBeenCalledWith(123_000);
  });

  it("Escape discards the draft", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "9" });
    fireEvent.keyDown(lcd, { key: "Escape" });
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("blur discards the draft", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    const lcd = screen.getByRole("spinbutton");
    fireEvent.keyDown(lcd, { key: "9" });
    fireEvent.blur(lcd);
    expect(onChange).not.toHaveBeenCalled();
  });

  it("per-digit stepping is inert while a draft is open", () => {
    const onChange = vi.fn();
    render(<LcdFreq khz={118_500} onChange={onChange} />);
    fireEvent.keyDown(screen.getByRole("spinbutton"), { key: "9" });
    fireEvent.click(screen.getAllByRole("button")[0]);
    expect(onChange).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/shared/components/LcdFreq.test.tsx)`
Expected: FAIL — the component takes `value`, renders no digit buttons and no `role="group"`.

- [ ] **Step 3: Rewrite the component**

Replace `frontend/src/shared/components/LcdFreq.tsx` with:

```tsx
import { useState, type KeyboardEvent, type MouseEvent, type WheelEvent } from "react";
import { MAX_KHZ, clampKhz, digitsOf, stepDigit } from "../freq";

interface LcdFreqProps {
  /** The canonical integer-kHz value. See shared/freq.ts. */
  khz: number;
  /** Digit size in px, supplied by the variant descriptor. */
  digitPx?: number;
  /** Render the "MHZ" suffix. The narrow variants drop it. */
  unit?: boolean;
  className?: string;
  /**
   * Opt-in editing. Omitted, the LCD is a read-only display with no focus stop
   * and no buttons. Supplied, it reports a new integer kHz value.
   */
  onChange?: (khz: number) => void;
}

/** Where the digit cursor starts: the 1 kHz decade. */
const INITIAL_PLACE = 0;

/**
 * LcdFreq renders an LCD-style frequency readout and, when editable, lets each
 * decade be changed on its own: hover a digit and wheel, or click it (shift to
 * go down). Keyboard users get ONE tab stop with spinbutton semantics —
 * ArrowLeft/ArrowRight move a digit cursor, ArrowUp/ArrowDown step the digit
 * under it — rather than a tab stop per digit, which would put seven stops on
 * every radio card.
 *
 * Digits are real <button>s, so click and Enter/Space are native behaviour and
 * typescript:S1082 does not apply. They carry tabIndex={-1} so only the LCD
 * itself is in the tab order.
 *
 * Typed entry is kept from the previous revision: digit keys build a draft,
 * Enter commits, Escape and blur discard. Blur is not a purposeful commit
 * gesture, and leaving a draft open would shadow a live server echo of the
 * value. While a draft is open the per-digit handlers are INERT — a partially
 * typed string has no stable decades to target — and the whole-LCD wheel keeps
 * its ±1 kHz fallback.
 *
 * Every value it emits is an integer kHz inside the 24-bit wire range; the MHz
 * float the DTO carries is derived by the caller at the boundary.
 */
export function LcdFreq({ khz, digitPx, unit, className, onChange }: LcdFreqProps) {
  const [draft, setDraft] = useState<string | null>(null);
  const [place, setPlace] = useState(INITIAL_PLACE);

  const editable = Boolean(onChange);
  const cells =
    draft === null
      ? digitsOf(khz)
      : draft.split("").map((c) => ({ char: c, place: null as number | null }));
  const places = cells.filter((c) => c.place !== null).map((c) => c.place as number);

  function step(p: number, dir: 1 | -1) {
    if (!onChange || draft !== null) return;
    const next = stepDigit(khz, p, dir);
    setPlace(p);
    if (next !== clampKhz(khz)) onChange(next);
  }

  function onDigitWheel(e: WheelEvent<HTMLButtonElement>, p: number) {
    if (!editable || draft !== null) return;
    e.preventDefault();
    e.stopPropagation(); // do not also run the whole-LCD fallback
    step(p, e.deltaY < 0 ? 1 : -1);
  }

  function onDigitClick(e: MouseEvent<HTMLButtonElement>, p: number) {
    if (!editable) return;
    e.stopPropagation(); // a digit click must not select the card
    step(p, e.shiftKey ? -1 : 1);
  }

  function moveCursor(dir: -1 | 1) {
    // `places` runs most-significant first, so ArrowLeft (dir -1) moves toward
    // the LARGER decade, i.e. one index earlier. The separator is not in
    // `places` at all, so it is skipped for free.
    const i = places.indexOf(place);
    const from = i < 0 ? places.length - 1 : i;
    const next = places[Math.min(places.length - 1, Math.max(0, from + dir))];
    if (next !== undefined) setPlace(next);
  }

  function commitDraft() {
    if (draft === null) return;
    const value = draft === "" ? 0 : Number.parseInt(draft, 10);
    onChange?.(clampKhz(value));
    setDraft(null);
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (!editable) return;
    if (/^[0-9]$/.test(e.key)) {
      e.preventDefault();
      setDraft((prev) => (prev ?? "") + e.key);
      return;
    }
    switch (e.key) {
      case "Backspace":
        e.preventDefault();
        setDraft((prev) => (prev ? prev.slice(0, -1) : prev));
        return;
      case "Enter":
        e.preventDefault();
        commitDraft();
        return;
      case "Escape":
        e.preventDefault();
        setDraft(null);
        return;
      case "ArrowLeft":
        e.preventDefault();
        moveCursor(-1);
        return;
      case "ArrowRight":
        e.preventDefault();
        moveCursor(1);
        return;
      case "ArrowUp":
        e.preventDefault();
        step(place, 1);
        return;
      case "ArrowDown":
        e.preventDefault();
        step(place, -1);
    }
  }

  /** The pre-existing whole-LCD fallback, live only while a draft is open. */
  function onLcdWheel(e: WheelEvent<HTMLDivElement>) {
    if (!editable || draft === null) return;
    e.preventDefault();
    const base = Number.parseInt(draft || "0", 10);
    onChange?.(clampKhz(base + (e.deltaY < 0 ? 1 : -1)));
    setDraft(null);
  }

  return (
    <div
      className={`lcd-screen ${className ?? ""}`.trim()}
      tabIndex={editable ? 0 : undefined}
      role={editable ? "spinbutton" : undefined}
      aria-label={editable ? "frequency" : undefined}
      aria-valuenow={editable ? khz : undefined}
      aria-valuemin={editable ? 0 : undefined}
      aria-valuemax={editable ? MAX_KHZ : undefined}
      onKeyDown={onKeyDown}
      onBlur={() => setDraft(null)}
      onWheel={onLcdWheel}
    >
      <span
        className="lcd-digits"
        role="group"
        aria-label="frequency"
        style={digitPx ? { fontSize: digitPx, lineHeight: 1 } : undefined}
      >
        {cells.map((c, i) =>
          editable && c.place !== null ? (
            <button
              key={i}
              type="button"
              tabIndex={-1}
              className="lcd-digit"
              data-cursor={c.place === place}
              aria-label={`digit ${c.place}`}
              onWheel={(e) => onDigitWheel(e, c.place as number)}
              onClick={(e) => onDigitClick(e, c.place as number)}
            >
              {c.char}
            </button>
          ) : (
            <span key={i} className="lcd-digit">
              {c.char}
            </span>
          ),
        )}
        {unit && <span className="lcd-unit">MHZ</span>}
      </span>
    </div>
  );
}
```

- [ ] **Step 4: Style the digit button and the unit**

Append to `frontend/src/shared/styles/components.css`, next to the existing `.lcd-digit` rules:

```css
/* A digit is a <button> now (native click + Enter/Space, no S1082 shim), so
   the browser's control styling has to be undone before .lcd-digit's own look
   applies. */
button.lcd-digit {
  background: none;
  border: 0;
  padding: 0;
  margin: 0;
  font: inherit;
  color: inherit;
  cursor: ns-resize;
}
button.lcd-digit[data-cursor="true"] {
  background: color-mix(in srgb, var(--ac-lcd) 18%, transparent);
  border-radius: 2px;
}
.lcd-unit {
  font-size: 0.4em;
  letter-spacing: 0.12em;
  color: var(--tx-3);
  align-self: flex-end;
  padding-bottom: 0.2em;
  margin-left: 0.4em;
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/shared/components/LcdFreq.test.tsx)`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/components/LcdFreq.tsx frontend/src/shared/components/LcdFreq.test.tsx frontend/src/shared/styles/components.css
git commit -m "feat(lcd): per-digit frequency editing on integer kHz

Hover-and-scroll, click up, shift-click down. Digits are real buttons with
tabIndex -1, so click and Enter/Space are native and the LCD stays one tab stop
with spinbutton semantics and a moving digit cursor.

Props take kHz rather than MHz: the canonical integer stays canonical and the
caller derives the wire float. Typed entry is unchanged; per-digit handlers go
inert while a draft is open, because a partial string has no stable decades."
```

---

## Task 7: `RadioFrame` — the state language

**Files:**
- Create: `frontend/src/windows/comms/RadioFrame.tsx`
- Test: `frontend/src/windows/comms/RadioFrame.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `export interface RadioState { selected: boolean; receiving: boolean; transmitting: boolean; intercom: boolean; disabled: boolean }`
  - `export function frameAccent(s: RadioState): string`
  - `export function RadioFrame(props: RadioState & { w: number; h: number; label: string; onSelect: () => void; children: ReactNode })`

The frame is the only place the five states are expressed, so they read
identically at every size (design reference §4).

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/comms/RadioFrame.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { RadioFrame, frameAccent, type RadioState } from "./RadioFrame";

const base: RadioState = {
  selected: false,
  receiving: false,
  transmitting: false,
  intercom: false,
  disabled: false,
};

function show(s: Partial<RadioState> = {}, onSelect = vi.fn()) {
  render(
    <RadioFrame {...base} {...s} w={280} h={166} label="R01 Fleet Common" onSelect={onSelect}>
      <span>body</span>
    </RadioFrame>,
  );
  return screen.getByRole("option");
}

describe("state attributes", () => {
  it("is plain by default", () => {
    const f = show();
    for (const a of ["data-selected", "data-intercom", "data-disabled", "data-tx", "data-rx"]) {
      expect(f).toHaveAttribute(a, "false");
    }
  });

  it("marks each state independently", () => {
    expect(show({ selected: true })).toHaveAttribute("data-selected", "true");
    expect(show({ intercom: true })).toHaveAttribute("data-intercom", "true");
    expect(show({ disabled: true })).toHaveAttribute("data-disabled", "true");
    expect(show({ transmitting: true })).toHaveAttribute("data-tx", "true");
    expect(show({ receiving: true })).toHaveAttribute("data-rx", "true");
  });
});

describe("the accent", () => {
  it("uses different colour channels for selection and intercom, so both can show", () => {
    expect(frameAccent({ ...base, selected: true })).toBe("var(--ac-primary)");
    expect(frameAccent({ ...base, intercom: true })).toBe("var(--ac-warn)");
    // A selected intercom radio keeps its blue border AND its amber brackets;
    // the two never compete for the same pixel.
    expect(frameAccent({ ...base, selected: true, intercom: true })).toBe("var(--ac-warn)");
  });

  it("drains to a border token when disabled", () => {
    expect(frameAccent({ ...base, disabled: true, selected: true })).toBe("var(--bd-1)");
  });

  it("never names a token the stylesheet does not define", () => {
    const states = [base, { ...base, selected: true }, { ...base, intercom: true }, { ...base, disabled: true }];
    for (const s of states) expect(frameAccent(s)).not.toContain("--ac-danger");
  });
});

describe("intercom and disabled treatments", () => {
  it("draws an amber left edge for intercom", () => {
    expect(show({ intercom: true }).querySelector("[data-intercom-edge]")).not.toBeNull();
    expect(show().querySelector("[data-intercom-edge]")).toBeNull();
  });

  it("draws a hatch overlay when disabled", () => {
    expect(show({ disabled: true }).querySelector("[data-disabled-hatch]")).not.toBeNull();
    expect(show().querySelector("[data-disabled-hatch]")).toBeNull();
  });

  it("does not let the hatch swallow clicks", () => {
    expect(show({ disabled: true }).querySelector("[data-disabled-hatch]")).toHaveStyle({
      pointerEvents: "none",
    });
  });

  it("draws three corner brackets", () => {
    expect(show().querySelectorAll("[data-bracket]")).toHaveLength(3);
  });
});

describe("selection", () => {
  it("is an option carrying its own selected state and accessible name", () => {
    const f = show({ selected: true });
    expect(f).toHaveAttribute("aria-selected", "true");
    expect(f).toHaveAccessibleName("R01 Fleet Common");
  });

  it("selects on click", () => {
    const onSelect = vi.fn();
    fireEvent.click(show({}, onSelect));
    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("selects on Enter and Space from the frame itself", () => {
    const onSelect = vi.fn();
    const f = show({}, onSelect);
    fireEvent.keyDown(f, { key: "Enter" });
    fireEvent.keyDown(f, { key: " " });
    expect(onSelect).toHaveBeenCalledTimes(2);
  });

  it("ignores a key bubbled from a child control", () => {
    const onSelect = vi.fn();
    show({}, onSelect);
    fireEvent.keyDown(screen.getByText("body"), { key: " " });
    expect(onSelect).not.toHaveBeenCalled();
  });

  it("is still selectable when the radio is disabled", () => {
    // "Disabled" is the RADIO's enabled flag, not the card's interactivity:
    // the user has to be able to select a disabled radio to turn it back on.
    const onSelect = vi.fn();
    fireEvent.click(show({ disabled: true }, onSelect));
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});

describe("size", () => {
  it("takes its box from the variant", () => {
    expect(show()).toHaveStyle({ width: "280px", height: "166px" });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/RadioFrame.test.tsx)`
Expected: FAIL — cannot resolve `./RadioFrame`.

- [ ] **Step 3: Write the component**

Create `frontend/src/windows/comms/RadioFrame.tsx`:

```tsx
import type { KeyboardEvent, ReactNode } from "react";

export interface RadioState {
  selected: boolean;
  receiving: boolean;
  transmitting: boolean;
  intercom: boolean;
  /** The radio's enabled flag is off. NOT "this card is inert". */
  disabled: boolean;
}

interface Props extends RadioState {
  w: number;
  h: number;
  /** The card's accessible name — "R01 Fleet Common". */
  label: string;
  onSelect: () => void;
  children: ReactNode;
}

/**
 * The accent the corner brackets take.
 *
 * Intercom wins over selection deliberately: the two use DIFFERENT colour
 * channels (amber vs blue) so a selected intercom radio shows both — the blue
 * border from `selected`, the amber edge and brackets from `intercom` —
 * instead of one state hiding the other. This is what lets the narrow variants
 * drop the ICOM chip without losing the information.
 *
 * Disabled drains everything: a radio that is off should not compete for
 * attention with one that is live.
 */
export function frameAccent(s: RadioState): string {
  if (s.disabled) return "var(--bd-1)";
  if (s.intercom) return "var(--ac-warn)";
  if (s.transmitting || s.selected) return "var(--ac-primary)";
  return "var(--bd-2)";
}

const BRACKET = 10;

const CORNERS = [
  { k: "tl", o: 0.7, left: 0, top: 0, bl: true, bt: true },
  { k: "tr", o: 0.5, right: 0, top: 0, br: true, bt: true },
  { k: "bl", o: 0.5, left: 0, bottom: 0, bl: true, bb: true },
] as const;

/**
 * RadioFrame is the card's border, corner brackets, intercom edge, disabled
 * hatch and selection behaviour. Every variant renders inside one, so the five
 * states read identically at 150px and at 360px — see
 * design/vcs/radio-variants.md §4.
 *
 * It is the card's single tab stop and carries role="option" against the
 * grid's role="listbox". A key bubbled from a child control (the name input,
 * the LCD) is ignored, so typing a space in the name does not re-select.
 */
export function RadioFrame({
  w,
  h,
  label,
  onSelect,
  children,
  selected,
  receiving,
  transmitting,
  intercom,
  disabled,
}: Props) {
  const accent = frameAccent({ selected, receiving, transmitting, intercom, disabled });
  const border = disabled
    ? "var(--bd-1)"
    : selected || transmitting
      ? "var(--ac-primary)"
      : "var(--bd-2)";

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (e.target !== e.currentTarget) return;
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onSelect();
    }
  }

  return (
    <div
      className="radio"
      role="option"
      aria-label={label}
      aria-selected={selected}
      tabIndex={0}
      data-selected={selected}
      data-rx={receiving}
      data-tx={transmitting}
      data-intercom={intercom}
      data-disabled={disabled}
      onClick={onSelect}
      onKeyDown={onKeyDown}
      style={{
        position: "relative",
        width: w,
        height: h,
        boxSizing: "border-box",
        overflow: "hidden",
        border: `1px solid ${border}`,
        background: disabled
          ? "var(--bg-1)"
          : "linear-gradient(180deg, var(--bg-3), var(--bg-2))",
        boxShadow: transmitting
          ? "0 0 0 1px var(--ac-primary), 0 0 14px -4px var(--ac-primary)"
          : undefined,
        opacity: disabled ? 0.72 : 1,
        cursor: "pointer",
      }}
    >
      {intercom && (
        <span
          data-intercom-edge
          aria-hidden="true"
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            bottom: 0,
            width: 3,
            background: "var(--ac-warn)",
            pointerEvents: "none",
          }}
        />
      )}
      {disabled && (
        <span
          data-disabled-hatch
          aria-hidden="true"
          style={{
            position: "absolute",
            inset: 0,
            pointerEvents: "none",
            backgroundImage:
              "repeating-linear-gradient(135deg, transparent 0 7px, color-mix(in srgb, var(--bd-1) 55%, transparent) 7px 8px)",
          }}
        />
      )}
      {CORNERS.map((c) => (
        <span
          key={c.k}
          data-bracket={c.k}
          aria-hidden="true"
          style={{
            position: "absolute",
            width: BRACKET,
            height: BRACKET,
            pointerEvents: "none",
            opacity: c.o,
            left: "left" in c ? c.left : undefined,
            right: "right" in c ? c.right : undefined,
            top: "top" in c ? c.top : undefined,
            bottom: "bottom" in c ? c.bottom : undefined,
            borderLeft: "bl" in c ? `1px solid ${accent}` : undefined,
            borderRight: "br" in c ? `1px solid ${accent}` : undefined,
            borderTop: "bt" in c ? `1px solid ${accent}` : undefined,
            borderBottom: "bb" in c ? `1px solid ${accent}` : undefined,
          }}
        />
      ))}
      {children}
    </div>
  );
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/windows/comms/RadioFrame.test.tsx)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/windows/comms/RadioFrame.tsx frontend/src/windows/comms/RadioFrame.test.tsx
git commit -m "feat(comms): RadioFrame carries the five-state language

Selection, receive, transmit, intercom and disabled are expressed once, on the
frame, so they read identically at 150px and at 360px. Intercom and selection
use different colour channels on purpose — a selected intercom radio shows
both, which is what lets the narrow variants drop the ICOM chip.

Selecting a DISABLED radio still works: the flag is the radio's, not the
card's, and turning it back on means clicking it first."
```

---

## Task 8: `TalkerLine` and `PttIndicator`

**Files:**
- Create: `frontend/src/windows/comms/TalkerLine.tsx`, `frontend/src/windows/comms/PttIndicator.tsx`
- Test: `frontend/src/windows/comms/TalkerLine.test.tsx`, `frontend/src/windows/comms/PttIndicator.test.tsx`

**Interfaces:**
- Consumes: `VU` from `../../shared/components/VU`, `Icon` from `../../shared/components/Icon`; the descriptor's `ptt` field from Task 1.
- Produces:
  - `export function TalkerLine(p: { talker?: string | null; self?: boolean; level?: number; disabled: boolean })`
  - `export function PttIndicator(p: { w: number | "fill"; h: number; transmitting: boolean; showLabel: boolean })`

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/comms/TalkerLine.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { TalkerLine } from "./TalkerLine";

describe("TalkerLine", () => {
  // design/vcs/radio-variants.md §9: there is no live per-radio RX feed yet.
  it("says there is no traffic rather than inventing a talker", () => {
    render(<TalkerLine disabled={false} />);
    expect(screen.getByText("no traffic")).toBeInTheDocument();
    expect(screen.getByRole("meter")).toHaveAttribute("aria-valuenow", "0");
  });

  it("shows a receiving talker with the receive accent", () => {
    render(<TalkerLine disabled={false} talker="Dabble" level={0.5} />);
    expect(screen.getByText("Dabble")).toHaveStyle({ color: "var(--ac-ok)" });
    expect(screen.getByTestId("talker-marker")).toHaveTextContent("▶");
  });

  it("shows own transmission distinctly from someone else's", () => {
    render(<TalkerLine disabled={false} self level={0.5} />);
    expect(screen.getByText("you")).toHaveStyle({ color: "var(--ac-primary)" });
    expect(screen.getByTestId("talker-marker")).toHaveTextContent("▲");
  });

  it("prefers own transmission over an incoming talker", () => {
    render(<TalkerLine disabled={false} self talker="Dabble" level={0.5} />);
    expect(screen.getByText("you")).toBeInTheDocument();
    expect(screen.queryByText("Dabble")).toBeNull();
  });

  it("replaces the whole line with OFF when the radio is disabled", () => {
    render(<TalkerLine disabled talker="Dabble" level={0.9} />);
    expect(screen.getByText("OFF")).toBeInTheDocument();
    expect(screen.queryByText("Dabble")).toBeNull();
    expect(screen.queryByRole("meter")).toBeNull();
  });

  // Review Focus #3, at this level: a long talker name must ellipsis, not grow.
  it("truncates a long talker name rather than widening the line", () => {
    render(<TalkerLine disabled={false} talker={"A".repeat(120)} level={0.5} />);
    expect(screen.getByText("A".repeat(120))).toHaveStyle({
      overflow: "hidden",
      textOverflow: "ellipsis",
      whiteSpace: "nowrap",
    });
  });

  it("keeps the meter next to the name rather than pushing it to the edge", () => {
    // The flexible spacer must come AFTER the meter, so the group reads as one
    // thing. `flex: 1` on the name would push the meter to the far right.
    render(<TalkerLine disabled={false} talker="Dabble" level={0.5} />);
    expect(screen.getByText("Dabble")).not.toHaveStyle({ flex: "1" });
    expect(screen.getByTestId("talker-spacer")).toBeInTheDocument();
  });
});
```

Create `frontend/src/windows/comms/PttIndicator.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PttIndicator } from "./PttIndicator";

describe("PttIndicator", () => {
  it("is an inert indicator, not a control", () => {
    render(<PttIndicator w={96} h={28} transmitting={false} showLabel />);
    const b = screen.getByRole("button");
    expect(b).toBeDisabled();
    expect(b.getAttribute("title")).toContain("keybind");
  });

  it("takes its box from the descriptor", () => {
    render(<PttIndicator w={64} h={64} transmitting={false} showLabel={false} />);
    expect(screen.getByRole("button")).toHaveStyle({ width: "64px", height: "64px" });
  });

  it("spans the content width when the descriptor says fill", () => {
    render(<PttIndicator w="fill" h={26} transmitting={false} showLabel={false} />);
    expect(screen.getByRole("button")).toHaveStyle({ width: "100%" });
  });

  it("is keyed while transmitting", () => {
    const { rerender } = render(<PttIndicator w={96} h={28} transmitting={false} showLabel />);
    expect(screen.getByRole("button").className).not.toContain("keyed");
    rerender(<PttIndicator w={96} h={28} transmitting showLabel />);
    expect(screen.getByRole("button").className).toContain("keyed");
    expect(screen.getByRole("button")).toHaveTextContent("TRANSMIT");
  });

  it("drops the text but keeps an accessible name when the descriptor has no room", () => {
    render(<PttIndicator w={32} h={30} transmitting={false} showLabel={false} />);
    const b = screen.getByRole("button");
    expect(b).not.toHaveTextContent("PUSH-TO-TALK");
    expect(b).toHaveAccessibleName(/push-to-talk/i);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/TalkerLine.test.tsx src/windows/comms/PttIndicator.test.tsx)`
Expected: FAIL — neither module resolves.

- [ ] **Step 3: Write the components**

Create `frontend/src/windows/comms/TalkerLine.tsx`:

```tsx
import { VU } from "../../shared/components/VU";

interface Props {
  /** Who is being received. Null/undefined means nothing is coming in. */
  talker?: string | null;
  /** This is our own transmission. Takes precedence over `talker`. */
  self?: boolean;
  /** Normalised 0-1 level for the meter. */
  level?: number;
  disabled: boolean;
}

/**
 * The card's status line: state dot, who is talking, and the meter — ONE
 * group, with the flexible spacer AFTER the meter so the three read as a unit.
 * Putting `flex: 1` on the name instead pushes the meter to the far edge,
 * where it looks like an unrelated widget.
 *
 * There is no live per-radio RX feed yet (design/vcs/radio-variants.md §9), so
 * with no talker this honestly says "no traffic" at zero level. Nothing here
 * simulates activity.
 */
export function TalkerLine({ talker, self, level = 0, disabled }: Props) {
  if (disabled) {
    return (
      <div className="row acenter gap-2" style={{ minWidth: 0 }}>
        <span className="cap mono" style={{ color: "var(--tx-4)", letterSpacing: "0.16em" }}>
          OFF
        </span>
      </div>
    );
  }

  const active = self || Boolean(talker);
  const accent = self ? "var(--ac-primary)" : talker ? "var(--ac-ok)" : "var(--tx-4)";

  return (
    <div className="row acenter gap-2" style={{ minWidth: 0 }}>
      <span
        aria-hidden="true"
        style={{
          width: 6,
          height: 6,
          borderRadius: "50%",
          flexShrink: 0,
          background: accent,
          boxShadow: active ? `0 0 6px ${accent}` : undefined,
        }}
      />
      <span data-testid="talker-marker" aria-hidden="true" style={{ color: accent, fontSize: 9 }}>
        {self ? "▲" : "▶"}
      </span>
      <span
        className="mono"
        style={{
          color: accent,
          fontSize: 11,
          minWidth: 0,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {self ? "you" : (talker ?? "no traffic")}
      </span>
      <VU level={active ? level : 0} segs={6} aria-label="signal" />
      <span data-testid="talker-spacer" style={{ flex: 1 }} />
    </div>
  );
}
```

Create `frontend/src/windows/comms/PttIndicator.tsx`:

```tsx
import { Icon } from "../../shared/components/Icon";

interface Props {
  w: number | "fill";
  h: number;
  transmitting: boolean;
  /** The variant has room for the word as well as the icon. */
  showLabel: boolean;
}

const TITLE = "Push-to-talk is driven by your configured keybind, not this button";

/**
 * A live transmit INDICATOR, not a click-to-talk control — wiring a real
 * handler needs backend binding surface that does not exist. It stays a native
 * disabled <button>: one never dispatches `click`, so it cannot re-select the
 * card, it still restyles and re-texts as transmit state changes, and it gets
 * the browser's own "this does nothing" affordance.
 *
 * Its box comes from the variant descriptor, so the narrow variants shrink it
 * to an icon without needing a second component.
 */
export function PttIndicator({ w, h, transmitting, showLabel }: Props) {
  return (
    <button
      className={`ptt ${transmitting ? "keyed" : ""}`.trim()}
      type="button"
      aria-label={transmitting ? "transmitting" : "push-to-talk"}
      style={{
        width: w === "fill" ? "100%" : w,
        height: h,
        flexShrink: 0,
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: 6,
      }}
      title={TITLE}
      disabled
    >
      <Icon name="mic" size={showLabel ? 12 : 14} />
      {showLabel && (transmitting ? "TRANSMIT" : "PUSH-TO-TALK")}
    </button>
  );
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/windows/comms/TalkerLine.test.tsx src/windows/comms/PttIndicator.test.tsx)`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/windows/comms/TalkerLine.tsx frontend/src/windows/comms/TalkerLine.test.tsx frontend/src/windows/comms/PttIndicator.tsx frontend/src/windows/comms/PttIndicator.test.tsx
git commit -m "feat(comms): TalkerLine and PttIndicator

The talker line is one group — dot, name, meter — with the spacer after the
meter rather than flex on the name, which had pushed the meter to the far edge.
With no RX feed it says 'no traffic' at zero rather than faking activity, and a
disabled radio shows OFF instead.

The PTT takes its box from the descriptor and stays a disabled button: it is an
indicator, and a disabled button cannot re-select the card."
```

---

## Task 9: The two layout shells

**Files:**
- Create: `frontend/src/windows/comms/RadioColumn.tsx`, `frontend/src/windows/comms/RadioRow.tsx`, `frontend/src/windows/comms/shells.tsx`
- Test: `frontend/src/windows/comms/shells.test.tsx`

**Interfaces:**
- Consumes: Task 1's `RadioVariant`.
- Produces (all exported from `shells.tsx`, re-exporting the two components):
  - `export interface RadioShellProps { variant: RadioVariant; rid: ReactNode; name: ReactNode; lcd: ReactNode; talker: ReactNode; ptt: ReactNode; chips: ReactNode }`
  - `export const SHELLS: Record<RadioVariant["orientation"], (p: RadioShellProps) => JSX.Element>`

A shell arranges pieces it is handed; it builds none of them. That is what
keeps a new SIZE from touching anything but `variants.ts` — the pieces already
size themselves from the descriptor, and the shell reads only `orientation`,
`shows` and the paddings derived from them.

**Row geometry** (design reference §2 — reproduce exactly):
- `column` at `lcdPx >= 24` (VERTICAL): pad `14 12`, gap 9, rows `header / lcd / ptt-row / status`, status on a `border-top` when `shows.statusDivider`.
- `column` at `lcdPx < 24` (NARROW-V): pad 10, gap 7, rows `rid+name / lcd / talker / ptt`. The name is **left-aligned after the rid**, not centred.
- `row` with chips (HORIZONTAL): pad `14 12`, a column of `header / lcd / talker` at gap 8, PTT beside it, vertically centred.
- `row` without chips (NARROW-H): pad 9, `lcd | (name over talker) | ptt`, all vertically centred.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/comms/shells.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SHELLS, type RadioShellProps } from "./shells";
import { variantById, VARIANTS } from "./variants";

function pieces(variant = variantById("vertical")): RadioShellProps {
  return {
    variant,
    rid: <span data-testid="rid">R01</span>,
    name: <span data-testid="name">Fleet Common</span>,
    lcd: <span data-testid="lcd">118.500</span>,
    talker: <span data-testid="talker">no traffic</span>,
    ptt: <span data-testid="ptt">PTT</span>,
    chips: <span data-testid="chips">ON</span>,
  };
}

describe("every variant renders through a shell", () => {
  it("has a shell for each orientation in the registry", () => {
    for (const v of VARIANTS) expect(SHELLS[v.orientation]).toBeTypeOf("function");
  });

  it("renders every piece it is handed, at every variant", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const { unmount } = render(<Shell {...pieces(v)} />);
      for (const id of ["rid", "name", "lcd", "talker", "ptt"]) {
        expect(screen.getByTestId(id), `${v.id} dropped ${id}`).toBeInTheDocument();
      }
      unmount();
    }
  });

  it("shows chips only where the descriptor asks for them", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const { unmount } = render(<Shell {...pieces(v)} />);
      if (v.shows.chips === "none") {
        expect(screen.queryByTestId("chips"), `${v.id} should hide chips`).toBeNull();
      } else {
        expect(screen.getByTestId("chips"), `${v.id} should show chips`).toBeInTheDocument();
      }
      unmount();
    }
  });
});

describe("the column shell", () => {
  it("draws the status divider when the descriptor asks", () => {
    const Column = SHELLS.column;
    const { unmount } = render(<Column {...pieces(variantById("vertical"))} />);
    expect(screen.getByTestId("shell-status")).toHaveStyle({ borderTop: "1px solid var(--bd-1)" });
    unmount();
    render(<Column {...pieces(variantById("narrow-v"))} />);
    expect(screen.getByTestId("shell-status")).not.toHaveStyle({
      borderTop: "1px solid var(--bd-1)",
    });
  });

  it("left-aligns the narrow-v name beside the rid rather than centring it", () => {
    const Column = SHELLS.column;
    render(<Column {...pieces(variantById("narrow-v"))} />);
    const header = screen.getByTestId("shell-header");
    expect(header).toHaveStyle({ justifyContent: "flex-start" });
    // rid first, then the name slot (which wraps the name).
    const kids = within(header).getAllByTestId(/^(rid|shell-name-slot)$/);
    expect(kids.map((k) => k.getAttribute("data-testid"))).toEqual(["rid", "shell-name-slot"]);
  });
});

describe("the row shell", () => {
  it("puts the PTT beside the content column, not under it", () => {
    const Row = SHELLS.row;
    render(<Row {...pieces(variantById("horizontal"))} />);
    expect(screen.getByTestId("shell-outer")).toHaveStyle({
      flexDirection: "row",
      alignItems: "center",
    });
  });
});

// Review Focus #3: a name longer than the card must never push the LCD out.
describe("a name longer than the card", () => {
  const LONG = "Fleet Common Alpha Bravo Charlie Delta Echo Foxtrot Golf Hotel";

  it("truncates instead of growing, at every variant", () => {
    for (const v of VARIANTS) {
      const Shell = SHELLS[v.orientation];
      const p = pieces(v);
      const { unmount } = render(
        <Shell {...p} name={<span data-testid="name">{LONG}</span>} />,
      );
      const slot = screen.getByTestId("shell-name-slot");
      expect(slot, `${v.id} name slot must clip`).toHaveStyle({
        overflow: "hidden",
        minWidth: "0px",
      });
      // Its row must also allow the shrink, or the clip never takes effect.
      expect(screen.getByTestId("shell-header"), `${v.id} header must allow shrink`).toHaveStyle({
        minWidth: "0px",
      });
      // The LCD is still rendered and still a sibling, not pushed out.
      expect(screen.getByTestId("lcd"), `${v.id} lost its LCD`).toBeInTheDocument();
      unmount();
    }
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/shells.test.tsx)`
Expected: FAIL — cannot resolve `./shells`.

- [ ] **Step 3: Write the column shell**

Create `frontend/src/windows/comms/RadioColumn.tsx`:

```tsx
import type { RadioShellProps } from "./shells";

/** Below this LCD size a column is the narrow variant: tighter, no divider. */
const NARROW_LCD_PX = 24;

/**
 * The column shell: header, LCD, then status, stacked.
 *
 * Its height is the SUM of its rows at one uniform gap, with nothing pinned by
 * `margin-top: auto`. That rule is why the vertical card is 166px and not the
 * 260px an earlier draft came to — the difference was not spacing, it was
 * leftover space collecting in a single gap above a pinned status row. See
 * design/vcs/radio-variants.md §3.
 */
export function RadioColumn({ variant, rid, name, lcd, talker, ptt, chips }: RadioShellProps) {
  const narrow = variant.lcdPx < NARROW_LCD_PX;
  const gap = narrow ? 7 : 9;
  const pad = narrow ? "10px" : "14px 12px 12px";

  return (
    <div
      data-testid="shell-outer"
      style={{
        display: "flex",
        flexDirection: "column",
        gap,
        padding: pad,
        height: "100%",
        boxSizing: "border-box",
        minWidth: 0,
      }}
    >
      <div
        data-testid="shell-header"
        className="row acenter gap-3"
        style={{ justifyContent: "flex-start", minWidth: 0, flexShrink: 0 }}
      >
        {rid}
        <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
          {name}
        </span>
        {variant.shows.chips !== "none" && chips}
      </div>

      <div style={{ display: "flex", justifyContent: "center", minWidth: 0 }}>{lcd}</div>

      {narrow ? (
        <>
          <div data-testid="shell-status" style={{ minWidth: 0 }}>
            {talker}
          </div>
          <div style={{ display: "flex" }}>{ptt}</div>
        </>
      ) : (
        <>
          <div className="row acenter" style={{ justifyContent: "flex-end", minWidth: 0 }}>
            {ptt}
          </div>
          <div
            data-testid="shell-status"
            style={{
              minWidth: 0,
              paddingTop: 6,
              borderTop: variant.shows.statusDivider ? "1px solid var(--bd-1)" : undefined,
            }}
          >
            {talker}
          </div>
        </>
      )}
    </div>
  );
}
```

- [ ] **Step 4: Write the row shell**

Create `frontend/src/windows/comms/RadioRow.tsx`:

```tsx
import type { RadioShellProps } from "./shells";

/**
 * The row shell: a content column with the PTT beside it, vertically centred.
 * The square PTT sets the card's minimum height at HORIZONTAL; at NARROW-H the
 * LCD anchors the left, two text lines take the middle and the PTT anchors the
 * right (design/vcs/radio-variants.md §2).
 */
export function RadioRow({ variant, rid, name, lcd, talker, ptt, chips }: RadioShellProps) {
  const narrow = variant.shows.chips === "none";

  if (narrow) {
    return (
      <div
        data-testid="shell-outer"
        className="row acenter"
        style={{
          display: "flex",
          flexDirection: "row",
          alignItems: "center",
          gap: 10,
          padding: 9,
          height: "100%",
          boxSizing: "border-box",
          minWidth: 0,
        }}
      >
        {lcd}
        <div style={{ display: "flex", flexDirection: "column", gap: 3, flex: 1, minWidth: 0 }}>
          <div
            data-testid="shell-header"
            className="row acenter gap-2"
            style={{ minWidth: 0, justifyContent: "flex-start" }}
          >
            {rid}
            <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
              {name}
            </span>
          </div>
          <div data-testid="shell-status" style={{ minWidth: 0 }}>
            {talker}
          </div>
        </div>
        {ptt}
      </div>
    );
  }

  return (
    <div
      data-testid="shell-outer"
      style={{
        display: "flex",
        flexDirection: "row",
        alignItems: "center",
        gap: 12,
        padding: "14px 12px 12px",
        height: "100%",
        boxSizing: "border-box",
        minWidth: 0,
      }}
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 8, flex: 1, minWidth: 0 }}>
        <div
          data-testid="shell-header"
          className="row acenter gap-3"
          style={{ minWidth: 0, justifyContent: "flex-start" }}
        >
          {rid}
          <span data-testid="shell-name-slot" style={{ minWidth: 0, overflow: "hidden", flex: 1 }}>
            {name}
          </span>
          {chips}
        </div>
        {/* Left-aligned, not centred: the column's left edge is the card's
            reading line. */}
        <div style={{ display: "flex", alignSelf: "flex-start", minWidth: 0 }}>{lcd}</div>
        <div data-testid="shell-status" style={{ minWidth: 0 }}>
          {talker}
        </div>
      </div>
      {ptt}
    </div>
  );
}
```

- [ ] **Step 5: Write the shell registry**

Create `frontend/src/windows/comms/shells.tsx`:

```tsx
import type { ReactNode } from "react";
import type { RadioVariant } from "./variants";
import { RadioColumn } from "./RadioColumn";
import { RadioRow } from "./RadioRow";

/**
 * What a shell is handed. A shell ARRANGES pieces; it builds none of them, and
 * it reads only `orientation`, `shows` and the paddings those imply. That is
 * what keeps a new SIZE from touching anything outside variants.ts — the
 * pieces already size themselves from the descriptor.
 */
export interface RadioShellProps {
  variant: RadioVariant;
  rid: ReactNode;
  name: ReactNode;
  lcd: ReactNode;
  talker: ReactNode;
  ptt: ReactNode;
  chips: ReactNode;
}

/**
 * One shell per orientation. Adding a new SHAPE is a new shell plus a key
 * here; adding a new SIZE is neither.
 */
export const SHELLS: Record<RadioVariant["orientation"], (p: RadioShellProps) => JSX.Element> = {
  column: RadioColumn,
  row: RadioRow,
};

export { RadioColumn, RadioRow };
```

If `JSX.Element` is not in scope under this TS config, use
`import type { ReactElement } from "react"` and `(p: RadioShellProps) => ReactElement`.

- [ ] **Step 6: Run tests to verify they pass**

```bash
(cd frontend && npx vitest run src/windows/comms/shells.test.tsx)
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/windows/comms/RadioColumn.tsx frontend/src/windows/comms/RadioRow.tsx frontend/src/windows/comms/shells.tsx frontend/src/windows/comms/shells.test.tsx
git commit -m "feat(comms): column and row layout shells

A shell arranges pieces it is handed and reads only orientation, shows and the
paddings they imply, so a new variant SIZE touches nothing here. Heights are
the sum of their rows at one uniform gap, with nothing pinned by margin-top
auto — that pin was 39px of leftover in an earlier vertical draft.

Every name slot clips at min-width 0, so a name longer than the card
truncates instead of pushing the LCD out."
```

---

## Task 10: `RadioCard` — descriptor resolution and wiring

**Files:**
- Modify: `frontend/src/windows/comms/RadioCard.tsx`
- Test: `frontend/src/windows/comms/RadioCard.test.tsx`

**Interfaces:**
- Consumes: `variantById` (Task 1), `khzToMhz`/`mhzToKhz` (Task 5), `LcdFreq` (Task 6), `RadioFrame` (Task 7), `TalkerLine`/`PttIndicator` (Task 8), `SHELLS` (Task 9).
- Produces: `interface Props { radio: RadioDTO; allRadios: RadioDTO[]; muted: boolean; variantId: string }`

**Behaviour that must not change:** edits are write-through via
`api.updateRadioInfo` and the store is NOT updated optimistically — the
server's `state:radio_update` echo is the source of truth. Selection IS
optimistic (`App.SelectRadio` has no echo). Transmit state uses the FROZEN
press-time `globalPttTargetId`, not live `selected`.

**What changes:** the permanent bordered `<input>` becomes a label that turns
into an input on double-click (design reference §9), the chips replace the two
`ENABLED`/`INTERCOM` toggle rows, and everything is sized by the descriptor.

- [ ] **Step 1: Write the failing tests**

Replace `frontend/src/windows/comms/RadioCard.test.tsx` with the cases below,
**keeping any existing test that covers write-through, selection optimism or
frozen-target transmit state** — port those forward rather than dropping them.

```tsx
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { RadioCard } from "./RadioCard";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";

const radio: RadioDTO = {
  id: 1,
  name: "Fleet Common",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
} as RadioDTO;

beforeEach(() => {
  vi.restoreAllMocks();
  useRadios.setState({ selectedRadioId: 0, heldPTT: new Set(), globalPttTargetId: 0 });
});

const show = (r: Partial<RadioDTO> = {}, variantId = "vertical") => {
  const full = { ...radio, ...r };
  render(<RadioCard radio={full} allRadios={[full]} muted={false} variantId={variantId} />);
  return full;
};

describe("variant resolution", () => {
  it("takes its box from the named variant", () => {
    show({}, "narrow-v");
    expect(screen.getByRole("option")).toHaveStyle({ width: "150px", height: "124px" });
  });

  // Review Focus #1, end to end.
  it("falls back to the default variant for an id no descriptor defines", () => {
    show({}, "dial-round");
    expect(screen.getByRole("option")).toHaveStyle({ width: "280px", height: "166px" });
  });

  it("shows the MHZ unit and both chips only on the variants that ask", () => {
    show({}, "vertical");
    expect(screen.getByText("MHZ")).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    expect(screen.getByRole("switch", { name: /intercom/i })).toBeInTheDocument();
  });

  it("shows only the enabled chip on horizontal", () => {
    show({}, "horizontal");
    expect(screen.getByRole("switch", { name: /enabled/i })).toBeInTheDocument();
    expect(screen.queryByRole("switch", { name: /intercom/i })).toBeNull();
  });

  it("shows no chips and no unit on the narrow variants", () => {
    show({}, "narrow-h");
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByText("MHZ")).toBeNull();
  });
});

describe("the frequency boundary", () => {
  it("renders the DTO's MHz float as integer kHz digits", () => {
    show({ frequency: 118.5 });
    expect(screen.getByRole("group", { name: "frequency" }).textContent).toBe("118.500");
  });

  it("converts an edited kHz value back to the wire float on commit", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const full = show({ frequency: 118.5 });
    const digits = screen.getAllByRole("button", { name: /digit/i });
    fireEvent.wheel(digits[3], { deltaY: -1 }); // 100 kHz up -> 118_600 kHz
    expect(spy).toHaveBeenCalledTimes(1);
    const sent = spy.mock.calls[0][0].radios.find((r) => r.id === full.id)!;
    // Exactly the expression internal/voice/freq.go uses; the server compares
    // this with ==.
    expect(sent.frequency).toBe(Math.fround(118_600 / 1000));
  });
});

describe("the name", () => {
  it("is a label, not a permanent input", () => {
    show();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
  });

  it("becomes an input on double-click and commits on Enter", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    const input = screen.getByRole("textbox", { name: /radio name/i });
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(spy.mock.calls[0][0].radios[0].name).toBe("Ops");
  });

  it("reverts on Escape without committing", () => {
    const spy = vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    const input = screen.getByRole("textbox", { name: /radio name/i });
    fireEvent.change(input, { target: { value: "Ops" } });
    fireEvent.keyDown(input, { key: "Escape" });
    expect(spy).not.toHaveBeenCalled();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
  });

  it("does not select the card while the name is being edited", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.doubleClick(screen.getByText("Fleet Common"));
    fireEvent.click(screen.getByRole("textbox", { name: /radio name/i }));
    expect(spy).not.toHaveBeenCalled();
  });
});

describe("state", () => {
  it("marks the card disabled when the radio is off", () => {
    show({ enabled: false });
    expect(screen.getByRole("option")).toHaveAttribute("data-disabled", "true");
    expect(screen.getByText("OFF")).toBeInTheDocument();
  });

  it("marks intercom on the frame even where no chip is shown", () => {
    show({ is_intercom: true }, "narrow-v");
    expect(screen.getByRole("option")).toHaveAttribute("data-intercom", "true");
    expect(screen.queryByRole("switch")).toBeNull();
  });

  it("uses the frozen press-time target for global PTT, not live selection", () => {
    useRadios.setState({
      selectedRadioId: 2,
      globalPttTargetId: 1,
      heldPTT: new Set(["global.ptt"]),
    });
    show();
    expect(screen.getByRole("option")).toHaveAttribute("data-tx", "true");
  });

  it("marks transmit for this radio's own PTT action", () => {
    useRadios.setState({ heldPTT: new Set(["radio.1.ptt"]), globalPttTargetId: 0 });
    show();
    expect(screen.getByRole("option")).toHaveAttribute("data-tx", "true");
  });

  it("selects optimistically and tells the backend", () => {
    const spy = vi.spyOn(api, "selectRadio").mockResolvedValue(undefined as never);
    show();
    fireEvent.click(screen.getByRole("option"));
    expect(useRadios.getState().selectedRadioId).toBe(1);
    expect(spy).toHaveBeenCalledWith(1);
  });

  it("does not update the radios store optimistically on an edit", () => {
    vi.spyOn(api, "updateRadioInfo").mockResolvedValue(undefined as never);
    const before = useRadios.getState().radios;
    show();
    fireEvent.click(screen.getByRole("switch", { name: /enabled/i }));
    expect(useRadios.getState().radios).toBe(before);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/RadioCard.test.tsx)`
Expected: FAIL — `RadioCard` takes no `variantId`, renders a permanent textbox and no `role="option"` frame of the right size.

- [ ] **Step 3: Rewrite the component**

Replace `frontend/src/windows/comms/RadioCard.tsx` with:

```tsx
import { useEffect, useState } from "react";
import { api, type RadioDTO } from "../../shared/api/client";
import { useRadios } from "../../shared/store/radios";
import { LcdFreq } from "../../shared/components/LcdFreq";
import { Toggle } from "../../shared/components/Toggle";
import { khzToMhz, mhzToKhz } from "../../shared/freq";
import { variantById } from "./variants";
import { SHELLS } from "./shells";
import { RadioFrame } from "./RadioFrame";
import { TalkerLine } from "./TalkerLine";
import { PttIndicator } from "./PttIndicator";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
  /** The stored variant id. An unknown one resolves to the default. */
  variantId: string;
}

/**
 * RadioCard resolves a variant descriptor, builds the card's pieces, and hands
 * them to the shell for that descriptor's orientation. It holds no geometry of
 * its own: adding a variant SIZE requires no change here.
 *
 * Editing is write-through: the edited radio is merged into `allRadios` to
 * build a full RadioInfoDTO and sent via api.updateRadioInfo. The store is NOT
 * updated optimistically — the server's `state:radio_update` echo is the
 * single source of truth for radio fields.
 *
 * Selection is different: a.st.SelectedRadio has no server echo (see
 * App.SelectRadio), so it IS applied optimistically to the shared store.
 *
 * Transmit state is derived: this radio's own PTT action held, or `global.ptt`
 * held with this radio as the FROZEN press-time target. Using live `selected`
 * there would let re-selecting mid-transmission retarget the indicator while
 * the backend keeps transmitting on whatever was selected at key-down.
 *
 * The frequency crosses one boundary here and nowhere else: the DTO carries an
 * MHz float, the LCD works in canonical integer kHz.
 */
export function RadioCard({ radio, allRadios, muted, variantId }: Props) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(radio.name);
  const selectedRadioId = useRadios((s) => s.selectedRadioId);
  const heldPTT = useRadios((s) => s.heldPTT);
  const globalPttTargetId = useRadios((s) => s.globalPttTargetId);

  // Re-sync the draft when the upstream name changes (e.g. a server echo).
  useEffect(() => setName(radio.name), [radio.name]);

  const variant = variantById(variantId);
  const Shell = SHELLS[variant.orientation];

  function commit(next: RadioDTO) {
    void api.updateRadioInfo({ muted, radios: allRadios.map((r) => (r.id === next.id ? next : r)) });
  }

  function select() {
    if (editing) return;
    useRadios.getState().setSelectedRadioId(radio.id);
    void api.selectRadio(radio.id);
  }

  const selected = selectedRadioId === radio.id;
  const transmitting =
    heldPTT.has(`radio.${radio.id}.ptt`) ||
    (heldPTT.has("global.ptt") && globalPttTargetId === radio.id);
  const disabled = !radio.enabled;
  const rid = `R${String(radio.id).padStart(2, "0")}`;

  const stop = (e: { stopPropagation: () => void }) => e.stopPropagation();

  return (
    <RadioFrame
      w={variant.w}
      h={variant.h}
      label={`${rid} ${radio.name}`}
      onSelect={select}
      selected={selected}
      receiving={false} /* no live per-radio RX feed yet — see the spec §7 */
      transmitting={transmitting}
      intercom={radio.is_intercom}
      disabled={disabled}
    >
      <Shell
        variant={variant}
        rid={
          <span
            className="cap mono"
            style={{
              color: radio.is_intercom ? "var(--ac-warn)" : "var(--ac-primary)",
              letterSpacing: "0.16em",
              flexShrink: 0,
            }}
          >
            {rid}
          </span>
        }
        name={
          editing ? (
            <input
              className="input"
              aria-label="radio name"
              autoFocus
              style={{ height: 20, fontSize: 12, padding: "0 6px", width: "100%" }}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onClick={stop}
              onBlur={() => {
                setEditing(false);
                setName(radio.name);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  setEditing(false);
                  commit({ ...radio, name });
                } else if (e.key === "Escape") {
                  e.preventDefault();
                  setName(radio.name);
                  setEditing(false);
                }
              }}
            />
          ) : (
            // A label, not a permanent bordered input: that input is the single
            // largest reason the shipped card read as a settings form.
            <span
              title="Double-click to rename"
              onDoubleClick={(e) => {
                stop(e);
                setEditing(true);
              }}
              style={{
                color: disabled ? "var(--tx-4)" : "var(--tx-0)",
                fontSize: 12,
                display: "block",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {radio.name}
            </span>
          )
        }
        lcd={
          <span onClick={stop} onDoubleClick={stop}>
            <LcdFreq
              khz={mhzToKhz(radio.frequency)}
              digitPx={variant.lcdPx}
              unit={variant.shows.unit}
              onChange={(khz) => commit({ ...radio, frequency: khzToMhz(khz) })}
            />
          </span>
        }
        talker={<TalkerLine disabled={disabled} self={transmitting} />}
        ptt={
          <PttIndicator
            w={variant.ptt.w}
            h={variant.ptt.h}
            transmitting={transmitting}
            showLabel={variant.ptt.w === "fill" || variant.ptt.w >= 90}
          />
        }
        chips={
          <span className="row acenter gap-2" onClick={stop} style={{ flexShrink: 0 }}>
            <Toggle
              on={radio.enabled}
              aria-label="enabled"
              onChange={() => commit({ ...radio, enabled: !radio.enabled })}
            />
            {variant.shows.chips === "full" && (
              <Toggle
                on={radio.is_intercom}
                aria-label="intercom"
                onChange={() => commit({ ...radio, is_intercom: !radio.is_intercom })}
              />
            )}
          </span>
        }
      />
    </RadioFrame>
  );
}
```

`Toggle` already renders `role="switch"` with `aria-checked` and forwards
`aria-label` (verified in `shared/components/Toggle.tsx`), so the chips are
reachable by role with no change to it.

- [ ] **Step 4: Run tests to verify they pass**

```bash
(cd frontend && npx vitest run src/windows/comms/RadioCard.test.tsx)
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/windows/comms/RadioCard.tsx frontend/src/windows/comms/RadioCard.test.tsx
git commit -m "feat(comms): RadioCard renders from a variant descriptor

Resolves the descriptor, builds the pieces, hands them to the shell for its
orientation. It holds no geometry, so a new variant size needs no change here.

The name becomes a label that opens on double-click — the permanent bordered
input was the single largest reason the card read as a settings form — and the
two toggle rows collapse to chips the descriptor can hide.

Write-through, non-optimistic radio edits and frozen-press-time transmit
targeting are unchanged; the MHz float crosses to canonical kHz in exactly one
place."
```

---

## Task 11: `RadioBlock` — snap resizing

**Files:**
- Modify: `frontend/src/windows/comms/RadioBlock.tsx`
- Test: `frontend/src/windows/comms/RadioBlock.test.tsx`

**Interfaces:**
- Consumes: `nearestVariant`, `variantById`, `VARIANTS` (Task 1); `RadioCard` (Task 10).
- Produces: `interface Props { radio: RadioDTO; allRadios: RadioDTO[]; muted: boolean; variantId: string; index: number; onResize: (radioId: number, variantId: string) => void; onReorder: (from: number, to: number) => void }`

**Why this is "snappier":** the drag no longer reports a size every
`pointermove` — it previews locally and reports **one** variant on
`pointerup`. That removes an IPC-debounced round trip per frame and means
the card never renders at an in-between size.

- [ ] **Step 1: Write the failing tests**

Replace the resize cases in `frontend/src/windows/comms/RadioBlock.test.tsx`
with the following, keeping the existing reorder and StrictMode cases:

```tsx
describe("snap resizing", () => {
  it("reports nothing while the pointer is still down", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -60 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("previews the variant the current drag would land on", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    // 280x166 dragged by (+80, -56) -> ~360x110 -> horizontal.
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    expect(screen.getByTestId("resize-preview")).toHaveTextContent("Horizontal");
  });

  it("commits exactly one variant on pointerup", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerMove(window, { clientX: 81, clientY: -56 });
    fireEvent.pointerUp(window);
    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize).toHaveBeenCalledWith(1, "horizontal");
  });

  it("clears the preview when the drag ends", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 80, clientY: -56 });
    fireEvent.pointerUp(window);
    expect(screen.queryByTestId("resize-preview")).toBeNull();
  });

  it("reports nothing when the drag lands back on the current variant", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "vertical", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 0, clientY: 0 });
    fireEvent.pointerMove(window, { clientX: 3, clientY: 2 });
    fireEvent.pointerUp(window);
    expect(onResize).not.toHaveBeenCalled();
  });

  it("sits the handle on the card's own corner, inside the block", () => {
    renderBlock({ variantId: "vertical", onResize: vi.fn() });
    const handle = screen.getByRole("button", { name: /resize/i });
    expect(handle).toHaveStyle({ position: "absolute", right: "0px", bottom: "0px" });
    // Inside the block's bounds, which are the card's bounds exactly.
    expect(screen.getByTestId("radio-block")).toHaveStyle({ width: "280px", height: "166px" });
  });
});

describe("keyboard resizing", () => {
  it("steps to the next larger variant and the next smaller", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-h", onResize });
    const handle = screen.getByRole("button", { name: /resize/i });
    // Variants ordered by area: narrow-v (18600), narrow-h (20400),
    // horizontal (39600), vertical (46480).
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onResize).toHaveBeenLastCalledWith(1, "horizontal");
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onResize).toHaveBeenLastCalledWith(1, "narrow-v");
  });

  it("stops at the ends rather than wrapping", () => {
    const onResize = vi.fn();
    renderBlock({ variantId: "narrow-v", onResize });
    fireEvent.keyDown(screen.getByRole("button", { name: /resize/i }), { key: "ArrowLeft" });
    expect(onResize).not.toHaveBeenCalled();
  });
});
```

Add the `renderBlock` helper at the top of the file:

```tsx
const radio = { id: 1, name: "Fleet Common", frequency: 118.5, enabled: true, is_intercom: false };

function renderBlock(p: { variantId: string; onResize: (id: number, v: string) => void }) {
  return render(
    <RadioBlock
      radio={radio as never}
      allRadios={[radio] as never}
      muted={false}
      variantId={p.variantId}
      index={0}
      onResize={p.onResize}
      onReorder={vi.fn()}
    />,
  );
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/RadioBlock.test.tsx)`
Expected: FAIL — the component still takes `width`/`height` and reports on every move.

- [ ] **Step 3: Rewrite the component**

Replace `frontend/src/windows/comms/RadioBlock.tsx` with:

```tsx
import { useEffect, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent, PointerEvent } from "react";
import type { RadioDTO } from "../../shared/api/client";
import { VARIANTS, nearestVariant, variantById } from "./variants";
import { RadioCard } from "./RadioCard";

interface Props {
  radio: RadioDTO;
  allRadios: RadioDTO[];
  muted: boolean;
  variantId: string;
  index: number;
  onResize: (radioId: number, variantId: string) => void;
  onReorder: (from: number, to: number) => void;
}

const DRAG_MIME = "text/plain";

/** Variants smallest-first, so an arrow key means "one size up/down". */
const BY_AREA = [...VARIANTS].sort((a, b) => a.w * a.h - b.w * b.h || a.id.localeCompare(b.id));

interface DragStart {
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * RadioBlock wraps a RadioCard and resizes it by SNAPPING to a variant.
 *
 * The drag reports nothing while the pointer is down: it previews locally and
 * commits exactly one variant on pointerup. That is what makes it snappy — no
 * IPC round trip per pointermove frame, and the card never renders at an
 * in-between size, because there is no such size.
 *
 * Window listeners are attached for exactly as long as a drag is active
 * (effect keyed on the drag origin) and the cleanup removes only the two this
 * effect added, so StrictMode's simulated unmount followed by the real one is
 * harmless.
 *
 * The handle sits on the card's own bottom-right corner, inside the block —
 * the block IS the card's box now, so there is no gap for it to float in.
 *
 * Reordering uses HTML5 drag-and-drop on the block body; the payload is the
 * source index and `onReorder(from, to)` fires on the target. There is
 * deliberately no keyboard path: RESET restores the stored order.
 */
export function RadioBlock({
  radio,
  allRadios,
  muted,
  variantId,
  index,
  onResize,
  onReorder,
}: Props) {
  const [drag, setDrag] = useState<DragStart | null>(null);
  const [previewId, setPreviewId] = useState<string | null>(null);

  const variant = variantById(variantId);

  // Latest values for the window listeners, so they attach once per drag.
  const latest = useRef({ radioId: radio.id, from: variant.id, onResize });
  latest.current = { radioId: radio.id, from: variant.id, onResize };
  const landed = useRef<string | null>(null);

  useEffect(() => {
    if (!drag) return;
    const move = (e: globalThis.PointerEvent) => {
      const v = nearestVariant(drag.w + (e.clientX - drag.x), drag.h + (e.clientY - drag.y));
      landed.current = v.id;
      setPreviewId(v.id);
    };
    const up = () => {
      const to = landed.current;
      landed.current = null;
      setPreviewId(null);
      setDrag(null);
      // A drag that ends on the variant it started from is not a change.
      if (to && to !== latest.current.from) latest.current.onResize(latest.current.radioId, to);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    return () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
  }, [drag]);

  function startResize(e: PointerEvent<HTMLSpanElement>) {
    e.preventDefault();
    e.stopPropagation();
    try {
      e.currentTarget.setPointerCapture?.(e.pointerId);
    } catch {
      /* capture is a nicety; the window listeners still deliver the drag */
    }
    landed.current = null;
    setDrag({ x: e.clientX, y: e.clientY, w: variant.w, h: variant.h });
  }

  function nudge(e: KeyboardEvent<HTMLSpanElement>) {
    let step = 0;
    if (e.key === "ArrowRight" || e.key === "ArrowDown") step = 1;
    else if (e.key === "ArrowLeft" || e.key === "ArrowUp") step = -1;
    else return;
    e.preventDefault();
    const i = BY_AREA.findIndex((v) => v.id === variant.id);
    const next = BY_AREA[i + step];
    if (next) onResize(radio.id, next.id); // the ends do not wrap
  }

  function onDragStart(e: DragEvent<HTMLDivElement>) {
    // A resize drag starts with a pointerdown inside this draggable element;
    // the browser would otherwise also begin an HTML5 drag of the whole block.
    if (drag) {
      e.preventDefault();
      return;
    }
    e.dataTransfer.setData(DRAG_MIME, String(index));
    e.dataTransfer.effectAllowed = "move";
  }

  function onDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault();
    const from = Number.parseInt(e.dataTransfer.getData(DRAG_MIME), 10);
    if (Number.isNaN(from) || from === index) return;
    onReorder(from, index);
  }

  const preview = previewId ? variantById(previewId) : null;

  return (
    <div
      data-testid="radio-block"
      draggable
      onDragStart={onDragStart}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
      }}
      onDrop={onDrop}
      style={{
        position: "relative",
        width: variant.w,
        height: variant.h,
        flexShrink: 0,
        boxSizing: "border-box",
      }}
    >
      <RadioCard radio={radio} allRadios={allRadios} muted={muted} variantId={variant.id} />
      {preview && (
        <span
          data-testid="resize-preview"
          style={{
            position: "absolute",
            left: 0,
            top: 0,
            width: preview.w,
            height: preview.h,
            pointerEvents: "none",
            border: "1px dashed var(--ac-primary)",
            background: "color-mix(in srgb, var(--ac-primary) 8%, transparent)",
            color: "var(--ac-primary)",
            fontSize: 10,
            letterSpacing: "0.14em",
            textTransform: "uppercase",
            padding: 4,
            zIndex: 2,
          }}
        >
          {preview.label}
        </span>
      )}
      <span
        role="button"
        tabIndex={0}
        aria-label="Resize radio block"
        title="Drag to snap to a size (arrow keys step through sizes)"
        onPointerDown={startResize}
        onKeyDown={nudge}
        style={{
          position: "absolute",
          right: 0,
          bottom: 0,
          width: 14,
          height: 14,
          cursor: "nwse-resize",
          touchAction: "none",
          zIndex: 3,
          borderRight: "2px solid var(--ac-primary)",
          borderBottom: "2px solid var(--ac-primary)",
        }}
      />
    </div>
  );
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `(cd frontend && npx vitest run src/windows/comms/RadioBlock.test.tsx)`
Expected: PASS, including the reorder and StrictMode cases carried over.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/windows/comms/RadioBlock.tsx frontend/src/windows/comms/RadioBlock.test.tsx
git commit -m "feat(comms): snap resizing to a variant

The drag previews locally and commits exactly ONE variant on pointerup instead
of reporting a size per pointermove frame — no IPC round trip per frame, and
the card never renders at an in-between size because there is no such size. A
drag that ends where it started reports nothing.

The handle moves onto the card's own corner: the block is the card's box now,
so there is no gap for it to float in. Arrow keys step through the sizes by
area and stop at the ends."
```

---

## Task 12: `CommsApp` — variant grid and chrome

**Files:**
- Modify: `frontend/src/windows/comms/CommsApp.tsx`, `frontend/src/shared/styles/components.css`
- Test: `frontend/src/windows/comms/CommsApp.test.tsx`, `frontend/src/windows/main/screens/Profiles.test.tsx`

**Interfaces:**
- Consumes: `DEFAULT_VARIANT_ID` (Task 1), `GRID_PAD`/`GRID_GAP` (Task 4), `RadioBlock` (Task 11).
- Produces: no new exports.

**The chrome bug, diagnosed:** `.popout-chrome` is
`grid-template-columns: auto 1fr auto` — three columns — but CommsApp gives it
**five** children (icon, title, profile name, dirty dot, `.ctrl`). The extras
flow onto implicit rows inside a 36px-tall `overflow: hidden` container, which
is the overlap. Separately, `.popout-chrome .ctrl button { width: 24px }`
forces the text buttons SAVE / REVERT / RESET to 24px wide.

Two fixes, both required:
1. Group the leading items into one element, so the grid gets exactly three children.
2. Exempt `.btn` from the 24px square rule.

- [ ] **Step 1: Write the failing tests**

Add to `frontend/src/windows/comms/CommsApp.test.tsx`:

```tsx
describe("the grid", () => {
  it("gives a radio with no stored block the default variant", async () => {
    // ...existing harness that mounts CommsApp with one radio and an empty layout
    expect(await screen.findByRole("option")).toHaveStyle({ width: "280px", height: "166px" });
  });

  it("renders a stored variant rather than the default", async () => {
    // ...harness with layout.blocks = [{ radio_id: 1, variant: "narrow-v" }]
    expect(await screen.findByRole("option")).toHaveStyle({ width: "150px", height: "124px" });
  });

  it("keeps every default card inside the popout's content width", async () => {
    // GRID_PAD is 12 a side, so a 540-wide popout has 516 of content. No
    // variant may be wider, or the card sits flush against the clip edge and
    // its border is cut — this was bug 2.
    for (const v of VARIANTS) expect(v.w).toBeLessThanOrEqual(540 - 2 * GRID_PAD);
  });

  it("persists a resize as a variant id", async () => {
    const spy = vi.spyOn(api, "setCommsLayout").mockResolvedValue(undefined as never);
    // ...mount, then:
    fireEvent.keyDown(await screen.findByRole("button", { name: /resize/i }), { key: "ArrowLeft" });
    await vi.waitFor(() => expect(spy).toHaveBeenCalled());
    const sent = spy.mock.calls.at(-1)![0];
    expect(sent.blocks[0]).toMatchObject({ radio_id: 1, variant: expect.any(String) });
    expect(sent.blocks[0]).not.toHaveProperty("w");
  });
});

describe("the chrome", () => {
  it("gives the three-column grid exactly three children", async () => {
    // ...mount with a dirty profile so SAVE/REVERT are present
    const chrome = document.querySelector(".popout-chrome")!;
    expect(chrome.children).toHaveLength(3);
  });

  it("keeps every control on one row", async () => {
    // ...mount with a dirty profile
    for (const label of ["SAVE", "REVERT", "RESET"]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    expect(screen.getByRole("button", { name: /close/i })).toBeInTheDocument();
  });
});
```

Add to `frontend/src/windows/main/screens/Profiles.test.tsx`:

```tsx
// Review Focus #5.
it("draws a profile with no blocks without NaN geometry", () => {
  // ...render the profile row with blocks: []
  const svg = document.querySelector("svg")!;
  expect(svg.querySelectorAll("rect")).toHaveLength(0);
  expect(svg.outerHTML).not.toContain("NaN");
});

it("draws one rect per block, sized from the variant", () => {
  // ...render with blocks [{radio_id:1,variant:"vertical"},{radio_id:2,variant:"narrow-v"}]
  const rects = document.querySelectorAll("svg rect");
  expect(rects).toHaveLength(2);
  expect(Number(rects[0].getAttribute("width"))).toBeGreaterThan(
    Number(rects[1].getAttribute("width")),
  );
});
```

Fill the elided harness lines from the mount helpers already in each file.

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/CommsApp.test.tsx src/windows/main/screens/Profiles.test.tsx)`
Expected: FAIL — `CommsApp` still imports `DEFAULT_BLOCK_W`, passes `width`/`height`, and the chrome has five children.

- [ ] **Step 3: Update `CommsApp`**

Three edits.

Replace the layout import:

```tsx
import { GRID_GAP, GRID_PAD } from "../../shared/layout";
import { DEFAULT_VARIANT_ID } from "./variants";
```

Replace the `shown` fallback and the `resize` handler:

```tsx
    for (const r of entry.radios) {
      if (!shown.some((x) => x.radio_id === r.id)) {
        shown.push({ radio_id: r.id, variant: DEFAULT_VARIANT_ID });
      }
    }
```

```tsx
  function resize(radioId: number, variant: string) {
    commit(withShown(shown.map((b) => (b.radio_id === radioId ? { ...b, variant } : b))));
  }
```

Replace the `RadioBlock` props:

```tsx
                <RadioBlock
                  key={r.id}
                  radio={r}
                  allRadios={entry.radios}
                  muted={entry.muted}
                  variantId={b.variant}
                  index={i}
                  onResize={resize}
                  onReorder={reorder}
                />
```

And group the chrome's leading items so the three-column grid gets three
children — the icon, title, profile name and dirty dot become one:

```tsx
      <div className="popout-chrome">
        <div className="row acenter gap-3" style={{ minWidth: 0 }}>
          <Icon name="broadcast" size={14} />
          <span className="ttl">Communications</span>
          {profile.active_name && (
            <span
              className="cap mono"
              style={{
                color: "var(--tx-3)",
                minWidth: 0,
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {profile.active_name}
            </span>
          )}
          {profile.dirty && (
            <span title="Unsaved layout changes" style={{ color: "var(--ac-primary)" }}>
              ●
            </span>
          )}
        </div>
        <span />
        <div className="ctrl">
          {/* ...SAVE / REVERT / RESET / close unchanged... */}
        </div>
      </div>
```

Leave the SAVE handler's `flush()` before `api.saveProfile()` exactly as it is:
an unsent drag has to be part of what gets saved.

- [ ] **Step 4: Fix the chrome button rule**

In `frontend/src/shared/styles/components.css`, after the existing
`.popout-chrome .ctrl button` rule:

```css
/* The 24px square above is for icon controls. A text button in the chrome
   (SAVE / REVERT / RESET) needs its own width, or the label is clipped to a
   24px box and the controls read as overlapping. */
.popout-chrome .ctrl button.btn {
  width: auto;
  height: 22px;
  padding: 0 8px;
}
.popout-chrome .ctrl {
  gap: 4px;
  align-items: center;
}
```

- [ ] **Step 5: Run the whole frontend suite**

```bash
(cd frontend && npx vitest run)
(cd frontend && npx tsc --noEmit)
(cd frontend && npm run build)
```
Expected: all tests pass, `tsc` reports 0 errors, the build succeeds. `tsc`
must now be clean — every file the earlier tasks left broken is fixed.

- [ ] **Step 6: Run the whole Go suite**

```bash
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...
PATH="/c/msys64/ucrt64/bin:$PATH" CGO_ENABLED=1 GOCACHE=$TMPDIR/vcs-gocache go test -race -count=1 -tags purego ./...
```
Expected: all green. `internal/voice` needs `dangerouslyDisableSandbox`.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/windows/comms/CommsApp.tsx frontend/src/windows/comms/CommsApp.test.tsx frontend/src/windows/main/screens/Profiles.test.tsx frontend/src/shared/styles/components.css
git commit -m "feat(comms): variant grid and a chrome that fits

Blocks carry a variant id end to end. No variant is wider than 360, so no card
reaches the popout's 516px content edge any more — that was bug 2.

The chrome was five children in a three-column grid, so the extras flowed onto
implicit rows inside a 36px overflow-hidden bar; they are now one group, and
the .ctrl 24px square rule is exempted for text buttons, which had been
clipping SAVE / REVERT / RESET to a 24px box."
```

---

## Manual verification

This branch is UI work. The automated suite cannot see any of it, so the
following go in the phase's manual-verification checklist rather than being
claimed as done:

1. Each of the four variants at its real size, on a real display — the roster
   was agreed against ASCII mockups and a browser companion, not the app.
2. Hover-and-scroll on a digit with a real wheel, including a trackpad's
   momentum scroll, which fires many small `deltaY` events per gesture and may
   need a threshold the unit tests cannot reveal.
3. A snap drag through all four variants, watching for flicker at the
   boundaries.
4. The intercom edge and the disabled hatch against the real dark background at
   each size.
5. Whether `no traffic` reads as honest or as broken to someone who does not
   know the RX feed is missing.

---

## Self-review notes

Checked against the spec and the design reference:

- **§1.1 bug 1** (detached handle) → Task 11. **Bug 2** (overflow) → Task 1's
  sizes plus Task 12's assertion that no variant exceeds the content width.
  **Bug 3** (chrome) → Task 12, with the cause diagnosed rather than guessed.
- **§2 / design ref §1–2**: the four descriptors and their row geometry →
  Tasks 1 and 9.
- **§3 descriptor shape** → Task 1, field for field.
- **§4 snap resolution** → Tasks 1 and 11.
- **§5.1 per-digit editing** → Tasks 5 and 6. **§5.2 state language** → Task 7.
  **§5.3 talker line**, **§5.4 PTT** → Task 8.
- **§6** → as above. **§7** (no RX feed) → Task 8 renders `no traffic`; Task 10
  passes `receiving={false}` with the reason inline.
- **§8** (supersedes Phase 7.3 §6) → Tasks 2, 3 and 4 remove `W`/`H`, the
  `MinBlockW/H` clamps and `clampBlock` rather than leaving them dead.
- **Design ref §9** (name as label, PTT as indicator, talker grouping,
  `no traffic`, narrow-V left alignment) → Tasks 8, 9 and 10.

The architectural invariant to hold at review: **adding a variant size must
touch `variants.ts` and nothing else.** Every other file reads the descriptor.
A reviewer can test this directly — add a fifth descriptor, confirm it appears
in the snap set and renders, then revert.
