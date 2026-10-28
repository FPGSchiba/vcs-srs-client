package control

import (
	"context"
	"io"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

// ErrHeldOverflow ends a held stream that received more updates than it may
// hold before Release. It carries codes.ResourceExhausted, the status of the
// server's own fell-behind end (SV2 C13), so the session treats the two
// alike: resubscribe and SyncClient.
var ErrHeldOverflow = status.Error(codes.ResourceExhausted,
	"update stream fell behind while the snapshot was loading: resubscribe and call SyncClient")

// Held consumes an update stream opened AHEAD of a SyncClient snapshot.
// Until Release, every update it receives is held, in order, instead of
// being routed: routing it would put it in the store before the snapshot,
// and the snapshot's ResetRoster would then wipe it. Release replays the
// held updates on top of the applied snapshot and switches to routing live.
//
// Every update the stream carries is a full-value set or a delete of one
// key (see route), so replaying one the snapshot already reflects leaves the
// store as it was, and replaying them in order converges on the server's
// state.
type Held struct {
	st     *state.Store
	tagged *events.Tagged
	max    int

	mu       sync.Mutex
	buf      []*srspb.ServerUpdate
	released bool
	done     chan struct{} // closed by Release
}

// NewHeld returns a Held that routes into st and em and holds at most max
// updates before Release.
func NewHeld(st *state.Store, em events.Emitter, max int) *Held {
	return &Held{st: st, tagged: events.New(em), max: max, done: make(chan struct{})}
}

// Consume reads stream until it ends. Before Release it only holds; an end
// of the stream before Release (including ErrHeldOverflow, after which it
// stops reading) is kept and returned only once Release has replayed what
// was held, so whoever handles the end sees the snapshot applied first. It
// returns ctx's error if ctx is done before Release.
//
// After Release it is Consume: nil when the server ends the stream cleanly,
// the stream's error otherwise.
func (h *Held) Consume(ctx context.Context, stream srspb.SRSService_SubscribeToUpdatesClient) error {
	for {
		upd, err := stream.Recv()
		if err == io.EOF {
			err = nil
		}
		h.mu.Lock()
		if !h.released {
			end := err != nil || upd == nil
			if !end && len(h.buf) >= h.max {
				end, err = true, ErrHeldOverflow
			}
			if end {
				h.mu.Unlock()
				return h.waitRelease(ctx, err)
			}
			h.buf = append(h.buf, upd)
			h.mu.Unlock()
			continue
		}
		h.mu.Unlock()
		if err != nil || upd == nil {
			return err
		}
		route(upd, h.st, h.tagged)
	}
}

// waitRelease returns err once Release has run, or ctx's error first.
func (h *Held) waitRelease(ctx context.Context, err error) error {
	select {
	case <-h.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release replays the held updates in arrival order, on the caller's
// goroutine, and from then on Consume routes each update as it arrives. The
// caller applies the snapshot first. Only the first call does anything.
//
// The replay runs under the lock Consume takes before deciding to hold or
// route, so no live update can overtake a held one.
func (h *Held) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return
	}
	for _, u := range h.buf {
		route(u, h.st, h.tagged)
	}
	h.buf = nil
	h.released = true
	close(h.done)
}
