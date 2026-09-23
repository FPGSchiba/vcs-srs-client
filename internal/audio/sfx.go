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
type SFX struct {
	mu sync.Mutex

	order   []string
	slots   map[string]effectSlot
	samples map[string][]float32
	voices  []voice

	warnedOnce map[string]bool
	log        *slog.Logger
}

func NewSFX() *SFX {
	s := &SFX{
		slots:      map[string]effectSlot{},
		samples:    map[string][]float32{},
		warnedOnce: map[string]bool{},
		log:        slog.Default(),
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

// Available reports whether a slot has a decoded sample behind it.
func (s *SFX) Available(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.samples[id]) > 0
}

// Play starts a one-shot. Unknown or absent ids are silently ignored.
func (s *SFX) Play(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[id]
	if len(samples) == 0 {
		return
	}
	if len(s.voices) >= maxVoices {
		s.voices = s.voices[1:] // evict oldest
	}
	s.voices = append(s.voices, voice{samples: samples})
}

// MixInto sums every active voice into dst and retires finished ones.
// Called from the DSP goroutine.
func (s *SFX) MixInto(dst []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.voices[:0]
	for _, v := range s.voices {
		remaining := len(v.samples) - v.pos
		count := len(dst)
		if remaining < count {
			count = remaining
		}
		for i := 0; i < count; i++ {
			dst[i] += v.samples[v.pos+i]
		}
		v.pos += count
		if v.pos < len(v.samples) {
			live = append(live, v)
		}
	}
	s.voices = live
}
