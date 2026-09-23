package keybinds

import (
	"errors"
	"fmt"
	"log/slog"
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
//
// An action holds AT MOST ONE keyboard trigger (see Add's doc comment for
// why). Load is the boundary where a hand-edited or version-skewed
// config.toml enters, so it enforces the same invariant defensively: only
// the FIRST KindKey trigger in an action's list survives, in the same
// per-entry-drop spirit as an unparseable trigger. The file's order is the
// user's order, so keeping the first is stable and predictable.
// Joystick triggers are unaffected -- there is no limit on those.
//
// The drop is NOT silent. It is destructive on the next write: the store is
// snapshotted back out on every Save, so a hand-edited
// `"global.push_to_mute" = ["V", "Ctrl+B"]` is rewritten as
// `"global.push_to_mute" = "V"` and the user's second chord is gone from
// their file for good. Dropping it still beats keeping a binding that is
// silently dead at dispatch (internal/hotkeys registers one chord per action
// ID), but the destruction has to be diagnosable, so each dropped chord gets
// a Warn naming the action and the chord.
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
		keptKey := "" // the one surviving chord, for the drop warning below
		for _, str := range list {
			t, err := trigger.Parse(str)
			if err != nil {
				continue // malformed trigger: drop this entry, keep the rest
			}
			if t.Kind == trigger.KindKey {
				if keptKey != "" {
					// At most one keyboard trigger per action: drop extras,
					// first wins. Logged because the next Save erases it
					// from the user's config.toml. keptKey rather than
					// out[0] -- a joystick trigger listed before the chord
					// would otherwise be reported as the survivor.
					slog.Default().Warn("keybind config has more than one keyboard chord for an action; "+
						"only the first is kept and the rest are dropped from the file on the next save",
						"action", id, "kept", keptKey, "dropped", t.String())
					continue
				}
				keptKey = t.String()
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
