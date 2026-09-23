package audio

// GateConfig is the user-tunable half of the gate, mirroring the [audio]
// settings. Durations are milliseconds at the binding boundary and are
// converted to frame counts on entry.
type GateConfig struct {
	VOXEnabled        bool
	VOXThreshold      float32
	VOXMinLengthMS    int
	VOXHangMS         int
	PTTStartDelayMS   int
	PTTReleaseDelayMS int
}

// GateInput is one frame's worth of gate stimulus.
type GateInput struct {
	// PTT is the refcounted press state from internal/keybinds -- already
	// joined across keyboard and joystick sources, so holding both and
	// releasing one does not appear here as a release.
	PTT bool
	// Muted is push-to-mute or the mute toggle.
	Muted bool
	// Level is the frame RMS, measured pre- or post-NS per the
	// vox_noise_cancel setting. The gate does not care which.
	Level float32
}

// Gate decides, once per frame, whether the microphone is open.
//
// It counts FRAMES, not wall-clock time. At a fixed 10 ms cadence the two are
// equivalent, and frame counting makes every timing behaviour testable with
// no clock, no sleeps and no flakiness.
type Gate struct {
	cfg GateConfig

	startDelay   int
	releaseDelay int
	voxMinLen    int
	voxHang      int

	pttHeld     int // consecutive frames PTT has been down
	tail        int // remaining release-tail frames
	voxSustain  int // consecutive frames above threshold
	voxHangLeft int
	open        bool
}

func msToFrames(ms int) int {
	if ms <= 0 {
		return 0
	}
	return ms / int(FrameDuration.Milliseconds())
}

func NewGate(cfg GateConfig) *Gate {
	g := &Gate{}
	g.SetConfig(cfg)
	return g
}

// SetConfig re-reads the tunables. Safe to call between frames; it does not
// reset the running state, so changing a delay mid-transmission does not cut
// the user off.
func (g *Gate) SetConfig(cfg GateConfig) {
	g.cfg = cfg
	g.startDelay = msToFrames(cfg.PTTStartDelayMS)
	g.releaseDelay = msToFrames(cfg.PTTReleaseDelayMS)
	g.voxMinLen = msToFrames(cfg.VOXMinLengthMS)
	g.voxHang = msToFrames(cfg.VOXHangMS)
}

// Step advances one frame and reports whether the mic is open.
func (g *Gate) Step(in GateInput) bool {
	// PTT branch.
	pttOpen := false
	if in.PTT {
		g.pttHeld++
		// > rather than >= : a 3-frame start delay means three frames are
		// suppressed (pttHeld == 1, 2, 3) and the fourth (pttHeld == 4)
		// opens.
		if g.pttHeld > g.startDelay {
			pttOpen = true
			g.tail = g.releaseDelay
		}
	} else {
		g.pttHeld = 0
		if g.tail > 0 {
			g.tail--
			pttOpen = true
		}
	}

	// VOX branch.
	voxOpen := false
	if g.cfg.VOXEnabled {
		if in.Level >= g.cfg.VOXThreshold {
			g.voxSustain++
			// >= rather than > : a 5-frame minimum length means the fifth
			// consecutive frame above threshold (voxSustain == 5) is the one
			// that opens the gate, not the sixth.
			if g.voxSustain >= g.voxMinLen {
				voxOpen = true
				g.voxHangLeft = g.voxHang
			}
		} else {
			g.voxSustain = 0
			if g.voxHangLeft > 0 {
				g.voxHangLeft--
				voxOpen = true
			}
		}
	} else {
		g.voxSustain = 0
		g.voxHangLeft = 0
	}

	// Mute wins unconditionally. A user who hits push-to-mute expects
	// silence regardless of what else is asserting.
	g.open = (pttOpen || voxOpen) && !in.Muted
	return g.open
}
