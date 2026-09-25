package voice

import (
	"io"
	"log/slog"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/audio/opus"
	"github.com/google/uuid"
)

// A Session is also the Phase 5 received-voice bus the Phase 4 pipeline
// pulls from. As with the audio.Sink assertion in tx_test.go, the check
// lives in the test so that this file's import of package audio (for
// Effect) is the only production coupling.
var _ audio.Source = (*Session)(nil)

// Three more wire frequencies, chosen asymmetric on the wire so a byte-order
// defect in the 24-bit frequency field cannot survive (see tx_test.go's
// freqAlpha/freqBravo, reused here):
//
//	400500 kHz -> 06 1C 74   (the global channel)
//	199700 kHz -> 03 0C 14   (tuned by nobody)
//	299800 kHz -> 04 93 18   (the server's echo/test frequency)
const (
	freqGlobal   = KHz(400500)
	freqUnlisted = KHz(199700)
	freqTest     = KHz(299800)
)

// Payload markers. The stub decoder turns marker m into a DC frame of
// m/1000, so two streams are told apart by amplitude and their sum is
// unambiguous: 100 -> 0.100, 20 -> 0.020, sum 0.120. Deliberately not equal
// and not a multiple of each other, so a demux that fed both packets to one
// stream, or summed one stream twice, lands on a value no assertion below
// accepts.
const (
	markerA = byte(100)
	markerB = byte(20)
)

// stampFor is the test's own model of what the stub decoder produces. It is
// written out here rather than called from the decoder so that the
// expectation and the fixture cannot drift into agreement with a broken
// implementation.
func stampFor(marker byte) float32 { return float32(marker) / 1000 }

// ---------------------------------------------------------------------------
// Stub codec
// ---------------------------------------------------------------------------

// decoderFactory hands out one stubDecoder per stream and records every one
// it ever made, so a test can assert both that streams got SEPARATE decoders
// and that every decoder was closed on teardown.
type decoderFactory struct {
	mu   sync.Mutex
	made []*stubDecoder
}

func (f *decoderFactory) new() (rxDecoder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &stubDecoder{f: f, id: len(f.made) + 1}
	f.made = append(f.made, d)
	return d, nil
}

func (f *decoderFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.made)
}

// closedCount reports how many of the decoders handed out so far have been
// closed. A decoder is a cgo allocation in production, so a stream torn down
// without closing its decoder is a leak that grows with every talker.
func (f *decoderFactory) closedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.made {
		if d.closed {
			n++
		}
	}
	return n
}

// concealCount reports how many packet-loss-concealment decodes (nil
// payload) were requested across every decoder.
func (f *decoderFactory) concealCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, d := range f.made {
		n += d.conceals
	}
	return n
}

// stubDecoder stands in for one stream's opus.Decoder. Its mutable state is
// guarded by its factory's lock because the decode goroutine writes it while
// the test goroutine reads it.
type stubDecoder struct {
	f  *decoderFactory
	id int

	closed   bool
	decodes  int
	conceals int
}

func (d *stubDecoder) Decode(payload []byte, pcm []float32) error {
	d.f.mu.Lock()
	var v float32
	if payload == nil {
		d.conceals++
	} else {
		d.decodes++
		v = float32(payload[0]) / 1000
	}
	d.f.mu.Unlock()
	for i := range pcm {
		pcm[i] = v
	}
	return nil
}

