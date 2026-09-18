# Joystick / Gamepad Keybinds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user bind any VCS action to a joystick/gamepad button or hat direction *in addition to* a keyboard chord, without taking exclusive ownership of the device away from Star Citizen.

**Architecture:** A new `internal/trigger` value package generalises "what activates an action" from `chord.Chord` to a `Trigger` sum type, and `keybinds.Store` moves from one chord per action to a list of triggers. A new `internal/joystick` package polls devices at 100 Hz behind a `Source` seam and feeds the *same* `hotkeys.Handler` the keyboard path already uses, so `internal/hotkeys` is not reopened. A per-action refcount in `internal/app` joins the two sources so holding one action from both cannot cut PTT.

**Tech Stack:** Go 1.25, BurntSushi/toml v1.6.0, vendored `gonutz/di8` (Windows DirectInput8, no cgo), `holoplot/go-evdev` (Linux, no cgo), React 19 + TypeScript + Zustand frontend.

**Spec:** [`docs/superpowers/specs/2026-09-18-joystick-gamepad-keybinds-design.md`](../specs/2026-09-18-joystick-gamepad-keybinds-design.md)

**Spike (background, not binding):** [`docs/superpowers/specs/2026-09-18-gamepad-joystick-bindings-spike.md`](../specs/2026-09-18-gamepad-joystick-bindings-spike.md)

## Global Constraints

Every task's requirements implicitly include this section. Values are copied verbatim from the spec.

1. **No SDL, on any platform.** SDL unconditionally takes `DISCL_EXCLUSIVE` on every DirectInput joystick it opens (spec D6).
2. **Windows cooperative level MUST be `SCL_NONEXCLUSIVE | SCL_BACKGROUND`.** A build requesting `SCL_EXCLUSIVE` anywhere is a defect (spec §8).
3. **`internal/chord` is unchanged.** It stays keyboard-only and stdlib-only (spec §3).
4. **`internal/hotkeys` behaviour is unchanged.** The keyboard path is not reopened. The only permitted change is additive wiring (spec §7).
5. **No cgo.** Both real backends are cgo-free (spec §8).
6. **macOS reports *unsupported*, never *denied*.** It is not something the user can fix (spec §8).
7. **`DeviceID` is constrained to `[A-Za-z0-9_.-]+`** so it can never contain the `:` or `+` field separators. Backends sanitise (spec §3).
8. **Existing `config.toml` files must round-trip byte-identical.** An action with exactly one trigger of kind key is written as a bare string (spec §6).
9. **Buttons 0..127; hats encoded `128 + hat*8 + dir`, hat 0..3, dir 0..7 clockwise from up** (spec §3).
10. **Keyboard logging stays action-ID-only.** Joystick edges DO log device and button identity; the Linux device filter must open only joystick-like devices, or this rule breaks (spec §11).
11. **Literal TOML fixtures, never symmetric round-trips alone,** for every persisted field. Phase 3 proved a symmetric round-trip cannot catch a mistyped struct tag (spec §12).
12. **`go test -race ./...` must pass.** The poll loop, manager and refcount are concurrent.
13. **Conventional commits.** End every commit message with `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

## File Structure

**New — pure value types (no OS, stdlib only):**
- `internal/trigger/trigger.go` — `Kind`, `Trigger`, `JoyBinding`, `JoyButton`, `DeviceID`, constructors
- `internal/trigger/button.go` — `Button`, hat encode/decode, display labels
- `internal/trigger/parse.go` — persisted-string grammar (`Parse` / `String`)

**New — joystick subsystem:**
- `internal/joystick/source.go` — `Source` seam, `Device`, `State`
- `internal/joystick/resolve.go` — satisfied/suppressed resolution (pure)
- `internal/joystick/manager.go` — poll loop, edge detection, hot-plug
- `internal/joystick/capture.go` — baseline/delta capture with modifier inference
- `internal/joystick/source_windows.go` / `source_linux.go` / `source_darwin.go` / `source_other.go`
- `internal/joystick/di8/` — vendored DirectInput8 wrapper (build-tagged `windows`)

**New — app layer:**
- `internal/app/presscount.go` — per-action press refcount

**Modified:**
- `internal/keybinds/store.go`, `actions.go` — `[]trigger.Trigger`
- `internal/config/config.go` — `KeybindValue` string-or-array
- `internal/app/dto.go`, `settings.go` — `TriggerDTO`, `AddTrigger`/`AddJoyTrigger`/`RemoveTrigger`
- `main.go` — wire the joystick manager
- `frontend/src/shared/store/settings.ts`, `shared/api/client.ts`
- `frontend/src/shared/components/TriggerChip.tsx` (new), `KeyChip.tsx` (kept, unchanged behaviour)
- `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`

## Verification note for cross-platform tasks

This plan is executed on macOS. Tasks 8 and 9 build Windows and Linux backends that **cannot be run here**. Their verification is:

```bash
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go build ./...
GOOS=darwin  GOARCH=arm64 go build ./...
```

plus unit tests of any pure helpers they contain. Do not claim runtime verification for these. Real-hardware verification is a separate gate recorded in spec §16 item 10.

---

## Task 1: `internal/trigger` value types and button encoding

**Files:**
- Create: `internal/trigger/trigger.go`
- Create: `internal/trigger/button.go`
- Test: `internal/trigger/button_test.go`

**Interfaces:**
- Consumes: `internal/chord` (`chord.Chord`) — unchanged.
- Produces: `trigger.Kind`, `trigger.KindKey`, `trigger.KindJoy`, `trigger.DeviceID`, `trigger.Button`, `trigger.JoyButton`, `trigger.JoyBinding`, `trigger.Trigger`, `trigger.Key(chord.Chord) Trigger`, `trigger.Joy(JoyBinding) Trigger`, `Trigger.IsZero() bool`, `Trigger.Equal(Trigger) bool`, `JoyBinding.Main() JoyButton`, `trigger.HatButton(hat, dir int) Button`, `Button.IsHat() bool`, `Button.Hat() (int, int)`, `Button.Label() string`, `trigger.MaxButton`, `trigger.HatCount`, `trigger.HatDirs`, `trigger.ValidDeviceID(string) bool`.

- [ ] **Step 1: Write the failing test**

Create `internal/trigger/button_test.go`:

```go
package trigger_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func TestHatButtonRoundTrip(t *testing.T) {
	for hat := 0; hat < trigger.HatCount; hat++ {
		for dir := 0; dir < trigger.HatDirs; dir++ {
			b := trigger.HatButton(hat, dir)
			if !b.IsHat() {
				t.Fatalf("HatButton(%d,%d) = %d, IsHat() = false, want true", hat, dir, b)
			}
			gotHat, gotDir := b.Hat()
			if gotHat != hat || gotDir != dir {
				t.Errorf("HatButton(%d,%d).Hat() = (%d,%d), want (%d,%d)",
					hat, dir, gotHat, gotDir, hat, dir)
			}
		}
	}
}

func TestPlainButtonIsNotHat(t *testing.T) {
	for _, b := range []trigger.Button{0, 1, 42, 127} {
		if b.IsHat() {
			t.Errorf("Button(%d).IsHat() = true, want false", b)
		}
	}
}

func TestHatEncodingIsContiguousFrom128(t *testing.T) {
	// Pins the on-the-wire encoding: 128 + hat*8 + dir. A change here
	// silently reinterprets every persisted hat binding.
	cases := map[trigger.Button][2]int{
		128: {0, 0},
		129: {0, 1},
		135: {0, 7},
		136: {1, 0},
		159: {3, 7},
	}
	for b, want := range cases {
		hat, dir := b.Hat()
		if hat != want[0] || dir != want[1] {
			t.Errorf("Button(%d).Hat() = (%d,%d), want (%d,%d)", b, hat, dir, want[0], want[1])
		}
		if got := trigger.HatButton(want[0], want[1]); got != b {
			t.Errorf("HatButton(%d,%d) = %d, want %d", want[0], want[1], got, b)
		}
	}
}

func TestButtonLabel(t *testing.T) {
	cases := []struct {
		b    trigger.Button
		want string
	}{
		{0, "Btn 1"},   // 0-indexed internally, 1-indexed for humans
		{11, "Btn 12"},
		{127, "Btn 128"},
		{trigger.HatButton(0, 0), "Hat 1 ↑"},
		{trigger.HatButton(0, 1), "Hat 1 ↗"},
		{trigger.HatButton(1, 2), "Hat 2 →"},
		{trigger.HatButton(3, 7), "Hat 4 ↖"},
	}
	for _, c := range cases {
		if got := c.b.Label(); got != c.want {
			t.Errorf("Button(%d).Label() = %q, want %q", c.b, got, c.want)
		}
	}
}

func TestValidDeviceID(t *testing.T) {
	valid := []string{"abc", "A-1_2.3", "6F1D2B70-D5A0-11CF", "x"}
	for _, s := range valid {
		if !trigger.ValidDeviceID(s) {
			t.Errorf("ValidDeviceID(%q) = false, want true", s)
		}
	}
	// ':' and '+' are the persisted-form field separators and MUST be rejected,
	// or a device id could forge a modifier or a field boundary.
	invalid := []string{"", "a:b", "a+b", "a b", "a/b", "ä"}
	for _, s := range invalid {
		if trigger.ValidDeviceID(s) {
			t.Errorf("ValidDeviceID(%q) = true, want false", s)
		}
	}
}

func TestTriggerEqual(t *testing.T) {
	mod := trigger.JoyButton{Device: "d1", Button: 5}
	a := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3, Modifier: &mod})
	// Distinct pointer, same value: Equal must compare the POINTEE, not the
	// pointer, or two loads of the same config compare unequal.
	mod2 := trigger.JoyButton{Device: "d1", Button: 5}
	b := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3, Modifier: &mod2})
	if !a.Equal(b) {
		t.Error("triggers with equal modifier values compare unequal")
	}
	c := trigger.Joy(trigger.JoyBinding{Device: "d1", Button: 3})
	if a.Equal(c) {
		t.Error("modifier and bare triggers compare equal")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/trigger/...`
Expected: FAIL — package `internal/trigger` does not exist.

- [ ] **Step 3: Write `internal/trigger/button.go`**

```go
package trigger

import "fmt"

// Button indexes one physical input on a device.
//
//	0..127   buttons
//	128..159 hat directions, encoded 128 + hat*8 + dir
//
// Encoding hats as high button indices (the DCS-SRS approach) means one
// edge-detection path and one capture path serve buttons and hats alike.
// This encoding is PERSISTED -- changing it silently reinterprets every
// stored hat binding.
type Button uint16

const (
	// MaxButton is the highest plain button index.
	MaxButton Button = 127
	// hatBase is the first hat-direction index.
	hatBase Button = 128
	// HatCount is how many hats a device may report (DIJOYSTATE2 has 4).
	HatCount = 4
	// HatDirs is how many directions a hat resolves to.
	HatDirs = 8
)

// hatArrows renders the 8 hat directions clockwise from up.
var hatArrows = [HatDirs]string{"↑", "↗", "→", "↘", "↓", "↙", "←", "↖"}

// HatButton encodes a hat direction as a Button. hat and dir are assumed in
// range; callers that take them from a device clamp first.
func HatButton(hat, dir int) Button {
	return hatBase + Button(hat*HatDirs+dir)
}

// IsHat reports whether b encodes a hat direction rather than a plain button.
func (b Button) IsHat() bool { return b >= hatBase }

// Hat decodes a hat-direction Button into its hat index and direction.
// Meaningless unless IsHat.
func (b Button) Hat() (hat, dir int) {
	n := int(b - hatBase)
	return n / HatDirs, n % HatDirs
}

// Label renders a human-readable name. Buttons and hats are 1-indexed for
// display because that is how every joystick vendor numbers them, while the
// wire encoding stays 0-indexed.
func (b Button) Label() string {
	if b.IsHat() {
		hat, dir := b.Hat()
		return fmt.Sprintf("Hat %d %s", hat+1, hatArrows[dir])
	}
	return fmt.Sprintf("Btn %d", int(b)+1)
}

// ValidDeviceID reports whether s is a legal DeviceID: one or more of
// [A-Za-z0-9_.-]. The excluded ':' and '+' are the persisted form's field
// separators (see parse.go), so allowing them would let a device id forge a
// field boundary or a modifier.
func ValidDeviceID(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Write `internal/trigger/trigger.go`**

```go
// Package trigger is the dependency-free value type for "what activates an
// action". It generalises internal/chord's keyboard-only Chord to a sum of a
// keyboard chord and a joystick binding, and owns the persisted string form
// of both.
//
// It deliberately imports nothing outside the standard library and
// internal/chord, so it is testable on any platform with no OS involvement.
package trigger

import "github.com/FPGSchiba/vcs-srs-client/internal/chord"

// Kind discriminates the Trigger sum.
type Kind uint8

const (
	// KindKey is a keyboard chord.
	KindKey Kind = iota
	// KindJoy is a joystick button or hat direction.
	KindJoy
)

// DeviceID is a backend-generated stable device identity. Constrained to
// [A-Za-z0-9_.-]+ (see ValidDeviceID) so it can never contain the ':' or '+'
// used as field separators in the persisted form. Backends sanitise.
type DeviceID string

// JoyButton names one physical input on one device.
type JoyButton struct {
	Device DeviceID
	Button Button
}

// JoyBinding is a button (or hat direction), optionally gated behind a
// modifier button. The modifier carries its own DeviceID because holding a
// throttle button to qualify a stick button is a normal HOTAS pattern.
type JoyBinding struct {
	Device   DeviceID
	Button   Button
	Modifier *JoyButton // nil = bare binding
}

// Main returns the binding's own button as a JoyButton.
func (b JoyBinding) Main() JoyButton {
	return JoyButton{Device: b.Device, Button: b.Button}
}

// Trigger is one way to activate an action.
type Trigger struct {
	Kind Kind
	Key  chord.Chord // valid when Kind == KindKey
	Joy  JoyBinding  // valid when Kind == KindJoy
}

// Key builds a keyboard trigger.
func Key(c chord.Chord) Trigger { return Trigger{Kind: KindKey, Key: c} }

// Joy builds a joystick trigger.
func Joy(b JoyBinding) Trigger { return Trigger{Kind: KindJoy, Joy: b} }

// IsZero reports whether the trigger is unset.
func (t Trigger) IsZero() bool {
	if t.Kind == KindKey {
		return t.Key.IsZero()
	}
	return t.Joy.Device == ""
}

// Equal compares by value. It exists because Trigger contains a pointer
// (JoyBinding.Modifier): == would compare pointer identity, so two loads of
// the same config would compare unequal and every conflict check would miss.
func (t Trigger) Equal(o Trigger) bool {
	if t.Kind != o.Kind {
		return false
	}
	if t.Kind == KindKey {
		return t.Key == o.Key
	}
	if t.Joy.Device != o.Joy.Device || t.Joy.Button != o.Joy.Button {
		return false
	}
	switch {
	case t.Joy.Modifier == nil && o.Joy.Modifier == nil:
		return true
	case t.Joy.Modifier == nil || o.Joy.Modifier == nil:
		return false
	default:
		return *t.Joy.Modifier == *o.Joy.Modifier
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/trigger/... -v`
Expected: PASS, all five tests.

- [ ] **Step 6: Prove `TestHatEncodingIsContiguousFrom128` actually bites**

Temporarily change `hatBase` to `129`, re-run, and confirm the test FAILS. Restore `128`. This is the Phase 3 discipline: a test that cannot fail is not a test.

- [ ] **Step 7: Commit**

```bash
git add internal/trigger/
git commit -m "$(cat <<'EOF'
feat(trigger): add Trigger value type with hat-aware button encoding

Generalises "what activates an action" from a keyboard-only chord.Chord
to a sum type. Hats encode as button indices 128+hat*8+dir so one edge
and capture path serves buttons and hats alike.

Equal compares the modifier POINTEE rather than the pointer: == would
make two loads of the same config compare unequal and every conflict
check would miss.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Persisted trigger-string grammar

**Files:**
- Create: `internal/trigger/parse.go`
- Test: `internal/trigger/parse_test.go`

**Interfaces:**
- Consumes: Task 1's `Trigger`, `JoyBinding`, `JoyButton`, `Button`, `HatButton`, `ValidDeviceID`; `chord.Parse`.
- Produces: `trigger.Parse(string) (Trigger, error)`, `Trigger.String() string`, and errors `trigger.ErrEmpty`, `ErrBadDevice`, `ErrBadInput`, `ErrBadModifier`.

**Grammar (spec §6), reproduced here because the implementer sees only this task:**

```
joy:[<ref>+]<ref>
<ref>   := <device-id>:<input>
<input> := btn<0-127> | hat<0-3>.<up|up_right|right|down_right|down|down_left|left|up_left>
```

Strip the `joy:` prefix, split on `+`. One part is a bare binding; **two parts mean the first is the modifier and the second the main input**. Each part splits on `:` into exactly two fields. A value without the `joy:` prefix is a keyboard chord, parsed by `chord.Parse`.

- [ ] **Step 1: Write the failing test**

Create `internal/trigger/parse_test.go`:

```go
package trigger_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func mustChord(t *testing.T, s string) chord.Chord {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", s, err)
	}
	return c
}

func TestParseRoundTrip(t *testing.T) {
	mod := trigger.JoyButton{Device: "throttle-a1", Button: 6}
	cases := []struct {
		s    string
		want trigger.Trigger
	}{
		{"V", trigger.Key(mustChord(t, "V"))},
		{"Ctrl+Alt+F1", trigger.Key(mustChord(t, "Ctrl+Alt+F1"))},
		{"joy:stick-c3:btn12", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 11})},
		{"joy:stick-c3:hat1.up", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: trigger.HatButton(0, 0)})},
		{"joy:stick-c3:hat4.up_left", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: trigger.HatButton(3, 7)})},
		// cross-device modifier: throttle button qualifies a stick button
		{"joy:throttle-a1:btn7+stick-c3:btn3", trigger.Joy(trigger.JoyBinding{
			Device: "stick-c3", Button: 2, Modifier: &mod})},
	}
	for _, c := range cases {
		got, err := trigger.Parse(c.s)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", c.s, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("Parse(%q) = %+v, want %+v", c.s, got, c.want)
		}
		if back := got.String(); back != c.s {
			t.Errorf("Parse(%q).String() = %q, want round trip", c.s, back)
		}
	}
}

func TestParseRejects(t *testing.T) {
	bad := []string{
		"",                              // empty
		"joy:",                          // no ref
		"joy:stick-c3",                  // no input
		"joy:stick-c3:btn0",             // buttons are 1-indexed in text
		"joy:stick-c3:btn129",           // above 128
		"joy:stick-c3:btn",              // no number
		"joy:stick-c3:hat0.up",          // hats are 1-indexed in text
		"joy:stick-c3:hat5.up",          // above HatCount
		"joy:stick-c3:hat1.sideways",    // not a direction
		"joy:stick c3:btn1",             // space is not a legal device id char
		"joy:a:btn1+b:btn2+c:btn3",      // only one modifier is supported
		"joy:stick-c3:wheel3",           // unknown input kind
	}
	for _, s := range bad {
		if got, err := trigger.Parse(s); err == nil {
			t.Errorf("Parse(%q) = %+v, want error", s, got)
		}
	}
}

func TestParseBareStringIsKeyboardChord(t *testing.T) {
	// Anything without the joy: prefix must go to chord.Parse, and its
	// failures must propagate rather than being swallowed into a joy binding.
	if _, err := trigger.Parse("NotAKey"); err == nil {
		t.Error("Parse(\"NotAKey\") = nil error, want chord parse failure")
	}
	got, err := trigger.Parse("Ctrl+E")
	if err != nil {
		t.Fatalf("Parse(\"Ctrl+E\"): %v", err)
	}
	if got.Kind != trigger.KindKey {
		t.Errorf("Parse(\"Ctrl+E\").Kind = %v, want KindKey", got.Kind)
	}
}

