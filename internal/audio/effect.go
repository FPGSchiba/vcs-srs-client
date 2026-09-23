package audio

import "math"

// Effect applies radio colouration: a bandpass that makes voice sound like
// it came through a radio, plus optional soft clipping.
//
// Presets are BUILT-IN NAMED CONFIGURATIONS, not files. The design lists
// them with .preset extensions, but there is no preset file format and no
// parser -- the filenames are identifiers. See spec D12.
type Effect struct {
	voiceID    string
	clippingID string

	hp, lp  biquad
	drive   float32
	hasBand bool
	hasClip bool
}

type bandSpec struct{ lowHz, highHz float64 }

var voiceBands = map[string]bandSpec{
	"comms_filter_low":  {200, 2400},
	"comms_filter_mid":  {300, 3000},
	"comms_filter_high": {500, 3800},
}

var clippingDrives = map[string]float32{
	"saturated_overdrive": 4.0,
	"soft_limit":          1.8,
}

// VoicePresets returns the selectable voice-effect ids, "" meaning off.
func VoicePresets() []string {
	return []string{"", "comms_filter_low", "comms_filter_mid", "comms_filter_high"}
}

// ClippingPresets returns the selectable clipping ids, "" meaning off.
func ClippingPresets() []string {
	return []string{"", "soft_limit", "saturated_overdrive"}
}

// NewEffect builds the chain. An unrecognised id degrades to passthrough
// rather than erroring: a config written by a newer version must not stop
// audio from working.
func NewEffect(voiceID, clippingID string) *Effect {
	e := &Effect{}
	if b, ok := voiceBands[voiceID]; ok {
		e.voiceID = voiceID
		e.hasBand = true
		e.hp = highpass(b.lowHz, SampleRate)
		e.lp = lowpass(b.highHz, SampleRate)
	}
	if d, ok := clippingDrives[clippingID]; ok {
		e.clippingID = clippingID
		e.hasClip = true
		e.drive = d
	}
	return e
}

// IDs reports the presets actually in effect (empty for passthrough).
func (e *Effect) IDs() (string, string) { return e.voiceID, e.clippingID }

// Process applies the chain in place.
func (e *Effect) Process(frame []float32) {
	if e.hasBand {
		for i, v := range frame {
			frame[i] = e.lp.step(e.hp.step(v))
		}
	}
	if e.hasClip {
		for i, v := range frame {
			// tanh soft clip: saturates smoothly instead of the square-edged
			// distortion a hard clamp produces.
			frame[i] = float32(math.Tanh(float64(v * e.drive)))
		}
	}
}

// biquad is a direct-form-I second-order section.
type biquad struct {
	b0, b1, b2, a1, a2 float32
	x1, x2, y1, y2     float32
}

func (f *biquad) step(x float32) float32 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// Butterworth Q for a single second-order section.
const butterworthQ = 0.7071067811865476

func lowpass(cutoffHz, sampleRate float64) biquad {
	w0 := 2 * math.Pi * cutoffHz / sampleRate
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * butterworthQ)
	a0 := 1 + alpha
	return biquad{
		b0: float32((1 - cw) / 2 / a0),
		b1: float32((1 - cw) / a0),
		b2: float32((1 - cw) / 2 / a0),
		a1: float32(-2 * cw / a0),
		a2: float32((1 - alpha) / a0),
	}
}

func highpass(cutoffHz, sampleRate float64) biquad {
	w0 := 2 * math.Pi * cutoffHz / sampleRate
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * butterworthQ)
	a0 := 1 + alpha
	return biquad{
		b0: float32((1 + cw) / 2 / a0),
		b1: float32(-(1 + cw) / a0),
		b2: float32((1 + cw) / 2 / a0),
		a1: float32(-2 * cw / a0),
		a2: float32((1 - alpha) / a0),
	}
}