func (d *stubDecoder) Close() {
	d.f.mu.Lock()
	d.closed = true
	d.f.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// rxFixture is a connected session wired to a stub codec, plus the fixture
// server and the injected clock that drive it.
type rxFixture struct {
	ts   *testServer
	s    *Session
	clk  *fakeClock
	dec  *decoderFactory
	self uuid.UUID
	peer uuid.UUID
	buf  []float32
}

// dialRX brings up a connected session whose RX path uses the stub codec.
// Every RX test starts here: the packets under test arrive over the real
// socket, through the real rxLoop, so the demux is exercised end to end
// rather than by reaching into it.
func dialRX(t *testing.T, mutate func(*Options)) *rxFixture {
	t.Helper()
	ts := newTestServer(t, goodSecret)
	clk := newFakeClock()
	rec := newStateRecorder()
	dec := &decoderFactory{}

	opt := testOptions(clk, rec)
	opt.rxNewDecoder = dec.new
	if mutate != nil {
		mutate(&opt)
	}

	self := uuid.New()
	s, err := Dial(Sources{Update: ts.addr()}, self, goodSecret, opt)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	waitFor(t, "StateConnected", func() bool { return rec.saw(StateConnected) })

	return &rxFixture{
		ts:   ts,
		s:    s,
		clk:  clk,
		dec:  dec,
		self: self,
		peer: uuid.New(),
		buf:  make([]float32, audio.FrameSamples),
	}
}

// send injects one VOICE packet from sender on freq, carrying marker, and
// blocks until the RX path has counted it. Counting rather than sleeping is
// what makes the assertions afterwards deterministic despite UDP.
func (f *rxFixture) send(t *testing.T, sender uuid.UUID, seq uint32, freq KHz, marker byte) {
	t.Helper()
	before := f.s.RXStats().Received
	payload := []byte{marker, 1, 2, 3, 4, 5, 6, 7}
	f.ts.sendToClient(t, NewVoice(sender, seq, freq, payload, true, false))
	waitFor(t, "the RX path to count the datagram", func() bool {
		return f.s.RXStats().Received > before
	})
}

// sendBurst injects n consecutive frames from sender on freq starting at
// sequence 0x010200, an asymmetric 24-bit base so a byte-order defect in the
// sequence field cannot survive either.
func (f *rxFixture) sendBurst(t *testing.T, sender uuid.UUID, freq KHz, marker byte, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		f.send(t, sender, 0x010200+uint32(i), freq, marker)
	}
}

// prime advances the injected clock past the jitter buffer's priming delay,
// so buffered frames become eligible for playout.
func (f *rxFixture) prime() { f.clk.advance(2 * defaultJitterMS * time.Millisecond) }

// pumpUntil calls ReadInto until cond holds on the frame it produced, and
// returns that frame. ReadInto is BOTH the consumer of the decoded rings and
// the decode loop's wake source (design decision D8 leaves the RX path no
// clock of its own), so a test that slept instead of pumping would wait
// forever.
func (f *rxFixture) pumpUntil(t *testing.T, what string, cond func([]float32) bool) []float32 {
	t.Helper()
	var loudest []float32
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.s.ReadInto(f.buf)
		if cond(f.buf) {
			return append([]float32(nil), f.buf...)
		}
		if loudest == nil || peakOf(f.buf) > peakOf(loudest) {
			loudest = append([]float32(nil), f.buf...)
		}
		time.Sleep(200 * time.Microsecond)
	}
	t.Fatalf("timed out waiting for %s; the loudest frame seen was [0]=%v [last]=%v",
		what, sampleAt(loudest, 0), sampleAt(loudest, audio.FrameSamples-1))
	return nil
}

// pumpQuiet calls ReadInto n times and fails on the first non-silent sample.
func (f *rxFixture) pumpQuiet(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		f.s.ReadInto(f.buf)
		for j, v := range f.buf {
			if v != 0 {
				t.Fatalf("frame %d sample %d = %v, want silence", i, j, v)
			}
		}
		time.Sleep(200 * time.Microsecond)
	}
}

func peakOf(f []float32) float32 {
	var p float32
	for _, v := range f {
		if v < 0 {
			v = -v
		}
		if v > p {
			p = v
		}
	}
	return p
}

func sampleAt(f []float32, i int) float32 {
	if i < len(f) {
		return f[i]
	}
	return 0
}

// near reports whether got is within tol of want.
func near(got, want, tol float32) bool {
	d := got - want
	return d >= -tol && d <= tol
}

