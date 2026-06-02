// Package state is the in-memory hub for live client/radio/settings state.
// Mutations go through Store methods; reads via Snapshot or per-key accessors.
package state

import (
	"sync"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// Snapshot is an immutable copy of the store's contents at a moment in time.
type Snapshot struct {
	Clients  map[string]*srspb.ClientInfo
	Radios   map[string]*srspb.RadioInfo
	Settings *srspb.ServerSettings
	SelfGUID string
	Self     *srspb.ClientInfo
}

// Store holds live client/radio/settings state. Safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	clients  map[string]*srspb.ClientInfo
	radios   map[string]*srspb.RadioInfo
	settings *srspb.ServerSettings
	selfGUID string
	self     *srspb.ClientInfo
}

// New constructs an empty Store.
func New() *Store {
	return &Store{
		clients: map[string]*srspb.ClientInfo{},
		radios:  map[string]*srspb.RadioInfo{},
	}
}

// UpdateClient inserts or overwrites the client's info.
func (s *Store) UpdateClient(guid string, info *srspb.ClientInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[guid] = info
}

// RemoveClient drops a client (and its radios) from the store.
func (s *Store) RemoveClient(guid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, guid)
	delete(s.radios, guid)
}

// Client returns the client info for guid, or false if absent.
func (s *Store) Client(guid string) (*srspb.ClientInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clients[guid]
	return c, ok
}

// SetRadios replaces the radio info for a client.
func (s *Store) SetRadios(guid string, info *srspb.RadioInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.radios[guid] = info
}

// Radios returns the radio info for guid, or false if absent.
func (s *Store) Radios(guid string) (*srspb.RadioInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.radios[guid]
	return r, ok
}

// SetSelf records the local client's own guid and info (set at connect).
func (s *Store) SetSelf(guid string, info *srspb.ClientInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selfGUID = guid
	s.self = info
}

// ClearSelf clears the local client identity (on disconnect).
func (s *Store) ClearSelf() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selfGUID = ""
	s.self = nil
}

// SetSettings overwrites the server settings.
func (s *Store) SetSettings(settings *srspb.ServerSettings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
}

// Settings returns the current server settings, or nil if unset.
func (s *Store) Settings() *srspb.ServerSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

// Snapshot returns a map-isolated copy of the store. Mutating the returned maps
// will not affect the store; the message pointers inside must be treated as
// read-only by callers.
func (s *Store) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clients := make(map[string]*srspb.ClientInfo, len(s.clients))
	for k, v := range s.clients {
		clients[k] = v
	}
	radios := make(map[string]*srspb.RadioInfo, len(s.radios))
	for k, v := range s.radios {
		radios[k] = v
	}
	return Snapshot{Clients: clients, Radios: radios, Settings: s.settings, SelfGUID: s.selfGUID, Self: s.self}
}
