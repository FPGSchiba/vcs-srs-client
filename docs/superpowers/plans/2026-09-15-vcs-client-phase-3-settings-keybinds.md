# VCS Client Phase 3 — Settings + Keybinds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a working Settings screen, persisted keybinds with conflict resolution, real OS-level global hotkey registration, and a system tray — on top of an upgraded Wails v3 beta.

**Architecture:** Four new Go packages with strict boundaries — `chord` (pure value type, no deps), `keybinds` (pure in-memory domain, no file access), `hotkeys` (OS registrar behind an interface seam), and extensions to `config` and `app`. Go owns all state; the frontend hydrates via bindings and re-renders from events, never optimistically.

**Tech Stack:** Go 1.26, Wails v3 beta.22, `golang.design/x/hotkey`, BurntSushi/toml, React 19 + TypeScript + Vite + Zustand, vitest + testing-library.

**Spec:** [`docs/superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md`](../specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md)

## Global Constraints

- Go module is `github.com/FPGSchiba/vcs-srs-client`. Logger is `log/slog`.
- `srspb/`, `frontend/bindings/`, `frontend/dist/`, `frontend/node_modules/`, `build/bin/` are generated and gitignored. Never commit or hand-edit them.
- Every new `config.Config` field MUST get a default in `Default()` so older `config.toml` files still load.
- Go tests are table-driven and MUST pass under `-race`.
- Event name constants in `internal/events/events.go` MUST stay in sync with `frontend/src/shared/api/events.ts`.
- Chord canonical form is `Ctrl+Alt+Shift+Super+<Key>`, modifiers always in that order. A modifier-only chord is invalid.
- Key capture uses `KeyboardEvent.code` (physical key), never `.key`.
- **Never fake hold-to-talk with a press-toggle** (spec R11).
- Do not implement anything from the six deferred Settings sections (Audio, Effects, Profiles, Notifications, Misc, Legacy) beyond the stub panel.
- Commit after every task. Conventional commit format (`feat:`, `fix:`, `chore:`, `docs:`, `test:`).

## Local toolchain note

`buf` and `task` are NOT installed on the dev machine; `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc` are. Generate `srspb/` with:

```bash
mkdir -p srspb && protoc --go_out=srspb --go_opt=paths=source_relative \
  --go-grpc_out=srspb --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
  srs.proto
```

If the sandbox denies the Go build cache or `~/go`, export writable overrides first:

```bash
export GOPATH="$TMPDIR/gopath" GOCACHE="$TMPDIR/gocache" GOMODCACHE="$TMPDIR/gomodcache"
```

## Spec refinements resolved in this plan

Two points where the spec was internally inconsistent; both resolved toward the spec's own §4 package layout:

1. **`SetKeybind` takes a raw capture, not a chord string.** Spec §6 shows `SetKeybind(actionID, chord string)`, but §4 puts the `KeyboardEvent.code` mapping table in `internal/chord/keycode.go` — i.e. Go owns the mapping. So the frontend sends `{code, ctrl, alt, shift, super}` and Go produces the canonical chord. This keeps the mapping table in exactly one place and unit-tested in Go. The UI renders the chord from the `keybinds:changed` event rather than from its own guess.
2. **`hotkeys.Apply` takes `map[string]Binding`, not `map[ActionID]chord.Chord`.** The registrar needs the `Hold` flag to know whether to wire a release handler, and `hotkeys` must not import `keybinds` (spec §4). `Binding{Chord, Hold}` carries it without the coupling.

---

### Task 1: Upgrade Wails v3 to beta.22

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `.github/workflows/test.yml:83`
- Modify: `.github/workflows/release.yml:51`
- Modify: `frontend/package.json` (dependency `@wailsio/runtime`)
- Modify: `frontend/package-lock.json` (regenerated)
- Modify: `docs/superpowers/specs/2026-05-31-vcs-client-design.md` (risk R1 row)

**Interfaces:**
- Consumes: nothing.
- Produces: a beta.22 toolchain every later task builds against. No Go API changes are expected — verified empirically 2026-09-15 that `go build ./...` and `go test -race ./...` pass with zero source changes.

- [ ] **Step 1: Generate protos so the tree compiles**

```bash
mkdir -p srspb && protoc --go_out=srspb --go_opt=paths=source_relative \
  --go-grpc_out=srspb --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
  srs.proto
```

- [ ] **Step 2: Record the baseline**

```bash
go build ./... && go test ./internal/... ./pkg/... 2>&1 | tail -20
```
Expected: builds clean; all packages `ok`. If anything fails here, STOP — it is pre-existing breakage, not caused by the upgrade.

- [ ] **Step 3: Bump the Go module**

```bash
go get github.com/wailsapp/wails/v3@v3.0.0-beta.22
go mod tidy
```
Expected transitive bumps: `go-git` 5.19.1→5.19.2, `tint` 1.1.2→1.1.3, `x/crypto`, `x/net`, `x/sys`, `x/text`.

- [ ] **Step 4: Verify the Go side still builds and passes**

```bash
go build ./... && go vet ./... && go test -race ./...
```
Expected: all green, zero source edits. If a compile error appears, fix it minimally and note what changed — the empirical check said there would be none, so a failure here is new information worth recording in the commit message.

- [ ] **Step 5: Bump the wails3 CLI locally**

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.22
wails3 version
```
Expected: `v3.0.0-beta.22`.

- [ ] **Step 6: Bump both CI pins**

In `.github/workflows/test.yml` line 83 and `.github/workflows/release.yml` line 51, change:
```yaml
run: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.96
```
to:
```yaml
run: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.22
```

- [ ] **Step 7: Bump the JS runtime**

In `frontend/package.json`, change `"@wailsio/runtime": "^3.0.0-alpha.79"` to `"@wailsio/runtime": "^3.0.0-beta.22"`, then:
```bash
cd frontend && npm install && cd ..
```

- [ ] **Step 8: Regenerate TypeScript bindings and build the frontend**

```bash
wails3 generate bindings -ts -clean=true
cd frontend && npm run build && npm test && cd ..
```

**This is the step with real risk.** beta ships a rewritten binding generator ("static source analysis for richer generated TypeScript bindings"). If the emitted shape or the `bindings/github.com/FPGSchiba/vcs-srs-client/internal/app` import path changed, `tsc` will fail in these files:
- `frontend/src/shared/api/client.ts:1` — the `App` import
- `frontend/src/shared/api/events.ts:1` — `Events` from `@wailsio/runtime`
- `frontend/src/shared/components/TopBar.tsx:1` and `PreLoginTopBar.tsx:1` — `Window`, `Application`

Fix the imports to match whatever the generator now emits. Do NOT change the `api` wrapper's exported surface — the rest of the app depends on it.

- [ ] **Step 9: Confirm the app actually runs**

```bash
wails3 build && ./bin/vcs-client
```
Expected: the welcome window opens and renders. Close it. A clean compile is not sufficient evidence for a runtime upgrade.

- [ ] **Step 10: Retire risk R1 in the parent spec**

In `docs/superpowers/specs/2026-05-31-vcs-client-design.md`, replace the R1 row:
```markdown
| R1 | Wails v3 still pre-stable; API churn breaks builds mid-development | M | Pin v3 version in `go.mod`; keep windowing/binding adapter (`internal/app/windows.go`) thin; run `wails doctor` in CI |
```
with:
```markdown
| R1 | ~~Wails v3 pre-stable; API churn breaks builds~~ **RETIRED 2026-09-15** | — | Upgraded to `v3.0.0-beta.22`, which ships a stable desktop API and an explicit compatibility promise. Version stays pinned in `go.mod` and both CI workflows; the thin windowing adapter (`internal/app/windows.go`) is kept regardless |
```

- [ ] **Step 11: Commit**

```bash
git add go.mod go.sum frontend/package.json frontend/package-lock.json \
        .github/workflows/test.yml .github/workflows/release.yml \
        docs/superpowers/specs/2026-05-31-vcs-client-design.md
git commit -m "chore(deps): upgrade Wails v3 alpha.96 -> beta.22

Go side needed no source changes. Bumps the module, both CI wails3
pins, and @wailsio/runtime, and regenerates the TypeScript bindings.

Beta ships a stable desktop API with a compatibility promise, which
retires spec risk R1."
```

---

### Task 2: `internal/chord` — chord value type and key mapping

**Files:**
- Create: `internal/chord/chord.go`
- Create: `internal/chord/keycode.go`
- Test: `internal/chord/chord_test.go`
- Test: `internal/chord/keycode_test.go`

**Interfaces:**
- Consumes: nothing. **This package MUST NOT import anything outside the standard library** — that is the whole point of the boundary (spec §4).
- Produces:
```go
type Mod uint8
const (ModCtrl Mod = 1 << iota; ModAlt; ModShift; ModSuper)
type Chord struct { Mods Mod; Key string }
func (c Chord) String() string
func (c Chord) IsZero() bool
func Parse(s string) (Chord, error)
func FromCode(code string, ctrl, alt, shift, super bool) (Chord, error)
var ErrEmpty, ErrModifierOnly, ErrUnknownKey error
```

- [ ] **Step 1: Write the failing chord tests**

Create `internal/chord/chord_test.go`:

```go
package chord

import "testing"

