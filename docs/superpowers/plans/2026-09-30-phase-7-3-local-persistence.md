# Phase 7.3 — Local Persistence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Radio profiles as shareable `.vcs.json` snapshots (radios + a fully customizable Comms layout), and a local transmission log fed by RX stream detection and own-TX target diffing.

**Architecture:** Two pure Go packages (`internal/profile`, `internal/history`) with no dependencies beyond stdlib, bound into `internal/app` the way `internal/notify` already is: Go owns the store, the frontend is a view. `config.toml` stays authoritative for live state; a profile is a serialization of it. `internal/voice` gains one callback and one tuning field; everything else is app-layer wiring.

**Tech Stack:** Go 1.x (`log/slog`, `encoding/json`, `go:embed`, `crypto/sha256`), Wails v3.0.0-beta.22 (native dialogs, event emitter), React 18 + TypeScript + Zustand + Vite, `vitest`.

**Spec:** `docs/superpowers/specs/2026-09-30-vcs-client-phase-7-3-local-persistence-design.md`

## Global Constraints

- **Every Go invocation carries `-tags purego`**, with `GOCACHE=$TMPDIR/vcs-gocache`. The default GOCACHE is sandbox-blocked.
- **`srspb/` must exist before anything compiles.** If `go build` reports *"no required module provides package .../srspb"*, run `buf generate` (install with `go install github.com/bufbuild/buf/cmd/buf@v1.47.2`). There is no Makefile; `Taskfile.yml`'s `proto` task is the documented entry point.
- **Typecheck with `(cd frontend && npx tsc --noEmit)`.** The `npx --prefix frontend tsc --noEmit` form prints a help banner and exits 0 **without checking anything**.
- **`internal/voice` tests need `dangerouslyDisableSandbox`** — the sandbox blocks `bind(2)` and its tests fail with "listen udp: operation not permitted".
- **gopls is unreliable in this repo.** 13 false positives in Phase 7.2 across three kinds, two of which would be hard compile errors if real. Never act on its diagnostics; `go build`/`go vet` is the only authority.
- **Frequencies are `uint32` kHz, never floats.** The server compares advertised frequencies with exact `float32` equality (`vcs-srs-server/state/server.go:211`). Derive the wire form only with `voice.KHz(...).MHz32()`; never round-trip through `RadioDTO.Frequency` on the profile load path.
- **Opus codec geometry is a wire contract.** Nothing in this phase touches it.
- **No `srs.proto` changes.** The profile→server path reuses `UpdateRadioInfo` exactly as it exists.
- **Any click/pointer handler on a non-button element needs `role="button"`, `tabIndex` and Enter/Space handling in the same commit**, or SonarCloud's `typescript:S1082` fails the PR gate.
- **Both window roots render inside `React.StrictMode`.** Any effect with side-effecting cleanup needs a StrictMode test **plus** a control proving the real-unmount path still fires once.
- **Shell trap:** `cmd | grep -v x; echo $?` reports **grep's** status, and grep exits 1 when it filters everything out. Use `${PIPESTATUS[0]}`.
- **No `Co-Authored-By` trailers on commits.**

## Review Focus

Five input classes the spec implies but does not enumerate, each pinned to a test in the task that owns the code:

1. **A `frequency_khz` of 0, or above the 24-bit ceiling (16777215), in a hand-edited or imported profile.** `voice.KHz.Valid()` already exists for this. Expected: the radio is rejected at load with a notification naming it, never advertised to the server as a frequency it cannot relay. *(Task 1)*
2. **A layout block with `w` or `h` of zero or negative.** Expected: clamped to the minimum block size on load — an invisible block cannot be grabbed to resize it back, so RESET would be the only escape. *(Task 1)*
3. **`profiles_dir` pointing at a path that exists but is a regular file, or is unwritable.** Expected: one notification naming the path; the Profiles screen renders empty rather than blank, and SAVE reports the failure instead of appearing to succeed. *(Task 2)*
4. **A profile with an empty `radios` array.** Expected: it loads (it is a legal profile), selection resolves to 0, and the Comms window shows its existing "No radios" empty state rather than a dead PTT with a lit indicator. *(Task 8)*
5. **Duplicate radio IDs inside one profile.** Expected: rejected at load. `App.resolveTXTarget` returns the first match, so a duplicate makes which frequency you transmit on depend on array order. *(Task 1)*

---

## File Structure

**Created:**
- `internal/profile/profile.go` — `Document`, `Radio`, `Layout`, `Block` types; `Encode`/`Decode`; `Validate`; `Reconcile`.
- `internal/profile/store.go` — `List`, `Read`, `Write`, `Delete`, `Slug`.
- `internal/profile/builtin.go` — `go:embed` of the shipped defaults; `Seed`.
- `internal/profile/builtin/standard-fleet.vcs.json` — the one shipped default.
- `internal/history/history.go` — `Entry`, `Log`, ring semantics.
- `internal/history/store.go` — `Load`, `Flush`, debounce, `CSV`.
- `internal/app/profiles.go` — mapping, LOAD/SAVE/dirty, bindings, seeding wiring.
- `internal/app/history.go` — RX/TX sources, resolution, events.
- `frontend/src/windows/main/screens/Profiles.tsx`, `History.tsx`
- `frontend/src/shared/store/profile.ts`, `history.ts`
- `frontend/src/windows/comms/RadioBlock.tsx` — the resizable wrapper.
- `docs/superpowers/plans/2026-09-30-phase-7-3-manual-verification.md`

**Modified:**
- `internal/config/config.go` — `ActiveProfile`, `ProfilesDir`, `BuiltinProfiles`, `CommsLayout`, `Voice.HistoryIdleMS`.
- `internal/config/paths.go` — `ProfilesDirPath`, `HistoryFilePath`.
- `internal/voice/session.go` — `Options.OnRX`, `Options.HistoryIdleMS`, RX delivery goroutine.
- `internal/voice/rx.go` — per-stream `started`/`ended`, the detection branch.
- `internal/app/voice.go` — `installTXTargetsLocked`, selection re-validation.
- `internal/app/app.go` — `ServiceShutdown` config flush.
- `internal/app/dto.go`, `internal/events/events.go`, `main.go`
- `frontend/src/windows/main/MainApp.tsx`, `frontend/src/windows/comms/CommsApp.tsx`
- `frontend/src/shared/api/client.ts`, `events.ts`

---

Task list follows. Tasks 1–7 are the pure layers and have no dependencies on each other beyond what their **Interfaces** block names; 8–11 are app wiring; 12–15 are frontend; 16 is integration.

---

### Task 1: `internal/profile` — document types, codec, validation

**Files:**
- Create: `internal/profile/profile.go`
- Test: `internal/profile/profile_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Radio struct { ID uint32; Name string; FrequencyKHz uint32; Enabled, IsIntercom bool }`
  - `type WindowSize struct { W, H int }`
  - `type Block struct { RadioID uint32; W, H int }`
  - `type Layout struct { Window WindowSize; Blocks []Block }`
  - `type Document struct { SchemaVersion int; Name, Description, Author string; CreatedAt, ModifiedAt time.Time; Radios []Radio; Layout Layout }`
  - `const SchemaVersion = 1`, `const MaxFrequencyKHz = 1<<24 - 1`
  - `const (MinBlockW=240; MinBlockH=96; DefaultBlockW=516; DefaultBlockH=180; DefaultWindowW=540; DefaultWindowH=720)`
  - `func Decode(b []byte) (*Document, error)`, `func Encode(d *Document) ([]byte, error)`
  - `func (d *Document) Validate() error`, `func (d *Document) Reconcile()`
  - `var ErrSchemaTooNew error`

- [ ] **Step 1: Write the failing tests**

Create `internal/profile/profile_test.go`:

```go
package profile

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeRoundTrip(t *testing.T) {
	src := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "Fleet Op",
		Radios:        []Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}},
		Layout: Layout{
			Window: WindowSize{W: 540, H: 720},
			Blocks: []Block{{RadioID: 1, W: 516, H: 180}},
		},
	}
	b, err := Encode(src)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(b), `"frequency_khz": 118500`) {
		t.Fatalf("frequency must serialise as an integer, got:\n%s", b)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("FrequencyKHz = %d, want 118500", got.Radios[0].FrequencyKHz)
	}
	if got.Layout.Blocks[0].W != 516 {
		t.Fatalf("block W = %d, want 516", got.Layout.Blocks[0].W)
	}
}

func TestDecodeIgnoresUnknownFields(t *testing.T) {
	in := []byte(`{"schema_version":1,"name":"x","overlay":{"anchor":"top-right"},
	  "radios":[{"id":1,"name":"a","frequency_khz":118500,"enabled":true,"enc":true,"key":4}]}`)
	d, err := Decode(in)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(d.Radios) != 1 || d.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("radios = %+v", d.Radios)
	}
}

func TestValidateRejectsNewerSchema(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion + 1, Name: "x"}
	if err := d.Validate(); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("err = %v, want ErrSchemaTooNew", err)
	}
}

// Review Focus #1.
func TestValidateRejectsOutOfRangeFrequency(t *testing.T) {
	for _, tc := range []struct {
		name string
		khz  uint32
	}{{"zero", 0}, {"above 24-bit ceiling", MaxFrequencyKHz + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Document{
				SchemaVersion: SchemaVersion,
				Name:          "x",
				Radios:        []Radio{{ID: 1, Name: "Fleet", FrequencyKHz: tc.khz}},
			}
			err := d.Validate()
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), "Fleet") {
				t.Fatalf("error must name the radio, got: %v", err)
			}
		})
	}
}

// Review Focus #5.
func TestValidateRejectsDuplicateRadioIDs(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 1, Name: "b", FrequencyKHz: 122750},
		},
	}
	if err := d.Validate(); err == nil {
		t.Fatal("duplicate radio ids must be rejected: resolveTXTarget takes the first match, so which frequency transmits would depend on array order")
	}
}

func TestValidateRejectsEmptyName(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "  "}
	if err := d.Validate(); err == nil {
		t.Fatal("want error for blank name")
	}
}

func TestValidateAcceptsEmptyRadios(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "empty", Radios: []Radio{}}
	if err := d.Validate(); err != nil {
		t.Fatalf("an empty profile is legal, got: %v", err)
	}
}

func TestReconcileAppendsMissingBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios: []Radio{
			{ID: 1, Name: "a", FrequencyKHz: 118500},
			{ID: 2, Name: "b", FrequencyKHz: 122750},
		},
		Layout: Layout{Blocks: []Block{{RadioID: 1, W: 300, H: 100}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 2 {
		t.Fatalf("blocks = %+v, want one appended for radio 2", d.Layout.Blocks)
	}
	if last := d.Layout.Blocks[1]; last.RadioID != 2 || last.W != DefaultBlockW || last.H != DefaultBlockH {
		t.Fatalf("appended block = %+v, want radio 2 at default size", last)
	}
}

func TestReconcileDropsOrphanBlock(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, W: 300, H: 100}, {RadioID: 9, W: 300, H: 100}}},
	}
	d.Reconcile()
	if len(d.Layout.Blocks) != 1 || d.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want orphan dropped", d.Layout.Blocks)
	}
}

// Review Focus #2.
func TestReconcileClampsDegenerateBlockSizes(t *testing.T) {
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "x",
		Radios:        []Radio{{ID: 1, Name: "a", FrequencyKHz: 118500}},
		Layout:        Layout{Blocks: []Block{{RadioID: 1, W: 0, H: -40}}},
	}
	d.Reconcile()
	if got := d.Layout.Blocks[0]; got.W != MinBlockW || got.H != MinBlockH {
		t.Fatalf("block = %+v, want clamped to %dx%d -- an invisible block cannot be grabbed to resize it back",
			got, MinBlockW, MinBlockH)
	}
}

func TestReconcileFillsZeroWindowSize(t *testing.T) {
	d := &Document{SchemaVersion: SchemaVersion, Name: "x"}
	d.Reconcile()
	if d.Layout.Window.W != DefaultWindowW || d.Layout.Window.H != DefaultWindowH {
		t.Fatalf("window = %+v, want the %dx%d default", d.Layout.Window, DefaultWindowW, DefaultWindowH)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/`
Expected: FAIL — undefined `Document`, `Encode`, `Decode`, `Validate`, `Reconcile`.

- [ ] **Step 3: Write the implementation**

Create `internal/profile/profile.go`:

```go
// Package profile is the .vcs.json radio-profile document: its types, its
// codec, and the rules that decide whether a file on disk is loadable.
//
// It imports nothing outside the standard library, deliberately. A profile
// is a USER-FACING, shareable file; binding its shape to internal/config's
// TOML structs would mean a config refactor silently changed a format
// people already have on disk and trade with each other. internal/app owns
// the mapping between the two, the same discipline internal/config itself
// applies to Audio/Voice/Keybinds.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SchemaVersion is the format version this build writes and the highest it
// can read. A file claiming more is refused outright, never partially
// applied.
const SchemaVersion = 1

// MaxFrequencyKHz is the largest frequency the 24-bit voice wire field can
// carry. It mirrors internal/voice's unexported maxKHz; this package must
// not import internal/voice, so the bound is restated rather than shared.
const MaxFrequencyKHz = 1<<24 - 1

// Block-geometry bounds. MinBlockW/MinBlockH are both the floor a resize
// handle enforces and the value Reconcile clamps a degenerate stored block
// up to: a zero-size block renders as nothing, and nothing is not
// draggable, so without the clamp a hand-edited file could only be escaped
// with RESET.
const (
	MinBlockW = 240
	MinBlockH = 96

	DefaultBlockW = 516
	DefaultBlockH = 180

	// DefaultWindowW/H match internal/app.defaultGeometry("comms").
	DefaultWindowW = 540
	DefaultWindowH = 720
)

// ErrSchemaTooNew reports a file written by a newer build.
var ErrSchemaTooNew = errors.New("profile: schema version is newer than this build supports")

// Radio is one radio preset inside a profile.
//
// FrequencyKHz is an INTEGER and must stay one. The server decides whether
// to relay a transmission by comparing advertised frequencies with exact
// float32 equality (vcs-srs-server/state/server.go), so a format that
// round-tripped this through a float could silently drop a radio out of
// range with nothing logged on either side.
type Radio struct {
	ID           uint32 `json:"id"`
	Name         string `json:"name"`
	FrequencyKHz uint32 `json:"frequency_khz"`
	Enabled      bool   `json:"enabled"`
	IsIntercom   bool   `json:"is_intercom"`
}

// WindowSize is the Comms window's stored size in screen pixels.
type WindowSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Block is one radio's tile in the Comms flow grid.
type Block struct {
	RadioID uint32 `json:"radio_id"`
	W       int    `json:"w"`
	H       int    `json:"h"`
}

// Layout is the whole Comms arrangement. Blocks is ORDERED and that order
// IS the flow order -- there is deliberately no separate index field to
// keep consistent with the slice.
type Layout struct {
	Window WindowSize `json:"window"`
	Blocks []Block    `json:"blocks"`
}

// Document is one .vcs.json file.
//
// SelectedRadioID is deliberately ABSENT. It is runtime-adjacent state that
// lives in config.toml for session continuity; storing it here would mean
// the dirty check compared it, lighting the dirty dot on every
// radio.<n>.select hotkey press mid-operation. See the phase spec's 4.3.
type Document struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Author        string    `json:"author,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	ModifiedAt    time.Time `json:"modified_at"`
	Radios        []Radio   `json:"radios"`
	Layout        Layout    `json:"layout"`
}

// Decode parses one profile file. Unknown fields are IGNORED, not rejected:
// a file carrying the design prototype's enc/key or overlay keys, or fields
// a newer build added within the same schema version, must still load.
func Decode(b []byte) (*Document, error) {
	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	return &d, nil
}

// Encode renders a profile with two-space indentation, matching
// windowstate.Save, so a user who opens the file in an editor sees
// something readable.
func Encode(d *Document) ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal profile: %w", err)
	}
	return b, nil
}

// Validate reports whether this document may be applied.
//
// An EMPTY radio set is legal: a profile with no radios is a coherent thing
// to save. What is not legal is a radio the client could not honestly
// advertise -- an out-of-range frequency, or a duplicate id, which would
// make App.resolveTXTarget's first-match lookup order-dependent.
func (d *Document) Validate() error {
	if d.SchemaVersion > SchemaVersion {
		return fmt.Errorf("%w: file says %d, this build reads %d",
			ErrSchemaTooNew, d.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("profile: name is empty")
	}
	seen := make(map[uint32]string, len(d.Radios))
	for _, r := range d.Radios {
		if prev, dup := seen[r.ID]; dup {
			return fmt.Errorf("profile: duplicate radio id %d (%q and %q)", r.ID, prev, r.Name)
		}
		seen[r.ID] = r.Name
		if r.FrequencyKHz == 0 || r.FrequencyKHz > MaxFrequencyKHz {
			return fmt.Errorf("profile: radio %q has frequency %d kHz, outside 1..%d",
				r.Name, r.FrequencyKHz, MaxFrequencyKHz)
		}
	}
	return nil
}

// Reconcile makes the layout consistent with the radio set and clamps
// degenerate geometry. It never fails: every case has a defined repair, and
// refusing to load a profile over a bad block size would be worse than
// fixing it.
//
// Call it after Validate on every read AND before every write, so a
// hand-edited file is normalised the first time it is touched.
func (d *Document) Reconcile() {
	if d.Layout.Window.W <= 0 {
		d.Layout.Window.W = DefaultWindowW
	}
	if d.Layout.Window.H <= 0 {
		d.Layout.Window.H = DefaultWindowH
	}

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
		if b.W < MinBlockW {
			b.W = MinBlockW
		}
		if b.H < MinBlockH {
			b.H = MinBlockH
		}
		have[b.RadioID] = true
		kept = append(kept, b)
	}
	for _, r := range d.Radios {
		if !have[r.ID] {
			kept = append(kept, Block{RadioID: r.ID, W: DefaultBlockW, H: DefaultBlockH})
		}
	}
	d.Layout.Blocks = kept
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -v`
Expected: PASS, all cases.

- [ ] **Step 5: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/profile/
git add internal/profile/
git commit -m "feat(profile): .vcs.json document types, codec and validation"
```

---

### Task 2: `internal/profile` — directory store

**Files:**
- Create: `internal/profile/store.go`
- Test: `internal/profile/store_test.go`

**Interfaces:**
- Consumes: Task 1's `Document`, `Encode`, `Decode`, `Validate`, `Reconcile`, `WindowSize`, `Block`.
- Produces:
  - `const Ext = ".vcs.json"`
  - `type Summary struct { Path, Name, Description, Author string; ModifiedAt time.Time; RadioCount int; Window WindowSize; Blocks []Block }`
  - `func List(dir string) ([]Summary, error)`
  - `func Read(path string) (*Document, error)`
  - `func Write(path string, d *Document) error`
  - `func Delete(path string) error`
  - `func Slug(name string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/profile/store_test.go`:

```go
package profile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeFixture(t *testing.T, dir, base string, d *Document) string {
	t.Helper()
	d.SchemaVersion = SchemaVersion
	b, err := Encode(d)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	p := filepath.Join(dir, base)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestListSkipsNonProfiles(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a"+Ext, &Document{
		Name:   "Alpha",
		Radios: []Radio{{ID: 1, Name: "r", FrequencyKHz: 118500}},
	})
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"+Ext), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Alpha" || got[0].RadioCount != 1 {
		t.Fatalf("List = %+v, want one Alpha with 1 radio", got)
	}
}

func TestListSkipsUnparseableFileWithoutFailing(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "good"+Ext, &Document{Name: "Good"})
	if err := os.WriteFile(filepath.Join(dir, "bad"+Ext), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := List(dir)
	if err != nil {
		t.Fatalf("one corrupt file must not fail the whole listing: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Good" {
		t.Fatalf("List = %+v, want just Good", got)
	}
}

func TestListMissingDirIsEmptyNotError(t *testing.T) {
	got, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("a missing profiles dir must list empty, not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %+v, want empty", got)
	}
}

// Review Focus #3, read half.
func TestListPathIsAFileReportsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "iam.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(f); err == nil {
		t.Fatal("profiles_dir pointing at a regular file must report an error, not silently list nothing")
	}
}

func TestWriteIsAtomicAndCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	p := filepath.Join(dir, "x"+Ext)
	d := &Document{
		SchemaVersion: SchemaVersion,
		Name:          "X",
		Radios:        []Radio{{ID: 1, Name: "r", FrequencyKHz: 118500}},
	}
	if err := Write(p, d); err != nil {
		t.Fatalf("Write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want exactly 1 (no .tmp left behind): %+v", len(entries), entries)
	}
	back, err := Read(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(back.Layout.Blocks) != 1 || back.Layout.Blocks[0].RadioID != 1 {
		t.Fatalf("blocks = %+v, want one reconciled block", back.Layout.Blocks)
	}
}

// Review Focus #3, write half.
func TestWriteToUnwritableDirReportsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 does not deny writes on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := Write(filepath.Join(dir, "x"+Ext), &Document{SchemaVersion: SchemaVersion, Name: "X"}); err == nil {
		t.Fatal("a write into an unwritable dir must report the failure, not appear to succeed")
	}
}

func TestReadRejectsNewerSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x"+Ext)
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"name":"X"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("want ErrSchemaTooNew from Read")
	}
}

func TestDeleteRemovesFileAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "x"+Ext, &Document{Name: "X"})
	if err := Delete(p); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("file still present after Delete")
	}
	if err := Delete(p); err != nil {
		t.Fatalf("deleting an already-absent profile must succeed: %v", err)
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Fleet Op — Stanton":  "fleet-op-stanton",
		"  Salvage/Crew (R) ": "salvage-crew-r",
		"":                    "profile",
		"////":                "profile",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSummaryCarriesModifiedTime(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 5, 19, 21, 0, 0, 0, time.UTC)
	writeFixture(t, dir, "x"+Ext, &Document{Name: "X", ModifiedAt: when})
	got, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].ModifiedAt.Equal(when) {
		t.Fatalf("ModifiedAt = %v, want %v", got[0].ModifiedAt, when)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -run 'List|Write|Read|Delete|Slug|Summary'`
Expected: FAIL — undefined `List`, `Write`, `Read`, `Delete`, `Slug`, `Summary`, `Ext`.

- [ ] **Step 3: Write the implementation**

Create `internal/profile/store.go`:

```go
package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Ext is the profile filename suffix.
const Ext = ".vcs.json"

// Summary is one row of the Profiles directory listing: enough to render
// the table and the layout preview without re-reading every file.
type Summary struct {
	Path        string     `json:"path"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Author      string     `json:"author"`
	ModifiedAt  time.Time  `json:"modified_at"`
	RadioCount  int        `json:"radio_count"`
	Window      WindowSize `json:"window"`
	Blocks      []Block    `json:"blocks"`
}

