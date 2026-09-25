package voice

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio"
	"github.com/FPGSchiba/vcs-srs-client/internal/audio/opus"
	"github.com/google/uuid"
)

// rxFrameDuration is the wall-clock length of one Opus frame, and the value
// every per-stream jitter buffer is built with.
//
// It is written out rather than derived from opus.FrameSamples/SampleRate so
// that a change to the codec geometry shows up as a FAILING TEST
// (TestRXFrameGeometryMatchesTheCodec) rather than as a silently rescaled
// playout schedule: every deadline the jitter buffer computes is a multiple
// of this, so a wrong value does not break anything visibly -- it just makes
// every reorder and loss decision fire at the wrong moment.
const rxFrameDuration = 20 * time.Millisecond

const (
	// rxJitterMax is the maximum buffered duration per stream, after which
	// the oldest frame is discarded (design doc §8.3). The C# peer caps at
	// 2500 ms; 500 ms is ample for any link a voice call survives on, and
	// it is what bounds per-talker memory here.
	rxJitterMax = 500 * time.Millisecond

	// rxIdleTimeout is how long a stream may go without an accepted packet
	// before it is torn down and its decoder released.
	//
	// The cost of getting it wrong is asymmetric, which is why it is
	// seconds rather than milliseconds. Too long merely holds a decoder, a
	// jitter buffer and a PCM ring for a talker who has stopped. Too short
	// tears a stream down BETWEEN two presses of the same conversation, and
	// the replacement has to prime its jitter buffer again -- so the first
	// 60 ms of the next sentence is silence. Five seconds is comfortably
	// longer than any gap inside a conversation and far shorter than the
	// session lifetimes that would make the state grow.
	rxIdleTimeout = 5 * time.Second

	// maxRXStreams bounds how many (sender, frequency) streams may exist at
	// once. Stream state is created from a field that a peer controls -- the
	// relayed packet's SenderID -- so an unbounded map is a remote memory
	// exhaustion with a decoder, a jitter buffer and a PCM ring per forged
	// identity. Thirty-two simultaneous talkers is already far past what any
	// human can follow.
	maxRXStreams = 32
)

// rxDecoder is the decode half of the codec, as the RX path uses it.
// *opus.Decoder satisfies it directly; it exists as an interface only so
// tests can substitute a deterministic stub on builds with no cgo, exactly
// as txState.encode does on the transmit side.
//
// A nil payload requests packet-loss concealment.
type rxDecoder interface {
	Decode(payload []byte, pcm []float32) error
	Close()
}

// newOpusDecoder is the production decoder factory.
func newOpusDecoder() (rxDecoder, error) {
	d, err := opus.NewDecoder()
	if err != nil {
		// Returning d here would hand back a non-nil interface wrapping a
		// nil pointer, which every `!= nil` check downstream would pass.
		return nil, err
	}
	return d, nil
}

// streamKey identifies one received stream.
//
// BOTH fields are load-bearing. The transmit path keeps a SEPARATE 24-bit
// sequence counter per frequency (see tx.go), so one talker on two
// frequencies produces two independent sequence spaces; and two talkers on
// one frequency obviously do too. Keying on either field alone would read
// one counter's numbering as the other's reordering and loss, and the
// jitter buffer would spend the whole transmission concealing gaps that
// were never there.
type streamKey struct {
	sender uuid.UUID
	freq   KHz
}

// rxContext is the immutable frequency filter the RX path applies. It is
// replaced wholesale by SetRXContext so a reader never sees a half-updated
// set.
type rxContext struct {
	accept map[KHz]struct{} // frequencies an enabled radio is tuned to
	global map[KHz]struct{} // server global channels: accepted, never effected
	test   map[KHz]struct{} // server echo frequencies: our own packets come back
}

// rxEffects is the radio-effect selection, as a comparable value so a
// stream can tell "the user changed the preset" from "nothing changed"
// without rebuilding anything.
type rxEffects struct{ voice, clipping string }