func TestStringOfZeroTriggerIsEmpty(t *testing.T) {
	if got := (trigger.Trigger{}).String(); got != "" {
		t.Errorf("zero Trigger.String() = %q, want \"\"", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/trigger/... -run TestParse`
Expected: FAIL — `trigger.Parse` undefined.

- [ ] **Step 3: Write `internal/trigger/parse.go`**

```go
package trigger

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// joyPrefix marks a persisted value as a joystick binding. Anything without
// it is a keyboard chord and goes to chord.Parse.
const joyPrefix = "joy:"

var (
	// ErrEmpty means the input string was empty.
	ErrEmpty = errors.New("trigger: empty")
	// ErrBadDevice means the device id was missing or contained an illegal character.
	ErrBadDevice = errors.New("trigger: bad device id")
	// ErrBadInput means the input part was not btn<n> or hat<n>.<dir>.
	ErrBadInput = errors.New("trigger: bad input")
	// ErrBadModifier means more than one modifier was supplied.
	ErrBadModifier = errors.New("trigger: at most one modifier is supported")
)

// hatDirNames maps the persisted direction names to their index, clockwise
// from up. Never reorder: these strings are written to config.toml.
var hatDirNames = [HatDirs]string{
	"up", "up_right", "right", "down_right", "down", "down_left", "left", "up_left",
}

// Parse reads a persisted trigger string. Values carrying the "joy:" prefix
// are joystick bindings; everything else is a keyboard chord.
func Parse(s string) (Trigger, error) {
	if strings.TrimSpace(s) == "" {
		return Trigger{}, ErrEmpty
	}
	if !strings.HasPrefix(s, joyPrefix) {
		c, err := chord.Parse(s)
		if err != nil {
			return Trigger{}, err
		}
		return Key(c), nil
	}

	parts := strings.Split(strings.TrimPrefix(s, joyPrefix), "+")
	if len(parts) > 2 {
		return Trigger{}, ErrBadModifier
	}

	main, err := parseRef(parts[len(parts)-1])
	if err != nil {
		return Trigger{}, err
	}
	b := JoyBinding{Device: main.Device, Button: main.Button}
	if len(parts) == 2 {
		mod, err := parseRef(parts[0])
		if err != nil {
			return Trigger{}, err
		}
		b.Modifier = &mod
	}
	return Joy(b), nil
}

// parseRef reads one "<device-id>:<input>" reference.
func parseRef(s string) (JoyButton, error) {
	dev, input, ok := strings.Cut(s, ":")
	if !ok {
		return JoyButton{}, ErrBadInput
	}
	if !ValidDeviceID(dev) {
		return JoyButton{}, fmt.Errorf("%w: %q", ErrBadDevice, dev)
	}
	btn, err := parseInput(input)
	if err != nil {
		return JoyButton{}, err
	}
	return JoyButton{Device: DeviceID(dev), Button: btn}, nil
}

// parseInput reads "btn<n>" or "hat<n>.<dir>". Both n values are 1-indexed in
// text and 0-indexed internally, matching Button.Label and how joystick
// vendors number their inputs.
func parseInput(s string) (Button, error) {
	switch {
	case strings.HasPrefix(s, "btn"):
		n, err := strconv.Atoi(strings.TrimPrefix(s, "btn"))
		if err != nil || n < 1 || n > int(MaxButton)+1 {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		return Button(n - 1), nil

	case strings.HasPrefix(s, "hat"):
		rest := strings.TrimPrefix(s, "hat")
		numStr, dirStr, ok := strings.Cut(rest, ".")
		if !ok {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		n, err := strconv.Atoi(numStr)
		if err != nil || n < 1 || n > HatCount {
			return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
		}
		for dir, name := range hatDirNames {
			if name == dirStr {
				return HatButton(n-1, dir), nil
			}
		}
		return 0, fmt.Errorf("%w: %q", ErrBadInput, s)

	default:
		return 0, fmt.Errorf("%w: %q", ErrBadInput, s)
	}
}

// String renders the persisted form. Zero triggers render "".
func (t Trigger) String() string {
	if t.IsZero() {
		return ""
	}
	if t.Kind == KindKey {
		return t.Key.String()
	}
	var b strings.Builder
	b.WriteString(joyPrefix)
	if t.Joy.Modifier != nil {
		b.WriteString(refString(*t.Joy.Modifier))
		b.WriteByte('+')
	}
	b.WriteString(refString(t.Joy.Main()))
	return b.String()
}

func refString(j JoyButton) string {
	return string(j.Device) + ":" + inputString(j.Button)
}

func inputString(b Button) string {
	if b.IsHat() {
		hat, dir := b.Hat()
		return fmt.Sprintf("hat%d.%s", hat+1, hatDirNames[dir])
	}
	return fmt.Sprintf("btn%d", int(b)+1)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/trigger/... -v`
Expected: PASS, all tests from Tasks 1 and 2.

- [ ] **Step 5: Prove the round-trip test bites**

Temporarily swap the modifier and main order in `Parse` (use `parts[0]` for main). Re-run: `TestParseRoundTrip` MUST fail on the cross-device case. Restore. Modifier-vs-main ordering is the single easiest thing to get backwards here, so confirm the test catches it.

- [ ] **Step 6: Commit**

```bash
git add internal/trigger/
git commit -m "$(cat <<'EOF'
feat(trigger): parse and render the persisted trigger grammar

joy:[<ref>+]<ref> where <ref> is <device-id>:<input>. Two parts mean the
FIRST is the modifier, so a throttle button can qualify a stick button.
Unambiguous because DeviceID excludes ':' and '+' by construction.

Buttons and hats are 1-indexed in text and 0-indexed internally, matching
how vendors number inputs and what Button.Label renders.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: `config.toml` string-or-array persistence

**Files:**
- Modify: `internal/config/config.go:31-34` (the `Keybinds` field) and `internal/config/config.go:50` (defaults)
- Test: `internal/config/config_test.go` (add cases; keep every existing one)

**Interfaces:**
- Consumes: nothing from earlier tasks (deliberately — `internal/config` must not import `internal/trigger`; it stays a raw-strings layer, exactly as it is a raw-chord-strings layer today).
- Produces: `config.KeybindValue []string` with `UnmarshalTOML(any) error` and `MarshalTOML() ([]byte, error)`; `Config.Keybinds map[string]KeybindValue`.

**This mechanism is already verified** against BurntSushi/toml v1.6.0: a single non-`joy:` entry re-encodes as a bare string, everything else as an array.

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestKeybindValueAcceptsStringOrArray(t *testing.T) {
	// Literal fixture, not a symmetric round trip: Phase 3 proved a
	// round-trip test stays green with a mistyped struct tag.
	const in = `
[keybinds]
global.push_to_mute = "V"
global.ptt = ["F1", "joy:throttle-a1:btn12"]
radio.1.ptt = ["joy:throttle-a1:btn7+stick-c3:btn3"]
`
	var got config.Config
	if _, err := toml.Decode(in, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v := got.Keybinds["global.push_to_mute"]; len(v) != 1 || v[0] != "V" {
		t.Errorf("scalar keybind = %v, want [V]", v)
	}
	if v := got.Keybinds["global.ptt"]; len(v) != 2 ||
		v[0] != "F1" || v[1] != "joy:throttle-a1:btn12" {
		t.Errorf("array keybind = %v, want [F1 joy:throttle-a1:btn12]", v)
	}
	if v := got.Keybinds["radio.1.ptt"]; len(v) != 1 ||
		v[0] != "joy:throttle-a1:btn7+stick-c3:btn3" {
		t.Errorf("modifier keybind = %v", v)
	}
}

func TestSingleKeyboardChordStaysAScalarOnDisk(t *testing.T) {
	// Global constraint 8: a user who never binds a joystick must never see
	// their config.toml change shape.
	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"global.push_to_mute": {"V"},
		"global.ptt":          {"F1", "joy:throttle-a1:btn12"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `global.push_to_mute = "V"`) {
		t.Errorf("single keyboard chord was not written as a scalar:\n%s", text)
	}
	if !strings.Contains(text, `global.ptt = ["F1", "joy:throttle-a1:btn12"]`) {
		t.Errorf("multi-trigger action was not written as an array:\n%s", text)
	}
}

func TestLoneJoyTriggerIsWrittenAsArray(t *testing.T) {
	// A single JOYSTICK trigger must still be an array: writing it bare
	// would be legal TOML but would make the file's shape depend on which
	// kind of trigger happens to be first, which is harder to reason about
	// than "keyboard-only files never change".
	cfg := config.Default()
	cfg.Keybinds = map[string]config.KeybindValue{
		"global.ptt": {"joy:throttle-a1:btn12"},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `global.ptt = ["joy:throttle-a1:btn12"]`) {
		t.Errorf("lone joy trigger not written as array:\n%s", raw)
	}
}

func TestKeybindValueRejectsNonString(t *testing.T) {
	const in = `
[keybinds]
global.ptt = [1, 2]
`
	var got config.Config
	if _, err := toml.Decode(in, &got); err == nil {
		t.Error("decode of numeric keybind entries = nil error, want failure")
	}
}
```

Ensure the test file imports `os`, `strings`, `path/filepath` and `github.com/BurntSushi/toml` if not already present.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL — `config.KeybindValue` undefined.

- [ ] **Step 3: Replace the `Keybinds` field in `internal/config/config.go`**

Replace lines 31-34 (the existing `Keybinds map[string]string` field and its comment) with:

```go
	// Keybinds is the raw action-ID -> trigger-string list. It is held raw
	// rather than typed so an unknown action ID written by a newer version
	// survives a load/save cycle. internal/keybinds owns interpretation, and
	// internal/trigger owns the string grammar -- this package deliberately
	// knows neither.
	Keybinds map[string]KeybindValue `toml:"keybinds"`

	// KeybindDevices maps a device id to its product name, purely for
	// display. It exists so a binding for a device that is NOT currently
	// attached can still say which device it wants -- without it, a chip for
	// an unplugged stick would render its raw id after a restart. Missing
	// entries are harmless: the UI falls back to the id and the binding
	// stays valid either way.
	KeybindDevices map[string]string `toml:"keybind_devices"`
```

and line 50's default to:

```go
		Keybinds:       map[string]KeybindValue{},
		KeybindDevices: map[string]string{},
```

- [ ] **Step 4: Add `KeybindValue` to `internal/config/config.go`**

```go
// KeybindValue is one action's trigger list on disk. It accepts a bare string
// or an array of strings on read, and writes a bare string only when the
// action has exactly one KEYBOARD trigger -- so a config.toml belonging to a
// user who never binds a joystick is rewritten byte-identical.
type KeybindValue []string

// joyPrefix duplicates internal/trigger's constant rather than importing it.
// internal/config stays a raw-strings layer with no knowledge of trigger
// semantics; this one prefix is the only thing it needs, and importing the
// trigger package to get it would invert the dependency.
const joyPrefix = "joy:"

// UnmarshalTOML accepts a string or an array of strings.
func (k *KeybindValue) UnmarshalTOML(data any) error {
	switch v := data.(type) {
	case string:
		*k = KeybindValue{v}
		return nil
	case []any:
		out := make(KeybindValue, 0, len(v))
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return fmt.Errorf("config: keybind entry is %T, want string", e)
			}
			out = append(out, s)
		}
		*k = out
		return nil
	default:
		return fmt.Errorf("config: keybind value is %T, want string or array of strings", data)
	}
}

// MarshalTOML writes the scalar form for a lone keyboard chord and the array
// form otherwise. See the type comment for why.
func (k KeybindValue) MarshalTOML() ([]byte, error) {
	if len(k) == 1 && !strings.HasPrefix(k[0], joyPrefix) {
		return []byte(strconv.Quote(k[0])), nil
	}
	parts := make([]string, len(k))
	for i, s := range k {
		parts[i] = strconv.Quote(s)
	}
	return []byte("[" + strings.Join(parts, ", ") + "]"), nil
}
```

Add `strconv` and `strings` to the file's imports if absent.

- [ ] **Step 5: Fix the existing tests that assume `map[string]string`**

`internal/config/config_test.go` currently uses `maps.Equal` on `Keybinds` and indexes it as strings (lines ~51, 65-66, 84-85, 102-103, 163-180, 249-267). Update each to the new type: `maps.Equal` becomes `maps.EqualFunc(a, b, slices.Equal)`, and `got.Keybinds["global.ptt"] != "F1"` becomes a length-plus-element check. **Do not delete any existing test** — every one of them is still load-bearing.

- [ ] **Step 6: Run the full config suite**

Run: `go test ./internal/config/... -v`
Expected: PASS, both the new tests and every pre-existing one.

- [ ] **Step 7: Prove the scalar-write test bites**

Temporarily change `MarshalTOML` to always emit the array form. Re-run: `TestSingleKeyboardChordStaysAScalarOnDisk` MUST fail. Restore. This is the test guarding global constraint 8.

- [ ] **Step 8: Commit**

```bash
git add internal/config/
git commit -m "$(cat <<'EOF'
feat(config): accept a string or an array per keybind

An action may now hold several triggers. Read takes either shape; write
emits a bare string only for a lone KEYBOARD chord, so a config.toml
belonging to a user who never binds a joystick round-trips unchanged.

internal/config stays a raw-strings layer: it duplicates the one "joy:"
prefix rather than importing internal/trigger, which would invert the
dependency.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: `keybinds.Store` holds a list of triggers

**Files:**
- Modify: `internal/keybinds/store.go` (whole file)
- Modify: `internal/keybinds/actions.go` (`Defaults` only)
- Test: `internal/keybinds/store_test.go`, `internal/keybinds/actions_test.go`

**Interfaces:**
- Consumes: `trigger.Trigger`, `trigger.Parse`, `Trigger.String()`, `Trigger.Equal()` (Tasks 1–2).
- Produces: `keybinds.Stolen{ActionID ActionID; Trigger trigger.Trigger}`; `Store.Load(map[string][]string)`, `Store.Snapshot() map[string][]string`, `Store.Get(ActionID) ([]trigger.Trigger, bool)`, `Store.All() map[ActionID][]trigger.Trigger`, `Store.Add(ActionID, trigger.Trigger) *Stolen`, `Store.RemoveAt(ActionID, int) error`, `Store.Clear(ActionID)`, `keybinds.ErrNoSuchTrigger`; `keybinds.Defaults() map[ActionID][]trigger.Trigger`.

**Note on conflict namespaces:** spec §10 requires keyboard and joystick triggers to occupy separate conflict namespaces. This falls out for free — `Trigger.Equal` compares `Kind` first, so a keyboard trigger can never equal a joystick one and can never steal from it. Do not add a separate namespace check; just do not break `Equal`.

**Note on the one-keyboard-trigger rule (spec §3):** an action holds **at most one** `KindKey` trigger and any number of `KindJoy` triggers; `Add` *replaces* an existing keyboard trigger instead of appending. This is load-bearing, not tidiness. `internal/hotkeys` keys both `Manager.Apply(map[string]Binding)` and `dispatcher.binds map[string]boundAction` by action ID, one entry each — so a second keyboard chord would save fine, display fine, and **never fire**, because `applyHotkeys` would overwrite the first with the second. Enforcing it here is what keeps global constraint 4 (do not reopen `internal/hotkeys`) intact.

- [ ] **Step 1: Write the failing test**

Replace the contents of `internal/keybinds/store_test.go` with (keeping the existing `TestIsPerRadioID` exactly as it is — it guards a bug already fixed once):

```go
package keybinds_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func key(t *testing.T, s string) trigger.Trigger {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("chord.Parse(%q): %v", s, err)
	}
	return trigger.Key(c)
}

func joy(dev string, btn trigger.Button) trigger.Trigger {
	return trigger.Joy(trigger.JoyBinding{Device: trigger.DeviceID(dev), Button: btn})
}

func TestAddAccumulatesTriggers(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))

	got, ok := s.Get("global.ptt")
	if !ok || len(got) != 2 {
		t.Fatalf("Get after two Adds = %v (ok=%v), want 2 triggers", got, ok)
	}
	if !got[0].Equal(key(t, "F1")) || !got[1].Equal(joy("stick-c3", 11)) {
		t.Errorf("triggers out of order or wrong: %+v", got)
	}
}

func TestAddIsIdempotentForTheSameTrigger(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	if stolen := s.Add("global.ptt", key(t, "F1")); stolen != nil {
		t.Errorf("re-adding an action's own trigger reported a steal: %+v", stolen)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 1 {
		t.Errorf("re-adding duplicated the trigger: %+v", got)
	}
}

func TestAddStealsFromAnotherAction(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	stolen := s.Add("channel.intercom", key(t, "F1"))
	if stolen == nil {
		t.Fatal("Add over another action's trigger reported no steal")
	}
	if stolen.ActionID != "global.ptt" {
		t.Errorf("stolen from %q, want global.ptt", stolen.ActionID)
	}
	if got, _ := s.Get("global.ptt"); len(got) != 0 {
		t.Errorf("victim kept the stolen trigger: %+v", got)
	}
}

func TestStealTakesOnlyTheConflictingTrigger(t *testing.T) {
	// The victim's OTHER bindings must survive -- this is the whole point of
	// the additive model.
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("channel.intercom", key(t, "F1"))

	got, _ := s.Get("global.ptt")
	if len(got) != 1 || !got[0].Equal(joy("stick-c3", 11)) {
		t.Errorf("steal took more than the conflicting trigger: %+v", got)
	}
}

func TestKeyboardNeverStealsFromJoystick(t *testing.T) {
	// Separate conflict namespaces (spec section 10). This works because
	// Trigger.Equal compares Kind first.
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	if stolen := s.Add("channel.intercom", key(t, "F1")); stolen != nil {
		t.Errorf("keyboard trigger stole from a joystick binding: %+v", stolen)
	}
}

func TestSecondKeyboardTriggerReplacesTheFirst(t *testing.T) {
	// internal/hotkeys registers one chord per action ID, so a second
	// keyboard chord would save, display, and never fire. Replace instead.
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", key(t, "F2"))

	got, _ := s.Get("global.ptt")
	if len(got) != 1 {
		t.Fatalf("Get = %+v, want exactly one keyboard trigger", got)
	}
	if !got[0].Equal(key(t, "F2")) {
		t.Errorf("kept %+v, want the newer chord F2", got[0])
	}
}

func TestKeyboardReplacementKeepsJoystickTriggers(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", key(t, "F2"))

	got, _ := s.Get("global.ptt")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want the joystick trigger plus one chord", got)
	}
	var joys, keysN int
	for _, tr := range got {
		if tr.Kind == trigger.KindJoy {
			joys++
		} else {
			keysN++
		}
	}
	if joys != 1 || keysN != 1 {
		t.Errorf("got %d joystick and %d keyboard triggers, want 1 and 1", joys, keysN)
	}
}

func TestSeveralJoystickTriggersAccumulate(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Add("global.ptt", joy("throttle-a1", 6))
	if got, _ := s.Get("global.ptt"); len(got) != 2 {
		t.Errorf("Get = %+v, want both joystick triggers", got)
	}
}

func TestRemoveAt(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))

	if err := s.RemoveAt("global.ptt", 0); err != nil {
		t.Fatalf("RemoveAt(0): %v", err)
	}
	got, _ := s.Get("global.ptt")
	if len(got) != 1 || !got[0].Equal(joy("stick-c3", 11)) {
		t.Errorf("after RemoveAt(0) = %+v, want the joystick trigger only", got)
	}
	for _, bad := range []int{-1, 1, 99} {
		if err := s.RemoveAt("global.ptt", bad); err == nil {
			t.Errorf("RemoveAt(%d) = nil error, want out-of-range failure", bad)
		}
	}
}

func TestLoadSnapshotRoundTrip(t *testing.T) {
	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt":       {"F1", "joy:stick-c3:btn12"},
		"channel.intercom": {"joy:throttle-a1:btn7+stick-c3:btn3"},
		"future.action":    {"Ctrl+Q"}, // unknown id: must survive verbatim
	})
	snap := s.Snapshot()
	if len(snap["global.ptt"]) != 2 ||
		snap["global.ptt"][0] != "F1" || snap["global.ptt"][1] != "joy:stick-c3:btn12" {
		t.Errorf("global.ptt round trip = %v", snap["global.ptt"])
	}
	if got := snap["future.action"]; len(got) != 1 || got[0] != "Ctrl+Q" {
		t.Errorf("unknown action id lost: %v", got)
	}
}

func TestLoadDropsOnlyTheUnparseableEntry(t *testing.T) {
	s := keybinds.New()
	s.Load(map[string][]string{
		"global.ptt": {"F1", "joy:!!!bad", "joy:stick-c3:btn12"},
	})
	got, _ := s.Get("global.ptt")
	if len(got) != 2 {
		t.Fatalf("Get = %+v, want the two parseable triggers", got)
	}
	if !got[0].Equal(key(t, "F1")) || !got[1].Equal(joy("stick-c3", 11)) {
		t.Errorf("wrong survivors: %+v", got)
	}
}

func TestClearRemovesEveryTrigger(t *testing.T) {
	s := keybinds.New()
	s.Add("global.ptt", key(t, "F1"))
	s.Add("global.ptt", joy("stick-c3", 11))
	s.Clear("global.ptt")
	if got, ok := s.Get("global.ptt"); ok && len(got) != 0 {
		t.Errorf("after Clear = %+v, want empty", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/keybinds/...`
Expected: FAIL — `Store.Add` undefined, `Load` signature mismatch.

- [ ] **Step 3: Rewrite `internal/keybinds/store.go`**

```go
package keybinds

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// ErrNoSuchTrigger means the index handed to RemoveAt was out of range.
var ErrNoSuchTrigger = errors.New("keybinds: no such trigger")

// Stolen reports which action lost a trigger when another action took it.
type Stolen struct {
	ActionID ActionID
	Trigger  trigger.Trigger
}

// Store holds the live action->triggers map. Safe for concurrent use.
//
// An action holds a LIST of triggers, so a user can drive the same action
// from a keyboard chord and a joystick button at once. Nothing here assumes
// a trigger's kind.
type Store struct {
	mu sync.RWMutex
	// binds holds triggers for action IDs we understand.
	binds map[ActionID][]trigger.Trigger
	// unknown holds raw entries whose action ID we do not recognise, so a
	// config written by a newer version survives a load/save cycle here.
	unknown map[string][]string
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		binds:   map[ActionID][]trigger.Trigger{},
		unknown: map[string][]string{},
	}
}

func knownIDs() map[ActionID]bool {
	m := map[ActionID]bool{}
	for _, a := range StaticActions() {
		m[a.ID] = true
	}
	return m
}

// isPerRadioID reports whether id looks like "radio.<n>.ptt" / "radio.<n>.select".
func isPerRadioID(id string) bool {
	if len(id) < len("radio.0.ptt") {
		return false
	}
	if !strings.HasPrefix(id, "radio.") {
		return false
	}
	return strings.HasSuffix(id, ".ptt") || strings.HasSuffix(id, ".select")
}

// Load replaces the store contents from a raw map (as read from config.toml).
// Entries that will not parse are dropped INDIVIDUALLY, keeping the rest of
// that action's list; entries whose action ID is unrecognised are preserved
// verbatim for the next Snapshot.
func (s *Store) Load(raw map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binds = map[ActionID][]trigger.Trigger{}
	s.unknown = map[string][]string{}
	known := knownIDs()
	for id, list := range raw {
		if !known[ActionID(id)] && !isPerRadioID(id) {
			s.unknown[id] = append([]string(nil), list...)
			continue
		}
		var out []trigger.Trigger
		for _, str := range list {
			t, err := trigger.Parse(str)
			if err != nil {
				continue // malformed trigger: drop this entry, keep the rest
			}
			out = append(out, t)
		}
		if len(out) > 0 {
			s.binds[ActionID(id)] = out
		}
	}
}

// Snapshot renders the store as a raw map for persistence, including
// preserved unknown entries.
func (s *Store) Snapshot() map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string][]string, len(s.binds)+len(s.unknown))
	for id, list := range s.binds {
		strs := make([]string, 0, len(list))
		for _, t := range list {
			strs = append(strs, t.String())
		}
		out[string(id)] = strs
	}
	for id, list := range s.unknown {
		out[id] = append([]string(nil), list...)
	}
	return out
}

// Get returns the triggers bound to id.
func (s *Store) Get(id ActionID) ([]trigger.Trigger, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list, ok := s.binds[id]
	return append([]trigger.Trigger(nil), list...), ok
}

// All returns a copy of the live bindings.
func (s *Store) All() map[ActionID][]trigger.Trigger {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[ActionID][]trigger.Trigger, len(s.binds))
	for k, v := range s.binds {
		out[k] = append([]trigger.Trigger(nil), v...)
	}
	return out
}

// Add appends t to id's trigger list. If another action already holds t, that
// action loses just that one trigger and it is returned as Stolen -- its
// other bindings survive, which is the point of the additive model. Adding a
// trigger an action already holds is a no-op, not a steal.
//
// Keyboard and joystick triggers can never collide because trigger.Equal
// compares Kind first, so the two kinds are separate conflict namespaces
// without a special case here.
//
// An action holds AT MOST ONE keyboard trigger: adding a second chord
// replaces the first. internal/hotkeys registers one chord per action ID
// (Manager.Apply and dispatcher.binds are both keyed that way), so a second
// chord would persist, render, and never fire. Replacing keeps that
// impossible without reopening the shipped keyboard path. Joystick triggers
// have no such limit.
func (s *Store) Add(id ActionID, t trigger.Trigger) *Stolen {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.binds[id] {
		if existing.Equal(t) {
			return nil // already bound here
		}
	}

	if t.Kind == trigger.KindKey {
		kept := s.binds[id][:0:0]
		for _, existing := range s.binds[id] {
			if existing.Kind != trigger.KindKey {
				kept = append(kept, existing)
			}
		}
		s.binds[id] = kept
	}

	var stolen *Stolen
	for other, list := range s.binds {
		if other == id {
			continue
		}
		for i, existing := range list {
			if !existing.Equal(t) {
				continue
			}
			stolen = &Stolen{ActionID: other, Trigger: existing}
			s.binds[other] = append(list[:i:i], list[i+1:]...)
			if len(s.binds[other]) == 0 {
				delete(s.binds, other)
			}
			break
		}
		if stolen != nil {
			break
		}
	}

	s.binds[id] = append(s.binds[id], t)
	return stolen
}

// RemoveAt drops the trigger at index i from id's list.
func (s *Store) RemoveAt(id ActionID, i int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.binds[id]
	if i < 0 || i >= len(list) {
		return fmt.Errorf("%w: %s[%d]", ErrNoSuchTrigger, id, i)
	}
	s.binds[id] = append(list[:i:i], list[i+1:]...)
	if len(s.binds[id]) == 0 {
		delete(s.binds, id)
	}
	return nil
}

// Clear removes every binding for id.
func (s *Store) Clear(id ActionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.binds, id)
}
```

- [ ] **Step 4: Update `Defaults` in `internal/keybinds/actions.go`**

Change the import of `chord` to also import `trigger`, and replace `Defaults` with:

```go
// Defaults are the bindings shipped on first run, matching the design
// prototype. global.ptt ships unbound deliberately -- it is the one the user
// is most likely to want on their own key. No joystick defaults ship: we
// cannot know what devices a user owns.
func Defaults() map[ActionID][]trigger.Trigger {
	must := func(s string) []trigger.Trigger {
		c, err := chord.Parse(s)
		if err != nil {
			panic("keybinds: bad default chord " + s + ": " + err.Error())
		}
		return []trigger.Trigger{trigger.Key(c)}
	}
	return map[ActionID][]trigger.Trigger{
		"global.push_to_mute":        must("V"),
		"global.mute_toggle":         must("M"),
		"global.emergency_broadcast": must("Ctrl+E"),
		"global.compact_overlay":     must("Ctrl+O"),
		"channel.intercom":           must("1"),
		"channel.role":               must("2"),
		"channel.ship":               must("3"),
		"channel.fleet":              must("4"),
		"status.available":           must("Alt+1"),
		"status.combat":              must("Alt+2"),
		"status.discipline":          must("Alt+3"),
		"status.afk":                 must("Alt+4"),
	}
}
```

Update `internal/keybinds/actions_test.go` wherever it asserts on `Defaults()` values to index the slice (`got["channel.intercom"][0]`).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/keybinds/... ./internal/trigger/... ./internal/config/... -v`
Expected: PASS.

- [ ] **Step 6: Prove the partial-steal test bites**

Temporarily make `Add` delete the victim's whole list (`delete(s.binds, other)` unconditionally). Re-run: `TestStealTakesOnlyTheConflictingTrigger` MUST fail. Restore.

- [ ] **Step 7: Commit**

```bash
git add internal/keybinds/
git commit -m "$(cat <<'EOF'
feat(keybinds): hold a list of triggers per action

Add/RemoveAt/Clear replace Set. A steal now takes only the conflicting
trigger and leaves the victim's other bindings intact -- without that the
additive model would be additive in name only.

Keyboard and joystick triggers are separate conflict namespaces for free:
trigger.Equal compares Kind first, so neither can ever steal from the
other and no special case is needed.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Joystick `Source` seam and specificity resolution

**Files:**
- Create: `internal/joystick/source.go`
- Create: `internal/joystick/resolve.go`
- Test: `internal/joystick/resolve_test.go`

**Interfaces:**
- Consumes: `trigger.DeviceID`, `trigger.Button`, `trigger.JoyButton`, `trigger.JoyBinding` (Task 1).
- Produces: `joystick.Device`, `joystick.State`, `State.IsHeld(trigger.JoyButton) bool`, `joystick.Source`, `joystick.ErrUnsupported`, `joystick.Binding{Joy trigger.JoyBinding; Hold bool}`, `joystick.Active(map[string][]Binding, State) map[string]bool`.

**The rule being implemented (spec §5), reproduced in full:**

Let `H` be the set of currently-held `(Device, Button)` pairs.

- **Satisfied:** trigger `T` is satisfied iff `T.Button ∈ H` and (`T.Modifier == nil` or `T.Modifier ∈ H`).
- **Suppressed:** a satisfied `T` is suppressed iff `T.Modifier == nil` and there exists another satisfied trigger `U` with `U.Modifier != nil` and (`U.Button == T.Button` or `U.Modifier == T.Button`).
- **Effective active set** = satisfied minus suppressed.

- [ ] **Step 1: Write the failing test**

Create `internal/joystick/resolve_test.go`:

```go
package joystick_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/joystick"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

const dev = trigger.DeviceID("stick-c3")
const dev2 = trigger.DeviceID("throttle-a1")

func jb(d trigger.DeviceID, b trigger.Button) trigger.JoyButton {
	return trigger.JoyButton{Device: d, Button: b}
}

// state builds a State with the given buttons held and their devices connected.
func state(held ...trigger.JoyButton) joystick.State {
	s := joystick.State{
		Connected: map[trigger.DeviceID]bool{},
		Held:      map[trigger.JoyButton]struct{}{},
	}
	for _, h := range held {
		s.Connected[h.Device] = true
		s.Held[h] = struct{}{}
	}
	return s
}

func bare(d trigger.DeviceID, b trigger.Button) joystick.Binding {
	return joystick.Binding{Joy: trigger.JoyBinding{Device: d, Button: b}, Hold: true}
}

func withMod(d trigger.DeviceID, b trigger.Button, mod trigger.JoyButton) joystick.Binding {
	m := mod
	return joystick.Binding{
		Joy:  trigger.JoyBinding{Device: d, Button: b, Modifier: &m},
		Hold: true,
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestActiveBareBinding(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3)},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); !got["global.ptt"] {
		t.Errorf("Active = %v, want global.ptt held", keys(got))
	}
	if got := joystick.Active(binds, state(jb(dev, 4))); got["global.ptt"] {
		t.Errorf("Active = %v, want nothing held", keys(got))
	}
}

func TestModifierMustAlsoBeHeld(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); got["radio.1.ptt"] {
		t.Error("modifier binding fired without its modifier held")
	}
	if got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 5))); !got["radio.1.ptt"] {
		t.Error("modifier binding did not fire with both held")
	}
}

func TestSpecificitySuppressesTheBareBinding(t *testing.T) {
	// The worked example from spec section 5:
	//   global.ptt  = Btn3
	//   radio.1.ptt = Btn5 + Btn3
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 3)},
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}

	got := joystick.Active(binds, state(jb(dev, 3)))
	if !got["global.ptt"] || got["radio.1.ptt"] {
		t.Errorf("Btn3 alone: Active = %v, want only global.ptt", keys(got))
	}

	got = joystick.Active(binds, state(jb(dev, 3), jb(dev, 5)))
	if !got["radio.1.ptt"] {
		t.Errorf("Btn5+Btn3: Active = %v, want radio.1.ptt", keys(got))
	}
	if got["global.ptt"] {
		t.Errorf("Btn5+Btn3: global.ptt was NOT suppressed; Active = %v", keys(got))
	}
}

func TestSuppressionAlsoAppliesWhenTheBareBindingIsTheModifier(t *testing.T) {
	// global.ptt is bound to the very button another binding uses AS its
	// modifier. Holding the combo must not also fire global.ptt.
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 5)},
		"radio.1.ptt": {withMod(dev, 3, jb(dev, 5))},
	}
	got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 5)))
	if !got["radio.1.ptt"] {
		t.Errorf("Active = %v, want radio.1.ptt", keys(got))
	}
	if got["global.ptt"] {
		t.Errorf("bare binding on the modifier button was not suppressed: %v", keys(got))
	}
}

func TestCrossDeviceModifier(t *testing.T) {
	// Throttle button qualifying a stick button -- a normal HOTAS pattern.
	binds := map[string][]joystick.Binding{
		"radio.2.ptt": {withMod(dev, 3, jb(dev2, 7))},
	}
	if got := joystick.Active(binds, state(jb(dev, 3))); got["radio.2.ptt"] {
		t.Error("fired without the cross-device modifier")
	}
	if got := joystick.Active(binds, state(jb(dev, 3), jb(dev2, 7))); !got["radio.2.ptt"] {
		t.Error("cross-device modifier did not fire")
	}
}

func TestUnrelatedModifierBindingDoesNotSuppress(t *testing.T) {
	// A modifier binding on entirely different buttons must leave the bare
	// binding alone -- suppression is targeted, not global.
	binds := map[string][]joystick.Binding{
		"global.ptt":  {bare(dev, 3)},
		"radio.1.ptt": {withMod(dev, 9, jb(dev, 8))},
	}
	got := joystick.Active(binds, state(jb(dev, 3), jb(dev, 8), jb(dev, 9)))
	if !got["global.ptt"] {
		t.Errorf("unrelated modifier binding suppressed global.ptt: %v", keys(got))
	}
	if !got["radio.1.ptt"] {
		t.Errorf("Active = %v, want radio.1.ptt too", keys(got))
	}
}

func TestActionWithSeveralTriggersIsHeldByAnyOfThem(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3), bare(dev2, 7)},
	}
	if got := joystick.Active(binds, state(jb(dev2, 7))); !got["global.ptt"] {
		t.Error("second trigger of the same action did not hold it")
	}
}

func TestDisconnectedDeviceHoldsNothing(t *testing.T) {
	binds := map[string][]joystick.Binding{
		"global.ptt": {bare(dev, 3)},
	}
	empty := joystick.State{
		Connected: map[trigger.DeviceID]bool{},
		Held:      map[trigger.JoyButton]struct{}{},
	}
	if got := joystick.Active(binds, empty); len(got) != 0 {
		t.Errorf("Active on empty state = %v, want nothing", keys(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/joystick/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write `internal/joystick/source.go`**

```go
// Package joystick reads gamepad, joystick and HOTAS input and turns it into
// the same Pressed/Released edges internal/hotkeys produces for the keyboard.
//
// It is a SIBLING of internal/hotkeys, not a change to it: the keyboard path
// is already shipped and tested, and joystick support is additive.
//
// The OS sits behind Source. Everything above that seam -- edge detection,
// specificity resolution, capture -- is a pure function over successive State
// values, so the entire behavioural core is testable with no device attached.
package joystick

import (
	"errors"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// ErrUnsupported is returned by backends on platforms where joystick input is
// not implemented. It is deliberately distinct from a permission denial: it
// is not something the user can fix, and the UI must not offer a grant
// affordance for it.
var ErrUnsupported = errors.New("joystick: not supported on this platform")

// Device is one attached input device.
type Device struct {
	// ID is stable across replug where the OS permits. Always a legal
	// trigger.DeviceID -- backends sanitise.
	ID trigger.DeviceID
	// Name is the human-readable product name, shown in the UI and persisted
	// as display metadata so an absent device is still nameable.
	Name string
	// Buttons is how many plain buttons the device reports.
	Buttons int
	// Hats is how many POV hats the device reports.
	Hats int
}

// State is a snapshot of every held input across all connected devices.
type State struct {
	// Connected names the devices the backend can currently see.
	Connected map[trigger.DeviceID]bool
	// Held is the set of currently-held inputs.
	Held map[trigger.JoyButton]struct{}
}

// IsHeld reports whether b is currently held.
func (s State) IsHeld(b trigger.JoyButton) bool {
	_, ok := s.Held[b]
	return ok
}

// Source is the OS seam. Implementations are not required to be safe for
// concurrent use: Manager serialises every call.
type Source interface {
	// Devices enumerates what is attached. Called on a slow timer for
	// hot-plug, and by the capture UI.
	Devices() ([]Device, error)
	// Poll returns the current held-input snapshot.
	Poll() (State, error)
	// Close releases the OS resources. Safe to call more than once and safe
	// on a Source that never opened anything.
	Close()
}
```

- [ ] **Step 4: Write `internal/joystick/resolve.go`**

```go
package joystick

import "github.com/FPGSchiba/vcs-srs-client/internal/trigger"

// Binding is one registerable joystick trigger. Hold means the action needs
// release as well as press (push-to-talk semantics), matching
// hotkeys.Binding.Hold.
type Binding struct {
	Joy  trigger.JoyBinding
	Hold bool
}

// Active returns the set of action IDs that should currently be held.
//
// An action is held when ANY of its triggers is effectively active, so the
// caller sees one boolean per action and never has to reason about which
// trigger won.
//
// SPECIFICITY. Keyboard chords get this for free -- internal/hotkeys matches
// modifiers exactly, so "E" simply does not match a Ctrl+E event. Joystick
// modifiers are arbitrary buttons, so "no modifier held" is not knowable
// without knowing which buttons are modifiers, and the rule has to be
// explicit:
//
//	global.ptt  = Btn3
//	radio.1.ptt = Btn5 + Btn3
//
// Holding Btn5+Btn3 must fire radio.1.ptt and NOT global.ptt. So a
// modifier-less trigger is suppressed whenever another ACTIVE trigger uses
// that same button as either its main input or its modifier.
func Active(binds map[string][]Binding, s State) map[string]bool {
	type sat struct {
		actionID string
		joy      trigger.JoyBinding
	}

	// Pass 1: everything whose buttons are all held.
	var satisfied []sat
	for id, list := range binds {
		for _, b := range list {
			if !s.IsHeld(b.Joy.Main()) {
				continue
			}
			if b.Joy.Modifier != nil && !s.IsHeld(*b.Joy.Modifier) {
				continue
			}
			satisfied = append(satisfied, sat{actionID: id, joy: b.Joy})
		}
	}

	// Pass 2: collect the buttons claimed by an active MODIFIER binding.
	// A bare binding on any of these loses to the more specific combination.
	claimed := map[trigger.JoyButton]bool{}
	for _, t := range satisfied {
		if t.joy.Modifier == nil {
			continue
		}
		claimed[t.joy.Main()] = true
		claimed[*t.joy.Modifier] = true
	}

	// Pass 3: drop suppressed bare bindings, fold the rest to action IDs.
	out := map[string]bool{}
	for _, t := range satisfied {
		if t.joy.Modifier == nil && claimed[t.joy.Main()] {
			continue // a more specific active binding owns this button
		}
		out[t.actionID] = true
	}
	return out
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/joystick/... -v`
Expected: PASS, all eight tests.

- [ ] **Step 6: Prove the suppression tests bite**

Temporarily delete pass 3's `continue` (so nothing is ever suppressed). Re-run: `TestSpecificitySuppressesTheBareBinding` and `TestSuppressionAlsoAppliesWhenTheBareBindingIsTheModifier` MUST both fail. Restore. This is the subtlest logic in the change (spec risk J3); a suppression bug is silent, so the tests guarding it must be known-good.

- [ ] **Step 7: Commit**

```bash
git add internal/joystick/
git commit -m "$(cat <<'EOF'
feat(joystick): add the Source seam and specificity resolution

Active() folds held buttons plus a binding table into the set of action
IDs that should be held. A modifier-less trigger is suppressed when
another active trigger claims that button as its main input or modifier,
so Btn5+Btn3 fires radio.1.ptt without also firing a bare Btn3 binding.

Keyboard chords get this free from exact modifier matching; joystick
modifiers are arbitrary buttons, so the rule has to be explicit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Joystick `Manager` — poll loop, edges, hot-plug

**Files:**
- Create: `internal/joystick/manager.go`
- Test: `internal/joystick/manager_test.go` (internal test, `package joystick`, so it can drive `tick` directly instead of sleeping)

**Interfaces:**
- Consumes: Task 5's `Source`, `State`, `Device`, `Binding`, `Active`, `ErrUnsupported`.
- Produces: `joystick.Handler` (structural twin of `hotkeys.Handler`), `joystick.New(Source, Handler, *slog.Logger) *Manager`, `Manager.Apply(map[string][]Binding) error`, `Manager.Start()`, `Manager.Close()`, `Manager.Suspend()`, `Manager.Resume()`, `Manager.Devices() []Device`, `Manager.Supported() bool`, `Manager.LastErr() error`.

**Why `joystick.Handler` rather than importing `hotkeys.Handler`:** the two packages are siblings and neither should depend on the other. Go interfaces are structural, so `*app.App` satisfies both with no adapter.

**Invariant this task must establish and test — the refcount in Task 10 depends on it:** the manager emits a `Released` for every **hold** action it currently holds whenever it stops holding it *for any reason* — button up, device unplugged, `Suspend`, `Apply`, or `Close`. An unbalanced edge here leaks into the refcount and silently deadens an action for the rest of the session.

- [ ] **Step 1: Write the failing test**

Create `internal/joystick/manager_test.go`:

```go
package joystick

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

const tdev = trigger.DeviceID("stick-c3")

func tbtn(b trigger.Button) trigger.JoyButton {
	return trigger.JoyButton{Device: tdev, Button: b}
}

type fakeSource struct {
	mu      sync.Mutex
	devices []Device
	held    map[trigger.JoyButton]struct{}
	conn    map[trigger.DeviceID]bool
	pollErr error
	closed  int
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		devices: []Device{{ID: tdev, Name: "Test Stick", Buttons: 32, Hats: 1}},
		held:    map[trigger.JoyButton]struct{}{},
		conn:    map[trigger.DeviceID]bool{tdev: true},
	}
}

func (f *fakeSource) hold(b trigger.JoyButton) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held[b] = struct{}{}
}

func (f *fakeSource) release(b trigger.JoyButton) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.held, b)
}

// unplug models the device vanishing mid-hold: nothing is connected and
// nothing reads as held any more.
func (f *fakeSource) unplug() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.devices = nil
	f.held = map[trigger.JoyButton]struct{}{}
	f.conn = map[trigger.DeviceID]bool{}
}

func (f *fakeSource) Devices() ([]Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Device(nil), f.devices...), nil
}

func (f *fakeSource) Poll() (State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollErr != nil {
		return State{}, f.pollErr
	}
	held := make(map[trigger.JoyButton]struct{}, len(f.held))
	for k := range f.held {
		held[k] = struct{}{}
	}
	conn := make(map[trigger.DeviceID]bool, len(f.conn))
	for k, v := range f.conn {
		conn[k] = v
	}
	return State{Connected: conn, Held: held}, nil
}

func (f *fakeSource) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

type recorder struct {
	mu   sync.Mutex
	logs []string
}

func (r *recorder) Pressed(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, "down:"+id)
}

func (r *recorder) Released(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, "up:"+id)
}

func (r *recorder) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

func testManager(t *testing.T) (*Manager, *fakeSource, *recorder) {
	t.Helper()
	src := newFakeSource()
	rec := &recorder{}
	m := New(src, rec, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return m, src, rec
}

func holdBind(b trigger.Button) Binding {
	return Binding{Joy: trigger.JoyBinding{Device: tdev, Button: b}, Hold: true}
}

func pressBind(b trigger.Button) Binding {
	return Binding{Joy: trigger.JoyBinding{Device: tdev, Button: b}, Hold: false}
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

func TestPressAndReleaseEdges(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	m.tick()
	eq(t, rec.events(), nil)

	src.hold(tbtn(3))
	m.tick()
	m.tick() // held across two polls must NOT re-fire
	eq(t, rec.events(), []string{"down:global.ptt"})

	src.release(tbtn(3))
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestPressKindNeverEmitsReleased(t *testing.T) {
	// Matches internal/hotkeys: only hold bindings owe a Released. Task 10's
	// refcount depends on this, so it is pinned here.
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.mute_toggle": {pressBind(4)}})

	src.hold(tbtn(4))
	m.tick()
	src.release(tbtn(4))
	m.tick()
	eq(t, rec.events(), []string{"down:global.mute_toggle"})
}

func TestDeviceVanishingWhileHeldReleases(t *testing.T) {
	// This is why the manager needs no stale-latch watchdog: polling makes
	// the failure self-healing, but only if we actually emit the edge.
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	src.hold(tbtn(3))
	m.tick()
	src.unplug()
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestSuspendReleasesHeldActions(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})

	src.hold(tbtn(3))
	m.tick()
	m.Suspend()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})

	// While suspended, polling must do nothing at all.
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})

	// Resuming with the button still held re-presses it.
	m.Resume()
	m.tick()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt", "down:global.ptt"})
}

func TestApplyReleasesActionsItDropped(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	// Rebinding the action away from the held button must release it, or the
	// refcount in Task 10 leaks and the action is dead for the session.
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(9)}})
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
}

func TestCloseReleasesHeldActions(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	m.Close()
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
	if src.closed != 1 {
		t.Errorf("source Close called %d times, want 1", src.closed)
	}
	m.Close() // must be safe twice
	if src.closed != 1 {
		t.Errorf("second Close reached the source (%d calls)", src.closed)
	}
}

func TestPollErrorIsRecordedNotPanicked(t *testing.T) {
	m, src, rec := testManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	src.hold(tbtn(3))
	m.tick()

	src.mu.Lock()
	src.pollErr = errors.New("device read failed")
	src.mu.Unlock()
	m.tick()

	// A failing poll must release what it was holding: we can no longer
	// prove the button is down, and a stuck-open microphone is the worst
	// possible failure here.
	eq(t, rec.events(), []string{"down:global.ptt", "up:global.ptt"})
	if m.LastErr() == nil {
		t.Error("LastErr = nil after a failing poll")
	}
}

func TestUnsupportedSourceReportsNotSupported(t *testing.T) {
	src := &unsupportedSource{}
	m := New(src, &recorder{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if m.Supported() {
		t.Error("Supported() = true for an unsupported source")
	}
	m.tick() // must not panic
}

type unsupportedSource struct{}

func (unsupportedSource) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (unsupportedSource) Poll() (State, error)       { return State{}, ErrUnsupported }
func (unsupportedSource) Close()                     {}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/joystick/...`
Expected: FAIL — `New`, `Manager`, `tick` undefined.

- [ ] **Step 3: Write `internal/joystick/manager.go`**

```go
package joystick

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// DefaultPollInterval is how often the manager samples device state. DCS-SRS
// ships 40ms; 10ms costs nothing measurable and keeps the added push-to-talk
// latency inaudible.
const DefaultPollInterval = 10 * time.Millisecond

// DefaultRediscoverInterval is how often the manager re-enumerates devices so
// a stick plugged in after launch starts working without a restart.
const DefaultRediscoverInterval = 3 * time.Second

// Handler receives joystick activity. Implementations must not block.
//
// Structurally identical to hotkeys.Handler, and deliberately NOT that type:
// the two packages are siblings and neither should depend on the other. Go
// interfaces are structural, so one application type satisfies both.
type Handler interface {
	Pressed(actionID string)
	Released(actionID string)
}

// Manager owns the poll loop and the current joystick binding set.
//
// It deliberately has no stale-latch watchdog, unlike internal/hotkeys.
// Polling makes that failure mode self-healing: a device that disappears
// mid-transmission reads as all-buttons-up on the next poll and releases
// naturally, which covers unplug, sleep and driver reset alike and is
// strictly better than a timeout.
type Manager struct {
	mu sync.Mutex

	src Source
	h   Handler
	log *slog.Logger

	desired map[string][]Binding
	hold    map[string]bool // action ID -> owes a Released
	active  map[string]bool // action IDs currently held

	suspended bool
	closed    bool
	supported bool
	lastErr   error

	devices []Device

	PollInterval       time.Duration
	RediscoverInterval time.Duration

	stop    chan struct{}
	done    chan struct{}
	started bool
}

// New constructs a Manager over a Source.
func New(src Source, h Handler, log *slog.Logger) *Manager {
	m := &Manager{
		src:                src,
		h:                  h,
		log:                log,
		desired:            map[string][]Binding{},
		hold:               map[string]bool{},
		active:             map[string]bool{},
		supported:          true,
		PollInterval:       DefaultPollInterval,
		RediscoverInterval: DefaultRediscoverInterval,
		stop:               make(chan struct{}),
		done:               make(chan struct{}),
	}
	// Probe once so Supported() is meaningful before the loop starts and the
	// UI can hide the affordance rather than showing a broken one.
	if _, err := src.Devices(); errors.Is(err, ErrUnsupported) {
		m.supported = false
		m.lastErr = err
	}
	return m
}

// Supported reports whether this platform has a real backend.
func (m *Manager) Supported() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.supported
}

// LastErr returns the most recent source error, if any.
func (m *Manager) LastErr() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}

// Devices returns the most recently enumerated device list.
func (m *Manager) Devices() []Device {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Device(nil), m.devices...)
}

// Apply replaces the desired binding set. Any action that was held and is no
// longer bound to what is currently held is released first, so the edge
// balance the app-level refcount depends on is never broken by a rebind.
func (m *Manager) Apply(binds map[string][]Binding) error {
	m.mu.Lock()
	next := make(map[string][]Binding, len(binds))
	hold := make(map[string]bool, len(binds))
	for id, list := range binds {
		next[id] = append([]Binding(nil), list...)
		for _, b := range list {
			if b.Hold {
				hold[id] = true
			}
		}
	}
	m.desired = next
	// Release everything currently held: the next tick re-presses whatever is
	// still legitimately active under the NEW table. Releasing then
	// re-pressing is correct and cheap; trying to diff old against new here
	// is where an unbalanced edge would hide.
	release := m.takeActiveLocked()
	m.hold = hold
	m.mu.Unlock()

	m.emitReleases(release)
	return nil
}

// Suspend stops driving handlers and releases anything held, so a capture
// cannot transmit while the user is binding a button.
func (m *Manager) Suspend() {
	m.mu.Lock()
	if m.suspended {
		m.mu.Unlock()
		return
	}
	m.suspended = true
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)
}

// Resume re-enables handler dispatch. The next tick re-presses whatever is
// genuinely held.
func (m *Manager) Resume() {
	m.mu.Lock()
	m.suspended = false
	m.mu.Unlock()
}

// Start begins the poll loop. Safe to call once; later calls are no-ops.
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started || m.closed || !m.supported {
		m.mu.Unlock()
		return
	}
	m.started = true
	poll, rediscover := m.PollInterval, m.RediscoverInterval
	m.mu.Unlock()

	go m.loop(poll, rediscover)
}

// Close stops the loop, releases anything held and closes the source. Safe to
// call more than once.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	started := m.started
	release := m.takeActiveLocked()
	m.mu.Unlock()

	m.emitReleases(release)

	if started {
		close(m.stop)
		<-m.done
	}
	m.src.Close()
}

func (m *Manager) loop(poll, rediscover time.Duration) {
	defer close(m.done)
	pollTick := time.NewTicker(poll)
	defer pollTick.Stop()
	discoverTick := time.NewTicker(rediscover)
	defer discoverTick.Stop()

	m.rediscover()
	for {
		select {
		case <-m.stop:
			return
		case <-pollTick.C:
			m.tick()
		case <-discoverTick.C:
			m.rediscover()
		}
	}
}

// rediscover re-enumerates devices so hot-plugged hardware starts working.
func (m *Manager) rediscover() {
	devs, err := m.src.Devices()
	m.mu.Lock()
	if err != nil {
		m.lastErr = err
	} else {
		m.devices = devs
	}
	m.mu.Unlock()
}

// tick samples the source once and drives the resulting edges. It is the
// whole behavioural core, and the tests call it directly rather than waiting
// on the ticker.
func (m *Manager) tick() {
	m.mu.Lock()
	if m.suspended || m.closed || !m.supported {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	state, err := m.src.Poll()
	if err != nil {
		// A failing poll means we can no longer prove anything is down.
		// Release everything: a stuck-open microphone is the worst outcome
		// available here.
		m.mu.Lock()
		m.lastErr = err
		release := m.takeActiveLocked()
		m.mu.Unlock()
		m.emitReleases(release)
		return
	}

	m.mu.Lock()
	if m.suspended || m.closed {
		m.mu.Unlock()
		return
	}
	want := Active(m.desired, state)

	var press []string
	var release []string
	for id := range want {
		if !m.active[id] {
			m.active[id] = true
			press = append(press, id)
		}
	}
	for id := range m.active {
		if !want[id] {
			delete(m.active, id)
			if m.hold[id] {
				release = append(release, id)
			}
		}
	}
	binds := m.desired
	m.mu.Unlock()

	// Handler calls happen outside the lock: Handler is documented as
	// non-blocking, but it reaches application code and the poll loop must
	// not be able to stall behind it.
	for _, id := range press {
		m.logEdge(id, binds[id], state)
		m.h.Pressed(id)
	}
	for _, id := range release {
		m.h.Released(id)
	}
}

// takeActiveLocked empties the active set and returns the HOLD actions that
// owe a Released. Caller holds m.mu.
func (m *Manager) takeActiveLocked() []string {
	var out []string
	for id := range m.active {
		if m.hold[id] {
			out = append(out, id)
		}
		delete(m.active, id)
	}
	return out
}

func (m *Manager) emitReleases(ids []string) {
	for _, id := range ids {
		m.h.Released(id)
	}
}

// logEdge records which physical input fired an action.
//
// Unlike internal/hotkeys, this DOES name the input. That rule exists there
// because the keyboard listener sees every keystroke on the machine, so a log
// naming keys would be a keylog. A joystick backend sees joystick buttons and
// nothing else, so the same reasoning does not apply -- and naming the button
// is what makes "my HOTAS bind does nothing" diagnosable at all.
func (m *Manager) logEdge(actionID string, binds []Binding, s State) {
	for _, b := range binds {
		if !s.IsHeld(b.Joy.Main()) {
			continue
		}
		if b.Joy.Modifier != nil && !s.IsHeld(*b.Joy.Modifier) {
			continue
		}
		attrs := []any{
			"action", actionID,
			"device", string(b.Joy.Device),
			"input", b.Joy.Button.Label(),
		}
		if b.Joy.Modifier != nil {
			attrs = append(attrs,
				"modifier_device", string(b.Joy.Modifier.Device),
				"modifier_input", b.Joy.Modifier.Button.Label())
		}
		m.log.Info("joystick bind fired", attrs...)
		return
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/joystick/... -v`
Expected: PASS, all resolve and manager tests.

- [ ] **Step 5: Prove the release-on-unplug test bites**

Temporarily make `tick` return early when `err == nil && len(state.Held) == 0`. Re-run: `TestDeviceVanishingWhileHeldReleases` MUST fail. Restore. A missed release here is a stuck-open microphone.

- [ ] **Step 6: Commit**

```bash
git add internal/joystick/
git commit -m "$(cat <<'EOF'
feat(joystick): add the polled Manager with balanced press/release edges

Samples the Source at 100Hz, folds state through Active() and drives
Pressed/Released. Re-enumerates every 3s so a stick plugged in after
launch works without a restart.

No stale-latch watchdog, unlike internal/hotkeys: polling makes that
self-healing. Every path that stops holding an action -- button up,
unplug, failing poll, Suspend, Apply, Close -- emits the Released a hold
binding owes, which is the invariant the app-level refcount relies on.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Joystick capture with modifier inference

**Files:**
- Create: `internal/joystick/capture.go`
- Modify: `internal/joystick/manager.go` (call the capture hook from `tick`)
- Test: `internal/joystick/capture_test.go` (internal test, `package joystick`)

**Interfaces:**
- Consumes: Task 5–6 `State`, `Manager`, `tick`.
- Produces: `joystick.Captured{Binding trigger.JoyBinding}`, `Manager.BeginCapture(func(Captured))`, `Manager.CancelCapture()`.

**Rules (spec §9), reproduced in full:**

- Capture detects presses by **delta against a baseline snapshot** taken when capture begins, so a button already held when capture starts cannot register.
- Capture completes **on release** (when nothing captured is held any more).
- Two buttons held → first-held becomes the modifier, last-pressed the main.
- One button → bare binding.
- **Tie-break:** if two buttons first appear held in the *same* poll sample, the one sorting lower by `(DeviceID, Button)` becomes the modifier.
- More than two → first-held is the modifier, last-pressed is the main, buttons in between ignored.

While a capture is armed the manager must **not** drive handlers, even if not otherwise suspended — pressing a button to bind it must never also fire the action it is being bound to.

- [ ] **Step 1: Write the failing test**

Create `internal/joystick/capture_test.go`:

```go
package joystick

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

func captureManager(t *testing.T) (*Manager, *fakeSource, *recorder) {
	t.Helper()
	return testManager(t)
}

func TestCaptureBareButton(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(11))
	m.tick()
	if got != nil {
		t.Fatal("capture completed on press; it must complete on release")
	}
	src.release(tbtn(11))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete on release")
	}
	if got.Binding.Device != tdev || got.Binding.Button != 11 {
		t.Errorf("captured %+v, want stick-c3 button 11", got.Binding)
	}
	if got.Binding.Modifier != nil {
		t.Errorf("bare capture produced a modifier: %+v", got.Binding.Modifier)
	}
}

func TestCaptureInfersModifierFromHoldOrder(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(5)) // held FIRST -> modifier
	m.tick()
	src.hold(tbtn(3)) // pressed SECOND -> main
	m.tick()
	src.release(tbtn(3))
	src.release(tbtn(5))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete")
	}
	if got.Binding.Button != 3 {
		t.Errorf("main = %v, want button 3", got.Binding.Button)
	}
	if got.Binding.Modifier == nil || got.Binding.Modifier.Button != 5 {
		t.Errorf("modifier = %+v, want button 5", got.Binding.Modifier)
	}
}

func TestCaptureIgnoresButtonsAlreadyHeldAtBaseline(t *testing.T) {
	// A button down when capture starts must not register -- otherwise a
	// user holding their PTT while opening settings binds it instantly.
	m, src, _ := captureManager(t)
	src.hold(tbtn(9))

	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })
	m.tick()
	src.release(tbtn(9))
	m.tick()

	if got != nil {
		t.Errorf("baseline-held button was captured: %+v", got.Binding)
	}
}

func TestSameTickTieBreakIsDeterministic(t *testing.T) {
	// Both buttons first appear in the SAME sample, so hold order is
	// unknowable. Lower (DeviceID, Button) must win the modifier slot, or
	// capture depends on poll alignment.
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	src.hold(tbtn(7))
	src.hold(tbtn(2))
	m.tick()
	src.release(tbtn(7))
	src.release(tbtn(2))
	m.tick()

	if got == nil {
		t.Fatal("capture did not complete")
	}
	if got.Binding.Modifier == nil || got.Binding.Modifier.Button != 2 {
		t.Errorf("modifier = %+v, want the lower-sorted button 2", got.Binding.Modifier)
	}
	if got.Binding.Button != 7 {
		t.Errorf("main = %v, want button 7", got.Binding.Button)
	}
}

func TestCaptureSuppressesHandlerDispatch(t *testing.T) {
	// Pressing a button to bind it must never also fire the action it is
	// being bound to.
	m, src, rec := captureManager(t)
	m.Apply(map[string][]Binding{"global.ptt": {holdBind(3)}})
	m.BeginCapture(func(Captured) {})

	src.hold(tbtn(3))
	m.tick()
	src.release(tbtn(3))
	m.tick()

	if ev := rec.events(); len(ev) != 0 {
		t.Errorf("handler fired during capture: %v", ev)
	}
}

func TestCancelCaptureStopsCapturing(t *testing.T) {
	m, src, _ := captureManager(t)
	called := false
	m.BeginCapture(func(Captured) { called = true })
	m.CancelCapture()

	src.hold(tbtn(3))
	m.tick()
	src.release(tbtn(3))
	m.tick()

	if called {
		t.Error("capture completed after CancelCapture")
	}
}

func TestCaptureHatDirection(t *testing.T) {
	m, src, _ := captureManager(t)
	var got *Captured
	m.BeginCapture(func(c Captured) { got = &c })

	hat := trigger.JoyButton{Device: tdev, Button: trigger.HatButton(0, 2)}
	src.hold(hat)
	m.tick()
	src.release(hat)
	m.tick()

	if got == nil || got.Binding.Button != trigger.HatButton(0, 2) {
		t.Errorf("hat capture = %+v, want hat 0 dir 2", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/joystick/... -run TestCapture`
Expected: FAIL — `Captured`, `BeginCapture` undefined.

- [ ] **Step 3: Write `internal/joystick/capture.go`**

```go
package joystick

import (
	"sort"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// Captured is the result of a completed capture.
type Captured struct {
	Binding trigger.JoyBinding
}

// captureState tracks one in-flight capture.
//
// Capture works by DELTA against a baseline taken when it begins, so a button
// already down when the user opened the dialog cannot register -- otherwise
// someone holding push-to-talk while opening settings would bind it instantly.
type captureState struct {
	// baseline is everything held when capture began. Ignored throughout.
	baseline map[trigger.JoyButton]struct{}
	// order is the newly-pressed inputs in the order they first appeared.
	order []trigger.JoyButton
	// seen dedupes order.
	seen map[trigger.JoyButton]struct{}
	// done is the caller's completion callback.
	done func(Captured)
}

// BeginCapture arms capture. The callback fires once, on release, on the poll
// goroutine. Arming a capture while one is in flight replaces it.
func (m *Manager) BeginCapture(done func(Captured)) {
	state, err := m.src.Poll()
	baseline := map[trigger.JoyButton]struct{}{}
	if err == nil {
		for b := range state.Held {
			baseline[b] = struct{}{}
		}
	}

	m.mu.Lock()
	m.capture = &captureState{
		baseline: baseline,
		seen:     map[trigger.JoyButton]struct{}{},
		done:     done,
	}
	m.mu.Unlock()
}

// CancelCapture disarms capture without completing it.
func (m *Manager) CancelCapture() {
	m.mu.Lock()
	m.capture = nil
	m.mu.Unlock()
}

// capturing reports whether a capture is armed.
func (m *Manager) capturing() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capture != nil
}

// feedCapture advances an in-flight capture with one poll sample and returns
// the completion callback plus its result when the capture finished.
//
// Completion is ON RELEASE: it needs the full hold order to tell a modifier
// from a main input, and that is only known once the user lets go. Harmless
// in a binding dialog, where nothing is transmitting.
func (m *Manager) feedCapture(s State) (func(Captured), Captured, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	c := m.capture
	if c == nil {
		return nil, Captured{}, false
	}

	// Anything in the baseline that has been released stops being ignored,
	// so a button held at the start can still be bound on its NEXT press.
	for b := range c.baseline {
		if !s.IsHeld(b) {
			delete(c.baseline, b)
		}
	}

	// Collect newly-held inputs. Within one sample the order is unknowable,
	// so sort by (Device, Button) to keep capture deterministic rather than
	// dependent on poll alignment or map iteration order.
	var fresh []trigger.JoyButton
	for b := range s.Held {
		if _, ignored := c.baseline[b]; ignored {
			continue
		}
		if _, dup := c.seen[b]; dup {
			continue
		}
		fresh = append(fresh, b)
	}
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].Device != fresh[j].Device {
			return fresh[i].Device < fresh[j].Device
		}
		return fresh[i].Button < fresh[j].Button
	})
	for _, b := range fresh {
		c.seen[b] = struct{}{}
		c.order = append(c.order, b)
	}

	if len(c.order) == 0 {
		return nil, Captured{}, false // nothing pressed yet
	}
	// Still holding something we captured: wait for release.
	for _, b := range c.order {
		if s.IsHeld(b) {
			return nil, Captured{}, false
		}
	}

	// Released. First-held is the modifier, last-pressed is the main input;
	// anything in between is ignored (spec section 9).
	main := c.order[len(c.order)-1]
	binding := trigger.JoyBinding{Device: main.Device, Button: main.Button}
	if len(c.order) > 1 {
		mod := c.order[0]
		binding.Modifier = &mod
	}
	done := c.done
	m.capture = nil
	return done, Captured{Binding: binding}, true
}
```

- [ ] **Step 4: Wire capture into `tick`**

In `internal/joystick/manager.go`, add the `capture *captureState` field to `Manager`, and insert this immediately after the successful `m.src.Poll()` in `tick`, **before** the dispatch block:

```go
	// A capture owns the device while it is armed: pressing a button to bind
	// it must never also fire the action being bound. This is checked here
	// rather than via Suspend so the app layer can arm a capture without
	// having to remember to suspend and un-suspend around it.
	if m.capturing() {
		if done, result, ok := m.feedCapture(state); ok && done != nil {
			done(result)
		}
		return
	}
```

- [ ] **Step 5: Run the tests**

Run: `go test -race ./internal/joystick/... -v`
Expected: PASS, every resolve, manager and capture test.

- [ ] **Step 6: Prove the baseline test bites**

Temporarily make `BeginCapture` start with an empty baseline. Re-run: `TestCaptureIgnoresButtonsAlreadyHeldAtBaseline` MUST fail. Restore.

- [ ] **Step 7: Commit**

```bash
git add internal/joystick/
git commit -m "$(cat <<'EOF'
feat(joystick): capture a binding by delta, inferring the modifier

Baseline snapshot at arm time means a button already held when the dialog
opens cannot register. Completion is on release, which is what makes the
hold order readable: first-held is the modifier, last-pressed the main,
with a (device, button) tie-break when both land in one sample.

An armed capture suppresses handler dispatch outright, so pressing a
button to bind it never also fires the action being bound.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Vendor di8 and implement the Windows backend

**Files:**
- Create: `internal/joystick/di8/` (vendored, all files build-tagged `windows`)
- Create: `internal/joystick/helperwindow_windows.go`
- Create: `internal/joystick/source_windows.go`
- Create: `internal/joystick/pov_test.go`

**Interfaces:**
- Consumes: Task 5's `Source`, `Device`, `State`, `ErrUnsupported`; `trigger.Button`, `trigger.HatButton`, `trigger.DeviceID`, `trigger.ValidDeviceID`.
- Produces: `joystick.NewOSSource() (Source, error)` on Windows; `joystick.povDirection(raw uint32) (int, bool)` (pure, testable on any OS via the build-tag-free test below).

**CANNOT BE RUN ON macOS.** Verification is `GOOS=windows GOARCH=amd64 go build ./...` plus the pure `povDirection` test. Do not claim runtime verification.

**Global constraint 2 is the whole point of this task:** the cooperative level MUST be `SCL_NONEXCLUSIVE | SCL_BACKGROUND`. `SCL_EXCLUSIVE` anywhere is a defect — it is what would cost Star Citizen its force feedback.

- [ ] **Step 1: Vendor di8**

```bash
mkdir -p internal/joystick/di8
cd internal/joystick/di8
for f in LICENSE constants.go device_windows.go direct_input_windows.go doc.go errors.go structs.go; do
  curl -fsSL "https://raw.githubusercontent.com/gonutz/di8/main/$f" -o "$f"
done
cd ../../..
```

Rename the files with no OS suffix so the whole package is Windows-only — they reference `HWND` and `syscall` types that do not exist elsewhere, and without this the package would break the macOS and Linux builds:

```bash
cd internal/joystick/di8
mv constants.go constants_windows.go
mv errors.go errors_windows.go
mv structs.go structs_windows.go
mv doc.go doc_windows.go
cd ../../..
```

Add `internal/joystick/di8/VENDOR.md`:

```markdown
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
```

- [ ] **Step 2: Write the pure POV test (runs on every platform)**

Create `internal/joystick/pov_test.go`:

```go
package joystick

import "testing"

func TestPovDirection(t *testing.T) {
	// DirectInput reports POV angle in hundredths of a degree, clockwise
	// from north, and a centered hat as -1 (0xFFFFFFFF) -- though some
	// drivers report any value with the high word set.
	cases := []struct {
		raw     uint32
		wantDir int
		wantOK  bool
	}{
		{0, 0, true},         // up
		{4500, 1, true},      // up-right
		{9000, 2, true},      // right
		{13500, 3, true},     // down-right
		{18000, 4, true},     // down
		{22500, 5, true},     // down-left
		{27000, 6, true},     // left
		{31500, 7, true},     // up-left
		{35999, 0, true},     // wraps back to up
		{2200, 0, true},      // rounds down to up
		{2300, 1, true},      // rounds up to up-right
		{0xFFFFFFFF, 0, false}, // centered
		{0xFFFF, 0, false},     // centered, 16-bit form
	}
	for _, c := range cases {
		dir, ok := povDirection(c.raw)
		if ok != c.wantOK {
			t.Errorf("povDirection(%d) ok = %v, want %v", c.raw, ok, c.wantOK)
			continue
		}
		if ok && dir != c.wantDir {
			t.Errorf("povDirection(%d) = %d, want %d", c.raw, dir, c.wantDir)
		}
	}
}
```

- [ ] **Step 3: Write the POV helper in a build-tag-free file**

Create `internal/joystick/pov.go` (no build tag — the test above must run on macOS):

```go
package joystick

// povDirection converts a DirectInput POV reading into one of the 8 hat
// directions. The angle is in hundredths of a degree clockwise from north;
// a centered hat reads as -1, which arrives as 0xFFFFFFFF or 0xFFFF
// depending on the driver, so anything at or above a full circle is treated
// as centered rather than trusting one sentinel.
func povDirection(raw uint32) (int, bool) {
	if raw >= 36000 {
		return 0, false
	}
	// Round to the nearest 45 degrees (4500 hundredths), then wrap 8 -> 0.
	return int((raw+2250)/4500) % 8, true
}
```

- [ ] **Step 4: Run the POV test**

Run: `go test ./internal/joystick/... -run TestPovDirection -v`
Expected: PASS.

- [ ] **Step 5: Write the message-only helper window**

Create `internal/joystick/helperwindow_windows.go`:

```go
//go:build windows

package joystick

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DirectInput's SetCooperativeLevel needs a top-level window handle. We make
// our own message-only window rather than reaching into Wails for the native
// handle: it keeps this package independent of the UI toolkit, and it keeps
// working when the main window is hidden to the tray -- which matters,
// because close-to-tray is a shipped feature.
//
// This mirrors what SDL itself does (SDL_HelperWindow) for the same reason.

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClss = user32.NewProc("RegisterClassExW")
	procCreateWindow = user32.NewProc("CreateWindowExW")
	procDestroyWin   = user32.NewProc("DestroyWindow")
	procDefWindowRaw = user32.NewProc("DefWindowProcW")
	procGetModuleHnd = kernel32.NewProc("GetModuleHandleW")
)

// hwndMessage parents a message-only window: never visible, never in the
// z-order, not enumerated as a top-level window.
const hwndMessage = ^uintptr(2) // (HWND)-3

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type helperWindow struct {
	hwnd syscall.Handle
}

func newHelperWindow() (*helperWindow, error) {
	name, err := syscall.UTF16PtrFromString("VCSJoystickHelper")
	if err != nil {
		return nil, err
	}
	inst, _, _ := procGetModuleHnd.Call(0)

	wc := wndClassExW{
		lpfnWndProc:   procDefWindowRaw.Addr(),
		hInstance:     syscall.Handle(inst),
		lpszClassName: name,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	// A duplicate class registration is not an error for us: the process may
	// have created a helper window before (a Source rebuilt after an error).
	procRegisterClss.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, callErr := procCreateWindow.Call(
		0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0,
		hwndMessage, 0, inst, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("joystick: create helper window: %w", callErr)
	}
	return &helperWindow{hwnd: syscall.Handle(hwnd)}, nil
}

func (h *helperWindow) Close() {
	if h == nil || h.hwnd == 0 {
		return
	}
	procDestroyWin.Call(uintptr(h.hwnd))
	h.hwnd = 0
}
```

- [ ] **Step 6: Write the Windows source**

Create `internal/joystick/source_windows.go`:

```go
//go:build windows

package joystick

import (
	"fmt"
	"strings"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/joystick/di8"
	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// winSource reads joysticks through DirectInput8.
//
// COOPERATIVE LEVEL IS LOad-BEARING. Every device is opened
// SCL_NONEXCLUSIVE | SCL_BACKGROUND:
//
//   - NONEXCLUSIVE because DirectInput allows exactly one exclusive owner per
//     device, and force feedback REQUIRES exclusive access. Taking it here
//     would mean Star Citizen could not have it -- the user would silently
//     lose force feedback on their HOTAS because a voice-comms app was
//     running. SDL takes exclusive unconditionally, which is exactly why we
//     do not use SDL.
//   - BACKGROUND because the whole point is reading push-to-talk while the
//     game has focus.
//
// This is the combination DCS-SRS ships and Microsoft documents as the
// default. If you are changing this line, you are introducing a defect.
type winSource struct {
	mu      sync.Mutex
	di      *di8.DirectInput
	helper  *helperWindow
	devices map[trigger.DeviceID]*winDevice
}

type winDevice struct {
	dev  *di8.Device
	info Device
}

// NewOSSource builds the platform joystick source.
func NewOSSource() (Source, error) {
	helper, err := newHelperWindow()
	if err != nil {
		return nil, err
	}
	di, err := di8.Create()
	if err != nil {
		helper.Close()
		return nil, fmt.Errorf("joystick: create DirectInput: %w", err)
	}
	s := &winSource{di: di, helper: helper, devices: map[trigger.DeviceID]*winDevice{}}
	if _, err := s.Devices(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// sanitiseID reduces a GUID to a legal trigger.DeviceID. Global constraint 7:
// the id must never contain ':' or '+', which are the persisted form's field
// separators.
func sanitiseID(s string) trigger.DeviceID {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if !trigger.ValidDeviceID(out) {
		return "unknown-device"
	}
	return trigger.DeviceID(out)
}

func (s *winSource) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[trigger.DeviceID]bool{}
	var out []Device

	err := s.di.EnumDevices(di8.DEVCLASS_GAMECTRL, func(inst *di8.DEVICEINSTANCE) bool {
		id := sanitiseID(inst.GuidInstance.String())
		seen[id] = true
		if existing, ok := s.devices[id]; ok {
			out = append(out, existing.info)
			return true
		}
		dev, err := s.di.CreateDevice(inst.GuidInstance)
		if err != nil {
			return true // skip this device, keep enumerating
		}
		if err := dev.SetDataFormat(&di8.Joystick2); err != nil {
			dev.Release()
			return true
		}
		// THE line. See the type comment.
		if err := dev.SetCooperativeLevel(s.helper.hwnd, di8.SCL_NONEXCLUSIVE|di8.SCL_BACKGROUND); err != nil {
			dev.Release()
			return true
		}
		if err := dev.Acquire(); err != nil {
			dev.Release()
			return true
		}
		info := Device{
			ID:      id,
			Name:    strings.TrimRight(inst.ProductName(), "\x00"),
			Buttons: int(trigger.MaxButton) + 1,
			Hats:    trigger.HatCount,
		}
		s.devices[id] = &winDevice{dev: dev, info: info}
		out = append(out, info)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("joystick: enumerate devices: %w", err)
	}

	// Drop devices that went away so a replug re-acquires cleanly.
	for id, d := range s.devices {
		if !seen[id] {
			d.dev.Unacquire()
			d.dev.Release()
			delete(s.devices, id)
		}
	}
	return out, nil
}

func (s *winSource) Poll() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := State{
		Connected: map[trigger.DeviceID]bool{},
		Held:      map[trigger.JoyButton]struct{}{},
	}
	for id, d := range s.devices {
		var raw di8.JOYSTATE2
		if err := d.dev.Poll(); err != nil {
			// A lost device is reacquired on the next Devices() pass; for now
			// simply report it absent, which releases anything held on it.
			_ = d.dev.Acquire()
			continue
		}
		if err := d.dev.GetDeviceState(&raw); err != nil {
			_ = d.dev.Acquire()
			continue
		}
		st.Connected[id] = true

		for i, pressed := range raw.Buttons {
			if pressed&0x80 == 0 {
				continue
			}
			if trigger.Button(i) > trigger.MaxButton {
				break
			}
			st.Held[trigger.JoyButton{Device: id, Button: trigger.Button(i)}] = struct{}{}
		}
		for hat, angle := range raw.POV {
			if hat >= trigger.HatCount {
				break
			}
			dir, ok := povDirection(angle)
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{
				Device: id,
				Button: trigger.HatButton(hat, dir),
			}] = struct{}{}
		}
	}
	return st, nil
}

func (s *winSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		d.dev.Unacquire()
		d.dev.Release()
		delete(s.devices, id)
	}
	if s.di != nil {
		s.di.Release()
		s.di = nil
	}
	s.helper.Close()
}
```

**If the vendored di8 API differs from the calls above** (method names, `EnumDevices` callback signature, `DEVICEINSTANCE` field names, `ProductName()`), adapt the call sites to whatever the vendored source actually exposes — read `internal/joystick/di8/*.go` and follow it. Do **not** change the cooperative-level flags to make something compile.

- [ ] **Step 7: Verify it builds for Windows**

```bash
GOOS=windows GOARCH=amd64 go build ./...
go test ./internal/joystick/... -run TestPovDirection -v
```
Expected: build succeeds; POV test passes.

- [ ] **Step 8: Grep-verify the constraint**

```bash
grep -rn "SCL_EXCLUSIVE\|DISCL_EXCLUSIVE" internal/ --include=*.go | grep -v "_test.go" | grep -v "di8/constants_windows.go"
```
Expected: **no output**. A hit outside di8's own constant table means global constraint 2 has been violated.

- [ ] **Step 9: Commit**

```bash
git add internal/joystick/
git commit -m "$(cat <<'EOF'
feat(joystick): vendor di8 and add the Windows DirectInput backend

Opens every device SCL_NONEXCLUSIVE | SCL_BACKGROUND -- the combination
DCS-SRS ships and Microsoft documents as the default. Exclusive access is
required for force feedback and only one process can hold it, so taking
it here would silently cost Star Citizen its force feedback. That is why
SDL, which takes exclusive unconditionally, is not used.

di8 is vendored rather than depended on: single-author, no other known
users, critical input path. DirectInput8 is frozen since 2005 so a thin
syscall wrapper cannot rot.

Cooperative level needs a top-level HWND, so we create our own
message-only window rather than reaching into Wails -- it survives
close-to-tray and keeps this package independent of the UI toolkit.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Linux evdev backend and the macOS / fallback stubs

**Files:**
- Create: `internal/joystick/source_linux.go`
- Create: `internal/joystick/source_darwin.go`
- Create: `internal/joystick/source_other.go`
- Modify: `go.mod`, `go.sum` (add `github.com/holoplot/go-evdev`)

**Interfaces:**
- Consumes: Task 5's `Source`, `Device`, `State`, `ErrUnsupported`; `trigger` types.
- Produces: `joystick.NewOSSource() (Source, error)` on every remaining platform.

**CANNOT BE RUN ON macOS beyond the stub.** Verification is cross-compilation for all three GOOS values.

**Global constraint 10 depends on this task:** the Linux filter must open **only joystick-like devices**. Opening a keyboard through evdev would make the joystick log lines a keylog and break the rule that permits them.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/holoplot/go-evdev@latest
go mod tidy
```

- [ ] **Step 2: Write `internal/joystick/source_darwin.go`**

```go
//go:build darwin

package joystick

// macOS has no backend, by decision rather than omission.
//
// Star Citizen has no macOS build and none is planned, so there is no game to
// talk over. Reading HID on macOS would also need IOHIDManager, which is
// gated behind the Input Monitoring TCC permission -- a SECOND, different
// grant from the Accessibility permission the keyboard hotkey path already
// has to negotiate. Spending a second permission prompt on a platform that
// cannot run the game is poor value.
//
// This reports UNSUPPORTED, never DENIED. The distinction matters to the UI:
// denied earns a grant affordance, unsupported must not, because there is
// nothing the user can do about it.
func NewOSSource() (Source, error) { return unsupportedSrc{}, nil }

type unsupportedSrc struct{}

func (unsupportedSrc) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (unsupportedSrc) Poll() (State, error)       { return State{}, ErrUnsupported }
func (unsupportedSrc) Close()                     {}
```

- [ ] **Step 3: Write `internal/joystick/source_other.go`**

```go
//go:build !windows && !linux && !darwin

package joystick

// No backend on this platform. Same contract as darwin: unsupported, not
// denied.
func NewOSSource() (Source, error) { return otherUnsupportedSrc{}, nil }

type otherUnsupportedSrc struct{}

func (otherUnsupportedSrc) Devices() ([]Device, error) { return nil, ErrUnsupported }
func (otherUnsupportedSrc) Poll() (State, error)       { return State{}, ErrUnsupported }
func (otherUnsupportedSrc) Close()                     {}
```

- [ ] **Step 4: Write `internal/joystick/source_linux.go`**

```go
//go:build linux

package joystick

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	evdev "github.com/holoplot/go-evdev"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// linuxSource reads joysticks through evdev.
//
// Reading evdev is inherently non-exclusive -- we never EVIOCGRAB -- so a game
// running under Proton reads the same devices undisturbed. That is the same
// property the Windows backend has to work for, and here it is free.
//
// DEVICE FILTER. Only devices that look like joysticks are opened. This is a
// correctness requirement, not a tidiness one: the manager logs which button
// fired an action, which is safe precisely because a joystick backend cannot
// observe typing. Opening a keyboard here would turn those log lines into a
// keylog.
type linuxSource struct {
	mu      sync.Mutex
	devices map[trigger.DeviceID]*linuxDevice
}

type linuxDevice struct {
	dev  *evdev.InputDevice
	info Device
	// hatAxes maps an ABS_HAT* axis code to its hat index.
	hatAxes map[evdev.EvCode]int
	// buttons maps an EV_KEY code to our button index, in the order the
	// device declares them -- BTN_TRIGGER, BTN_THUMB, ... are not contiguous.
	buttons map[evdev.EvCode]trigger.Button
}

// NewOSSource builds the platform joystick source.
func NewOSSource() (Source, error) {
	s := &linuxSource{devices: map[trigger.DeviceID]*linuxDevice{}}
	if _, err := s.Devices(); err != nil {
		return nil, err
	}
	return s, nil
}

// looksLikeJoystick reports whether the device declares joystick-shaped
// capabilities: an absolute X axis plus at least one gamepad/joystick button.
// See the type comment for why this filter is load-bearing.
func looksLikeJoystick(d *evdev.InputDevice) bool {
	absCodes := d.CapableEvents(evdev.EV_ABS)
	hasAbsX := false
	for _, c := range absCodes {
		if c == evdev.ABS_X {
			hasAbsX = true
			break
		}
	}
	if !hasAbsX {
		return false
	}
	for _, c := range d.CapableEvents(evdev.EV_KEY) {
		if c >= evdev.BTN_JOYSTICK && c <= evdev.BTN_GAMEPAD+0x7f {
			return true
		}
	}
	return false
}

// stableID prefers the /dev/input/by-id symlink, which carries the device
// model and serial and therefore survives a replug. It falls back to the
// device name when no symlink exists.
func stableID(path, name string) trigger.DeviceID {
	const byID = "/dev/input/by-id"
	entries, err := os.ReadDir(byID)
	if err == nil {
		for _, e := range entries {
			link := filepath.Join(byID, e.Name())
			if target, err := filepath.EvalSymlinks(link); err == nil && target == path {
				return sanitiseLinuxID(e.Name())
			}
		}
	}
	return sanitiseLinuxID(name)
}

// sanitiseLinuxID enforces global constraint 7: ':' and '+' can never appear
// in a DeviceID, because they are the persisted form's field separators.
func sanitiseLinuxID(s string) trigger.DeviceID {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if !trigger.ValidDeviceID(out) {
		return "unknown-device"
	}
	return trigger.DeviceID(out)
}

func (s *linuxSource) Devices() ([]Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	paths, err := evdev.ListDevicePaths()
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, permissionError(err)
		}
		return nil, fmt.Errorf("joystick: list input devices: %w", err)
	}

	seen := map[trigger.DeviceID]bool{}
	var out []Device
	var permErr error

	for _, p := range paths {
		dev, err := evdev.Open(p.Path)
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				permErr = permissionError(err)
			}
			continue
		}
		if !looksLikeJoystick(dev) {
			dev.Close()
			continue
		}
		name, _ := dev.Name()
		id := stableID(p.Path, name)
		if seen[id] {
			dev.Close()
			continue
		}
		seen[id] = true

		if existing, ok := s.devices[id]; ok {
			dev.Close()
			out = append(out, existing.info)
			continue
		}

		ld := &linuxDevice{
			dev:     dev,
			hatAxes: map[evdev.EvCode]int{},
			buttons: map[evdev.EvCode]trigger.Button{},
		}
		var next trigger.Button
		for _, c := range dev.CapableEvents(evdev.EV_KEY) {
			if next > trigger.MaxButton {
				break
			}
			ld.buttons[c] = next
			next++
		}
		hat := 0
		for _, c := range dev.CapableEvents(evdev.EV_ABS) {
			if c >= evdev.ABS_HAT0X && c <= evdev.ABS_HAT3Y && hat < trigger.HatCount {
				ld.hatAxes[c] = int(c-evdev.ABS_HAT0X) / 2
				hat++
			}
		}
		ld.info = Device{
			ID:      id,
			Name:    name,
			Buttons: len(ld.buttons),
			Hats:    hat,
		}
		s.devices[id] = ld
		out = append(out, ld.info)
	}

	for id, d := range s.devices {
		if !seen[id] {
			d.dev.Close()
			delete(s.devices, id)
		}
	}

	if len(out) == 0 && permErr != nil {
		return nil, permErr
	}
	return out, nil
}

// permissionError names the remedy. /dev/input/event* is typically
// 0600 root:root, so a user who has never played a game under Steam may
// simply not be in the input group. Deliberately NOT routed through the
// keyboard path's Accessibility messaging: different cause, different fix.
func permissionError(err error) error {
	return fmt.Errorf(
		"joystick: cannot read /dev/input (add your user to the 'input' group, then log out and back in): %w",
		err)
}

func (s *linuxSource) Poll() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := State{
		Connected: map[trigger.DeviceID]bool{},
		Held:      map[trigger.JoyButton]struct{}{},
	}
	for id, d := range s.devices {
		st.Connected[id] = true

		keys, err := d.dev.State(evdev.EV_KEY)
		if err != nil {
			continue
		}
		for code, pressed := range keys {
			if !pressed {
				continue
			}
			btn, ok := d.buttons[code]
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{Device: id, Button: btn}] = struct{}{}
		}

		for code, hat := range d.hatAxes {
			val, err := d.dev.AbsState(code)
			if err != nil || val.Value == 0 {
				continue
			}
			// Even ABS_HAT codes are the X axis, odd are Y. Convert the
			// -1/0/+1 pair into one of the 8 clockwise-from-up directions.
			dir, ok := hatDirection(code, val.Value)
			if !ok {
				continue
			}
			st.Held[trigger.JoyButton{Device: id, Button: trigger.HatButton(hat, dir)}] = struct{}{}
		}
	}
	return st, nil
}

// hatDirection maps one hat axis deflection to a direction index. Diagonals
// are not represented: evdev reports X and Y separately, so a diagonal press
// registers as two directions held at once, which is the behaviour a user
// binding "hat up" expects anyway.
func hatDirection(code evdev.EvCode, value int32) (int, bool) {
	isX := (code-evdev.ABS_HAT0X)%2 == 0
	switch {
	case isX && value > 0:
		return 2, true // right
	case isX && value < 0:
		return 6, true // left
	case !isX && value < 0:
		return 0, true // up
	case !isX && value > 0:
		return 4, true // down
	}
	return 0, false
}

func (s *linuxSource) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, d := range s.devices {
		d.dev.Close()
		delete(s.devices, id)
	}
}
```

**If `holoplot/go-evdev`'s API differs** (`ListDevicePaths`, `Open`, `CapableEvents`, `State`, `AbsState`, `Name`, constant names), adapt to the real API — read the package source in the module cache and follow it. Keep the joystick-only filter and the permission message intact whatever the API shape turns out to be.

- [ ] **Step 5: Verify every platform builds**

```bash
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go build ./...
GOOS=darwin  GOARCH=arm64 go build ./...
go test -race ./internal/joystick/... -v
```
Expected: three clean builds; all joystick tests pass on darwin (the darwin stub plus every pure-logic test).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/joystick/
git commit -m "$(cat <<'EOF'
feat(joystick): add the Linux evdev backend and platform stubs

evdev reading is inherently non-exclusive (we never EVIOCGRAB), so a game
under Proton reads the same devices undisturbed.

Only joystick-shaped devices are opened. That filter is a correctness
requirement, not tidiness: the manager logs which button fired an action,
which is safe only because a joystick backend cannot observe typing.

macOS reports unsupported rather than denied -- there is no Star Citizen
build for it, and IOHIDManager would need a second TCC permission
distinct from the keyboard path's Accessibility grant. Unsupported earns
no grant affordance because the user cannot act on it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Per-action press refcount

**Files:**
- Create: `internal/app/presscount.go`
- Test: `internal/app/presscount_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `pressCount` (unexported), `newPressCount() *pressCount`, `(*pressCount).press(string) bool`, `(*pressCount).release(string) bool`, `(*pressCount).forget(string)`, `(*pressCount).reset()`.

**Why this exists.** `App.Pressed` / `App.Released` currently emit straight through. Once an action can be held from the keyboard *and* a joystick at the same time, releasing one source emits `HotkeyReleased` while the other is still held — **cutting the user's push-to-talk off while their finger is still down**. That is a silent failure, not a crash, so it gets its own task and its own tests.

**It applies only to HOLD actions.** Press-kind actions never receive a `Released` from either manager (`internal/hotkeys/dispatch.go` returns a pending release only when `b.hold`, and Task 6 mirrors that), so refcounting them would leave the count stuck at 1 and silently deaden the action after its first press. Task 11 does the hold lookup; this type just counts.

- [ ] **Step 1: Write the failing test**

Create `internal/app/presscount_test.go`:

```go
package app

import (
	"sync"
	"testing"
)

func TestPressCountSingleSource(t *testing.T) {
	p := newPressCount()
	if !p.press("global.ptt") {
		t.Error("first press = false, want true (0->1 must emit)")
	}
	if !p.release("global.ptt") {
		t.Error("matching release = false, want true (1->0 must emit)")
	}
}

func TestPressCountTwoSourcesEmitOnce(t *testing.T) {
	// The defect this type exists to prevent: keyboard and joystick both
	// holding global.ptt must produce exactly one down and one up.
	p := newPressCount()

	if !p.press("global.ptt") {
		t.Fatal("keyboard press = false, want true")
	}
	if p.press("global.ptt") {
		t.Error("joystick press = true, want false: the action is already held")
	}
	if p.release("global.ptt") {
		t.Error("releasing ONE source emitted a release while the other still holds it")
	}
	if !p.release("global.ptt") {
		t.Error("releasing the last source = false, want true")
	}
}

func TestPressCountNeverGoesNegative(t *testing.T) {
	// An unbalanced Released (a manager bug, a forced release after the
	// action was already let go) must not make the NEXT press silent.
	p := newPressCount()
	if p.release("global.ptt") {
		t.Error("release with nothing held = true, want false")
	}
	if !p.press("global.ptt") {
		t.Error("press after a spurious release = false, want true")
	}
}

func TestPressCountActionsAreIndependent(t *testing.T) {
	p := newPressCount()
	p.press("global.ptt")
	if !p.press("radio.1.ptt") {
		t.Error("a different action was affected by the first action's count")
	}
}

func TestForgetClearsALeakedCount(t *testing.T) {
	// Rebinding an action drops its held state; without forget, a count left
	// behind by a missed release would deaden the action for the session.
	p := newPressCount()
	p.press("global.ptt")
	p.forget("global.ptt")
	if !p.press("global.ptt") {
		t.Error("press after forget = false, want true")
	}
}

func TestPressCountIsRaceFree(t *testing.T) {
	// Both managers call into this from their own goroutines.
	p := newPressCount()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				p.press("global.ptt")
				p.release("global.ptt")
			}
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/... -run TestPressCount`
Expected: FAIL — `newPressCount` undefined.

- [ ] **Step 3: Write `internal/app/presscount.go`**

```go
package app

import "sync"

// pressCount joins several input sources into one press/release edge per
// action.
//
// An action can be held from the keyboard and a joystick at the same time.
// Without this, releasing either source would emit HotkeyReleased while the
// other was still held -- cutting the user's push-to-talk off mid-sentence.
// So: emit on 0->1 and on 1->0, and swallow everything in between.
//
// Counts HOLD actions only. Press-kind actions never receive a Released from
// either manager, so counting them would strand the count at 1 and silently
// deaden the action after its first press. The caller does that filtering.
type pressCount struct {
	mu sync.Mutex
	n  map[string]int
}

func newPressCount() *pressCount {
	return &pressCount{n: map[string]int{}}
}

// press records a source taking the action and reports whether this is the
// transition that should be emitted (0 -> 1).
func (p *pressCount) press(actionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n[actionID]++
	return p.n[actionID] == 1
}

// release records a source letting go and reports whether this is the
// transition that should be emitted (1 -> 0).
//
// A release with nothing held is ignored rather than going negative: an
// unbalanced Released from a source would otherwise make the NEXT genuine
// press silent, which is exactly the class of bug this type exists to stop.
func (p *pressCount) release(actionID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.n[actionID] <= 0 {
		delete(p.n, actionID)
		return false
	}
	p.n[actionID]--
	if p.n[actionID] == 0 {
		delete(p.n, actionID)
		return true
	}
	return false
}

// forget drops any held state for actionID without emitting. Used when an
// action is rebound, so a count left behind by a missed release cannot
// deaden it for the rest of the session.
func (p *pressCount) forget(actionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.n, actionID)
}