// allNear reports whether every sample of f is within tol of want.
func allNear(f []float32, want, tol float32) bool {
	for _, v := range f {
		if !near(v, want, tol) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRXFrameGeometryMatchesTheCodec pins the constant the jitter buffer is
// handed as frameDur against the codec that actually produces the frames. If
// the two ever disagree, every playout deadline in the buffer is computed
// against the wrong frame length and the error accumulates silently.
func TestRXFrameGeometryMatchesTheCodec(t *testing.T) {
	// 960 samples at 48 kHz is 20 ms, written out rather than derived.
	if opus.FrameSamples != 960 || opus.SampleRate != 48000 {
		t.Fatalf("codec geometry is %d samples at %d Hz; these tests are written for 960 at 48000",
			opus.FrameSamples, opus.SampleRate)
	}
	if rxFrameDuration != 20*time.Millisecond {
		t.Fatalf("rxFrameDuration = %v, want 20ms", rxFrameDuration)
	}
	if audio.SampleRate != opus.SampleRate {
		t.Fatalf("pipeline runs at %d Hz and the codec at %d Hz; the RX path resamples nothing",
			audio.SampleRate, opus.SampleRate)
	}
}

// TestRXDemuxesTwoSendersOnOneFrequency pins the (sender, frequency) key.
// Two people talking on one channel are two independent streams: they have
// independent sequence counters, so folding them into one jitter buffer
// would read each one's sequences as the other one's reorders and loss.
func TestRXDemuxesTwoSendersOnOneFrequency(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	other := uuid.New()
	f.sendBurst(t, f.peer, freqAlpha, markerA, 6)
	f.sendBurst(t, other, freqAlpha, markerB, 6)
	f.prime()

	// 0.100 + 0.020. Written out from the two markers, not read back from
	// the decoder.
	want := stampFor(markerA) + stampFor(markerB)
	f.pumpUntil(t, "both senders summed into one frame", func(fr []float32) bool {
		return allNear(fr, want, 0.002)
	})

	if got := f.dec.count(); got != 2 {
		t.Fatalf("%d decoders were created for two senders on one frequency, want 2", got)
	}
	if got := f.s.RXStats().Opened; got != 2 {
		t.Fatalf("RXStats().Opened = %d, want 2 streams", got)
	}
}

// TestRXDemuxesOneSenderOnTwoFrequencies is the other half of the key: one
// talker transmitting on two radios is two streams, because the TX path
// keeps a SEPARATE sequence counter per frequency (see tx.go) and a shared
// jitter buffer would see each counter as the other's gaps.
func TestRXDemuxesOneSenderOnTwoFrequencies(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha, freqBravo}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 6)
	f.sendBurst(t, f.peer, freqBravo, markerB, 6)
	f.prime()

	want := stampFor(markerA) + stampFor(markerB)
	f.pumpUntil(t, "both frequencies summed into one frame", func(fr []float32) bool {
		return allNear(fr, want, 0.002)
	})

	if got := f.dec.count(); got != 2 {
		t.Fatalf("%d decoders were created for one sender on two frequencies, want 2", got)
	}
}

// TestRXDropsOurOwnPackets pins the first drop rule. The server relays to
// every client tuned to the frequency, ourselves included, so without this
// every transmission comes straight back as an echo of our own voice.
func TestRXDropsOurOwnPackets(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.self, freqAlpha, markerA, 6)
	f.prime()

	f.pumpQuiet(t, 20)
	st := f.s.RXStats()
	if st.DroppedOwn != 6 {
		t.Fatalf("RXStats().DroppedOwn = %d, want 6", st.DroppedOwn)
	}
	if st.Opened != 0 {
		t.Fatalf("%d streams were opened for our own echo, want 0", st.Opened)
	}
}

// TestRXAcceptsOurOwnPacketsOnATestFrequency pins the exception. The
// server's test frequency exists precisely to echo a transmission back to
// its sender, so the own-sender rule must not swallow the one case where the
// echo is the point.
func TestRXAcceptsOurOwnPacketsOnATestFrequency(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, []KHz{freqTest})

	f.sendBurst(t, f.self, freqTest, markerA, 6)
	f.prime()

	f.pumpUntil(t, "our own audio echoed back on the test frequency", func(fr []float32) bool {
		return allNear(fr, stampFor(markerA), 0.002)
	})
	if got := f.s.RXStats().DroppedOwn; got != 0 {
		t.Fatalf("RXStats().DroppedOwn = %d on the test frequency, want 0", got)
	}
}

// TestRXDropsUnmatchedFrequencies pins the second drop rule: defence in
// depth behind the server's own filter. A frequency no radio is tuned to
// must produce silence and open no stream, or a misrouting server turns into
// unbounded per-talker state here.
func TestRXDropsUnmatchedFrequencies(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, []KHz{freqGlobal}, []KHz{freqTest})

	f.sendBurst(t, f.peer, freqUnlisted, markerA, 6)
	f.prime()

	f.pumpQuiet(t, 20)
	st := f.s.RXStats()
	if st.DroppedFreq != 6 {
		t.Fatalf("RXStats().DroppedFreq = %d, want 6", st.DroppedFreq)
	}
	if st.Opened != 0 {
		t.Fatalf("%d streams were opened for an untuned frequency, want 0", st.Opened)
	}
}