func TestChordString(t *testing.T) {
	tests := []struct {
		name string
		in   Chord
		want string
	}{
		{"bare key", Chord{Key: "F1"}, "F1"},
		{"single modifier", Chord{Mods: ModCtrl, Key: "E"}, "Ctrl+E"},
		{"canonical order", Chord{Mods: ModSuper | ModShift | ModAlt | ModCtrl, Key: "A"}, "Ctrl+Alt+Shift+Super+A"},
		{"alt digit", Chord{Mods: ModAlt, Key: "1"}, "Alt+1"},
		{"zero", Chord{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, s := range []string{"F1", "Ctrl+E", "Ctrl+Alt+Shift+Super+A", "Alt+1", "Space"} {
		t.Run(s, func(t *testing.T) {
			c, err := Parse(s)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", s, err)
			}
			if got := c.String(); got != s {
				t.Errorf("round trip = %q, want %q", got, s)
			}
		})
	}
}

func TestParseNormalisesModifierOrder(t *testing.T) {
	c, err := Parse("Shift+Ctrl+A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := c.String(); got != "Ctrl+Shift+A" {
		t.Errorf("got %q, want canonical %q", got, "Ctrl+Shift+A")
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name, in string
		wantErr  error
	}{
		{"empty", "", ErrEmpty},
		{"modifier only", "Ctrl", ErrModifierOnly},
		{"modifiers only", "Ctrl+Shift", ErrModifierOnly},
		{"unknown key", "Ctrl+NotAKey", ErrUnknownKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Parse(tt.in); err == nil {
				t.Fatalf("Parse(%q) = nil error, want %v", tt.in, tt.wantErr)
			}
		})
	}
}

func TestIsZero(t *testing.T) {
	if !(Chord{}).IsZero() {
		t.Error("empty chord should be zero")
	}
	if (Chord{Key: "A"}).IsZero() {
		t.Error("chord with key should not be zero")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/chord/ -v`
Expected: FAIL — package does not compile, `undefined: Chord`.

- [ ] **Step 3: Implement `chord.go`**

```go
// Package chord is a dependency-free keyboard-chord value type: parsing,
// canonical formatting, and physical-key-code mapping. It deliberately imports
// nothing outside the standard library so it is testable on any platform with
// no OS involvement.
package chord

import (
	"errors"
	"strings"
)

// Mod is a bitmask of chord modifiers.
type Mod uint8

const (
	ModCtrl Mod = 1 << iota
	ModAlt
	ModShift
	ModSuper
)

var (
	// ErrEmpty means the input string was empty.
	ErrEmpty = errors.New("chord: empty")
	// ErrModifierOnly means the chord had modifiers but no key.
	ErrModifierOnly = errors.New("chord: modifier-only chord is not bindable")
	// ErrUnknownKey means the key name is not in the canonical key set.
	ErrUnknownKey = errors.New("chord: unknown key")
)

// Chord is a set of modifiers plus one canonical key name.
type Chord struct {
	Mods Mod
	Key  string
}

// IsZero reports whether the chord is unset.
func (c Chord) IsZero() bool { return c.Key == "" }

// modOrder is the canonical modifier ordering. Never reorder this — the string
// form is persisted to config.toml and compared for equality.
var modOrder = []struct {
	bit  Mod
	name string
}{
	{ModCtrl, "Ctrl"},
	{ModAlt, "Alt"},
	{ModShift, "Shift"},
	{ModSuper, "Super"},
}

// String renders the canonical form, e.g. "Ctrl+Alt+F1". Zero chords render "".
func (c Chord) String() string {
	if c.IsZero() {
		return ""
	}
	var b strings.Builder
	for _, m := range modOrder {
		if c.Mods&m.bit != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(c.Key)
	return b.String()
}

// Parse reads a canonical (or differently-ordered) chord string. Modifier order
// in the input is not significant; the result is always canonical.
func Parse(s string) (Chord, error) {
	if strings.TrimSpace(s) == "" {
		return Chord{}, ErrEmpty
	}
	parts := strings.Split(s, "+")
	var c Chord
	for i, p := range parts {
		switch p {
		case "Ctrl":
			c.Mods |= ModCtrl
			continue
		case "Alt":
			c.Mods |= ModAlt
			continue
		case "Shift":
			c.Mods |= ModShift
			continue
		case "Super":
			c.Mods |= ModSuper
			continue
		}
		if i != len(parts)-1 {
			return Chord{}, ErrUnknownKey
		}
		if !validKey(p) {
			return Chord{}, ErrUnknownKey
		}
		c.Key = p
	}
	if c.Key == "" {
		return Chord{}, ErrModifierOnly
	}
	return c, nil
}
```

- [ ] **Step 4: Write the failing keycode tests**

Create `internal/chord/keycode_test.go`:

```go
package chord

import "testing"

func TestFromCode(t *testing.T) {
	tests := []struct {
		name                       string
		code                       string
		ctrl, alt, shift, super    bool
		want                       string
	}{
		{"letter", "KeyD", false, false, false, false, "D"},
		{"digit", "Digit1", false, false, false, false, "1"},
		{"alt digit", "Digit1", false, true, false, false, "Alt+1"},
		{"function key", "F1", false, false, false, false, "F1"},
		{"space", "Space", false, false, false, false, "Space"},
		{"ctrl letter", "KeyE", true, false, false, false, "Ctrl+E"},
		{"numpad", "Numpad7", false, false, false, false, "Numpad7"},
		{"arrow", "ArrowUp", false, false, false, false, "ArrowUp"},
		{"all modifiers", "KeyA", true, true, true, true, "Ctrl+Alt+Shift+Super+A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := FromCode(tt.code, tt.ctrl, tt.alt, tt.shift, tt.super)
			if err != nil {
				t.Fatalf("FromCode error: %v", err)
			}
			if got := c.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFromCodeRejectsModifierKeys(t *testing.T) {
	// Pressing a bare modifier must not produce a binding.
	for _, code := range []string{"ControlLeft", "AltRight", "ShiftLeft", "MetaLeft"} {
		t.Run(code, func(t *testing.T) {
			if _, err := FromCode(code, true, false, false, false); err == nil {
				t.Errorf("FromCode(%q) = nil error, want rejection", code)
			}
		})
	}
}

func TestFromCodeRejectsUnknown(t *testing.T) {
	if _, err := FromCode("NotARealCode", false, false, false, false); err == nil {
		t.Error("expected error for unknown code")
	}
}
```

- [ ] **Step 5: Run to verify it fails**

Run: `go test ./internal/chord/ -run TestFromCode -v`
Expected: FAIL — `undefined: FromCode`.

- [ ] **Step 6: Implement `keycode.go`**

```go
package chord

import "strings"

// codeToKey maps a browser KeyboardEvent.code (a PHYSICAL key, layout
// independent) to our canonical key name. We use .code rather than .key because
// OS-level hotkey registration matches physical keys; .key varies by layout.
var codeToKey = func() map[string]string {
	m := map[string]string{
		"Space":        "Space",
		"Escape":       "Escape",
		"Enter":        "Enter",
		"Tab":          "Tab",
		"Backspace":    "Backspace",
		"Delete":       "Delete",
		"Insert":       "Insert",
		"Home":         "Home",
		"End":          "End",
		"PageUp":       "PageUp",
		"PageDown":     "PageDown",
		"ArrowUp":      "ArrowUp",
		"ArrowDown":    "ArrowDown",
		"ArrowLeft":    "ArrowLeft",
		"ArrowRight":   "ArrowRight",
		"Minus":        "Minus",
		"Equal":        "Equal",
		"BracketLeft":  "BracketLeft",
		"BracketRight": "BracketRight",
		"Semicolon":    "Semicolon",
		"Quote":        "Quote",
		"Backquote":    "Backquote",
		"Backslash":    "Backslash",
		"Comma":        "Comma",
		"Period":       "Period",
		"Slash":        "Slash",
		"NumpadAdd":    "NumpadAdd",
		"NumpadSubtract": "NumpadSubtract",
		"NumpadMultiply": "NumpadMultiply",
		"NumpadDivide":   "NumpadDivide",
		"NumpadDecimal":  "NumpadDecimal",
		"NumpadEnter":    "NumpadEnter",
	}
	for c := 'A'; c <= 'Z'; c++ {
		m["Key"+string(c)] = string(c)
	}
	for d := '0'; d <= '9'; d++ {
		m["Digit"+string(d)] = string(d)
		m["Numpad"+string(d)] = "Numpad" + string(d)
	}
	for i := 1; i <= 24; i++ {
		name := "F" + itoa(i)
		m[name] = name
	}
	return m
}()

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// modifierCodes are physical modifier keys. Pressing one alone must never
// produce a binding.
var modifierCodes = map[string]bool{
	"ControlLeft": true, "ControlRight": true,
	"AltLeft": true, "AltRight": true,
	"ShiftLeft": true, "ShiftRight": true,
	"MetaLeft": true, "MetaRight": true,
	"CapsLock": true,
}

func validKey(k string) bool {
	for _, v := range codeToKey {
		if v == k {
			return true
		}
	}
	return false
}

// FromCode builds a Chord from a browser KeyboardEvent.code plus modifier flags.
func FromCode(code string, ctrl, alt, shift, super bool) (Chord, error) {
	if strings.TrimSpace(code) == "" {
		return Chord{}, ErrEmpty
	}
	if modifierCodes[code] {
		return Chord{}, ErrModifierOnly
	}
	key, ok := codeToKey[code]
	if !ok {
		return Chord{}, ErrUnknownKey
	}
	var c Chord
	if ctrl {
		c.Mods |= ModCtrl
	}
	if alt {
		c.Mods |= ModAlt
	}
	if shift {
		c.Mods |= ModShift
	}
	if super {
		c.Mods |= ModSuper
	}
	c.Key = key
	return c, nil
}
```

- [ ] **Step 7: Run all chord tests**

Run: `go test -race ./internal/chord/ -v`
Expected: PASS, all cases.

- [ ] **Step 8: Verify the no-dependency boundary**

Run: `go list -deps ./internal/chord | grep -v '^internal/\|^[a-z]*$' | grep '\.' || echo "stdlib only — boundary holds"`
Expected: `stdlib only — boundary holds`. If any external module appears, remove the import — this boundary is the point of the package.

- [ ] **Step 9: Commit**

```bash
git add internal/chord/
git commit -m "feat(chord): add dependency-free keyboard chord type

Canonical Ctrl+Alt+Shift+Super ordering, round-trip parsing, and a
physical KeyboardEvent.code mapping table. Uses .code rather than .key
because OS hotkey registration matches physical keys and .key varies
by keyboard layout.

Imports stdlib only by design, so it is testable on any platform with
no OS involvement."
```

---

### Task 3: `internal/keybinds` — action registry and binding store

**Files:**
- Create: `internal/keybinds/actions.go`
- Create: `internal/keybinds/store.go`
- Test: `internal/keybinds/actions_test.go`
- Test: `internal/keybinds/store_test.go`

**Interfaces:**
- Consumes: `internal/chord` — `chord.Chord`, `chord.Parse`.
- **MUST NOT import `internal/config`, `os`, or any filesystem package** (spec §4). Persistence is the caller's job.
- Produces:
```go
type ActionID string
type Kind uint8;     const (KindPress Kind = iota; KindHold)
type Category uint8; const (CatGlobal Category = iota; CatChannel; CatPerRadio; CatStatus)
type Action struct { ID ActionID; Label, Desc string; Category Category; Kind Kind }
func StaticActions() []Action
func PerRadioActions(radios []RadioRef) []Action
type RadioRef struct { ID uint32; Name string }
func Defaults() map[ActionID]chord.Chord
type Stolen struct { ActionID ActionID; Chord chord.Chord }
type Store struct{ ... }
func New() *Store
func (s *Store) Load(raw map[string]string)
func (s *Store) Snapshot() map[string]string
func (s *Store) Get(id ActionID) (chord.Chord, bool)
func (s *Store) Set(id ActionID, c chord.Chord) *Stolen
func (s *Store) Clear(id ActionID)
func (s *Store) All() map[ActionID]chord.Chord
```

- [ ] **Step 1: Write the failing action-registry test**

Create `internal/keybinds/actions_test.go`:

```go
package keybinds

import "testing"

func TestStaticActionsCoverAllCategories(t *testing.T) {
	seen := map[Category]int{}
	for _, a := range StaticActions() {
		seen[a.Category]++
	}
	for _, c := range []Category{CatGlobal, CatChannel, CatStatus} {
		if seen[c] == 0 {
			t.Errorf("no static actions in category %v", c)
		}
	}
	if seen[CatPerRadio] != 0 {
		t.Error("per-radio actions must not be static")
	}
}

func TestPTTIsHold(t *testing.T) {
	want := map[ActionID]Kind{
		"global.ptt":           KindHold,
		"global.push_to_mute":  KindHold,
		"global.mute_toggle":   KindPress,
		"status.afk":           KindPress,
	}
	got := map[ActionID]Kind{}
	for _, a := range StaticActions() {
		got[a.ID] = a.Kind
	}
	for id, k := range want {
		if got[id] != k {
			t.Errorf("%s kind = %v, want %v", id, got[id], k)
		}
	}
}

func TestStaticActionIDsAreUnique(t *testing.T) {
	seen := map[ActionID]bool{}
	for _, a := range StaticActions() {
		if seen[a.ID] {
			t.Errorf("duplicate action ID %q", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestPerRadioActions(t *testing.T) {
	acts := PerRadioActions([]RadioRef{{ID: 1, Name: "GUARD"}, {ID: 2, Name: "FLEET"}})
	if len(acts) != 4 {
		t.Fatalf("got %d actions, want 4 (2 radios x ptt+select)", len(acts))
	}
	byID := map[ActionID]Action{}
	for _, a := range acts {
		byID[a.ID] = a
	}
	ptt, ok := byID["radio.1.ptt"]
	if !ok {
		t.Fatal("missing radio.1.ptt")
	}
	if ptt.Kind != KindHold {
		t.Error("radio ptt must be KindHold")
	}
	if ptt.Category != CatPerRadio {
		t.Error("radio ptt must be CatPerRadio")
	}
	if sel := byID["radio.2.select"]; sel.Kind != KindPress {
		t.Error("radio select must be KindPress")
	}
}

func TestDefaultsParse(t *testing.T) {
	for id, c := range Defaults() {
		if c.IsZero() {
			t.Errorf("default for %s is zero", id)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/keybinds/ -v`
Expected: FAIL — `undefined: StaticActions`.

- [ ] **Step 3: Implement `actions.go`**

```go
// Package keybinds owns the canonical action registry and the action->chord
// binding store. It is pure in-memory: it does not touch the filesystem and does
// not import internal/config. Callers persist Snapshot() wherever they like.
package keybinds

import (
	"fmt"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// ActionID identifies a bindable action, e.g. "global.ptt", "radio.1.ptt".
type ActionID string

// Kind says whether an action needs key release as well as key press.
type Kind uint8

const (
	// KindPress fires once on key down.
	KindPress Kind = iota
	// KindHold fires on key down AND key up (push-to-talk semantics).
	KindHold
)

// Category groups actions for display.
type Category uint8

const (
	CatGlobal Category = iota
	CatChannel
	CatPerRadio
	CatStatus
)

// Action is a bindable action's static metadata.
type Action struct {
	ID       ActionID
	Label    string
	Desc     string
	Category Category
	Kind     Kind
}

// StaticActions returns every action that is not derived from a radio.
func StaticActions() []Action {
	return []Action{
		{"global.ptt", "Global PTT", "Transmits on the currently Selected radio", CatGlobal, KindHold},
		{"global.push_to_mute", "Push-to-mute", "", CatGlobal, KindHold},
		{"global.mute_toggle", "Mute toggle", "", CatGlobal, KindPress},
		{"global.emergency_broadcast", "Emergency broadcast", "", CatGlobal, KindPress},
		{"global.compact_overlay", "Open compact overlay", "", CatGlobal, KindPress},

		{"channel.intercom", "Intercom", "", CatChannel, KindPress},
		{"channel.role", "Role channel", "", CatChannel, KindPress},
		{"channel.ship", "Ship-wide", "", CatChannel, KindPress},
		{"channel.fleet", "Fleet-wide", "", CatChannel, KindPress},

		{"status.available", "Set status: Available", "", CatStatus, KindPress},
		{"status.combat", "Set status: In Combat", "", CatStatus, KindPress},
		{"status.discipline", "Set status: Comms Discipline", "", CatStatus, KindPress},
		{"status.afk", "Set status: AFK", "", CatStatus, KindPress},
	}
}

// RadioRef is the minimal radio identity the registry needs.
type RadioRef struct {
	ID   uint32
	Name string
}

// PerRadioActions derives the PTT and Select actions for the given radios.
func PerRadioActions(radios []RadioRef) []Action {
	out := make([]Action, 0, len(radios)*2)
	for _, r := range radios {
		label := fmt.Sprintf("R%02d · %s", r.ID, r.Name)
		out = append(out,
			Action{ActionID(fmt.Sprintf("radio.%d.ptt", r.ID)), label + " (PTT)", "", CatPerRadio, KindHold},
			Action{ActionID(fmt.Sprintf("radio.%d.select", r.ID)), label + " (Select)", "", CatPerRadio, KindPress},
		)
	}
	return out
}

// Defaults are the bindings shipped on first run, matching the design
// prototype. global.ptt ships unbound deliberately — it is the one the user is
// most likely to want on their own key.
func Defaults() map[ActionID]chord.Chord {
	must := func(s string) chord.Chord {
		c, err := chord.Parse(s)
		if err != nil {
			panic("keybinds: bad default chord " + s + ": " + err.Error())
		}
		return c
	}
	return map[ActionID]chord.Chord{
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

- [ ] **Step 4: Run the action tests**

Run: `go test ./internal/keybinds/ -v`
Expected: PASS.

- [ ] **Step 5: Write the failing store tests**

Create `internal/keybinds/store_test.go`:

```go
package keybinds

import (
	"reflect"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

func mustChord(t *testing.T, s string) chord.Chord {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return c
}

func TestSetAndGet(t *testing.T) {
	s := New()
	c := mustChord(t, "F1")
	if stolen := s.Set("global.ptt", c); stolen != nil {
		t.Errorf("unexpected steal: %+v", stolen)
	}
	got, ok := s.Get("global.ptt")
	if !ok || got != c {
		t.Errorf("Get = %v/%v, want %v/true", got, ok, c)
	}
}

func TestSetStealsExistingBinding(t *testing.T) {
	s := New()
	f1 := mustChord(t, "F1")
	s.Set("radio.1.ptt", f1)

	stolen := s.Set("radio.2.ptt", f1)
	if stolen == nil {
		t.Fatal("expected a steal, got nil")
	}
	if stolen.ActionID != "radio.1.ptt" {
		t.Errorf("stolen from %q, want radio.1.ptt", stolen.ActionID)
	}
	if stolen.Chord != f1 {
		t.Errorf("stolen chord = %v, want %v", stolen.Chord, f1)
	}
	if _, ok := s.Get("radio.1.ptt"); ok {
		t.Error("previous owner must be unbound after a steal")
	}
	if got, _ := s.Get("radio.2.ptt"); got != f1 {
		t.Error("new owner must hold the chord")
	}
}

func TestSetSameActionSameChordIsNotASteal(t *testing.T) {
	s := New()
	f1 := mustChord(t, "F1")
	s.Set("global.ptt", f1)
	if stolen := s.Set("global.ptt", f1); stolen != nil {
		t.Errorf("rebinding an action to its own chord must not report a steal, got %+v", stolen)
	}
}

func TestClear(t *testing.T) {
	s := New()
	s.Set("global.ptt", mustChord(t, "F1"))
	s.Clear("global.ptt")
	if _, ok := s.Get("global.ptt"); ok {
		t.Error("binding should be gone after Clear")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := New()
	s.Set("global.ptt", mustChord(t, "F1"))
	s.Set("global.mute_toggle", mustChord(t, "Ctrl+M"))

	snap := s.Snapshot()
	want := map[string]string{"global.ptt": "F1", "global.mute_toggle": "Ctrl+M"}
	if !reflect.DeepEqual(snap, want) {
		t.Errorf("Snapshot() = %v, want %v", snap, want)
	}

	s2 := New()
	s2.Load(snap)
	if !reflect.DeepEqual(s2.Snapshot(), want) {
		t.Errorf("round trip lost data: %v", s2.Snapshot())
	}
}

func TestLoadPreservesUnknownActionIDs(t *testing.T) {
	// A binding written by a FUTURE version must survive a load/save cycle
	// rather than being silently dropped.
	s := New()
	s.Load(map[string]string{
		"global.ptt":            "F1",
		"future.unknown.action": "Ctrl+Shift+Z",
	})
	snap := s.Snapshot()
	if snap["future.unknown.action"] != "Ctrl+Shift+Z" {
		t.Errorf("unknown action ID was dropped; snapshot = %v", snap)
	}
}

func TestLoadDropsUnparseableChords(t *testing.T) {
	s := New()
	s.Load(map[string]string{
		"global.ptt":         "F1",
		"global.mute_toggle": "!!!not-a-chord!!!",
	})
	if _, ok := s.Get("global.mute_toggle"); ok {
		t.Error("unparseable chord should not become an active binding")
	}
	if _, ok := s.Get("global.ptt"); !ok {
		t.Error("a bad entry must not prevent good entries from loading")
	}
}

func TestAllReturnsACopy(t *testing.T) {
	s := New()
	s.Set("global.ptt", mustChord(t, "F1"))
	all := s.All()
	delete(all, "global.ptt")
	if _, ok := s.Get("global.ptt"); !ok {
		t.Error("All() must return a copy; mutating it changed the store")
	}
}

func TestConcurrentAccessIsRaceFree(t *testing.T) {
	s := New()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			s.Set("global.ptt", mustChord(t, "F1"))
		}
		close(done)
	}()
	for i := 0; i < 200; i++ {
		_ = s.Snapshot()
	}
	<-done
}
```

- [ ] **Step 6: Run to verify it fails**

Run: `go test ./internal/keybinds/ -run TestSet -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 7: Implement `store.go`**

```go
package keybinds

import (
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// Stolen reports which action lost a chord when another action took it.
type Stolen struct {
	ActionID ActionID
	Chord    chord.Chord
}

// Store holds the live action->chord map. Safe for concurrent use.
type Store struct {
	mu sync.RWMutex
	// binds holds bindings for action IDs we understand.
	binds map[ActionID]chord.Chord
	// unknown holds raw entries whose action ID we do not recognise, so a
	// config written by a newer version survives a load/save cycle here.
	unknown map[string]string
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		binds:   map[ActionID]chord.Chord{},
		unknown: map[string]string{},
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
	if len(id) < len("radio.0.ptt") || id[:6] != "radio." {
		return false
	}
	return hasSuffix(id, ".ptt") || hasSuffix(id, ".select")
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}

// Load replaces the store contents from a raw map (as read from config.toml).
// Entries whose chord will not parse are dropped; entries whose action ID is
// unrecognised are preserved verbatim for the next Snapshot.
func (s *Store) Load(raw map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binds = map[ActionID]chord.Chord{}
	s.unknown = map[string]string{}
	known := knownIDs()
	for id, str := range raw {
		if !known[ActionID(id)] && !isPerRadioID(id) {
			s.unknown[id] = str
			continue
		}
		c, err := chord.Parse(str)
		if err != nil {
			continue // malformed chord: drop this entry, keep the rest
		}
		s.binds[ActionID(id)] = c
	}
}

// Snapshot renders the store as a raw map for persistence, including preserved
// unknown entries.
func (s *Store) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.binds)+len(s.unknown))
	for id, c := range s.binds {
		out[string(id)] = c.String()
	}
	for id, str := range s.unknown {
		out[id] = str
	}
	return out
}

// Get returns the chord bound to id.
func (s *Store) Get(id ActionID) (chord.Chord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.binds[id]
	return c, ok
}

// All returns a copy of the live bindings.
func (s *Store) All() map[ActionID]chord.Chord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[ActionID]chord.Chord, len(s.binds))
	for k, v := range s.binds {
		out[k] = v
	}
	return out
}

// Set binds c to id. If another action already holds c, that action is unbound
// and returned as Stolen. Rebinding an action to the chord it already holds is
// not a steal.
func (s *Store) Set(id ActionID, c chord.Chord) *Stolen {
	s.mu.Lock()
	defer s.mu.Unlock()
	var stolen *Stolen
	for other, existing := range s.binds {
		if other != id && existing == c {
			stolen = &Stolen{ActionID: other, Chord: existing}
			delete(s.binds, other)
			break
		}
	}
	s.binds[id] = c
	return stolen
}

// Clear removes any binding for id.
func (s *Store) Clear(id ActionID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.binds, id)
}
```

- [ ] **Step 8: Run all keybinds tests under race**

Run: `go test -race ./internal/keybinds/ -v`
Expected: PASS, all cases.

- [ ] **Step 9: Verify the no-filesystem boundary**

Run: `go list -deps ./internal/keybinds | grep -E 'internal/config|^os$' || echo "boundary holds — no config, no os"`
Expected: `boundary holds — no config, no os`.

- [ ] **Step 10: Commit**

```bash
git add internal/keybinds/
git commit -m "feat(keybinds): add action registry and binding store

Static registry plus per-radio actions derived from live radio IDs.
PTT and push-to-mute are KindHold (need press and release); everything
else is KindPress.

Set() implements steal-on-conflict: the new binding wins and the
previous owner is returned so the UI can report what lost its key.
Unknown action IDs from a newer version survive a load/save cycle.

Pure in-memory by design: no config import, no filesystem access."
```

---

### Task 4: `internal/config` — `[general]` and `[keybinds]` tables

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go` (append)

**Interfaces:**
- Consumes: nothing new.
- Produces:
```go
type General struct {
    StartMinimized, MinimizeToTray, ShowTransmitterName,
    PlayConnectionSounds, RadioSwitchAsPTT bool
}
// Config gains: General General `toml:"general"`
//               Keybinds map[string]string `toml:"keybinds"`
```

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestDefaultGeneral(t *testing.T) {
	g := Default().General
	if g.StartMinimized {
		t.Error("StartMinimized should default false")
	}
	if !g.MinimizeToTray {
		t.Error("MinimizeToTray should default true")
	}
	if !g.ShowTransmitterName {
		t.Error("ShowTransmitterName should default true")
	}
	if !g.PlayConnectionSounds {
		t.Error("PlayConnectionSounds should default true")
	}
	if g.RadioSwitchAsPTT {
		t.Error("RadioSwitchAsPTT should default false")
	}
}

func TestLoadPrePhase3ConfigGetsDefaults(t *testing.T) {
	// A config.toml written before Phase 3 has neither [general] nor
	// [keybinds]. It must still load, with defaults filled in.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	old := "log_level = \"DEBUG\"\nserver_url = \"localhost:5002\"\nping_interval_seconds = 7\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LogLevel != "DEBUG" || cfg.PingIntervalSeconds != 7 {
		t.Errorf("existing values lost: %+v", cfg)
	}
	if !cfg.General.MinimizeToTray {
		t.Error("missing [general] should fall back to defaults")
	}
}

func TestKeybindsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	cfg := Default()
	cfg.Keybinds = map[string]string{
		"global.ptt":     "F1",
		"radio.1.select": "Alt+1",
	}
	cfg.General.StartMinimized = true
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Keybinds["global.ptt"] != "F1" || got.Keybinds["radio.1.select"] != "Alt+1" {
		t.Errorf("keybinds lost in round trip: %v", got.Keybinds)
	}
	if !got.General.StartMinimized {
		t.Error("general lost in round trip")
	}
}
```

Ensure `config_test.go` imports `os`, `path/filepath`, `testing`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/config/ -run 'TestDefaultGeneral|TestLoadPrePhase3|TestKeybindsRoundTrip' -v`
Expected: FAIL — `cfg.General undefined`.

- [ ] **Step 3: Extend `Config` and `Default()`**

In `internal/config/config.go`, replace the `Config` struct and `Default` function:

```go
// General holds the user-facing toggles from the Settings > General section.
// ShowTransmitterName, PlayConnectionSounds and RadioSwitchAsPTT are stored and
// exposed now; their consumers arrive in Phases 4/5.
type General struct {
	StartMinimized       bool `toml:"start_minimized"`
	MinimizeToTray       bool `toml:"minimize_to_tray"`
	ShowTransmitterName  bool `toml:"show_transmitter_name"`
	PlayConnectionSounds bool `toml:"play_connection_sounds"`
	RadioSwitchAsPTT     bool `toml:"radio_switch_as_ptt"`
}

// Config holds the persisted user/app settings. New fields MUST get a default
// in Default() so older config files still load.
type Config struct {
	LogLevel            string `toml:"log_level"`
	ServerURL           string `toml:"server_url"`
	PingIntervalSeconds int    `toml:"ping_interval_seconds"`

	General General `toml:"general"`

	// Keybinds is the raw action-ID -> chord-string map. It is held raw rather
	// than typed so that entries written by a newer client version survive a
	// load/save cycle. internal/keybinds owns interpretation.
	Keybinds map[string]string `toml:"keybinds"`
}

// Default returns the baseline config used when no file exists.
func Default() *Config {
	return &Config{
		LogLevel:            "INFO",
		ServerURL:           "",
		PingIntervalSeconds: 5,
		General: General{
			StartMinimized:       false,
			MinimizeToTray:       true,
			ShowTransmitterName:  true,
			PlayConnectionSounds: true,
			RadioSwitchAsPTT:     false,
		},
		Keybinds: map[string]string{},
	}
}
```

Note: `Load` already starts from `Default()` and decodes over it, so absent tables keep their defaults with no further change.

- [ ] **Step 4: Run the config tests**

Run: `go test -race ./internal/config/ -v`
Expected: PASS. (If the pre-existing `paths_test.go` tests fail with "operation not permitted", that is the sandbox denying `~/Library/Application Support` — run with `HOME="$TMPDIR/fakehome"` set.)

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add [general] and [keybinds] tables

Keybinds live in config.toml rather than a separate file (spec 5.3).
Held as a raw map[string]string so entries from a newer client version
survive a load/save cycle; internal/keybinds owns interpretation.

Pre-Phase-3 config files with neither table still load, falling back to
defaults."
```

---

### Task 5: `internal/hotkeys` — OS registration behind an interface seam

**Files:**
- Create: `internal/hotkeys/hotkeys.go`
- Create: `internal/hotkeys/keymap.go`
- Create: `internal/hotkeys/registrar_x.go`
- Test: `internal/hotkeys/hotkeys_test.go`
- Modify: `go.mod` (adds `golang.design/x/hotkey`)

**Interfaces:**
- Consumes: `internal/chord` only. **MUST NOT import `internal/keybinds`** (spec §4).
- Produces:
```go
type Binding struct { Chord chord.Chord; Hold bool }
type Handler interface { Pressed(actionID string); Released(actionID string) }
type Registrar interface {
    Register(actionID string, c chord.Chord, hold bool, h Handler) error
    UnregisterAll()
}
type Manager struct{ ... }
func New(r Registrar, h Handler) *Manager
func (m *Manager) Apply(binds map[string]Binding) error
func (m *Manager) Suspend()
func (m *Manager) Resume() error
func (m *Manager) Registered() bool
func (m *Manager) LastError() error
func NewOSRegistrar() Registrar
func SupportsRelease() bool
```

- [ ] **Step 1: Resolve risk R11 before writing any Manager code**

This is the spec's highest-impact unknown: does `golang.design/x/hotkey` expose key **release**, which hold-to-talk PTT requires?

```bash
go get golang.design/x/hotkey@latest
mkdir -p "$TMPDIR/hkprobe" && cat > "$TMPDIR/hkprobe/main.go" <<'EOF'
package main

import (
	"fmt"
	"golang.design/x/hotkey"
)

func main() {
	hk := hotkey.New([]hotkey.Modifier{hotkey.ModCtrl}, hotkey.KeyF1)
	if err := hk.Register(); err != nil {
		fmt.Println("register error:", err)
		return
	}
	defer hk.Unregister()
	fmt.Printf("Keydown channel: %T\n", hk.Keydown())
	fmt.Printf("Keyup channel:   %T\n", hk.Keyup())
	fmt.Println("RELEASE SUPPORTED")
}
EOF
go doc golang.design/x/hotkey.Hotkey
```

Record the answer in the commit message.

- **If `Keyup()` exists** → proceed with the full plan below. Set `SupportsRelease()` to return `true`.
- **If it does NOT exist** → do NOT fake it with a press-toggle (Global Constraints). Instead: `SupportsRelease()` returns `false`; `Manager.Apply` skips bindings where `Hold` is true, records them in `SkippedHold()`, and Task 11 renders "hold-to-talk unavailable with the current backend" next to those rows. Then add a note to spec §12 R11 recording the outcome and deferring library selection to Phase 4/5.

- [ ] **Step 2: Write the failing Manager tests (fake registrar, no OS)**

Create `internal/hotkeys/hotkeys_test.go`:

```go
package hotkeys

import (
	"errors"
	"sync"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

type fakeRegistrar struct {
	mu           sync.Mutex
	registered   map[string]Binding
	unregisters  int
	failOn       string
	registerErr  error
}

func newFake() *fakeRegistrar {
	return &fakeRegistrar{registered: map[string]Binding{}}
}

func (f *fakeRegistrar) Register(id string, c chord.Chord, hold bool, _ Handler) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn == id {
		return f.registerErr
	}
	f.registered[id] = Binding{Chord: c, Hold: hold}
	return nil
}

func (f *fakeRegistrar) UnregisterAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = map[string]Binding{}
	f.unregisters++
}

func (f *fakeRegistrar) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registered)
}

type nopHandler struct{}

func (nopHandler) Pressed(string)  {}
func (nopHandler) Released(string) {}

func mustChord(t *testing.T, s string) chord.Chord {
	t.Helper()
	c, err := chord.Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return c
}

func TestApplyRegistersAll(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	err := m.Apply(map[string]Binding{
		"global.ptt":         {mustChord(t, "F1"), true},
		"global.mute_toggle": {mustChord(t, "M"), false},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if f.count() != 2 {
		t.Errorf("registered %d, want 2", f.count())
	}
	if !m.Registered() {
		t.Error("Registered() should be true after a clean Apply")
	}
}

func TestApplyClearsPreviousRegistrations(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"a": {mustChord(t, "F1"), false}})
	m.Apply(map[string]Binding{"b": {mustChord(t, "F2"), false}})
	if f.count() != 1 {
		t.Errorf("registered %d after re-Apply, want 1", f.count())
	}
	if _, stale := f.registered["a"]; stale {
		t.Error("previous registration was not cleared")
	}
}

func TestSuspendUnregistersAndResumeRestores(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	binds := map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}}
	m.Apply(binds)

	m.Suspend()
	if f.count() != 0 {
		t.Errorf("after Suspend registered %d, want 0", f.count())
	}
	if m.Registered() {
		t.Error("Registered() should be false while suspended")
	}

	if err := m.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if f.count() != 1 {
		t.Errorf("after Resume registered %d, want 1", f.count())
	}
}

func TestApplyWhileSuspendedDefersRegistration(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Suspend()
	m.Apply(map[string]Binding{"global.ptt": {mustChord(t, "F1"), true}})
	if f.count() != 0 {
		t.Error("Apply during suspension must not register with the OS")
	}
	if err := m.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if f.count() != 1 {
		t.Error("Resume must register the bindings applied while suspended")
	}
}

func TestApplySkipsZeroChords(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{
		"bound":   {mustChord(t, "F1"), false},
		"unbound": {chord.Chord{}, false},
	})
	if f.count() != 1 {
		t.Errorf("registered %d, want 1 (unbound must be skipped)", f.count())
	}
}

func TestRegistrationFailureIsRecordedNotFatal(t *testing.T) {
	f := newFake()
	f.failOn = "global.ptt"
	f.registerErr = errors.New("permission denied")
	m := New(f, nopHandler{})

	err := m.Apply(map[string]Binding{
		"global.ptt":         {mustChord(t, "F1"), true},
		"global.mute_toggle": {mustChord(t, "M"), false},
	})
	if err == nil {
		t.Error("Apply should report the failure")
	}
	if m.LastError() == nil {
		t.Error("LastError should hold the failure")
	}
	if f.count() != 1 {
		t.Error("the binding that could register should still have registered")
	}
	if m.Registered() {
		t.Error("Registered() should be false when any registration failed")
	}
}

func TestSuspendIsIdempotent(t *testing.T) {
	f := newFake()
	m := New(f, nopHandler{})
	m.Apply(map[string]Binding{"a": {mustChord(t, "F1"), false}})
	m.Suspend()
	m.Suspend()
	if err := m.Resume(); err != nil {
		t.Fatalf("Resume after double Suspend: %v", err)
	}
	if f.count() != 1 {
		t.Error("one Resume should restore after repeated Suspend")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/hotkeys/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 4: Implement `hotkeys.go`**

```go
// Package hotkeys registers global OS hotkeys. It deliberately knows nothing
// about internal/keybinds: callers hand it a map of action ID -> Binding, and it
// registers what it is given. The OS layer sits behind the Registrar interface
// so tests never touch real hotkeys.
package hotkeys

import (
	"fmt"
	"sync"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
)

// Binding is one registerable hotkey. Hold means the action needs key release
// as well as key press (push-to-talk semantics).
type Binding struct {
	Chord chord.Chord
	Hold  bool
}

// Handler receives hotkey activity. Implementations must not block.
type Handler interface {
	Pressed(actionID string)
	Released(actionID string)
}

// Registrar is the OS seam.
type Registrar interface {
	Register(actionID string, c chord.Chord, hold bool, h Handler) error
	UnregisterAll()
}

// Manager owns the current registration set and the suspend/resume state.
type Manager struct {
	mu        sync.Mutex
	reg       Registrar
	handler   Handler
	desired   map[string]Binding
	suspended bool
	lastErr   error
}

// New constructs a Manager over a Registrar.
func New(r Registrar, h Handler) *Manager {
	return &Manager{reg: r, handler: h, desired: map[string]Binding{}}
}

// Apply replaces the desired binding set and re-registers. While suspended it
// records the set without touching the OS; Resume registers it.
func (m *Manager) Apply(binds map[string]Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.desired = make(map[string]Binding, len(binds))
	for k, v := range binds {
		m.desired[k] = v
	}
	if m.suspended {
		return nil
	}
	return m.registerLocked()
}

// registerLocked clears and re-registers the desired set. Caller holds m.mu.
func (m *Manager) registerLocked() error {
	m.reg.UnregisterAll()
	m.lastErr = nil
	var firstErr error
	for id, b := range m.desired {
		if b.Chord.IsZero() {
			continue // unbound action
		}
		if err := m.reg.Register(id, b.Chord, b.Hold, m.handler); err != nil {
			wrapped := fmt.Errorf("register %s (%s): %w", id, b.Chord, err)
			if firstErr == nil {
				firstErr = wrapped
			}
		}
	}
	m.lastErr = firstErr
	return firstErr
}

// Suspend releases every OS registration. Used while the UI captures a keypress,
// so a registered hotkey does not swallow the key being rebound. Idempotent.
func (m *Manager) Suspend() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.suspended {
		return
	}
	m.suspended = true
	m.reg.UnregisterAll()
}

// Resume re-registers the desired set. Idempotent.
func (m *Manager) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.suspended {
		return nil
	}
	m.suspended = false
	return m.registerLocked()
}

// Registered reports whether the desired set is currently live with no errors.
func (m *Manager) Registered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.suspended && m.lastErr == nil
}

// LastError returns the most recent registration failure, if any.
func (m *Manager) LastError() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastErr
}
```

- [ ] **Step 5: Run the Manager tests**

Run: `go test -race ./internal/hotkeys/ -v`
Expected: PASS, all cases.

- [ ] **Step 6: Implement the real OS registrar**

Create `internal/hotkeys/keymap.go` mapping `chord.Chord` to the library's key and modifier constants, and `internal/hotkeys/registrar_x.go` implementing `Registrar` over `golang.design/x/hotkey`. `NewOSRegistrar()` returns it.

Required behaviour:
- `Register` maps the chord, calls the library's `Register()`, and starts **one goroutine per hotkey** that selects on the keydown channel (and the keyup channel when `hold` is true), calling `h.Pressed(actionID)` / `h.Released(actionID)`.
- `UnregisterAll` stops every goroutine (close a per-registrar `done` channel) and unregisters every hotkey. It MUST be safe to call when nothing is registered.
- A chord whose key has no equivalent in the library returns an error naming the key, rather than silently not registering.
- If Step 1 found no keyup support: `hold` bindings return `ErrHoldUnsupported` from `Register` and `SupportsRelease()` returns false.

There is no unit test for this file — it is the untestable OS seam, which is exactly why `Manager` holds all the logic. It is covered by the manual checklist in Task 12.

- [ ] **Step 7: Verify the package boundary and build on all platforms**

```bash
go list -deps ./internal/hotkeys | grep 'internal/keybinds' && echo "BOUNDARY VIOLATION" || echo "boundary holds"
GOOS=windows go build ./internal/hotkeys/
GOOS=linux   go build ./internal/hotkeys/
go build ./internal/hotkeys/
```
Expected: `boundary holds`, and all three builds succeed.

- [ ] **Step 8: Commit**

```bash
git add internal/hotkeys/ go.mod go.sum
git commit -m "feat(hotkeys): add global hotkey manager behind a registrar seam

Manager owns apply/suspend/resume and is fully unit tested against a
fake registrar, so tests never touch real OS hotkeys. The x/hotkey
implementation is isolated in registrar_x.go.

Suspend() exists so the keybind capture UI can release registrations
while listening -- otherwise a registered hotkey swallows the very
keypress meant to rebind it.

Registration failure is recorded in LastError() and never fatal.

R11: golang.design/x/hotkey key-release support = <RECORD THE ANSWER FROM STEP 1>"
```

---

### Task 6: `internal/app` — settings & keybind bindings, events, capture lifecycle

**Files:**
- Create: `internal/app/settings.go`
- Modify: `internal/app/dto.go` (append DTOs)
- Modify: `internal/app/app.go` (App fields + wiring setter)
- Modify: `internal/events/events.go` (event names + typed emitters)
- Test: `internal/app/settings_test.go`
- Test: `internal/events/events_test.go` (append)

**Interfaces:**
- Consumes: `keybinds.Store`, `hotkeys.Manager`, `config.Config`, `chord.FromCode`, `state.Store`.
- Produces (the frontend-facing surface). **JSON tags are snake_case and are load-bearing** — Tasks 9/10/11 consume exactly these names, and this matches the existing convention in `dto.go` (`is_intercom`, `unit_id`, `self_guid`). Write them verbatim:
```go
type SettingsDTO struct {
    StartMinimized       bool `json:"start_minimized"`
    MinimizeToTray       bool `json:"minimize_to_tray"`
    ShowTransmitterName  bool `json:"show_transmitter_name"`
    PlayConnectionSounds bool `json:"play_connection_sounds"`
    RadioSwitchAsPTT     bool `json:"radio_switch_as_ptt"`
}
type CaptureDTO struct {
    Code  string `json:"code"`
    Ctrl  bool   `json:"ctrl"`
    Alt   bool   `json:"alt"`
    Shift bool   `json:"shift"`
    Super bool   `json:"super"`
}
type KeybindDTO struct {
    ActionID string `json:"action_id"`
    Label    string `json:"label"`
    Desc     string `json:"desc"`
    Category string `json:"category"` // "global" | "channel" | "per_radio" | "status"
    Kind     string `json:"kind"`     // "hold" | "press"
    Chord    string `json:"chord"`    // canonical form, "" when unbound
}
type StolenDTO struct {
    ActionID string `json:"action_id"`
    Label    string `json:"label"`
    Chord    string `json:"chord"`
}
type SetKeybindResult struct {
    Stolen *StolenDTO `json:"stolen"` // nil when there was no conflict
}
type HotkeyStateDTO struct {
    Registered bool   `json:"registered"`
    Error      string `json:"error"`
}
func (a *App) GetSettings() SettingsDTO
func (a *App) SetSettings(s SettingsDTO) error
func (a *App) GetKeybinds() []KeybindDTO
func (a *App) SetKeybind(actionID string, cap CaptureDTO) (SetKeybindResult, error)
func (a *App) ClearKeybind(actionID string) error
func (a *App) BeginCapture() error
func (a *App) EndCapture() error
func (a *App) GetHotkeyState() HotkeyStateDTO
```

- [ ] **Step 1: Add the event constants and emitters**

In `internal/events/events.go`, add to the const block:

```go
	EventSettingsChanged   = "settings:changed"
	EventKeybindsChanged   = "keybinds:changed"
	EventHotkeyPressed     = "hotkey:pressed"
	EventHotkeyReleased    = "hotkey:released"
	EventHotkeysState      = "hotkeys:state"
```

and add the typed emitters at the end of the file:

```go
// HotkeyStatePayload is the EventHotkeysState payload.
type HotkeyStatePayload struct {
	Registered bool   `json:"registered"`
	Error      string `json:"error"`
}

// SettingsChanged emits EventSettingsChanged with the full settings struct.
func (t *Tagged) SettingsChanged(payload any) { t.em.Emit(EventSettingsChanged, payload) }

// KeybindsChanged emits EventKeybindsChanged with the FULL binding list.
// Full replacement rather than a delta: the list is small, and it removes a
// class of frontend/backend divergence bug.
func (t *Tagged) KeybindsChanged(payload any) { t.em.Emit(EventKeybindsChanged, payload) }

// HotkeyPressed emits EventHotkeyPressed.
func (t *Tagged) HotkeyPressed(actionID string) {
	t.em.Emit(EventHotkeyPressed, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}

// HotkeyReleased emits EventHotkeyReleased (Hold actions only).
func (t *Tagged) HotkeyReleased(actionID string) {
	t.em.Emit(EventHotkeyReleased, struct {
		ActionID string `json:"action_id"`
	}{ActionID: actionID})
}

// HotkeysState emits EventHotkeysState so a failed registration is visible in
// the UI rather than silent.
func (t *Tagged) HotkeysState(registered bool, errMsg string) {
	t.em.Emit(EventHotkeysState, HotkeyStatePayload{Registered: registered, Error: errMsg})
}
```

- [ ] **Step 2: Write the failing binding tests**

Create `internal/app/settings_test.go`:

```go
package app

import (
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/chord"
	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/hotkeys"
	"github.com/FPGSchiba/vcs-srs-client/internal/keybinds"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

type recordingEmitter struct {
	events []string
}

func (r *recordingEmitter) Emit(name string, _ any) { r.events = append(r.events, name) }

type countingRegistrar struct {
	registers   int
	unregisters int
}

func (c *countingRegistrar) Register(string, chord.Chord, bool, hotkeys.Handler) error {
	c.registers++
	return nil
}
func (c *countingRegistrar) UnregisterAll() { c.unregisters++ }

// newTestApp wires an App with in-memory settings deps and no Wails.
func newTestApp(t *testing.T) (*App, *recordingEmitter, *countingRegistrar) {
	t.Helper()
	em := &recordingEmitter{}
	reg := &countingRegistrar{}
	a := NewForTest(state.New(), nil, nil)
	cfg := config.Default()
	kb := keybinds.New()
	kb.Load(map[string]string{})
	hk := hotkeys.New(reg, a)
	a.SetSettingsBackend(cfg, "", kb, hk, em)
	return a, em, reg
}

func TestGetSettingsReturnsDefaults(t *testing.T) {
	a, _, _ := newTestApp(t)
	s := a.GetSettings()
	if !s.MinimizeToTray {
		t.Error("MinimizeToTray should default true")
	}
	if s.StartMinimized {
		t.Error("StartMinimized should default false")
	}
}

func TestSetSettingsEmitsChange(t *testing.T) {
	a, em, _ := newTestApp(t)
	s := a.GetSettings()
	s.StartMinimized = true
	if err := a.SetSettings(s); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if !a.GetSettings().StartMinimized {
		t.Error("setting did not stick")
	}
	if !contains(em.events, events.EventSettingsChanged) {
		t.Errorf("expected %s, got %v", events.EventSettingsChanged, em.events)
	}
}

func TestGetKeybindsJoinsRegistryWithChords(t *testing.T) {
	a, _, _ := newTestApp(t)
	list := a.GetKeybinds()
	if len(list) == 0 {
		t.Fatal("expected keybind rows")
	}
	byID := map[string]KeybindDTO{}
	for _, k := range list {
		byID[k.ActionID] = k
	}
	ptt, ok := byID["global.ptt"]
	if !ok {
		t.Fatal("global.ptt missing from the joined list")
	}
	if ptt.Label != "Global PTT" {
		t.Errorf("label = %q, want %q", ptt.Label, "Global PTT")
	}
	if ptt.Kind != "hold" {
		t.Errorf("kind = %q, want hold", ptt.Kind)
	}
}

func TestSetKeybindStoresAndEmits(t *testing.T) {
	a, em, _ := newTestApp(t)
	res, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err != nil {
		t.Fatalf("SetKeybind: %v", err)
	}
	if res.Stolen != nil {
		t.Errorf("unexpected steal: %+v", res.Stolen)
	}
	if !contains(em.events, events.EventKeybindsChanged) {
		t.Error("expected keybinds:changed event")
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "F1" {
			t.Errorf("chord = %q, want F1", k.Chord)
		}
	}
}

func TestSetKeybindReportsSteal(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.SetKeybind("global.mute_toggle", CaptureDTO{Code: "F1"}); err != nil {
		t.Fatal(err)
	}
	res, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Stolen == nil {
		t.Fatal("expected a steal report")
	}
	if res.Stolen.ActionID != "global.mute_toggle" {
		t.Errorf("stolen from %q, want global.mute_toggle", res.Stolen.ActionID)
	}
	if res.Stolen.Label != "Mute toggle" {
		t.Errorf("stolen label = %q, want %q", res.Stolen.Label, "Mute toggle")
	}
}

func TestSetKeybindRejectsBadCapture(t *testing.T) {
	a, _, _ := newTestApp(t)
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "ControlLeft", Ctrl: true}); err == nil {
		t.Error("a bare modifier must be rejected")
	}
	if _, err := a.SetKeybind("global.ptt", CaptureDTO{Code: "Nonsense"}); err == nil {
		t.Error("an unknown code must be rejected")
	}
}

func TestBeginCaptureSuspendsAndEndCaptureRestores(t *testing.T) {
	a, _, reg := newTestApp(t)
	a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	before := reg.unregisters

	if err := a.BeginCapture(); err != nil {
		t.Fatalf("BeginCapture: %v", err)
	}
	if reg.unregisters <= before {
		t.Error("BeginCapture must unregister OS hotkeys")
	}
	if err := a.EndCapture(); err != nil {
		t.Fatalf("EndCapture: %v", err)
	}
}

func TestCaptureAutoResumesAfterTimeout(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.SetCaptureTimeout(50 * time.Millisecond)
	if err := a.BeginCapture(); err != nil {
		t.Fatal(err)
	}
	// Never call EndCapture — simulates the frontend dying mid-capture.
	time.Sleep(150 * time.Millisecond)
	if !a.hotkeysResumed() {
		t.Error("hotkeys must auto-resume if EndCapture never arrives")
	}
}

func TestClearKeybind(t *testing.T) {
	a, _, _ := newTestApp(t)
	a.SetKeybind("global.ptt", CaptureDTO{Code: "F1"})
	if err := a.ClearKeybind("global.ptt"); err != nil {
		t.Fatal(err)
	}
	for _, k := range a.GetKeybinds() {
		if k.ActionID == "global.ptt" && k.Chord != "" {
			t.Errorf("chord = %q, want empty after clear", k.Chord)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/app/ -run 'TestGetSettings|TestSetKeybind' -v`
Expected: FAIL — `a.SetSettingsBackend undefined`.

- [ ] **Step 4: Implement `internal/app/settings.go`**

Requirements, all exercised by the tests above:

- `SetSettingsBackend(cfg *config.Config, cfgPath string, kb *keybinds.Store, hk *hotkeys.Manager, em events.Emitter)` stores the deps on `App` and seeds `kb` from `cfg.Keybinds` (falling back to `keybinds.Defaults()` when `cfg.Keybinds` is empty).
- `GetSettings` / `SetSettings` map `config.General` ↔ `SettingsDTO`. `SetSettings` persists via `config.Save(cfgPath, cfg)` (skipped when `cfgPath == ""`, as in tests) and emits `settings:changed`.
- `GetKeybinds` joins `keybinds.StaticActions()` + `keybinds.PerRadioActions(...)` (radios read from `a.st`) with the store's chords. `Category` and `Kind` serialise as lowercase strings (`"global"`, `"per_radio"`, `"hold"`, `"press"`).
- `SetKeybind` runs `chord.FromCode(cap.Code, cap.Ctrl, cap.Alt, cap.Shift, cap.Super)`, returns the error on failure, calls `kb.Set`, persists, re-applies hotkeys, emits `keybinds:changed`, and maps `*keybinds.Stolen` to `*StolenDTO` (resolving the label from the registry).
- `ClearKeybind` mirrors it.
- `BeginCapture` calls `hk.Suspend()` and arms a `time.AfterFunc` (default **10 seconds**, overridable via `SetCaptureTimeout` for tests) that calls `hk.Resume()`. `EndCapture` stops the timer and calls `hk.Resume()`. Both are safe to call repeatedly.
- `hotkeysResumed()` is an unexported test helper returning `hk.Registered()`.
- `App` implements `hotkeys.Handler`: `Pressed(id)` emits `hotkey:pressed`, `Released(id)` emits `hotkey:released`.
- `GetHotkeyState()` returns `HotkeyStateDTO{Registered: hk.Registered(), Error: errString(hk.LastError())}`.
- Every mutation re-emits; **no method returns state the frontend is expected to cache on its own.**

- [ ] **Step 5: Run the tests**

Run: `go test -race ./internal/app/ ./internal/events/ -v`
Expected: PASS.

- [ ] **Step 6: Sync the frontend event names**

In `frontend/src/shared/api/events.ts`, add to `EV`:

```ts
  settingsChanged: "settings:changed",
  keybindsChanged: "keybinds:changed",
  hotkeyPressed: "hotkey:pressed",
  hotkeyReleased: "hotkey:released",
  hotkeysState: "hotkeys:state",
```

- [ ] **Step 7: Commit**

```bash
git add internal/app/ internal/events/ frontend/src/shared/api/events.ts
git commit -m "feat(app): add settings and keybind bindings with capture lifecycle

BeginCapture suspends every OS registration while the UI listens for a
keypress, otherwise a registered hotkey swallows the key being rebound.
EndCapture re-arms. A 10s timeout auto-resumes if the frontend dies
mid-capture -- a stuck-suspended state is invisible and maddening to
diagnose, a spurious re-arm is harmless.

SetKeybind takes a raw {code, modifiers} capture rather than a chord
string so the physical-key mapping table lives only in internal/chord.

keybinds:changed ships the full list rather than a delta."
```

---

### Task 7: System tray and window lifecycle

**Files:**
- Create: `internal/app/tray.go`
- Create: `build/trayicon.png` (new asset)
- Modify: `main.go`

**Interfaces:**
- Consumes: `App.GetSettings()`, `sessionAPI.Disconnect`, the Wails `*application.App`.
- Produces: `func (a *App) SetupTray(icon []byte)`, `func (a *App) OnMainWindowClose() (preventClose bool)`.

- [ ] **Step 1: Create the tray icon asset**

Produce a 22×22 monochrome PNG template icon from the VCS radar mark drawn inline in `frontend/src/windows/main/screens/Welcome.tsx` (the concentric circles plus chevron). Save as `build/trayicon.png`. It must be black-on-transparent — macOS template icons are recoloured by the OS, so any colour in the source is wrong.

- [ ] **Step 2: Implement `internal/app/tray.go`**

```go
package app

import "github.com/wailsapp/wails/v3/pkg/application"

// SetupTray creates the system tray icon and menu. Call after the main window
// exists. If tray creation fails, close-to-tray is force-disabled (spec R13) --
// otherwise the user hides the app with no way to bring it back.
func (a *App) SetupTray(icon []byte) {
	tray := a.wailsApp.SystemTray.New()
	tray.SetTemplateIcon(icon)
	tray.SetTooltip("Vanguard Communications System")

	menu := application.NewMenu()
	menu.Add("Show VCS").OnClick(func(*application.Context) { a.showMainWindow() })
	menu.Add("Settings").OnClick(func(*application.Context) { a.showMainWindow() })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { a.quit() })
	tray.SetMenu(menu)
	tray.OnClick(func() { a.toggleMainWindow() })

	a.tray = tray
}
```

Also implement, on `App`:
- `showMainWindow()` / `toggleMainWindow()` — resolve the main window via the existing `windowsAPI` registry.
- `quit()` — call `a.sess.Disconnect(context.Background())` first so the server sees a clean leave rather than a dropped stream, then `a.wailsApp.Quit()`.
- `OnMainWindowClose() bool` — returns `true` (prevent close, hide instead) when `MinimizeToTray` is on **and** the tray was created successfully; otherwise `false`.
- `TrayAvailable() bool`.

- [ ] **Step 3: Wire it into `main.go`**

Three changes:

1. Embed the icon next to the existing assets embed:
```go
//go:embed build/trayicon.png
var trayIcon []byte
```

2. Replace the hard-coded Mac option (currently `main.go:62`) so it follows the setting:
```go
Mac: application.MacOptions{
    // Must be false when close-to-tray is on, or hiding the last window
    // quits the app -- the exact opposite of the intent.
    ApplicationShouldTerminateAfterLastWindowClosed: !cfg.General.MinimizeToTray,
},
```

3. After the main window is created, set up the tray and the close interceptor:
```go
gui.SetupTray(trayIcon)
```
and register the window close handler so it consults `gui.OnMainWindowClose()`, hiding the window instead of closing when it returns true. If `start_minimized` is set, hide the main window immediately after creation.

- [ ] **Step 4: Verify it builds on all three platforms**

```bash
go build ./... && GOOS=windows go build ./... && GOOS=linux go build ./...
```
Expected: all succeed.

- [ ] **Step 5: Verify tray behaviour by hand**

```bash
wails3 build && ./bin/vcs-client
```
Check, on this machine:
- Tray icon appears in the menu bar and is legible in both light and dark mode
- Clicking it toggles the main window
- Closing the main window hides it (does not quit) with `minimize_to_tray` on
- Tray menu → Quit actually exits

- [ ] **Step 6: Commit**

```bash
git add internal/app/tray.go build/trayicon.png main.go
git commit -m "feat(tray): add system tray with close-to-tray and clean quit

ApplicationShouldTerminateAfterLastWindowClosed now follows the
minimize_to_tray setting. Left hard-coded true, hiding the last window
would quit the app on macOS.

Quit disconnects the control session first so the server logs a proper
leave rather than a dropped stream.

If tray creation fails, close-to-tray is force-disabled (R13) -- a
silent tray failure would otherwise hide the app with no way back."
```

---

### Task 8: Frontend shared components — Panel, SettingRow, KeyChip

**Files:**
- Create: `frontend/src/shared/components/Panel.tsx`
- Create: `frontend/src/shared/components/SettingRow.tsx`
- Create: `frontend/src/shared/components/KeyChip.tsx`
- Test: `frontend/src/shared/components/KeyChip.test.tsx`

**Interfaces:**
- Consumes: existing CSS only. **No new CSS** — `.panel` (components.css:784), `.kbd` / `.kbd.listening` / `.kbd.unbound` / `.kbd-row` (1171–1197) are already ported.
- Produces:
```tsx
export function Panel({ title, children }: { title: string; children: React.ReactNode })
export function SettingRow({ label, desc, control }: { label: string; desc?: string; control: React.ReactNode })
export interface Capture { code: string; ctrl: boolean; alt: boolean; shift: boolean; super: boolean }
export function KeyChip({ binding, onCapture, onCancel }: {
  binding: string; onCapture: (c: Capture) => void; onCancel?: () => void })
```

- [ ] **Step 1: Write the failing KeyChip test**

Create `frontend/src/shared/components/KeyChip.test.tsx`:

```tsx
import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { KeyChip } from "./KeyChip";

describe("KeyChip", () => {
  it("renders the bound chord", () => {
    render(<KeyChip binding="Ctrl+E" onCapture={() => {}} />);
    expect(screen.getByText("Ctrl")).toBeInTheDocument();
    expect(screen.getByText("E")).toBeInTheDocument();
  });

  it("shows an em dash when unbound", () => {
    render(<KeyChip binding="" onCapture={() => {}} />);
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("enters listening state on click", () => {
    render(<KeyChip binding="" onCapture={() => {}} />);
    fireEvent.click(screen.getByText("—"));
    expect(screen.getByText("PRESS…")).toBeInTheDocument();
  });

  it("reports the physical code and modifiers, not the layout key", () => {
    const onCapture = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.keyDown(window, { code: "Digit1", key: "!", altKey: true });
    expect(onCapture).toHaveBeenCalledWith({
      code: "Digit1", ctrl: false, alt: true, shift: false, super: false,
    });
  });

  it("cancels on Escape without capturing", () => {
    const onCapture = vi.fn();
    const onCancel = vi.fn();
    render(<KeyChip binding="F1" onCapture={onCapture} onCancel={onCancel} />);
    fireEvent.click(screen.getByText("F1"));
    fireEvent.keyDown(window, { code: "Escape", key: "Escape" });
    expect(onCapture).not.toHaveBeenCalled();
    expect(onCancel).toHaveBeenCalled();
  });

  it("ignores bare modifier presses while listening", () => {
    const onCapture = vi.fn();
    render(<KeyChip binding="" onCapture={onCapture} />);
    fireEvent.click(screen.getByText("—"));
    fireEvent.keyDown(window, { code: "ShiftLeft", key: "Shift", shiftKey: true });
    expect(onCapture).not.toHaveBeenCalled();
    expect(screen.getByText("PRESS…")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/shared/components/KeyChip.test.tsx`
Expected: FAIL — cannot resolve `./KeyChip`.

- [ ] **Step 3: Implement the three components**

`KeyChip.tsx` requirements (the test pins all of them):
- Renders `binding.split("+")` as `.kbd` spans joined by `.plus` separators inside a `.kbd-row`; renders `—` in a `.kbd.unbound` when `binding` is empty.
- Click toggles listening; while listening the chip shows `PRESS…` and carries the `listening` class.
- While listening, a `keydown` listener on `window` calls `preventDefault()` and emits `{code, ctrl, alt, shift, super}` from `e.code` / `e.ctrlKey` / `e.altKey` / `e.shiftKey` / `e.metaKey`.
- `Escape` cancels: calls `onCancel`, does not call `onCapture`.
- Bare modifier codes (`ShiftLeft`, `ControlLeft`, `AltRight`, `MetaLeft`, …) are ignored so the chip keeps listening for a real key.
- The listener is removed on unmount and when listening stops, and `onCancel` fires on unmount-while-listening so the backend never stays suspended.

`Panel.tsx` and `SettingRow.tsx` are direct ports of the prototype's markup using the existing classes.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && npx vitest run src/shared/components/KeyChip.test.tsx`
Expected: PASS, 6 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/components/
git commit -m "feat(ui): add Panel, SettingRow and KeyChip components

KeyChip captures KeyboardEvent.code rather than .key so what the user
sees matches what the OS registers -- .key varies by keyboard layout,
and Alt+1 yields different values on non-US layouts.

Escape cancels, bare modifiers are ignored, and cancelling on unmount
means the backend never stays suspended after the UI goes away.

No new CSS: .panel and .kbd were already ported in Phase 1."
```

---

### Task 9: Frontend settings store and API client

**Files:**
- Create: `frontend/src/shared/store/settings.ts`
- Modify: `frontend/src/shared/api/client.ts`
- Test: `frontend/src/shared/store/settings.test.ts`

**Interfaces:**
- Consumes: the bindings from Task 6.
- Produces:
```ts
export interface Settings { start_minimized: boolean; minimize_to_tray: boolean;
  show_transmitter_name: boolean; play_connection_sounds: boolean; radio_switch_as_ptt: boolean }
export interface Keybind { action_id: string; label: string; desc: string;
  category: string; kind: string; chord: string }
export interface HotkeyState { registered: boolean; error: string }
export const useSettings: UseBoundStore<...>   // { settings, keybinds, hotkeys, setSettings, setKeybinds, setHotkeyState }
// api gains: getSettings, setSettings, getKeybinds, setKeybind, clearKeybind,
//            beginCapture, endCapture, getHotkeyState
```

- [ ] **Step 1: Write the failing store test**

Create `frontend/src/shared/store/settings.test.ts`:

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { useSettings } from "./settings";

const blank = {
  start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
  play_connection_sounds: true, radio_switch_as_ptt: false,
};

describe("settings store", () => {
  beforeEach(() => {
    useSettings.setState({ settings: null, keybinds: [], hotkeys: { registered: false, error: "" } });
  });

  it("starts empty", () => {
    expect(useSettings.getState().settings).toBeNull();
    expect(useSettings.getState().keybinds).toEqual([]);
  });

  it("stores settings", () => {
    useSettings.getState().setSettings(blank);
    expect(useSettings.getState().settings?.minimize_to_tray).toBe(true);
  });

  it("replaces the whole keybind list rather than merging", () => {
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", chord: "F1" },
      { action_id: "b", label: "B", desc: "", category: "global", kind: "press", chord: "F2" },
    ]);
    useSettings.getState().setKeybinds([
      { action_id: "a", label: "A", desc: "", category: "global", kind: "press", chord: "F3" },
    ]);
    const kb = useSettings.getState().keybinds;
    expect(kb).toHaveLength(1);
    expect(kb[0].chord).toBe("F3");
  });

  it("stores hotkey registration state", () => {
    useSettings.getState().setHotkeyState({ registered: false, error: "permission denied" });
    expect(useSettings.getState().hotkeys.error).toBe("permission denied");
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/shared/store/settings.test.ts`
Expected: FAIL — cannot resolve `./settings`.

- [ ] **Step 3: Implement the store and extend the api wrapper**

`settings.ts` follows the existing Zustand pattern in `shared/store/session.ts`. Add the eight new methods to the `api` object in `client.ts`, delegating to the generated `App` bindings, matching the existing style.

- [ ] **Step 4: Run the tests**

Run: `cd frontend && npx vitest run src/shared/store/settings.test.ts`
Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/shared/store/settings.ts frontend/src/shared/store/settings.test.ts frontend/src/shared/api/client.ts
git commit -m "feat(ui): add settings store and API client methods

The store lives in shared/ rather than windows/main/ because the Comms
popout needs keybind data once PTT is live.

setKeybinds replaces the whole list rather than merging, matching the
backend's full-list event."
```

---

### Task 10: Settings screen shell, General section, deferred stubs

**Files:**
- Create: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx`
- Create: `frontend/src/windows/main/screens/settings/sections/General.tsx`
- Create: `frontend/src/windows/main/screens/settings/sections/Deferred.tsx`
- Modify: `frontend/src/windows/main/MainApp.tsx`
- Test: `frontend/src/windows/main/screens/settings/SettingsScreen.test.tsx`

**Interfaces:**
- Consumes: `Panel`, `SettingRow`, `Toggle`, `useSettings`, `api`.
- Produces: `export function SettingsScreen()`.

- [ ] **Step 1: Write the failing screen test**

Create `frontend/src/windows/main/screens/settings/SettingsScreen.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

import { SettingsScreen } from "./SettingsScreen";
import { useSettings } from "../../../../shared/store/settings";

vi.mock("../../../../shared/api/client", () => ({
  api: {
    getSettings: vi.fn().mockResolvedValue({
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, radio_switch_as_ptt: false,
    }),
    setSettings: vi.fn().mockResolvedValue(undefined),
    getKeybinds: vi.fn().mockResolvedValue([]),
    getHotkeyState: vi.fn().mockResolvedValue({ registered: true, error: "" }),
    setKeybind: vi.fn(), clearKeybind: vi.fn(),
    beginCapture: vi.fn(), endCapture: vi.fn(),
  },
}));

describe("SettingsScreen", () => {
  beforeEach(() => {
    useSettings.setState({
      settings: {
        start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
        play_connection_sounds: true, radio_switch_as_ptt: false,
      },
      keybinds: [],
      hotkeys: { registered: true, error: "" },
    });
  });

  it("renders all eight sections in the rail", () => {
    render(<SettingsScreen />);
    for (const label of ["General", "Keybinds", "Audio & Sounds", "Radio Effects",
                         "Profiles & Layouts", "Notifications", "Miscellaneous", "Legacy"]) {
      expect(screen.getByText(label)).toBeInTheDocument();
    }
  });

  it("opens on General with its toggles", () => {
    render(<SettingsScreen />);
    expect(screen.getByText("Start minimized")).toBeInTheDocument();
    expect(screen.getByText("Minimize to tray")).toBeInTheDocument();
  });

  it("shows the deferred notice for a not-yet-built section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Audio & Sounds"));
    expect(screen.getByText(/Arrives in Phase 4/i)).toBeInTheDocument();
  });

  it("names the right phase per deferred section", () => {
    render(<SettingsScreen />);
    fireEvent.click(screen.getByText("Profiles & Layouts"));
    expect(screen.getByText(/Arrives in Phase 7/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/windows/main/screens/settings/`
Expected: FAIL — cannot resolve `./SettingsScreen`.

- [ ] **Step 3: Implement the screen, General section and Deferred stub**

`SettingsScreen.tsx` mirrors the prototype's `ScreenSettings`: a 200px `.nav-item` rail on the left, scrolling body on the right, section held in local `useState`. On mount it hydrates via `api.getSettings()`, `api.getKeybinds()`, `api.getHotkeyState()` into the store and subscribes to `EV.settingsChanged`, `EV.keybindsChanged`, `EV.hotkeysState`, unsubscribing on unmount.

Section list and phase mapping:

| key | label | component |
|---|---|---|
| `general` | General | `<General/>` |
| `keybinds` | Keybinds | `<Keybinds/>` (Task 11; render `<Deferred title="KEYBINDS" phase={3} .../>` until then) |
| `audio` | Audio & Sounds | `<Deferred phase={4} items={["Device selection","AGC","Noise suppression","VU metering"]}/>` |
| `effects` | Radio Effects | `<Deferred phase={4} items={["Radio FX","Squelch","Static","TX/RX tones"]}/>` |
| `profiles` | Profiles & Layouts | `<Deferred phase={7} items={["Save/load profiles","Import & export","Window layouts"]}/>` |
| `notif` | Notifications | `<Deferred phase={7} items={["Alert rules","Sound alerts","Server notifications"]}/>` |
| `misc` | Miscellaneous | `<Deferred phase={10} items={["Telemetry","Crash reports","Auto-update","Update channel"]}/>` |
| `legacy` | Legacy | `<Deferred phase={4} items={["Legacy SRS audio engine","Legacy overlay","Disable hardware acceleration"]}/>` |

`General.tsx` renders the five `SettingRow` + `Toggle` pairs from the prototype. Each change calls `api.setSettings` with the full struct; the store updates from the `settings:changed` event, **not** optimistically.

- [ ] **Step 4: Wire into MainApp**

In `frontend/src/windows/main/MainApp.tsx`, import `SettingsScreen` and change the screen switch so `view === "settings"` renders `<SettingsScreen />` instead of falling through to `<Placeholder />`.

- [ ] **Step 5: Run the tests**

Run: `cd frontend && npx vitest run && npx tsc --noEmit`
Expected: PASS, no type errors.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/windows/main/
git commit -m "feat(ui): add Settings screen with General section and deferred stubs

All eight sections from the design render. The six whose subsystems do
not exist yet show an honest 'arrives in Phase N' panel rather than
fake controls that lie about what they do.

General writes through the backend and re-renders from the
settings:changed event rather than updating optimistically."
```

---

### Task 11: Keybinds section

**Files:**
- Create: `frontend/src/windows/main/screens/settings/sections/Keybinds.tsx`
- Modify: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx` (swap the stub for the real section)
- Test: `frontend/src/windows/main/screens/settings/sections/Keybinds.test.tsx`

**Interfaces:**
- Consumes: `useSettings`, `api`, `Panel`, `KeyChip`, `Button`.
- Produces: `export function Keybinds()`.

- [ ] **Step 1: Write the failing section test**

Create `frontend/src/windows/main/screens/settings/sections/Keybinds.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

import { Keybinds } from "./Keybinds";
import { useSettings } from "../../../../../shared/store/settings";

const setKeybind = vi.fn();
const beginCapture = vi.fn().mockResolvedValue(undefined);
const endCapture = vi.fn().mockResolvedValue(undefined);

vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setKeybind: (...a: unknown[]) => setKeybind(...a),
    clearKeybind: vi.fn().mockResolvedValue(undefined),
    beginCapture: () => beginCapture(),
    endCapture: () => endCapture(),
  },
}));

const rows = [
  { action_id: "global.ptt", label: "Global PTT", desc: "Transmits on the Selected radio",
    category: "global", kind: "hold", chord: "" },
  { action_id: "global.mute_toggle", label: "Mute toggle", desc: "",
    category: "global", kind: "press", chord: "M" },
  { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", desc: "",
    category: "per_radio", kind: "hold", chord: "F1" },
];

describe("Keybinds section", () => {
  beforeEach(() => {
    setKeybind.mockReset().mockResolvedValue({ stolen: null });
    beginCapture.mockClear();
    endCapture.mockClear();
    useSettings.setState({
      settings: null, keybinds: rows, hotkeys: { registered: true, error: "" },
    });
  });

  it("groups bindings by category", () => {
    render(<Keybinds />);
    expect(screen.getByText("GLOBAL")).toBeInTheDocument();
    expect(screen.getByText("PER-RADIO BINDINGS")).toBeInTheDocument();
    expect(screen.getByText("Global PTT")).toBeInTheDocument();
    expect(screen.getByText("R01 · GUARD (PTT)")).toBeInTheDocument();
  });

  it("suspends hotkeys while capturing", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
  });

  it("sends the capture and re-arms hotkeys", async () => {
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F2", key: "F2" });
    await waitFor(() => {
      expect(setKeybind).toHaveBeenCalledWith("global.ptt", {
        code: "F2", ctrl: false, alt: false, shift: false, super: false,
      });
      expect(endCapture).toHaveBeenCalled();
    });
  });

  it("reports which action lost a stolen key", async () => {
    setKeybind.mockResolvedValue({
      stolen: { action_id: "radio.1.ptt", label: "R01 · GUARD (PTT)", chord: "F1" },
    });
    render(<Keybinds />);
    fireEvent.click(screen.getAllByText("—")[0]);
    await waitFor(() => expect(beginCapture).toHaveBeenCalled());
    fireEvent.keyDown(window, { code: "F1", key: "F1" });
    await waitFor(() =>
      expect(screen.getByText(/F1 taken from R01 · GUARD \(PTT\)/i)).toBeInTheDocument(),
    );
  });

  it("warns when global hotkeys failed to register", () => {
    useSettings.setState({
      settings: null, keybinds: rows,
      hotkeys: { registered: false, error: "permission denied" },
    });
    render(<Keybinds />);
    expect(screen.getByText(/global hotkeys unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/permission denied/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && npx vitest run src/windows/main/screens/settings/sections/Keybinds.test.tsx`
Expected: FAIL — cannot resolve `./Keybinds`.

- [ ] **Step 3: Implement `Keybinds.tsx`**

Requirements, all pinned by the tests:
- Reads `keybinds` from the store and groups by `category` into four `Panel`s titled `GLOBAL`, `CHANNEL HOTKEYS`, `PER-RADIO BINDINGS`, `QUICK-STATUS HOTKEYS`. A group with no rows is not rendered.
- Per-radio rows render in a `.tbl` table matching the prototype (Radio | PTT | Select | UNBIND); the other groups render as rows with a `KeyChip` and an UNBIND button.
- Clicking a chip calls `api.beginCapture()` first, then lets `KeyChip` listen.
- On capture: `api.setKeybind(actionID, capture)` → then `api.endCapture()` in a `finally` block so **the backend re-arms even if the call throws**.
- On cancel (Escape/blur/unmount): `api.endCapture()`.
- If the result carries `stolen`, render an inline `.cap` warning in `var(--ac-warn)`: `⚠ {chord} taken from {label}`. Clear it on the next capture.
- When `hotkeys.registered` is false, render a banner above the panels: "Global hotkeys unavailable — {error}", styled with `var(--ac-alert)`.
- If Task 5 Step 1 found no key-release support, additionally mark `kind === "hold"` rows with "hold-to-talk unavailable with the current backend".

Then in `SettingsScreen.tsx`, replace the `keybinds` section's placeholder with `<Keybinds />`.

- [ ] **Step 4: Run the full frontend suite**

Run: `cd frontend && npx vitest run && npx tsc --noEmit && npm run build`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/windows/main/screens/settings/
git commit -m "feat(ui): add Keybinds settings section

Capture calls beginCapture before listening and endCapture in a finally
block, so the backend re-arms its OS registrations even if the write
fails.

Conflicts report inline which action lost the key, and a failed global
registration shows a banner rather than silently pretending the
bindings are live."
```

---

### Task 12: Verify end-to-end and close out the phase

**Files:**
- Modify: `docs/ROADMAP.md`
- Modify: `CLAUDE.md`
- Modify: `docs/superpowers/specs/2026-09-15-vcs-client-phase-3-settings-keybinds-design.md` (R11 outcome)

- [ ] **Step 1: Run every automated check**

```bash
go vet ./... && go test -race ./... && \
cd frontend && npx tsc --noEmit && npx vitest run && npm run build && cd .. && \
wails3 build
```
Expected: all green. Fix anything that is not before continuing.

- [ ] **Step 2: Walk the Definition of Done by hand**

Launch `./bin/vcs-client` and verify each of the 12 items in spec §13. The ones only a human can confirm:

- Bind a key, quit, relaunch → the binding is still there (DoD 6)
- Bind a key already in use → the previous owner shows unbound and the note names it (DoD 7)
- Focus another application, press a bound key → `hotkey:pressed` fires (DoD 8). Check the log or a temporary console line
- Open the Comms popout, change a setting in the main window → the popout sees it without being reopened (DoD 10)
- Close the main window with `minimize_to_tray` on → it hides; tray click restores it (DoD 3)
- Quit from the tray → server logs a clean leave (DoD 4)

Record any failures as new tasks rather than patching them silently.

- [ ] **Step 3: Record the R11 outcome in the spec**

Update the R11 row in spec §12 with what Task 5 Step 1 actually found, and whether hold-to-talk registration shipped or was deferred.

- [ ] **Step 4: Update the roadmap and CLAUDE.md**

In `docs/ROADMAP.md`, set Phase 3 to `[x]` with the completion date and a one-line summary. In `CLAUDE.md`, update the "Current status" table: Phase 3 `[x]`, and name the next phase (Phase 4 — Audio I/O).

- [ ] **Step 5: Commit**

```bash
git add docs/ CLAUDE.md
git commit -m "docs: mark Phase 3 complete

Settings screen, persisted keybinds with steal-on-conflict, global
hotkey registration and system tray all landed. Records the R11
outcome for x/hotkey key-release support."
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| §3 decisions | 1–11 throughout |
| §4 package layout | 2 (chord), 3 (keybinds), 5 (hotkeys), 4 (config), 6 (app) |
| §5.1 action registry | 3 |
| §5.2 chord | 2 |
| §5.3 `[keybinds]` | 4 |
| §5.4 `[general]` | 4 |
| §6 bindings | 6 |
| §6.1 events | 6 |
| §7 capture lifecycle | 6 (backend), 8 + 11 (frontend) |
| §8 tray + lifecycle | 7 |
| §9 frontend structure | 8, 9, 10, 11 |
| §10 Wails upgrade | 1 |
| §11 testing | every task's test steps; manual checklist in 12 |
| §12 R11–R16 | R11 in 5, R12 in 6 + 11, R13 in 7, R14 in 3 (defaults), R15 in 6 |
| §13 DoD | 12 |

**Placeholder scan:** none. The one genuinely conditional branch (R11, Task 5 Step 1) states both outcomes and what each means for Tasks 5 and 11.

**Type consistency:** `CaptureDTO`/`Capture` fields (`code`, `ctrl`, `alt`, `shift`, `super`) match across Tasks 6, 8, 9 and 11. `keybinds.Stolen{ActionID, Chord}` → `StolenDTO{ActionID, Label, Chord}` → TS `{action_id, label, chord}` is consistent. `hotkeys.Binding{Chord, Hold}` is used identically in Tasks 5 and 6. `chord.FromCode` has the same signature in Tasks 2 and 6.

**Known gap:** `internal/hotkeys/registrar_x.go` has no unit test. That is deliberate — it is the OS seam, which is why all the logic lives in `Manager`. It is covered by Task 7 Step 5 and Task 12 Step 2.