// RXStats reports receive counters for diagnostics.
//
// Received counts VOICE datagrams that PARSED, before any filtering, so the
// drop counters below it always sum to at most that. The three drop reasons
// are kept apart because they mean completely different things to whoever
// is diagnosing silence: DroppedOwn is normal and constant while
// transmitting, DroppedFreq means the server is relaying something we are
// not tuned to (or our RX context is stale), and DroppedJitter means the
// network is delivering duplicates or frames whose playout slot has already
// passed.
//
// Decoded counts frames that reached a PCM ring; Concealed counts the
// subset of decodes that were loss concealment rather than real audio, so a
// Concealed/Decoded ratio is the client-side packet-loss figure.
type RXStats struct {
	Received uint64

	DroppedOwn       uint64
	DroppedFreq      uint64
	DroppedEmpty     uint64
	DroppedJitter    uint64
	DroppedStreamCap uint64
	DroppedNoDecoder uint64

	Decoded      uint64
	Concealed    uint64
	DecodeErrors uint64
	RingDropped  uint64

	Opened uint64
	Reaped uint64
	Byes   uint64

	// Active is a gauge, not a counter: the number of streams alive right
	// now.
	Active int
}

// rxStream is one (sender, frequency) transmission being received.
//
// Field ownership is split three ways and the split is what keeps ReadInto
// off every lock:
//
//   - jit is internally synchronised and is the hand-off point: rxLoop
//     pushes, the decode goroutine pops. That is exactly the arrangement
//     jitter.go documents.
//   - dec, effect, fx, fxSet and pcm belong to the DECODE GOROUTINE alone.
//     Nothing else reads or writes them, so none of them is synchronised.
//   - ring is a single-producer / single-consumer Ring: the decode
//     goroutine is the only writer and the DSP goroutine (ReadInto) is the
//     only reader.
//
// lastSeen is the one field two goroutines share, so it is an atomic rather
// than living under rxState.mu -- the reaper reads it once per stream per
// decode wake, and taking a lock to do that would put the receive goroutine
// behind the decode goroutine on every packet.
type rxStream struct {
	key    streamKey
	global bool // a server global channel: never carries a radio effect

	jit  *jitter
	ring *audio.Ring

	dec    rxDecoder
	effect *audio.Effect // nil until retune runs; always nil when global
	fx     rxEffects     // the selection `effect` was built from
	fxSet  bool
	pcm    []float32 // decode scratch, one Opus frame long

	lastSeen atomic.Int64 // UnixNano of the most recent accepted packet
}

// rxState is everything the receive path owns. Like txState it lives inside
// Session, because its packets arrive on Session's socket.
type rxState struct {
	// snapshot is the stream list as ReadInto sees it: an immutable slice,
	// replaced wholesale whenever a stream is opened or reaped. ReadInto
	// runs on the DSP goroutine and must not take a lock, so it gets a
	// single atomic load and iterates; the map below, which the receive and
	// decode goroutines mutate, is never touched from there.
	snapshot atomic.Pointer[[]*rxStream]

	ctx atomic.Pointer[rxContext]
	fx  atomic.Pointer[rxEffects]

	// mu guards streams only. It is taken by the receive goroutine (once
	// per packet, for a map lookup) and by the decode goroutine (only when
	// opening or reaping). The DSP goroutine NEVER takes it, which is what
	// makes ReadInto's "no contended lock" claim true rather than hopeful.
	mu      sync.Mutex
	streams map[streamKey]*rxStream

	// wake carries no data: it is a one-slot "there is work" flag. The
	// decode loop is deliberately CLOCKLESS (design decision D8) -- its only
	// two wake sources are a packet arriving and ReadInto draining a ring,
	// i.e. dspLoop's tick arriving by proxy. A ticker here would be a second
	// 10 ms clock running free against dspLoop's, and two unsynchronised
	// clocks at the same nominal rate drift into periodic underruns.
	wake chan struct{}

	// mix is ReadInto's scratch buffer, allocated once. It belongs to the
	// DSP goroutine exclusively.
	mix []float32

	newDecoder func() (rxDecoder, error)

	jitterTarget time.Duration

	// aheadSamples is how much decoded PCM the decode loop keeps in each
	// stream's ring.
	//
	// It is a SMALL CONSTANT -- two Opus frames -- and deliberately NOT the
	// jitter target. The division of responsibility is:
	//
	//	the jitter buffer owns the late-arrival grace window;
	//	this ring only smooths the handoff to the DSP goroutine.
	//
	// It was originally set equal to the jitter target, reasoning that since
	// pop() hands out every contiguous frame as fast as it is asked once
	// primed, this threshold rather than the target is what paces playout.
	// The first half is true; the conclusion was backwards. A large value
	// drains the jitter buffer into the ring immediately, so playout
	// advances past a frame that is merely late and the frame is CONCEALED
	// instead of absorbed -- precisely what the configured target exists to
	// prevent.
	//
	// Measured, driving these same types, with the consumer draining from
	// tick zero as dspLoop really does: against a 60 ms target, a frame
	// arriving 41 ms late is concealed at 2880 samples (60 ms) and absorbed
	// at both 960 and 1920. Short reads were identical across all three, so
	// the smaller value costs no underrun margin. A warm-up before the first
	// drain hides the defect entirely, which is why
	// TestRXLateFrameWithinTheJitterTargetIsAbsorbedNotConcealed has none.
	//
	// Capped at the target so a deliberately tiny configured jitter buffer
	// is still honoured, and floored at one whole frame.
	aheadSamples int
	ringFrames   int

	received atomic.Uint64

	dropOwn       atomic.Uint64
	dropFreq      atomic.Uint64
	dropEmpty     atomic.Uint64
	dropJitter    atomic.Uint64
	dropStreamCap atomic.Uint64
	dropNoDecoder atomic.Uint64

	decoded     atomic.Uint64
	concealed   atomic.Uint64
	decodeErr   atomic.Uint64
	ringDropped atomic.Uint64

	opened atomic.Uint64
	reaped atomic.Uint64
	byes   atomic.Uint64

	// Each of these failures repeats for every frame for as long as it
	// lasts, so each is logged exactly once per session.
	noDecoderOnce sync.Once
	decErrOnce    sync.Once
	ringFullOnce  sync.Once
}

