package voice

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/FPGSchiba/vcs-srs-client/internal/audio/opus"
)

const (
	// txBitrate is the encoder bitrate in bits per second. It is not on the
	// wire, but it is matched to the C# peer anyway (design doc D1) so a
	// transmission sounds the same whichever client sent it.
	txBitrate = 48000

	// txQueueDepth is how many encoded-but-unsent 20 ms frames the transmit
	// queue holds: 8 is roughly 160 ms.
	//
	// IT HAS A HARD FLOOR OF 4, AND THE REASON MUST NOT BE LOST. Phase 4's
	// dspLoop catch-up path (maxCatchUpFrames = 3, so a ceiling of four
	// frames in one tick) can call WriteFrame four times inside a single
	// 10 ms tick. Because the send is non-blocking, a queue shallower than
	// that burst does not merely delay audio, it DELETES it -- and it
	// deletes it exactly when a latency spike is already in progress, i.e.
	// when the backlog arrives. Do not reduce this below 4 under any
	// circumstances.
	txQueueDepth = 8

	// txBufferCount is the size of the PCM free list. It is one larger than
	// the queue so that txLoop holding a buffer while it encodes does not
	// cost the queue a slot: at most txQueueDepth buffers can be queued and
	// one in flight.
	txBufferCount = txQueueDepth + 1
)

// TXTarget is one frequency to transmit on this press.
type TXTarget struct {
	Freq     KHz
	Intercom bool
}

// TXStats reports transmit counters for diagnostics.
//
// Sent counts datagrams handed to the socket, NOT datagrams relayed by the
// server. The server drops anything it does not like -- an unbound source
// address, a frequency no radio is tuned to, a payload of five bytes or
// fewer -- and says nothing about it, so this number is an upper bound on
// what any peer actually heard.
//
// DroppedEncode covers every drop on the encode side of txSend: no encoder
// configured, an Opus encode error, a non-positive byte count, and an
// over-ceiling frame. They are folded into one counter rather than four
// because the diagnosis and the remedy are the same for all of them ("audio
// is not reaching the wire, check the log for which reason"); what matters
// for "am I transmitting at all" is that the counter moves.
//
// DroppedWrite counts frames that were encoded and sized correctly but
// failed on the per-target socket write (e.g. a sticky ICMP
// port-unreachable). It is separate from DroppedEncode because it locates
// the failure on the other side of the encode call, which matters for
// diagnosing it.
type TXStats struct{ Sent, DroppedFull, DroppedNoTarget, DroppedEncode, DroppedWrite uint64 }

// txState is everything the transmit path owns. It lives inside Session
// because the packets go out on Session's socket, through the same lock that
// serialises every write against Close's BYE.
type txState struct {
	// targets is the set of frequencies the next encoded frame is emitted
	// on. Held in an atomic pointer so WriteFrame can read it with a single
	// load on the realtime goroutine, and replaced wholesale so a reader
	// never sees a half-updated set.
	targets atomic.Pointer[[]TXTarget]

	// gen is bumped by EndTransmission. WriteFrame reads it before every
	// append and resets the accumulator when it has changed.
	//
	// Gate close is a problem the audio.Sink interface cannot express:
	// WriteFrame is only called while the gate is open, so a partially
	// filled accumulator would otherwise be glued onto the front of the NEXT
	// transmission -- 10 ms of audio from a previous press, at the wrong
	// frequency, possibly minutes later.
	gen atomic.Uint64

	// pcm carries full 20 ms PCM frames from the DSP goroutine to txLoop;
	// free recycles the buffers back. Buffers are conserved between the two
	// channels and txLoop's hand, so the transmit path allocates nothing per
	// frame after init.
	pcm  chan []float32
	free chan []float32

	// encode is the Opus encode call. It is a field rather than a direct
	// method call so tests can drive the ceiling and back-pressure paths
	// deterministically; enc is the real encoder behind it, kept for Close.
	encode func([]float32, []byte) (int, error)
	enc    *opus.Encoder

	// Each of these failures repeats every 20 ms for as long as it lasts, so
	// each is logged exactly once per session.
	oversizeOnce sync.Once
	encErrOnce   sync.Once
	noEncOnce    sync.Once
	emptyOnce    sync.Once
	writeErrOnce sync.Once

	sent         atomic.Uint64
	dropFull     atomic.Uint64
	dropNoTarget atomic.Uint64
	dropEncode   atomic.Uint64
	dropWrite    atomic.Uint64

	// mu guards the accumulator only, and is never held across anything that
	// can block. audio.Sink documents that WriteFrame may be invoked
	// concurrently -- an abandoned dspLoop generation and a live one can run
	// at once -- so the accumulator cannot be goroutine-confined; in steady
	// state there is exactly one caller and the lock is uncontended.
	mu     sync.Mutex
	acc    []float32
	accN   int
	accGen uint64
}

