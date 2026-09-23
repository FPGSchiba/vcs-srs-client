package audio

import (
	"embed"
	"log/slog"
	"sort"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed assets
var assetFS embed.FS

// maxVoices caps simultaneous one-shots. Eight is generous for radio chirps
// and bounds the mix cost; the oldest is evicted rather than refusing the
// newest, so the most recent event is always audible.
const maxVoices = 8

// maxPendingEffects caps the queue Play appends to. Self-limiting in
// practice -- Play's critical section is a microsecond append and
// MixInto's TryLock will essentially always win at 10ms cadence -- but a
// caller hammering PlayEffect from many goroutines deserves backpressure
// rather than unbounded growth (Fix C). Consistent with the voice pool's
// own eviction policy: drop the oldest pending id when full, so the
// newest request always wins.
const maxPendingEffects = 32

type effectSlot struct {
	Label string `toml:"label"`
	File  string `toml:"file"`
	// Order fixes EffectIDs' display order. Unmarshalling manifest.toml
	// into a Go map does not preserve table order, so without this the
	// only "stable order" EffectIDs could offer was alphabetical --
	// which does not match the design prototype's TX/RX/Intercom/
	// Encryption grouping. See manifest.toml's own doc comment.
	Order int `toml:"order"`
}

type voice struct {
	samples []float32
	pos     int
}

// SFX owns the decoded sample set: order/slots/samples, populated once in
// NewSFX and never mutated afterward. Any goroutine may read them without a
// lock -- construction happens-before every use via the *SFX returned to
// the caller.
//
// A missing asset is a first-class, expected state: the sample pack is
// supplied separately (spec D11) and the client must work fully without it.
//
// SFX does NOT itself own the mutable mixing state a DSP loop drives every
// tick -- see voicePool below for why that had to change (Fix A, round 2).
// SFX's own Play/MixInto (used by direct/standalone callers and this
// file's own tests) work against a private default voicePool; Manager
// instead calls NewVoicePool per generation and threads the result through
// as a dspLoop parameter, exactly like captureRing/playbackRing.
type SFX struct {
	order   []string
	slots   map[string]effectSlot
	samples map[string][]float32

	voices *voicePool
	log    *slog.Logger
}

func NewSFX() *SFX {
	s := &SFX{
		slots:   map[string]effectSlot{},
		samples: map[string][]float32{},
		log:     slog.Default(),
	}
	s.voices = newVoicePool()
	if err := s.loadManifest(); err != nil {
		s.log.Error("audio: sfx manifest unreadable; all effects silent", "err", err)
	}
	s.loadSamples()
	return s
}

func (s *SFX) loadManifest() error {
	b, err := assetFS.ReadFile("assets/manifest.toml")
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(b, &s.slots); err != nil {
		return err
	}
	for id := range s.slots {
		s.order = append(s.order, id)
	}
	sort.Slice(s.order, func(i, j int) bool {
		return s.slots[s.order[i]].Order < s.slots[s.order[j]].Order
	})
	return nil
}

func (s *SFX) loadSamples() {
	for id, slot := range s.slots {
		if slot.File == "" {
			continue
		}
		b, err := assetFS.ReadFile("assets/" + slot.File)
		if err != nil {
			// Expected until the pack lands. Logged once per asset, at info:
			// this is a known pending dependency, not a malfunction.
			s.log.Info("audio: sfx sample absent; slot silent", "effect", id, "file", slot.File)
			continue
		}
		samples, err := DecodeWAV(b)
		if err != nil {
			s.log.Warn("audio: sfx sample undecodable", "effect", id, "file", slot.File, "err", err)
			continue
		}
		s.samples[id] = samples
	}
}

// EffectIDs returns the manifest slots in stable order (manifest.toml's
// `order` field), for the UI -- the backend's single source of truth for
// both the slot id SET and their display order, so a frontend Radio
// Effects panel has no reason to keep its own copy of either.
func (s *SFX) EffectIDs() []string {
	return append([]string(nil), s.order...)
}

// Label returns an effect's display name, or the id when unknown.
func (s *SFX) Label(id string) string {
	if slot, ok := s.slots[id]; ok && slot.Label != "" {
		return slot.Label
	}
	return id
}

// Available reports whether a slot has a decoded sample behind it. samples
// is immutable after construction (see the SFX doc), so this needs no lock.
func (s *SFX) Available(id string) bool {
	return len(s.samples[id]) > 0
}

// sampleFor looks up a decoded sample by id, or nil if unknown/absent. It's
// how a voicePool resolves a queued id against SFX's shared sample store
// without needing to hold a reference to the whole map.
func (s *SFX) sampleFor(id string) []float32 { return s.samples[id] }

// Play queues a one-shot on SFX's own default voice pool -- for direct,
// standalone SFX use (and this package's own tests). Manager does NOT use
// this path; see NewVoicePool. Unknown or absent ids are silently ignored.
func (s *SFX) Play(id string) {
	if len(s.samples[id]) == 0 {
		return
	}
	s.voices.play(id)
}

// MixInto mixes SFX's own default voice pool into dst. See Play's doc: this
// is for standalone use, not Manager's per-generation pools.
func (s *SFX) MixInto(dst []float32) {
	s.voices.mixInto(dst, s.sampleFor)
}

// NewVoicePool creates a fresh, independent mixing state against this SFX's
// (shared, immutable-after-construction) sample store. Manager creates one
// per Start() call -- see manager.go's Start and dspLoop -- so that an
// abandoned generation's dspLoop (Stop()'s bounded join, Fix 7) can never
// share mixing state with the next generation's.
func (s *SFX) NewVoicePool() *voicePool { return newVoicePool() }

// voicePool is the mutable mixing state a single DSP loop drives every
// tick: a pending queue and the voice ring itself.
//
// Concurrency (Fix 4, and Fix A round 2): mu guards ONLY pending, and is
// never held while mixing.
//
//   - play (any goroutine, off the realtime path) appends the requested id
//     to pending under mu. That's fine -- it isn't the audio thread.
//   - mixInto (DSP goroutine only, called every FrameDuration) uses
//     TryLock to drain pending into voices -- a pool owned exclusively by
//     the DSP goroutine from that point on -- then mixes with NO LOCK HELD
//     AT ALL. If TryLock fails (a play is mid-append on another goroutine),
//     mixInto skips the drain for this tick: the effect starts up to one
//     frame (10ms) later, which is inaudible, and the realtime path never
//     blocks on a contended lock.
//
// "The DSP goroutine" above means exactly one goroutine, for this pool's
// entire lifetime. That's why a voicePool is per-generation rather than a
// single Manager-lifetime field: Stop()'s bounded join (Fix 7) can abandon
// a dspLoop that is still running when the next Start() launches a new
// one, and if both generations' dspLoops shared ONE voicePool, they would
// become two concurrent, unsynchronized mixers over the same voices/head/
// count -- exactly the kind of corruption Fix A's ring-parameter fix
// closed for captureRing/playbackRing, just reached through m.sfx instead.
// A fresh voicePool per generation, passed as a dspLoop parameter, means an
// abandoned generation's pool is only ever touched by its own (leaked)
// goroutine, never the next generation's.
type voicePool struct {
	mu      sync.Mutex
	pending []string

	// voices is a fixed-capacity ring, indexed mod maxVoices, touched by
	// exactly one goroutine -- whichever calls mixInto -- so it needs no
	// synchronisation of its own. head is the oldest active slot; count is
	// how many of the maxVoices slots (starting at head) are live. Fixed
	// capacity means no slice growth/reallocation ever, unlike the
	// evict-front-then-append pattern this replaces (Task 9 finding).
	voices [maxVoices]voice
	head   int
	count  int
}

func newVoicePool() *voicePool { return &voicePool{} }

// play queues id for the DSP goroutine's next mixInto call. Safe from any
// goroutine; this is off the realtime path -- mu here is never contended by
// mixInto's mix step, only by its brief, best-effort pending drain. Caller
// (SFX.Play / Manager.PlayEffect) is responsible for filtering unknown ids
// before calling this, so pending capacity isn't spent on garbage.
func (p *voicePool) play(id string) {
	p.mu.Lock()
	if len(p.pending) >= maxPendingEffects {
		// Drop the oldest queued id, in place (no cap shrink -- see the
		// voice ring's own doc for why slicing off the front and
		// appending is the wrong pattern here).
		copy(p.pending, p.pending[1:])
		p.pending = p.pending[:len(p.pending)-1]
	}
	p.pending = append(p.pending, id)
	p.mu.Unlock()
}

// addVoice inserts samples into the fixed ring, evicting the oldest active
// voice if the pool is already full so the newest event is always audible.
// DSP-goroutine-only; no lock (see the voicePool doc).
func (p *voicePool) addVoice(samples []float32) {
	if p.count < maxVoices {
		idx := (p.head + p.count) % maxVoices
		p.voices[idx] = voice{samples: samples}
		p.count++
		return
	}
	p.voices[p.head] = voice{samples: samples}
	p.head = (p.head + 1) % maxVoices
}

// mixInto sums every active voice into dst and retires finished ones,
// resolving newly-queued ids to samples via lookup. Called from the DSP
// goroutine only, every FrameDuration. Takes no lock on its mixing path --
// see the voicePool doc for why that's safe.
func (p *voicePool) mixInto(dst []float32, lookup func(id string) []float32) {
	if p.mu.TryLock() {
		pending := p.pending
		p.pending = nil
		p.mu.Unlock()
		for _, id := range pending {
			if samples := lookup(id); len(samples) > 0 {
				p.addVoice(samples)
			}
		}
	}
	// Past this point nothing touches p.mu: voices/head/count are owned
	// solely by this goroutine, so the rest of this call is lock-free.

	write := 0
	for i := 0; i < p.count; i++ {
		v := p.voices[(p.head+i)%maxVoices]

		remaining := len(v.samples) - v.pos
		n := len(dst)
		if remaining < n {
			n = remaining
		}
		for j := 0; j < n; j++ {
			dst[j] += v.samples[v.pos+j]
		}
		v.pos += n

		if v.pos < len(v.samples) {
			p.voices[(p.head+write)%maxVoices] = v
			write++
		}
	}
	p.count = write
}