// init allocates the receive path's fixed state. A nil newDecoder means
// "build real Opus decoders"; a build without cgo then fails at the first
// stream instead of at Dial, which is the same degradation the transmit
// side takes -- the session still keeps its binding alive, it just cannot
// play anything back.
func (r *rxState) init(newDecoder func() (rxDecoder, error), jitterMS int) {
	r.streams = map[streamKey]*rxStream{}
	empty := []*rxStream{}
	r.snapshot.Store(&empty)
	r.wake = make(chan struct{}, 1)
	r.mix = make([]float32, audio.FrameSamples)
	r.jitterTarget = time.Duration(jitterMS) * time.Millisecond

	r.newDecoder = newDecoder
	if r.newDecoder == nil {
		r.newDecoder = newOpusDecoder
	}

	// See aheadSamples' doc: a small constant, not the jitter target.
	r.aheadSamples = 2 * opus.FrameSamples
	if target := jitterMS * (audio.SampleRate / 1000); r.aheadSamples > target {
		r.aheadSamples = target
	}
	if r.aheadSamples < opus.FrameSamples {
		// One whole Opus frame is the floor: a threshold below one frame
		// would be satisfied by a partially drained ring and the loop would
		// never decode ahead at all.
		r.aheadSamples = opus.FrameSamples
	}
	// Room for the threshold plus two more frames, so the top-up loop's final
	// write always fits and Ring.Write -- which drops a chunk WHOLE rather
	// than tearing a frame -- never has to refuse one.
	r.ringFrames = (r.aheadSamples + 2*opus.FrameSamples + audio.FrameSamples - 1) / audio.FrameSamples
}

// close releases every stream's decoder. The caller must have joined the
// receive and decode goroutines first, so nothing can be inside a Decode
// call. It is safe on a never-initialised rxState (Dial's failure path).
func (r *rxState) close() {
	r.mu.Lock()
	streams := r.streams
	r.streams = map[streamKey]*rxStream{}
	r.publishLocked()
	r.mu.Unlock()

	for _, st := range streams {
		st.dec.Close()
	}
}

// publishLocked republishes the stream list ReadInto reads. Caller holds
// r.mu.
//
// A whole new slice each time, never a mutation of the published one: the
// DSP goroutine may be iterating the previous slice at this very moment,
// and it holds no lock that could be made to wait.
func (r *rxState) publishLocked() {
	next := make([]*rxStream, 0, len(r.streams))
	for _, st := range r.streams {
		next = append(next, st)
	}
	r.snapshot.Store(&next)
}

// signal wakes the decode loop without ever blocking the caller. The channel
// carries no data, so a full buffer means "already awake" and the send can
// simply be dropped.
func (r *rxState) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// freqSet turns a frequency list into a set for O(1) matching. Nil in, empty
// set out -- an absent list must match nothing, never everything.
func freqSet(freqs []KHz) map[KHz]struct{} {
	set := make(map[KHz]struct{}, len(freqs))
	for _, f := range freqs {
		set[f] = struct{}{}
	}
	return set
}

