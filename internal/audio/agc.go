package audio

import "math"

const (
	// agcTargetRMS is roughly -20 dBFS, a conventional speech operating
	// level that leaves plenty of headroom for transients.
	agcTargetRMS = 0.1
	// agcMaxGain caps amplification so a muted or unplugged microphone's
	// noise floor cannot be driven up into audible hiss.
	agcMaxGain = 20.0
	agcMinGain = 0.05
	// Attack is faster than release: coming DOWN from too-loud must happen
	// quickly to avoid clipping, while creeping UP slowly is what stops the
	// between-sentences noise swell.
	agcAttack  = 0.25
	agcRelease = 0.02
	// Below this RMS the frame is treated as silence and the gain is frozen
	// rather than chased. Without it the follower winds up to agcMaxGain
	// during every pause.
	agcSilenceFloor = 1e-4
)

// AGC is a smoothed RMS-targeting gain follower with a hard limiter.
//
// It is deliberately a follower rather than a per-frame normaliser: dividing
// each frame by its own RMS destroys the dynamics that make speech
// intelligible and pumps audibly on every consonant.
type AGC struct {
	gain float32
}

func NewAGC() *AGC { return &AGC{gain: 1.0} }

// Gain reports the current smoothed gain.
func (a *AGC) Gain() float32 { return a.gain }

// Process applies gain in place. len(frame) must be FrameSamples.
func (a *AGC) Process(frame []float32) {
	level := rms(frame)
	if level > agcSilenceFloor {
		desired := agcTargetRMS / level
		if desired > agcMaxGain {
			desired = agcMaxGain
		}
		if desired < agcMinGain {
			desired = agcMinGain
		}
		rate := float32(agcRelease)
		if desired < a.gain {
			rate = agcAttack
		}
		a.gain += (desired - a.gain) * rate
	}
	for i, v := range frame {
		s := v * a.gain
		// Hard limiter. The follower is smoothed, so it cannot react within
		// a single frame to a transient -- this is what guarantees the
		// never-exceeds-full-scale property regardless of gain history.
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		frame[i] = s
	}
}

// rms is the root-mean-square level of a frame. Shared with the gate, which
// compares it against the VOX threshold.
func rms(frame []float32) float32 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, v := range frame {
		sum += float64(v) * float64(v)
	}
	return float32(math.Sqrt(sum / float64(len(frame))))
}
