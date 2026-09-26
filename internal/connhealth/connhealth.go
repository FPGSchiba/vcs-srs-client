// Package connhealth owns the client's derived connection-health model: one
// Snapshot describing both transports -- the gRPC control plane and the UDP
// voice plane -- kept current by a single ticker.
//
// It exists so the status surface has exactly one source. Before it, the
// banner and the status bar would each have had to derive health from two
// partially-overlapping event streams, which is the frontend/backend
// divergence class this codebase already rejects for keybinds:changed and
// joystick:state.
//
// It depends only on injected function values, never on internal/session or
// internal/voice, so it is unit-testable under an injected clock with no
// sockets and no goroutines but its own.
package connhealth

import (
	"context"
	"sync"
	"time"
)

// Link state values. The control plane uses the first three, mirroring
// events.ConnectionState; the voice plane uses voice.State.String() plus
// StateUnavailable.
const (
	StateConnected    = "connected"
	StateReconnecting = "reconnecting"
	StateDisconnected = "disconnected"

	// StateUnavailable means no session is possible at all -- no voice
	// secret yet, no session, or a build whose codec is the stub. It is
	// deliberately NOT an error state: voice that never started is not
	// voice that broke. Exact precedent: JoystickState.Supported, which
	// exists so macOS's "unsupported here" never renders as a failure.
	StateUnavailable = "unavailable"
)

// RTTUnknown is the RTTMs value meaning "nothing has been measured".
//
// It is -1 rather than 0 because 0 is a value voice.Session.RTT() genuinely
// returns: before the first answered keepalive, and after every rebind,
// since resetBindingLocked clears it precisely so the UI never shows a
// healthy ping for a broken session. Rendered as a number, that is a
// plausible-looking "0ms" on a session that has measured nothing.
const RTTUnknown int64 = -1

// Defaults. Interval and FailureThreshold mirror internal/voice's
// defaultKeepalive and bindingLossThreshold exactly, so the two planes
// declare loss on the same budget and a user watching both sees them agree.
const (
	DefaultInterval         = 5 * time.Second
	DefaultFailureThreshold = 3
)

// Link is one transport plane's health.
type Link struct {
	State string `json:"state"`
	// RTTMs is the last measured round trip in milliseconds, or RTTUnknown.
	RTTMs int64 `json:"rtt_ms"`
	// Healthy reports that the plane's probe is answering. It is separate
	// from State on purpose: a probe that has begun failing while the
	// stream is still alive is a health fact, not a link fact, and folding
	// it into State would churn the three-value contract MainApp's login
	// phase gating depends on.
	Healthy bool `json:"healthy"`
	// Available is meaningful for the voice plane only; the control plane
	// always reports true.
	Available bool `json:"available"`
	// Error is the last transition's error text, "" when none.
	Error string `json:"error"`
}

// Snapshot is the whole model at a moment in time. It is a value: callers
// may hold and compare it freely.
type Snapshot struct {
	Server  string `json:"server"`
	Control Link   `json:"control"`
	Voice   Link   `json:"voice"`
}

// Options configures a Monitor. The zero value is usable: every field falls
// back to a documented default, and a nil hook is simply not called.
type Options struct {
	// Interval is the probe period. 0 means DefaultInterval.
	Interval time.Duration
	// FailureThreshold is how many consecutive failed probes declare loss.
	// 0 means DefaultFailureThreshold.
	FailureThreshold int
	// Clock is the time source. nil means time.Now.
	Clock func() time.Time

	// Ping probes the control plane, returning the round trip in
	// milliseconds. It is handed the previous measurement so the caller can
	// echo it to the server, which is the sole input to the server's
	// per-client latency map.
	Ping func(ctx context.Context, lastRTTMs int64) (int64, error)

	// VoiceRTT reads the voice session's current round trip. A zero return
	// means "not measured" and is mapped to RTTUnknown.
	VoiceRTT func() time.Duration

	// OnLoss fires once when consecutive failures reach the threshold. The
	// Monitor does NOT set the state itself: the owner emits the loss
	// through its single normal path, which comes back in via
	// SetControlState. That is what keeps one emission path and makes the
	// two detectors' snapshots impossible to disagree.
	OnLoss func()

	// OnChange receives every published Snapshot. It is called from the
	// Monitor's own goroutine and from state setters, never concurrently
	// with itself.
	OnChange func(Snapshot)
}

