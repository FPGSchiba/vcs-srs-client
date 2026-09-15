package state_test

import (
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestStore_UpdateAndGetClient(t *testing.T) {
	s := state.New()
	s.UpdateClient("guid-1", &srspb.ClientInfo{Name: "Alice", Coalition: "VG", UnitId: "DSC", RoleId: 1})

	got, ok := s.Client("guid-1")
	if !ok {
		t.Fatal("expected Client(guid-1) to be present")
	}
	if got.Name != "Alice" {
		t.Fatalf("expected Alice, got %q", got.Name)
	}
}

func TestStore_RemoveClient(t *testing.T) {
	s := state.New()
	s.UpdateClient("g", &srspb.ClientInfo{Name: "A"})
	s.RemoveClient("g")
	if _, ok := s.Client("g"); ok {
		t.Fatal("expected client to be gone after RemoveClient")
	}
}

func TestStore_Snapshot_IsImmutableCopy(t *testing.T) {
	s := state.New()
	s.UpdateClient("g", &srspb.ClientInfo{Name: "A"})

	snap := s.Snapshot()
	if len(snap.Clients) != 1 {
		t.Fatalf("expected 1 client in snapshot, got %d", len(snap.Clients))
	}
	delete(snap.Clients, "g")
	if _, ok := s.Client("g"); !ok {
		t.Fatal("mutating snapshot affected the store")
	}
}

func TestStore_ConcurrentWritesAreSafe(t *testing.T) {
	s := state.New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.UpdateClient("g", &srspb.ClientInfo{Name: "X"})
		}()
	}
	wg.Wait()
	if _, ok := s.Client("g"); !ok {
		t.Fatal("expected client to be present after concurrent writes")
	}
}

func TestStore_SetAndGetRadios(t *testing.T) {
	s := state.New()
	s.SetRadios("g", &srspb.RadioInfo{
		Radios: []*srspb.Radio{{Id: 1, Name: "Fleet", Frequency: 118.5, Enabled: true}},
	})
	r, ok := s.Radios("g")
	if !ok {
		t.Fatal("expected radios present")
	}
	if len(r.Radios) != 1 || r.Radios[0].Name != "Fleet" {
		t.Fatalf("unexpected radios: %+v", r)
	}
}

func TestStore_SetSelfAppearsInSnapshot(t *testing.T) {
	s := state.New()
	s.SetSelf("guid-self", &srspb.ClientInfo{Name: "Spacer", Coalition: "VG", UnitId: "VG-0271-DSC"})

	snap := s.Snapshot()
	if snap.SelfGUID != "guid-self" {
		t.Fatalf("expected self guid, got %q", snap.SelfGUID)
	}
	if snap.Self == nil || snap.Self.GetName() != "Spacer" || snap.Self.GetUnitId() != "VG-0271-DSC" {
		t.Fatalf("unexpected self: %+v", snap.Self)
	}

	s.ClearSelf()
	if snap2 := s.Snapshot(); snap2.SelfGUID != "" || snap2.Self != nil {
		t.Fatalf("expected self cleared, got guid=%q self=%+v", snap2.SelfGUID, snap2.Self)
	}
}

// TestStore_OnRadiosChangedFiresForEveryRadioMutation pins the observer
// contract the per-radio keybind refresh depends on: the local radio set
// becomes knowable across several distinct mutations (SyncClient fills radios
// BEFORE SetSelf records which GUID is ours), so every one of them must
// notify.
func TestStore_OnRadiosChangedFiresForEveryRadioMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*state.Store)
	}{
		{"SetRadios", func(s *state.Store) { s.SetRadios("g", &srspb.RadioInfo{}) }},
		{"SetSelf", func(s *state.Store) { s.SetSelf("g", &srspb.ClientInfo{}) }},
		{"ClearSelf", func(s *state.Store) { s.ClearSelf() }},
		{"RemoveClient", func(s *state.Store) { s.RemoveClient("g") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := state.New()
			calls := 0
			s.OnRadiosChanged(func() { calls++ })
			tt.mutate(s)
			if calls != 1 {
				t.Errorf("%s notified %d observers, want 1", tt.name, calls)
			}
		})
	}
}

// TestStore_ObserverCanReadTheStore is the deadlock guard: observers run with
// the store lock RELEASED, so an observer is free to call Snapshot -- which is
// exactly what the per-radio keybind refresh does.
func TestStore_ObserverCanReadTheStore(t *testing.T) {
	s := state.New()
	var seen int
	s.OnRadiosChanged(func() { seen = len(s.Snapshot().Radios) })

	done := make(chan struct{})
	go func() {
		s.SetRadios("g", &srspb.RadioInfo{})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("observer deadlocked against the store lock")
	}
	if seen != 1 {
		t.Errorf("observer saw %d radios, want 1", seen)
	}
}
