package voice

import (
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// rxHistoryHarness drives rxService against a caller-controlled clock with a
// bare Session: rx initialised (with the stub decoder factory from
// rx_test.go), no socket, no goroutines. rxVoice, rxService and
// endOpenRXStreams are pure with respect to the network, so the history
// branch can be driven deterministically without binding anything.
//
// Delivery is drained SYNCHRONOUSLY by got(), which reads the same channel
// rxDeliverLoop would. That keeps the detection assertions free of sleeps;
// the real delivery goroutine is covered separately below.
type rxHistoryHarness struct {
	s      *Session
	mu     sync.Mutex
	events []RXEvent
}

func (h *rxHistoryHarness) got() []RXEvent {
	for {
		select {
		case ev := <-h.s.rxEvents:
			h.s.onRX(ev)
		default:
			h.mu.Lock()
			defer h.mu.Unlock()
			out := make([]RXEvent, len(h.events))
			copy(out, h.events)
			return out
		}
	}
}

func newRXHistoryHarness(t *testing.T, now func() time.Time, idle time.Duration) *rxHistoryHarness {
	t.Helper()
	h := &rxHistoryHarness{}
	dec := &decoderFactory{}
	h.s = &Session{
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		self:        uuid.New(),
		clock:       now,
		historyIdle: idle,
		rxEvents:    make(chan RXEvent, 64),
		onRX: func(ev RXEvent) {
			h.mu.Lock()
			h.events = append(h.events, ev)
			h.mu.Unlock()
		},
	}
	h.s.rx.init(dec.new, defaultJitterMS, 500)
	h.s.SetRXContext([]KHz{118500}, nil, nil)
	t.Cleanup(h.s.rx.close)
	return h
}

func TestRXHistoryEmitsAfterIdleThreshold(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)

	sender := uuid.New()
	// Two packets 20ms apart, then silence.
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)
	cur = cur.Add(20 * time.Millisecond)
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 2, Payload: []byte("aaaaaaaa")}, cur)

	cur = cur.Add(300 * time.Millisecond)
	h.s.rxService(cur)
	if len(h.got()) != 0 {
		t.Fatalf("emitted before the 500ms threshold: %+v", h.got())
	}

	cur = cur.Add(300 * time.Millisecond) // 600ms since the last packet
	h.s.rxService(cur)
	ev := h.got()
	if len(ev) != 1 {
		t.Fatalf("events = %+v, want exactly 1", ev)
	}
	if ev[0].Sender != sender || ev[0].Freq != 118500 {
		t.Fatalf("event = %+v", ev[0])
	}
	if !ev[0].Start.Equal(base) {
		t.Fatalf("Start = %v, want the FIRST packet's time %v", ev[0].Start, base)
	}
	if !ev[0].End.Equal(base.Add(20 * time.Millisecond)) {
		t.Fatalf("End = %v, want the LAST packet's time, not the detection time", ev[0].End)
	}
}

func TestRXHistoryEmitsOncePerTransmission(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	// Sweep repeatedly past the threshold but before the 5s reap.
	for i := 0; i < 5; i++ {
		cur = cur.Add(600 * time.Millisecond)
		h.s.rxService(cur)
	}
	if got := h.got(); len(got) != 1 {
		t.Fatalf("events = %d, want exactly 1 -- the ended flag must suppress re-emission until a new packet arrives", len(got))
	}
}

func TestRXHistoryRestartsAfterEnd(t *testing.T) {
	// Two sentences 600ms apart, inside the 5s reap window, are TWO rows.
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()

	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)
	cur = cur.Add(600 * time.Millisecond)
	h.s.rxService(cur) // emits #1

	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 2, Payload: []byte("aaaaaaaa")}, cur)
	secondStart := cur
	cur = cur.Add(600 * time.Millisecond)
	h.s.rxService(cur) // emits #2

	got := h.got()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2 -- a packet on an ended stream starts a NEW transmission", len(got))
	}
	if !got[1].Start.Equal(secondStart) {
		t.Fatalf("second Start = %v, want %v", got[1].Start, secondStart)
	}
}