// reset drops every held count.
func (p *pressCount) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n = map[string]int{}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/app/... -run TestPressCount -v`
Expected: PASS, all six.

- [ ] **Step 5: Prove the two-source test bites**

Temporarily make `press` always return `true`. Re-run: `TestPressCountTwoSourcesEmitOnce` MUST fail. Restore.

- [ ] **Step 6: Commit**

```bash
git add internal/app/presscount.go internal/app/presscount_test.go
git commit -m "$(cat <<'EOF'
feat(app): add a per-action press refcount

An action can now be held from the keyboard and a joystick at once.
Without a refcount, releasing either source emits HotkeyReleased while
the other is still held -- cutting push-to-talk off mid-sentence. Emit on
0->1 and 1->0 only.

A release with nothing held is ignored rather than going negative: an
unbalanced Released would otherwise silence the next genuine press.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: App layer — DTOs, trigger CRUD, dual-manager capture

**Files:**
- Modify: `internal/app/dto.go:99-129` (`CaptureDTO` kept; `KeybindDTO`, `StolenDTO` changed)
- Modify: `internal/app/settings.go` (`settingsBackend`, `GetKeybinds`, `SetKeybind` → `AddTrigger`, new `RemoveTrigger`, `ClearKeybind`, `BeginCapture`, `EndCapture`, `persistKeybinds`, `applyHotkeys`, `Pressed`, `Released`)
- Modify: `internal/app/events.go` (add the joystick-captured event)
- Test: `internal/app/settings_test.go` (extend; keep every existing test)

