package notify

import (
	"testing"
	"time"
)

func TestNewStartsEmpty(t *testing.T) {
	n := New(Options{})
	got := n.Snapshot()
	if len(got.Items) != 0 {
		t.Fatalf("Items = %d, want 0", len(got.Items))
	}
	if got.Unread != 0 {
		t.Fatalf("Unread = %d, want 0", got.Unread)
	}
	if got.Items == nil {
		t.Fatal("Items is nil; must be a non-nil empty slice so it marshals to [] not null")
	}
}

func TestNewAppliesDefaults(t *testing.T) {
	n := New(Options{})
	if n.opt.Cap != DefaultCap {
		t.Fatalf("Cap = %d, want %d", n.opt.Cap, DefaultCap)
	}
	if n.opt.Now == nil {
		t.Fatal("Now must default to time.Now")
	}
}

func TestNewKeepsInjectedClock(t *testing.T) {
	fixed := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	n := New(Options{Now: func() time.Time { return fixed }})
	if got := n.opt.Now(); !got.Equal(fixed) {
		t.Fatalf("Now() = %v, want %v", got, fixed)
	}
}