// TestRXDropsEverythingBeforeAContextIsSet pins that the RX path fails
// CLOSED. Between Dial and the first SetRXContext the client knows nothing
// about which frequencies it is tuned to, and "accept everything until told
// otherwise" would play whatever the server happened to be relaying at
// connect time.
func TestRXDropsEverythingBeforeAContextIsSet(t *testing.T) {
	f := dialRX(t, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 4)
	f.prime()

	f.pumpQuiet(t, 20)
	if got := f.s.RXStats().Opened; got != 0 {
		t.Fatalf("%d streams were opened with no RX context configured, want 0", got)
	}
}

// TestRXGlobalFrequenciesBypassEffects pins design decision D7's second
// half. The C# peer applies no radio colouration on the server's global
// channels, so a VCS client that did would make the two sound different on
// the one channel everybody shares.
//
// The two halves run on the same session, from the same marker, through the
// same stub decoder: the ONLY difference is which frequency the packets
// carried, so a difference in the output can only be the effect.
func TestRXGlobalFrequenciesBypassEffects(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetEffects("comms_filter_mid", "")
	f.s.SetRXContext([]KHz{freqAlpha}, []KHz{freqGlobal}, nil)

	// The global stream only. Its DC must survive untouched.
	f.sendBurst(t, f.peer, freqGlobal, markerA, 8)
	f.prime()
	f.pumpUntil(t, "unfiltered audio on the global frequency", func(fr []float32) bool {
		return allNear(fr, stampFor(markerA), 0.002)
	})
}

// TestRXNonGlobalFrequenciesAreEffected is the control for the test above.
// Without it, an implementation that never built an Effect at all would pass
// the global-bypass assertion perfectly.
//
// The probe is DC: every voice preset opens with a highpass, so a filtered
// constant decays to nothing within a few dozen samples while an unfiltered
// one stays flat. That makes "did the filter run" a tail-sample question
// rather than a spectral one.
func TestRXNonGlobalFrequenciesAreEffected(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetEffects("comms_filter_mid", "")
	f.s.SetRXContext([]KHz{freqAlpha}, []KHz{freqGlobal}, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 8)
	f.prime()

	// Wait for the stream to be producing at all (the head of the first
	// frame still carries most of the step), then require the tail to have
	// been blocked.
	fr := f.pumpUntil(t, "audio on the tuned frequency", func(fr []float32) bool {
		return peakOf(fr) > 0.01
	})
	tail := fr[len(fr)-1]
	if !near(tail, 0, 0.02) {
		t.Fatalf("tail sample = %v on a non-global frequency, want ~0: the radio effect's "+
			"highpass must have blocked the DC probe (D7 moved effects onto the RX path)", tail)
	}
}

// TestRXEffectIsNotRebuiltWhileItsIdsAreUnchanged pins that the per-stream
// effect survives from wake to wake. The decode loop is woken ~100 times a
// second by ReadInto, and rebuilding the Effect on each wake would reset the
// biquad's state every 10 ms -- a continuous stream of filter transients
// that sounds like crackle, not like a filter.
//
// A DC probe makes it observable: a filter whose state is intact blocks DC
// completely after the first few dozen samples, while one that is rebuilt
// every wake restarts its step response at the top of every frame.
func TestRXEffectIsNotRebuiltWhileItsIdsAreUnchanged(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetEffects("comms_filter_mid", "")
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 12)
	f.prime()

	// Get past the stream's genuine first transient.
	f.pumpUntil(t, "audio on the tuned frequency", func(fr []float32) bool {
		return peakOf(fr) > 0.01
	})
	// Every subsequent frame must be settled: a rebuilt filter would put a
	// fresh ~0.1 step at the head of one.
	for i := 0; i < 6; i++ {
		f.s.ReadInto(f.buf)
		if p := peakOf(f.buf); p > 0.02 {
			t.Fatalf("frame %d after the first peaks at %v, want a settled filter (<0.02): "+
				"the per-stream Effect must not be rebuilt on every decode wake", i, p)
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// TestReadIntoSumsTwoStreamsAndOverwrites pins both halves of ReadInto's
// contract: concurrent streams are SUMMED, and the caller's buffer is
// OVERWRITTEN rather than added into. dspLoop reuses one scratch buffer
// forever, so a summing ReadInto would accumulate the same audio into it
// tick after tick until it saturated the limiter.
func TestReadIntoSumsTwoStreamsAndOverwrites(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha, freqBravo}, nil, nil)

	other := uuid.New()
	f.sendBurst(t, f.peer, freqAlpha, markerA, 8)
	f.sendBurst(t, other, freqBravo, markerB, 8)
	f.prime()

	want := stampFor(markerA) + stampFor(markerB)
	f.pumpUntil(t, "two streams summed", func(fr []float32) bool {
		return allNear(fr, want, 0.002)
	})

	// Overwrite: hand ReadInto a buffer pre-filled with a value nothing in
	// the pipeline produces, and require it to be gone.
	for i := range f.buf {
		f.buf[i] = -0.75
	}
	f.s.ReadInto(f.buf)
	for i, v := range f.buf {
		if v < -0.1 {
			t.Fatalf("sample %d = %v: ReadInto must OVERWRITE its buffer, not sum into it", i, v)
		}
	}
}