**Interfaces:**
- Consumes: `keybinds.Store.Add/RemoveAt/Get/All/Load/Snapshot`, `keybinds.Stolen`; `trigger.Trigger/Parse/String`; `joystick.Manager/Binding/Device/Captured`; `pressCount` (Task 10); `config.KeybindValue`.
- Produces: `app.TriggerDTO`, `KeybindDTO.Triggers []TriggerDTO`, `App.AddTrigger(string, CaptureDTO) (SetKeybindResult, error)`, `App.RemoveTrigger(string, int) error`, `App.BeginCapture(actionID string) int64`, `App.SetJoystickBackend(*joystick.Manager)`, `App.GetJoystickState() JoystickStateDTO`.

**Design notes the implementer needs:**

1. **`BeginCapture` gains an `actionID` parameter.** Keyboard capture still round-trips through the frontend (`AddTrigger`), but joystick capture completes in the backend — the manager already knows the binding, and bouncing it to the frontend only to be sent back adds a race for nothing. So `BeginCapture` tells the backend which action a completed joystick capture belongs to.
2. **`BeginCapture` must suspend BOTH managers** (spec §9). Without this, binding a joystick button transmits while you bind it.
3. **`applyHotkeys` splits triggers by kind** and calls `hotkeys.Manager.Apply` with the keyboard ones and `joystick.Manager.Apply` with the joystick ones.
4. **`Pressed`/`Released` consult the refcount, but only for hold actions.**
5. **The joystick manager is optional.** Every existing test constructs a settings backend without one, so every call site must tolerate `sb.joy == nil`.

