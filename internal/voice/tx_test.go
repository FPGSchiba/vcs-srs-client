package voice

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/audio/opus"
)

// A Session is the Phase 5 network sink the Phase 4 pipeline writes into.
// The assertion lives in the test rather than in tx.go so that package voice
// does not take a production dependency on package audio.
var _ audio.Sink = (*Session)(nil)

// dspFrameSamples is Phase 4's 10 ms capture frame at 48 kHz, written out
// rather than read from package audio: the point of these tests is that two
// of THESE make one 20 ms Opus frame, and deriving the number from the
// pipeline would make the assertion true by construction.
const dspFrameSamples = 480

// Two transmit frequencies, chosen asymmetric on the wire so a byte-order
// defect in the 24-bit frequency field cannot survive:
//
//	251300 kHz -> 03 D5 A4
//	124800 kHz -> 01 E7 80
//
// Neither is a palindrome, and neither is the reverse of the other.
const (
	freqAlpha = KHz(251300)
	freqBravo = KHz(124800)
)

// errStubEncode is returned by injected encoders once they are released.
var errStubEncode = errors.New("tx test: stub encoder stopped")

func TestTXFixtureConstantsMatchThePipeline(t *testing.T) {
	if audio.FrameSamples != dspFrameSamples {
		t.Fatalf("audio.FrameSamples = %d, but these tests are written for %d",
			audio.FrameSamples, dspFrameSamples)
	}
	if opus.FrameSamples != 2*dspFrameSamples {
		t.Fatalf("opus.FrameSamples = %d, want exactly two %d-sample DSP frames",
			opus.FrameSamples, dspFrameSamples)
	}
}

// sineFrame builds one 480-sample DSP frame of a 1 kHz tone, continuing the
// phase from frame index n so consecutive frames splice cleanly.
//
// Real signal, not zeros, on purpose: a bit-exact digital-silence frame
// encodes to 3 bytes even with DTX off, and the server discards any voice
// payload of 5 bytes or fewer. A silent fixture would still exercise the
// code but would not resemble anything that survives a real relay.
func sineFrame(n int) []float32 {
	f := make([]float32, dspFrameSamples)
	for i := range f {
		t := float64(n*dspFrameSamples+i) / 48000.0
		f[i] = 0.5 * float32(math.Sin(2*math.Pi*1000*t))
	}
	return f
}

// requireOpus skips a test that needs the real codec. A CGO_ENABLED=0 build
// has no encoder at all: Dial still succeeds and the session still keeps its
// binding alive, it just cannot transmit. That degradation is deliberate
// (see txState.init), so these tests report "not applicable" rather than
// "broken". Tests driving an injected encoder do NOT call this -- they
// exercise the transmit path on any build.
func requireOpus(t *testing.T) {
	t.Helper()
	if !opus.Available() {
		t.Skip("no Opus codec in this build (CGO_ENABLED=0): transmit is disabled by design")
	}
}

// dialConnectedTX brings up a session against the fixture server and waits
// for the handshake, which is the precondition for every TX assertion.
func dialConnectedTX(t *testing.T, mutate func(*Options)) (*testServer, *Session) {
	t.Helper()
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()
	opt := testOptions(clk, rec)
	if mutate != nil {
		mutate(&opt)
	}
	s := dialTestSession(t, ts, goodSecret, opt)
	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })
	return ts, s
}

// waitVoice waits for at least n VOICE datagrams, then settles and requires
// that EXACTLY n arrived, so an over-count is a failure rather than a pass.
func waitVoice(t *testing.T, ts *testServer, n int) []*Packet {
	t.Helper()
	waitFor(t, "voice datagrams", func() bool { return ts.voiceCount() >= n })
	settle()
	got := ts.voicePackets()
	if len(got) != n {
		t.Fatalf("voice datagram count = %d, want exactly %d", len(got), n)
	}
	return got
}

// byFreq groups datagrams by their frequency field, preserving arrival order
// within each group. Tests assert per-stream, because UDP does not promise
// the interleaving between two streams.
func byFreq(pkts []*Packet) map[KHz][]*Packet {
	out := map[KHz][]*Packet{}
	for _, p := range pkts {
		out[p.Frequency] = append(out[p.Frequency], p)
	}
	return out
}

