package state_test

import (
	"sync"
	"testing"

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