- [ ] **Step 1: Write the failing test**

Add to `internal/app/settings_test.go`:

```go
func TestGetKeybindsReturnsAllTriggers(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatalf("AddTrigger: %v", err)
	}
	rows := a.GetKeybinds()
	var got *KeybindDTO
	for i := range rows {
		if rows[i].ActionID == "global.ptt" {
			got = &rows[i]
		}
	}
	if got == nil {
		t.Fatal("global.ptt missing from GetKeybinds")
	}
	if len(got.Triggers) != 1 {
		t.Fatalf("Triggers = %+v, want one", got.Triggers)
	}
	if got.Triggers[0].Kind != "key" || got.Triggers[0].Chord != "F1" {
		t.Errorf("trigger = %+v, want key/F1", got.Triggers[0])
	}
}

func TestRemoveTriggerDropsOnlyThatOne(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	a.settings.kb.Add("global.ptt", trigger.Joy(trigger.JoyBinding{Device: "stick-c3", Button: 11}))

	if err := a.RemoveTrigger("global.ptt", 0); err != nil {
		t.Fatalf("RemoveTrigger: %v", err)
	}
	list, _ := a.settings.kb.Get("global.ptt")
	if len(list) != 1 || list[0].Kind != trigger.KindJoy {
		t.Errorf("after RemoveTrigger = %+v, want the joystick trigger only", list)
	}
}

func TestRemoveTriggerOutOfRangeErrors(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"})
	if err := a.RemoveTrigger("global.ptt", 5); err == nil {
		t.Error("RemoveTrigger(5) = nil error, want out-of-range failure")
	}
}

func TestHoldActionHeldByTwoSourcesEmitsOneEdgePair(t *testing.T) {
	// The PTT-cut defect, end to end through App.Pressed/Released.
	a, em, _ := newTestApp(t)
	a.AddTrigger("global.ptt", CaptureDTO{Code: "F1"}) // global.ptt is KindHold

	a.Pressed("global.ptt")  // keyboard
	a.Pressed("global.ptt")  // joystick
	a.Released("global.ptt") // keyboard lets go; joystick still held

	if n := em.count(events.EventHotkeyPressed); n != 1 {
		t.Errorf("HotkeyPressed emitted %d times, want 1", n)
	}
	if n := em.count(events.EventHotkeyReleased); n != 0 {
		t.Errorf("HotkeyReleased emitted while the joystick still held it (%d times)", n)
	}

	a.Released("global.ptt") // joystick lets go
	if n := em.count(events.EventHotkeyReleased); n != 1 {
		t.Errorf("HotkeyReleased emitted %d times, want 1", n)
	}
}

func TestPressKindActionIsNotRefcounted(t *testing.T) {
	// global.mute_toggle is KindPress and never receives a Released, so
	// refcounting it would silence every press after the first.
	a, em, _ := newTestApp(t)
	a.Pressed("global.mute_toggle")
	a.Pressed("global.mute_toggle")
	if n := em.count(events.EventHotkeyPressed); n != 2 {
		t.Errorf("press-kind action emitted %d times, want 2", n)
	}
}

func TestBeginCaptureSuspendsBothManagers(t *testing.T) {
	a, _, _ := newTestApp(t)
	joySrc := newFakeJoySource()
	jm := joystick.New(joySrc, a, slog.New(slog.NewTextHandler(io.Discard, nil)))
	a.SetJoystickBackend(jm)

	token := a.BeginCapture("global.ptt")
	if token == 0 {
		t.Fatal("BeginCapture returned no token")
	}
	if !a.settings.hk.Suspended() {
		t.Error("keyboard manager not suspended during capture")
	}
	if !jm.IsSuspendedForTest() {
		t.Error("joystick manager not suspended during capture")
	}

	a.EndCapture(token)
	if jm.IsSuspendedForTest() {
		t.Error("joystick manager still suspended after EndCapture")
	}
}
```