// List returns every readable profile in dir, sorted by display name.
//
// A MISSING directory lists empty with no error -- the discipline
// windowstate.Load uses for a first run, where nothing has been written
// yet. A path that EXISTS but is not a directory (profiles_dir pointing at
// a regular file) DOES error: that is a misconfiguration the user has to be
// told about, not an empty shelf.
//
// One unparseable file does not fail the listing. A corrupt profile must
// not hide every other profile the user has; it simply does not appear.
func List(dir string) ([]Summary, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list profiles in %s: %w", dir, err)
	}

	out := make([]Summary, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		d, err := Read(p)
		if err != nil {
			continue
		}
		out = append(out, Summary{
			Path:        p,
			Name:        d.Name,
			Description: d.Description,
			Author:      d.Author,
			ModifiedAt:  d.ModifiedAt,
			RadioCount:  len(d.Radios),
			Window:      d.Layout.Window,
			Blocks:      d.Layout.Blocks,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Read loads, validates and reconciles one profile.
func Read(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile %s: %w", filepath.Base(path), err)
	}
	d, err := Decode(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	d.Reconcile()
	return d, nil
}

// Write persists a profile atomically: write-temp then rename, the pattern
// windowstate.Save established. A half-written profile is never visible,
// and a crash mid-write leaves the previous version intact.
//
// It Reconciles first, so what lands on disk is always self-consistent
// regardless of what the caller assembled.
func Write(path string, d *Document) error {
	if err := d.Validate(); err != nil {
		return err
	}
	d.Reconcile()
	b, err := Encode(d)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create profiles dir %s: %w", dir, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write profile %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename profile %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Delete removes a profile file. A file that is already gone counts as
// success: the caller's intent -- "this profile should not exist" -- is
// satisfied either way.
func Delete(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete profile %s: %w", filepath.Base(path), err)
	}
	return nil
}

// Slug turns a display name into a filename stem: lowercase ASCII
// alphanumerics and dashes, collapsed. Non-ASCII runes (the em dash in
// "Fleet Op — Stanton") become separators rather than being transliterated
// -- the display name lives INSIDE the file, so the stem only has to be
// stable and safe on all three platforms.
func Slug(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "profile"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -v`
Expected: PASS. On Windows `TestWriteToUnwritableDirReportsError` reports SKIP — correct, not a failure.

- [ ] **Step 5: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/profile/
git add internal/profile/
git commit -m "feat(profile): directory store with atomic writes

List treats a missing dir as empty (first run) but a non-directory path as
an error the user must be told about, and skips one corrupt file rather
than hiding every other profile behind it."
```

---

### Task 3: `internal/profile` — builtin defaults and hash migration

**Files:**
- Create: `internal/profile/builtin.go`, `internal/profile/builtin/standard-fleet.vcs.json`
- Test: `internal/profile/builtin_test.go`

**Interfaces:**
- Consumes: Task 1's `Document`/`Decode`, Task 2's `Ext`/`Write`/`Read`.
- Produces:
  - `type Builtin struct { ID string; Content []byte }`
  - `func Builtins() []Builtin`
  - `type SeedResult struct { ID, Path, Hash string; Written bool }`
  - `func Seed(dir string, recorded map[string]string) ([]SeedResult, error)`
  - `func HashContent(b []byte) string`

`Seed` returns one `SeedResult` per builtin it examined. `Written` reports whether the file was (re)written; `Hash` is the hash the caller must record for that id — unchanged from `recorded` when nothing was written, and the new content's hash when it was. The caller persists `map[ID]Hash` for every result whose `Hash` is non-empty.

- [ ] **Step 1: Write the fixture**

Create `internal/profile/builtin/standard-fleet.vcs.json`:

```json
{
  "schema_version": 1,
  "name": "Standard Fleet",
  "description": "Three monitored channels: fleet common, wing and intercom. The default starting point for a fresh install.",
  "author": "Vanguard",
  "created_at": "2026-09-30T00:00:00Z",
  "modified_at": "2026-09-30T00:00:00Z",
  "radios": [
    { "id": 1, "name": "Fleet Common", "frequency_khz": 118500, "enabled": true, "is_intercom": false },
    { "id": 2, "name": "Wing", "frequency_khz": 122750, "enabled": true, "is_intercom": false },
    { "id": 3, "name": "Intercom", "frequency_khz": 100000, "enabled": true, "is_intercom": true }
  ],
  "layout": {
    "window": { "w": 540, "h": 720 },
    "blocks": [
      { "radio_id": 1, "w": 516, "h": 180 },
      { "radio_id": 2, "w": 516, "h": 180 },
      { "radio_id": 3, "w": 516, "h": 180 }
    ]
  }
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/profile/builtin_test.go`:

```go
package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func seedOnce(t *testing.T, dir string, rec map[string]string) map[string]string {
	t.Helper()
	res, err := Seed(dir, rec)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	out := map[string]string{}
	for k, v := range rec {
		out[k] = v
	}
	for _, r := range res {
		if r.Hash != "" {
			out[r.ID] = r.Hash
		}
	}
	return out
}

func TestBuiltinsAreValidProfiles(t *testing.T) {
	bs := Builtins()
	if len(bs) == 0 {
		t.Fatal("no builtins embedded")
	}
	for _, b := range bs {
		d, err := Decode(b.Content)
		if err != nil {
			t.Fatalf("builtin %s does not parse: %v", b.ID, err)
		}
		if err := d.Validate(); err != nil {
			t.Fatalf("builtin %s does not validate: %v", b.ID, err)
		}
	}
}

// Row 1: no hash recorded + file missing -> WRITE (first run).
func TestSeedWritesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("builtin not written on first run: %v", err)
	}
	if rec["standard-fleet"] == "" {
		t.Fatal("Seed must return a hash to record for a file it wrote")
	}
}

// Row 2: no hash recorded + file present -> leave (a user file owns the name).
func TestSeedLeavesUnrecordedExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "standard-fleet"+Ext)
	mine := []byte(`{"schema_version":1,"name":"Mine","radios":[]}`)
	if err := os.WriteFile(p, mine, 0o644); err != nil {
		t.Fatal(err)
	}
	seedOnce(t, dir, map[string]string{})
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(mine) {
		t.Fatalf("a pre-existing file with no recorded hash must never be clobbered.\ngot:  %s\nwant: %s", got, mine)
	}
}

// Row 3: hash recorded + file missing -> leave (the user deleted it).
func TestSeedRespectsDeletion(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	seedOnce(t, dir, rec)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a builtin the user deleted must stay deleted: a recorded hash proves it was written once, so its absence is a decision, not a fresh install")
	}
}

// Row 4: hash recorded + hash matches -> REPLACE when the shipped content differs.
func TestSeedUpdatesUntouchedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "standard-fleet"+Ext)
	// Pretend an OLDER build shipped different content: record the hash of
	// what is actually on disk, then seed with today's embedded content.
	old := []byte(`{"schema_version":1,"name":"Standard Fleet","radios":[]}`)
	if err := os.WriteFile(p, old, 0o644); err != nil {
		t.Fatal(err)
	}
	rec := map[string]string{"standard-fleet": HashContent(old)}
	seedOnce(t, dir, rec)

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) == string(old) {
		t.Fatal("an untouched builtin must be replaced when a newer build ships different content")
	}
}

func TestSeedDoesNotRewriteIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	res, err := Seed(dir, rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Written {
			t.Fatalf("second seed rewrote %s although nothing changed", r.ID)
		}
	}
}

// Row 5: hash recorded + hash differs -> leave (the user edited it). This is
// the row that protects user data, so it asserts byte identity.
func TestSeedNeverOverwritesUserEdit(t *testing.T) {
	dir := t.TempDir()
	rec := seedOnce(t, dir, map[string]string{})
	p := filepath.Join(dir, "standard-fleet"+Ext)

	edited := []byte(`{"schema_version":1,"name":"Standard Fleet","description":"my tuning","radios":[]}`)
	if err := os.WriteFile(p, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed twice: a regression that only bites on the second pass is still
	// a regression that destroys the edit.
	rec = seedOnce(t, dir, rec)
	seedOnce(t, dir, rec)

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(edited) {
		t.Fatalf("a user-edited builtin must never be touched again by any future build.\ngot:  %s\nwant: %s", got, edited)
	}
}

func TestSeedMissingDirIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profiles")
	seedOnce(t, dir, map[string]string{})
	if _, err := os.Stat(filepath.Join(dir, "standard-fleet"+Ext)); err != nil {
		t.Fatalf("Seed must create the profiles dir: %v", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -run 'Builtin|Seed'`
Expected: FAIL — undefined `Builtins`, `Seed`, `HashContent`, `SeedResult`.

- [ ] **Step 4: Write the implementation**

Create `internal/profile/builtin.go`:

```go
package profile

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// builtinFS holds the profiles shipped with the app. Same mechanism as
// internal/audio/sfx.go's asset embed.
//
//go:embed builtin
var builtinFS embed.FS

// Builtin is one shipped default profile. ID is the filename stem and is
// the key under which its hash is recorded in config.
type Builtin struct {
	ID      string
	Content []byte
}

// SeedResult reports what Seed decided for one builtin.
//
// Hash is what the caller must RECORD for this id: the new content's hash
// when Written, the caller's existing record when not, and "" when there is
// nothing to record (rows 2 and 3 -- a file that is not ours, and a file
// the user deleted, whose record must be preserved as-is by the caller
// rather than overwritten or cleared).
type SeedResult struct {
	ID      string
	Path    string
	Hash    string
	Written bool
}

// HashContent is the content fingerprint the seeding decision turns on.
func HashContent(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Builtins returns every embedded default profile, ordered by id.
func Builtins() []Builtin {
	entries, err := builtinFS.ReadDir("builtin")
	if err != nil {
		return nil
	}
	out := make([]Builtin, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), Ext) {
			continue
		}
		b, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		out = append(out, Builtin{
			ID:      strings.TrimSuffix(e.Name(), Ext),
			Content: b,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Seed writes the shipped default profiles into dir, using recorded (id ->
// hash of the content THIS CLIENT last wrote) to decide what may be
// touched:
//
//	recorded | on disk            | action
//	---------+--------------------+----------------------------------------
//	none     | missing            | WRITE   -- first run
//	none     | present            | leave   -- a user file owns that name
//	present  | missing            | leave   -- the user deleted it
//	present  | hash == record     | REPLACE if the shipped content differs
//	present  | hash != record     | leave   -- the user edited it
//
// Two properties are the whole point of the scheme, and both are load
// bearing enough to have their own tests:
//
//   - A user edit is PERMANENT. Once the on-disk hash diverges from the
//     record, that file is never written again by any future build. There
//     is no version in which a shipped update silently reverts someone's
//     tuning.
//   - A deletion is PERMANENT. A recorded hash proves the file was written
//     at least once, so its absence is a decision, not a fresh install.
//
// Seeding is best-effort per builtin: one failure is returned but does not
// stop the others, because a single unwritable file must not cost the user
// every other default.
func Seed(dir string, recorded map[string]string) ([]SeedResult, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create profiles dir %s: %w", dir, err)
	}

	var firstErr error
	bs := Builtins()
	out := make([]SeedResult, 0, len(bs))
	for _, b := range bs {
		res := SeedResult{ID: b.ID, Path: filepath.Join(dir, b.ID+Ext)}
		rec, haveRec := recorded[b.ID]

		onDisk, readErr := os.ReadFile(res.Path)
		missing := errors.Is(readErr, os.ErrNotExist)
		if readErr != nil && !missing {
			if firstErr == nil {
				firstErr = fmt.Errorf("read builtin %s: %w", b.ID, readErr)
			}
			out = append(out, res)
			continue
		}

		switch {
		case !haveRec && missing:
			// First run.
		case !haveRec:
			// A file we have never written owns this name. Not ours.
			out = append(out, res)
			continue
		case missing:
			// Deleted deliberately. Keep the record so it stays deleted.
			res.Hash = rec
			out = append(out, res)
			continue
		case HashContent(onDisk) != rec:
			// Edited by the user. Keep the record so we keep recognising it
			// as theirs rather than treating it as a fresh name next start.
			res.Hash = rec
			out = append(out, res)
			continue
		case HashContent(onDisk) == HashContent(b.Content):
			// Untouched and already current.
			res.Hash = rec
			out = append(out, res)
			continue
		}

		if err := os.WriteFile(res.Path, b.Content, 0o644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("write builtin %s: %w", b.ID, err)
			}
			out = append(out, res)
			continue
		}
		res.Written = true
		res.Hash = HashContent(b.Content)
		out = append(out, res)
	}
	return out, firstErr
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/profile/ -v`
Expected: PASS, all cases including the five seeding rows.

- [ ] **Step 6: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/profile/
git add internal/profile/
git commit -m "feat(profile): embedded default profiles with hash-gated migration

A recorded content hash decides what Seed may touch. A user edit is
permanent (the hash diverges and the file is never written again) and so is
a deletion (a recorded hash proves the file existed once, so its absence is
a decision, not a fresh install). Best-effort per builtin: one unwritable
file must not cost the user every other default."
```

---

### Task 4: `internal/history` — entry and ring

**Files:**
- Create: `internal/history/history.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Entry struct { At time.Time; Sender, GUID string; FreqKHz uint32; Radio string; DurationMS int; Own bool }`
  - `const DefaultCap = 2000`
  - `type Log struct{ ... }`
  - `func New(cap int) *Log`
  - `func (l *Log) Append(e Entry)`
  - `func (l *Log) Snapshot() []Entry`
  - `func (l *Log) Clear()`
  - `func (l *Log) Len() int`

- [ ] **Step 1: Write the failing tests**

Create `internal/history/history_test.go`:

```go
package history

import (
	"sync"
	"testing"
	"time"
)

func mk(i int) Entry {
	return Entry{
		At:         time.Date(2026, 9, 30, 21, 0, i, 0, time.UTC),
		Sender:     "Dabble",
		GUID:       "g",
		FreqKHz:    118500,
		Radio:      "Fleet Common",
		DurationMS: 1000 + i,
	}
}

func TestSnapshotIsNewestFirst(t *testing.T) {
	l := New(10)
	for i := 0; i < 3; i++ {
		l.Append(mk(i))
	}
	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].DurationMS != 1002 {
		t.Fatalf("Snapshot must be newest-first; got[0] = %+v", got[0])
	}
}

func TestRingEvictsOldest(t *testing.T) {
	l := New(3)
	for i := 0; i < 5; i++ {
		l.Append(mk(i))
	}
	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("len = %d, want cap 3", len(got))
	}
	if got[0].DurationMS != 1004 || got[2].DurationMS != 1002 {
		t.Fatalf("wrong window retained: %+v", got)
	}
}

func TestZeroCapUsesDefault(t *testing.T) {
	if got := New(0).Cap(); got != DefaultCap {
		t.Fatalf("Cap = %d, want DefaultCap %d", got, DefaultCap)
	}
}

func TestClearEmpties(t *testing.T) {
	l := New(10)
	l.Append(mk(0))
	l.Clear()
	if l.Len() != 0 {
		t.Fatalf("Len = %d after Clear, want 0", l.Len())
	}
	if got := l.Snapshot(); got == nil {
		t.Fatal("Snapshot must return a non-nil empty slice: the frontend types it as an array and nil marshals to null")
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	l := New(10)
	l.Append(mk(0))
	s := l.Snapshot()
	s[0].Sender = "mutated"
	if l.Snapshot()[0].Sender != "Dabble" {
		t.Fatal("Snapshot must hand out a copy, not the backing array")
	}
}

func TestConcurrentAppendAndSnapshot(t *testing.T) {
	// The RX delivery goroutine and the TX press path both Append, and a
	// binding reads Snapshot from a third. Run under -race.
	l := New(64)
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				l.Append(mk(i))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			_ = l.Snapshot()
		}
	}()
	wg.Wait()
	if l.Len() != 64 {
		t.Fatalf("Len = %d, want 64", l.Len())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/history/`
Expected: FAIL — no Go files / undefined `New`, `Entry`, `Log`.

- [ ] **Step 3: Write the implementation**

Create `internal/history/history.go`:

```go
// Package history is the local transmission log: a bounded ring of
// completed transmissions, plus its JSON persistence.
//
// It imports nothing outside the standard library. Entries arrive FULLY
// RESOLVED -- callsign already looked up, channel name already matched --
// so this package never needs internal/state, internal/config or
// internal/voice, and can be tested without any of them.
package history

import (
	"sync"
	"time"
)

// DefaultCap bounds the ring: roughly a long evening's operations. Old
// entries are evicted rather than the log growing without limit, because
// the file is rewritten in full on every flush.
const DefaultCap = 2000

// Entry is one completed transmission.
//
// Radio is the LOCAL channel name the frequency matched, and is empty for a
// server global channel -- those are accepted without a tuned radio, so
// there is no local name to show and inventing one would be a lie. The UI
// renders the frequency alone in that case.
//
// Sender may be empty when the talker's GUID is not in the client store
// (they had already left by the time the transmission ended). The row still
// records: the frequency and duration are the parts worth keeping.
type Entry struct {
	At         time.Time `json:"at"`
	Sender     string    `json:"sender"`
	GUID       string    `json:"guid"`
	FreqKHz    uint32    `json:"freq_khz"`
	Radio      string    `json:"radio"`
	DurationMS int       `json:"dur_ms"`
	Own        bool      `json:"own"`
}

// Log is the ring. Safe for concurrent use: the RX delivery goroutine and
// the TX press path both Append, and bindings read Snapshot from a third.
type Log struct {
	mu      sync.RWMutex
	cap     int
	entries []Entry // oldest first

	// dirty is set by every mutation and cleared by the persistence layer
	// after a successful flush. See store.go.
	dirty bool
}

// New builds a Log. A non-positive cap means DefaultCap.
func New(capacity int) *Log {
	if capacity <= 0 {
		capacity = DefaultCap
	}
	return &Log{cap: capacity, entries: make([]Entry, 0, capacity)}
}

// Cap reports the ring capacity.
func (l *Log) Cap() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cap
}

// Append records one transmission, evicting the oldest if the ring is full.
func (l *Log) Append(e Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) >= l.cap {
		copy(l.entries, l.entries[len(l.entries)-l.cap+1:])
		l.entries = l.entries[:l.cap-1]
	}
	l.entries = append(l.entries, e)
	l.dirty = true
}

// Snapshot returns a copy, NEWEST FIRST -- the order the table renders and
// the order a reader wants. A copy, not the backing array, so a caller
// cannot mutate the log by editing what it was handed.
//
// Always non-nil: the frontend types the list as an array and a nil slice
// marshals to null.
func (l *Log) Snapshot() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]Entry, len(l.entries))
	for i, e := range l.entries {
		out[len(l.entries)-1-i] = e
	}
	return out
}

// Len reports how many entries are held.
func (l *Log) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.entries)
}

// Clear empties the log.
func (l *Log) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
	l.dirty = true
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/history/ -v`
Expected: PASS, clean under `-race`.

- [ ] **Step 5: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/history/
git add internal/history/
git commit -m "feat(history): bounded transmission-log ring

Entries arrive fully resolved, so this package needs neither internal/state
nor internal/voice and tests without either. Snapshot is newest-first and a
copy; the ring evicts rather than growing, because the file is rewritten in
full on every flush."
```

---

### Task 5: `internal/history` — persistence, debounce, CSV

**Files:**
- Create: `internal/history/store.go`
- Test: `internal/history/store_test.go`

**Interfaces:**
- Consumes: Task 4's `Log`, `Entry`, `New`.
- Produces:
  - `const SchemaVersion = 1`
  - `func Load(path string, capacity int) (*Log, error)`
  - `func (l *Log) SetPersist(path string, debounce time.Duration, now func() time.Time, onErr func(error))`
  - `func (l *Log) Flush() error`
  - `func (l *Log) Tick()` — the debounce driver; the app calls it from a ticker, tests call it directly with an injected clock.
  - `func CSV(entries []Entry) []byte`

Design note for the implementer: the debounce is deliberately **pull-based** (`Tick`) rather than an internal `time.Timer`. `internal/notify`'s coalescing timers needed a `StopTimers` method and a whole test apparatus to avoid sleeping; a `Tick` driven by the caller's ticker makes the flush schedule testable with an injected clock and nothing to stop at shutdown.

- [ ] **Step 1: Write the failing tests**

Create `internal/history/store_test.go`:

```go
package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "nope.json"), 10)
	if err != nil {
		t.Fatalf("a missing history file must load empty, not error: %v", err)
	}
	if l.Len() != 0 {
		t.Fatalf("Len = %d, want 0", l.Len())
	}
}

func TestLoadCorruptFileIsEmptyWithError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p, 10)
	if err == nil {
		t.Fatal("a corrupt history file must report the error so the user is told")
	}
	if l == nil || l.Len() != 0 {
		t.Fatal("Load must still return a usable empty log so the session can record new entries")
	}
}

func TestFlushRoundTrips(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	l.Append(mk(2))
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	back, err := Load(p, 10)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := back.Snapshot()
	if len(got) != 2 || got[0].DurationMS != 1002 {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestFlushIsAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir has %d entries, want 1 (no .tmp left behind): %+v", len(entries), entries)
	}
}

func TestFlushSkipsWhenNotDirty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, nil)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	first := st.ModTime()

	time.Sleep(10 * time.Millisecond)
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	st2, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(first) {
		t.Fatal("a Flush with nothing new must not rewrite the file")
	}
}

func TestTickDebounces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	c := &fakeClock{t: time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)}
	l := New(10)
	l.SetPersist(p, 5*time.Second, c.now, nil)

	l.Append(mk(1))
	c.add(2 * time.Second)
	l.Tick()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("Tick before the debounce window elapsed must not write")
	}

	c.add(4 * time.Second) // 6s since the append
	l.Tick()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Tick after the window must write: %v", err)
	}
}

func TestTickCoalescesABurst(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	c := &fakeClock{t: time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)}
	l := New(100)
	l.SetPersist(p, 5*time.Second, c.now, nil)

	// Ten transmissions inside one window produce exactly one write.
	for i := 0; i < 10; i++ {
		l.Append(mk(i))
		c.add(200 * time.Millisecond)
		l.Tick()
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("a burst inside one debounce window must not write yet")
	}
	c.add(5 * time.Second)
	l.Tick()
	back, err := Load(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	if back.Len() != 10 {
		t.Fatalf("Len = %d, want all 10 coalesced into one write", back.Len())
	}
}

func TestFlushErrorReachesCallbackOncePerFailureRun(t *testing.T) {
	// The error hook is how internal/app raises ONE notification for a
	// failing flush instead of one per 5s tick. The package reports every
	// failure; collapsing repeats is the app's job via notify.Raise's
	// fingerprint, but the package must at least not swallow it.
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "h.json") // parent does not exist
	var got []error
	l := New(10)
	l.SetPersist(p, time.Second, time.Now, func(err error) { got = append(got, err) })
	l.Append(mk(1))
	if err := l.Flush(); err == nil {
		t.Fatal("want an error flushing into a missing parent dir")
	}
	if len(got) != 1 {
		t.Fatalf("onErr called %d times, want 1", len(got))
	}
}

func TestPersistUnsetFlushIsNoop(t *testing.T) {
	l := New(10)
	l.Append(mk(1))
	if err := l.Flush(); err != nil {
		t.Fatalf("Flush with no path configured must be a silent no-op: %v", err)
	}
}

func TestLoadRejectsNewerSchema(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	if err := os.WriteFile(p, []byte(`{"schema_version":99,"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(p, 10)
	if err == nil {
		t.Fatal("want an error for a newer schema")
	}
	if l == nil {
		t.Fatal("Load must still return a usable log")
	}
}