// init allocates the transmit buffers and the encoder. A nil encode override
// means "build the real one"; if that fails (a build without cgo, say) the
// session still works for everything except transmitting, which is better
// than refusing to connect at all.
func (t *txState) init(encode func([]float32, []byte) (int, error), log *slog.Logger) {
	t.pcm = make(chan []float32, txQueueDepth)
	t.free = make(chan []float32, txBufferCount)
	for i := 0; i < txBufferCount; i++ {
		t.free <- make([]float32, opus.FrameSamples)
	}
	t.acc = make([]float32, opus.FrameSamples)

	if encode != nil {
		t.encode = encode
		return
	}
	enc, err := opus.NewEncoder(txBitrate)
	if err != nil {
		log.Error("voice: no Opus encoder; transmit is disabled for this session", "err", err)
		return
	}
	t.enc = enc
	t.encode = enc.Encode
}

// close frees the encoder. The caller must have joined txLoop first.
func (t *txState) close() {
	t.enc.Close()
	t.enc = nil
	t.encode = nil
}

// SetTXFrequencies replaces the set of frequencies the next encoded frame is
// emitted on. Safe to call from any goroutine.
//
// The slice is copied: the caller keeps ownership of its own, and txLoop
// reads the stored one without a lock.
func (s *Session) SetTXFrequencies(targets []TXTarget) {
	cp := append([]TXTarget(nil), targets...)
	s.tx.targets.Store(&cp)
}

// EndTransmission marks a gate close so a partially filled accumulator is
// discarded rather than glued to the front of the next transmission. It
// costs at most 10 ms of already-captured audio at the tail of the press.
func (s *Session) EndTransmission() { s.tx.gen.Add(1) }

// TXStats reports transmit counters for diagnostics.
func (s *Session) TXStats() TXStats {
	return TXStats{
		Sent:            s.tx.sent.Load(),
		DroppedFull:     s.tx.dropFull.Load(),
		DroppedNoTarget: s.tx.dropNoTarget.Load(),
		DroppedEncode:   s.tx.dropEncode.Load(),
		DroppedWrite:    s.tx.dropWrite.Load(),
	}
}

// WriteFrame implements audio.Sink.
//
// IT RUNS ON THE DSP GOROUTINE INSIDE A 10 ms REALTIME BUDGET. It does an
// atomic load, a memcpy and a non-blocking channel send, and nothing else:
// no blocking, no allocation, no logging, no contended lock, and above all
// no Opus encode and no socket write -- both of those happen on txLoop. Every
// microsecond spent here is taken from the capture callback.
//
// frame is not retained: it is copied into the accumulator before the call
// returns, as the Sink contract requires.
func (s *Session) WriteFrame(frame []float32) {
	t := &s.tx

	targets := t.targets.Load()
	if targets == nil || len(*targets) == 0 {
		// Not transmitting. Counting rather than silently returning is what
		// makes "I pressed the key and nobody heard me" diagnosable.
		t.dropNoTarget.Add(1)
		return
	}

	gen := t.gen.Load()

	t.mu.Lock()
	if gen != t.accGen {
		t.accGen = gen
		t.accN = 0
	}
	// Two 10 ms DSP frames fill one 20 ms Opus frame. The loop is here so
	// that a frame length other than half the accumulator -- which the Sink
	// interface permits even though the pipeline never does it -- cannot
	// silently truncate audio.
	for len(frame) > 0 {
		n := copy(t.acc[t.accN:], frame)
		t.accN += n
		frame = frame[n:]
		if t.accN < len(t.acc) {
			break
		}
		t.accN = 0
		t.flushLocked()
	}
	t.mu.Unlock()
}