// TestReadIntoNeverAllocates pins the realtime contract. ReadInto runs on
// the DSP goroutine inside the same 10 ms budget as the capture chain, and
// an allocation there is a GC assist in the audio callback's critical path.
//
// The session runs with a very long poll so the lifecycle goroutine is not
// building a timer per millisecond alongside the measurement; the DECODE
// goroutine is deliberately left running, because "allocation-free while the
// decode loop is servicing streams" is the claim that matters.
func TestReadIntoNeverAllocates(t *testing.T) {
	f := dialRX(t, func(o *Options) { o.poll = time.Hour })
	f.s.SetRXContext([]KHz{freqAlpha, freqBravo}, nil, nil)

	other := uuid.New()
	f.sendBurst(t, f.peer, freqAlpha, markerA, 8)
	f.sendBurst(t, other, freqBravo, markerB, 8)
	f.prime()

	want := stampFor(markerA) + stampFor(markerB)
	f.pumpUntil(t, "two streams summed", func(fr []float32) bool {
		return allNear(fr, want, 0.002)
	})

	buf := make([]float32, audio.FrameSamples)
	if allocs := testing.AllocsPerRun(500, func() { f.s.ReadInto(buf) }); allocs != 0 {
		t.Fatalf("ReadInto allocates %v per call on the DSP goroutine", allocs)
	}
}

// TestReadIntoIsSilentWithNoStreams pins that a session nobody is talking on
// produces silence rather than whatever was in the buffer, and does so
// without opening anything.
func TestReadIntoIsSilentWithNoStreams(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)
	for i := range f.buf {
		f.buf[i] = 0.9
	}
	f.s.ReadInto(f.buf)
	for i, v := range f.buf {
		if v != 0 {
			t.Fatalf("sample %d = %v with no streams, want silence", i, v)
		}
	}
}

// TestRXStreamsAreTornDownWhenIdle pins the idle reaper. Every stream holds
// a decoder (a cgo allocation), a jitter buffer and a PCM ring; without a
// teardown the client accumulates one set per talker per frequency for the
// whole session, and a busy server has a lot of both.
//
// It pins the RELEASE as well as the removal: an implementation that dropped
// the stream from the map but never closed its decoder would leak exactly as
// much C memory as no teardown at all, while looking tidy from the outside.
func TestRXStreamsAreTornDownWhenIdle(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 4)
	f.prime()
	f.pumpUntil(t, "audio from the stream", func(fr []float32) bool {
		return peakOf(fr) > 0.01
	})
	if got := f.s.RXStats().Active; got != 1 {
		t.Fatalf("Active = %d after a burst, want 1", got)
	}

	// Written out rather than read from rxIdleTimeout: the expectation is
	// "several seconds of silence", and deriving the advance from the
	// constant under test would keep the test green for any timeout at all.
	f.clk.advance(30 * time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && f.s.RXStats().Active != 0 {
		f.s.ReadInto(f.buf)
		time.Sleep(200 * time.Microsecond)
	}
	st := f.s.RXStats()
	if st.Active != 0 {
		t.Fatalf("Active = %d after 30s of silence, want 0", st.Active)
	}
	if st.Reaped != 1 {
		t.Fatalf("RXStats().Reaped = %d, want 1", st.Reaped)
	}
	if got := f.dec.closedCount(); got != 1 {
		t.Fatalf("%d of %d decoders were closed on teardown, want 1 -- a reaped stream "+
			"whose decoder is never closed leaks its C allocation", got, f.dec.count())
	}
}

