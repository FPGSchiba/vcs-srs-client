package loadgen

import "testing"

func TestRxStampHealthKeepsDropsWhenCmsgAbsent(t *testing.T) {
	var h RxStampHealth
	h.Observe(Stamp{Stamped: true, DropsSeen: true, Drops: 7})
	h.Observe(Stamp{Truncated: true})
	h.Observe(Stamp{ParseErr: true})
	if h.Drops != 7 {
		t.Fatalf("Drops=%d, want 7 (absent cmsg must not reset)", h.Drops)
	}
	if h.Reads != 3 || h.Unstamped != 2 || h.Truncated != 1 || h.ParseErrs != 1 {
		t.Fatalf("%+v", h)
	}
	h.Observe(Stamp{Stamped: true, DropsSeen: true, Drops: 9})
	if h.Drops != 9 {
		t.Fatalf("Drops=%d, want 9", h.Drops)
	}
}