func TestTXTwoDSPFramesMakeOnePacket(t *testing.T) {
	requireOpus(t)
	ts, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})

	s.WriteFrame(sineFrame(0))
	settle()
	if got := ts.voiceCount(); got != 0 {
		t.Fatalf("after ONE 10 ms frame: %d datagrams, want 0 (20 ms of audio makes a packet)", got)
	}

	s.WriteFrame(sineFrame(1))
	pkts := waitVoice(t, ts, 1)

	p := pkts[0]
	if p.Type != PacketTypeVoice {
		t.Fatalf("packet type = %v, want VOICE", p.Type)
	}
	if p.Frequency != freqAlpha {
		t.Fatalf("frequency = %d, want %d", p.Frequency, freqAlpha)
	}
	if p.SenderID != s.self {
		t.Fatalf("sender = %v, want %v", p.SenderID, s.self)
	}
	if len(p.Payload) <= MinVoicePayload {
		t.Fatalf("payload is %d bytes; the server discards anything at or below %d",
			len(p.Payload), MinVoicePayload)
	}
	if got := s.TXStats().Sent; got != 1 {
		t.Fatalf("TXStats().Sent = %d, want 1", got)
	}
}

func TestTXOnePacketPerTargetFrequency(t *testing.T) {
	requireOpus(t)
	ts, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}, {Freq: freqBravo}})

	s.WriteFrame(sineFrame(0))
	s.WriteFrame(sineFrame(1))

	pkts := waitVoice(t, ts, 2)
	groups := byFreq(pkts)
	if len(groups) != 2 {
		t.Fatalf("saw %d distinct frequencies, want 2", len(groups))
	}
	a, b := groups[freqAlpha], groups[freqBravo]
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("per-frequency counts: %d on %d, %d on %d; want 1 each",
			len(a), freqAlpha, len(b), freqBravo)
	}
	// One encode, two datagrams: the payload must be byte-identical.
	if string(a[0].Payload) != string(b[0].Payload) {
		t.Fatalf("the two frequencies carry different payloads (%d vs %d bytes); "+
			"the frame must be encoded once and emitted per frequency",
			len(a[0].Payload), len(b[0].Payload))
	}
	if got := s.TXStats().Sent; got != 2 {
		t.Fatalf("TXStats().Sent = %d, want 2", got)
	}
}

func TestTXPerFrequencySequenceCounters(t *testing.T) {
	requireOpus(t)
	ts, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}, {Freq: freqBravo}})

	// Six 10 ms frames = three 20 ms packets per frequency.
	for i := 0; i < 6; i++ {
		s.WriteFrame(sineFrame(i))
	}
	pkts := waitVoice(t, ts, 6)
	groups := byFreq(pkts)

	// Written out by hand. A shared counter would yield 0,2,4 on each
	// frequency; deriving these from the implementation's counter would make
	// that bug invisible.
	want := []uint32{0, 1, 2}
	for _, freq := range []KHz{freqAlpha, freqBravo} {
		got := groups[freq]
		if len(got) != 3 {
			t.Fatalf("frequency %d got %d packets, want 3", freq, len(got))
		}
		for i, p := range got {
			if p.Sequence != want[i] {
				var seqs []uint32
				for _, q := range got {
					seqs = append(seqs, q.Sequence)
				}
				t.Fatalf("frequency %d sequences = %v, want %v "+
					"(each frequency counts independently; receivers key jitter "+
					"buffers by (sender, frequency))", freq, seqs, want)
			}
		}
	}
}

func TestTXFlagsPTTAlwaysAndIntercomPerTarget(t *testing.T) {
	requireOpus(t)
	ts, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{
		{Freq: freqAlpha, Intercom: true},
		{Freq: freqBravo, Intercom: false},
	})

	s.WriteFrame(sineFrame(0))
	s.WriteFrame(sineFrame(1))

	groups := byFreq(waitVoice(t, ts, 2))
	if len(groups[freqAlpha]) != 1 || len(groups[freqBravo]) != 1 {
		t.Fatalf("want one packet per frequency, got %d and %d",
			len(groups[freqAlpha]), len(groups[freqBravo]))
	}
	alpha, bravo := groups[freqAlpha][0], groups[freqBravo][0]

	if !alpha.PTT() || !bravo.PTT() {
		t.Fatalf("PTT flags: alpha=%v bravo=%v; every transmitted frame is a held press",
			alpha.PTT(), bravo.PTT())
	}
	if !alpha.Intercom() {
		t.Fatalf("intercom target did not carry the intercom flag (flags=0x%02x)", alpha.Flags)
	}
	if bravo.Intercom() {
		t.Fatalf("non-intercom target carried the intercom flag (flags=0x%02x)", bravo.Flags)
	}
}