// TestRXStreamsAreNotTornDownWhileTalking is the reaper's control. Without
// it, a reaper that fired unconditionally -- ignoring lastSeen entirely --
// would pass the test above and cut every transmission a few frames in.
func TestRXStreamsAreNotTornDownWhileTalking(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 4)
	f.prime()
	f.pumpUntil(t, "audio from the stream", func(fr []float32) bool {
		return peakOf(fr) > 0.01
	})

	// Keep talking across an interval far longer than the idle timeout,
	// refreshing the stream as a real sender would.
	for i := 0; i < 6; i++ {
		f.clk.advance(2 * time.Second)
		f.send(t, f.peer, 0x010300+uint32(i), freqAlpha, markerA)
		for j := 0; j < 5; j++ {
			f.s.ReadInto(f.buf)
			time.Sleep(200 * time.Microsecond)
		}
		if got := f.s.RXStats().Active; got != 1 {
			t.Fatalf("Active = %d while the sender is still talking, want 1", got)
		}
	}
	if got := f.s.RXStats().Reaped; got != 0 {
		t.Fatalf("RXStats().Reaped = %d while the sender never stopped, want 0", got)
	}
	if got := f.dec.closedCount(); got != 0 {
		t.Fatalf("%d decoders were closed while the stream was live, want 0", got)
	}
}

// TestRXAllDecodersAreClosedOnSessionClose pins the other release path. The
// reaper only ever runs for streams that go quiet; a session closed while
// three people are mid-sentence must still free all three decoders.
func TestRXAllDecodersAreClosedOnSessionClose(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha, freqBravo}, nil, nil)

	other := uuid.New()
	f.sendBurst(t, f.peer, freqAlpha, markerA, 3)
	f.sendBurst(t, other, freqAlpha, markerB, 3)
	f.sendBurst(t, f.peer, freqBravo, markerB, 3)
	if got := f.s.RXStats().Opened; got != 3 {
		t.Fatalf("Opened = %d, want 3", got)
	}

	f.s.Close()

	if got := f.dec.closedCount(); got != 3 {
		t.Fatalf("%d of %d decoders were closed by Close(), want 3", got, f.dec.count())
	}
	if got := f.s.RXStats().Active; got != 0 {
		t.Fatalf("Active = %d after Close, want 0", got)
	}
}

// TestRXBoundsTheNumberOfStreams pins the stream cap. Stream state is
// created from an attacker-reachable field -- a relayed packet's SenderID --
// so without a bound a server relaying junk, or a peer forging sender ids,
// allocates a decoder and a PCM ring per forged identity until the client
// runs out of memory.
func TestRXBoundsTheNumberOfStreams(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	// One more sender than the cap allows.
	for i := 0; i <= maxRXStreams; i++ {
		f.send(t, uuid.New(), 0x010200, freqAlpha, markerA)
	}

	st := f.s.RXStats()
	if st.Opened != uint64(maxRXStreams) {
		t.Fatalf("Opened = %d, want the cap of %d", st.Opened, maxRXStreams)
	}
	if st.DroppedStreamCap != 1 {
		t.Fatalf("RXStats().DroppedStreamCap = %d, want 1", st.DroppedStreamCap)
	}
	if got := f.dec.count(); got != maxRXStreams {
		t.Fatalf("%d decoders were created, want the cap of %d", got, maxRXStreams)
	}
}

// TestRXConcealsAConfirmedLoss pins that a gap whose playout deadline has
// passed reaches the decoder as a nil payload -- Opus's packet-loss
// concealment -- rather than as a hole in the ring. A dropped frame that is
// simply skipped is an audible click; concealed, it is a smear nobody
// notices.
func TestRXConcealsAConfirmedLoss(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	// Sequences 0x010200, 0x010201, then a hole at 0x010202, then 0x010203
	// onward. The hole's playout slot expires once the clock moves past it.
	for _, seq := range []uint32{0x010200, 0x010201, 0x010203, 0x010204, 0x010205, 0x010206} {
		f.send(t, f.peer, seq, freqAlpha, markerA)
	}
	f.prime()
	f.clk.advance(500 * time.Millisecond)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && f.dec.concealCount() == 0 {
		f.s.ReadInto(f.buf)
		time.Sleep(200 * time.Microsecond)
	}
	if got := f.dec.concealCount(); got != 1 {
		t.Fatalf("%d concealment decodes, want exactly 1 for the single missing sequence", got)
	}
	if got := f.s.RXStats().Concealed; got != 1 {
		t.Fatalf("RXStats().Concealed = %d, want 1", got)
	}
}