**Test-helper facts, already checked against the tree — do not re-derive them:**

- The setup helper is `newTestApp(t) (*App, *recordingEmitter, *countingRegistrar)` in `internal/app/settings_test.go:113`. There is no `newTestAppWithSettings`.
- `recordingEmitter` already has `count(name string) int` (`settings_test.go:47`). Use it with the event-name constants; do **not** add per-action counters. The tests above press only one action, so a plain count is exact.
- Event names live in `internal/events`: `events.EventHotkeyPressed`, `events.EventHotkeyReleased`. Import that package in the test.
- **`hotkeys.Manager.Suspended()` already exists** (`internal/hotkeys/hotkeys.go:216`). Do **not** add one.

Still to add: `newFakeJoySource` (a `joystick.Source` returning one device and no held buttons) and `IsSuspendedForTest()` on `joystick.Manager` — a read-only accessor, which changes no behaviour.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/app/...`
Expected: FAIL — `AddTrigger`, `RemoveTrigger`, `TriggerDTO` undefined.

- [ ] **Step 3: Update the DTOs in `internal/app/dto.go`**

Replace `KeybindDTO` and `StolenDTO` (lines 109-129) with:

```go
// TriggerDTO is one way to activate an action, as the frontend sees it.
//
// Display labels are rendered HERE, in Go, not in the frontend: the physical
// naming of buttons and hats has exactly one home, the same discipline that
// keeps canonical chord formatting inside internal/chord.
type TriggerDTO struct {
	Kind string `json:"kind"` // "key" | "joy"
	// Chord is the canonical chord form; set only when Kind == "key".
	Chord string `json:"chord"`
	// Device is the stable device id; set only when Kind == "joy".
	Device string `json:"device"`
	// DeviceName is the product name for display. Falls back to Device when
	// the device has never been seen.
	DeviceName string `json:"device_name"`
	// Label is the rendered input name, e.g. "Btn 12", "Hat 1 ↑",
	// "Btn 5 + Btn 3".
	Label string `json:"label"`
	// Connected reports whether the device is attached right now. A binding
	// for an absent device is still valid and renders muted rather than
	// vanishing.
	Connected bool `json:"connected"`
}

