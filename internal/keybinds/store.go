package keybinds

import (
	"strings"
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
	if len(id) < len("radio.0.ptt") {
		return false
	}
	if !strings.HasPrefix(id, "radio.") {
		return false
	}
	return strings.HasSuffix(id, ".ptt") || strings.HasSuffix(id, ".select")
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