// TestRXDropsDuplicateSequences pins that the jitter buffer's refusal is
// counted rather than swallowed. A relay that duplicates a datagram must not
// produce the same 20 ms of audio twice.
func TestRXDropsDuplicateSequences(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.send(t, f.peer, 0x010200, freqAlpha, markerA)
	f.send(t, f.peer, 0x010200, freqAlpha, markerA)

	if got := f.s.RXStats().DroppedJitter; got != 1 {
		t.Fatalf("RXStats().DroppedJitter = %d after one duplicate, want 1", got)
	}
}

// TestRXDropsEmptyVoicePayloads pins that a VOICE datagram carrying no
// payload is discarded rather than handed to the decoder. A nil payload is
// Opus's request for packet-loss CONCEALMENT, so passing one straight
// through would make an empty datagram synthesise a frame of audio out of
// nothing -- and an empty datagram is the cheapest thing on the wire to
// forge or to produce by accident.
func TestRXDropsEmptyVoicePayloads(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	before := f.s.RXStats().Received
	f.ts.sendToClient(t, NewVoice(f.peer, 0x010200, freqAlpha, nil, true, false))
	waitFor(t, "the RX path to count the datagram", func() bool {
		return f.s.RXStats().Received > before
	})

	f.prime()
	f.pumpQuiet(t, 10)

	st := f.s.RXStats()
	if st.DroppedEmpty != 1 {
		t.Fatalf("RXStats().DroppedEmpty = %d, want 1", st.DroppedEmpty)
	}
	if st.Opened != 0 {
		t.Fatalf("%d streams were opened for an empty datagram, want 0", st.Opened)
	}
	if got := f.dec.concealCount(); got != 0 {
		t.Fatalf("%d concealment decodes from an empty datagram, want 0", got)
	}
}

// TestRXDecodesOnPacketArrivalWithoutAReadInto pins the OTHER of the decode
// loop's two wake sources. Every other test in this file pumps ReadInto, so
// every other test would pass with the arrival wake deleted entirely -- and
// the loop would then decode nothing until the next playback tick, adding up
// to a whole tick of latency to the start of every transmission and leaving
// the RX path unable to make progress at all in a build where the audio
// pipeline has not been started.
//
// It calls ReadInto exactly never.
func TestRXDecodesOnPacketArrivalWithoutAReadInto(t *testing.T) {
	f := dialRX(t, nil)
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 4)
	f.prime()
	// One further packet AFTER the clock moved past the priming deadline.
	// Its arrival is now the only event that can wake the decode loop.
	f.send(t, f.peer, 0x010204, freqAlpha, markerA)

	waitFor(t, "a decode driven purely by packet arrival", func() bool {
		return f.s.RXStats().Decoded > 0
	})
}

// TestRXDecodesRealOpus is the one test that exercises the PRODUCTION
// decoder factory. Every other test in this file injects a stub, so all of
// them would pass with newOpusDecoder broken, mis-sized or never reached --
// and the failure would only show up against a real peer.
//
// It sends real Opus on a GLOBAL frequency so no radio effect stands between
// the decoder's output and the assertion.
func TestRXDecodesRealOpus(t *testing.T) {
	requireOpus(t)
	// rxNewDecoder nil means "build real Opus decoders" -- the production
	// path.
	f := dialRX(t, func(o *Options) { o.rxNewDecoder = nil })
	f.s.SetRXContext(nil, []KHz{freqGlobal}, nil)

	enc, err := opus.NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()

	const frames = 12
	pcm := make([]float32, opus.FrameSamples)
	packet := make([]byte, opus.MaxPacket)
	for i := 0; i < frames; i++ {
		// A continuous 1 kHz tone at half scale, phase carried across
		// frames so the encoder sees a real signal rather than a sequence
		// of discontinuities.
		for j := range pcm {
			sec := float64(i*opus.FrameSamples+j) / float64(opus.SampleRate)
			pcm[j] = 0.5 * float32(math.Sin(2*math.Pi*1000*sec))
		}
		n, err := enc.Encode(pcm, packet)
		if err != nil {
			t.Fatalf("Encode frame %d: %v", i, err)
		}
		f.ts.sendToClient(t, NewVoice(f.peer, 0x010200+uint32(i), freqGlobal,
			append([]byte(nil), packet[:n]...), true, false))
	}
	waitFor(t, "every encoded frame to reach the RX path", func() bool {
		return f.s.RXStats().Received >= frames
	})
	f.prime()

	// Half-scale in, so anything above a tenth of full scale is real
	// decoded signal rather than noise. The threshold is written out from
	// the amplitude that was encoded, not read back from the decoder.
	f.pumpUntil(t, "real decoded audio", func(fr []float32) bool {
		return peakOf(fr) > 0.1
	})
	if got := f.s.RXStats().DecodeErrors; got != 0 {
		t.Fatalf("RXStats().DecodeErrors = %d, want 0", got)
	}
}