// Monitor owns the model and the ticker that keeps it current.
type Monitor struct {
	opt Options

	mu   sync.Mutex
	snap Snapshot
	// failures counts consecutive failed probes. Reset by any success and
	// by any transition away from connected.
	failures int
	// lossFired records that OnLoss has already been called for the current
	// connected period, so the threshold declares loss once and not on
	// every subsequent tick.
	lossFired bool

	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
	// emitMu serialises OnChange deliveries so a tick and a state setter
	// cannot interleave two snapshots out of order.
	emitMu sync.Mutex
}

// New constructs a Monitor in its honest starting state: control
// disconnected with nothing measured, voice unavailable.
func New(opt Options) *Monitor {
	if opt.Interval <= 0 {
		opt.Interval = DefaultInterval
	}
	if opt.FailureThreshold <= 0 {
		opt.FailureThreshold = DefaultFailureThreshold
	}
	if opt.Clock == nil {
		opt.Clock = time.Now
	}
	return &Monitor{
		opt:  opt,
		done: make(chan struct{}),
		snap: Snapshot{
			Control: Link{State: StateDisconnected, RTTMs: RTTUnknown, Available: true},
			Voice:   Link{State: StateUnavailable, RTTMs: RTTUnknown},
		},
	}
}

// Snapshot returns the current model.
func (m *Monitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snap
}

// SetServer records the server address the surface displays.
func (m *Monitor) SetServer(server string) {
	m.mu.Lock()
	if m.snap.Server == server {
		m.mu.Unlock()
		return
	}
	m.snap.Server = server
	snap := m.snap
	m.mu.Unlock()
	m.publish(snap)
}

// SetControlState records the control link's lifecycle state.
//
// It is called from the SAME sites that emit events.EventControlConnection,
// which is what makes the lifecycle event and the health snapshot incapable
// of disagreeing about the link.
//
// Any transition resets everything measured against the previous connection:
// RTT, the failure count and the one-shot loss latch. Carrying an RTT across
// a reconnect would show a healthy ping for a link that has not answered a
// single probe.
func (m *Monitor) SetControlState(state string) {
	m.mu.Lock()
	if m.snap.Control.State == state {
		m.mu.Unlock()
		return
	}
	m.snap.Control.State = state
	m.snap.Control.RTTMs = RTTUnknown
	m.snap.Control.Healthy = false
	m.snap.Control.Error = ""
	m.failures = 0
	m.lossFired = false
	snap := m.snap
	m.mu.Unlock()
	m.publish(snap)
}

// SetVoiceState records the voice session's lifecycle state, its last
// transition error, and whether a session is possible at all.
//
// state is voice.State.String() when available is true, and is forced to
// StateUnavailable when it is false -- an unavailable plane has no lifecycle
// to report.
func (m *Monitor) SetVoiceState(state, errMsg string, available bool) {
	if !available {
		state = StateUnavailable
	}
	m.mu.Lock()
	next := Link{
		State:     state,
		RTTMs:     RTTUnknown,
		Healthy:   available && state == StateConnected,
		Available: available,
		Error:     errMsg,
	}
	if next.Healthy {
		// Preserve a measurement already taken on this same connected
		// period; the tick will refresh it within one interval.
		next.RTTMs = m.snap.Voice.RTTMs
	}
	if m.snap.Voice == next {
		m.mu.Unlock()
		return
	}
	m.snap.Voice = next
	snap := m.snap
	m.mu.Unlock()
	m.publish(snap)
}

// publish delivers one Snapshot to OnChange, serialised so a tick and a
// state setter cannot interleave two snapshots out of order.
func (m *Monitor) publish(s Snapshot) {
	if m.opt.OnChange == nil {
		return
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.opt.OnChange(s)
}
