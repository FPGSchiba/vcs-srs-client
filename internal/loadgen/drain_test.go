package loadgen

import (
	"io"
	"testing"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestDrainCountsByType(t *testing.T) {
	script := []*srspb.ServerUpdate{
		{Type: srspb.ServerUpdate_CLIENT_JOINED},
		{Type: srspb.ServerUpdate_CLIENT_JOINED},
		{Type: srspb.ServerUpdate_CLIENT_JOINED},
		{Type: srspb.ServerUpdate_CLIENT_RADIO_UPDATE},
		{Type: srspb.ServerUpdate_CLIENT_RADIO_UPDATE},
	}
	i := 0
	recv := func() (*srspb.ServerUpdate, error) {
		if i >= len(script) {
			return nil, io.EOF
		}
		u := script[i]
		i++
		return u, nil
	}
	var c UpdateCounts
	if err := Drain(recv, &c); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if got := c.Get(srspb.ServerUpdate_CLIENT_JOINED); got != 3 {
		t.Fatalf("joined = %d, want 3", got)
	}
	if got := c.Get(srspb.ServerUpdate_CLIENT_RADIO_UPDATE); got != 2 {
		t.Fatalf("radio = %d, want 2", got)
	}
	if c.Total() != 5 {
		t.Fatalf("total = %d, want 5", c.Total())
	}
}

func TestDrainOutOfRangeTypeCountsOther(t *testing.T) {
	n := 0
	recv := func() (*srspb.ServerUpdate, error) {
		if n > 0 {
			return nil, io.EOF
		}
		n++
		return &srspb.ServerUpdate{Type: srspb.ServerUpdate_UpdateType(99)}, nil
	}
	var c UpdateCounts
	if err := Drain(recv, &c); err != nil {
		t.Fatal(err)
	}
	if c.Other.Load() != 1 || c.Total() != 1 {
		t.Fatalf("other=%d total=%d, want 1 and 1", c.Other.Load(), c.Total())
	}
}

func TestDrainReturnsError(t *testing.T) {
	want := io.ErrUnexpectedEOF
	var c UpdateCounts
	if err := Drain(func() (*srspb.ServerUpdate, error) { return nil, want }, &c); err != want {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