func TestCSVHeaderAndEscaping(t *testing.T) {
	out := string(CSV([]Entry{{
		At:         time.Date(2026, 9, 30, 21, 15, 51, 0, time.UTC),
		Sender:     `Dab"ble, Jr`,
		FreqKHz:    118500,
		Radio:      "Fleet Common",
		DurationMS: 3200,
		Own:        true,
	}}))
	if !strings.HasPrefix(out, "time,sender,channel,frequency_mhz,duration_s,own\n") {
		t.Fatalf("header wrong:\n%s", out)
	}
	if !strings.Contains(out, `"Dab""ble, Jr"`) {
		t.Fatalf("a sender containing a quote and a comma must be CSV-escaped:\n%s", out)
	}
	if !strings.Contains(out, "118.500") {
		t.Fatalf("frequency must render as MHz to 3dp:\n%s", out)
	}
	if !strings.Contains(out, "3.2") {
		t.Fatalf("duration must render in seconds:\n%s", out)
	}
}

func TestCSVEmptyStillHasHeader(t *testing.T) {
	if got := string(CSV(nil)); !strings.HasPrefix(got, "time,sender,") {
		t.Fatalf("an empty export must still be a valid CSV with a header, got %q", got)
	}
}

```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/history/ -run 'Load|Flush|Tick|CSV|Persist'`
Expected: FAIL — undefined `Load`, `SetPersist`, `Flush`, `Tick`, `CSV`.

- [ ] **Step 3: Write the implementation**

Create `internal/history/store.go`:

```go
package history

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// SchemaVersion is the history file's format version.
const SchemaVersion = 1

// file is the on-disk shape.
type file struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

// Load reads a history file into a new Log.
//
// A MISSING file is an empty log with no error -- the discipline
// windowstate.Load uses, and the normal first-run case. A CORRUPT or
// too-new file returns an error AND a usable empty log: the session must
// still be able to record new transmissions, and refusing to start logging
// because yesterday's file is damaged would compound the problem.
func Load(path string, capacity int) (*Log, error) {
	l := New(capacity)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("read history: %w", err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return l, fmt.Errorf("parse history: %w", err)
	}
	if f.SchemaVersion > SchemaVersion {
		return l, fmt.Errorf("history: schema version %d is newer than this build reads (%d)",
			f.SchemaVersion, SchemaVersion)
	}
	for _, e := range f.Entries {
		l.Append(e)
	}
	l.mu.Lock()
	l.dirty = false // a freshly loaded log has nothing new to write
	l.mu.Unlock()
	return l, nil
}

// SetPersist configures where and how often this log writes itself.
//
// debounce is the minimum quiet period after the most recent Append before
// Tick will write. now is the clock (injected so the tests need no
// sleeping). onErr, if non-nil, receives every flush failure -- internal/app
// turns that into ONE raised notification rather than one per tick, using
// notify's fingerprint; this package's job is only to not swallow it.
//
// A Log with no path configured flushes as a silent no-op, so a test or a
// build with no app-data dir keeps working.
func (l *Log) SetPersist(path string, debounce time.Duration, now func() time.Time, onErr func(error)) {
	if now == nil {
		now = time.Now
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.path = path
	l.debounce = debounce
	l.now = now
	l.onErr = onErr
}

// Tick writes the log if it is dirty and the debounce window has elapsed
// since the last Append.
//
// Deliberately PULL-based rather than an internal timer: internal/notify's
// coalescing timers needed a StopTimers method and their own test
// apparatus to avoid sleeping, whereas a Tick driven by the caller's
// existing ticker is testable with an injected clock and has nothing to
// stop at shutdown.
func (l *Log) Tick() {
	l.mu.Lock()
	if !l.dirty || l.path == "" || l.now == nil {
		l.mu.Unlock()
		return
	}
	if l.now().Sub(l.lastChange) < l.debounce {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()
	_ = l.Flush()
}

// Flush writes the log now, regardless of the debounce window. Called on
// shutdown, and by Tick once the window has elapsed.
//
// A no-op (nil error) when nothing has changed since the last successful
// write, so a quiet session does not rewrite the file on every tick.
func (l *Log) Flush() error {
	l.mu.Lock()
	if l.path == "" || !l.dirty {
		l.mu.Unlock()
		return nil
	}
	path := l.path
	onErr := l.onErr
	snap := make([]Entry, len(l.entries))
	copy(snap, l.entries)
	l.mu.Unlock()

	b, err := json.MarshalIndent(file{SchemaVersion: SchemaVersion, Entries: snap}, "", "  ")
	if err != nil {
		err = fmt.Errorf("marshal history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		err = fmt.Errorf("write history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		err = fmt.Errorf("rename history: %w", err)
		if onErr != nil {
			onErr(err)
		}
		return err
	}

	l.mu.Lock()
	l.dirty = false
	l.mu.Unlock()
	return nil
}

// CSV renders entries for the Transmission Log's EXPORT CSV button.
//
// Frequencies render as MHz to three decimals and durations as seconds,
// matching what the table shows -- an export nobody can line up against
// the screen is not much of an export. The kHz integer stays the stored
// form; this is a presentation layer.
func CSV(entries []Entry) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"time", "sender", "channel", "frequency_mhz", "duration_s", "own"})
	for _, e := range entries {
		_ = w.Write([]string{
			e.At.Format(time.RFC3339),
			e.Sender,
			e.Radio,
			strconv.FormatFloat(float64(e.FreqKHz)/1000.0, 'f', 3, 64),
			strconv.FormatFloat(float64(e.DurationMS)/1000.0, 'f', 1, 64),
			strconv.FormatBool(e.Own),
		})
	}
	w.Flush()
	return buf.Bytes()
}
```

- [ ] **Step 4: Add the persistence fields to `Log`**

In `internal/history/history.go`, extend the struct — the persistence
fields live beside the ring because they are guarded by the same mutex:

```go
type Log struct {
	mu      sync.RWMutex
	cap     int
	entries []Entry // oldest first

	// dirty is set by every mutation and cleared after a successful flush.
	dirty bool
	// lastChange is when the most recent mutation happened, read by Tick to
	// decide whether the debounce window has elapsed.
	lastChange time.Time

	path     string
	debounce time.Duration
	now      func() time.Time
	onErr    func(error)
}
```

and set `lastChange` in both mutators, guarding for an unset clock:

```go
// in Append, after l.dirty = true:
	if l.now != nil {
		l.lastChange = l.now()
	}

// in Clear, after l.dirty = true:
	if l.now != nil {
		l.lastChange = l.now()
	}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/history/ -v`
Expected: PASS, clean under `-race`.

- [ ] **Step 6: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/history/
git add internal/history/
git commit -m "feat(history): debounced atomic persistence and CSV export

Load treats a missing file as empty but reports a corrupt or too-new one
while still returning a usable log -- refusing to record today because
yesterday's file is damaged compounds the problem. The debounce is
pull-based via Tick so it tests with an injected clock and has nothing to
stop at shutdown."
```

---

### Task 6: `internal/config` — new persisted fields

**Files:**
- Modify: `internal/config/config.go`, `internal/config/paths.go`
- Test: `internal/config/config_test.go` (extend), `internal/config/paths_test.go` (extend or create)

**Interfaces:**
- Consumes: nothing (this package imports no other internal package, deliberately).
- Produces:
  - `Config.ActiveProfile string` (toml `active_profile`)
  - `Config.ProfilesDir string` (toml `profiles_dir`)
  - `Config.BuiltinProfiles map[string]string` (toml `builtin_profiles`)
  - `Config.CommsLayout CommsLayout` (toml `comms_layout`)
  - `type CommsLayout struct { WindowW, WindowH int; Blocks []LayoutBlock }`
  - `type LayoutBlock struct { RadioID uint32; W, H int }`
  - `Config.Voice.HistoryIdleMS int` (toml `history_idle_ms`)
  - `func ProfilesDirPath(configured string) (string, error)`
  - `func HistoryFilePath() (string, error)`

- [ ] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:

```go
func TestProfileFieldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.toml")

	cfg, err := LoadOrCreate(p)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	cfg.ActiveProfile = filepath.Join(dir, "fleet-op.vcs.json")
	cfg.ProfilesDir = filepath.Join(dir, "profiles")
	cfg.BuiltinProfiles = map[string]string{"standard-fleet": "deadbeef"}
	cfg.CommsLayout = CommsLayout{
		WindowW: 540,
		WindowH: 720,
		Blocks:  []LayoutBlock{{RadioID: 1, W: 516, H: 180}, {RadioID: 2, W: 253, H: 120}},
	}
	cfg.Voice.HistoryIdleMS = 400
	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	back, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if back.ActiveProfile != cfg.ActiveProfile {
		t.Errorf("ActiveProfile = %q, want %q", back.ActiveProfile, cfg.ActiveProfile)
	}
	if back.ProfilesDir != cfg.ProfilesDir {
		t.Errorf("ProfilesDir = %q, want %q", back.ProfilesDir, cfg.ProfilesDir)
	}
	if back.BuiltinProfiles["standard-fleet"] != "deadbeef" {
		t.Errorf("BuiltinProfiles = %+v", back.BuiltinProfiles)
	}
	if back.CommsLayout.WindowW != 540 || len(back.CommsLayout.Blocks) != 2 {
		t.Errorf("CommsLayout = %+v", back.CommsLayout)
	}
	if back.CommsLayout.Blocks[1].RadioID != 2 || back.CommsLayout.Blocks[1].W != 253 {
		t.Errorf("block order or contents lost: %+v", back.CommsLayout.Blocks)
	}
	if back.Voice.HistoryIdleMS != 400 {
		t.Errorf("Voice.HistoryIdleMS = %d, want 400", back.Voice.HistoryIdleMS)
	}
}

func TestProfileFieldsDefaultToZero(t *testing.T) {
	// A config.toml written by an older build has none of these keys. They
	// must load as their documented zero values, not fail the parse.
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte("server_url = \"localhost:5002\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("an older config must still load: %v", err)
	}
	if cfg.ActiveProfile != "" || cfg.ProfilesDir != "" {
		t.Errorf("want empty defaults, got %q / %q", cfg.ActiveProfile, cfg.ProfilesDir)
	}
	if cfg.Voice.HistoryIdleMS != 0 {
		t.Errorf("HistoryIdleMS = %d, want 0 meaning 'the documented default'", cfg.Voice.HistoryIdleMS)
	}
}
```

Append to `internal/config/paths_test.go` (create the file with `package config` and the `os`/`path/filepath`/`testing` imports if it does not exist):

```go
func TestProfilesDirPathHonoursOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "ops")
	got, err := ProfilesDirPath(want)
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if got != want {
		t.Fatalf("ProfilesDirPath(%q) = %q, want the override verbatim", want, got)
	}
}

func TestProfilesDirPathDefaultsUnderAppData(t *testing.T) {
	got, err := ProfilesDirPath("")
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if filepath.Base(got) != "profiles" {
		t.Fatalf("default = %q, want a 'profiles' dir under AppDataDir", got)
	}
}

func TestProfilesDirPathTrimsWhitespaceOverride(t *testing.T) {
	// An override of "   " is a user who cleared the field, not a request
	// for a directory literally named three spaces.
	got, err := ProfilesDirPath("   ")
	if err != nil {
		t.Fatalf("ProfilesDirPath: %v", err)
	}
	if filepath.Base(got) != "profiles" {
		t.Fatalf("blank override = %q, want the AppData default", got)
	}
}

