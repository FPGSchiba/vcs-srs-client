package audio

// Levels are the four bus positions, as stored in [audio.levels]. They are
// KNOB POSITIONS in [0,1], not gains -- see taper.
type Levels struct {
	Master       float32
	Voice        float32
	SFX          float32
	Notification float32
}

// taper converts a knob position to a gain.
//
// A linear position wired straight to amplitude makes the top third of every
// knob inaudible as a change, because loudness perception is roughly
// logarithmic. Cubing approximates a ~60 dB fader law closely enough for a
// comms client and costs nothing.
func taper(position float32) float32 {
	if position <= 0 {
		return 0
	}
	if position >= 1 {
		return 1
	}
	return position * position * position
}

// Mixer sums the buses into the playback frame.
type Mixer struct {
	levels Levels
}

func NewMixer() *Mixer {
	return &Mixer{levels: Levels{Master: 0.75, Voice: 1, SFX: 0.8, Notification: 0.8}}
}

// SetLevels replaces the bus positions. Called from the DSP goroutine only.
func (m *Mixer) SetLevels(l Levels) { m.levels = l }

// Mix writes master × (voice×(monitor+received) + sfx×sfx +
// notification×notif) into out, hard-limited to [-1, 1].
//
// The Phase 5 addition this bus structure was built for has landed:
// `received` carries the decoded, per-stream-effected voice the RX path
// sums in internal/voice's ReadInto, and it rides the SAME voice gain as
// the local monitor. One bus, two sources -- a listener turning "Voice"
// down expects both their own sidetone and the people they are listening to
// to go quiet together, and a separate level for received audio is not
// something the design's four knobs offer.
//
// Both voice sources are summed BEFORE the limiter, deliberately: the
// clamp's job is to protect the output device from the sum of everything,
// so limiting each source on its own would let two half-scale sources still
// add up past full scale afterwards.
func (m *Mixer) Mix(out, monitor, received, sfx, notif []float32) {
	master := taper(m.levels.Master)
	gv := taper(m.levels.Voice) * master
	gs := taper(m.levels.SFX) * master
	gn := taper(m.levels.Notification) * master

	for i := range out {
		var s float32
		if i < len(monitor) {
			s += monitor[i] * gv
		}
		if i < len(received) {
			s += received[i] * gv
		}
		if i < len(sfx) {
			s += sfx[i] * gs
		}
		if i < len(notif) {
			s += notif[i] * gn
		}
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = s
	}
}