// SetRXContext tells the RX path which frequencies to accept, which are the
// server's global channels, and which are its echo/test frequencies. Safe to
// call from any goroutine.
//
// accept is the set an enabled radio is tuned to. global is accepted as
// well, and additionally BYPASSES the radio effect, because the C# peer
// leaves global channels dry and the two clients must sound the same on the
// one channel everybody shares. testFreqs is accepted as well, and
// additionally relaxes the own-sender rule: the server's test frequency
// exists to echo a transmission back to whoever sent it, so dropping our own
// SenderID there would silence the one case where the echo is the point.
//
// Until this is called the RX path accepts NOTHING. That is deliberate: a
// freshly dialed session knows nothing about which radios are enabled, and
// "play everything until told otherwise" would dump whatever the server
// happened to be relaying into the user's headset at connect time.
//
// Frequencies that DISAPPEAR from accept are not torn down here. Their
// streams simply stop being fed, drain, and are reaped by the idle timeout
// -- which is also what makes retuning mid-sentence sound like a radio
// rather than like a hard cut.
func (s *Session) SetRXContext(accept, global, testFreqs []KHz) {
	s.rx.ctx.Store(&rxContext{
		accept: freqSet(accept),
		global: freqSet(global),
		test:   freqSet(testFreqs),
	})
}

// SetEffects configures the radio-effect presets applied to received audio.
// Safe to call from any goroutine; every live stream picks the change up on
// its next decode wake, and global-frequency streams ignore it.
//
// Effects live HERE, not on the transmit path (design decision D7). The C#
// peer applies them on receive, so transmitting pre-effected audio left a C#
// listener hearing us double-effected while we heard them dry -- and made
// the "no effects on global frequencies" rule unimplementable from the
// sending side, since a sender cannot know each listener's frequency.
func (s *Session) SetEffects(voiceEffect, clippingEffect string) {
	s.rx.fx.Store(&rxEffects{voice: voiceEffect, clipping: clippingEffect})
	s.rx.signal()
}

// RXStats reports receive counters for diagnostics.
func (s *Session) RXStats() RXStats {
	r := &s.rx
	st := RXStats{
		Received:         r.received.Load(),
		DroppedOwn:       r.dropOwn.Load(),
		DroppedFreq:      r.dropFreq.Load(),
		DroppedEmpty:     r.dropEmpty.Load(),
		DroppedJitter:    r.dropJitter.Load(),
		DroppedStreamCap: r.dropStreamCap.Load(),
		DroppedNoDecoder: r.dropNoDecoder.Load(),
		Decoded:          r.decoded.Load(),
		Concealed:        r.concealed.Load(),
		DecodeErrors:     r.decodeErr.Load(),
		RingDropped:      r.ringDropped.Load(),
		Opened:           r.opened.Load(),
		Reaped:           r.reaped.Load(),
		Byes:             r.byes.Load(),
	}
	if p := r.snapshot.Load(); p != nil {
		st.Active = len(*p)
	}
	return st
}

// ReadInto sums every active received stream into buf, OVERWRITING it.
//
// IT RUNS ON THE DSP GOROUTINE INSIDE A 10 ms REALTIME BUDGET, and it does
// one atomic load, one Ring.Read and one add per active stream, and nothing
// else: no decode, no allocation, no blocking, no logging, and no lock the
// receive or decode goroutine could be holding. Every microsecond spent here
// is taken from the playback callback.
//
// Overwriting rather than summing is part of the contract: dspLoop hands it
// the same scratch buffer on every tick, so a summing implementation would
// accumulate the same audio into it tick after tick until it pinned the
// mixer's limiter.
//
// The trailing wake is what keeps the decode loop clockless (D8): draining a
// ring is exactly the event that means "there is room to decode another
// frame", and it arrives on dspLoop's tick, which is the only clock the RX
// path is allowed to have.
func (s *Session) ReadInto(buf []float32) {
	r := &s.rx
	clear(buf)

	n := len(buf)
	if n > len(r.mix) {
		// dspLoop always passes exactly FrameSamples. A longer buffer would
		// need a bigger scratch, and growing one here would allocate on the
		// realtime path; the tail is already cleared, so it stays silent.
		n = len(r.mix)
	}
	if streams := r.snapshot.Load(); streams != nil && n > 0 {
		scratch := r.mix[:n]
		for _, st := range *streams {
			got := st.ring.Read(scratch)
			for i := 0; i < got; i++ {
				buf[i] += scratch[i]
			}
		}
	}

	r.signal()
}

