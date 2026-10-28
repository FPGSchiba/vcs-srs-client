package loadgen

import (
	"errors"
	"io"
	"sync/atomic"

	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// updateTypes is the number of ServerUpdate_UpdateType values counted by slot
// (UNKNOWN through VOICE_ADDRESS_UPDATE).
const updateTypes = 8

// UpdateCounts counts received ServerUpdates by type. Types outside the
// counted range go to Other. It is safe for concurrent use.
type UpdateCounts struct {
	byType [updateTypes]atomic.Uint64
	Other  atomic.Uint64
}

func (c *UpdateCounts) add(t srspb.ServerUpdate_UpdateType) {
	if t < 0 || int(t) >= updateTypes {
		c.Other.Add(1)
		return
	}
	c.byType[t].Add(1)
}

// Get returns the count for one update type (0 for an uncounted type).
func (c *UpdateCounts) Get(t srspb.ServerUpdate_UpdateType) uint64 {
	if t < 0 || int(t) >= updateTypes {
		return 0
	}
	return c.byType[t].Load()
}

// Total returns every update seen, including Other.
func (c *UpdateCounts) Total() uint64 {
	n := c.Other.Load()
	for i := range c.byType {
		n += c.byType[i].Load()
	}
	return n
}

// Drain reads updates until the stream ends, counting each by type and
// discarding it. It keeps no state.Store and routes nothing (design 4.2): the
// point is to be a subscriber the server must fan out to, at the lowest
// generator cost. io.EOF returns nil; any other error is returned as is.
func Drain(recv func() (*srspb.ServerUpdate, error), c *UpdateCounts) error {
	for {
		u, err := recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		c.add(u.GetType())
	}
}
