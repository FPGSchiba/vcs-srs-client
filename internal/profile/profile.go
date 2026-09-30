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