// rxVoice admits one parsed VOICE datagram. It is called from rxLoop, which
// must never block or the kernel drops datagrams behind it, so this does
// exactly two things: filter, and push into the stream's jitter buffer.
// Every decode, every effect and every allocation of consequence happens on
// the decode goroutine instead.
//
// The payload is handed to the jitter buffer BY REFERENCE and is not copied.
// That is safe because Parse already copied it out of rxLoop's reused read
// buffer, and it is deliberate because it makes the receive path allocate
// once per packet rather than twice. DO NOT introduce a pooled or reused
// payload buffer upstream of this: a jitter buffer can hold a frame for up
// to rxJitterMax, and recycling the memory under it would corrupt audio that
// is still queued, with a symptom that looks like a codec bug rather than a
// lifetime bug.
func (s *Session) rxVoice(pkt *Packet, at time.Time) {
	r := &s.rx
	r.received.Add(1)

	ctx := r.ctx.Load()
	if ctx == nil {
		// No RX context yet: fail closed. See SetRXContext.
		r.dropFreq.Add(1)
		return
	}

	_, isTest := ctx.test[pkt.Frequency]
	if pkt.SenderID == s.self && !isTest {
		// The server relays to everyone tuned to the frequency, us
		// included, so without this every transmission comes straight back
		// as an echo of our own voice.
		r.dropOwn.Add(1)
		return
	}

	_, isGlobal := ctx.global[pkt.Frequency]
	if _, ok := ctx.accept[pkt.Frequency]; !ok && !isGlobal && !isTest {
		// Defence in depth: the server already filters by frequency, so
		// this only fires on a misrouting or hostile relay -- which is
		// exactly when unbounded per-talker state would be created here.
		r.dropFreq.Add(1)
		return
	}

	if len(pkt.Payload) == 0 {
		// A VOICE packet with no payload would reach the decoder as a nil
		// slice, which is Opus's request for loss CONCEALMENT -- so an
		// empty datagram would synthesise audio instead of being ignored.
		r.dropEmpty.Add(1)
		return
	}

	st := s.rxStreamFor(streamKey{sender: pkt.SenderID, freq: pkt.Frequency}, isGlobal, at)
	if st == nil {
		return
	}
	st.lastSeen.Store(at.UnixNano())
	if !st.jit.push(pkt.Sequence, pkt.Payload, at) {
		r.dropJitter.Add(1)
	}
	r.signal()
}

// rxStreamFor returns the stream for key, opening one if this is the first
// packet of a new transmission. It returns nil when no stream could be
// opened, having counted the reason.
func (s *Session) rxStreamFor(key streamKey, global bool, at time.Time) *rxStream {
	r := &s.rx

	r.mu.Lock()
	defer r.mu.Unlock()

	if st, ok := r.streams[key]; ok {
		return st
	}
	if len(r.streams) >= maxRXStreams {
		r.dropStreamCap.Add(1)
		return nil
	}

	// Opening a stream allocates a decoder, a jitter buffer and a PCM ring
	// on the receive goroutine. That is bounded work that happens once per
	// talker per frequency -- not per packet -- and moving it onto the
	// decode goroutine would mean queueing the packet somewhere first, i.e.
	// a second buffer with a second drop policy for no benefit.
	dec, err := r.newDecoder()
	if err != nil || dec == nil {
		// The `dec == nil` half is not redundant with the error check. The
		// decode goroutine calls dec.Decode unconditionally, so a factory
		// that returned no decoder and no error would panic there -- on a
		// goroutine with no recover, taking the whole client down over a
		// codec that merely failed to initialise. Refusing the stream
		// instead degrades to "this talker is inaudible", which is what the
		// error path already does.
		r.dropNoDecoder.Add(1)
		r.noDecoderOnce.Do(func() {
			s.log.Error("voice: no Opus decoder; received audio is unavailable for this session", "err", err)
		})
		return nil
	}

	st := &rxStream{
		key:    key,
		global: global,
		jit:    newJitter(r.jitterTarget, rxJitterMax, rxFrameDuration),
		ring:   audio.NewRing(r.ringFrames),
		dec:    dec,
		pcm:    make([]float32, opus.FrameSamples),
	}
	st.lastSeen.Store(at.UnixNano())

	r.streams[key] = st
	r.publishLocked()
	r.opened.Add(1)
	return st
}

