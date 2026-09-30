package audio

import "log/slog"

// NotifyAlert is the one notification sound slot Phase 7.2 defines.
//
// Sound follows the toast: only error-severity notifications are audible,
// so one slot covers every case. A distinct warn sound can be added later
// by extending notifFiles -- the engine needs no change.
const NotifyAlert = "notify_alert"

// notifFiles maps a notification slot id to its WAV filename under assets/.
//
// Deliberately NOT in manifest.toml. That file drives SFX.EffectIDs(),
// which is the single source of truth for the Radio Effects panel's row
// set -- a notification sound listed there would appear in a UI it does not
// belong in.
var notifFiles = map[string]string{
	NotifyAlert: "notify_alert.wav",
}

// NotifSFX owns the decoded notification sample set. Populated once in
// NewNotifSFX and never mutated afterward, so any goroutine may read it
// without a lock -- construction happens-before every use via the returned
// pointer. The same ownership discipline as SFX.
//
// A missing asset is a first-class, expected state. The sample pack is
// supplied by the project (assets/README.md) and must not be substituted,
// so until it lands Available reports false and the slot plays silence.
type NotifSFX struct {
	samples map[string][]float32
	log     *slog.Logger
}

// NewNotifSFX decodes whatever notification samples are embedded.
func NewNotifSFX(log *slog.Logger) *NotifSFX {
	if log == nil {
		log = slog.Default()
	}
	n := &NotifSFX{samples: map[string][]float32{}, log: log}
	for id, file := range notifFiles {
		b, err := assetFS.ReadFile("assets/" + file)
		if err != nil {
			// Expected until the pack lands. Info, not Warn: a known
			// pending dependency is not a malfunction. Mirrors
			// SFX.loadSamples exactly.
			n.log.Info("audio: notification sample absent; slot silent", "slot", id, "file", file)
			continue
		}
		samples, err := DecodeWAV(b)
		if err != nil {
			n.log.Warn("audio: notification sample undecodable", "slot", id, "file", file, "err", err)
			continue
		}
		n.samples[id] = samples
	}
	return n
}

// Available reports whether a decoded sample currently backs the slot. False
// for every slot until the pack lands -- the honest current answer, not a
// placeholder.
func (n *NotifSFX) Available(id string) bool { return len(n.samples[id]) > 0 }

// sampleFor is the lookup handed to voicePool.mixInto.
func (n *NotifSFX) sampleFor(id string) []float32 { return n.samples[id] }

// NewVoicePool builds this set's mixing state for one Manager generation.
//
// Per-generation, exactly like SFX.NewVoicePool: Stop()'s bounded joins can
// leave a previous generation's dspLoop running alongside a new one, and two
// dspLoops sharing one pool is a data race -race has already caught once in
// this package.
func (n *NotifSFX) NewVoicePool() *voicePool { return newVoicePool() }