// flushLocked hands the filled accumulator to txLoop. Every step is
// non-blocking; when there is nowhere to put the frame it is dropped and
// counted, because stalling here would stall audio capture itself.
// Caller holds t.mu.
func (t *txState) flushLocked() {
	var buf []float32
	select {
	case buf = <-t.free:
	default:
		// No free buffer means every buffer is queued or being encoded:
		// txLoop is behind, or the session is closed.
		t.dropFull.Add(1)
		return
	}

	copy(buf, t.acc)

	select {
	case t.pcm <- buf:
	default:
		// Unreachable while buffers are conserved (there are more buffers
		// than queue slots), but a buffer dropped here would shrink the pool
		// for the rest of the session, so hand it back rather than leak it.
		select {
		case t.free <- buf:
		default:
		}
		t.dropFull.Add(1)
	}
}

// txPayloadFits reports whether an encoded frame of n bytes still fits in a
// VCS datagram once the 27-byte header is added.
//
// The check matters because the failure is invisible: the server reads into
// a fixed 1024-byte buffer and TRUNCATES anything longer rather than
// rejecting it, so an over-long frame arrives as a corrupt payload with no
// error reported anywhere.
func txPayloadFits(n int) bool { return HeaderSize+n <= MaxDatagram }

// txLoop is the transmit path's own goroutine: it encodes and writes, which
// are exactly the two things WriteFrame must never do.
func (s *Session) txLoop() {
	defer s.wg.Done()
	t := &s.tx

	// Deliberately MaxDatagram rather than opus.MaxPacket: sizing the
	// destination at the ceiling would let libopus clip to it silently, and
	// the over-ceiling case would then be undetectable instead of dropped.
	encoded := make([]byte, MaxDatagram)
	wire := make([]byte, 0, MaxDatagram)

	// One 24-bit sequence counter per frequency, owned solely by this
	// goroutine. Receivers key their jitter buffers by (sender, frequency),
	// so a counter shared across frequencies would show every one of them
	// artificial gaps.
	seqs := map[KHz]uint32{}

	for {
		select {
		case <-s.done:
			return
		case pcm := <-t.pcm:
			s.txSend(pcm, encoded, wire, seqs)
			select {
			case t.free <- pcm:
			default:
			}
		}
	}
}

// txSend encodes one 20 ms frame and emits it once per active frequency.
// The frame is encoded ONCE: the payload is identical on every frequency,
// only the header differs.
func (s *Session) txSend(pcm []float32, encoded, wire []byte, seqs map[KHz]uint32) {
	t := &s.tx

	if t.encode == nil {
		t.dropEncode.Add(1)
		t.noEncOnce.Do(func() {
			s.log.Error("voice: dropping transmit audio: no encoder")
		})
		return
	}
	n, err := t.encode(pcm, encoded)
	if err != nil {
		t.dropEncode.Add(1)
		t.encErrOnce.Do(func() {
			s.log.Error("voice: Opus encode failed; dropping transmit audio", "err", err)
		})
		return
	}
	if n <= 0 {
		t.dropEncode.Add(1)
		t.emptyOnce.Do(func() {
			s.log.Error("voice: Opus encode returned no bytes; dropping transmit audio", "n", n)
		})
		return
	}
	if n > opus.MaxPacket || !txPayloadFits(n) {
		t.dropEncode.Add(1)
		t.oversizeOnce.Do(func() {
			s.log.Error("voice: encoded frame exceeds the datagram ceiling; dropping",
				"bytes", n, "max", opus.MaxPacket)
		})
		return
	}
	payload := encoded[:n]

	targets := t.targets.Load()
	if targets == nil {
		return
	}
	for _, target := range *targets {
		seq := seqs[target.Freq]
		seqs[target.Freq] = (seq + 1) & wireMask24

		p := NewVoice(s.self, seq, target.Freq, payload, true, target.Intercom)
		if err := s.sendScratch(p, wire); err != nil {
			t.dropWrite.Add(1)
			if !errors.Is(err, errSessionClosed) {
				// Throttled like every sibling failure in this function: a
				// persistent socket error (a sticky ICMP port-unreachable on
				// a connected UDP socket, say) would otherwise log at 50 Hz
				// per stuck frequency for as long as the fault lasts.
				t.writeErrOnce.Do(func() {
					s.log.Debug("voice: voice packet not sent", "freq", uint32(target.Freq), "err", err)
				})
			}
			continue
		}
		t.sent.Add(1)
	}
}