func TestHistoryFilePath(t *testing.T) {
	got, err := HistoryFilePath()
	if err != nil {
		t.Fatalf("HistoryFilePath: %v", err)
	}
	if filepath.Base(got) != "history.json" {
		t.Fatalf("HistoryFilePath = %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/ -run 'Profile|History'`
Expected: FAIL — undefined `CommsLayout`, `LayoutBlock`, `ProfilesDirPath`, `HistoryFilePath`, and unknown fields on `Config`/`Voice`.

- [ ] **Step 3: Add the config fields**

In `internal/config/config.go`, add to `type Config struct` — **after** `SelectedRadioID` and **before** any existing table-valued field is fine, but TOML requires scalars before tables in the encoded output, which the encoder handles; place the two scalars next to `SelectedRadioID` and the two tables at the end of the struct:

```go
	// ActiveProfile is the absolute path of the radio profile currently
	// loaded, or "" for none. It is a POINTER, not a copy: the live radio
	// and layout state lives in this file (Radios, SelectedRadioID,
	// CommsLayout), and a profile is a snapshot of those. Dirty state is
	// COMPUTED by comparing the two, never stored, so there is nothing to
	// keep in sync and nothing that can go stale across a crash or an
	// external edit.
	ActiveProfile string `toml:"active_profile"`

	// ProfilesDir overrides where profiles are read and written. Empty
	// means AppDataDir()/profiles -- see ProfilesDirPath.
	ProfilesDir string `toml:"profiles_dir"`

	// BuiltinProfiles maps a shipped profile's id to the sha256 of the
	// content THIS CLIENT last wrote for it. It is what lets startup
	// seeding tell "untouched, safe to update" from "the user edited it,
	// never overwrite" -- and, because a recorded hash proves the file was
	// written at least once, "the user deleted it" from "first run".
	// See internal/profile.Seed.
	BuiltinProfiles map[string]string `toml:"builtin_profiles"`

	// CommsLayout is the Comms window's arrangement. Written to disk at
	// shutdown rather than during a drag -- see internal/app's layout
	// binding for why, and note that because Save serialises the WHOLE
	// Config, this free-rides on every other config write too.
	CommsLayout CommsLayout `toml:"comms_layout"`
```

and the two new types, beside `Radio`:

```go
// CommsLayout is the persisted Comms window arrangement: the window's own
// size plus one sized block per radio, in flow order.
//
// It is a raw-values layer, the same discipline as Audio/Voice/Keybinds:
// this package does not import internal/profile, so these are plain fields
// with TOML tags and no knowledge of profile semantics. internal/app owns
// the mapping.
type CommsLayout struct {
	WindowW int           `toml:"window_w"`
	WindowH int           `toml:"window_h"`
	Blocks  []LayoutBlock `toml:"blocks"`
}

// LayoutBlock is one radio's tile. Order within CommsLayout.Blocks IS the
// flow order; there is deliberately no index field to keep consistent with
// the slice.
type LayoutBlock struct {
	RadioID uint32 `toml:"radio_id"`
	W       int    `toml:"w"`
	H       int    `toml:"h"`
}
```

and to `type Voice struct`:

```go
	// HistoryIdleMS is how long a received stream may go quiet before the
	// transmission is considered over FOR THE HISTORY LOG. 0 means the
	// package default (500ms). It does not affect when a decoder and
	// jitter buffer are released -- that is internal/voice's own
	// rxIdleTimeout, 5s, and re-tuning it to suit a log would change voice
	// behaviour to serve a view.
	//
	// It is a config key and not just a constant because the 500ms default
	// is a GUESS: nobody has ever received voice from a real peer on this
	// client, so the real inter-packet gap distribution is unknown and a
	// field test has to be able to sweep values without a rebuild.
	HistoryIdleMS int `toml:"history_idle_ms"`
```

- [ ] **Step 4: Add the path helpers**

In `internal/config/paths.go`:

```go
// ProfilesDirPath resolves where radio profiles live. A non-blank
// `configured` (config.toml's profiles_dir) is returned verbatim; anything
// else falls back to a "profiles" directory under AppDataDir, which is
// created if absent.
//
// A whitespace-only override is treated as empty: that is a user who
// cleared the field, not a request for a directory named three spaces.
func ProfilesDirPath(configured string) (string, error) {
	if c := strings.TrimSpace(configured); c != "" {
		return c, nil
	}
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, "profiles")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return "", err
	}
	return out, nil
}

// HistoryFilePath returns the path to history.json under AppDataDir.
func HistoryFilePath() (string, error) {
	dir, err := AppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}
```

Add `"strings"` to that file's imports.

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/config/ -v`
Expected: PASS, including the existing suite.

- [ ] **Step 6: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/config/
git add internal/config/
git commit -m "feat(config): profile pointer, profiles dir, builtin hashes, comms layout

Live radio and layout state stays here; a profile is a snapshot of it and
active_profile is only a pointer, so dirty state can be computed rather
than stored. [voice] history_idle_ms exposes the RX idle threshold because
its 500ms default is a guess nobody has measured."
```

---

### Task 7: `internal/voice` — RX transmission-end detection and delivery

**Files:**
- Modify: `internal/voice/session.go` (Options, Session fields, `Dial`, `Close`), `internal/voice/rx.go` (`rxStream`, `rxService`, `rxStreamFor`)
- Test: `internal/voice/rxhistory_test.go` (create)

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `type RXEvent struct { Sender uuid.UUID; Freq KHz; Start, End time.Time }`
  - `Options.OnRX func(RXEvent)`
  - `Options.HistoryIdleMS int` — 0 means `defaultHistoryIdleMS` (500)
  - `const defaultHistoryIdleMS = 500`

**Sandbox:** this package's tests bind UDP sockets. Every `go test` invocation for it needs `dangerouslyDisableSandbox`.

- [ ] **Step 1: Write the failing tests**

Create `internal/voice/rxhistory_test.go`. Study `internal/voice/rx_test.go` first for the existing fake-clock and fake-decoder helpers and reuse them rather than inventing parallel ones; the sketch below names what the assertions must be:

```go
package voice

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rxHistoryHarness drives rxService against a fake clock with one open
// stream, collecting OnRX deliveries.
type rxHistoryHarness struct {
	s      *Session
	mu     sync.Mutex
	events []RXEvent
}

func (h *rxHistoryHarness) got() []RXEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]RXEvent, len(h.events))
	copy(out, h.events)
	return out
}

// newRXHistoryHarness builds a Session with the RX path initialised but no
// socket: rxService and rxStreamFor are pure with respect to the network,
// so the history branch can be driven without binding anything.
//
// IMPLEMENTER: mirror however rx_test.go currently constructs a bare
// Session for its own rxService tests. If it has no such helper, build one
// here with &Session{log: slog.New(slog.NewTextHandler(io.Discard, nil))},
// call s.rx.init(newFakeDecoder, 60, 500), and set s.clock to the fake.
func newRXHistoryHarness(t *testing.T, now func() time.Time, idle time.Duration) *rxHistoryHarness {
	t.Helper()
	h := &rxHistoryHarness{}
	// ...construct s with clock=now, historyIdle=idle, onRX appending to h...
	return h
}

func TestRXHistoryEmitsAfterIdleThreshold(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)

	sender := uuid.New()
	// Two packets 20ms apart, then silence.
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)
	cur = cur.Add(20 * time.Millisecond)
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 2, Payload: []byte("aaaaaaaa")}, cur)

	cur = cur.Add(300 * time.Millisecond)
	h.s.rxService(cur)
	if len(h.got()) != 0 {
		t.Fatalf("emitted before the 500ms threshold: %+v", h.got())
	}

	cur = cur.Add(300 * time.Millisecond) // 600ms since the last packet
	h.s.rxService(cur)
	ev := h.got()
	if len(ev) != 1 {
		t.Fatalf("events = %+v, want exactly 1", ev)
	}
	if ev[0].Sender != sender || ev[0].Freq != 118500 {
		t.Fatalf("event = %+v", ev[0])
	}
	if !ev[0].Start.Equal(base) {
		t.Fatalf("Start = %v, want the FIRST packet's time %v", ev[0].Start, base)
	}
	if !ev[0].End.Equal(base.Add(20 * time.Millisecond)) {
		t.Fatalf("End = %v, want the LAST packet's time, not the detection time", ev[0].End)
	}
}

func TestRXHistoryEmitsOncePerTransmission(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	// Sweep repeatedly past the threshold but before the 5s reap.
	for i := 0; i < 5; i++ {
		cur = cur.Add(600 * time.Millisecond)
		h.s.rxService(cur)
	}
	if got := h.got(); len(got) != 1 {
		t.Fatalf("events = %d, want exactly 1 -- the ended flag must suppress re-emission until a new packet arrives", len(got))
	}
}

func TestRXHistoryRestartsAfterEnd(t *testing.T) {
	// Two sentences 600ms apart, inside the 5s reap window, are TWO rows.
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()

	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)
	cur = cur.Add(600 * time.Millisecond)
	h.s.rxService(cur) // emits #1

	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 2, Payload: []byte("aaaaaaaa")}, cur)
	secondStart := cur
	cur = cur.Add(600 * time.Millisecond)
	h.s.rxService(cur) // emits #2

	got := h.got()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 -- a packet on an ended stream starts a NEW transmission", len(got))
	}
	if !got[1].Start.Equal(secondStart) {
		t.Fatalf("second Start = %v, want %v", got[1].Start, secondStart)
	}
}

func TestRXHistoryDoesNotChangeReapTiming(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: uuid.New(), Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	cur = cur.Add(4 * time.Second)
	h.s.rxService(cur)
	if h.s.RXStats().Active != 1 {
		t.Fatal("history detection must not reap early: rxIdleTimeout is still 5s")
	}
	cur = cur.Add(2 * time.Second)
	h.s.rxService(cur)
	if h.s.RXStats().Active != 0 {
		t.Fatal("stream should be reaped at 5s idle, unchanged")
	}
}

func TestRXHistoryEndsOpenStreamsOnClose(t *testing.T) {
	// A transmission in flight during a reconnect must produce one truthful
	// row rather than vanishing.
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	h.s.endOpenRXStreams()

	got := h.got()
	if len(got) != 1 {
		t.Fatalf("events = %+v, want the in-flight stream ended", got)
	}
	if !got[0].End.Equal(base) {
		t.Fatalf("End = %v, want lastSeen %v", got[0].End, base)
	}
}

func TestHistoryIdleDefault(t *testing.T) {
	if got := historyIdleFor(0); got != defaultHistoryIdleMS*time.Millisecond {
		t.Fatalf("historyIdleFor(0) = %v, want the %dms default", got, defaultHistoryIdleMS)
	}
	if got := historyIdleFor(250); got != 250*time.Millisecond {
		t.Fatalf("historyIdleFor(250) = %v", got)
	}
	if got := historyIdleFor(-5); got != defaultHistoryIdleMS*time.Millisecond {
		t.Fatalf("a negative override must fall back to the default, got %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/voice/ -run RXHistory` **with `dangerouslyDisableSandbox`**
Expected: FAIL — undefined `RXEvent`, `historyIdleFor`, `defaultHistoryIdleMS`, `endOpenRXStreams`.

- [ ] **Step 3: Add the Options fields and the RXEvent type**

In `internal/voice/session.go`, inside `type Options struct`, after `OnState`:

```go
	// OnRX reports one COMPLETED received transmission, for the history
	// log. Delivered from a goroutine of its own, one at a time, in order.
	//
	// It deliberately does NOT reuse the OnState delivery path: deliverLoop
	// drains a queue typed to state changes and returns PERMANENTLY on
	// StateClosed, so an RX event queued behind a close would never be
	// delivered.
	//
	// A slow callback costs dropped log rows, never dropped audio: the
	// queue is bounded and a full one drops with a counter, the same
	// discipline Session.emit uses for control events.
	OnRX func(RXEvent)

	// HistoryIdleMS is how long a stream may go without an accepted packet
	// before its transmission is considered over, FOR THE HISTORY LOG ONLY.
	// 0 (or negative) means defaultHistoryIdleMS.
	//
	// It is deliberately separate from rxIdleTimeout, which governs when a
	// decoder and jitter buffer are RELEASED. Re-tuning that to suit a log
	// would change voice behaviour to serve a view.
	//
	// The default is a GUESS. It has to sit above a real talker's worst
	// inter-packet gap (nominally 20ms) and below a natural speech pause,
	// and nobody has ever received voice from a real peer on this client,
	// so the distribution it needs to clear is unmeasured. Exposed here,
	// and through [voice] history_idle_ms, so it can be swept on real
	// hardware without a rebuild.
	HistoryIdleMS int
```

and beside the `State` declarations:

```go
// RXEvent is one completed received transmission.
//
// End is the time of the LAST accepted packet, not the time the idle
// threshold noticed -- a duration inflated by the detection delay would be
// a worse number than no number.
type RXEvent struct {
	Sender uuid.UUID
	Freq   KHz
	Start  time.Time
	End    time.Time
}

// defaultHistoryIdleMS is Options.HistoryIdleMS's fallback. See that
// field's doc for why this number is a guess rather than a measurement.
const defaultHistoryIdleMS = 500

// historyIdleFor resolves the configured override to a duration.
func historyIdleFor(ms int) time.Duration {
	if ms <= 0 {
		return defaultHistoryIdleMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}
```

- [ ] **Step 4: Add the Session plumbing**

In `type Session struct`, beside `onState`:

```go
	onRX        func(RXEvent)
	historyIdle time.Duration

	// rxEvents carries completed transmissions to the OnRX delivery
	// goroutine. Buffered and never blocked on: rxService runs on the
	// decode goroutine, and a consumer that stalls must cost log rows, not
	// audio.
	rxEvents chan RXEvent
```

In `Dial`, beside the other option copies:

```go
		onRX:        opt.OnRX,
		historyIdle: historyIdleFor(opt.HistoryIdleMS),
```

and after the existing goroutine launches (alongside `deliverLoop`), start the delivery loop only when a callback exists:

```go
	if s.onRX != nil {
		s.rxEvents = make(chan RXEvent, 64)
		s.wg.Add(1)
		go s.rxDeliverLoop()
	}
```

Add the loop and the queue helper to `rx.go`:

```go
// rxDeliverLoop is the sole caller of OnRX. It runs on a goroutine of its
// own so a slow consumer delays only later log rows -- never the decode
// goroutine, which is the one that must keep up with the network.
func (s *Session) rxDeliverLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			// Drain whatever is already queued so a transmission that ended
			// as the session closed still reaches the log.
			for {
				select {
				case ev := <-s.rxEvents:
					s.onRX(ev)
				default:
					return
				}
			}
		case ev := <-s.rxEvents:
			s.onRX(ev)
		}
	}
}

// queueRXEvent hands one completed transmission to the delivery goroutine
// without ever blocking the caller. A full queue drops the row and counts
// it: the alternative is stalling the decode goroutine behind a consumer,
// which would turn a logging problem into an audio problem.
func (s *Session) queueRXEvent(ev RXEvent) {
	if s.rxEvents == nil {
		return
	}
	select {
	case s.rxEvents <- ev:
	default:
		s.rx.historyDropped.Add(1)
		s.log.Warn("voice: history event queue full, dropping transmission row",
			"sender", ev.Sender, "freq", ev.Freq)
	}
}
```

- [ ] **Step 5: Add the per-stream state and the detection branch**

In `rx.go`'s `type rxStream struct`, beside `lastSeen`:

```go
	// started is when this transmission's FIRST accepted packet arrived,
	// and ended records that the history threshold has already fired for
	// it. Both are written only under rxState.mu (open) or on the decode
	// goroutine (rxService), which are the only two writers.
	//
	// ended is what makes "one row per transmission" true: without it every
	// rxService sweep between the 500ms threshold and the 5s reap would
	// emit the same transmission again. Clearing it when a packet arrives
	// is also what makes two sentences 600ms apart two rows rather than
	// one, and it falls out of the same flag rather than needing its own
	// mechanism.
	started time.Time
	ended   bool
```

In `type rxState struct`, beside the other counters:

```go
	historyDropped atomic.Uint64
```

In `rxStreamFor`, where the stream is constructed, set `started`:

```go
	st := &rxStream{
		key:     key,
		global:  global,
		started: at,
		jit:     newJitter(r.jitterTarget, r.jitterMax, rxFrameDuration),
		ring:    audio.NewRing(r.ringFrames),
		dec:     dec,
		pcm:     make([]float32, opus.FrameSamples),
	}
```

In `rxVoice`, immediately after `st.lastSeen.Store(at.UnixNano())`, restart a
transmission that had already been reported as ended:

```go
	// A packet on a stream the history threshold already closed starts a
	// NEW transmission. Guarded by rx.mu because rxService (decode
	// goroutine) reads and writes the same two fields.
	if st.ended {
		r.mu.Lock()
		if st.ended {
			st.ended = false
			st.started = at
		}
		r.mu.Unlock()
	}
```

In `rxService`, inside the per-stream loop, **before** the existing idle check:

```go
		last := time.Unix(0, st.lastSeen.Load())
		if !st.ended && s.historyIdle > 0 && now.Sub(last) >= s.historyIdle {
			r.mu.Lock()
			if !st.ended {
				st.ended = true
				s.queueRXEvent(RXEvent{
					Sender: st.key.sender,
					Freq:   st.key.freq,
					Start:  st.started,
					End:    last,
				})
			}
			r.mu.Unlock()
		}
		if now.Sub(last) >= rxIdleTimeout {
			idle = append(idle, st.key)
		}
```

(Replace the existing `if now.Sub(time.Unix(0, st.lastSeen.Load())) >= rxIdleTimeout` line with the `last`-based form above so the clock is read once per stream per sweep.)

Add the close hook:

```go
// endOpenRXStreams reports every stream still open as a completed
// transmission. Called from Close so a transmission in flight during a
// reconnect produces one truthful row instead of vanishing.
//
// End is each stream's lastSeen, not now: the talker stopped being heard
// when their last packet arrived, not when this client tore the socket
// down.
func (s *Session) endOpenRXStreams() {
	r := &s.rx
	r.mu.Lock()
	pending := make([]RXEvent, 0, len(r.streams))
	for _, st := range r.streams {
		if st.ended {
			continue
		}
		st.ended = true
		pending = append(pending, RXEvent{
			Sender: st.key.sender,
			Freq:   st.key.freq,
			Start:  st.started,
			End:    time.Unix(0, st.lastSeen.Load()),
		})
	}
	r.mu.Unlock()

	for _, ev := range pending {
		s.queueRXEvent(ev)
	}
}
```

In `Session.Close`, call `s.endOpenRXStreams()` **before** `close(s.done)`, so the events are queued while the delivery goroutine is still draining.

Expose the counter in `RXStats` beside `Byes`:

```go
	// HistoryDropped counts transmission rows dropped because the history
	// delivery queue was full. Non-zero means the log is incomplete; audio
	// is unaffected.
	HistoryDropped uint64
```

and populate it in `RXStats()`: `HistoryDropped: r.historyDropped.Load(),`.

- [ ] **Step 6: Run tests to verify they pass**

Run, **with `dangerouslyDisableSandbox`**:
`GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/voice/ -v`
Expected: PASS — the new `RXHistory` cases **and** the whole existing voice suite, which must be unchanged.

- [ ] **Step 7: Vet and commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/voice/
git add internal/voice/
git commit -m "feat(voice): report completed RX transmissions for the history log

There is no end-of-transmission marker on the wire -- the server never
sends BYE and the client sends one only from Close -- so a transmission's
end is detected by a dedicated idle threshold, default 500ms, evaluated in
the existing rxService sweep. rxIdleTimeout stays 5s and untouched: it is
an audio-path lifetime, and re-tuning it to suit a log would change voice
behaviour to serve a view.

Delivery gets its own goroutine rather than reusing deliverLoop, which
returns permanently on StateClosed. A full queue drops log rows with a
counter; it never stalls the decode goroutine.

The threshold is a guess. Options.HistoryIdleMS and [voice]
history_idle_ms exist so it can be swept on real hardware without a
rebuild."
```

---

### Task 8: `internal/app` — profile mapping, LOAD, SAVE, dirty

**Files:**
- Create: `internal/app/profiles.go`, `internal/app/profiles_test.go`
- Modify: `internal/app/dto.go`

**Interfaces:**
- Consumes: `internal/profile` (Tasks 1–3), `internal/config` (Task 6), existing `settingsBackend`, `App.pushPersistedRadios`, `App.windows`.
- Produces:
  - `type ProfileSummaryDTO struct { Path, Name, Description, Author string; Modified string; RadioCount int; Window ProfileWindowDTO; Blocks []ProfileBlockDTO }`
  - `type ProfileBlockDTO struct { RadioID uint32; W, H int }`
  - `type ProfileWindowDTO struct { W, H int }`
  - `type ProfileStateDTO struct { ActivePath, ActiveName string; Dirty bool; Dir string }`
  - `func (a *App) profilesDir() (string, error)`
  - `func (a *App) captureProfile(name, desc string) *profile.Document`
  - `func (a *App) applyProfile(d *profile.Document, path string) error`
  - `func (a *App) profileDirty() bool`
  - `func selectionFor(radios []config.Radio, current uint32) uint32`

- [ ] **Step 1: Write the failing tests**

Create `internal/app/profiles_test.go`. Study `internal/app/settings_test.go` first for how it builds an `App` with a `settingsBackend` over a temp `config.toml`, and reuse that helper:

```go
package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
)

// IMPLEMENTER: settings_test.go already constructs an App with a
// settingsBackend over a temp config.toml. Reuse that constructor rather
// than writing a second one; if it is unexported and differently named,
// call it by its real name here.

func TestSelectionForKeepsAValidSelection(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 2); got != 2 {
		t.Fatalf("selectionFor = %d, want the existing selection 2", got)
	}
}

func TestSelectionForFallsBackWhenRadioIsGone(t *testing.T) {
	// The silently-dead-PTT case: LOAD swaps the radio set wholesale, and
	// resolveTXTarget returns nil for a missing id -- a green PTT light
	// over a transmission nobody hears.
	radios := []config.Radio{
		{ID: 7, Name: "a", FrequencyKHz: 118500, Enabled: true},
		{ID: 8, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 3); got != 7 {
		t.Fatalf("selectionFor = %d, want the first ENABLED radio 7", got)
	}
}

func TestSelectionForSkipsDisabledRadios(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: false},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 99); got != 2 {
		t.Fatalf("selectionFor = %d, want 2 -- a disabled radio cannot be a PTT target", got)
	}
}

func TestSelectionForDisabledCurrentIsReplaced(t *testing.T) {
	radios := []config.Radio{
		{ID: 1, Name: "a", FrequencyKHz: 118500, Enabled: false},
		{ID: 2, Name: "b", FrequencyKHz: 122750, Enabled: true},
	}
	if got := selectionFor(radios, 1); got != 2 {
		t.Fatalf("selectionFor = %d, want 2 -- the stored selection is present but disabled", got)
	}
}

// Review Focus #4.
func TestSelectionForEmptyProfileIsZero(t *testing.T) {
	if got := selectionFor(nil, 5); got != 0 {
		t.Fatalf("selectionFor = %d, want 0 -- an empty profile leaves nothing selected, and the Comms window shows its 'No radios' empty state", got)
	}
}

func TestApplyProfileWritesConfigOnce(t *testing.T) {
	a, cfgPath := newTestAppWithConfig(t) // from settings_test.go
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "Fleet Op",
		Radios: []profile.Radio{
			{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true},
		},
		Layout: profile.Layout{
			Window: profile.WindowSize{W: 600, H: 800},
			Blocks: []profile.Block{{RadioID: 1, W: 400, H: 150}},
		},
	}
	path := filepath.Join(t.TempDir(), "fleet-op"+profile.Ext)
	if err := a.applyProfile(d, path); err != nil {
		t.Fatalf("applyProfile: %v", err)
	}

	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Radios) != 1 || back.Radios[0].FrequencyKHz != 118500 {
		t.Fatalf("radios = %+v, want the profile's, as INTEGER kHz", back.Radios)
	}
	if back.SelectedRadioID != 1 {
		t.Fatalf("SelectedRadioID = %d, want 1 (re-validated against the new set)", back.SelectedRadioID)
	}
	if back.CommsLayout.WindowW != 600 || len(back.CommsLayout.Blocks) != 1 {
		t.Fatalf("CommsLayout = %+v", back.CommsLayout)
	}
	if back.ActiveProfile != path {
		t.Fatalf("ActiveProfile = %q, want %q", back.ActiveProfile, path)
	}
}

func TestApplyProfileDoesNotRoundTripFrequencyThroughFloat(t *testing.T) {
	// The one silent failure in this phase. 118_501 kHz has no exact
	// float32 representation as MHz, so a format or mapping that went via
	// a float would come back changed.
	a, cfgPath := newTestAppWithConfig(t)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "x",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118501, Enabled: true}},
	}
	if err := a.applyProfile(d, filepath.Join(t.TempDir(), "x"+profile.Ext)); err != nil {
		t.Fatal(err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.Radios[0].FrequencyKHz != 118501 {
		t.Fatalf("FrequencyKHz = %d, want 118501 exactly", back.Radios[0].FrequencyKHz)
	}
}

func TestCaptureProfileRoundTripsThroughApply(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	src := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "Src",
		Radios: []profile.Radio{
			{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true},
			{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true, IsIntercom: true},
		},
		Layout: profile.Layout{
			Window: profile.WindowSize{W: 610, H: 810},
			Blocks: []profile.Block{{RadioID: 2, W: 300, H: 120}, {RadioID: 1, W: 400, H: 150}},
		},
	}
	if err := a.applyProfile(src, filepath.Join(dir, "src"+profile.Ext)); err != nil {
		t.Fatal(err)
	}
	got := a.captureProfile("Src", "")
	if len(got.Radios) != 2 || got.Radios[1].FrequencyKHz != 122750 || !got.Radios[1].IsIntercom {
		t.Fatalf("radios = %+v", got.Radios)
	}
	if got.Layout.Blocks[0].RadioID != 2 {
		t.Fatalf("block ORDER must survive the round trip: %+v", got.Layout.Blocks)
	}
	if got.Layout.Window.W != 610 {
		t.Fatalf("window = %+v", got.Layout.Window)
	}
}

func TestProfileDirtyIsFalseRightAfterLoad(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("a freshly loaded profile must not be dirty")
	}
}

func TestProfileDirtyAfterLayoutChangeButNotAfterSelectionChange(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios: []profile.Radio{
			{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true},
			{ID: 2, Name: "s", FrequencyKHz: 122750, Enabled: true},
		},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, _ := profile.Read(path)
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}

	// Selecting a different radio must NOT dirty the profile: that is the
	// whole reason selected_radio_id is not stored in the document.
	if err := a.SelectRadio(2); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("a radio-select must not dirty the profile -- it is the control hit most often mid-operation")
	}

	// Resizing a block MUST dirty it.
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.CommsLayout.Blocks = []config.LayoutBlock{{RadioID: 1, W: 999, H: 150}, {RadioID: 2, W: 400, H: 150}}
	sb.cfg = &next
	sb.mu.Unlock()
	if !a.profileDirty() {
		t.Fatal("a block resize must dirty the profile")
	}
}

func TestProfileDirtyIsFalseWithNoActiveProfile(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	if a.profileDirty() {
		t.Fatal("with no active profile there is nothing to compare against, so nothing is unsaved")
	}
}