// decodeLoop is the receive path's own goroutine: it decodes and applies
// effects, which are exactly the two things rxLoop and ReadInto must never
// do.
//
// It has NO CLOCK OF ITS OWN (design decision D8). It parks until something
// wakes it, and the only two things that ever do are a packet arriving and
// ReadInto draining a ring on dspLoop's tick. A 10 ms ticker here would run
// free against dspLoop's own 10 ms ticker, and two unsynchronised clocks at
// the same nominal rate beat against each other into periodic underruns --
// audible as a click every few seconds, and untraceable to either loop in
// isolation.
func (s *Session) decodeLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case <-s.rx.wake:
		}
		s.rxService(s.now())
	}
}

// rxService tops every stream's PCM ring up and reaps the ones that have
// gone quiet. It runs only on the decode goroutine.
func (s *Session) rxService(now time.Time) {
	r := &s.rx

	streams := r.snapshot.Load()
	if streams == nil {
		return
	}
	var want rxEffects
	if p := r.fx.Load(); p != nil {
		want = *p
	}

	var idle []streamKey
	for _, st := range *streams {
		st.retune(want)
		st.topUp(now, r, s.log)
		if now.Sub(time.Unix(0, st.lastSeen.Load())) >= rxIdleTimeout {
			idle = append(idle, st.key)
		}
	}
	for _, key := range idle {
		r.reap(key, now)
	}
}

// retune rebuilds this stream's Effect when, and only when, the requested
// preset ids have changed.
//
// The comparison is against the REQUEST, not against Effect.IDs(): an
// unrecognised id degrades to passthrough and reports empty ids, so
// comparing the resolved ids would see a mismatch forever and rebuild the
// filter on every wake. That matters because rebuilding resets the biquad
// state, and resetting it ~100 times a second -- once per ReadInto -- turns
// a filter into a stream of step transients. It sounds like crackle, and
// nothing about it looks wrong in a code review.
func (st *rxStream) retune(want rxEffects) {
	if st.global {
		// Global channels are dry, by decision D7, to match the C# peer.
		return
	}
	if st.fxSet && st.fx == want {
		return
	}
	st.effect = audio.NewEffect(want.voice, want.clipping)
	st.fx, st.fxSet = want, true
}

// topUp decodes frames into this stream's PCM ring until it holds
// aheadSamples, or until the jitter buffer has nothing ready.
//
// Stopping at a threshold rather than draining the buffer is the whole
// pacing mechanism: see rxState.aheadSamples for why that threshold, and not
// the jitter target, is what actually decides how much late-arrival grace a
// stream gets.
func (st *rxStream) topUp(now time.Time, r *rxState, log *slog.Logger) {
	for st.ring.Available() < r.aheadSamples {
		payload, lost, ok := st.jit.pop(now)
		if !ok {
			return
		}
		if lost {
			// pop returns a nil payload here, which is Opus's request for
			// packet-loss concealment: the decoder synthesises a plausible
			// continuation rather than leaving a hole, which is the
			// difference between a smear nobody notices and an audible
			// click.
			r.concealed.Add(1)
		}
		if err := st.dec.Decode(payload, st.pcm); err != nil {
			r.decodeErr.Add(1)
			r.decErrOnce.Do(func() {
				log.Error("voice: Opus decode failed; dropping received audio", "err", err)
			})
			continue
		}
		if st.effect != nil {
			st.effect.Process(st.pcm)
		}
		if dropped := st.ring.Write(st.pcm); dropped > 0 {
			// Unreachable while ringFrames is sized against aheadSamples,
			// but a silently discarded frame here would sound like network
			// loss, so it is counted rather than ignored.
			r.ringDropped.Add(uint64(dropped))
			r.ringFullOnce.Do(func() {
				log.Warn("voice: received PCM ring full; dropping decoded audio",
					"freq", uint32(st.key.freq))
			})
			return
		}
		r.decoded.Add(1)
	}
}

// reap tears down a stream that has gone quiet and releases its decoder.
//
// The idle check is repeated under the lock against the CURRENT clock: a
// packet can arrive between rxService observing the stream as idle and this
// call, and reaping it then would throw away the first frames of a sentence
// that had just restarted.
func (r *rxState) reap(key streamKey, now time.Time) {
	r.mu.Lock()
	st, ok := r.streams[key]
	if !ok {
		r.mu.Unlock()
		return
	}
	if now.Sub(time.Unix(0, st.lastSeen.Load())) < rxIdleTimeout {
		r.mu.Unlock()
		return
	}
	delete(r.streams, key)
	r.publishLocked()
	r.mu.Unlock()

	// Safe outside the lock: reap runs on the decode goroutine, which is
	// the only caller of Decode, and it is not inside one.
	st.dec.Close()
	r.reaped.Add(1)
}