// KeybindDTO is one row of the joined action-registry + bound-trigger list.
type KeybindDTO struct {
	ActionID string       `json:"action_id"`
	Label    string       `json:"label"`
	Desc     string       `json:"desc"`
	Category string       `json:"category"` // "global" | "channel" | "per_radio" | "status"
	Kind     string       `json:"kind"`     // "hold" | "press"
	Triggers []TriggerDTO `json:"triggers"`
}

// StolenDTO reports which action lost a trigger to a new binding.
type StolenDTO struct {
	ActionID string     `json:"action_id"`
	Label    string     `json:"label"`
	Trigger  TriggerDTO `json:"trigger"`
}

// SetKeybindResult is the result of AddTrigger.
type SetKeybindResult struct {
	Stolen *StolenDTO `json:"stolen"` // nil when there was no conflict
}

// JoystickStateDTO reports the joystick subsystem's health.
type JoystickStateDTO struct {
	// Supported is false where there is no backend (macOS). The UI must hide
	// the joystick affordance rather than showing a broken one, and must NOT
	// offer a permission grant: unsupported is not denied.
	Supported bool `json:"supported"`
	// Error is the most recent source error, "" when healthy. On Linux this
	// carries the actionable 'input' group message.
	Error string `json:"error"`
	// Devices is the attached device list, for display.
	Devices []JoystickDeviceDTO `json:"devices"`
}

// JoystickDeviceDTO is one attached device.
type JoystickDeviceDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

- [ ] **Step 4: Update `internal/app/settings.go`**

Add to `settingsBackend`:

```go
	// joy is the joystick manager. Optional: tests and any build without a
	// backend leave it nil, so every use site must check.
	joy *joystick.Manager
	// presses joins keyboard and joystick edges into one press/release pair
	// per action. See presscount.go.
	presses *pressCount
	// holds records which actions owe a Released (Kind == KindHold),
	// refreshed by applyHotkeys. Press-kind actions must NOT be refcounted.
	holds map[string]bool
	// deviceNames remembers product names so a binding for an absent device
	// is still nameable in the UI.
	deviceNames map[string]string
	// captureAction is the action a completed joystick capture binds to.
	captureAction string
```

Initialise `presses: newPressCount()`, `holds: map[string]bool{}`, `deviceNames: map[string]string{}` in `SetSettingsBackend`.

Add the joystick backend setter:

```go
// SetJoystickBackend wires the joystick manager. Optional -- when it is never
// called, every joystick path degrades to "unsupported" and the keyboard
// behaviour is exactly what it was before this feature existed.
func (a *App) SetJoystickBackend(jm *joystick.Manager) {
	sb := a.settings
	sb.mu.Lock()
	sb.joy = jm
	sb.mu.Unlock()
	a.applyHotkeys()
	jm.Start()
}
```

Replace `GetKeybinds`:

```go
// GetKeybinds joins the static + per-radio action registry with the store's
// live triggers into the frontend-facing row list.
func (a *App) GetKeybinds() []KeybindDTO {
	sb := a.settings
	actions := a.keybindActions()
	connected := a.connectedDevices()

	out := make([]KeybindDTO, 0, len(actions))
	for _, act := range actions {
		list, _ := sb.kb.Get(act.ID)
		triggers := make([]TriggerDTO, 0, len(list))
		for _, t := range list {
			triggers = append(triggers, a.triggerDTO(t, connected))
		}
		out = append(out, KeybindDTO{
			ActionID: string(act.ID),
			Label:    act.Label,
			Desc:     act.Desc,
			Category: categoryString(act.Category),
			Kind:     kindString(act.Kind),
			Triggers: triggers,
		})
	}
	return out
}

// connectedDevices returns the currently-attached device ids, and refreshes
// the remembered product names as a side effect so an absent device stays
// nameable later.
func (a *App) connectedDevices() map[string]bool {
	sb := a.settings
	out := map[string]bool{}
	sb.mu.Lock()
	jm := sb.joy
	sb.mu.Unlock()
	if jm == nil {
		return out
	}
	for _, d := range jm.Devices() {
		out[string(d.ID)] = true
		sb.mu.Lock()
		sb.deviceNames[string(d.ID)] = d.Name
		sb.mu.Unlock()
	}
	return out
}

// triggerDTO renders one trigger for the frontend.
func (a *App) triggerDTO(t trigger.Trigger, connected map[string]bool) TriggerDTO {
	if t.Kind == trigger.KindKey {
		return TriggerDTO{Kind: "key", Chord: t.Key.String(), Label: t.Key.String(), Connected: true}
	}
	sb := a.settings
	dev := string(t.Joy.Device)
	sb.mu.Lock()
	name := sb.deviceNames[dev]
	sb.mu.Unlock()
	if name == "" {
		name = dev
	}
	label := t.Joy.Button.Label()
	if t.Joy.Modifier != nil {
		label = t.Joy.Modifier.Button.Label() + " + " + label
	}
	return TriggerDTO{
		Kind:       "joy",
		Device:     dev,
		DeviceName: name,
		Label:      label,
		Connected:  connected[dev],
	}
}
```

Rename `SetKeybind` to `AddTrigger` and change the store call from `Set` to `Add`; everything else about it (the `writeMu` discipline, the rollback on persist failure, the emit) stays exactly as it is. The `stolen` conversion becomes:

```go
	result := SetKeybindResult{}
	if stolen != nil {
		result.Stolen = &StolenDTO{
			ActionID: string(stolen.ActionID),
			Label:    a.labelFor(stolen.ActionID),
			Trigger:  a.triggerDTO(stolen.Trigger, a.connectedDevices()),
		}
	}
```

Add `RemoveTrigger`, mirroring `ClearKeybind`'s rollback discipline exactly:

```go
// RemoveTrigger drops one trigger from actionID, persists, re-applies, and
// emits keybinds:changed. If persistence fails the store is rolled back, so
// it never disagrees with disk.
func (a *App) RemoveTrigger(actionID string, index int) error {
	sb := a.settings
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	before := sb.kb.Snapshot()
	if err := sb.kb.RemoveAt(keybinds.ActionID(actionID), index); err != nil {
		return err
	}
	if err := a.persistKeybinds(); err != nil {
		sb.kb.Load(before)
		return err
	}
	a.applyHotkeys()
	sb.em.KeybindsChanged(a.GetKeybinds())
	return nil
}
```

Replace `applyHotkeys`:

```go
// applyHotkeys re-applies the full desired binding set to BOTH OS layers from
// the current keybind store contents, splitting triggers by kind.
func (a *App) applyHotkeys() {
	sb := a.settings
	kbBinds := map[string]hotkeys.Binding{}
	joyBinds := map[string][]joystick.Binding{}
	holds := map[string]bool{}

	for _, act := range a.keybindActions() {
		id := string(act.ID)
		hold := act.Kind == keybinds.KindHold
		holds[id] = hold

		list, ok := sb.kb.Get(act.ID)
		if !ok {
			continue
		}
		for _, t := range list {
			switch t.Kind {
			case trigger.KindKey:
				if t.Key.IsZero() {
					continue
				}
				kbBinds[id] = hotkeys.Binding{Chord: t.Key, Hold: hold}
			case trigger.KindJoy:
				joyBinds[id] = append(joyBinds[id], joystick.Binding{Joy: t.Joy, Hold: hold})
			}
		}
	}

	sb.mu.Lock()
	sb.holds = holds
	jm := sb.joy
	sb.mu.Unlock()

	// A rebind drops whatever was held, and both managers emit the releases
	// they owe. Clearing the refcount here as well means a release that got
	// lost in the shuffle cannot leave an action permanently "held" and
	// therefore permanently silent.
	sb.presses.reset()

	_ = sb.hk.Apply(kbBinds) // failures surface via the event below
	if jm != nil {
		_ = jm.Apply(joyBinds)
	}
	a.emitHotkeyState()
}
```

Replace `Pressed` and `Released` (keeping their existing doc comments about logging, and adding the refcount):

```go
func (a *App) Pressed(actionID string) {
	if a.settings == nil {
		return
	}
	if a.isHold(actionID) && !a.settings.presses.press(actionID) {
		return // already held by another input source
	}
	a.logger.Info("hotkey fired", "action", actionID, "edge", "down")
	a.settings.em.HotkeyPressed(actionID)
}

func (a *App) Released(actionID string) {
	if a.settings == nil {
		return
	}
	if a.isHold(actionID) && !a.settings.presses.release(actionID) {
		return // another input source still holds it
	}
	a.logger.Info("hotkey fired", "action", actionID, "edge", "up")
	a.settings.em.HotkeyReleased(actionID)
}

// isHold reports whether actionID owes a Released. Only hold actions are
// refcounted: press-kind actions never receive a Released from either
// manager, so counting them would strand the count at 1 and silently deaden
// the action after its first press.
func (a *App) isHold(actionID string) bool {
	sb := a.settings
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.holds[actionID]
}
```

Change `BeginCapture` to take the action id, suspend both managers, and arm joystick capture. Keep its entire existing doc comment about the capture token — it is still exactly right — and add:

```go
func (a *App) BeginCapture(actionID string) int64 {
	sb := a.settings
	sb.mu.Lock()
	sb.captureGen++
	token := sb.captureGen
	sb.captureAction = actionID
	jm := sb.joy
	sb.mu.Unlock()

	// ... existing suspend of sb.hk and the auto-resume timer, unchanged ...

	if jm != nil {
		jm.Suspend()
		jm.BeginCapture(func(c joystick.Captured) {
			a.onJoystickCaptured(token, c)
		})
	}
	return token
}

// onJoystickCaptured binds a completed joystick capture. Unlike keyboard
// capture, this does not round-trip through the frontend: the manager
// already holds the binding, and bouncing it out only to be sent back would
// add a race for nothing.
//
// The token is checked for the same reason EndCapture checks it -- a capture
// superseded by the user clicking another row must not bind anything.
func (a *App) onJoystickCaptured(token int64, c joystick.Captured) {
	sb := a.settings
	sb.mu.Lock()
	stale := token != sb.captureGen
	actionID := sb.captureAction
	sb.mu.Unlock()
	if stale || actionID == "" {
		return
	}

	sb.writeMu.Lock()
	before := sb.kb.Snapshot()
	stolen := sb.kb.Add(keybinds.ActionID(actionID), trigger.Joy(c.Binding))
	if err := a.persistKeybinds(); err != nil {
		sb.kb.Load(before)
		sb.writeMu.Unlock()
		a.logger.Warn("could not persist joystick binding", "action", actionID, "err", err)
		return
	}
	a.applyHotkeys()
	sb.em.KeybindsChanged(a.GetKeybinds())
	sb.em.JoystickCaptured(actionID)
	sb.writeMu.Unlock()

	if stolen != nil {
		a.logger.Info("joystick binding stolen from another action",
			"from", string(stolen.ActionID), "to", actionID)
	}
}
```

In `EndCapture` and `resumeCapture`, wherever `sb.hk.Resume()` is called, also cancel and resume the joystick side:

```go
	if jm != nil {
		jm.CancelCapture()
		jm.Resume()
	}
```

Add `GetJoystickState`:

```go
// GetJoystickState reports the joystick subsystem's health for the UI.
func (a *App) GetJoystickState() JoystickStateDTO {
	sb := a.settings
	sb.mu.Lock()
	jm := sb.joy
	sb.mu.Unlock()
	if jm == nil {
		return JoystickStateDTO{Supported: false}
	}
	out := JoystickStateDTO{Supported: jm.Supported()}
	if err := jm.LastErr(); err != nil && jm.Supported() {
		out.Error = err.Error()
	}
	for _, d := range jm.Devices() {
		out.Devices = append(out.Devices, JoystickDeviceDTO{ID: string(d.ID), Name: d.Name})
	}
	return out
}
```

**On `HotkeyStateDTO.Failed`:** joystick bindings have no per-binding registration failure mode — polling either sees a device or it does not, and absence is already carried by `TriggerDTO.Connected`. So `Failed` stays keyboard-only and needs no joystick entries. Do not invent them.

**Persist device names.** `connectedDevices` must also write newly-seen names through to `cfg.KeybindDevices`, and `SetSettingsBackend` must seed `sb.deviceNames` from it, or a chip for an unplugged stick renders its raw id after a restart:

```go
// rememberDevice records a device's product name for display. Persisted
// (config.KeybindDevices) rather than kept in memory so a binding for a
// device that is not currently attached is still nameable after a restart.
// Best-effort: a failed save costs a display name, never a binding.
func (a *App) rememberDevice(id, name string) {
	sb := a.settings
	sb.mu.Lock()
	if name == "" || sb.deviceNames[id] == name {
		sb.mu.Unlock()
		return
	}
	sb.deviceNames[id] = name
	next := *sb.cfg
	next.KeybindDevices = map[string]string{}
	for k, v := range sb.cfg.KeybindDevices {
		next.KeybindDevices[k] = v
	}
	next.KeybindDevices[id] = name
	if sb.cfgPath != "" {
		if err := config.Save(sb.cfgPath, &next); err == nil {
			sb.cfg = &next
		}
	} else {
		sb.cfg = &next
	}
	sb.mu.Unlock()
}
```

Call it from `connectedDevices` in place of the direct `sb.deviceNames[...] = d.Name` assignment.

- [ ] **Step 5: Update `persistKeybinds` for the new config type**

```go
	next.Keybinds = map[string]config.KeybindValue{}
	for id, list := range sb.kb.Snapshot() {
		next.Keybinds[id] = config.KeybindValue(list)
	}
```

and wherever the store is seeded from config (in `SetSettingsBackend`), convert the other way:

```go
	raw := map[string][]string{}
	for id, v := range cfg.Keybinds {
		raw[id] = []string(v)
	}
	kb.Load(raw)
```

- [ ] **Step 6: Add the event**

The emitter is **`internal/events/events.go`** (there is no `internal/app/events.go`). It declares an `Event*` name constant per event and a method on `*Tagged`. Add both, following `HotkeyPressed` at `events.go:146` exactly:

```go
// EventJoystickCaptured is emitted when a joystick capture completed and bound
// itself.
EventJoystickCaptured = "keybinds:joy_captured"
```

```go
// JoystickCaptured tells the UI that a joystick capture completed and bound
// itself, so the listening chip can close. The binding itself arrives via
// EventKeybindsChanged -- this carries only the action id, because the UI
// needs to know WHICH row to close and nothing more.
func (t *Tagged) JoystickCaptured(actionID string) {
	t.em.Emit(EventJoystickCaptured, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}
```

The payload is a struct with a JSON tag, not a bare string, matching `HotkeyPressed`/`HotkeyReleased` — the frontend destructures `{action_id}` from every other keybind event and an inconsistent payload here would be a trap.

- [ ] **Step 7: Run the tests**

Run: `go test -race ./internal/... -v`
Expected: PASS, including every pre-existing settings test.

- [ ] **Step 8: Prove the PTT-cut test bites**

Temporarily remove the `a.isHold(actionID) &&` guard from `Released` so it always emits. Re-run: `TestHoldActionHeldByTwoSourcesEmitsOneEdgePair` MUST fail. Restore.

- [ ] **Step 9: Commit**

```bash
git add internal/app/
git commit -m "$(cat <<'EOF'
feat(app): expose trigger lists and drive both input managers

GetKeybinds returns a trigger list per action; SetKeybind becomes
AddTrigger and RemoveTrigger lands beside it, both keeping the existing
persist-then-rollback discipline.

applyHotkeys splits triggers by kind across the keyboard and joystick
managers. Pressed/Released consult the refcount for hold actions only --
press-kind actions never receive a Released, so counting them would
deaden them after one press.

BeginCapture now takes the action id and suspends BOTH managers, or
binding a joystick button would transmit while you bind it. Joystick
capture completes in the backend rather than round-tripping through the
frontend, which would add a race for nothing.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Wire the joystick manager in `main.go`

**Files:**
- Modify: `main.go:94-96` (backend wiring) and the shutdown path
- Test: `main_wiring_test.go` (new, package `main`)

**Interfaces:**
- Consumes: `joystick.NewOSSource`, `joystick.New`, `App.SetJoystickBackend`.
- Produces: nothing for later tasks.

**Why this task exists as its own gate.** Phase 3 shipped a plan in which nothing called `SetSettingsBackend`, and the whole phase would have gone out inert. The grep step below exists so that cannot happen twice.

- [ ] **Step 1: Write the failing test**

Create `main_wiring_test.go`:

```go
package main

import (
	"os"
	"strings"
	"testing"
)