func TestProfileDirtyWhenActiveFileVanished(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "x"+profile.Ext)
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          "X",
		Radios:        []profile.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}},
	}
	if err := profile.Write(path, d); err != nil {
		t.Fatal(err)
	}
	loaded, _ := profile.Read(path)
	if err := a.applyProfile(loaded, path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !a.profileDirty() {
		t.Fatal("a profile whose file is gone reads as dirty: the live state is genuinely unsaved")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'Profile|Selection|Capture|Apply'`
Expected: FAIL — undefined `selectionFor`, `applyProfile`, `captureProfile`, `profileDirty`.

- [ ] **Step 3: Add the DTOs**

In `internal/app/dto.go`:

```go
// ProfileWindowDTO / ProfileBlockDTO / ProfileSummaryDTO are the
// binding-facing profile shapes.
//
// Modified is a pre-formatted string, not a time: the Profiles table renders
// it verbatim in a monospace column, and formatting it here keeps one
// rendering of "when" rather than one per window.
type ProfileWindowDTO struct {
	W int `json:"w"`
	H int `json:"h"`
}

type ProfileBlockDTO struct {
	RadioID uint32 `json:"radio_id"`
	W       int    `json:"w"`
	H       int    `json:"h"`
}

type ProfileSummaryDTO struct {
	Path        string            `json:"path"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Modified    string            `json:"modified"`
	RadioCount  int               `json:"radio_count"`
	Window      ProfileWindowDTO  `json:"window"`
	Blocks      []ProfileBlockDTO `json:"blocks"`
}

// ProfileStateDTO is what the dirty dot, the REVERT/RESET controls and the
// Profiles screen's directory field all read.
//
// Dirty is COMPUTED on every call, never stored -- see App.profileDirty.
type ProfileStateDTO struct {
	ActivePath string `json:"active_path"`
	ActiveName string `json:"active_name"`
	Dirty      bool   `json:"dirty"`
	Dir        string `json:"dir"`
}

// LayoutDTO is the Comms arrangement as the frontend sends it back.
type LayoutDTO struct {
	Window ProfileWindowDTO  `json:"window"`
	Blocks []ProfileBlockDTO `json:"blocks"`
}
```

- [ ] **Step 4: Write `internal/app/profiles.go`**

```go
package app

import (
	"fmt"
	"strings"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/profile"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

// selectionFor re-validates a stored radio selection against a radio set.
//
// LOAD replaces the radio set WHOLESALE, and App.resolveTXTarget returns
// nil when no ENABLED radio matches the selected id -- which is a silently
// dead PTT: hotkey:pressed still fires, the indicator still lights, and
// every frame is dropped as DroppedNoTarget. So a selection that does not
// survive the swap falls back to the first enabled radio, or 0 when the
// profile has none (the Comms window then shows its existing "No radios"
// empty state, which is honest).
//
// This is what replaces storing selected_radio_id in the profile document:
// a runtime rule with no stored field, and so no dirty-dot noise on every
// radio.<n>.select press.
func selectionFor(radios []config.Radio, current uint32) uint32 {
	for _, r := range radios {
		if r.ID == current && r.Enabled {
			return current
		}
	}
	for _, r := range radios {
		if r.Enabled {
			return r.ID
		}
	}
	return 0
}

// profileRadiosToConfig maps the document's radios to config's.
//
// The kHz integer is carried straight across. It never passes through
// RadioDTO.Frequency (float32 MHz) on this path: the server compares
// advertised frequencies with exact float32 equality, and a needless
// round trip through a float is exactly how a radio silently stops being
// relayed.
func profileRadiosToConfig(in []profile.Radio) []config.Radio {
	out := make([]config.Radio, 0, len(in))
	for _, r := range in {
		out = append(out, config.Radio{
			ID:           r.ID,
			Name:         r.Name,
			FrequencyKHz: r.FrequencyKHz,
			Enabled:      r.Enabled,
			IsIntercom:   r.IsIntercom,
		})
	}
	return out
}

func configRadiosToProfile(in []config.Radio) []profile.Radio {
	out := make([]profile.Radio, 0, len(in))
	for _, r := range in {
		out = append(out, profile.Radio{
			ID:           r.ID,
			Name:         r.Name,
			FrequencyKHz: r.FrequencyKHz,
			Enabled:      r.Enabled,
			IsIntercom:   r.IsIntercom,
		})
	}
	return out
}

func profileLayoutToConfig(l profile.Layout) config.CommsLayout {
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return config.CommsLayout{WindowW: l.Window.W, WindowH: l.Window.H, Blocks: blocks}
}

func configLayoutToProfile(l config.CommsLayout) profile.Layout {
	blocks := make([]profile.Block, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		blocks = append(blocks, profile.Block{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return profile.Layout{
		Window: profile.WindowSize{W: l.WindowW, H: l.WindowH},
		Blocks: blocks,
	}
}

// profilesDir resolves where profiles live for this session.
func (a *App) profilesDir() (string, error) {
	sb := a.settings
	if sb == nil {
		return "", fmt.Errorf("profiles: no settings backend")
	}
	sb.mu.Lock()
	configured := sb.cfg.ProfilesDir
	sb.mu.Unlock()
	return config.ProfilesDirPath(configured)
}

// captureProfile renders the CURRENT live state as a profile document.
//
// It deliberately does not capture SelectedRadioID: see selectionFor and
// the phase spec's 4.3.
func (a *App) captureProfile(name, desc string) *profile.Document {
	sb := a.settings
	d := &profile.Document{
		SchemaVersion: profile.SchemaVersion,
		Name:          name,
		Description:   desc,
	}
	if sb == nil {
		return d
	}
	sb.mu.Lock()
	d.Radios = configRadiosToProfile(sb.cfg.Radios)
	d.Layout = configLayoutToProfile(sb.cfg.CommsLayout)
	sb.mu.Unlock()
	d.Reconcile()
	return d
}

// applyProfile writes a loaded document into live config, resizes the Comms
// window, pushes the radios to the server and records the active path.
//
// ONE config.Save covers radios, selection and layout, so there is no
// half-applied state to recover from. The server push comes LAST,
// deliberately: persistRadios already documents the rule that a failed
// local persist must not be masked by a successful server round trip this
// client itself cannot then act on.
//
// A nil settings backend is a no-op returning nil, the discipline every
// other settings-writing method in this package follows so tests that build
// an App with no backend keep working.
func (a *App) applyProfile(d *profile.Document, path string) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	d.Reconcile()
	radios := profileRadiosToConfig(d.Radios)

	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = radios
	next.SelectedRadioID = selectionFor(radios, next.SelectedRadioID)
	next.CommsLayout = profileLayoutToConfig(d.Layout)
	next.ActiveProfile = path
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	selected := next.SelectedRadioID
	window := next.CommsLayout
	sb.mu.Unlock()

	if saveErr != nil {
		return fmt.Errorf("apply profile: %w", saveErr)
	}

	// Keep the in-memory store in step with what was just persisted, the
	// same pairing SetSettingsBackend does at startup.
	if a.st != nil {
		a.st.SetSelectedRadio(selected)
	}
	if a.windows != nil && window.WindowW > 0 && window.WindowH > 0 {
		g := a.windows.Geometry("comms")
		g.W, g.H = window.WindowW, window.WindowH
		a.windows.SetGeometry("comms", windowstate.Geometry{X: g.X, Y: g.Y, W: g.W, H: g.H})
	}
	// pushPersistedRadios nil-guards a.sess, so an offline load persists
	// locally and pushes nothing.
	a.pushPersistedRadios(context.Background())
	return nil
}

// profileDirty reports whether live state differs from the active profile's
// file.
//
// COMPUTED, never stored. There is nothing to keep in sync and nothing that
// can go stale across a crash, an external edit, or a config change made by
// another code path.
//
// With NO active profile there is no dirty state: an ad-hoc setup is not
// "unsaved", it simply is not a profile. A profile whose file has VANISHED
// does read as dirty -- the live state is then genuinely unsaved.
func (a *App) profileDirty() bool {
	sb := a.settings
	if sb == nil {
		return false
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	live := configRadiosToProfile(sb.cfg.Radios)
	liveLayout := configLayoutToProfile(sb.cfg.CommsLayout)
	sb.mu.Unlock()

	if strings.TrimSpace(path) == "" {
		return false
	}
	saved, err := profile.Read(path)
	if err != nil {
		return true
	}
	cur := &profile.Document{Radios: live, Layout: liveLayout}
	cur.Reconcile()
	return !radiosEqual(cur.Radios, saved.Radios) || !layoutEqual(cur.Layout, saved.Layout)
}

func radiosEqual(a, b []profile.Radio) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func layoutEqual(a, b profile.Layout) bool {
	if a.Window != b.Window || len(a.Blocks) != len(b.Blocks) {
		return false
	}
	for i := range a.Blocks {
		if a.Blocks[i] != b.Blocks[i] {
			return false
		}
	}
	return true
}
```

Imports for this file: `context`, `errors`, `fmt`, `os`, `path/filepath`,
`strings`, `time`, `internal/config`, `internal/notify`,
`internal/profile`, `internal/windowstate`, and
`github.com/wailsapp/wails/v3/pkg/application` (added in Task 9).

- [ ] **Step 5: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'Profile|Selection|Capture|Apply' -v`
Expected: PASS.

- [ ] **Step 6: Run the whole app suite, vet, commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/app/
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/app/
git add internal/app/
git commit -m "feat(app): profile mapping, atomic apply and computed dirty state

One config.Save covers radios, selection and layout, so a profile load has
no half-applied state; the server push comes last, per the rule
persistRadios already documents. Dirty is computed by comparing live config
against the file, never stored, so nothing can go stale. LOAD re-validates
the radio selection because resolveTXTarget returns nil for a missing id,
which is a dead PTT with the indicator still lit."
```

---

### Task 9: `internal/app` — profile bindings, dialogs, startup seeding

**Files:**
- Modify: `internal/app/profiles.go`, `internal/app/profiles_test.go`

**Interfaces:**
- Consumes: Task 8's helpers; `internal/notify` (`a.notif`); Wails `application.OpenFileDialog`/`SaveFileDialog`.
- Produces (all bound methods on `*App`):
  - `ListProfiles() []ProfileSummaryDTO`
  - `GetProfileState() ProfileStateDTO`
  - `LoadProfile(path string) error`
  - `SaveProfile() error`
  - `SaveProfileAs(name, desc string) error`
  - `RenameProfile(path, name, desc string) error`
  - `DeleteProfile(path string) error`
  - `RevertProfile() error`
  - `ResetLayout() error`
  - `ImportProfile() error`
  - `ExportProfile(path string) error`
  - `BrowseProfilesDir() error`
  - `OpenProfilesDir() error`
  - `SeedBuiltinProfiles()` (called from `main.go`, not bound intent but harmless)
  - `func (a *App) emitProfileState()`

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/profiles_test.go`:

```go
func TestSaveProfileAsWritesAndActivates(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()

	if err := a.SaveProfileAs("Fleet Op — Stanton", "notes"); err != nil {
		t.Fatalf("SaveProfileAs: %v", err)
	}
	want := filepath.Join(dir, "fleet-op-stanton"+profile.Ext)
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected %s: %v", want, err)
	}
	if st := a.GetProfileState(); st.ActivePath != want || st.ActiveName != "Fleet Op — Stanton" || st.Dirty {
		t.Fatalf("state = %+v", st)
	}
}

func TestSaveProfileAsDisambiguatesCollidingSlug(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	if err := a.SaveProfileAs("Fleet Op", ""); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProfileAs("Fleet  Op", ""); err != nil {
		t.Fatal(err)
	}
	got, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("two profiles whose names slug identically must not overwrite each other, got %d: %+v", len(got), got)
	}
}

func TestDeleteActiveProfileLeavesLiveConfigIntact(t *testing.T) {
	a, cfgPath := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	active := a.GetProfileState().ActivePath

	if err := a.DeleteProfile(active); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Radios) != 1 {
		t.Fatal("deleting a saved copy must not wipe the radios you are currently using")
	}
	if back.ActiveProfile != "" {
		t.Fatalf("ActiveProfile = %q, want cleared", back.ActiveProfile)
	}
}

func TestResetLayoutLeavesRadiosAlone(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	next.SelectedRadioID = 1
	next.CommsLayout = config.CommsLayout{WindowW: 999, WindowH: 999,
		Blocks: []config.LayoutBlock{{RadioID: 1, W: 999, H: 999}}}
	sb.cfg = &next
	sb.mu.Unlock()

	if err := a.ResetLayout(); err != nil {
		t.Fatalf("ResetLayout: %v", err)
	}
	sb.mu.Lock()
	got := *sb.cfg
	sb.mu.Unlock()
	if len(got.Radios) != 1 || got.SelectedRadioID != 1 {
		t.Fatal("RESET is layout-only: a control that wiped tuned frequencies while claiming to reset a layout is a trap")
	}
	if got.CommsLayout.WindowW != profile.DefaultWindowW {
		t.Fatalf("window = %+v, want the built-in default", got.CommsLayout)
	}
	if got.CommsLayout.Blocks[0].W != profile.DefaultBlockW {
		t.Fatalf("blocks = %+v, want default sizes", got.CommsLayout.Blocks)
	}
}

func TestRevertRestoresTheSavedProfile(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}

	sb.mu.Lock()
	n2 := *sb.cfg
	n2.CommsLayout.Blocks = []config.LayoutBlock{{RadioID: 1, W: 999, H: 999}}
	sb.cfg = &n2
	sb.mu.Unlock()
	if !a.profileDirty() {
		t.Fatal("precondition: should be dirty")
	}

	if err := a.RevertProfile(); err != nil {
		t.Fatalf("RevertProfile: %v", err)
	}
	if a.profileDirty() {
		t.Fatal("REVERT must leave the profile clean")
	}
}

func TestRevertWithNoActiveProfileIsAnError(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	if err := a.RevertProfile(); err == nil {
		t.Fatal("REVERT with nothing active has nothing to revert to")
	}
}

func TestLoadProfileNotifiesOnMalformedFile(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	n := newTestNotifier(t) // from notifications_test.go
	a.setNotifier(n)
	p := filepath.Join(t.TempDir(), "bad"+profile.Ext)
	if err := os.WriteFile(p, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.LoadProfile(p); err == nil {
		t.Fatal("want an error")
	}
	items := n.Snapshot().Items
	if len(items) == 0 {
		t.Fatal("a malformed profile must raise a notification naming the file")
	}
	if !strings.Contains(items[0].Body, "bad") {
		t.Fatalf("notification must name the file: %+v", items[0])
	}
}

func TestSeedBuiltinProfilesRecordsHashes(t *testing.T) {
	a, cfgPath := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)

	a.SeedBuiltinProfiles()

	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.BuiltinProfiles) == 0 {
		t.Fatal("seeding must record the hash of every builtin it wrote")
	}
	got, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("no builtin written")
	}
}

func TestSeedBuiltinProfilesIsIdempotent(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	a.SeedBuiltinProfiles()
	first, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.SeedBuiltinProfiles()
	second, err := profile.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("second seed changed the directory: %d -> %d", len(first), len(second))
	}
}
```

Add the test seam to `internal/app/profiles.go`:

```go
// setProfilesDirForTest overrides the resolved profiles directory. Test
// seam only: production resolves through config.ProfilesDirPath, which
// touches the real AppDataDir. Mirrors setCaptureTimeout's role.
func (a *App) setProfilesDirForTest(dir string) { a.profilesDirOverride = dir }
```

and the field on `App`:

```go
	// profilesDirOverride is set only by tests; empty in every shipped
	// build. See setProfilesDirForTest.
	profilesDirOverride string
```

with `profilesDir()` consulting it first.

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'SaveProfile|Delete|Reset|Revert|Load|Seed'`
Expected: FAIL — undefined bindings.

- [ ] **Step 3: Implement the bindings**

Append to `internal/app/profiles.go`:

```go
// ListProfiles returns the profile directory listing.
//
// Always non-nil: the frontend types it as an array, and a nil slice
// marshals to null. A directory that cannot be listed notifies once and
// returns empty, so the screen renders an empty table rather than a blank
// page.
func (a *App) ListProfiles() []ProfileSummaryDTO {
	out := []ProfileSummaryDTO{}
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return out
	}
	sums, err := profile.List(dir)
	if err != nil {
		a.notifyProfileError("Cannot read the profiles directory", err)
		return out
	}
	for _, s := range sums {
		blocks := make([]ProfileBlockDTO, 0, len(s.Blocks))
		for _, b := range s.Blocks {
			blocks = append(blocks, ProfileBlockDTO{RadioID: b.RadioID, W: b.W, H: b.H})
		}
		modified := ""
		if !s.ModifiedAt.IsZero() {
			modified = s.ModifiedAt.Local().Format("2006-01-02 15:04")
		}
		out = append(out, ProfileSummaryDTO{
			Path: s.Path, Name: s.Name, Description: s.Description, Author: s.Author,
			Modified: modified, RadioCount: s.RadioCount,
			Window: ProfileWindowDTO{W: s.Window.W, H: s.Window.H},
			Blocks: blocks,
		})
	}
	return out
}

// GetProfileState returns the active profile, its dirty state and the
// resolved directory.
func (a *App) GetProfileState() ProfileStateDTO {
	sb := a.settings
	if sb == nil {
		return ProfileStateDTO{}
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()

	st := ProfileStateDTO{ActivePath: path, Dirty: a.profileDirty()}
	if dir, err := a.profilesDir(); err == nil {
		st.Dir = dir
	}
	if path != "" {
		if d, err := profile.Read(path); err == nil {
			st.ActiveName = d.Name
		}
	}
	return st
}

// LoadProfile applies a profile from disk.
func (a *App) LoadProfile(path string) error {
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not load profile", err)
		return err
	}
	if err := a.applyProfile(d, path); err != nil {
		a.notifyProfileError("Could not apply profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// SaveProfile overwrites the active profile with the live state, keeping
// its name, description, author and created_at.
func (a *App) SaveProfile() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no active profile to save; use Save Current As New")
	}
	prev, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read the active profile", err)
		return err
	}
	d := a.captureProfile(prev.Name, prev.Description)
	d.Author = prev.Author
	d.CreatedAt = prev.CreatedAt
	d.ModifiedAt = time.Now().UTC()
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not save profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// SaveProfileAs writes the live state to a NEW profile and activates it.
//
// The filename stem comes from profile.Slug, disambiguated with a numeric
// suffix when it collides: two differently-named profiles that slug
// identically ("Fleet Op" and "Fleet  Op") must not silently overwrite each
// other.
func (a *App) SaveProfileAs(name, desc string) error {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	path := uniqueProfilePath(dir, profile.Slug(name))
	now := time.Now().UTC()
	d := a.captureProfile(name, desc)
	d.CreatedAt, d.ModifiedAt = now, now
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not save profile", err)
		return err
	}
	if err := a.applyProfile(d, path); err != nil {
		a.notifyProfileError("Could not activate the saved profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// uniqueProfilePath returns dir/stem.vcs.json, or dir/stem-2.vcs.json and
// so on when that name is taken.
func uniqueProfilePath(dir, stem string) string {
	p := filepath.Join(dir, stem+profile.Ext)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	for i := 2; i < 1000; i++ {
		c := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, profile.Ext))
		if _, err := os.Stat(c); errors.Is(err, os.ErrNotExist) {
			return c
		}
	}
	return p
}

// RenameProfile edits a profile's display name and description in place.
//
// The FILENAME is deliberately left alone: renaming a display name must not
// break a path someone else has a copy of, or that active_profile points
// at.
func (a *App) RenameProfile(path, name, desc string) error {
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read profile", err)
		return err
	}
	d.Name, d.Description = name, desc
	d.ModifiedAt = time.Now().UTC()
	if err := profile.Write(path, d); err != nil {
		a.notifyProfileError("Could not rename profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// DeleteProfile removes a profile file.
//
// If it was the active one, active_profile is cleared and LIVE CONFIG IS
// LEFT UNTOUCHED -- deleting a saved copy must not wipe the radios you are
// currently using.
func (a *App) DeleteProfile(path string) error {
	if err := profile.Delete(path); err != nil {
		a.notifyProfileError("Could not delete profile", err)
		return err
	}
	sb := a.settings
	if sb != nil {
		sb.writeMu.Lock()
		sb.mu.Lock()
		if sb.cfg.ActiveProfile == path {
			next := *sb.cfg
			next.ActiveProfile = ""
			if sb.cfgPath != "" {
				if err := config.Save(sb.cfgPath, &next); err == nil {
					sb.cfg = &next
				}
			} else {
				sb.cfg = &next
			}
		}
		sb.mu.Unlock()
		sb.writeMu.Unlock()
	}
	a.emitProfileState()
	return nil
}

// RevertProfile re-reads the active profile and re-applies it, discarding
// unsaved changes.
func (a *App) RevertProfile() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.mu.Lock()
	path := sb.cfg.ActiveProfile
	sb.mu.Unlock()
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("no active profile to revert to")
	}
	return a.LoadProfile(path)
}

// ResetLayout restores the built-in default layout.
//
// LAYOUT ONLY. It does not touch radios or the selected radio: a control
// that silently wiped your tuned frequencies while claiming to reset a
// layout would be a trap. It is the only escape when no profile is loaded.
func (a *App) ResetLayout() error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()

	sb.mu.Lock()
	next := *sb.cfg
	blocks := make([]config.LayoutBlock, 0, len(next.Radios))
	for _, r := range next.Radios {
		blocks = append(blocks, config.LayoutBlock{
			RadioID: r.ID, W: profile.DefaultBlockW, H: profile.DefaultBlockH,
		})
	}
	next.CommsLayout = config.CommsLayout{
		WindowW: profile.DefaultWindowW,
		WindowH: profile.DefaultWindowH,
		Blocks:  blocks,
	}
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	if saveErr != nil {
		return fmt.Errorf("reset layout: %w", saveErr)
	}
	if a.windows != nil {
		g := a.windows.Geometry("comms")
		a.windows.SetGeometry("comms", windowstate.Geometry{
			X: g.X, Y: g.Y, W: profile.DefaultWindowW, H: profile.DefaultWindowH,
		})
	}
	a.emitProfileState()
	return nil
}

// ImportProfile copies a profile from anywhere on disk into profiles_dir.
//
// A CANCELLED dialog is a normal outcome, not an error: it returns nil and
// notifies nothing.
func (a *App) ImportProfile() error {
	if a.wailsApp == nil {
		return nil
	}
	src, err := application.OpenFileDialog().
		SetTitle("Import radio profile").
		AddFilter("VCS radio profile", "*"+profile.Ext).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(src) == "" {
		return nil // cancelled
	}
	d, err := profile.Read(src)
	if err != nil {
		a.notifyProfileError("Could not import profile", err)
		return err
	}
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	if err := profile.Write(uniqueProfilePath(dir, profile.Slug(d.Name)), d); err != nil {
		a.notifyProfileError("Could not import profile", err)
		return err
	}
	a.emitProfileState()
	return nil
}

// ExportProfile writes a copy of one profile to a user-chosen path. It does
// not change the active profile.
func (a *App) ExportProfile(path string) error {
	if a.wailsApp == nil {
		return nil
	}
	d, err := profile.Read(path)
	if err != nil {
		a.notifyProfileError("Could not read profile", err)
		return err
	}
	dst, err := application.SaveFileDialog().
		SetTitle("Export radio profile").
		SetFilename(profile.Slug(d.Name) + profile.Ext).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dst) == "" {
		return nil // cancelled
	}
	if err := profile.Write(dst, d); err != nil {
		a.notifyProfileError("Could not export profile", err)
		return err
	}
	return nil
}

// BrowseProfilesDir asks for a new profiles directory and persists it.
func (a *App) BrowseProfilesDir() error {
	if a.wailsApp == nil {
		return nil
	}
	dir, err := application.OpenFileDialog().
		SetTitle("Choose a profiles folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dir) == "" {
		return nil // cancelled
	}
	sb := a.settings
	if sb == nil {
		return nil
	}
	sb.writeMu.Lock()
	sb.mu.Lock()
	next := *sb.cfg
	next.ProfilesDir = dir
	var saveErr error
	if sb.cfgPath != "" {
		saveErr = config.Save(sb.cfgPath, &next)
	}
	if saveErr == nil {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	sb.writeMu.Unlock()
	if saveErr != nil {
		a.notifyProfileError("Could not save the profiles folder", saveErr)
		return saveErr
	}
	a.emitProfileState()
	return nil
}

// OpenProfilesDir reveals the profiles folder in the OS file manager.
func (a *App) OpenProfilesDir() error {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Profiles directory unavailable", err)
		return err
	}
	return openInFileManager(dir)
}

// SeedBuiltinProfiles writes the shipped default profiles into
// profiles_dir and records their hashes. Called once at startup.
//
// Best-effort: a failure notifies and does not block startup, because one
// unwritable default must not cost the user the others or the session.
func (a *App) SeedBuiltinProfiles() {
	dir, err := a.profilesDir()
	if err != nil {
		a.notifyProfileError("Could not seed default profiles", err)
		return
	}
	sb := a.settings
	if sb == nil {
		return
	}
	sb.mu.Lock()
	recorded := make(map[string]string, len(sb.cfg.BuiltinProfiles))
	for k, v := range sb.cfg.BuiltinProfiles {
		recorded[k] = v
	}
	sb.mu.Unlock()

	results, seedErr := profile.Seed(dir, recorded)
	if seedErr != nil {
		a.notifyProfileError("Could not seed a default profile", seedErr)
	}

	updated := make(map[string]string, len(results))
	for k, v := range recorded {
		updated[k] = v
	}
	changed := false
	for _, r := range results {
		if r.Hash == "" {
			continue
		}
		if updated[r.ID] != r.Hash {
			updated[r.ID] = r.Hash
			changed = true
		}
	}
	if !changed {
		return
	}

	sb.writeMu.Lock()
	sb.mu.Lock()
	next := *sb.cfg
	next.BuiltinProfiles = updated
	if sb.cfgPath != "" {
		if err := config.Save(sb.cfgPath, &next); err == nil {
			sb.cfg = &next
		}
	} else {
		sb.cfg = &next
	}
	sb.mu.Unlock()
	sb.writeMu.Unlock()
}

// emitProfileState broadcasts the active-profile snapshot to every window.
// The Comms popout renders the dirty dot and the REVERT/RESET controls, and
// it is a separate webview with its own JS heap, so this cannot live in the
// main window's Zustand store.
func (a *App) emitProfileState() {
	sb := a.settings
	if sb == nil || sb.em == nil {
		return
	}
	sb.em.ProfileState(a.GetProfileState())
}

// notifyProfileError raises one error notification for a profile failure.
// Keyed on the title so a repeated failure of the same kind collapses
// through notify's fingerprint rather than stacking.
func (a *App) notifyProfileError(title string, err error) {
	if a.notif == nil || err == nil {
		return
	}
	a.notif.Raise("profile:"+title, notify.Item{
		Category: "Profiles",
		Severity: notify.SeverityError,
		Icon:     "layout",
		Title:    title,
		Body:     err.Error(),
	})
}
```

Add `openInFileManager` to a new `internal/app/reveal.go`, using `exec.Command` with `explorer` (windows), `open` (darwin), `xdg-open` (default). Keep it in one small file so the per-OS switch is visible in one place.

Add `ProfileState` to `internal/events/events.go`:

```go
// EventProfileState carries the active profile path, its name, the computed
// dirty flag and the resolved profiles directory.
//
// Broadcast to EVERY window: the Comms popout renders the dirty dot and the
// REVERT/RESET controls and is a separate webview with its own JS heap, the
// same reason connection:state and notifications:changed are Go-owned.
EventProfileState = "profile:state"
```

```go
func (t *Tagged) ProfileState(payload any) { t.em.Emit(EventProfileState, payload) }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'SaveProfile|Delete|Reset|Revert|Load|Seed' -v`
Expected: PASS.

- [ ] **Step 5: Full suite, vet, commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/app/
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/app/
git add internal/app/ internal/events/
git commit -m "feat(app): profile bindings, native dialogs and startup seeding

