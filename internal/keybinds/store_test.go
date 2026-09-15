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