// TestJoystickBackendIsWired guards the failure mode Phase 3 nearly shipped:
// a complete, tested subsystem that nothing ever constructs, so the feature
// is inert in the built binary while every unit test passes.
func TestJoystickBackendIsWired(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	for _, want := range []string{
		"joystick.NewOSSource",
		"joystick.New(",
		"SetJoystickBackend",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("main.go does not call %s -- the joystick subsystem would be inert", want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -run TestJoystickBackendIsWired ./...`
Expected: FAIL on all three strings.

- [ ] **Step 3: Wire it in `main.go`**

Add `"github.com/FPGSchiba/vcs-srs-client/internal/joystick"` to the imports, and after the existing `gui.SetSettingsBackend(...)` call at line 96:

```go
	// Joystick/gamepad input. A failure here is never fatal: the client is a
	// voice-comms app first, and keyboard binds must keep working on a
	// machine with no joystick, no permission to read one, or no backend at
	// all (macOS). The manager reports "unsupported" and the UI hides the
	// affordance.
	if joySrc, err := joystick.NewOSSource(); err != nil {
		appLog.Warn("joystick input unavailable; keyboard binds are unaffected", "err", err)
	} else {
		jm := joystick.New(joySrc, gui, appLog)
		gui.SetJoystickBackend(jm)
		defer jm.Close()
	}
```

`gui` already satisfies `joystick.Handler` structurally via its existing `Pressed` / `Released` methods — no adapter is needed, and none should be added.

- [ ] **Step 4: Verify**

```bash
go test -run TestJoystickBackendIsWired ./...
go vet ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go build ./...
go build ./...
```
Expected: test passes, vet clean, all three platforms build.

- [ ] **Step 5: Commit**

```bash
git add main.go main_wiring_test.go
git commit -m "$(cat <<'EOF'
feat: wire the joystick manager into startup

A source failure is logged and swallowed: the client is a voice-comms app
first, and keyboard binds must keep working on a machine with no
joystick, no permission to read one, or no backend at all.

The wiring test guards the failure mode Phase 3 nearly shipped -- a fully
tested subsystem that nothing constructs, inert in the binary while every
unit test passes.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Frontend types, API client, and `TriggerChip`

**Files:**
- Modify: `frontend/src/shared/store/settings.ts` (the `Keybind` interface, plus new joystick types)
- Modify: `frontend/src/shared/api/client.ts`
- Create: `frontend/src/shared/components/TriggerChip.tsx`
- Create: `frontend/src/shared/components/TriggerChip.test.tsx`

**Interfaces:**
- Consumes: Task 11's `TriggerDTO`, `KeybindDTO`, `JoystickStateDTO` JSON shapes.
- Produces: TS `Trigger`, `Keybind.triggers`, `JoystickState`, `JoystickDevice`; `api.addTrigger`, `api.removeTrigger`, `api.beginCapture(actionId)`, `api.getJoystickState`; `<TriggerChip trigger={...} onRemove={...} />`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/shared/components/TriggerChip.test.tsx`:

```tsx
import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { TriggerChip } from "./TriggerChip";
import type { Trigger } from "../store/settings";

const keyTrigger: Trigger = {
  kind: "key",
  chord: "Ctrl+F1",
  device: "",
  device_name: "",
  label: "Ctrl+F1",
  connected: true,
};

const joyTrigger: Trigger = {
  kind: "joy",
  chord: "",
  device: "stick-c3",
  device_name: "VPC MongoosT-50CM3",
  label: "Btn 12",
  connected: true,
};

describe("TriggerChip", () => {
  it("splits a keyboard chord into one kbd per key", () => {
    render(<TriggerChip trigger={keyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByText("Ctrl")).toBeInTheDocument();
    expect(screen.getByText("F1")).toBeInTheDocument();
  });

  it("renders a joystick trigger as a single labelled chip", () => {
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByText("Btn 12")).toBeInTheDocument();
  });

  it("marks a disconnected device without hiding the binding", () => {
    // The binding is still valid and returns when the stick is plugged back
    // in, so it must stay visible and stay named.
    render(
      <TriggerChip trigger={{ ...joyTrigger, connected: false }} onRemove={vi.fn()} />,
    );
    const chip = screen.getByTitle(/not connected/i);
    expect(chip).toBeInTheDocument();
    expect(chip.className).toContain("disconnected");
    expect(screen.getByText("Btn 12")).toBeInTheDocument();
  });

  it("names the device in the title so identical sticks are tellable apart", () => {
    render(<TriggerChip trigger={joyTrigger} onRemove={vi.fn()} />);
    expect(screen.getByTitle(/VPC MongoosT-50CM3/)).toBeInTheDocument();
  });

  it("calls onRemove when the remove affordance is clicked", () => {
    const onRemove = vi.fn();
    render(<TriggerChip trigger={joyTrigger} onRemove={onRemove} />);
    fireEvent.click(screen.getByRole("button", { name: /remove/i }));
    expect(onRemove).toHaveBeenCalledTimes(1);
  });

  it("does not call onRemove when the chip itself is clicked", () => {
    // The chip is not a capture affordance any more -- the row's + is.
    const onRemove = vi.fn();
    render(<TriggerChip trigger={joyTrigger} onRemove={onRemove} />);
    fireEvent.click(screen.getByText("Btn 12"));
    expect(onRemove).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npx vitest run src/shared/components/TriggerChip.test.tsx`
Expected: FAIL — cannot resolve `./TriggerChip`.

- [ ] **Step 3: Update `frontend/src/shared/store/settings.ts`**

Replace the `Keybind` interface and add the joystick types:

```ts
/** One way to activate an action. Mirrors Go's `app.TriggerDTO`.
 *
 * `label` is rendered by the backend, not here: the physical naming of
 * buttons and hats has exactly one home, the same way canonical chord
 * formatting lives in Go's `internal/chord`. */
export interface Trigger {
  kind: "key" | "joy";
  chord: string;
  device: string;
  device_name: string;
  label: string;
  connected: boolean;
}

export interface Keybind {
  action_id: string;
  label: string;
  desc: string;
  category: string;
  kind: string;
  triggers: Trigger[];
}

export interface JoystickDevice {
  id: string;
  name: string;
}

/** Joystick subsystem health, mirroring Go's `app.JoystickStateDTO`.
 *
 * `supported: false` (macOS) means HIDE the affordance -- it is explicitly
 * NOT a permission denial, so it must never render a grant button. There is
 * nothing the user can do about it. */
export interface JoystickState {
  supported: boolean;
  error: string;
  devices: JoystickDevice[];
}
```

Add `joystick: JoystickState` to the store's state with the default `{ supported: false, error: "", devices: [] }`, and a setter alongside the existing `setKeybinds` / `setHotkeys`.

- [ ] **Step 4: Update `frontend/src/shared/api/client.ts`**

Rename `setKeybind` to `addTrigger`, and add the rest. Follow whatever binding-import style the file already uses:

```ts
  addTrigger: (actionId: string, cap: Capture) => Promise<SetKeybindResult>,
  removeTrigger: (actionId: string, index: number) => Promise<void>,
  beginCapture: (actionId: string) => Promise<number>,
  getJoystickState: () => Promise<JoystickState>,
```

`beginCapture` gains the action id because a completed joystick capture is bound by the backend, which therefore has to know which action it belongs to.

- [ ] **Step 5: Write `frontend/src/shared/components/TriggerChip.tsx`**

```tsx
import { Fragment } from "react";
import type { Trigger } from "../store/settings";

interface TriggerChipProps {
  trigger: Trigger;
  onRemove: () => void;
}

/**
 * TriggerChip renders one bound trigger plus its remove affordance.
 *
 * Unlike KeyChip, this chip does NOT capture. Capture moved to a single `+`
 * per row that accepts either a keyboard chord or a joystick button, so a
 * chip is purely a display of something already bound.
 *
 * A keyboard trigger renders as `.kbd` spans joined by `.plus`, matching the
 * design prototype. A joystick trigger renders as one `.kbd` carrying the
 * backend-rendered label ("Btn 12", "Hat 1 ↑", "Btn 5 + Btn 3").
 *
 * A trigger whose device is absent renders MUTED rather than disappearing:
 * the binding is still valid and starts working again the moment the stick is
 * plugged back in, so hiding it would misrepresent the saved configuration.
 */
export function TriggerChip({ trigger, onRemove }: TriggerChipProps) {
  const title =
    trigger.kind === "joy"
      ? `${trigger.device_name}${trigger.connected ? "" : " — not connected"}`
      : undefined;

  const body =
    trigger.kind === "key" ? (
      trigger.label.split("+").map((part, i) => (
        <Fragment key={`${part}-${i}`}>
          {i > 0 && <span className="plus">+</span>}
          <span className="kbd">{part}</span>
        </Fragment>
      ))
    ) : (
      <span className={`kbd${trigger.connected ? "" : " disconnected"}`}>
        {trigger.label}
      </span>
    );

  return (
    <span className="trigger-chip" title={title}>
      {body}
      <button
        type="button"
        className="trigger-remove"
        aria-label={`Remove ${trigger.label}`}
        onClick={onRemove}
      >
        ×
      </button>
    </span>
  );
}
```

Add matching styles to the settings stylesheet, following the tokens already used by `.kbd`: `.trigger-chip` lays its contents out inline with a small gap, `.trigger-remove` is a muted glyph that brightens on hover, and `.kbd.disconnected` drops opacity and uses the muted foreground token. Do not introduce new colour literals — use the existing design tokens.

- [ ] **Step 6: Run the tests**

```bash
cd frontend && npx vitest run src/shared/components/TriggerChip.test.tsx && npx tsc --noEmit
```
Expected: six passing tests, no type errors.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/shared/ 
git commit -m "$(cat <<'EOF'
feat(frontend): add TriggerChip and the trigger-list types

A chip displays one bound trigger and removes it; it no longer captures,
because capture moved to a single per-row affordance that takes either
input kind.

A trigger whose device is absent renders muted rather than disappearing:
the binding is still valid and resumes the moment the stick is plugged
back in, so hiding it would misrepresent the saved configuration.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 14: Settings Keybinds section — multi-chip rows, one capture affordance

**Files:**
- Modify: `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`
- Modify: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx` (subscribe to `keybinds:joy_captured`, hydrate joystick state)
- Test: `frontend/src/windows/main/screens/settings/sections/Keybinds.test.tsx` (extend; keep every existing test)

**Interfaces:**
- Consumes: Task 13's `TriggerChip`, `Trigger`, `JoystickState`, `api.addTrigger`, `api.removeTrigger`, `api.beginCapture(actionId)`; the existing `KeyChip` for the keyboard half of capture.
- Produces: nothing for later tasks.

**Behaviour required (spec §9, §10):**

1. Each row renders one `TriggerChip` per trigger, plus a `+` that starts capture.
2. **One capture affordance, either input.** While capturing, whichever arrives first wins: a DOM `keydown` (via the existing `KeyChip`, unchanged) or a joystick binding from the backend (`keybinds:joy_captured`).
3. The prompt reads: **"Press a key or joystick button — hold a second button first for a modifier."**
4. Capture keeps the existing token discipline exactly: one row listening at a time, `beginCapture` per row, `endCapture` in a `finally`.
5. When `joystick.supported` is false, the prompt drops the joystick half and **no grant affordance appears** — unsupported is not denied.
6. When `joystick.error` is non-empty and `joystick.supported` is true, show it once above the section. Do **not** reuse the Accessibility banner copy.

- [ ] **Step 1: Write the failing test**

Add to `Keybinds.test.tsx`:

```tsx
it("renders one chip per trigger", () => {
  renderWithKeybinds([
    {
      action_id: "global.ptt",
      label: "Global PTT",
      desc: "",
      category: "global",
      kind: "hold",
      triggers: [
        { kind: "key", chord: "F1", device: "", device_name: "", label: "F1", connected: true },
        {
          kind: "joy",
          chord: "",
          device: "stick-c3",
          device_name: "Test Stick",
          label: "Btn 12",
          connected: true,
        },
      ],
    },
  ]);
  expect(screen.getByText("F1")).toBeInTheDocument();
  expect(screen.getByText("Btn 12")).toBeInTheDocument();
});

it("passes the action id to beginCapture", async () => {
  const beginCapture = vi.spyOn(api, "beginCapture").mockResolvedValue(1);
  renderWithKeybinds([pttRow([])]);
  fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
  await waitFor(() => expect(beginCapture).toHaveBeenCalledWith("global.ptt"));
});

it("removes the clicked trigger by index", async () => {
  const removeTrigger = vi.spyOn(api, "removeTrigger").mockResolvedValue(undefined);
  renderWithKeybinds([
    pttRow([
      { kind: "key", chord: "F1", device: "", device_name: "", label: "F1", connected: true },
      {
        kind: "joy",
        chord: "",
        device: "stick-c3",
        device_name: "Test Stick",
        label: "Btn 12",
        connected: true,
      },
    ]),
  ]);
  fireEvent.click(screen.getByRole("button", { name: /remove btn 12/i }));
  await waitFor(() => expect(removeTrigger).toHaveBeenCalledWith("global.ptt", 1));
});

it("mentions joystick in the capture prompt when supported", () => {
  renderWithKeybinds([pttRow([])], { supported: true, error: "", devices: [] });
  fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
  expect(screen.getByText(/joystick button/i)).toBeInTheDocument();
});

it("omits the joystick half of the prompt when unsupported", () => {
  renderWithKeybinds([pttRow([])], { supported: false, error: "", devices: [] });
  fireEvent.click(screen.getByRole("button", { name: /add binding/i }));
  expect(screen.queryByText(/joystick button/i)).not.toBeInTheDocument();
});

it("offers no grant affordance for an unsupported platform", () => {
  // Unsupported is NOT denied. There is nothing the user can grant, so
  // offering a button would be a dead end.
  renderWithKeybinds([pttRow([])], { supported: false, error: "", devices: [] });
  expect(screen.queryByRole("button", { name: /grant/i })).not.toBeInTheDocument();
});

it("shows a joystick error without reusing the accessibility copy", () => {
  renderWithKeybinds([pttRow([])], {
    supported: true,
    error: "joystick: cannot read /dev/input (add your user to the 'input' group...)",
    devices: [],
  });
  expect(screen.getByText(/input' group/)).toBeInTheDocument();
  expect(screen.queryByText(/accessibility/i)).not.toBeInTheDocument();
});
```

Add the `pttRow(triggers)` helper and extend `renderWithKeybinds` to take an optional `JoystickState`. Update **every existing test in this file** that builds a `Keybind` with `chord:` to use `triggers: []` instead.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd frontend && npx vitest run src/windows/main/screens/settings/sections/Keybinds.test.tsx`
Expected: FAIL — rows render no chips; `beginCapture` called without an action id.

- [ ] **Step 3: Update `Keybinds.tsx`**

Keep the file's existing four correctness properties and their doc comment verbatim — they are all still true — and add a fifth:

```
 * 5. One capture affordance, either input. Rather than making the user
 *    choose "keyboard or joystick" first, a single capture accepts
 *    whichever arrives first: a DOM keydown through KeyChip, or a joystick
 *    binding the backend captured and bound itself. The joystick half
 *    completes in Go (the manager already holds the binding, so bouncing it
 *    through here would add a race for nothing) and arrives as
 *    `keybinds:joy_captured`, which this component uses only to close the
 *    listening chip.
```

Render each row as its chips plus the capture affordance:

```tsx
const renderTriggers = (kb: Keybind) => (
  <span className="trigger-row">
    {kb.triggers.map((t, i) => (
      <TriggerChip
        key={`${t.kind}-${t.label}-${i}`}
        trigger={t}
        onRemove={() => void handleRemove(kb.action_id, i)}
      />
    ))}
    {capturingId === kb.action_id ? (
      <KeyChip
        key={`capture-${kb.action_id}-${epoch[kb.action_id] ?? 0}`}
        binding=""
        onCapture={handleCapture(kb.action_id)}
        onCancel={handleCancel(kb.action_id)}
      />
    ) : (
      <button
        type="button"
        className="kbd add-binding"
        aria-label={`Add binding for ${kb.label}`}
        onClick={() => handleChipClick(kb.action_id)}
      >
        +
      </button>
    )}
  </span>
);
```

Add the remove handler:

```tsx
const handleRemove = async (actionId: string, index: number) => {
  try {
    await api.removeTrigger(actionId, index);
  } catch (err) {
    // The backend is the source of truth (keybinds:changed), so a failed
    // removal simply leaves the row as it was.
    console.error("removeTrigger failed", err);
  }
};
```

Change `handleChipClick` to pass the action id through to `beginCapture`:

```tsx
    tokens.current.set(
      actionId,
      api.beginCapture(actionId).catch((err) => {
        console.error("beginCapture failed", err);
        return 0;
      }),
    );
```

Change `handleCapture` to call `api.addTrigger` instead of `api.setKeybind`, and read `res.stolen.trigger.label` rather than `res.stolen.chord`.

Render the capture prompt conditionally on joystick support, and the joystick error above the section:

```tsx
const joystick = useSettings((s) => s.joystick);

const capturePrompt = joystick.supported
  ? "Press a key or joystick button — hold a second button first for a modifier."
  : "Press a key.";
```

```tsx
{joystick.supported && joystick.error && (
  <div className="banner banner-warn">{joystick.error}</div>
)}
```

- [ ] **Step 4: Close the listening chip on `keybinds:joy_captured`**

In `SettingsScreen.tsx`, alongside the existing `keybinds:changed` subscription, add one for `keybinds:joy_captured` that clears the capturing row. Hydrate `joystick` from `api.getJoystickState()` in the same place the screen hydrates `hotkeys`.

- [ ] **Step 5: Run the full frontend suite**

```bash
cd frontend && npx vitest run && npx tsc --noEmit && npm run build
```
Expected: all tests pass, no type errors, production build succeeds.

- [ ] **Step 6: Prove the unsupported-prompt test bites**

Temporarily make `capturePrompt` unconditional. Re-run: `omits the joystick half of the prompt when unsupported` MUST fail. Restore.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/
git commit -m "$(cat <<'EOF'
feat(frontend): render trigger lists with one capture affordance

Each row shows a chip per trigger plus a + that captures. A single
capture accepts whichever input arrives first -- a DOM keydown or a
joystick binding -- so there is no "keyboard or joystick?" mode choice.

The joystick half completes in Go and arrives as keybinds:joy_captured,
used here only to close the listening chip.

An unsupported platform loses the joystick half of the prompt and is
offered no grant affordance: unsupported is not denied, and a button the
user cannot act on is a dead end.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 15: Documentation and the hardware-verification checklist

**Files:**
- Modify: `docs/ROADMAP.md` (Phase 3.5 status)
- Modify: `CLAUDE.md` (current-status table)
- Create: `docs/superpowers/plans/2026-09-18-joystick-manual-verification.md`

**Interfaces:** none.

**Why this is a task and not a footnote:** spec §16 item 10 makes hardware verification part of the definition of done, and spec risks J1/J2/J5 can only be closed on real hardware. None of it can be run in this environment, so the deliverable is a checklist precise enough for a human to execute without reading the plan.

- [ ] **Step 1: Write the manual verification checklist**

Create `docs/superpowers/plans/2026-09-18-joystick-manual-verification.md`:

```markdown
# Joystick keybinds — manual verification

Nothing here can be automated: every item needs real hardware, and the most
important one needs Star Citizen running. Run it on **Windows** with a HOTAS.

**Closes spec risks J1 (di8 unproven), J2 (helper-window cooperative level),
J5 (identical devices) and Definition-of-Done item 10.**

## 1. Force feedback survives — THE critical test

The one finding the spike could not prove, and the reason this design avoids
SDL. If this fails, stop and report: nothing else matters.

- [ ] Start Star Citizen with a force-feedback stick. Confirm FFB works
      (stick shakes on weapon fire / turbulence).
- [ ] Leave SC running. Start VCS.
- [ ] Confirm **FFB still works in SC**.
- [ ] Quit and restart in the other order: VCS first, then SC. Confirm FFB
      works.
- [ ] Confirm VCS still reads the stick in both orders.

If FFB breaks in either order, capture which order and report it — that is
evidence the cooperative level is not what it should be.

## 2. Background input

- [ ] Bind a joystick button to Global PTT.
- [ ] Give SC focus. Press the button. Confirm VCS registers the PTT (the
      log records `joystick bind fired` with the device and input).
- [ ] Confirm SC still receives that button itself — it must not be swallowed.

## 3. Minimise to tray

- [ ] Close VCS to tray. Press the bound button. Confirm it still fires.
      (This is what the message-only helper window buys us.)

## 4. Buttons, hats and modifiers

- [ ] Bind a plain button. Fires.
- [ ] Bind a hat direction. Fires, and only in that direction.
- [ ] Bind a modifier combo: hold button A, press button B, release both.
      Confirm the chip shows "A + B" and that it fires only with A held.
- [ ] Bind a **cross-device** modifier: hold a throttle button, press a stick
      button. Confirm it fires.
- [ ] Specificity: bind Global PTT to button B alone AND another action to
      A+B. Confirm A+B fires only the second action.

## 5. Multi-source PTT — the refcount

- [ ] Bind Global PTT to both a keyboard key and a joystick button.
- [ ] Hold the keyboard key, then also press the joystick button, then
      release **only the keyboard key**.
- [ ] Confirm transmission CONTINUES — it must not cut while the joystick
      button is still held.
- [ ] Release the joystick button. Confirm transmission stops.

## 6. Hot-plug and unplug

- [ ] With VCS running, unplug the stick mid-PTT. Confirm transmission stops
      and nothing latches open.
- [ ] Plug it back in. Confirm the binding works again within ~3 seconds,
      with no restart.
- [ ] Confirm the chip showed as muted/"not connected" while unplugged and
      did not disappear.

## 7. Identical devices (only if two of the same model are available)

- [ ] Attach two identical sticks. Bind a button on each to different actions.
- [ ] Confirm each fires only its own action. If both fire, record it — that
      is spec risk J5 realised.

## 8. Config round-trip

- [ ] Open `%APPDATA%\vcs-client\config.toml`. Confirm keyboard-only actions
      are still bare strings (`global.push_to_mute = "V"`).
- [ ] Confirm joystick binds appear as arrays.
- [ ] Restart VCS. Confirm every binding survived.

## 9. Capture safety

- [ ] Hold the bound PTT button, and while holding it click `+` on a row.
      Confirm the held button does NOT get bound instantly (baseline).
- [ ] During capture, confirm pressing the button does not transmit.
- [ ] Press Escape mid-capture. Confirm keyboard hotkeys still work
      afterwards.

## 10. Linux (if available)

- [ ] Run as a user NOT in the `input` group. Confirm the error names the
      group and does not mention Accessibility.
- [ ] Add the user to `input`, re-login, confirm devices appear.

## 11. macOS

- [ ] Confirm the app builds and runs.
- [ ] Confirm the Keybinds section offers no joystick affordance and shows
      **no** permission-grant button.
- [ ] Confirm keyboard binds are completely unaffected.
```

- [ ] **Step 2: Update `docs/ROADMAP.md`**

Change Phase 3.5's status to `[x]` complete **only if** the automated suite is green, and add a verification-status paragraph mirroring Phase 3's, pointing at the checklist above and stating plainly that the phase is code-complete but **not field-verified** until it has been run.

- [ ] **Step 3: Update `CLAUDE.md`**

Add a Phase 3.5 row to the current-status table with the same code-complete/not-field-verified wording, and note under it that joystick input is deliberately Windows+Linux only.

- [ ] **Step 4: Run the whole suite one last time**

```bash
go vet ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go build ./...
GOOS=linux   GOARCH=amd64 go build ./...
cd frontend && npx tsc --noEmit && npx vitest run && npm run build
```
Expected: everything green.

- [ ] **Step 5: Commit**

```bash
git add docs/ CLAUDE.md
git commit -m "$(cat <<'EOF'
docs: record joystick status and the hardware verification checklist

The phase is code-complete but not field-verified: the force-feedback
coexistence test needs Star Citizen and a real FFB stick, and it is the
one finding the spike could not prove. Spec risks J1, J2 and J5 close
only on hardware.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
EOF
)"
```

---