func TestRXHistoryDoesNotChangeReapTiming(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: uuid.New(), Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	cur = cur.Add(4 * time.Second)
	h.s.rxService(cur)
	if h.s.RXStats().Active != 1 {
		t.Fatal("history detection must not reap early: rxIdleTimeout is still 5s")
	}
	cur = cur.Add(2 * time.Second)
	h.s.rxService(cur)
	if h.s.RXStats().Active != 0 {
		t.Fatal("stream should be reaped at 5s idle, unchanged")
	}
}

func TestRXHistoryEndsOpenStreamsOnClose(t *testing.T) {
	// A transmission in flight during a reconnect must produce one truthful
	// row rather than vanishing.
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	sender := uuid.New()
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: sender, Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)

	// The clock has moved on; End must still be lastSeen, not "now".
	cur = cur.Add(200 * time.Millisecond)
	h.s.endOpenRXStreams()

	got := h.got()
	if len(got) != 1 {
		t.Fatalf("events = %+v, want the in-flight stream ended", got)
	}
	if !got[0].End.Equal(base) {
		t.Fatalf("End = %v, want lastSeen %v", got[0].End, base)
	}

	// Already-ended streams are not reported twice.
	h.s.endOpenRXStreams()
	if got := h.got(); len(got) != 1 {
		t.Fatalf("events = %d after a second call, want still 1", len(got))
	}
}

func TestRXHistoryDoesNotDoubleReportAnEndedStreamOnClose(t *testing.T) {
	base := time.Date(2026, 9, 30, 21, 0, 0, 0, time.UTC)
	cur := base
	h := newRXHistoryHarness(t, func() time.Time { return cur }, 500*time.Millisecond)
	h.s.rxVoice(&Packet{Type: PacketTypeVoice, SenderID: uuid.New(), Frequency: 118500, Sequence: 1, Payload: []byte("aaaaaaaa")}, cur)
	cur = cur.Add(600 * time.Millisecond)
	h.s.rxService(cur)
	h.s.endOpenRXStreams()
	if got := h.got(); len(got) != 1 {
		t.Fatalf("events = %d, want 1", len(got))
	}
}

func TestRXHistoryQueueNeverBlocksAndCountsDrops(t *testing.T) {
	h := newRXHistoryHarness(t, time.Now, 500*time.Millisecond)
	h.s.rxEvents = make(chan RXEvent, 2) // and nobody is consuming
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			h.s.queueRXEvent(RXEvent{Sender: uuid.New(), Freq: 118500})
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("queueRXEvent blocked on a full queue; the decode goroutine would stall behind a slow consumer")
	}
	if got := h.s.RXStats().HistoryDropped; got != 3 {
		t.Fatalf("HistoryDropped = %d, want 3", got)
	}
}

func TestRXHistoryDeliversThroughDialAndFlushesOnClose(t *testing.T) {
	var mu sync.Mutex
	var got []RXEvent
	f := dialRX(t, func(o *Options) {
		o.OnRX = func(ev RXEvent) {
			mu.Lock()
			got = append(got, ev)
			mu.Unlock()
		}
	})
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)
	f.send(t, f.peer, 0x010200, freqAlpha, markerA)

	// The stream is still open: only Close's endOpenRXStreams can report it,
	// and the delivery goroutine must flush it before Close returns.
	if err := f.s.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("delivered %d events, want 1 flushed by Close", len(got))
	}
	if got[0].Sender != f.peer || got[0].Freq != freqAlpha {
		t.Fatalf("event = %+v", got[0])
	}
}

func TestHistoryIdleDefault(t *testing.T) {
	if got := historyIdleFor(0); got != defaultHistoryIdleMS*time.Millisecond {
		t.Fatalf("historyIdleFor(0) = %v, want the %dms default", got, defaultHistoryIdleMS)
	}
	if got := historyIdleFor(250); got != 250*time.Millisecond {
		t.Fatalf("historyIdleFor(250) = %v", got)
	}
	if got := historyIdleFor(-5); got != defaultHistoryIdleMS*time.Millisecond {
		t.Fatalf("a negative override must fall back to the default, got %v", got)
	}
}