// TestRXSurvivesADecoderFactoryThatReturnsNothing pins that a codec which
// fails to initialise makes a talker inaudible rather than killing the
// process. The decode goroutine calls Decode unconditionally and has no
// recover, so a stream opened around a nil decoder is a panic on a
// background goroutine -- the whole client gone because one codec handle
// could not be allocated.
func TestRXSurvivesADecoderFactoryThatReturnsNothing(t *testing.T) {
	f := dialRX(t, func(o *Options) {
		o.rxNewDecoder = func() (rxDecoder, error) { return nil, nil }
	})
	f.s.SetRXContext([]KHz{freqAlpha}, nil, nil)

	f.sendBurst(t, f.peer, freqAlpha, markerA, 4)
	f.prime()
	f.pumpQuiet(t, 20)

	st := f.s.RXStats()
	if st.DroppedNoDecoder != 4 {
		t.Fatalf("RXStats().DroppedNoDecoder = %d, want 4", st.DroppedNoDecoder)
	}
	if st.Opened != 0 {
		t.Fatalf("%d streams were opened around a nil decoder, want 0", st.Opened)
	}
}

// TestRXLateFrameWithinTheJitterTargetIsAbsorbedNotConcealed pins the
// division of responsibility between the jitter buffer and the PCM ring:
// the jitter buffer owns the late-arrival grace window, and the ring only
// smooths the handoff to the DSP goroutine.
//
// aheadSamples was originally the whole jitter target. That drains the
// jitter buffer into the ring as fast as pop() will yield, so playout
// advances past a frame that is merely late and the frame is concealed
// rather than absorbed -- the exact outcome the configured target exists to
// prevent, and worst at the start of a transmission where it eats the first
// word.
//
// The consumer drains from tick zero here, without a warm-up, because that
// is what dspLoop really does: it calls ReadInto every 10 ms from the moment
// the session starts, long before any audio arrives. A warm-up hides the
// defect entirely -- with one, every candidate value conceals nothing.
//
// 41 ms is chosen as comfortably inside the 60 ms target and comfortably
// outside one 20 ms frame, so neither bound makes the result accidental.
func TestRXLateFrameWithinTheJitterTargetIsAbsorbedNotConcealed(t *testing.T) {
	const (
		lateIdx   = 20
		lateBy    = 41 * time.Millisecond
		totalPkts = 40
	)

	f := &decoderFactory{}
	var r rxState
	r.init(f.new, defaultJitterMS)

	dec, err := r.newDecoder()
	if err != nil {
		t.Fatalf("newDecoder: %v", err)
	}
	st := &rxStream{
		jit:  newJitter(r.jitterTarget, rxJitterMax, rxFrameDuration),
		ring: audio.NewRing(r.ringFrames),
		dec:  dec,
		pcm:  make([]float32, opus.FrameSamples),
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	buf := make([]float32, audio.FrameSamples)
	base := time.Unix(0, 0)
	sent := make(map[int]bool, totalPkts)

	for ms := 0; ms < 900; ms++ {
		now := base.Add(time.Duration(ms) * time.Millisecond)
		for i := 0; i < totalPkts; i++ {
			at := time.Duration(i) * rxFrameDuration
			if i == lateIdx {
				at += lateBy
			}
			if !sent[i] && at <= time.Duration(ms)*time.Millisecond {
				sent[i] = true
				st.jit.push(uint32(i), []byte{1}, now)
				st.topUp(now, &r, log)
			}
		}
		if ms%10 == 0 { // dspLoop's tick, from the very first one
			st.ring.Read(buf)
			st.topUp(now, &r, log)
		}
	}

	sd := dec.(*stubDecoder)
	f.mu.Lock()
	conceals, decodes := sd.conceals, sd.decodes
	f.mu.Unlock()

	if conceals != 0 {
		t.Errorf("a frame %v late against a %v jitter target was concealed %d time(s); "+
			"aheadSamples (%d) is consuming the grace window the jitter buffer owns",
			lateBy, r.jitterTarget, conceals, r.aheadSamples)
	}
	if decodes != totalPkts {
		t.Errorf("decoded %d of %d frames; the late frame should be absorbed, not dropped", decodes, totalPkts)
	}
}