func TestTXEndTransmissionDiscardsPartialFrame(t *testing.T) {
	requireOpus(t)
	ts, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})

	// Half a 20 ms frame, then the gate closes.
	s.WriteFrame(sineFrame(0))
	s.EndTransmission()

	// The next press supplies a full 20 ms of its own.
	s.WriteFrame(sineFrame(10))
	s.WriteFrame(sineFrame(11))

	// Exactly one datagram: the orphaned half-frame was discarded, not glued
	// onto the front of this transmission. (Asserting on the datagram count
	// rather than decoded samples -- Opus is lossy, so comparing PCM would be
	// an assertion about the codec, not about the accumulator.)
	pkts := waitVoice(t, ts, 1)
	if pkts[0].Sequence != 0 {
		t.Fatalf("sequence = %d, want 0", pkts[0].Sequence)
	}

	// And a third frame after the reset must NOT immediately complete a
	// packet: the accumulator started empty at the reset, so it is now
	// half-full again, not full.
	s.WriteFrame(sineFrame(12))
	settle()
	if got := ts.voiceCount(); got != 1 {
		t.Fatalf("after one more 10 ms frame: %d datagrams, want still 1", got)
	}
}

func TestTXWriteFrameNeverBlocks(t *testing.T) {
	block := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }

	ts, s := dialConnectedTX(t, func(o *Options) {
		o.txEncode = func([]float32, []byte) (int, error) {
			<-block
			return 0, errStubEncode
		}
	})
	// Registered after dialTestSession's Close, so cleanup (LIFO) releases
	// the parked encoder before Close joins txLoop.
	t.Cleanup(release)

	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})

	// txLoop is parked inside the stub encoder, so nothing drains txPCM.
	// Push until the queue is provably full.
	frame := sineFrame(0)
	for i := 0; i < 400 && s.TXStats().DroppedFull == 0; i++ {
		s.WriteFrame(frame)
	}
	if s.TXStats().DroppedFull == 0 {
		t.Fatal("the TX queue never reported a drop; it cannot be bounded")
	}

	start := time.Now()
	for i := 0; i < 1000; i++ {
		s.WriteFrame(frame)
	}
	elapsed := time.Since(start)
	if elapsed >= 10*time.Millisecond {
		t.Fatalf("1000 WriteFrame calls took %v against a full queue; "+
			"the whole DSP tick budget is 10 ms", elapsed)
	}
	if got := ts.voiceCount(); got != 0 {
		t.Fatalf("%d datagrams escaped while the encoder was parked", got)
	}
	release()
}

func TestTXNoTargetsProducesNoPackets(t *testing.T) {
	ts, s := dialConnectedTX(t, nil)

	// Never calling SetTXFrequencies is the cold-start case.
	for i := 0; i < 4; i++ {
		s.WriteFrame(sineFrame(i))
	}
	settle()
	if got := ts.voiceCount(); got != 0 {
		t.Fatalf("%d datagrams sent with no TX frequency set, want 0", got)
	}
	if got := s.TXStats().DroppedNoTarget; got != 4 {
		t.Fatalf("TXStats().DroppedNoTarget = %d, want 4 (one per WriteFrame)", got)
	}

	// An explicitly emptied set behaves the same way.
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})
	s.SetTXFrequencies(nil)
	for i := 0; i < 2; i++ {
		s.WriteFrame(sineFrame(i))
	}
	settle()
	if got := ts.voiceCount(); got != 0 {
		t.Fatalf("%d datagrams sent after the target set was emptied, want 0", got)
	}
	if got := s.TXStats().DroppedNoTarget; got != 6 {
		t.Fatalf("TXStats().DroppedNoTarget = %d, want 6", got)
	}
	if got := s.TXStats().Sent; got != 0 {
		t.Fatalf("TXStats().Sent = %d, want 0", got)
	}
}