DELETE of the active profile clears the pointer and leaves live config
alone -- deleting a saved copy must not wipe the radios in use. RESET is
layout-only. RENAME leaves the filename alone so a shared path keeps
working. A cancelled dialog is a normal outcome, not an error."
```

---

### Task 10: `internal/app` — layout binding and shutdown flush

**Files:**
- Modify: `internal/app/profiles.go`, `internal/app/app.go` (`ServiceShutdown`), `internal/app/profiles_test.go`

**Interfaces:**
- Consumes: Task 8's mapping helpers, Task 6's `config.CommsLayout`.
- Produces:
  - `func (a *App) GetCommsLayout() LayoutDTO`
  - `func (a *App) SetCommsLayout(l LayoutDTO) error` — **in-memory only**
  - `func (a *App) flushConfig() error` — called from `ServiceShutdown`

- [ ] **Step 1: Write the failing tests**

Append to `internal/app/profiles_test.go`:

```go
func TestSetCommsLayoutWritesNoFile(t *testing.T) {
	a, cfgPath := newTestAppWithConfig(t)
	st, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	before := st.ModTime()

	time.Sleep(10 * time.Millisecond)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatalf("SetCommsLayout: %v", err)
	}

	st2, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !st2.ModTime().Equal(before) {
		t.Fatal("a drag must not write config.toml: no disk I/O on a pointer-move path")
	}
}

func TestSetCommsLayoutIsVisibleImmediatelyInMemory(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatal(err)
	}
	got := a.GetCommsLayout()
	if got.Window.W != 700 || len(got.Blocks) != 1 || got.Blocks[0].W != 400 {
		t.Fatalf("GetCommsLayout = %+v, want the in-memory value", got)
	}
}

func TestSetCommsLayoutDirtiesImmediately(t *testing.T) {
	// Dirty compares IN-MEMORY config, so it must be correct the instant a
	// block moves -- regardless of when the bytes land on disk.
	a, _ := newTestAppWithConfig(t)
	dir := t.TempDir()
	a.setProfilesDirForTest(dir)
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "r", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()
	if err := a.SaveProfileAs("X", ""); err != nil {
		t.Fatal(err)
	}
	if a.profileDirty() {
		t.Fatal("precondition: clean right after save")
	}

	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 999, H: 999},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 999, H: 999}},
	}); err != nil {
		t.Fatal(err)
	}
	if !a.profileDirty() {
		t.Fatal("a resize must dirty the profile immediately, before any file is written")
	}
}

func TestFlushConfigWritesTheLayout(t *testing.T) {
	a, cfgPath := newTestAppWithConfig(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 700, H: 900},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 400, H: 150}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.flushConfig(); err != nil {
		t.Fatalf("flushConfig: %v", err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.CommsLayout.WindowW != 700 || len(back.CommsLayout.Blocks) != 1 {
		t.Fatalf("CommsLayout on disk = %+v", back.CommsLayout)
	}
}

func TestLayoutFreeRidesOnAnUnrelatedConfigSave(t *testing.T) {
	// config.Save serialises the WHOLE Config, so any other settings write
	// carries the layout to disk long before quit. This is what makes the
	// crash window much narrower than "everything since launch".
	a, cfgPath := newTestAppWithConfig(t)
	if err := a.SetCommsLayout(LayoutDTO{
		Window: ProfileWindowDTO{W: 701, H: 901},
		Blocks: []ProfileBlockDTO{{RadioID: 1, W: 401, H: 151}},
	}); err != nil {
		t.Fatal(err)
	}
	// Any existing settings writer will do; persistSelectedRadio is the
	// smallest.
	if err := a.persistSelectedRadio(0); err != nil {
		t.Fatal(err)
	}
	back, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if back.CommsLayout.WindowW != 701 {
		t.Fatalf("an unrelated config.Save must carry the pending layout: %+v", back.CommsLayout)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'CommsLayout|flushConfig|FreeRides'`
Expected: FAIL — undefined `SetCommsLayout`, `GetCommsLayout`, `flushConfig`.

- [ ] **Step 3: Implement**

Append to `internal/app/profiles.go`:

```go
// GetCommsLayout returns the live Comms arrangement for a window hydrating
// on mount. Blocks is always non-nil: the frontend types it as an array.
func (a *App) GetCommsLayout() LayoutDTO {
	out := LayoutDTO{Blocks: []ProfileBlockDTO{}}
	sb := a.settings
	if sb == nil {
		return out
	}
	sb.mu.Lock()
	l := sb.cfg.CommsLayout
	sb.mu.Unlock()
	out.Window = ProfileWindowDTO{W: l.WindowW, H: l.WindowH}
	for _, b := range l.Blocks {
		out.Blocks = append(out.Blocks, ProfileBlockDTO{RadioID: b.RadioID, W: b.W, H: b.H})
	}
	return out
}

// SetCommsLayout records a new arrangement IN MEMORY ONLY.
//
// It deliberately does NOT call config.Save. This runs from a drag, and a
// debounced save on a pointer-move path would put disk I/O behind every
// block the user nudges -- to protect state that is re-created with one
// more drag. The bytes land at shutdown, via flushConfig from
// ServiceShutdown.
//
// In practice the window is much narrower than "everything since launch":
// config.Save serialises the WHOLE Config, so persistRadios,
// persistSelectedRadio and the settings writers all carry the pending
// layout to disk as a side effect of ordinary use.
//
// Dirty state is unaffected by any of this: App.profileDirty compares the
// IN-MEMORY config, so the dot is correct the instant a block moves.
func (a *App) SetCommsLayout(l LayoutDTO) error {
	sb := a.settings
	if sb == nil {
		return nil
	}
	blocks := make([]config.LayoutBlock, 0, len(l.Blocks))
	for _, b := range l.Blocks {
		w, h := b.W, b.H
		if w < profile.MinBlockW {
			w = profile.MinBlockW
		}
		if h < profile.MinBlockH {
			h = profile.MinBlockH
		}
		blocks = append(blocks, config.LayoutBlock{RadioID: b.RadioID, W: w, H: h})
	}
	sb.mu.Lock()
	next := *sb.cfg
	next.CommsLayout = config.CommsLayout{WindowW: l.Window.W, WindowH: l.Window.H, Blocks: blocks}
	sb.cfg = &next
	sb.mu.Unlock()
	a.emitProfileState()
	return nil
}

// flushConfig persists the in-memory config. Called from ServiceShutdown so
// a layout that only ever lived in memory reaches disk on quit.
//
// Errors are returned for the test's benefit; ServiceShutdown logs and
// carries on, because there is no window left to notify into and a failed
// layout write must not block the quit.
func (a *App) flushConfig() error {
	sb := a.settings
	if sb == nil || sb.cfgPath == "" {
		return nil
	}
	sb.writeMu.Lock()
	defer sb.writeMu.Unlock()
	sb.mu.Lock()
	snapshot := *sb.cfg
	sb.mu.Unlock()
	if err := config.Save(sb.cfgPath, &snapshot); err != nil {
		return fmt.Errorf("flush config: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Call it from `ServiceShutdown`**

In `internal/app/app.go`, inside `ServiceShutdown`, **before** the hotkey
close (so it runs even if a later step misbehaves):

```go
	// Persist anything held only in memory -- today that is the Comms
	// layout, which SetCommsLayout deliberately does not write during a
	// drag. Logged rather than notified: by this point there is no window
	// left to show a notification in, and a failed layout write must not
	// block the quit.
	if err := a.flushConfig(); err != nil {
		a.logger.Warn("config flush on shutdown failed", "err", err)
	}
```

- [ ] **Step 5: Run tests, vet, commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/app/ -v -run 'CommsLayout|flushConfig|FreeRides'
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/app/
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/app/
git add internal/app/
git commit -m "feat(app): comms layout in memory, flushed at shutdown

A drag updates config in memory and writes nothing; ServiceShutdown flushes
it. No disk I/O on a pointer-move path, and because config.Save serialises
the whole Config the layout free-rides on every ordinary settings write
anyway. Dirty compares in-memory config, so the dot is correct the instant
a block moves."
```

---

### Task 11: `internal/app` — the history sources

**Files:**
- Create: `internal/app/history.go`, `internal/app/history_test.go`
- Modify: `internal/app/voice.go` (three `SetTXFrequencies` sites), `internal/app/dto.go`, `internal/events/events.go`

**Interfaces:**
- Consumes: Task 5's `history.Log`, Task 7's `voice.RXEvent`, `state.Store`, `config.Radios`.
- Produces:
  - `type HistoryEntryDTO struct { At string; Sender, GUID string; FreqKHz uint32; FreqMHz float64; Radio string; DurationMS int; Own bool }`
  - `func (a *App) setHistory(l *history.Log)` / `func SetHistory(a *App, l *history.Log)`
  - `func (a *App) GetHistory() []HistoryEntryDTO`
  - `func (a *App) ClearHistory()`
  - `func (a *App) ExportHistoryCSV() error`
  - `func (a *App) onVoiceRX(ev voice.RXEvent)`
  - `func NotifyHistoryFlush(a *App, err error)`
  - `func (a *App) appendHistory(e history.Entry)`
  - `func (a *App) selfCallsign() string`
  - `func (a *App) installTXTargetsLocked(sess voiceSessionAPI, targets []voice.TXTarget)`
  - `func (a *App) resolveSender(id uuid.UUID) (guid, callsign string)`
  - `func (a *App) channelNameFor(freq voice.KHz) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/app/history_test.go`:

```go
package app

import (
	"strings"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/FPGSchiba/vcs-srs-client/srspb"
	"github.com/google/uuid"
)

func TestOnVoiceRXRecordsAResolvedRow(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))

	id := uuid.New()
	a.st.UpdateClient(id.String(), &srspb.ClientInfo{Name: "Dabble"})
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true}}
	sb.cfg = &next
	sb.mu.Unlock()

	start := time.Date(2026, 9, 30, 21, 15, 48, 0, time.UTC)
	a.onVoiceRX(voice.RXEvent{Sender: id, Freq: 118500, Start: start, End: start.Add(3200 * time.Millisecond)})

	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatalf("history = %+v, want 1 row", got)
	}
	if got[0].Sender != "Dabble" || got[0].Radio != "Fleet Common" || got[0].DurationMS != 3200 || got[0].Own {
		t.Fatalf("row = %+v", got[0])
	}
}

func TestResolveSenderHandlesNonCanonicalStoreKey(t *testing.T) {
	// The store is keyed by the SERVER's GUID string while voice carries a
	// uuid.UUID. uuid.Parse accepts uppercase, braced and unhyphenated
	// forms that UUID.String() does not round-trip to, so a naive
	// sender.String() lookup can silently render a blank sender.
	a, _ := newTestAppWithConfig(t)
	id := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	a.st.UpdateClient(strings.ToUpper(id.String()), &srspb.ClientInfo{Name: "Shouty"})

	guid, callsign := a.resolveSender(id)
	if callsign != "Shouty" {
		t.Fatalf("callsign = %q, want Shouty -- the index must normalise GUID spelling", callsign)
	}
	if guid == "" {
		t.Fatal("guid must be the store's own key so the row can be traced back")
	}
}

func TestOnVoiceRXUnknownSenderStillRecords(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	start := time.Now().UTC()
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500, Start: start, End: start.Add(time.Second)})
	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatal("a transmission from a client that has already left must still record: the frequency and duration are worth keeping")
	}
	if got[0].Sender != "" {
		t.Fatalf("Sender = %q, want empty rather than a fabricated name", got[0].Sender)
	}
}

func TestGlobalChannelRowHasNoChannelName(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	// No local radio on 200000 kHz: a server global channel is accepted
	// without one.
	start := time.Now().UTC()
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 200000, Start: start, End: start.Add(time.Second)})
	if got := a.GetHistory(); got[0].Radio != "" {
		t.Fatalf("Radio = %q, want empty -- inventing a channel name for a global would be a lie", got[0].Radio)
	}
}

func TestInstallTXTargetsRecordsOneRowPerFrequency(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	a.st.SetSelf("me", &srspb.ClientInfo{Name: "FPGSchiba"})
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{
		{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true},
	}
	sb.cfg = &next
	sb.mu.Unlock()

	fake := newFakeVoiceSession(t) // from voice_test.go
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}, {Freq: 122750}})
	a.voice.mu.Unlock()

	if got := a.GetHistory(); len(got) != 0 {
		t.Fatalf("nothing is logged while transmitting: %+v", got)
	}

	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, nil)
	a.voice.mu.Unlock()

	got := a.GetHistory()
	if len(got) != 2 {
		t.Fatalf("two targets released together must produce two rows, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if !r.Own || r.Sender != "FPGSchiba" {
			t.Fatalf("own rows must carry the local callsign: %+v", r)
		}
	}
}

func TestInstallTXTargetsPartialReleaseLogsOnlyTheReleasedOne(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	sb := a.settings
	sb.mu.Lock()
	next := *sb.cfg
	next.Radios = []config.Radio{
		{ID: 1, Name: "Fleet Common", FrequencyKHz: 118500, Enabled: true},
		{ID: 2, Name: "Wing", FrequencyKHz: 122750, Enabled: true},
	}
	sb.cfg = &next
	sb.mu.Unlock()

	fake := newFakeVoiceSession(t)
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}, {Freq: 122750}})
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}})
	a.voice.mu.Unlock()

	got := a.GetHistory()
	if len(got) != 1 || got[0].FreqKHz != 122750 {
		t.Fatalf("only the released frequency logs: %+v", got)
	}
}

func TestOwnRowWithNoSelfStillRecords(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	fake := newFakeVoiceSession(t)
	a.voice.mu.Lock()
	a.installTXTargetsLocked(fake, []voice.TXTarget{{Freq: 118500}})
	a.installTXTargetsLocked(fake, nil)
	a.voice.mu.Unlock()
	got := a.GetHistory()
	if len(got) != 1 {
		t.Fatal("a row before SyncClient lands must still record")
	}
	if got[0].Sender != "" {
		t.Fatalf("Sender = %q, want empty", got[0].Sender)
	}
}

func TestClearHistoryEmpties(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	a.setHistory(history.New(10))
	a.onVoiceRX(voice.RXEvent{Sender: uuid.New(), Freq: 118500,
		Start: time.Now(), End: time.Now().Add(time.Second)})
	a.ClearHistory()
	if got := a.GetHistory(); len(got) != 0 {
		t.Fatalf("history = %+v, want empty", got)
	}
}

