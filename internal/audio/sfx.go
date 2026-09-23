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

type effectSlot struct {
	Label string `toml:"label"`
	File  string `toml:"file"`
}

type voice struct {
	samples []float32
	pos     int
}

// SFX owns the decoded sample set and the playing voice pool.
//
// A missing asset is a first-class, expected state: the sample pack is
// supplied separately (spec D11) and the client must work fully without it.
//
// Concurrency (Fix 4): order/slots/samples are populated once in NewSFX and
// never mutated afterward, so any goroutine may read them without a lock --
// construction happens-before every use via the *SFX returned to the
// caller. mu guards ONLY the pending queue below and is never held while
// mixing:
//
//   - Play (any goroutine, off the realtime path) appends the requested id
//     to pending under mu. That's fine -- it isn't the audio thread.
//   - MixInto (DSP goroutine only, called every FrameDuration) uses
//     TryLock to drain pending into voices -- a pool owned exclusively by
//     the DSP goroutine from that point on -- then mixes with NO LOCK HELD
//     AT ALL. If TryLock fails (a Play is mid-append on another goroutine),
//     MixInto skips the drain for this tick: the effect starts up to one
//     frame (10ms) later, which is inaudible, and the realtime path never
//     blocks on a contended lock.
type SFX struct {
	order   []string
	slots   map[string]effectSlot
	samples map[string][]float32

	mu      sync.Mutex
	pending []string

	// voices is a fixed-capacity ring, indexed mod maxVoices, touched by
	// exactly one goroutine -- whichever calls MixInto -- so it needs no
	// synchronisation of its own. head is the oldest active slot; count is
	// how many of the maxVoices slots (starting at head) are live. Fixed
	// capacity means no slice growth/reallocation ever, unlike the
	// evict-front-then-append pattern this replaces (Task 9 finding).
	voices [maxVoices]voice
	head   int
	count  int

	log *slog.Logger
}

func NewSFX() *SFX {
	s := &SFX{
		slots:   map[string]effectSlot{},
		samples: map[string][]float32{},
		log:     slog.Default(),
	}
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
	sort.Strings(s.order)
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

// EffectIDs returns the manifest slots in stable order, for the UI.
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

// Play queues a one-shot to start on the DSP goroutine's next MixInto call.
// Unknown or absent ids are silently ignored. Safe from any goroutine; this
// is off the realtime path -- mu here is never contended by MixInto's mix
// step, only by its brief, best-effort pending drain.
func (s *SFX) Play(id string) {
	if len(s.samples[id]) == 0 {
		return
	}
	s.mu.Lock()
	s.pending = append(s.pending, id)
	s.mu.Unlock()
}

// addVoice inserts samples into the fixed ring, evicting the oldest active
// voice if the pool is already full so the newest event is always audible.
// DSP-goroutine-only; no lock (see the SFX doc).
func (s *SFX) addVoice(samples []float32) {
	if s.count < maxVoices {
		idx := (s.head + s.count) % maxVoices
		s.voices[idx] = voice{samples: samples}
		s.count++
		return
	}
	s.voices[s.head] = voice{samples: samples}
	s.head = (s.head + 1) % maxVoices
}

// MixInto sums every active voice into dst and retires finished ones.
// Called from the DSP goroutine only, every FrameDuration. Takes no lock on
// its mixing path -- see the SFX doc for why that's safe (Fix 4).
func (s *SFX) MixInto(dst []float32) {
	if s.mu.TryLock() {
		pending := s.pending
		s.pending = nil
		s.mu.Unlock()
		for _, id := range pending {
			if samples := s.samples[id]; len(samples) > 0 {
				s.addVoice(samples)
			}
		}
	}
	// Past this point nothing touches s.mu: voices/head/count are owned
	// solely by this goroutine, so the rest of this call is lock-free.

	write := 0
	for i := 0; i < s.count; i++ {
		v := s.voices[(s.head+i)%maxVoices]

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
			s.voices[(s.head+write)%maxVoices] = v
			write++
		}
	}
	s.count = write
}