// TestTXPacketCeilingIsTheDatagramCeiling pins the two constants against each
// other with literals. The server reads into a fixed 1024-byte buffer and
// TRUNCATES anything larger rather than rejecting it, so an over-long payload
// is corruption with no error anywhere.
func TestTXPacketCeilingIsTheDatagramCeiling(t *testing.T) {
	if HeaderSize != 27 {
		t.Fatalf("HeaderSize = %d, want 27", HeaderSize)
	}
	if MaxDatagram != 1024 {
		t.Fatalf("MaxDatagram = %d, want 1024", MaxDatagram)
	}
	if opus.MaxPacket != 997 {
		t.Fatalf("opus.MaxPacket = %d, want 997 (= 1024 - 27)", opus.MaxPacket)
	}
	if !txPayloadFits(997) {
		t.Fatal("a 997-byte payload must fit in a 1024-byte datagram")
	}
	if txPayloadFits(998) {
		t.Fatal("a 998-byte payload must NOT fit: 27 + 998 = 1025 > 1024")
	}
}

func TestTXOversizeEncodeIsDroppedNotSent(t *testing.T) {
	// The control run first: 997 bytes is the largest payload that fits, and
	// it must go out. Without this the "nothing was sent" assertion below
	// would also pass if TX were broken outright.
	tsOK, sOK := dialConnectedTX(t, func(o *Options) {
		o.txEncode = func(_ []float32, dst []byte) (int, error) {
			for i := 0; i < 997; i++ {
				dst[i] = byte(i)
			}
			return 997, nil
		}
	})
	sOK.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})
	sOK.WriteFrame(sineFrame(0))
	sOK.WriteFrame(sineFrame(1))
	pkts := waitVoice(t, tsOK, 1)
	if len(pkts[0].Payload) != 997 {
		t.Fatalf("control payload = %d bytes, want 997", len(pkts[0].Payload))
	}

	tsBig, sBig := dialConnectedTX(t, func(o *Options) {
		o.txEncode = func(_ []float32, dst []byte) (int, error) {
			for i := 0; i < 998; i++ {
				dst[i] = byte(i)
			}
			return 998, nil
		}
	})
	sBig.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})
	sBig.WriteFrame(sineFrame(0))
	sBig.WriteFrame(sineFrame(1))
	settle()
	if got := tsBig.voiceCount(); got != 0 {
		t.Fatalf("%d datagrams sent for a 998-byte payload, want 0", got)
	}
	if got := sBig.TXStats().Sent; got != 0 {
		t.Fatalf("TXStats().Sent = %d for an over-ceiling payload, want 0", got)
	}
}

// TestTXQueueDepthFloor guards a number whose reason is easy to lose.
// dspLoop's catch-up path (Phase 4, maxCatchUpFrames = 3) can call WriteFrame
// four times inside one 10 ms tick. A queue shallower than that turns a
// recoverable latency spike into permanent audio loss, because the
// non-blocking send drops the overflow exactly when the backlog arrives.
func TestTXQueueDepthFloor(t *testing.T) {
	if txQueueDepth < 4 {
		t.Fatalf("txQueueDepth = %d, below the hard floor of 4", txQueueDepth)
	}
	if txQueueDepth != 8 {
		t.Fatalf("txQueueDepth = %d, want 8 (~160 ms); the floor is 4", txQueueDepth)
	}
}

// TestTXSurvivesClose checks that a WriteFrame racing Close neither panics
// nor blocks. The DSP goroutine is not synchronised against session
// teardown, so this ordering happens in production.
func TestTXSurvivesClose(t *testing.T) {
	_, s := dialConnectedTX(t, nil)
	s.SetTXFrequencies([]TXTarget{{Freq: freqAlpha}})
	s.WriteFrame(sineFrame(0))
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			s.WriteFrame(sineFrame(i))
		}
		s.EndTransmission()
		s.SetTXFrequencies([]TXTarget{{Freq: freqBravo}})
		_ = s.TXStats()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WriteFrame blocked after Close")
	}
}