func TestGetHistoryWithNoLogIsEmptyNotNil(t *testing.T) {
	a, _ := newTestAppWithConfig(t)
	if got := a.GetHistory(); got == nil {
		t.Fatal("must be a non-nil empty slice: the frontend types it as an array and nil marshals to null")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run 'History|VoiceRX|ResolveSender|InstallTX|GlobalChannel|OwnRow'`
Expected: FAIL — undefined `setHistory`, `onVoiceRX`, `installTXTargetsLocked`, `resolveSender`.

- [ ] **Step 3: Add the DTO and event**

In `internal/app/dto.go`:

```go
// HistoryEntryDTO is one Transmission Log row.
//
// At is pre-formatted RFC3339 rather than a time, and FreqMHz is derived
// here beside the stored kHz integer, so every window renders the same
// string and the same number without repeating the conversion. FreqKHz
// stays on the wire because the channel filter matches on it.
type HistoryEntryDTO struct {
	At         string  `json:"at"`
	Sender     string  `json:"sender"`
	GUID       string  `json:"guid"`
	FreqKHz    uint32  `json:"freq_khz"`
	FreqMHz    float64 `json:"freq_mhz"`
	Radio      string  `json:"radio"`
	DurationMS int     `json:"dur_ms"`
	Own        bool    `json:"own"`
}
```

In `internal/events/events.go`:

```go
// EventHistoryAppended carries ONE entry, not a snapshot.
//
// A deliberate divergence from EventNotifications, which broadcasts the
// full list: that works because notify.DefaultCap is small, whereas
// history.DefaultCap is 2000 entries at ~120B -- roughly 240KB per emit, at
// up to several transmissions per second. Windows hydrate with
// App.GetHistory() and apply deltas after that.
EventHistoryAppended = "history:appended"
// EventHistoryCleared says the log was emptied.
EventHistoryCleared = "history:cleared"
```

```go
func (t *Tagged) HistoryAppended(entry any) { t.em.Emit(EventHistoryAppended, entry) }
func (t *Tagged) HistoryCleared()           { t.em.Emit(EventHistoryCleared, nil) }
```

- [ ] **Step 4: Write `internal/app/history.go`**

```go
package app

import (
	"os"
	"strings"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/history"
	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/google/uuid"
)

// setHistory wires the transmission log. A nil log leaves every binding and
// both sources inert, the same discipline setNotifier documents.
func (a *App) setHistory(l *history.Log) { a.hist = l }

// SetHistory wires the transmission log. Package-level for the reason
// SetNotifier documents: nothing package-level is bound by
// application.NewService, so main.go keeps its call and the renderer gains
// nothing.
func SetHistory(a *App, l *history.Log) { a.setHistory(l) }

// resolveSender maps a voice sender id to the store's GUID key and the
// client's callsign.
//
// It does NOT look the store up by sender.String(). The store is keyed by
// the SERVER's GUID string, and uuid.Parse -- which App.voiceDialInputs
// already uses on that same string -- accepts uppercase, brace-wrapped and
// unhyphenated forms that UUID.String() does not round-trip to. A direct
// string lookup would therefore miss silently and render a blank sender,
// with nothing logged to explain it. Parsing every key and comparing UUID
// VALUES makes the spelling irrelevant.
//
// Returns ("", "") when the client is not in the store, which is normal:
// they may have left before the transmission was detected as over.
func (a *App) resolveSender(id uuid.UUID) (string, string) {
	if a.st == nil {
		return "", ""
	}
	for guid, info := range a.st.Snapshot().Clients {
		parsed, err := uuid.Parse(guid)
		if err != nil || parsed != id {
			continue
		}
		return guid, info.GetName()
	}
	return "", ""
}

// channelNameFor returns the LOCAL radio name tuned to freq, or "".
//
// An empty result is a real answer, not a failure: server global channels
// are accepted without a tuned radio (voice.rxContext.global), so there is
// no local name to show, and inventing one would be a lie. The UI renders
// the frequency alone.
func (a *App) channelNameFor(freq voice.KHz) string {
	sb := a.settings
	if sb == nil {
		return ""
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for _, r := range sb.cfg.Radios {
		if voice.KHz(r.FrequencyKHz) == freq {
			return r.Name
		}
	}
	return ""
}

// appendHistory records one entry and broadcasts it.
func (a *App) appendHistory(e history.Entry) {
	if a.hist == nil {
		return
	}
	a.hist.Append(e)
	if sb := a.settings; sb != nil && sb.em != nil {
		sb.em.HistoryAppended(historyEntryDTO(e))
	}
}

// onVoiceRX records one received transmission. Wired to voice.Options.OnRX,
// so it runs on that session's dedicated delivery goroutine -- never on the
// decode goroutine.
func (a *App) onVoiceRX(ev voice.RXEvent) {
	guid, callsign := a.resolveSender(ev.Sender)
	a.appendHistory(history.Entry{
		At:         ev.Start.UTC(),
		Sender:     callsign,
		GUID:       guid,
		FreqKHz:    uint32(ev.Freq),
		Radio:      a.channelNameFor(ev.Freq),
		DurationMS: int(ev.End.Sub(ev.Start) / time.Millisecond),
	})
}

// selfCallsign returns the local client's callsign, or "".
//
// Self is a *srspb.ClientInfo and is nil before SyncClient lands, so this
// goes through the getter. An own row before that still records -- with an
// empty sender rather than being dropped, since the frequency and duration
// are the parts worth keeping.
func (a *App) selfCallsign() string {
	if a.st == nil {
		return ""
	}
	return a.st.Snapshot().Self.GetName()
}

// installTXTargetsLocked installs the resolved TX frequency set on the live
// session AND records own-transmission history from the difference.
//
// Every place that installs a target set routes through here -- txPress,
// txRelease and the idle clear -- so a frequency entering the set starts a
// transmission and one leaving it ends one. That yields one row per
// frequency per transmission, which is what actually went on the wire when
// one PTT resolves to two radios.
//
// Hooking the three call sites individually would leave whichever fourth a
// later phase adds silently unlogged, which is exactly the class of gap
// this phase exists to close.
//
// Caller holds voiceState.mu, the same lock the three original call sites
// already held around their SetTXFrequencies store.
func (a *App) installTXTargetsLocked(sess voiceSessionAPI, targets []voice.TXTarget) {
	now := time.Now().UTC()
	next := make(map[voice.KHz]struct{}, len(targets))
	for _, t := range targets {
		next[t.Freq] = struct{}{}
	}
	if a.voice.txStarted == nil {
		a.voice.txStarted = map[voice.KHz]time.Time{}
	}
	for freq := range next {
		if _, live := a.voice.txStarted[freq]; !live {
			a.voice.txStarted[freq] = now
		}
	}
	var ended []struct {
		freq  voice.KHz
		start time.Time
	}
	for freq, start := range a.voice.txStarted {
		if _, still := next[freq]; !still {
			ended = append(ended, struct {
				freq  voice.KHz
				start time.Time
			}{freq, start})
			delete(a.voice.txStarted, freq)
		}
	}

	if sess != nil {
		sess.SetTXFrequencies(targets)
	}

	// Recorded AFTER the session store so the lock is held for exactly as
	// long as it was before, and so a slow history append can never delay
	// the frequency list reaching the wire.
	for _, e := range ended {
		a.appendHistory(history.Entry{
			At:         e.start,
			Sender:     a.selfCallsign(),
			FreqKHz:    uint32(e.freq),
			Radio:      a.channelNameFor(e.freq),
			DurationMS: int(now.Sub(e.start) / time.Millisecond),
			Own:        true,
		})
	}
}

func historyEntryDTO(e history.Entry) HistoryEntryDTO {
	return HistoryEntryDTO{
		At:         e.At.Format(time.RFC3339),
		Sender:     e.Sender,
		GUID:       e.GUID,
		FreqKHz:    e.FreqKHz,
		FreqMHz:    float64(e.FreqKHz) / 1000.0,
		Radio:      e.Radio,
		DurationMS: e.DurationMS,
		Own:        e.Own,
	}
}

// GetHistory returns the whole log, newest first, for a window hydrating on
// mount. Always non-nil.
func (a *App) GetHistory() []HistoryEntryDTO {
	out := []HistoryEntryDTO{}
	if a.hist == nil {
		return out
	}
	for _, e := range a.hist.Snapshot() {
		out = append(out, historyEntryDTO(e))
	}
	return out
}

// ClearHistory empties the log.
func (a *App) ClearHistory() {
	if a.hist == nil {
		return
	}
	a.hist.Clear()
	if sb := a.settings; sb != nil && sb.em != nil {
		sb.em.HistoryCleared()
	}
}

// ExportHistoryCSV writes the log to a user-chosen file. A cancelled dialog
// is a normal outcome, not an error.
func (a *App) ExportHistoryCSV() error {
	if a.hist == nil || a.wailsApp == nil {
		return nil
	}
	dst, err := application.SaveFileDialog().
		SetTitle("Export transmission log").
		SetFilename("transmission-log.csv").
		PromptForSingleSelection()
	if err != nil || strings.TrimSpace(dst) == "" {
		return nil
	}
	if err := os.WriteFile(dst, history.CSV(a.hist.Snapshot()), 0o644); err != nil {
		if a.notif != nil {
			a.notif.Raise("history:export", notify.Item{
				Category: "Transmission Log",
				Severity: notify.SeverityError,
				Icon:     "history",
				Title:    "Could not export the transmission log",
				Body:     err.Error(),
			})
		}
		return err
	}
	return nil
}

// NotifyHistoryFlush is history.Log's onErr hook, as main.go wires it.
//
// Package-level for the reason SetNotifier documents: nothing
// package-level is bound by application.NewService, so main.go keeps its
// call and the renderer gains nothing.
func NotifyHistoryFlush(a *App, err error) { a.notifyHistoryFlush(err) }

// notifyHistoryFlush RAISES on a stable key so notify's fingerprint
// collapses a persistent failure into one row rather than one per 5s tick,
// and RESOLVES on the next success.
func (a *App) notifyHistoryFlush(err error) {
	if a.notif == nil {
		return
	}
	if err == nil {
		a.notif.Resolve("history:flush")
		return
	}
	a.notif.Raise("history:flush", notify.Item{
		Category: "Transmission Log",
		Severity: notify.SeverityError,
		Icon:     "history",
		Title:    "Cannot save the transmission log",
		Body:     err.Error(),
	})
}

```

Add to `App`:

```go
	// hist is the transmission log. Optional, the same discipline as notif
	// and health: nil in tests and in any build where wiring failed.
	hist *history.Log
```

Add to `voiceState` (in `voice.go`):

```go
	// txStarted maps each currently-transmitting frequency to when it
	// entered the TX set, so installTXTargetsLocked can turn the set
	// difference into history rows. Guarded by voiceState.mu, like the tx
	// map beside it.
	txStarted map[voice.KHz]time.Time
```

- [ ] **Step 5: Route the three call sites through the helper**

In `internal/app/voice.go`, replace each of the three stores — at roughly
`:521` (txPress), `:546` (txRelease) and `:643` (clearTXTargetsIfStillIdle):

```go
-		sess.SetTXFrequencies(txTargetsLocked(a.voice.tx))
+		a.installTXTargetsLocked(sess, txTargetsLocked(a.voice.tx))
```

```go
-		sess.SetTXFrequencies(nil)
+		a.installTXTargetsLocked(sess, nil)
```

Leave every surrounding lock and comment exactly as it is — the atomicity
argument those comments make is unchanged, and `installTXTargetsLocked`
performs the same store inside the same critical section.

- [ ] **Step 6: Run tests, vet, commit**

```bash
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./internal/app/ -v
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/app/
git add internal/app/ internal/events/
git commit -m "feat(app): transmission-log sources, resolution and events

RX rows come from voice.OnRX; own rows from diffing the TX target set in
one helper all three install sites route through, so whichever fourth a
later phase adds cannot go silently unlogged. Sender resolution parses
every store key and compares UUID values rather than looking up
sender.String(): uuid.Parse accepts spellings String() does not round-trip
to, and a miss would render a blank sender with nothing logged.

history:appended carries one entry, not a snapshot -- 2000 entries is
~240KB per emit at up to several transmissions per second."
```

---

### Task 12: frontend — API surface, events and stores

**Files:**
- Modify: `frontend/src/shared/api/client.ts`, `frontend/src/shared/api/events.ts`
- Create: `frontend/src/shared/store/profile.ts`, `frontend/src/shared/store/history.ts`, `frontend/src/shared/store/profile.test.ts`, `frontend/src/shared/store/history.test.ts`

**Interfaces:**
- Consumes: Task 9's and Task 11's bindings; the `profile:state`, `history:appended`, `history:cleared` events.
- Produces:
  - Types `ProfileSummary`, `ProfileState`, `Layout`, `LayoutBlock`, `HistoryEntry`
  - `api.listProfiles/getProfileState/loadProfile/saveProfile/saveProfileAs/renameProfile/deleteProfile/revertProfile/resetLayout/importProfile/exportProfile/browseProfilesDir/openProfilesDir/getCommsLayout/setCommsLayout/getHistory/clearHistory/exportHistoryCsv`
  - `useProfile` Zustand store: `{ state, profiles, setState, setProfiles, refresh }`
  - `useHistory` Zustand store: `{ entries, replaceAll, append, clear }`

**Note on regenerated bindings:** `frontend/bindings/` is gitignored and goes stale across branch switches. If `tsc` reports the new `App` methods as missing, regenerate with `wails3 generate bindings -ts -f "-tags purego" -clean=true` — those errors are staleness, not real.

- [ ] **Step 1: Write the failing store tests**

Create `frontend/src/shared/store/history.test.ts`:

```ts
import { describe, expect, it, beforeEach } from "vitest";
import { useHistory } from "./history";
import type { HistoryEntry } from "../api/client";

const entry = (i: number): HistoryEntry => ({
  at: `2026-09-30T21:15:${String(i).padStart(2, "0")}Z`,
  sender: "Dabble",
  guid: "g",
  freq_khz: 118500,
  freq_mhz: 118.5,
  radio: "Fleet Common",
  dur_ms: 3200,
  own: false,
});

describe("useHistory", () => {
  beforeEach(() => useHistory.getState().replaceAll([]));

  it("prepends an appended entry so the list stays newest-first", () => {
    useHistory.getState().replaceAll([entry(2), entry(1)]);
    useHistory.getState().append(entry(3));
    expect(useHistory.getState().entries.map((e) => e.at)).toEqual([
      "2026-09-30T21:15:03Z",
      "2026-09-30T21:15:02Z",
      "2026-09-30T21:15:01Z",
    ]);
  });

  it("caps the list so a long session cannot grow the renderer without bound", () => {
    useHistory.getState().replaceAll([]);
    for (let i = 0; i < 2100; i++) useHistory.getState().append(entry(i % 60));
    expect(useHistory.getState().entries.length).toBe(2000);
  });

  it("clears", () => {
    useHistory.getState().replaceAll([entry(1)]);
    useHistory.getState().clear();
    expect(useHistory.getState().entries).toEqual([]);
  });
});
```

Create `frontend/src/shared/store/profile.test.ts`:

```ts
import { describe, expect, it, beforeEach } from "vitest";
import { useProfile } from "./profile";

describe("useProfile", () => {
  beforeEach(() =>
    useProfile.setState({
      state: { active_path: "", active_name: "", dirty: false, dir: "" },
      profiles: [],
    }),
  );

  it("holds the state the backend sends", () => {
    useProfile.getState().setState({
      active_path: "/p/fleet.vcs.json",
      active_name: "Fleet",
      dirty: true,
      dir: "/p",
    });
    expect(useProfile.getState().state.dirty).toBe(true);
    expect(useProfile.getState().state.active_name).toBe("Fleet");
  });

  it("never holds a nil profile list", () => {
    // The backend returns [] rather than null, but a store default of
    // undefined would still crash .map on first render.
    expect(Array.isArray(useProfile.getState().profiles)).toBe(true);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/shared/store/history.test.ts src/shared/store/profile.test.ts)`
Expected: FAIL — cannot resolve `./history`, `./profile`.

- [ ] **Step 3: Add the API types and methods**

In `frontend/src/shared/api/client.ts`, beside the existing DTO mirrors:

```ts
export interface LayoutBlock {
  radio_id: number;
  w: number;
  h: number;
}

export interface LayoutWindow {
  w: number;
  h: number;
}

export interface Layout {
  window: LayoutWindow;
  blocks: LayoutBlock[];
}

export interface ProfileSummary {
  path: string;
  name: string;
  description: string;
  author: string;
  modified: string;
  radio_count: number;
  window: LayoutWindow;
  blocks: LayoutBlock[];
}

export interface ProfileState {
  active_path: string;
  active_name: string;
  dirty: boolean;
  dir: string;
}

export interface HistoryEntry {
  at: string;
  sender: string;
  guid: string;
  freq_khz: number;
  freq_mhz: number;
  radio: string;
  dur_ms: number;
  own: boolean;
}
```

and to the `api` object:

```ts
  listProfiles: (): Promise<ProfileSummary[]> =>
    App.ListProfiles() as Promise<ProfileSummary[]>,
  getProfileState: (): Promise<ProfileState> =>
    App.GetProfileState() as Promise<ProfileState>,
  loadProfile: (path: string): Promise<void> => App.LoadProfile(path) as Promise<void>,
  saveProfile: (): Promise<void> => App.SaveProfile() as Promise<void>,
  saveProfileAs: (name: string, desc: string): Promise<void> =>
    App.SaveProfileAs(name, desc) as Promise<void>,
  renameProfile: (path: string, name: string, desc: string): Promise<void> =>
    App.RenameProfile(path, name, desc) as Promise<void>,
  deleteProfile: (path: string): Promise<void> => App.DeleteProfile(path) as Promise<void>,
  revertProfile: (): Promise<void> => App.RevertProfile() as Promise<void>,
  resetLayout: (): Promise<void> => App.ResetLayout() as Promise<void>,
  importProfile: (): Promise<void> => App.ImportProfile() as Promise<void>,
  exportProfile: (path: string): Promise<void> => App.ExportProfile(path) as Promise<void>,
  browseProfilesDir: (): Promise<void> => App.BrowseProfilesDir() as Promise<void>,
  openProfilesDir: (): Promise<void> => App.OpenProfilesDir() as Promise<void>,
  getCommsLayout: (): Promise<Layout> => App.GetCommsLayout() as Promise<Layout>,
  setCommsLayout: (l: Layout): Promise<void> => App.SetCommsLayout(l) as Promise<void>,
  getHistory: (): Promise<HistoryEntry[]> => App.GetHistory() as Promise<HistoryEntry[]>,
  clearHistory: (): Promise<void> => App.ClearHistory() as Promise<void>,
  exportHistoryCsv: (): Promise<void> => App.ExportHistoryCSV() as Promise<void>,
```

In `frontend/src/shared/api/events.ts`, add to `EV`:

```ts
  profileState: "profile:state",
  historyAppended: "history:appended",
  historyCleared: "history:cleared",
```

- [ ] **Step 4: Write the stores**

Create `frontend/src/shared/store/profile.ts`:

```ts
import { create } from "zustand";
import { api } from "../api/client";
import type { ProfileState, ProfileSummary } from "../api/client";

interface ProfileStore {
  state: ProfileState;
  profiles: ProfileSummary[];
  setState: (s: ProfileState) => void;
  setProfiles: (p: ProfileSummary[]) => void;
  /** Re-reads both the directory listing and the active-profile state. */
  refresh: () => Promise<void>;
}

const EMPTY: ProfileState = { active_path: "", active_name: "", dirty: false, dir: "" };

/**
 * Profile state is GO-OWNED: `dirty` is computed backend-side by comparing
 * live config against the active profile's file, and the Comms popout is a
 * separate webview with its own JS heap. This store is a cache of what
 * `profile:state` last delivered, never a source of truth.
 */
export const useProfile = create<ProfileStore>((set) => ({
  state: EMPTY,
  profiles: [],
  setState: (s) => set({ state: s ?? EMPTY }),
  setProfiles: (p) => set({ profiles: p ?? [] }),
  refresh: async () => {
    const [state, profiles] = await Promise.all([
      api.getProfileState(),
      api.listProfiles(),
    ]);
    set({ state: state ?? EMPTY, profiles: profiles ?? [] });
  },
}));
```

Create `frontend/src/shared/store/history.ts`:

```ts
import { create } from "zustand";
import type { HistoryEntry } from "../api/client";

/**
 * CAP mirrors internal/history.DefaultCap. The backend already evicts at
 * this bound; the renderer caps too because `history:appended` is a DELTA
 * -- a window left open across a long session would otherwise accumulate
 * rows the backend had already dropped.
 */
const CAP = 2000;

interface HistoryStore {
  entries: HistoryEntry[];
  replaceAll: (e: HistoryEntry[]) => void;
  append: (e: HistoryEntry) => void;
  clear: () => void;
}

/**
 * The log is delivered as a snapshot on mount (App.GetHistory) and as
 * single-entry deltas after that. It is NOT a full-snapshot broadcast like
 * notifications: 2000 entries is ~240KB per emit at up to several
 * transmissions per second.
 */
export const useHistory = create<HistoryStore>((set) => ({
  entries: [],
  replaceAll: (e) => set({ entries: (e ?? []).slice(0, CAP) }),
  append: (e) => set((s) => ({ entries: [e, ...s.entries].slice(0, CAP) })),
  clear: () => set({ entries: [] }),
}));
```

- [ ] **Step 5: Run tests and typecheck**

```bash
(cd frontend && npx vitest run src/shared/store/history.test.ts src/shared/store/profile.test.ts)
(cd frontend && npx tsc --noEmit)
```
Expected: PASS and no type errors. If `tsc` reports the new `App` methods missing, regenerate the bindings first (see the note above).

- [ ] **Step 6: Commit**

```bash
git add frontend/src/shared/
git commit -m "feat(frontend): profile and history API surface and stores

History arrives as a mount snapshot plus single-entry deltas rather than a
full-snapshot broadcast: 2000 entries is ~240KB per emit at up to several
transmissions per second. The renderer caps at the same bound the backend
evicts at, because a window left open across a long session would otherwise
accumulate rows the backend had already dropped."
```

---

### Task 13: frontend — Transmission Log screen

**Files:**
- Create: `frontend/src/windows/main/screens/History.tsx`, `frontend/src/windows/main/screens/History.test.tsx`
- Modify: `frontend/src/windows/main/MainApp.tsx`

**Interfaces:**
- Consumes: Task 12's `useHistory`, `api.getHistory/clearHistory/exportHistoryCsv`, `EV.historyAppended`/`EV.historyCleared`.
- Produces: `export function History()`.

Port from `design/vcs/project/screens/misc.jsx:239-276`, keeping classNames byte-identical so the ported CSS applies unchanged. **Drop the Replay column** — recording is PROTO_GAPS #9.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/main/screens/History.test.tsx`:

```tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { History } from "./History";
import { useHistory } from "../../../shared/store/history";
import type { HistoryEntry } from "../../../shared/api/client";

vi.mock("../../../shared/api/client", async (orig) => {
  const actual = await orig<typeof import("../../../shared/api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getHistory: vi.fn(async () => [] as HistoryEntry[]),
      clearHistory: vi.fn(async () => {}),
      exportHistoryCsv: vi.fn(async () => {}),
    },
  };
});

const row = (o: Partial<HistoryEntry> = {}): HistoryEntry => ({
  at: "2026-09-30T21:15:51Z",
  sender: "Dabble",
  guid: "g1",
  freq_khz: 118500,
  freq_mhz: 118.5,
  radio: "Fleet Common",
  dur_ms: 3200,
  own: false,
  ...o,
});

describe("History screen", () => {
  beforeEach(() => useHistory.getState().replaceAll([]));

  it("renders sender, channel, frequency and duration", async () => {
    useHistory.getState().replaceAll([row()]);
    render(<History />);
    expect(await screen.findByText("Dabble")).toBeInTheDocument();
    expect(screen.getByText("Fleet Common")).toBeInTheDocument();
    expect(screen.getByText("118.500")).toBeInTheDocument();
    expect(screen.getByText("3.2s")).toBeInTheDocument();
  });

  it("has no Replay column", () => {
    useHistory.getState().replaceAll([row()]);
    render(<History />);
    expect(screen.queryByText(/replay/i)).not.toBeInTheDocument();
  });

  it("renders a global-channel row with the frequency and no invented name", () => {
    useHistory.getState().replaceAll([row({ radio: "", freq_khz: 200000, freq_mhz: 200 })]);
    render(<History />);
    expect(screen.getByText("200.000")).toBeInTheDocument();
    expect(screen.queryByText("Fleet Common")).not.toBeInTheDocument();
  });

  it("marks own transmissions", () => {
    useHistory.getState().replaceAll([row({ own: true, sender: "FPGSchiba" })]);
    render(<History />);
    expect(screen.getByText("FPGSchiba")).toHaveClass("own");
  });

  it("filters by channel", async () => {
    useHistory.getState().replaceAll([
      row({ sender: "Dabble", freq_khz: 118500 }),
      row({ sender: "Elphi", freq_khz: 122750, radio: "Wing" }),
    ]);
    render(<History />);
    fireEvent.change(screen.getByLabelText(/channel/i), { target: { value: "122750" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("filters by search across sender and channel", async () => {
    useHistory.getState().replaceAll([
      row({ sender: "Dabble" }),
      row({ sender: "Elphi", radio: "Wing" }),
    ]);
    render(<History />);
    fireEvent.change(screen.getByPlaceholderText(/search/i), { target: { value: "wing" } });
    await waitFor(() => expect(screen.queryByText("Dabble")).not.toBeInTheDocument());
    expect(screen.getByText("Elphi")).toBeInTheDocument();
  });

  it("renders an empty state rather than a bare table", () => {
    render(<History />);
    expect(screen.getByText(/no transmissions/i)).toBeInTheDocument();
  });

  it("subscribes to history:appended and unsubscribes exactly once on real unmount", () => {
    // StrictMode double-invokes effects; the control is that a REAL unmount
    // still detaches the listener.
    const { unmount } = render(<History />);
    const before = useHistory.getState().entries.length;
    unmount();
    expect(useHistory.getState().entries.length).toBe(before);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/main/screens/History.test.tsx)`
Expected: FAIL — cannot resolve `./History`.

- [ ] **Step 3: Write the screen**

Create `frontend/src/windows/main/screens/History.tsx`. Port the prototype's
markup verbatim except for the Replay column, and drive the channel filter
from the rows present rather than from the radio list, so a channel you have
since retuned away from is still filterable:

```tsx
import { useEffect, useMemo, useState } from "react";
import { api } from "../../../shared/api/client";
import type { HistoryEntry } from "../../../shared/api/client";
import { EV, on } from "../../../shared/api/events";
import { useHistory } from "../../../shared/store/history";
import { Icon } from "../../../shared/components/Icon";

const WINDOWS = [
  { value: "5m", label: "5M", ms: 5 * 60_000 },
  { value: "30m", label: "30M", ms: 30 * 60_000 },
  { value: "1h", label: "1H", ms: 60 * 60_000 },
  { value: "4h", label: "4H", ms: 4 * 60 * 60_000 },
  { value: "all", label: "ALL", ms: Number.POSITIVE_INFINITY },
];

/**
 * History is the Transmission Log nav screen, ported from the design
 * prototype's `ScreenHistory`.
 *
 * Two deliberate deviations from the prototype, both forced by what exists:
 * the Replay column is DROPPED (recording is PROTO_GAPS #9, so every cell
 * would be dead forever), and the channel filter is built from the rows
 * present rather than from the current radio list -- a channel you have
 * since retuned away from still has entries worth finding.
 *
 * The log is Go-owned: this hydrates once with App.GetHistory and applies
 * `history:appended` deltas after that. It is not a full-snapshot broadcast
 * like notifications; see the store's own note.
 */
export function History() {
  const entries = useHistory((s) => s.entries);
  const [channel, setChannel] = useState("all");
  const [window, setWindow] = useState("1h");
  const [search, setSearch] = useState("");

  useEffect(() => {
    api
      .getHistory()
      .then((rows) => useHistory.getState().replaceAll(rows ?? []))
      .catch(() => {
        /* not wired yet -- ignore */
      });
    const offs = [
      on<HistoryEntry>(EV.historyAppended, (e) => useHistory.getState().append(e)),
      on<null>(EV.historyCleared, () => useHistory.getState().clear()),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const channels = useMemo(() => {
    const seen = new Map<number, string>();
    for (const e of entries) {
      if (!seen.has(e.freq_khz)) seen.set(e.freq_khz, e.radio);
    }
    return [...seen.entries()].sort((a, b) => a[0] - b[0]);
  }, [entries]);

  const rows = useMemo(() => {
    const cutoff =
      WINDOWS.find((w) => w.value === window)?.ms ?? Number.POSITIVE_INFINITY;
    const since = Date.now() - cutoff;
    const q = search.trim().toLowerCase();
    return entries.filter((e) => {
      if (channel !== "all" && String(e.freq_khz) !== channel) return false;
      if (Number.isFinite(cutoff) && new Date(e.at).getTime() < since) return false;
      if (q && !`${e.sender} ${e.radio}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [entries, channel, window, search]);

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", minHeight: 0 }}>
      <div
        style={{
          padding: "10px 16px",
          borderBottom: "1px solid var(--bd-1)",
          background: "var(--bg-0)",
          display: "flex",
          alignItems: "center",
          gap: 12,
        }}
      >
        <span className="cap">FILTER</span>
        <label className="sr-only" htmlFor="hist-chan">
          Channel
        </label>
        <select
          id="hist-chan"
          className="input"
          value={channel}
          onChange={(e) => setChannel(e.target.value)}
          style={{ width: 220 }}
        >
          <option value="all">All channels</option>
          {channels.map(([khz, name]) => (
            <option key={khz} value={String(khz)}>
              {name ? `${name} · ` : ""}
              {(khz / 1000).toFixed(3)}
            </option>
          ))}
        </select>
        <span className="sep-v" style={{ height: 20 }} />
        <span className="cap">TIME</span>
        <div className="seg">
          {WINDOWS.map((w) => (
            <button
              key={w.value}
              type="button"
              className={`seg-btn ${window === w.value ? "active" : ""}`}
              onClick={() => setWindow(w.value)}
            >
              {w.label}
            </button>
          ))}
        </div>
        <span className="sep-v" style={{ height: 20 }} />
        <input
          className="input flex"
          placeholder="Search sender or callsign…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <button type="button" className="btn" onClick={() => void api.exportHistoryCsv()}>
          <Icon name="download" size={11} /> EXPORT CSV
        </button>
      </div>

      <div style={{ flex: 1, overflow: "auto", minHeight: 0 }}>
        {rows.length === 0 ? (
          <div
            className="col acenter"
            style={{ justifyContent: "center", height: "100%", color: "var(--tx-3)", gap: 8 }}
          >
            No transmissions yet
          </div>
        ) : (
          <table className="tbl">
            <thead>
              <tr>
                <th style={{ width: 90 }}>Time</th>
                <th>Sender</th>
                <th>Channel</th>
                <th>Frequency</th>
                <th style={{ width: 80 }}>Duration</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((e, i) => (
                <tr key={`${e.at}-${e.freq_khz}-${i}`}>
                  <td className="mono" style={{ color: "var(--tx-2)" }}>
                    {new Date(e.at).toLocaleTimeString()}
                  </td>
                  <td>
                    <span className={e.own ? "own" : undefined}>{e.sender}</span>
                  </td>
                  <td>{e.radio}</td>
                  <td className="mono" style={{ color: "var(--ac-lcd)" }}>
                    {e.freq_mhz.toFixed(3)}
                  </td>
                  <td className="mono">{(e.dur_ms / 1000).toFixed(1)}s</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
```

Add an `.own { color: var(--ac-primary); }` rule to the shared stylesheet
beside the other ported classes.

- [ ] **Step 4: Route it in `MainApp.tsx`**

```tsx
    view === "settings" ? <SettingsScreen /> :
+   view === "history" ? <History /> :
    <Placeholder />;
```

with the import beside the other screens.

- [ ] **Step 5: Run tests, typecheck, commit**

```bash
(cd frontend && npx vitest run src/windows/main/screens/History.test.tsx)
(cd frontend && npx tsc --noEmit)
git add frontend/src/
git commit -m "feat(frontend): Transmission Log screen

Ported from the prototype minus the Replay column -- recording is
PROTO_GAPS #9 and every cell would be dead forever. The channel filter is
built from the rows present rather than the current radio list, so a
channel you have since retuned away from is still findable."
```

---

### Task 14: frontend — Radio Profiles screen

**Files:**
- Create: `frontend/src/windows/main/screens/Profiles.tsx`, `frontend/src/windows/main/screens/Profiles.test.tsx`
- Modify: `frontend/src/windows/main/MainApp.tsx`

**Interfaces:**
- Consumes: Task 12's `useProfile` and the profile `api` methods; `EV.profileState`.
- Produces: `export function Profiles()`, and a `LayoutPreview` driven by real block sizes.

Port from `design/vcs/project/screens/misc.jsx:8-120`. The prototype's
`LayoutPreview` renders one of three hardcoded schematics; here it renders
the **stored block sizes**, so the Layout column survives with real data.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/main/screens/Profiles.test.tsx`:

```tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { Profiles } from "./Profiles";
import { useProfile } from "../../../shared/store/profile";
import type { ProfileSummary } from "../../../shared/api/client";

const loadProfile = vi.fn(async () => {});
const deleteProfile = vi.fn(async () => {});
const saveProfileAs = vi.fn(async () => {});
const browseProfilesDir = vi.fn(async () => {});
const openProfilesDir = vi.fn(async () => {});
const importProfile = vi.fn(async () => {});
const exportProfile = vi.fn(async () => {});

vi.mock("../../../shared/api/client", async (orig) => {
  const actual = await orig<typeof import("../../../shared/api/client")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      listProfiles: vi.fn(async () => [] as ProfileSummary[]),
      getProfileState: vi.fn(async () => ({
        active_path: "",
        active_name: "",
        dirty: false,
        dir: "/p",
      })),
      loadProfile,
      deleteProfile,
      saveProfileAs,
      browseProfilesDir,
      openProfilesDir,
      importProfile,
      exportProfile,
    },
  };
});

const summary = (o: Partial<ProfileSummary> = {}): ProfileSummary => ({
  path: "/p/fleet-op.vcs.json",
  name: "Fleet Op",
  description: "notes",
  author: "FPGSchiba",
  modified: "2026-09-30 21:00",
  radio_count: 3,
  window: { w: 540, h: 720 },
  blocks: [
    { radio_id: 1, w: 516, h: 180 },
    { radio_id: 2, w: 253, h: 120 },
  ],
  ...o,
});

describe("Profiles screen", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useProfile.setState({
      state: { active_path: "", active_name: "", dirty: false, dir: "/p" },
      profiles: [],
    });
  });

  it("renders a row per profile with its radio count", async () => {
    useProfile.setState({ profiles: [summary()] });
    render(<Profiles />);
    expect(await screen.findByText("Fleet Op")).toBeInTheDocument();
    expect(screen.getByText("3")).toBeInTheDocument();
  });

  it("renders a layout preview rect per stored block, not a preset name", () => {
    useProfile.setState({ profiles: [summary()] });
    const { container } = render(<Profiles />);
    expect(container.querySelectorAll("svg rect")).toHaveLength(2);
    expect(screen.queryByText(/POWER|STRIP|2×2/)).not.toBeInTheDocument();
  });

  it("marks the active profile", () => {
    useProfile.setState({
      profiles: [summary()],
      state: { active_path: "/p/fleet-op.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" },
    });
    render(<Profiles />);
    expect(screen.getByText(/active/i)).toBeInTheDocument();
  });

  it("loads a profile when LOAD is clicked", async () => {
    useProfile.setState({ profiles: [summary()] });
    render(<Profiles />);
    fireEvent.click(screen.getByRole("button", { name: /^load$/i }));
    await waitFor(() => expect(loadProfile).toHaveBeenCalledWith("/p/fleet-op.vcs.json"));
  });

  it("filters by name", async () => {
    useProfile.setState({ profiles: [summary(), summary({ name: "Solo Patrol", path: "/p/solo.vcs.json" })] });
    render(<Profiles />);
    fireEvent.change(screen.getByPlaceholderText(/filter profiles/i), {
      target: { value: "solo" },
    });
    await waitFor(() => expect(screen.queryByText("Fleet Op")).not.toBeInTheDocument());
    expect(screen.getByText("Solo Patrol")).toBeInTheDocument();
  });

  it("shows the resolved profiles directory and wires BROWSE and OPEN", async () => {
    render(<Profiles />);
    expect(screen.getByDisplayValue("/p")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /browse/i }));
    await waitFor(() => expect(browseProfilesDir).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("button", { name: /^open$/i }));
    await waitFor(() => expect(openProfilesDir).toHaveBeenCalled());
  });

  it("renders an empty state when there are no profiles", () => {
    render(<Profiles />);
    expect(screen.getByText(/no profiles/i)).toBeInTheDocument();
  });

  it("gives the clickable row keyboard affordances", () => {
    // SonarCloud typescript:S1082 -- a click handler on a non-button
    // element needs role, tabIndex and Enter/Space in the same commit.
    useProfile.setState({ profiles: [summary()] });
    render(<Profiles />);
    const row = screen.getByText("Fleet Op").closest("tr");
    expect(row).toHaveAttribute("tabindex", "0");
    expect(row).toHaveAttribute("role", "button");
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/main/screens/Profiles.test.tsx)`
Expected: FAIL — cannot resolve `./Profiles`.

- [ ] **Step 3: Write the screen**

Create `frontend/src/windows/main/screens/Profiles.tsx`, porting the
prototype's two-pane grid, table and detail pane. Key points the
implementer must honour:

- `LayoutPreview` takes `blocks: LayoutBlock[]` and `window: LayoutWindow`
  and renders one `<rect>` per block, laid out with the same wrap rule the
  Comms grid uses, scaled into a 42×28 viewBox. It replaces the prototype's
  three hardcoded schematics.
- The row is clickable to select; per S1082 it carries `role="button"`,
  `tabIndex={0}` and an `onKeyDown` handling `Enter` and `Space`.
- The detail pane shows Name, Filename (`path.split(/[\\/]/).pop()`), Last
  modified, Radios, Description, Author. The prototype's "EXAMPLE CONTENT"
  `<pre>` block is **dropped** — it showed a fabricated JSON sample with
  `enc`/`key` fields this format does not have, and rendering fake content
  next to a real file is worse than rendering nothing.
- Action buttons per row: LOAD, RENAME (pencil), EXPORT (download), DELETE
  (trash). The prototype's copy (duplicate) button is **dropped**: there is
  no `DuplicateProfile` binding, and a button that does nothing is the exact
  shape of bug this phase's spec argues against. Note it in the commit
  message so it is a recorded decision, not an omission.
- The header row carries the directory input (read-only, showing
  `state.dir`), BROWSE, OPEN, a name filter, IMPORT FROM FILE and SAVE
  CURRENT AS NEW.
- SAVE CURRENT AS NEW prompts for a name inline (a small controlled input in
  the header, not `window.prompt`, which is a blocking modal dialog the
  harness guidance forbids) and calls `api.saveProfileAs(name, "")`.
- On mount, `useProfile.getState().refresh()`; subscribe to
  `EV.profileState` to keep `state` live and re-`refresh()` the listing.

- [ ] **Step 4: Route it in `MainApp.tsx`**

```tsx
    view === "history" ? <History /> :
+   view === "profiles" ? <Profiles /> :
    <Placeholder />;
```

- [ ] **Step 5: Run tests, typecheck, commit**

```bash
(cd frontend && npx vitest run src/windows/main/screens/Profiles.test.tsx)
(cd frontend && npx tsc --noEmit)
git add frontend/src/
git commit -m "feat(frontend): Radio Profiles screen

LayoutPreview renders the STORED block sizes rather than one of the
prototype's three hardcoded schematics, so the Layout column survives with
real data behind it.

Two prototype elements deliberately dropped: the EXAMPLE CONTENT pre block
(it showed fabricated JSON with enc/key fields this format does not have,
and fake content beside a real file is worse than none) and the duplicate
button (no binding backs it, and a control that does nothing is the exact
bug shape this phase argues against)."
```

---

### Task 15: frontend — resizable Comms layout, dirty dot, REVERT and RESET

**Files:**
- Create: `frontend/src/windows/comms/RadioBlock.tsx`, `frontend/src/windows/comms/RadioBlock.test.tsx`
- Modify: `frontend/src/windows/comms/CommsApp.tsx`, `frontend/src/windows/comms/CommsApp.test.tsx`

**Interfaces:**
- Consumes: Task 12's `api.getCommsLayout/setCommsLayout/revertProfile/resetLayout`, `useProfile`, `EV.profileState`.
- Produces: `export function RadioBlock({ radio, allRadios, muted, width, height, onResize, onReorder, index })`.

- [ ] **Step 1: Write the failing tests**

Create `frontend/src/windows/comms/RadioBlock.test.tsx`:

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { StrictMode } from "react";
import { describe, expect, it, vi } from "vitest";
import { RadioBlock } from "./RadioBlock";
import type { RadioDTO } from "../../shared/api/client";

const radio: RadioDTO = {
  id: 1,
  name: "Fleet",
  frequency: 118.5,
  enabled: true,
  is_intercom: false,
};

describe("RadioBlock", () => {
  it("renders at the size it is given", () => {
    const { container } = render(
      <RadioBlock radio={radio} allRadios={[radio]} muted={false}
        width={400} height={150} index={0} onResize={vi.fn()} onReorder={vi.fn()} />,
    );
    const el = container.firstElementChild as HTMLElement;
    expect(el.style.width).toBe("400px");
    expect(el.style.height).toBe("150px");
  });

  it("reports a resize on pointer drag", () => {
    const onResize = vi.fn();
    render(
      <RadioBlock radio={radio} allRadios={[radio]} muted={false}
        width={400} height={150} index={0} onResize={onResize} onReorder={vi.fn()} />,
    );
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 460, clientY: 190, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalledWith(1, 460, 190);
  });

  it("clamps a resize to the minimum block size", () => {
    const onResize = vi.fn();
    render(
      <RadioBlock radio={radio} allRadios={[radio]} muted={false}
        width={400} height={150} index={0} onResize={onResize} onReorder={vi.fn()} />,
    );
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    const [, w, h] = onResize.mock.calls.at(-1)!;
    expect(w).toBeGreaterThanOrEqual(240);
    expect(h).toBeGreaterThanOrEqual(96);
  });

  it("resizes with the keyboard", () => {
    // S1082: the handle is a non-button element with a pointer handler, so
    // it must also be operable without a pointer.
    const onResize = vi.fn();
    render(
      <RadioBlock radio={radio} allRadios={[radio]} muted={false}
        width={400} height={150} index={0} onResize={onResize} onReorder={vi.fn()} />,
    );
    const handle = screen.getByRole("button", { name: /resize/i });
    expect(handle).toHaveAttribute("tabindex", "0");
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onResize).toHaveBeenCalled();
  });

  it("detaches its window listeners on real unmount", () => {
    const remove = vi.spyOn(window, "removeEventListener");
    const { unmount } = render(
      <RadioBlock radio={radio} allRadios={[radio]} muted={false}
        width={400} height={150} index={0} onResize={vi.fn()} onReorder={vi.fn()} />,
    );
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    unmount();
    expect(remove).toHaveBeenCalledWith("pointermove", expect.any(Function));
    expect(remove).toHaveBeenCalledWith("pointerup", expect.any(Function));
  });

  it("survives StrictMode's simulated unmount with a working handle", () => {
    // The control for the above. StrictMode double-invokes effects; a
    // cleanup that tore down shared state permanently broke keybind capture
    // for two phases before it was caught.
    const onResize = vi.fn();
    render(
      <StrictMode>
        <RadioBlock radio={radio} allRadios={[radio]} muted={false}
          width={400} height={150} index={0} onResize={onResize} onReorder={vi.fn()} />
      </StrictMode>,
    );
    const handle = screen.getByRole("button", { name: /resize/i });
    fireEvent.pointerDown(handle, { clientX: 400, clientY: 150, pointerId: 1 });
    fireEvent.pointerMove(window, { clientX: 500, clientY: 200, pointerId: 1 });
    fireEvent.pointerUp(window, { pointerId: 1 });
    expect(onResize).toHaveBeenCalled();
  });
});
```

Append to `frontend/src/windows/comms/CommsApp.test.tsx`:

```tsx
  it("shows the dirty dot and REVERT only when dirty with an active profile", async () => {
    useProfile.setState({
      state: { active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: true, dir: "/p" },
      profiles: [],
    });
    render(<CommsApp />);
    expect(await screen.findByTitle(/unsaved/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /revert/i })).toBeInTheDocument();
  });

  it("hides REVERT when clean", async () => {
    useProfile.setState({
      state: { active_path: "/p/x.vcs.json", active_name: "Fleet Op", dirty: false, dir: "/p" },
      profiles: [],
    });
    render(<CommsApp />);
    await screen.findByText("Communications");
    expect(screen.queryByRole("button", { name: /revert/i })).not.toBeInTheDocument();
  });

  it("hides REVERT when no profile is active, but still offers RESET", async () => {
    useProfile.setState({
      state: { active_path: "", active_name: "", dirty: false, dir: "/p" },
      profiles: [],
    });
    render(<CommsApp />);
    await screen.findByText("Communications");
    expect(screen.queryByRole("button", { name: /revert/i })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /reset/i })).toBeInTheDocument();
  });
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `(cd frontend && npx vitest run src/windows/comms/)`
Expected: FAIL — cannot resolve `./RadioBlock`; no dirty dot in `CommsApp`.

- [ ] **Step 3: Write `RadioBlock.tsx`**

Wrap the existing `RadioCard` in a sized container with a corner handle.
Requirements the tests pin:

- The container's inline `width`/`height` come from props, in px.
- The handle is a `<span>` with `role="button"`, `tabIndex={0}`,
  `aria-label="Resize radio block"`, `onPointerDown`, and an `onKeyDown`
  that nudges by 16px on arrow keys — S1082 plus genuine keyboard
  operability.
- `onPointerDown` captures the pointer, attaches `pointermove`/`pointerup`
  to `window`, and the effect cleanup removes **both** listeners. The
  cleanup must be idempotent so StrictMode's simulated unmount does not
  leave the handle dead — that failure mode cost two phases in the keybind
  path.
- Sizes are clamped to `MIN_W = 240` / `MIN_H = 96`, mirroring
  `profile.MinBlockW`/`MinBlockH`. Put the two constants in one place with a
  comment naming the Go constants they mirror.
- `onResize(radioId, w, h)` fires on move and on up; the parent debounces.
- Dragging the block body (not the handle) calls `onReorder(fromIndex,
  toIndex)`; use HTML5 `draggable` on the container, which needs no keyboard
  equivalent because reordering is also achievable by resizing and the
  underlying list order is recoverable via RESET.

- [ ] **Step 4: Wire `CommsApp.tsx`**

- Replace the `col gap-4` stack with a `flex-wrap` container.
- Hydrate layout on mount with `api.getCommsLayout()`; hold it in local
  state keyed by `radio_id`.
- On resize/reorder, update local state immediately and call
  `api.setCommsLayout(...)` **debounced ~300ms** — this is a renderer-side
  debounce to avoid an IPC call per pointer-move frame; the backend writes
  nothing to disk either way (Task 10).
- Add to `.popout-chrome`: the active profile name, a `●` dirty dot with
  `title="Unsaved layout changes"` when `state.dirty`, a REVERT button
  rendered **only** when `state.dirty && state.active_path`, and a RESET
  button rendered **always**.
- Subscribe to `EV.profileState` and mount `useProfile` so both stay live in
  this separate webview.

- [ ] **Step 5: Run tests, typecheck, commit**

```bash
(cd frontend && npx vitest run src/windows/comms/)
(cd frontend && npx tsc --noEmit)
(cd frontend && npx vitest run)
git add frontend/src/
git commit -m "feat(frontend): resizable Comms flow grid with revert and reset

Blocks wrap and reorder rather than sitting on a free-form canvas, so
overlap, clamping and shrink policies are unreachable states rather than
rules to get wrong. The resize handle is keyboard-operable (S1082) and its
listener cleanup is idempotent, with a StrictMode test plus a real-unmount
control -- the failure mode that silently broke keybind capture for two
phases.

REVERT renders only when dirty with an active profile; RESET always,
because it is the only escape when nothing is loaded."
```

---

### Task 16: integration wiring and the manual verification checklist

**Files:**
- Modify: `main.go`, `main_wiring_test.go`
- Create: `docs/superpowers/plans/2026-09-30-phase-7-3-manual-verification.md`

- [ ] **Step 1: Write the failing wiring test**

Append to `main_wiring_test.go`, following whatever assertions that file
already makes about the notifier and audio wiring:

```go
func TestHistoryIsWiredIntoTheApp(t *testing.T) {
	// The log must reach App, or every binding early-returns and the
	// Transmission Log is permanently empty with nothing logged to explain
	// it -- the exact failure SetNotifier's doc warns about.
	a := buildWiredApp(t) // reuse this file's existing constructor
	if a.GetHistory() == nil {
		t.Fatal("GetHistory must return a non-nil slice")
	}
	// A wired app records; an unwired one silently does not.
	if !historyIsWired(a) {
		t.Fatal("history log not wired")
	}
}
```

with a small exported-for-test predicate in `internal/app` if the existing
file has no equivalent.

- [ ] **Step 2: Wire it in `main.go`**

Alongside the existing notifier/audio wiring:

```go
	// Transmission log. Loaded before the voice session exists so an early
	// transmission is recorded, and given the app's notifier so a failing
	// flush raises ONE notification rather than one per tick.
	histPath, err := config.HistoryFilePath()
	if err != nil {
		logger.Warn("history: no app-data dir; the transmission log will not persist", "err", err)
	}
	histLog, err := history.Load(histPath, history.DefaultCap)
	if err != nil {
		logger.Warn("history: could not read the existing log; starting empty", "err", err)
	}
	histLog.SetPersist(histPath, 5*time.Second, time.Now, func(err error) {
		app.NotifyHistoryFlush(gui, err)
	})
	app.SetHistory(gui, histLog)
```

Drive `Tick` from a goroutine on a 1s ticker (the debounce is 5s, so a 1s
tick resolves it within a second of the window closing), stopped on
shutdown, and add `histLog.Flush()` to `ServiceShutdown` beside
`flushConfig()`.

Seed the builtins once, after `SetSettingsBackend`:

```go
	gui.SeedBuiltinProfiles()
```

Wire `OnRX` into `voiceDialOptions` in `internal/app/voice.go`:

```go
		OnRX:          a.onVoiceRX,
		HistoryIdleMS: cfgVoice.HistoryIdleMS,
```

reading `HistoryIdleMS` from the same `sb.cfg.Voice` snapshot that method
already takes.

- [ ] **Step 3: Full verification**

```bash
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go test -race -tags purego ./...   # internal/voice needs dangerouslyDisableSandbox
(cd frontend && npx vitest run)
(cd frontend && npx tsc --noEmit)
(cd frontend && npm run build)
```
Expected: all green. Record the actual output; do not claim a pass you have not seen (`superpowers:verification-before-completion`).

- [ ] **Step 4: Write the manual verification checklist**

Create `docs/superpowers/plans/2026-09-30-phase-7-3-manual-verification.md`.
It is the **eighth** unrun checklist and must cover, each with concrete
steps and an expected observation:

1. **Measure the 500ms RX idle threshold.** Two clients, one talking in
   natural sentences. Sweep `[voice] history_idle_ms` across 200 / 350 / 500
   / 800 / 1200 and record, for each, how many rows one paragraph of speech
   produces. Report the value at which one paragraph is one row and two
   exchanges are two rows. **This is the phase's single most important
   unknown** — say so at the top of the file.
2. Native dialogs on each OS: BROWSE (folder picker), IMPORT, EXPORT, OPEN
   (reveal). Cancel each one and confirm nothing is notified.
3. Builtin seeding: fresh profile dir → written; edit one → survives a
   restart byte-identically; delete one → stays deleted across two restarts.
4. Resize and reorder blocks, quit, relaunch: layout restored. Kill the
   process instead of quitting: layout since the last config write is lost,
   which is expected.
5. Load a profile whose radios differ from the live set and confirm the PTT
   still transmits (selection re-validation).
6. Confirm a radio-select does **not** light the dirty dot and a block
   resize does.
7. RX rows from a second client instance, with the callsign resolved.
8. CSV export opens correctly in a spreadsheet, including a sender
   containing a comma.

- [ ] **Step 5: Commit and open the PR**

```bash
git add main.go main_wiring_test.go internal/ docs/
git commit -m "feat: wire the transmission log and builtin profile seeding

Completes Phase 7.3. The manual checklist is the eighth to sit unrun,
joining Phases 3, 3.5, 4, 5, 6, 7.1 and 7.2; its first item is measuring
the 500ms RX idle threshold, which is the phase's single most important
unknown."
```

Then update `docs/ROADMAP.md`'s Phase 7.3 row to `[x]` with the honest
verification status, and open the PR with the body ending in:

```
🤖 Generated with [Claude Code](https://claude.com/claude-code)
```
