# Phase 6 — Connection-status surface — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make both transports' health observable — detect control-plane loss, measure both planes' latency, and render it in the `conn-banner` and the status bar's `dual-pill`.

**Architecture:** A new `internal/connhealth` package owns a derived dual-plane `Snapshot` and one ticker; `internal/session` reports stream termination under intent and generation guards and exposes `PingOnce`; `internal/app` turns the snapshot into a single `connection:state` event; the frontend derives banner variant and dot colours from that one event.

**Tech Stack:** Go 1.x (`log/slog`, `google.golang.org/grpc`), React 18 + TypeScript + Zustand, Wails v3 events, Vitest + Testing Library.

**Spec:** [`docs/superpowers/specs/2026-09-26-vcs-client-phase-6-connection-status-design.md`](../specs/2026-09-26-vcs-client-phase-6-connection-status-design.md)

## Global Constraints

- **Every Go invocation carries `-tags purego`.** No exceptions.
- **`GOCACHE=$TMPDIR/vcs-gocache`** on every Go command — the default GOCACHE is blocked by the sandbox.
- **Typecheck the frontend with `(cd frontend && npx tsc --noEmit)`.** `npx --prefix frontend tsc --noEmit` prints a help banner and exits 0 without checking anything.
- **`frontend/bindings/` is gitignored and goes stale across branch switches.** If `tsc` reports missing methods on the App bindings, regenerate with `wails3 generate bindings -ts -f "-tags purego" -clean=true`. Those errors are staleness, not your code.
- **`gopls` is unreliable in this repo.** Trust `go build` / `go vet`, not the language server.
- **`internal/grpctest` uses `bufconn`, not a real socket** — its tests need no sandbox exemption. Only a test that genuinely calls `bind(2)` needs `dangerouslyDisableSandbox: true`.
- **Ping interval default is `5`** (`config.Config.PingIntervalSeconds`). Failure threshold is `3`. Both mirror `internal/voice`'s `defaultKeepalive` / `bindingLossThreshold`.
- **gRPC client keepalive must be ≥60s** — the server sets `KeepaliveEnforcementPolicy{MinTime: 60s}`. Use `75s`.
- **Design classNames are byte-identical to `design/vcs/project/styles.css`.** Do not invent visuals or rename classes.
- **SonarCloud's PR gate evaluates only the PR's own new code.** Keep new code clean; do not take on the pre-existing backlog (Phase 10's job).
- **Commit after every task.** Conventional commits (`feat:`, `fix:`, `docs:`, `test:`). End every commit message with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## Review Focus

Five failure modes the spec implies that no task's happy-path tests would exercise. Each has a test pinned to the task that owns the code.

1. **A success between failures must reset the loss counter.** A link that drops one ping every 20s is not a link that is down; without a reset, failures accumulate across hours and eventually trip the threshold on a healthy connection. → Task 1, Step 11.
2. **Loss declared twice — the ping threshold and the stream's death racing.** Both detectors fire on a real cable pull. The user must see one `disconnected`, not two, and the second must not re-close an already-closed connection. → Task 2, Step 15.
3. **A pushed `connection:state` arriving before the mount-time `getConnectionState()` resolves.** The hydrate's late promise must not clobber a newer pushed snapshot — the store would silently roll back to a stale state that never updates again. → Task 5, Step 11.
4. **Voice RTT of exactly zero while connected.** `voice.Session.RTT()` returns `0` until the first keepalive is answered and after every rebind. Rendered naively that is a healthy-looking `0ms` on a session that has measured nothing. → Task 1, Step 13 (mapping) and Task 6, Step 7 (render).
5. **The reconnect button clicked twice before the first call resolves.** `App.Reconnect` re-dials and re-pushes radios; two in flight race each other's stream generation. → Task 7, Step 9.

---

## File Structure

**Created:**

| File | Responsibility |
|---|---|
| `internal/connhealth/connhealth.go` | `Link`, `Snapshot`, `Monitor`, options, state setters |
| `internal/connhealth/monitor.go` | The ticker loop, failure counting, change emission |
| `internal/connhealth/connhealth_test.go` | Unit tests under an injected clock |
| `frontend/src/shared/store/connection.ts` | `useConnection` Zustand store + derived selectors |
| `frontend/src/shared/store/useConnectionSync.ts` | Hydrate + subscribe hook (one per window shell) |
| `frontend/src/shared/store/useConnectionSync.test.tsx` | Hook tests, including StrictMode |
| `frontend/src/shared/components/StatusBar.test.tsx` | Dual-pill rendering + StrictMode |
| `frontend/src/shared/components/ConnBanner.test.tsx` | Three variants + reconnect UX + StrictMode |
| `docs/superpowers/plans/2026-09-26-phase-6-manual-verification.md` | The hardware/real-server checklist |

**Modified:**

| File | Change |
|---|---|
| `internal/session/session.go` | Stream generation, termination handling, `MarkControlLost`, `PingOnce`, `Deps.OnControlState` |
| `internal/session/dial.go` | `grpc.WithKeepaliveParams` |
| `internal/events/events.go` | `EventConnectionState`, `ConnectionHealth` emitter method |
| `internal/app/dto.go` | `ConnLinkDTO`, `ConnectionStateDTO` |
| `internal/app/app.go` | `sessionAPI` gains `PingOnce`/`MarkControlLost`; `connhealth` field; `SetConnHealth` |
| `internal/app/bindings.go` | `GetConnectionState`, `ReconnectVoice` |
| `internal/app/voice.go` | `voiceSessionAPI` gains `RTT()`; `OnState` feeds the monitor; voice availability |
| `internal/audio/assets/manifest.toml` | `[connect]` / `[disconnect]` slots |
| `main.go` | Construct and wire the `connhealth.Monitor` |
| `frontend/src/shared/api/events.ts` | `connectionState`, `voiceState` in `EV` |
| `frontend/src/shared/api/client.ts` | `getConnectionState`, `reconnectVoice`, types |
| `frontend/src/shared/components/StatusBar.tsx` | Dual-pill |
| `frontend/src/shared/components/ConnBanner.tsx` | Three variants, in-flight state, error slot |
| `frontend/src/windows/main/MainApp.tsx` | Mount `useConnectionSync`; pass `onNavigate` to StatusBar |
| `docs/PROTO_GAPS.md` | Entry #10 |
| `docs/ROADMAP.md` | Phase 6 row |

---

## Task 1: `internal/connhealth` — the derived model

**Files:**
- Create: `internal/connhealth/connhealth.go`
- Create: `internal/connhealth/monitor.go`
- Test: `internal/connhealth/connhealth_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Link struct { State string; RTTMs int64; Healthy bool; Available bool; Error string }`
  - `type Snapshot struct { Server string; Control Link; Voice Link }`
  - `type Options struct { Interval time.Duration; FailureThreshold int; Clock func() time.Time; Ping func(ctx context.Context, lastRTTMs int64) (int64, error); VoiceRTT func() time.Duration; OnLoss func(); OnChange func(Snapshot) }`
  - `func New(opt Options) *Monitor`
  - `func (m *Monitor) Start()` / `func (m *Monitor) Stop()`
  - `func (m *Monitor) Snapshot() Snapshot`
  - `func (m *Monitor) SetServer(server string)`
  - `func (m *Monitor) SetControlState(state string)`
  - `func (m *Monitor) SetVoiceState(state, errMsg string, available bool)`
  - Constants: `StateConnected = "connected"`, `StateReconnecting = "reconnecting"`, `StateDisconnected = "disconnected"`, `StateUnavailable = "unavailable"`, `RTTUnknown int64 = -1`

- [ ] **Step 1: Write the failing test for the zero value and defaults**

Create `internal/connhealth/connhealth_test.go`:

```go
package connhealth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
)

// recorder collects every Snapshot the Monitor publishes.
type recorder struct {
	mu   sync.Mutex
	snaps []connhealth.Snapshot
}

func (r *recorder) add(s connhealth.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snaps = append(r.snaps, s)
}

func (r *recorder) last() (connhealth.Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.snaps) == 0 {
		return connhealth.Snapshot{}, false
	}
	return r.snaps[len(r.snaps)-1], true
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.snaps)
}

func TestNew_StartsDisconnectedWithUnknownRTT(t *testing.T) {
	m := connhealth.New(connhealth.Options{})
	got := m.Snapshot()

	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateDisconnected)
	}
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d, want %d", got.Control.RTTMs, connhealth.RTTUnknown)
	}
	// Healthy is optimistic-false until a probe has actually answered:
	// claiming health nothing has confirmed is the failure mode
	// useSettingsSync's getHotkeyState comment already calls out.
	if got.Control.Healthy {
		t.Error("Control.Healthy = true on a fresh Monitor, want false")
	}
	if got.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q, want %q", got.Voice.State, connhealth.StateUnavailable)
	}
	if got.Voice.Available {
		t.Error("Voice.Available = true on a fresh Monitor, want false")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -run TestNew_StartsDisconnected -v`
Expected: FAIL — the package does not exist (`no Go files in .../internal/connhealth`).

- [ ] **Step 3: Write the types and constructor**

Create `internal/connhealth/connhealth.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -run TestNew_StartsDisconnected -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for state setters and change publication**

Append to `internal/connhealth/connhealth_test.go`:

```go
func TestSetControlState_PublishesAndResetsProbeState(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetControlState(connhealth.StateConnected)

	got, ok := rec.last()
	if !ok {
		t.Fatal("SetControlState published nothing")
	}
	if got.Control.State != connhealth.StateConnected {
		t.Errorf("Control.State = %q, want %q", got.Control.State, connhealth.StateConnected)
	}
	// A fresh connection has measured nothing yet -- carrying the previous
	// connection's RTT forward would show a healthy ping for a link that has
	// not answered once. Same reasoning as voice.Session.resetBindingLocked.
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d after reconnect, want %d", got.Control.RTTMs, connhealth.RTTUnknown)
	}
}

func TestSetVoiceState_CarriesErrorAndAvailability(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetVoiceState("retrying", "voice: no HELLO_ACK after 5 attempts", true)

	got, _ := rec.last()
	if got.Voice.State != "retrying" {
		t.Errorf("Voice.State = %q, want %q", got.Voice.State, "retrying")
	}
	if got.Voice.Error != "voice: no HELLO_ACK after 5 attempts" {
		t.Errorf("Voice.Error = %q, want the transition error", got.Voice.Error)
	}
	if !got.Voice.Available {
		t.Error("Voice.Available = false, want true")
	}
}

func TestSetVoiceState_UnavailableForcesUnknownRTT(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		VoiceRTT: func() time.Duration { return 42 * time.Millisecond },
	})

	m.SetVoiceState(connhealth.StateConnected, "", true)
	m.SetVoiceState(connhealth.StateUnavailable, "", false)

	got, _ := rec.last()
	if got.Voice.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Voice.RTTMs = %d while unavailable, want %d", got.Voice.RTTMs, connhealth.RTTUnknown)
	}
	if got.Voice.Healthy {
		t.Error("Voice.Healthy = true while unavailable, want false")
	}
}

func TestSetServer_Publishes(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetServer("127.0.0.1:5002")

	got, _ := rec.last()
	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
}

func TestSetControlState_NoOpWhenUnchanged(t *testing.T) {
	// Re-asserting the state the Monitor is already in must not publish: the
	// control path calls SetControlState on every emission, including the
	// redundant reconnecting -> reconnecting a failed Reconnect produces.
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{OnChange: rec.add})

	m.SetControlState(connhealth.StateConnected)
	before := rec.count()
	m.SetControlState(connhealth.StateConnected)

	if rec.count() != before {
		t.Errorf("published %d times for a repeated state, want %d", rec.count(), before)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -v`
Expected: FAIL — `m.SetControlState undefined`, `m.SetVoiceState undefined`, `m.SetServer undefined`.

- [ ] **Step 7: Implement the state setters**

Append to `internal/connhealth/connhealth.go`:

```go
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
```

- [ ] **Step 8: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -v`
Expected: PASS (all five tests)

- [ ] **Step 9: Write the failing test for the ticker, failure counting and loss**

Append to `internal/connhealth/connhealth_test.go`:

```go
// tickFor drives one probe cycle synchronously. Tick is exported for exactly
// this: the loop is a thin wrapper around it, so tests never sleep.
func TestTick_SuccessRecordsRTTAndHealth(t *testing.T) {
	rec := &recorder{}
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 8, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())

	got, _ := rec.last()
	if got.Control.RTTMs != 8 {
		t.Errorf("Control.RTTMs = %d, want 8", got.Control.RTTMs)
	}
	if !got.Control.Healthy {
		t.Error("Control.Healthy = false after a successful probe, want true")
	}
}

func TestTick_EchoesPreviousRTTToPing(t *testing.T) {
	// The echo is the sole input to the server's per-client latency map
	// (srs/srs_service.go Ping writes client.LatencyToControlMs = LastRttMs),
	// so dropping it leaves that map at zero for every VCS client forever.
	var seen []int64
	m := connhealth.New(connhealth.Options{
		Ping: func(_ context.Context, last int64) (int64, error) {
			seen = append(seen, last)
			return 12, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())
	m.Tick(context.Background())

	if len(seen) != 2 {
		t.Fatalf("Ping called %d times, want 2", len(seen))
	}
	if seen[0] != connhealth.RTTUnknown {
		t.Errorf("first echo = %d, want %d", seen[0], connhealth.RTTUnknown)
	}
	if seen[1] != 12 {
		t.Errorf("second echo = %d, want 12 (the first probe's measurement)", seen[1])
	}
}

func TestTick_FailuresBelowThresholdOnlyClearHealth(t *testing.T) {
	rec := &recorder{}
	lost := 0
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnChange:         rec.add,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("deadline exceeded")
		},
	})
	m.SetControlState(connhealth.StateConnected)

	m.Tick(context.Background())
	m.Tick(context.Background())

	got, _ := rec.last()
	if got.Control.State != connhealth.StateConnected {
		t.Errorf("Control.State = %q after 2 failures, want it unchanged at %q",
			got.Control.State, connhealth.StateConnected)
	}
	if got.Control.Healthy {
		t.Error("Control.Healthy = true after a failed probe, want false")
	}
	if lost != 0 {
		t.Errorf("OnLoss fired %d times below the threshold, want 0", lost)
	}
}

func TestTick_ThresholdFiresLossExactlyOnce(t *testing.T) {
	lost := 0
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			return 0, errors.New("deadline exceeded")
		},
	})
	m.SetControlState(connhealth.StateConnected)

	for i := 0; i < 6; i++ {
		m.Tick(context.Background())
	}

	if lost != 1 {
		t.Errorf("OnLoss fired %d times, want exactly 1", lost)
	}
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -run TestTick -v`
Expected: FAIL — `m.Tick undefined`.

- [ ] **Step 11: Write the failing test for the counter reset (Review Focus #1) and the skip-while-disconnected rule**

Append to `internal/connhealth/connhealth_test.go`:

```go
// Review Focus #1. A link that drops one probe every so often is not a link
// that is down. Without a reset on success, failures accumulate across the
// whole session and eventually declare loss on a healthy connection.
func TestTick_SuccessResetsTheFailureCounter(t *testing.T) {
	lost := 0
	fail := true
	m := connhealth.New(connhealth.Options{
		FailureThreshold: 3,
		OnLoss:           func() { lost++ },
		Ping: func(_ context.Context, _ int64) (int64, error) {
			if fail {
				return 0, errors.New("deadline exceeded")
			}
			return 7, nil
		},
	})
	m.SetControlState(connhealth.StateConnected)

	// Two failures, one success, two failures. Five ticks, never three in a
	// row, so loss must never be declared.
	m.Tick(context.Background())
	m.Tick(context.Background())
	fail = false
	m.Tick(context.Background())
	fail = true
	m.Tick(context.Background())
	m.Tick(context.Background())

	if lost != 0 {
		t.Errorf("OnLoss fired %d times across a flapping link, want 0", lost)
	}
}

func TestTick_DoesNotProbeWhileDisconnected(t *testing.T) {
	// Hammering a server we already know is gone is pure noise, and a probe
	// against a nil control client can only ever return an error, which
	// would re-fire loss on a link already reported lost.
	calls := 0
	m := connhealth.New(connhealth.Options{
		Ping: func(_ context.Context, _ int64) (int64, error) {
			calls++
			return 1, nil
		},
	})
	// Never set connected: the Monitor starts disconnected.

	m.Tick(context.Background())
	m.Tick(context.Background())

	if calls != 0 {
		t.Errorf("Ping called %d times while disconnected, want 0", calls)
	}
}
```

- [ ] **Step 12: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -run TestTick -v`
Expected: FAIL — `m.Tick undefined`.

- [ ] **Step 13: Write the failing test for voice RTT mapping (Review Focus #4)**

Append to `internal/connhealth/connhealth_test.go`:

```go
// Review Focus #4. voice.Session.RTT() returns 0 before the first answered
// keepalive and after every rebind (resetBindingLocked clears it precisely
// so the UI never shows a healthy ping for a broken session). Passed through
// as a number that is a plausible-looking "0ms".
func TestTick_VoiceZeroRTTIsUnknownNotZero(t *testing.T) {
	rec := &recorder{}
	rtt := time.Duration(0)
	m := connhealth.New(connhealth.Options{
		OnChange: rec.add,
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 3, nil },
		VoiceRTT: func() time.Duration { return rtt },
	})
	m.SetControlState(connhealth.StateConnected)
	m.SetVoiceState(connhealth.StateConnected, "", true)

	m.Tick(context.Background())
	got, _ := rec.last()
	if got.Voice.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Voice.RTTMs = %d for a zero RTT, want %d", got.Voice.RTTMs, connhealth.RTTUnknown)
	}

	rtt = 24 * time.Millisecond
	m.Tick(context.Background())
	got, _ = rec.last()
	if got.Voice.RTTMs != 24 {
		t.Errorf("Voice.RTTMs = %d, want 24", got.Voice.RTTMs)
	}
}

func TestTick_VoiceRTTNotReadWhileUnavailable(t *testing.T) {
	calls := 0
	m := connhealth.New(connhealth.Options{
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 3, nil },
		VoiceRTT: func() time.Duration { calls++; return 5 * time.Millisecond },
	})
	m.SetControlState(connhealth.StateConnected)
	m.SetVoiceState(connhealth.StateUnavailable, "", false)

	m.Tick(context.Background())

	if calls != 0 {
		t.Errorf("VoiceRTT called %d times while unavailable, want 0", calls)
	}
}
```

- [ ] **Step 14: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/connhealth/ -run TestTick -v`
Expected: FAIL — `m.Tick undefined`.

- [ ] **Step 15: Implement the ticker**

Create `internal/connhealth/monitor.go`:

```go
package connhealth

import (
	"context"
	"time"
)

// Start launches the probe loop. Safe to call more than once; only the first
// call does anything. A Monitor with no Ping hook still starts -- the loop
// simply has nothing to probe -- so callers need no special case for a
// backend that is not wired.
func (m *Monitor) Start() {
	m.startOnce.Do(func() {
		m.wg.Add(1)
		go m.loop()
	})
}

// Stop halts the probe loop and waits for it. Safe to call more than once
// and from any goroutine.
func (m *Monitor) Stop() {
	m.stopOnce.Do(func() { close(m.done) })
	m.wg.Wait()
}

// loop is a thin wrapper around Tick. All of the policy lives in Tick so
// tests can drive a whole probe cycle synchronously, with no sleeping and no
// injected timer.
func (m *Monitor) loop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.opt.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			// Bounded by one interval. PingOnce takes a ctx but nothing
			// else bounds it, and an unbounded unary call on a half-open
			// connection blocks forever -- which is precisely the case
			// this probe exists to detect.
			ctx, cancel := context.WithTimeout(context.Background(), m.opt.Interval)
			m.Tick(ctx)
			cancel()
		}
	}
}

// Tick runs one probe cycle: probe the control plane, read the voice plane's
// round trip, and publish the result if anything changed.
//
// Exported because it is the whole of the loop's behaviour. Driving it
// directly is what lets the tests cover the failure ladder without a clock.
func (m *Monitor) Tick(ctx context.Context) {
	m.mu.Lock()
	connected := m.snap.Control.State == StateConnected
	lastRTT := m.snap.Control.RTTMs
	voiceAvailable := m.snap.Voice.Available
	voiceConnected := voiceAvailable && m.snap.Voice.State == StateConnected
	m.mu.Unlock()

	if !connected {
		// Nothing to probe. Hammering a server already known to be gone is
		// noise, and a probe against a nil control client can only fail,
		// which would re-fire loss on a link already reported lost.
		return
	}

	var (
		rttMs   int64
		probeOK bool
	)
	if m.opt.Ping != nil {
		if measured, err := m.opt.Ping(ctx, lastRTT); err == nil {
			rttMs, probeOK = measured, true
		}
	}

	var voiceRTT int64 = RTTUnknown
	if voiceConnected && m.opt.VoiceRTT != nil {
		// Zero means "not measured" -- see RTTUnknown's doc.
		if d := m.opt.VoiceRTT(); d > 0 {
			voiceRTT = d.Milliseconds()
		}
	}

	m.mu.Lock()
	before := m.snap
	if probeOK {
		m.snap.Control.RTTMs = rttMs
		m.snap.Control.Healthy = true
		m.snap.Control.Error = ""
		m.failures = 0
	} else {
		m.snap.Control.Healthy = false
		m.failures++
	}
	m.snap.Voice.RTTMs = voiceRTT

	fireLoss := !probeOK && m.failures >= m.opt.FailureThreshold && !m.lossFired
	if fireLoss {
		// Latched so the threshold declares loss once, not on every tick
		// after it. Cleared by the next SetControlState transition.
		m.lossFired = true
	}
	changed := m.snap != before
	snap := m.snap
	m.mu.Unlock()

	if changed {
		m.publish(snap)
	}
	if fireLoss && m.opt.OnLoss != nil {
		// The Monitor does NOT set disconnected itself: the owner emits the
		// loss through its single normal path, which comes back in via
		// SetControlState. One emission path, two detectors, no disagreement.
		m.opt.OnLoss()
	}
}
```

- [ ] **Step 16: Run the whole package and the race detector**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/connhealth/ -v`
Expected: PASS — all tests, no race reports.

- [ ] **Step 17: Write the failing test for Start/Stop lifecycle**

Append to `internal/connhealth/connhealth_test.go`:

```go
func TestStartStop_IsIdempotentAndJoins(t *testing.T) {
	m := connhealth.New(connhealth.Options{
		Interval: 5 * time.Millisecond,
		Ping:     func(_ context.Context, _ int64) (int64, error) { return 1, nil },
	})
	m.SetControlState(connhealth.StateConnected)

	m.Start()
	m.Start() // second call is a no-op, not a second goroutine

	// Give the loop a chance to run at least once. This is the only test in
	// the package that touches the real clock; everything else drives Tick.
	time.Sleep(30 * time.Millisecond)

	m.Stop()
	m.Stop() // second call must not panic on an already-closed channel

	if got := m.Snapshot(); got.Control.RTTMs != 1 {
		t.Errorf("Control.RTTMs = %d after the loop ran, want 1", got.Control.RTTMs)
	}
}
```

- [ ] **Step 18: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/connhealth/ -v`
Expected: PASS

- [ ] **Step 19: Vet and commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./internal/connhealth/
git add internal/connhealth/
git commit -m "feat(connhealth): derived dual-plane connection-health model

One owner, one Snapshot, one ticker. The banner and the status bar would
otherwise each derive health from two partially-overlapping event streams,
which is the divergence class this codebase already rejects for
keybinds:changed and joystick:state.

Three things the model is deliberate about:

- RTTUnknown is -1, not 0. voice.Session.RTT() genuinely returns 0 before
  the first answered keepalive and after every rebind, so 0 as a number is
  a plausible-looking healthy ping on a session that has measured nothing.
- Healthy is separate from State. A probe that has begun failing while the
  stream is still alive is a health fact, not a link fact, and folding it
  into State would churn the three-value contract MainApp's login phase
  gating depends on.
- OnLoss does not set disconnected itself. The owner emits it through its
  single normal path, which comes back in via SetControlState -- so the two
  detectors cannot produce disagreeing snapshots.

Interval 5s and threshold 3 mirror internal/voice's defaultKeepalive and
bindingLossThreshold exactly, so both planes declare loss on the same budget.

Tick is exported because it is the whole of the loop's behaviour; the tests
drive it directly and never sleep, except the one Start/Stop lifecycle case.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: `internal/session` — liveness detection

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/dial.go`
- Test: `internal/session/liveness_test.go` (create)

**Interfaces:**
- Consumes: `connhealth.StateConnected` / `StateReconnecting` / `StateDisconnected` (Task 1) — used only as string values by the caller; `session` itself keeps using `events.ConnectionState`.
- Produces:
  - `Deps.OnControlState func(events.ConnectionState)` — optional observer, called on every control transition.
  - `func (s *Session) PingOnce(ctx context.Context, lastRTTMs int64) (int64, error)`
  - `func (s *Session) MarkControlLost()`

- [ ] **Step 1: Write the failing test for an unexpected stream drop**

Create `internal/session/liveness_test.go`:

```go
package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/events"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// stateRecorder collects every control transition the session reports
// through Deps.OnControlState.
type stateRecorder struct {
	mu     sync.Mutex
	states []events.ConnectionState
}

func (r *stateRecorder) add(s events.ConnectionState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, s)
}

func (r *stateRecorder) all() []events.ConnectionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.ConnectionState(nil), r.states...)
}

func (r *stateRecorder) countOf(want events.ConnectionState) int {
	n := 0
	for _, s := range r.all() {
		if s == want {
			n++
		}
	}
	return n
}

// waitFor polls cond until it holds or the deadline passes. The stream
// goroutine reports asynchronously, so there is nothing to synchronise on
// from the test side.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func TestStreamDrop_EmitsDisconnected(t *testing.T) {
	// The core Phase 6 gap (G1): before this, ConsumeUpdates' terminating
	// error went into `_ =` and the UI reported connected indefinitely.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	f.CloseStream() // the server drops the subscription

	if !waitFor(t, func() bool { return rec.countOf(events.ConnDisconnected) == 1 }) {
		t.Fatalf("no disconnected after the stream dropped; saw %v", rec.all())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/session/ -run TestStreamDrop -v`
Expected: FAIL — `unknown field OnControlState in struct literal`.

- [ ] **Step 3: Add the observer hook and route every transition through it**

In `internal/session/session.go`, add to `Deps`:

```go
// Deps are injected dependencies (the Dialer is overridable in tests).
type Deps struct {
	Dialer  Dialer // if nil, Connect builds an insecure-localhost dialer from serverURL
	Version string

	// OnControlState observes every control-link transition, alongside the
	// events.EventControlConnection emission. It exists so the connection-
	// health model is fed from the SAME call sites that emit the lifecycle
	// event -- which is what makes the two incapable of disagreeing about
	// the link. Optional; nil is a no-op.
	//
	// Called synchronously on the transitioning goroutine, so it must not
	// block. connhealth.Monitor.SetControlState satisfies that.
	OnControlState func(events.ConnectionState)
}
```

Add a single transition helper, and replace **every** `tagged.ConnectionState(...)` call in the file with it:

```go
// setConnState is the one place a control transition is published. Both the
// lifecycle event and the health observer are driven from here so no call
// site can ever feed one without the other.
func (s *Session) setConnState(st events.ConnectionState) {
	events.New(s.em).ConnectionState(st)
	if s.dep.OnControlState != nil {
		s.dep.OnControlState(st)
	}
}
```

Replacements (there are six):
- `Connect`: `tagged.ConnectionState(events.ConnReconnecting)` → `s.setConnState(events.ConnReconnecting)`
- `Connect`: `tagged.ConnectionState(events.ConnConnected)` → `s.setConnState(events.ConnConnected)`
- `Disconnect`: `tagged.ConnectionState(events.ConnDisconnected)` → `s.setConnState(events.ConnDisconnected)`
- `Reconnect`: the opening `tagged.ConnectionState(events.ConnReconnecting)` → `s.setConnState(events.ConnReconnecting)`
- `Reconnect`: all three `tagged.ConnectionState(events.ConnDisconnected)` failure paths → `s.setConnState(events.ConnDisconnected)`
- `Reconnect`: the closing `tagged.ConnectionState(events.ConnConnected)` → `s.setConnState(events.ConnConnected)`

Keep the `tagged` local only where it is still used for `SessionChanged`.

- [ ] **Step 4: Add the stream generation and the termination handler**

In `internal/session/session.go`, add to the `Session` struct:

```go
	// streamGen identifies the current update stream. A goroutine captures
	// it at launch and compares before reporting termination, so a stale
	// stream from a previous connection cannot report disconnected over a
	// healthy new one.
	//
	// This is the same pattern internal/app/voice.go uses for voice session
	// generations, and for the same reason: the generation must be captured
	// BEFORE the goroutine is scheduled, not inside it.
	streamGen uint64
```

Add the handler and the launcher:

```go
// startStream launches the update-stream consumer and returns the generation
// it was launched under. Caller holds s.mu.
func (s *Session) startStreamLocked(ctx context.Context, cc *control.Client) {
	s.streamGen++
	gen := s.streamGen
	go func() {
		err := cc.ConsumeUpdates(ctx, s.st, s.em)
		s.handleStreamEnd(ctx, gen, err)
	}()
}

// handleStreamEnd reports an update stream's termination as a control-link
// loss -- unless we caused it, or it belongs to a connection that has since
// been replaced.
//
// Before this existed the error went into `_ =` at both call sites, so a
// server restart, a network drop, or the server's SubscribeToUpdates
// refusing a duplicate subscription all left the UI reporting connected
// indefinitely, with ConnBanner's disconnected variant sitting behind a
// signal that never arrived.
func (s *Session) handleStreamEnd(ctx context.Context, gen uint64, err error) {
	// Intent guard. A cancelled context means Disconnect or Reconnect ended
	// this stream on purpose; that path publishes its own state, and
	// emitting here would stack a spurious disconnected on top of a
	// teardown or a reconnect already under way.
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.log("update stream ended", err)
	}
	s.markControlLost(gen)
}

// log reports a stream-level problem. Session has no logger of its own, and
// adding one is out of this phase's scope; slog.Default is routed to the
// rotating file by main.go.
func (s *Session) log(msg string, err error) {
	slog.Default().Warn("session: "+msg, "err", err)
}
```

Add `"log/slog"` to the imports.

- [ ] **Step 5: Implement `markControlLost` and its exported wrapper**

```go
// MarkControlLost declares the control link dead from outside the stream
// goroutine -- the probe detector's entry point, called when consecutive
// pings have failed past the threshold.
//
// It is idempotent against the stream-termination path: both detectors fire
// on a real cable pull, and the user must see one disconnected, not two.
func (s *Session) MarkControlLost() {
	s.mu.Lock()
	gen := s.streamGen
	s.mu.Unlock()
	s.markControlLost(gen)
}

// markControlLost tears the dead connection down and publishes the loss,
// exactly once per generation.
//
// Closing the connection here is what makes the second detector's call a
// no-op: whichever of the two arrives first bumps the generation, and the
// other's captured generation no longer matches.
func (s *Session) markControlLost(gen uint64) {
	s.mu.Lock()
	if gen != s.streamGen || s.conn == nil {
		// Already handled by the other detector, or belongs to a connection
		// that has since been replaced.
		s.mu.Unlock()
		return
	}
	// Bump so the losing detector's captured generation goes stale.
	s.streamGen++
	cancel := s.cancel
	conn := s.conn
	s.conn, s.control, s.cancel = nil, nil, nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if conn != nil {
		_ = conn.Close()
	}
	s.setConnState(events.ConnDisconnected)
}
```

- [ ] **Step 6: Replace both `ConsumeUpdates` launch sites**

In `Connect`, replace:

```go
	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.mu.Unlock()

	s.mu.Lock()
	s.lastDialer = dialer
	s.lastToken = guest.Token
	s.mu.Unlock()

	go func() { _ = cc.ConsumeUpdates(streamCtx, s.st, s.em) }()
```

with:

```go
	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.lastDialer = dialer
	s.lastToken = guest.Token
	s.startStreamLocked(streamCtx, cc)
	s.mu.Unlock()
```

In `Reconnect`, replace:

```go
	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.mu.Unlock()

	go func() { _ = cc.ConsumeUpdates(streamCtx, s.st, s.em) }()
```

with:

```go
	streamCtx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn, s.control, s.cancel = conn, cc, cancel
	s.startStreamLocked(streamCtx, cc)
	s.mu.Unlock()
```

In `Disconnect`, the existing `cancel()` already sets `streamCtx.Err()`, which the intent guard reads — but it must also bump the generation so a slow stream goroutine cannot report over a later connection. Replace the existing field-clearing block:

```go
	s.mu.Lock()
	conn, cc, cancel := s.conn, s.control, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.mu.Unlock()
```

with:

```go
	s.mu.Lock()
	conn, cc, cancel := s.conn, s.control, s.cancel
	s.conn, s.control, s.cancel = nil, nil, nil
	s.streamGen++ // retire this stream's generation
	s.mu.Unlock()
```

- [ ] **Step 7: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/ -run TestStreamDrop -v`
Expected: PASS

- [ ] **Step 8: Write the failing test for the intent guard**

Append to `internal/session/liveness_test.go`:

```go
func TestDisconnect_DoesNotDoubleReportLoss(t *testing.T) {
	// Disconnect publishes its own disconnected. If the stream goroutine
	// also reported one, every clean logout would show the user a
	// DISCONNECTED banner on the way out.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Disconnect(context.Background()); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	// Give any stray goroutine every chance to misbehave.
	time.Sleep(100 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times on a clean logout, want 1; saw %v",
			got, rec.all())
	}
}

func TestReconnect_StaleStreamCannotReportOverTheNewOne(t *testing.T) {
	// The generation guard. Reconnect cancels the old stream and opens a
	// new one; without the guard the old goroutine's termination lands on
	// the fresh, healthy connection and reports it dead.
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := s.Reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	states := rec.all()
	if len(states) == 0 || states[len(states)-1] != events.ConnConnected {
		t.Errorf("final state = %v, want connected after a successful reconnect", states)
	}
}
```

- [ ] **Step 9: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/ -v`
Expected: PASS — all tests in the package, including the pre-existing ones.

- [ ] **Step 10: Write the failing test for `PingOnce`**

Append to `internal/session/liveness_test.go`:

```go
func TestPingOnce_MeasuresWhileConnected(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	s := session.New(state.New(), &capEmitter{}, session.Deps{Dialer: dial, Version: "test"})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	rtt, err := s.PingOnce(context.Background(), -1)
	if err != nil {
		t.Fatalf("PingOnce: %v", err)
	}
	if rtt < 0 {
		t.Errorf("PingOnce returned %d ms, want a non-negative measurement", rtt)
	}
}

func TestPingOnce_ErrorsWhileDisconnected(t *testing.T) {
	// The Monitor skips probing while disconnected, but a probe racing a
	// teardown must still fail cleanly rather than nil-deref the control
	// client.
	s := session.New(state.New(), &capEmitter{}, session.Deps{Version: "test"})

	if _, err := s.PingOnce(context.Background(), -1); err == nil {
		t.Fatal("PingOnce() = nil error while never connected, want an error")
	}
}
```

- [ ] **Step 11: Implement `PingOnce`**

Append to `internal/session/session.go`:

```go
// PingOnce probes the control plane through the live control client,
// returning the round trip in milliseconds and echoing lastRTTMs to the
// server.
//
// The echo is not decoration: srs_service.go's Ping handler writes
// client.LatencyToControlMs = req.LastRttMs and nothing else ever does, so
// dropping it leaves the server's per-client latency map at zero for every
// VCS client.
//
// Returns an error when no control client is live, which is what a probe
// racing a teardown sees.
func (s *Session) PingOnce(ctx context.Context, lastRTTMs int64) (int64, error) {
	s.mu.Lock()
	cc := s.control
	s.mu.Unlock()
	if cc == nil {
		return 0, fmt.Errorf("ping: not connected")
	}
	return cc.PingOnce(ctx, lastRTTMs)
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/ -v`
Expected: PASS

- [ ] **Step 13: Add the gRPC client keepalive**

In `internal/session/dial.go`, add imports `"time"` and `"google.golang.org/grpc/keepalive"`, and a documented constant block plus the dial option:

```go
// clientKeepalive* are the gRPC transport keepalive parameters.
//
// The server sets KeepaliveEnforcementPolicy{MinTime: 60s} on the gRPC
// server that serves SRSService (vngd-srs-server control/server.go), so a
// client pinging more often than that earns a GOAWAY with too_many_pings and
// loses the connection -- an availability feature turned into an outage.
// 75s leaves margin against that floor, since a ping arriving marginally
// early still counts against the policy.
//
// This is a BACKSTOP, not the detector. The server's own ServerParameters
// already drop a dead client at roughly 70s; the application-level Ping in
// connhealth answers in about 15s, and that is what the status surface
// depends on. This only covers the idle case more cheaply.
const (
	clientKeepaliveTime    = 75 * time.Second
	clientKeepaliveTimeout = 10 * time.Second
)

func keepaliveOption() grpc.DialOption {
	return grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:    clientKeepaliveTime,
		Timeout: clientKeepaliveTimeout,
		// Matches the server's PermitWithoutStream: true. The update stream
		// is long-lived, so this rarely matters, but it is the correct
		// pairing and costs nothing.
		PermitWithoutStream: true,
	})
}
```

And add it to the dialer:

```go
	return func(ctx context.Context) (*grpc.ClientConn, error) {
		return grpc.DialContext(ctx, serverURL,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			keepaliveOption(),
			grpc.WithBlock(),
		)
	}, nil
```

- [ ] **Step 14: Write the failing test for the keepalive floor**

Append to `internal/session/liveness_test.go`:

```go
func TestKeepaliveRespectsTheServersEnforcementFloor(t *testing.T) {
	// The server sets KeepaliveEnforcementPolicy{MinTime: 60s}. A client
	// configured below that floor is not merely suboptimal: gRPC answers
	// with GOAWAY too_many_pings and kills the connection. Pinned as a
	// constant test because the value is a cross-repo contract that nothing
	// else in this repo would catch drifting.
	if session.ClientKeepaliveTime() < 60*time.Second {
		t.Errorf("client keepalive Time = %v, want >= 60s (the server's MinTime)",
			session.ClientKeepaliveTime())
	}
}
```

Add the exported accessor to `internal/session/dial.go`:

```go
// ClientKeepaliveTime exposes the configured keepalive period so a test can
// pin it against the server's enforcement floor. See clientKeepaliveTime.
func ClientKeepaliveTime() time.Duration { return clientKeepaliveTime }
```

- [ ] **Step 15: Write the failing test for double-loss (Review Focus #2)**

Append to `internal/session/liveness_test.go`:

```go
// Review Focus #2. On a real cable pull both detectors fire: the probe
// threshold and the stream's own death. The user must see one disconnected,
// and the second path must not re-close an already-closed connection.
func TestBothDetectors_ReportLossExactlyOnce(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Both detectors, as close to simultaneously as the test can arrange.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.MarkControlLost() }()
	go func() { defer wg.Done(); f.CloseStream() }()
	wg.Wait()

	time.Sleep(150 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times, want exactly 1; saw %v", got, rec.all())
	}
}

func TestMarkControlLost_IsIdempotent(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()

	rec := &stateRecorder{}
	s := session.New(state.New(), &capEmitter{}, session.Deps{
		Dialer:         dial,
		Version:        "test",
		OnControlState: rec.add,
	})
	if err := s.Connect(context.Background(), "localhost:0", "n", "p", "u"); err != nil {
		t.Fatalf("connect: %v", err)
	}

	s.MarkControlLost()
	s.MarkControlLost()
	s.MarkControlLost()

	time.Sleep(100 * time.Millisecond)

	if got := rec.countOf(events.ConnDisconnected); got != 1 {
		t.Errorf("disconnected reported %d times for three calls, want 1", got)
	}
}
```

- [ ] **Step 16: Run the whole package under race**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/session/ ./internal/control/ -v`
Expected: PASS — all tests, no races.

- [ ] **Step 17: Build, vet, and commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...
git add internal/session/
git commit -m "fix(session): stop discarding the control stream's death

Both ConsumeUpdates goroutines -- Connect's and Reconnect's -- put the
stream's terminating error into \`_ =\`. A server restart, a network drop, or
the server's SubscribeToUpdates refusing a duplicate subscription all left
the UI reporting connected indefinitely, with ConnBanner's disconnected
variant sitting behind a signal that could never arrive.

Termination now reports, under two guards that are both load-bearing:

- Intent: a cancelled stream context means Disconnect or Reconnect ended
  this stream on purpose. Without it, every clean logout would flash a
  DISCONNECTED banner on the way out.
- Generation: captured before the goroutine is scheduled, compared before
  reporting. Without it, the stream Reconnect just cancelled reports the
  fresh, healthy connection dead. Same pattern as internal/app/voice.go's
  voice generations, and for the same reason.

MarkControlLost gives the probe detector the same entry point, idempotent
against the stream path: both fire on a real cable pull, and whichever
arrives first bumps the generation so the other becomes a no-op.

PingOnce finally gets a caller path. Its lastRTTMs echo is the sole input to
the server's per-client latency map, which has read zero for every VCS client
since Phase 1.

Deps.OnControlState feeds the health model from the same call sites that emit
the lifecycle event, which is what makes the two incapable of disagreeing.

Client keepalive is set at 75s, respecting the server's
KeepaliveEnforcementPolicy MinTime of 60s -- below that floor gRPC answers
GOAWAY too_many_pings and kills the connection. It is a backstop for the idle
case, not the detector.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `internal/app` — expose connection health

**Files:**
- Modify: `internal/events/events.go`
- Modify: `internal/app/dto.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/bindings.go`
- Modify: `internal/app/voice.go`
- Modify: `main.go`
- Test: `internal/app/connhealth_test.go` (create)

**Interfaces:**
- Consumes: `connhealth.Monitor` and all its methods (Task 1); `session.Session.PingOnce` / `MarkControlLost` / `Deps.OnControlState` (Task 2).
- Produces:
  - `events.EventConnectionState = "connection:state"`; `func (t *Tagged) ConnectionHealth(payload any)`
  - `type ConnLinkDTO struct { State string; RTTMs int64; Healthy bool; Available bool; Error string }` (json: `state`, `rtt_ms`, `healthy`, `available`, `error`)
  - `type ConnectionStateDTO struct { Server string; Control ConnLinkDTO; Voice ConnLinkDTO }` (json: `server`, `control`, `voice`)
  - `func (a *App) GetConnectionState() ConnectionStateDTO`
  - `func (a *App) ReconnectVoice() error`
  - `func (a *App) SetConnHealth(m *connhealth.Monitor)`
  - `voiceSessionAPI` gains `RTT() time.Duration`

- [ ] **Step 1: Write the failing test for the DTO conversion**

Create `internal/app/connhealth_test.go`:

```go
package app

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

func TestConnectionStateDTOFrom_MapsEveryField(t *testing.T) {
	snap := connhealth.Snapshot{
		Server: "127.0.0.1:5002",
		Control: connhealth.Link{
			State: connhealth.StateConnected, RTTMs: 8, Healthy: true, Available: true,
		},
		Voice: connhealth.Link{
			State: "retrying", RTTMs: connhealth.RTTUnknown, Available: true,
			Error: "voice: no HELLO_ACK after 5 attempts",
		},
	}

	got := ConnectionStateDTOFrom(snap)

	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
	if got.Control.State != "connected" || got.Control.RTTMs != 8 || !got.Control.Healthy {
		t.Errorf("Control = %+v, want connected/8/healthy", got.Control)
	}
	if got.Voice.State != "retrying" || got.Voice.RTTMs != -1 {
		t.Errorf("Voice = %+v, want retrying/-1", got.Voice)
	}
	if got.Voice.Error != "voice: no HELLO_ACK after 5 attempts" {
		t.Errorf("Voice.Error = %q, want the transition error", got.Voice.Error)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestConnectionStateDTOFrom -v`
Expected: FAIL — `undefined: ConnectionStateDTOFrom`.

- [ ] **Step 3: Add the event name and emitter method**

In `internal/events/events.go`, add to the event-name const block:

```go
	// EventConnectionState is the connection-health snapshot both planes
	// feed: the single source for the status surface (the conn-banner and
	// the status bar's dual-pill).
	//
	// It is deliberately SEPARATE from EventControlConnection rather than
	// replacing it. They answer different questions -- that one is a
	// lifecycle signal driving MainApp's login phase gating, this one is a
	// health snapshot updating every five seconds -- and coupling a 0.2 Hz
	// health feed to the login state machine would invite exactly the
	// re-render and re-hydration the phase gating exists to insulate from.
	//
	// It is NOT suppressed when unchanged the way EventAudioVU is: RTT
	// differs on nearly every tick, so suppression would save almost
	// nothing, and at 0.2 Hz across a handful of windows the traffic is
	// negligible.
	EventConnectionState = "connection:state"
```

And the emitter method, next to `ConnectionState`:

```go
// ConnectionHealth emits EventConnectionState with the full dual-plane
// snapshot. The payload is typed as any for the same reason
// KeybindsChanged's is: the concrete shape is app.ConnectionStateDTO, and
// internal/app already imports this package, so naming it here would be an
// import cycle.
func (t *Tagged) ConnectionHealth(payload any) {
	t.em.Emit(EventConnectionState, payload)
}
```

- [ ] **Step 4: Add the DTOs and the converter**

In `internal/app/dto.go`:

```go
// ConnLinkDTO is one transport plane's health, mirroring connhealth.Link.
//
// RTTMs is -1 when nothing has been measured, never 0 -- see
// connhealth.RTTUnknown for why the distinction is load-bearing. The
// frontend renders -1 as an em dash.
type ConnLinkDTO struct {
	State     string `json:"state"`
	RTTMs     int64  `json:"rtt_ms"`
	Healthy   bool   `json:"healthy"`
	Available bool   `json:"available"`
	Error     string `json:"error"`
}

// ConnectionStateDTO is the connection:state event payload and
// GetConnectionState's return, mirroring connhealth.Snapshot.
type ConnectionStateDTO struct {
	Server  string      `json:"server"`
	Control ConnLinkDTO `json:"control"`
	Voice   ConnLinkDTO `json:"voice"`
}

// ConnectionStateDTOFrom converts a connhealth.Snapshot to its wire shape.
// One shared converter for the event path and the getter, for exactly the
// reason AudioStateDTOFrom exists: a hand-written mapping on one of the two
// paths is how fields silently go missing from the other.
func ConnectionStateDTOFrom(s connhealth.Snapshot) ConnectionStateDTO {
	return ConnectionStateDTO{
		Server:  s.Server,
		Control: connLinkDTOFrom(s.Control),
		Voice:   connLinkDTOFrom(s.Voice),
	}
}

func connLinkDTOFrom(l connhealth.Link) ConnLinkDTO {
	return ConnLinkDTO{
		State:     l.State,
		RTTMs:     l.RTTMs,
		Healthy:   l.Healthy,
		Available: l.Available,
		Error:     l.Error,
	}
}
```

Add the `connhealth` import to `internal/app/dto.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestConnectionStateDTOFrom -v`
Expected: PASS

- [ ] **Step 6: Write the failing test for the bindings**

Append to `internal/app/connhealth_test.go`:

```go
func TestGetConnectionState_ReturnsTheMonitorsSnapshot(t *testing.T) {
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	m.SetServer("127.0.0.1:5002")
	m.SetControlState(connhealth.StateConnected)
	a.SetConnHealth(m)

	got := a.GetConnectionState()

	if got.Server != "127.0.0.1:5002" {
		t.Errorf("Server = %q, want %q", got.Server, "127.0.0.1:5002")
	}
	if got.Control.State != "connected" {
		t.Errorf("Control.State = %q, want connected", got.Control.State)
	}
}

func TestGetConnectionState_HonestWhenNoMonitorIsWired(t *testing.T) {
	// Tests and any build where wiring failed leave the monitor nil. The
	// binding must answer honestly rather than panic -- the same
	// optional-dependency discipline GetAudioState follows for a machine
	// with no sound card.
	a := NewForTest(state.New(), nil, nil)

	got := a.GetConnectionState()

	if got.Control.State != connhealth.StateDisconnected {
		t.Errorf("Control.State = %q with no monitor, want %q",
			got.Control.State, connhealth.StateDisconnected)
	}
	if got.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q with no monitor, want %q",
			got.Voice.State, connhealth.StateUnavailable)
	}
	if got.Control.RTTMs != connhealth.RTTUnknown {
		t.Errorf("Control.RTTMs = %d with no monitor, want %d",
			got.Control.RTTMs, connhealth.RTTUnknown)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestGetConnectionState -v`
Expected: FAIL — `a.SetConnHealth undefined`, `a.GetConnectionState undefined`.

- [ ] **Step 8: Implement the App wiring and bindings**

In `internal/app/app.go`, extend `sessionAPI`:

```go
// sessionAPI is the session surface the bindings depend on (fakeable in tests).
type sessionAPI interface {
	Connect(ctx context.Context, serverURL, name, password, unitID string) error
	Disconnect(ctx context.Context) error
	Reconnect(ctx context.Context) error
	UpdateRadioInfo(ctx context.Context, info *srspb.RadioInfo) error
	// PingOnce probes the control plane. Driven by the connhealth ticker;
	// returns an error when no control client is live.
	PingOnce(ctx context.Context, lastRTTMs int64) (int64, error)
	// MarkControlLost declares the control link dead after the probe
	// threshold trips. Idempotent against the stream-termination path.
	MarkControlLost()
}
```

Add the field to `App`:

```go
	// health is the connection-health model. Optional, the same discipline
	// as settings.audio and settings.joy: nil in tests and in any build
	// where wiring failed, so every use site must check.
	health *connhealth.Monitor
```

And the wiring method:

```go
// SetConnHealth injects the connection-health monitor (called from main.go
// after the session exists). Written once, before anything reads it.
func (a *App) SetConnHealth(m *connhealth.Monitor) { a.health = m }

// ConnHealth returns the wired monitor, or nil. Used by the voice layer to
// report lifecycle transitions into the model.
func (a *App) ConnHealth() *connhealth.Monitor { return a.health }
```

In `internal/app/bindings.go`:

```go
// GetConnectionState returns the current connection-health snapshot, for a
// window hydrating on mount. The push counterpart is the connection:state
// event; this exists because a window opened AFTER a transition has missed
// it -- exactly the precedent GetHotkeyState, GetJoystickState and
// GetAudioState set.
//
// With no monitor wired it answers honestly -- disconnected, voice
// unavailable, nothing measured -- rather than panicking or claiming health
// nothing has confirmed.
func (a *App) GetConnectionState() ConnectionStateDTO {
	if a.health == nil {
		return ConnectionStateDTOFrom(connhealth.Snapshot{
			Control: connhealth.Link{
				State: connhealth.StateDisconnected, RTTMs: connhealth.RTTUnknown, Available: true,
			},
			Voice: connhealth.Link{
				State: connhealth.StateUnavailable, RTTMs: connhealth.RTTUnknown,
			},
		})
	}
	return ConnectionStateDTOFrom(a.health.Snapshot())
}

// ReconnectVoice tears the live voice session down and dials a fresh one.
//
// The voice plane already recovers on its own -- voice.Session re-HELLOs on
// its ladder and, failing that, re-dials a fresh socket every 15s
// indefinitely -- so this is not the only route back. It exists so the
// banner's RECONNECT VOICE button can force the attempt NOW rather than
// leaving the user to wait out the current rung.
//
// It returns nil when no session could be started (no voice secret yet, an
// unparseable self GUID, a build whose codec is the stub): those are the
// documented voice-unavailable paths, not errors the user can act on, and
// startVoiceSession already logs each one.
func (a *App) ReconnectVoice() error {
	a.stopVoiceSession()
	a.startVoiceSession()
	return nil
}
```

Add the `connhealth` import to both files.

- [ ] **Step 9: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestGetConnectionState -v`
Expected: PASS

- [ ] **Step 10: Write the failing test for voice state reaching the monitor**

Append to `internal/app/connhealth_test.go`:

```go
func TestVoiceStateReachesTheMonitor(t *testing.T) {
	// Phase 5's I2 fix wired voice.Session's OnState to an event. Nothing in
	// the shipped binary ever subscribed, and the health model needs the
	// same transitions, so OnState feeds both.
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	a.SetConnHealth(m)

	a.reportVoiceState("connected", "")

	if got := m.Snapshot().Voice.State; got != "connected" {
		t.Errorf("Voice.State = %q, want connected", got)
	}
	if !m.Snapshot().Voice.Available {
		t.Error("Voice.Available = false after a connected transition, want true")
	}
}

func TestVoiceClosedReportsUnavailable(t *testing.T) {
	// A closed session is not a session in a failed state: it is no session
	// at all, which is exactly what Available is for. Without this, tearing
	// voice down on a clean logout would leave the pill showing a red VOICE
	// alert for a plane nobody asked to be running.
	a := NewForTest(state.New(), nil, nil)
	m := connhealth.New(connhealth.Options{})
	a.SetConnHealth(m)

	a.reportVoiceState("connected", "")
	a.reportVoiceUnavailable()

	snap := m.Snapshot()
	if snap.Voice.State != connhealth.StateUnavailable {
		t.Errorf("Voice.State = %q, want %q", snap.Voice.State, connhealth.StateUnavailable)
	}
	if snap.Voice.Available {
		t.Error("Voice.Available = true after teardown, want false")
	}
}
```

- [ ] **Step 11: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestVoice -v`
Expected: FAIL — `a.reportVoiceState undefined`, `a.reportVoiceUnavailable undefined`.

- [ ] **Step 12: Implement the voice reporting and extend `voiceSessionAPI`**

In `internal/app/voice.go`, extend the interface:

```go
type voiceSessionAPI interface {
	SetTXFrequencies(targets []voice.TXTarget)
	EndTransmission()
	SetRXContext(accept, global, testFreqs []voice.KHz)
	SetEffects(voiceEffect, clippingEffect string)
	State() voice.State
	// RTT reports the most recent keepalive round trip, or zero when no
	// keepalive has been answered on the current binding. Read by the
	// connhealth ticker for the status bar's VOICE segment. It is reset on
	// every rebind (see voice.Session.resetBindingLocked), which is what
	// keeps the pill from showing a healthy ping for a broken session.
	RTT() time.Duration
	Close() error
}
```

Add `"time"` to the imports if absent.

Add the reporting helpers:

```go
// reportVoiceState feeds one voice lifecycle transition into the
// connection-health model AND the voice:state event.
//
// Both, not either: the event is Phase 5's I2 contract and other consumers
// may subscribe to it, while the model is what the status surface reads.
func (a *App) reportVoiceState(state, errMsg string) {
	if a.health != nil {
		a.health.SetVoiceState(state, errMsg, true)
	}
}

// reportVoiceUnavailable records that no voice session is possible: torn
// down, never started, no voice secret, or a build whose codec is the stub.
//
// Distinct from a failed state on purpose. Voice that never started is not
// voice that broke, and rendering it as an alert would tell the user to
// reconnect something that was never going to connect -- which is the normal
// case on every CGO-less Windows release build (Phase 5 issue #1).
func (a *App) reportVoiceUnavailable() {
	if a.health != nil {
		a.health.SetVoiceState(connhealth.StateUnavailable, "", false)
	}
}

// voiceRTTMillis reads the live session's round trip for the connhealth
// ticker. Zero (no session, or nothing measured) is mapped to RTTUnknown by
// the Monitor.
func (a *App) voiceRTT() time.Duration {
	sess := a.voiceSession()
	if sess == nil {
		return 0
	}
	return sess.RTT()
}
```

Wire `OnState` in `voiceDialOptions` to call both:

```go
		OnState: func(st voice.State, err error) {
			msg := ""
			if err != nil {
				msg = err.Error()
			}
			// The health model first: it is what the status surface reads,
			// and em may be nil in a build with no settings backend while
			// the model is still wired.
			if st == voice.StateClosed {
				a.reportVoiceUnavailable()
			} else {
				a.reportVoiceState(st.String(), msg)
			}
			if em == nil {
				return
			}
			em.VoiceState(st.String(), msg)
		},
```

In `stopVoiceSession`, add `a.reportVoiceUnavailable()` after the session is cleared, and in `startVoiceSessionWithGen`'s early-return path (`if !ok { return }`) add `a.reportVoiceUnavailable()` before the return — a dial that cannot start is exactly the unavailable case.

- [ ] **Step 13: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/app/ -v`
Expected: PASS — including every pre-existing test. Fix the `fakeVoiceSession` in `voice_test.go` by adding:

```go
func (f *fakeVoiceSession) RTT() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rtt
}
```

and an `rtt time.Duration` field to the struct. Add `RTT` to `fakeSession`/`fakeControlSession` in `bindings_test.go` and `voice_test.go` as needed — `PingOnce` and `MarkControlLost` must also be added to whichever fakes implement `sessionAPI`:

```go
func (f *fakeSession) PingOnce(context.Context, int64) (int64, error) { return 1, nil }
func (f *fakeSession) MarkControlLost()                               {}
```

- [ ] **Step 14: Wire the monitor in `main.go`**

In `main.go`, after `gui.SetBackend(sess, registry)` and before the keybind store, insert:

```go
	// Connection health: one model, one ticker, one event, fed from the same
	// call sites that emit the control lifecycle event (see
	// session.Deps.OnControlState) so the two cannot disagree about the link.
	healthEvents := vcsevents.New(emitter)
	interval := time.Duration(cfg.PingIntervalSeconds) * time.Second
	monitor := connhealth.New(connhealth.Options{
		Interval: interval, // 0 falls back to connhealth.DefaultInterval
		Ping:     sess.PingOnce,
		VoiceRTT: gui.VoiceRTT,
		// The Monitor does not set disconnected itself: the session emits it
		// through its single normal path, which comes straight back in via
		// SetControlState. One emission path, two detectors.
		OnLoss: sess.MarkControlLost,
		OnChange: func(s connhealth.Snapshot) {
			healthEvents.ConnectionHealth(app.ConnectionStateDTOFrom(s))
		},
	})
	monitor.SetServer(cfg.ServerURL)
	gui.SetConnHealth(monitor)
	monitor.Start()
	defer monitor.Stop()
```

`session.New`'s existing construction is unchanged. The observer is installed
after the monitor exists, because the two halves need each other — the monitor
probes through `sess.PingOnce`, the session reports through the monitor's
`SetControlState` — so one of the two links must be late-bound, and the
session is the one with somewhere to put it. Immediately after `monitor` is
constructed:

```go
	sess.SetControlStateObserver(func(st vcsevents.ConnectionState) {
		monitor.SetControlState(string(st))
	})
```

Add the setter to `internal/session/session.go`:

```go
// SetControlStateObserver installs the control-transition observer after
// construction. It exists because the session and the health monitor each
// need the other -- the monitor probes through Session.PingOnce, the session
// reports through the monitor's SetControlState -- so one of the two links
// must be late-bound, and this is the one with somewhere to put it.
//
// Must be called before Connect. Setting it twice replaces the first.
func (s *Session) SetControlStateObserver(fn func(events.ConnectionState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dep.OnControlState = fn
}
```

Make `setConnState` read the hook under the lock:

```go
func (s *Session) setConnState(st events.ConnectionState) {
	events.New(s.em).ConnectionState(st)
	s.mu.Lock()
	fn := s.dep.OnControlState
	s.mu.Unlock()
	if fn != nil {
		fn(st)
	}
}
```

> Note: `markControlLost` calls `setConnState` **after** releasing `s.mu`, so this nested lock is safe. Verify that when editing.

Add `connhealth` and `time` to `main.go`'s imports.

Add `VoiceRTT` as the exported accessor in `internal/app/voice.go`:

```go
// VoiceRTT exposes the live voice session's round trip for main.go's
// connhealth wiring. Zero means no session or nothing measured.
func (a *App) VoiceRTT() time.Duration { return a.voiceRTT() }
```

- [ ] **Step 15: Build, vet, test, commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./...
git add internal/events/ internal/app/ main.go internal/session/
git commit -m "feat(app): surface connection health as connection:state

One event carrying both planes, one getter for a window that mounts after a
transition (the GetHotkeyState/GetJoystickState/GetAudioState precedent), one
shared DTO converter so the event path and the getter cannot drift -- the
exact failure AudioStateDTOFrom was introduced to stop.

voiceSessionAPI gains RTT(). Phase 5 exposed it on voice.Session and nothing
ever read it; the status bar's VOICE segment is its first consumer.

voice.Session's OnState now feeds the health model as well as the voice:state
event. A closed session reports UNAVAILABLE rather than a failed state: voice
that never started is not voice that broke, and an alert there would tell the
user to reconnect something that was never going to connect -- the normal
case on every CGO-less Windows release build.

main.go late-binds the session's control-state observer because the two halves
need each other: the monitor probes through Session.PingOnce, the session
reports through the monitor's SetControlState.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Connection SFX

**Files:**
- Modify: `internal/audio/assets/manifest.toml`
- Modify: `internal/app/voice.go` (or a new small `internal/app/connsfx.go`)
- Test: `internal/app/connhealth_test.go` (append)

**Interfaces:**
- Consumes: `a.audioManager()` and `Manager.PlayEffect(id string)` (existing); `connhealth.StateConnected` / `StateDisconnected` (Task 1).
- Produces: `func (a *App) playConnectionSFX(state string)` — called from the control-state observer.

- [ ] **Step 1: Write the failing test**

Append to `internal/app/connhealth_test.go`:

```go
func TestConnectionSFX_SlotsExistInTheManifest(t *testing.T) {
	// The engine is asset-agnostic (Phase 4 D11): a missing SAMPLE is
	// logged once and plays silence. A missing SLOT is different -- it is a
	// silent no-op with nothing to log, so the wiring would look correct
	// and do nothing forever.
	sfx := audio.NewSFX()
	ids := sfx.EffectIDs()

	for _, want := range []string{"connect", "disconnect"} {
		found := false
		for _, id := range ids {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("manifest has no %q slot; ids = %v", want, ids)
		}
	}
}
```

Add the `audio` import to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestConnectionSFX -v`
Expected: FAIL — `manifest has no "connect" slot`.

- [ ] **Step 3: Add the manifest slots**

Append to `internal/audio/assets/manifest.toml`:

```toml
[connect]
label = "Connect"
file = "connect.wav"
order = 8

[disconnect]
label = "Disconnect"
file = "disconnect.wav"
order = 9
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestConnectionSFX -v`
Expected: PASS

- [ ] **Step 5: Write the failing test for the gate**

Append to `internal/app/connhealth_test.go`:

```go
func TestPlayConnectionSFX_RespectsTheSetting(t *testing.T) {
	// general.play_connection_sounds has been persisted and exposed in
	// Settings since Phase 3 with NO consumer at all -- a toggle that did
	// nothing. This is its first one, so the gate is the point of the test.
	a := NewForTest(state.New(), nil, nil)
	a.settings = &settingsBackend{cfg: &config.Config{}}

	a.settings.cfg.General.PlayConnectionSounds = false
	if got := a.connectionSFXID(connhealth.StateConnected); got != "" {
		t.Errorf("connectionSFXID = %q with sounds off, want \"\"", got)
	}

	a.settings.cfg.General.PlayConnectionSounds = true
	if got := a.connectionSFXID(connhealth.StateConnected); got != "connect" {
		t.Errorf("connectionSFXID = %q for connected, want \"connect\"", got)
	}
	if got := a.connectionSFXID(connhealth.StateDisconnected); got != "disconnect" {
		t.Errorf("connectionSFXID = %q for disconnected, want \"disconnect\"", got)
	}
	// Reconnecting is a transient the user sees in the pill, not an event
	// worth a sound on every retry.
	if got := a.connectionSFXID(connhealth.StateReconnecting); got != "" {
		t.Errorf("connectionSFXID = %q for reconnecting, want \"\"", got)
	}
}
```

Add the `config` import to the test file.

- [ ] **Step 6: Run test to verify it fails**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestPlayConnectionSFX -v`
Expected: FAIL — `a.connectionSFXID undefined`.

- [ ] **Step 7: Implement**

Create `internal/app/connsfx.go`:

```go
package app

import (
	"github.com/FPGSchiba/vcs-srs-client/internal/connhealth"
)

// connectionSFXID maps a control-link transition to the SFX slot that marks
// it, or "" for a transition that gets no sound.
//
// Gated on general.play_connection_sounds, which has been persisted and
// exposed in Settings since Phase 3 with no consumer at all. This is its
// first one.
//
// Reconnecting deliberately gets nothing: it is a transient the user already
// sees in the status bar's pill, and a sound on every retry rung would be
// noise during exactly the period the user is already annoyed.
func (a *App) connectionSFXID(state string) string {
	sb := a.settings
	if sb == nil || sb.cfg == nil {
		return ""
	}
	sb.mu.Lock()
	enabled := sb.cfg.General.PlayConnectionSounds
	sb.mu.Unlock()
	if !enabled {
		return ""
	}
	switch state {
	case connhealth.StateConnected:
		return "connect"
	case connhealth.StateDisconnected:
		return "disconnect"
	default:
		return ""
	}
}

// playConnectionSFX plays the sound marking a control-link transition.
//
// It ships SILENT: connect.wav and disconnect.wav do not exist, adding to
// the seven sample files already outstanding from Phase 4. The engine is
// asset-agnostic by design (Phase 4 D11) -- a missing sample is logged once
// and plays silence, never a crash -- so this wiring is correct and testable
// today and goes audible the moment the samples land.
func (a *App) playConnectionSFX(state string) {
	id := a.connectionSFXID(state)
	if id == "" {
		return
	}
	m := a.audioManager()
	if m == nil {
		return
	}
	m.PlayEffect(id)
}
```

> `a.audioManager()` dereferences `a.settings` without a nil check today. `connectionSFXID` returns `""` for a nil `settings`, so `playConnectionSFX` returns before reaching it — keep that ordering.

- [ ] **Step 8: Run test to verify it passes**

Run: `GOCACHE=$TMPDIR/vcs-gocache go test -tags purego ./internal/app/ -run TestPlayConnectionSFX -v`
Expected: PASS

- [ ] **Step 9: Wire it into the control-state observer**

In `main.go`, extend the observer installed in Task 3 Step 14:

```go
	sess.SetControlStateObserver(func(st vcsevents.ConnectionState) {
		monitor.SetControlState(string(st))
		gui.PlayConnectionSFX(string(st))
	})
```

Add the exported wrapper in `internal/app/connsfx.go`:

```go
// PlayConnectionSFX is playConnectionSFX's exported form, for main.go's
// control-state observer wiring.
func (a *App) PlayConnectionSFX(state string) { a.playConnectionSFX(state) }
```

- [ ] **Step 10: Build, test, commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./internal/app/ ./internal/audio/
git add internal/audio/assets/manifest.toml internal/app/ main.go
git commit -m "feat(audio): connection sounds, wired asset-agnostically

general.play_connection_sounds has been persisted and exposed in Settings
since Phase 3 with no consumer at all -- a toggle that did nothing. This is
its first one.

It ships SILENT. connect.wav and disconnect.wav do not exist, adding two to
the seven sample files already outstanding from Phase 4. The engine is
asset-agnostic by design (D11): a missing sample is logged once and plays
silence, never a crash. The wiring is correct and testable now and goes
audible the moment the samples land.

A missing SLOT would not have been: that is a silent no-op with nothing to
log, so the wiring would look correct and do nothing forever. Hence the test
that pins both slot ids against the manifest.

Reconnecting gets no sound. It is a transient the user already sees in the
status bar, and a rung-by-rung chirp during an outage is noise at exactly
the wrong moment.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Frontend — the connection store

**Files:**
- Create: `frontend/src/shared/store/connection.ts`
- Create: `frontend/src/shared/store/useConnectionSync.ts`
- Create: `frontend/src/shared/store/useConnectionSync.test.tsx`
- Modify: `frontend/src/shared/api/events.ts`
- Modify: `frontend/src/shared/api/client.ts`
- Modify: `frontend/src/windows/main/MainApp.tsx`

**Interfaces:**
- Consumes: `connection:state` event and `App.GetConnectionState()` / `App.ReconnectVoice()` (Task 3).
- Produces:
  - `export type Dot = "ok" | "warn" | "alert" | "off"`
  - `export type BannerVariant = "none" | "control-only" | "voice-only" | "disconnected"`
  - `export interface ConnLink { state: string; rtt_ms: number; healthy: boolean; available: boolean; error: string }`
  - `export interface ConnectionState { server: string; control: ConnLink; voice: ConnLink }`
  - `export const useConnection` — Zustand store with `{ conn: ConnectionState; setConn(c): void }`
  - `export function controlDot(l: ConnLink): Dot`
  - `export function voiceDot(l: ConnLink): Dot`
  - `export function bannerVariant(c: ConnectionState): BannerVariant`
  - `export function formatRTT(ms: number): string`
  - `export function useConnectionSync(): void`

- [ ] **Step 1: Write the failing test for the derivations**

Create `frontend/src/shared/store/connection.test.ts`:

```ts
import { describe, it, expect } from "vitest";
import {
  controlDot,
  voiceDot,
  bannerVariant,
  formatRTT,
  emptyConnection,
  type ConnLink,
} from "./connection";

const link = (over: Partial<ConnLink> = {}): ConnLink => ({
  state: "connected",
  rtt_ms: 8,
  healthy: true,
  available: true,
  error: "",
  ...over,
});

describe("controlDot", () => {
  it("is ok when connected and the probe answers", () => {
    expect(controlDot(link())).toBe("ok");
  });

  it("is warn when connected but the probe has begun failing", () => {
    // One or two failed pings clear `healthy` without changing state. The
    // dot has to move before the banner does, or the only warning the user
    // gets is the banner arriving 15s later with no build-up.
    expect(controlDot(link({ healthy: false }))).toBe("warn");
  });

  it("is warn while reconnecting", () => {
    expect(controlDot(link({ state: "reconnecting", healthy: false }))).toBe("warn");
  });

  it("is alert when disconnected", () => {
    expect(controlDot(link({ state: "disconnected", healthy: false }))).toBe("alert");
  });
});

describe("voiceDot", () => {
  it("is off when unavailable", () => {
    // Not alert. Voice that never started is not voice that broke -- and on
    // every CGO-less Windows release build this is the NORMAL state.
    expect(voiceDot(link({ state: "unavailable", available: false }))).toBe("off");
  });

  it("is ok when connected", () => {
    expect(voiceDot(link())).toBe("ok");
  });

  it.each(["resolving", "handshaking", "rebinding", "retrying"])(
    "is warn while %s",
    (state) => {
      expect(voiceDot(link({ state, healthy: false }))).toBe("warn");
    },
  );

  it.each(["idle", "closed"])("is alert when %s", (state) => {
    expect(voiceDot(link({ state, healthy: false }))).toBe("alert");
  });
});

describe("bannerVariant", () => {
  it("shows nothing when both planes are healthy", () => {
    expect(bannerVariant({ server: "s", control: link(), voice: link() })).toBe("none");
  });

  it("shows control-only (VOICE DEGRADED) when voice alone is down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link(),
        voice: link({ state: "closed", healthy: false }),
      }),
    ).toBe("control-only");
  });

  it("shows voice-only (CONTROL DEGRADED) when control alone is down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: link(),
      }),
    ).toBe("voice-only");
  });

  it("shows disconnected when both are down", () => {
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: link({ state: "closed", healthy: false }),
      }),
    ).toBe("disconnected");
  });

  it("ignores voice entirely when it is unavailable", () => {
    // An unavailable voice plane is not a factor in the banner: a healthy
    // control link with a stub codec must show NO banner, not a permanent
    // VOICE DEGRADED on every Windows release build.
    const unavailable = link({ state: "unavailable", available: false, healthy: false });
    expect(bannerVariant({ server: "s", control: link(), voice: unavailable })).toBe("none");
    expect(
      bannerVariant({
        server: "s",
        control: link({ state: "disconnected", healthy: false }),
        voice: unavailable,
      }),
    ).toBe("disconnected");
  });
});

describe("formatRTT", () => {
  it("renders an em dash for an unmeasured link", () => {
    // -1, not 0: voice.Session.RTT() genuinely returns 0 before the first
    // answered keepalive and after every rebind.
    expect(formatRTT(-1)).toBe("—");
  });

  it("renders milliseconds otherwise", () => {
    expect(formatRTT(0)).toBe("0ms");
    expect(formatRTT(142)).toBe("142ms");
  });
});

describe("emptyConnection", () => {
  it("is honest before anything has been measured", () => {
    const c = emptyConnection();
    expect(c.control.state).toBe("disconnected");
    expect(c.control.healthy).toBe(false);
    expect(c.voice.available).toBe(false);
    expect(c.control.rtt_ms).toBe(-1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/shared/store/connection.test.ts`
Expected: FAIL — cannot resolve `./connection`.

- [ ] **Step 3: Implement the store and derivations**

Create `frontend/src/shared/store/connection.ts`:

```ts
import { create } from "zustand";

/** Status-dot colour classes, matching `.dual-pill .d.*` in the design CSS.
 *  `off` is the dimmed, non-alarming rendering for an unavailable plane. */
export type Dot = "ok" | "warn" | "alert" | "off";

/** Which ConnBanner variant to render, matching the design prototype's
 *  `shell.jsx` ConnBanner states. */
export type BannerVariant = "none" | "control-only" | "voice-only" | "disconnected";

/** One transport plane's health. Mirrors Go's `app.ConnLinkDTO`. */
export interface ConnLink {
  state: string;
  /** -1 means nothing has been measured. NOT 0 — see formatRTT. */
  rtt_ms: number;
  healthy: boolean;
  available: boolean;
  error: string;
}

/** The full dual-plane snapshot. Mirrors Go's `app.ConnectionStateDTO`. */
export interface ConnectionState {
  server: string;
  control: ConnLink;
  voice: ConnLink;
}

/** The honest pre-hydration state: disconnected, nothing measured, voice
 *  unavailable. Deliberately not optimistic — claiming health nothing has
 *  confirmed is the failure mode `useSettingsSync`'s getHotkeyState comment
 *  already calls out. */
export function emptyConnection(): ConnectionState {
  return {
    server: "",
    control: { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" },
    voice: { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" },
  };
}

/** Voice lifecycle states that mean "working on it", from `voice.State`. */
const VOICE_TRANSIENT = ["resolving", "handshaking", "rebinding", "retrying"];

export function controlDot(l: ConnLink): Dot {
  if (l.state === "disconnected") return "alert";
  if (l.state === "reconnecting") return "warn";
  // Connected but the probe has begun failing. The dot moves before the
  // banner does, so the user gets build-up rather than a banner appearing
  // out of nowhere 15s into an outage.
  if (!l.healthy) return "warn";
  return "ok";
}

export function voiceDot(l: ConnLink): Dot {
  // Checked first and unconditionally: an unavailable plane has no
  // lifecycle worth colouring, and rendering it as an alert would put a red
  // dot on every CGO-less Windows release build as its normal state.
  if (!l.available) return "off";
  if (l.state === "connected") return "ok";
  if (VOICE_TRANSIENT.includes(l.state)) return "warn";
  return "alert";
}

export function bannerVariant(c: ConnectionState): BannerVariant {
  const controlDown = controlDot(c.control) === "alert";
  // An unavailable voice plane drops out of the banner's input entirely.
  const voiceDown = c.voice.available && voiceDot(c.voice) === "alert";

  if (controlDown && voiceDown) return "disconnected";
  if (controlDown) return "voice-only";
  if (voiceDown) return "control-only";
  return "none";
}

/** Renders a latency figure, or an em dash when nothing has been measured.
 *  The -1 sentinel exists because 0 is a value the voice plane genuinely
 *  reports — before its first answered keepalive, and after every rebind. */
export function formatRTT(ms: number): string {
  if (ms < 0) return "—";
  return `${ms}ms`;
}

interface ConnectionStore {
  conn: ConnectionState;
  setConn: (c: ConnectionState) => void;
}

export const useConnection = create<ConnectionStore>((set) => ({
  conn: emptyConnection(),
  setConn: (conn) => set({ conn }),
}));
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/shared/store/connection.test.ts`
Expected: PASS

- [ ] **Step 5: Add the event names and API methods**

In `frontend/src/shared/api/events.ts`, add to `EV`:

```ts
  connectionState: "connection:state",
  // Emitted by the backend since Phase 5's I2 fix, and subscribed by
  // nothing until now — the whole voice.Session state machine had no
  // frontend consumer at all.
  voiceState: "voice:state",
  voiceAddressUpdate: "voice:address_update",
```

In `frontend/src/shared/api/client.ts`, add the type re-export and the two methods:

```ts
import type { ConnectionState } from "../store/connection";

// ... inside `api`:
  getConnectionState: (): Promise<ConnectionState> =>
    App.GetConnectionState() as Promise<ConnectionState>,
  reconnectVoice: (): Promise<void> => App.ReconnectVoice() as Promise<void>,
```

- [ ] **Step 6: Write the failing test for the sync hook**

Create `frontend/src/shared/store/useConnectionSync.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, waitFor } from "@testing-library/react";

const getConnectionState = vi.fn();

vi.mock("../api/client", () => ({
  api: { getConnectionState: () => getConnectionState() },
}));

const handlers = new Map<string, Set<(d: unknown) => void>>();
const emit = (name: string, data: unknown) => handlers.get(name)?.forEach((h) => h(data));

vi.mock("../api/events", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../api/events")>();
  return {
    EV: actual.EV,
    on: (name: string, cb: (d: unknown) => void) => {
      const set = handlers.get(name) ?? new Set();
      set.add(cb);
      handlers.set(name, set);
      return () => set.delete(cb);
    },
  };
});

import { EV } from "../api/events";
import { useConnection, emptyConnection, type ConnectionState } from "./connection";
import { useConnectionSync } from "./useConnectionSync";

const snapshot = (over: Partial<ConnectionState> = {}): ConnectionState => ({
  server: "127.0.0.1:5002",
  control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
  voice: { state: "connected", rtt_ms: 12, healthy: true, available: true, error: "" },
  ...over,
});

function Probe() {
  useConnectionSync();
  return null;
}

describe("useConnectionSync", () => {
  beforeEach(() => {
    handlers.clear();
    getConnectionState.mockReset().mockResolvedValue(snapshot());
    useConnection.setState({ conn: emptyConnection() });
  });
  afterEach(() => vi.restoreAllMocks());

  it("hydrates the store on mount", async () => {
    render(<Probe />);
    await waitFor(() => expect(useConnection.getState().conn.server).toBe("127.0.0.1:5002"));
  });

  it("keeps the store live via connection:state", async () => {
    render(<Probe />);
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    emit(EV.connectionState, snapshot({
      control: { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" },
    }));
    expect(useConnection.getState().conn.control.state).toBe("disconnected");
  });

  it("unsubscribes on unmount so a remount cannot double-handle", () => {
    const { unmount } = render(<Probe />);
    unmount();
    emit(EV.connectionState, snapshot({ server: "elsewhere:1" }));
    expect(useConnection.getState().conn.server).not.toBe("elsewhere:1");
  });

  it("logs rather than swallows a getConnectionState rejection", async () => {
    // Swallowed, this leaves the surface showing the honest-but-frozen
    // empty state with nothing to explain why it never updates.
    const err = vi.spyOn(console, "error").mockImplementation(() => {});
    getConnectionState.mockRejectedValue(new Error("backend unreachable"));

    render(<Probe />);
    await waitFor(() => expect(err).toHaveBeenCalled());
  });
});
```

- [ ] **Step 7: Run test to verify it fails**

Run: `cd frontend && npx vitest run src/shared/store/useConnectionSync.test.tsx`
Expected: FAIL — cannot resolve `./useConnectionSync`.

- [ ] **Step 8: Implement the hook**

Create `frontend/src/shared/store/useConnectionSync.ts`:

```ts
import { useEffect } from "react";
import { api } from "../api/client";
import { on, EV } from "../api/events";
import { useConnection, type ConnectionState } from "./connection";

/**
 * useConnectionSync hydrates the shared connection-health store and keeps it
 * live for as long as the calling window is mounted.
 *
 * It belongs to a WINDOW, not a screen — the same discipline as
 * `useSettingsSync`, and for the same reason: each window is a separate Wails
 * webview with its own JS runtime, so a store mounted inside one screen is
 * torn down the moment the user navigates away.
 *
 * The mount-time fetch exists because events are push-only: a window opened
 * AFTER a transition has missed it, and would otherwise sit on the honest
 * but frozen empty state until the next tick.
 */
export function useConnectionSync(): void {
  useEffect(() => {
    // `stale` guards the hydrate against its own lateness. The backend can
    // push a newer snapshot while this promise is still in flight, and
    // letting the resolution write unconditionally would roll the store back
    // to a state that is already wrong — with no further event guaranteed
    // to correct it until the next 5s tick.
    let stale = false;

    api
      .getConnectionState()
      .then((c) => {
        if (stale) return;
        useConnection.getState().setConn(c);
      })
      .catch((err) => {
        // Logged rather than swallowed: the store's honest empty default
        // means a rejection here leaves the surface reporting disconnected
        // with nothing measured, and no trace of why it never updates.
        console.error("getConnectionState failed; connection health is unknown", err);
      });

    const offs = [
      on<ConnectionState>(EV.connectionState, (c) => {
        // Any pushed snapshot supersedes the in-flight hydrate.
        stale = true;
        useConnection.getState().setConn(c);
      }),
    ];
    return () => {
      stale = true;
      offs.forEach((off) => off());
    };
  }, []);
}
```

- [ ] **Step 9: Run test to verify it passes**

Run: `cd frontend && npx vitest run src/shared/store/useConnectionSync.test.tsx`
Expected: PASS

- [ ] **Step 10: Write the StrictMode tests and the real-unmount control**

Append to `frontend/src/shared/store/useConnectionSync.test.tsx`:

```tsx
/**
 * Both window roots render inside `React.StrictMode` (frontend/src/main.tsx
 * and comms.tsx), which in development runs every effect as
 * setup -> cleanup -> setup on the same element. The suite above renders
 * bare, so it does not exercise the mode the app actually runs in — the gap
 * that hid a total keybind-capture break through two phases (#30).
 *
 * This hook's effect cleanup calls the Wails unsubscribe, which is a real
 * side effect, so it is exactly the shape that broke before.
 */
describe("useConnectionSync under StrictMode", () => {
  beforeEach(() => {
    handlers.clear();
    getConnectionState.mockReset().mockResolvedValue(snapshot());
    useConnection.setState({ conn: emptyConnection() });
  });

  it("still receives events after the simulated unmount and re-mount", async () => {
    render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    emit(EV.connectionState, snapshot({ server: "after-strict:1" }));
    await waitFor(() =>
      expect(useConnection.getState().conn.server).toBe("after-strict:1"),
    );
  });

  it("leaves exactly one live subscription, not two", async () => {
    // StrictMode's setup -> cleanup -> setup must net out at one handler. Two
    // would double-apply every snapshot — harmless for an idempotent set, but
    // it means the cleanup is not actually removing what the setup added, and
    // the next hook with non-idempotent handling inherits a live bug.
    render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    expect(handlers.get(EV.connectionState)?.size).toBe(1);
  });

  // The control: a genuine unmount must still unsubscribe. Without this, a
  // fix that simply stopped cleaning up would pass the two tests above.
  it("still unsubscribes on a real unmount", async () => {
    const { unmount } = render(
      <StrictMode>
        <Probe />
      </StrictMode>,
    );
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    unmount();

    expect(handlers.get(EV.connectionState)?.size ?? 0).toBe(0);
  });
});
```

- [ ] **Step 11: Write the failing test for the hydrate race (Review Focus #3)**

Append to `frontend/src/shared/store/useConnectionSync.test.tsx`:

```tsx
describe("useConnectionSync hydrate race", () => {
  beforeEach(() => {
    handlers.clear();
    useConnection.setState({ conn: emptyConnection() });
  });

  // Review Focus #3. The mount-time fetch is a promise; the backend can push
  // a newer snapshot while it is still in flight. Letting the late resolution
  // write unconditionally rolls the store back to a state that is already
  // wrong, and nothing is guaranteed to correct it until the next 5s tick.
  it("does not let a late hydrate clobber a newer pushed snapshot", async () => {
    let resolveHydrate!: (c: ConnectionState) => void;
    getConnectionState.mockReset().mockReturnValue(
      new Promise<ConnectionState>((res) => {
        resolveHydrate = res;
      }),
    );

    render(<Probe />);
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    // The push wins the race.
    emit(EV.connectionState, snapshot({ server: "newer:2" }));
    expect(useConnection.getState().conn.server).toBe("newer:2");

    // The stale hydrate resolves afterwards and must be ignored.
    resolveHydrate(snapshot({ server: "stale:1" }));
    await waitFor(() => expect(getConnectionState).toHaveBeenCalled());

    expect(useConnection.getState().conn.server).toBe("newer:2");
  });
});
```

- [ ] **Step 12: Run the whole file**

Run: `cd frontend && npx vitest run src/shared/store/useConnectionSync.test.tsx src/shared/store/connection.test.ts`
Expected: PASS — all tests.

- [ ] **Step 13: Mount the hook in MainApp**

In `frontend/src/windows/main/MainApp.tsx`, add the import and the call next to `useSettingsSync()`:

```tsx
import { useConnectionSync } from "../../shared/store/useConnectionSync";
```

```tsx
  // Keeps the shared settings/keybinds store live for this window's whole
  // lifetime, not just while the Settings screen happens to be open.
  useSettingsSync();
  // Same discipline for connection health: the status bar and the banner
  // both read it, and both are part of the window shell.
  useConnectionSync();
```

- [ ] **Step 14: Typecheck and commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend && npx tsc --noEmit
cd /Users/schiba/Projects/vanguard/vcs-srs-client
git add frontend/src/shared/store/ frontend/src/shared/api/ frontend/src/windows/main/MainApp.tsx
git commit -m "feat(frontend): connection-health store and its window sync

One store, hydrated on mount and kept live by connection:state. voice:state
also joins the EV map: the backend has emitted it since Phase 5's I2 fix and
nothing has ever subscribed, in any window.

The derivations live here rather than in the components because both the
banner and the pill need them and they are the part worth testing: an
unavailable voice plane drops out of the banner's input entirely, so a
healthy control link with a stub codec shows NO banner rather than a
permanent VOICE DEGRADED on every Windows release build.

formatRTT renders -1 as an em dash. The sentinel is -1 and not 0 because 0
is a value voice.Session.RTT() genuinely returns -- before its first answered
keepalive, and after every rebind.

The hydrate guards against its own lateness: the backend can push a newer
snapshot while the mount-time fetch is still in flight, and an unconditional
write on resolution rolls the store back to a state that is already wrong,
with nothing guaranteed to correct it until the next tick.

StrictMode tests plus a real-unmount control, per the #30 lesson: this hook's
effect cleanup calls the Wails unsubscribe, which is exactly the
side-effecting-cleanup shape that broke keybind capture through two phases.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Frontend — the status bar dual-pill

**Files:**
- Modify: `frontend/src/shared/components/StatusBar.tsx`
- Modify: `frontend/src/windows/main/MainApp.tsx`
- Test: `frontend/src/shared/components/StatusBar.test.tsx` (create)

**Interfaces:**
- Consumes: `useConnection`, `controlDot`, `voiceDot`, `formatRTT` (Task 5).
- Produces: `StatusBar` accepts `{ onNavigate?: (key: string) => void }`.

- [ ] **Step 1: Write the failing test**

Create `frontend/src/shared/components/StatusBar.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, fireEvent } from "@testing-library/react";

vi.mock("../hooks/useBuildInfo", () => ({
  useBuildInfo: () => ({ client_version: "0.1.0", protocol_version: "1", build: "dev" }),
}));

import { StatusBar } from "./StatusBar";
import { useConnection, emptyConnection, type ConnectionState } from "../store/connection";

const snapshot = (over: Partial<ConnectionState> = {}): ConnectionState => ({
  server: "127.0.0.1:5002",
  control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
  voice: { state: "connected", rtt_ms: 12, healthy: true, available: true, error: "" },
  ...over,
});

describe("StatusBar", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

  it("renders both segments of the dual-pill", () => {
    useConnection.setState({ conn: snapshot() });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".dual-pill")).not.toBeNull();
    expect(container.querySelector(".seg.ctrl")).not.toBeNull();
    expect(container.querySelector(".seg.voice")).not.toBeNull();
    expect(screen.getByText("CTRL")).toBeInTheDocument();
    expect(screen.getByText("VOICE")).toBeInTheDocument();
  });

  it("renders each plane's own latency", () => {
    useConnection.setState({ conn: snapshot() });
    render(<StatusBar />);

    expect(screen.getByText("8ms")).toBeInTheDocument();
    expect(screen.getByText("12ms")).toBeInTheDocument();
  });

  it("colours the dots per plane", () => {
    useConnection.setState({
      conn: snapshot({
        control: { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" },
        voice: { state: "closed", rtt_ms: -1, healthy: false, available: true, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.ctrl .d.ok")).not.toBeNull();
    expect(container.querySelector(".seg.voice .d.alert")).not.toBeNull();
  });

  it("dims the voice dot when voice is unavailable", () => {
    // The whole point of the fourth state: on a CGO-less Windows release
    // build this is the NORMAL rendering, and a red dot there would be
    // telling the user something is broken that was never going to run.
    useConnection.setState({
      conn: snapshot({
        voice: { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.voice .d.off")).not.toBeNull();
    expect(container.querySelector(".seg.voice .d.alert")).toBeNull();
  });

  it("navigates to Server Details when the pill is clicked", () => {
    useConnection.setState({ conn: snapshot() });
    const onNavigate = vi.fn();
    const { container } = render(<StatusBar onNavigate={onNavigate} />);

    fireEvent.click(container.querySelector(".dual-pill")!);

    expect(onNavigate).toHaveBeenCalledWith("server");
  });
});
```

- [ ] **Step 2: Write the failing test for the zero-RTT render (Review Focus #4)**

Append to `frontend/src/shared/components/StatusBar.test.tsx`:

```tsx
describe("StatusBar latency rendering", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

  // Review Focus #4. -1 means nothing has been measured; 0 means a genuine
  // sub-millisecond measurement. Collapsing them would show a confident
  // "0ms" on a plane that has never answered a probe.
  it("renders an em dash for an unmeasured plane but 0ms for a measured zero", () => {
    useConnection.setState({
      conn: snapshot({
        control: { state: "connected", rtt_ms: 0, healthy: true, available: true, error: "" },
        voice: { state: "connected", rtt_ms: -1, healthy: true, available: true, error: "" },
      }),
    });
    const { container } = render(<StatusBar />);

    expect(container.querySelector(".seg.ctrl")!.textContent).toContain("0ms");
    expect(container.querySelector(".seg.voice")!.textContent).toContain("—");
  });
});

/**
 * StatusBar subscribes to nothing itself — `useConnection` is a plain
 * Zustand selector — but it renders inside StrictMode in both window roots,
 * so the suite pins that it survives the double-render rather than assuming
 * it. See the #30 lesson.
 */
describe("StatusBar under StrictMode", () => {
  beforeEach(() => useConnection.setState({ conn: emptyConnection() }));

  it("renders the same dual-pill under the simulated double-render", () => {
    useConnection.setState({ conn: snapshot() });
    const { container } = render(
      <StrictMode>
        <StatusBar />
      </StrictMode>,
    );

    expect(container.querySelectorAll(".dual-pill")).toHaveLength(1);
    expect(container.querySelectorAll(".seg.ctrl")).toHaveLength(1);
    expect(screen.getByText("8ms")).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/shared/components/StatusBar.test.tsx`
Expected: FAIL — `.dual-pill` is null (StatusBar still renders the single `sb-item`).

- [ ] **Step 4: Implement the dual-pill**

Replace `frontend/src/shared/components/StatusBar.tsx` entirely:

```tsx
import { Icon } from "./Icon";
import { useBuildInfo } from "../hooks/useBuildInfo";
import {
  useConnection,
  controlDot,
  voiceDot,
  formatRTT,
  type ConnLink,
  type Dot,
} from "../store/connection";

interface StatusBarProps {
  /** Navigates the main window to a nav key. Optional so the Comms popout
   *  can render the bar without a router. */
  onNavigate?: (key: string) => void;
}

/** One half of the dual-pill. `kind` selects the design's `.seg.ctrl` /
 *  `.seg.voice` tinting; `dot` selects `.d.ok` / `.d.warn` / `.d.alert`, plus
 *  `.d.off` for a plane that is not running at all. */
function Segment({
  kind,
  label,
  link,
  dot,
  server,
}: {
  kind: "ctrl" | "voice";
  label: string;
  link: ConnLink;
  dot: Dot;
  server: string;
}) {
  return (
    <span className={`seg ${kind}`}>
      <span className={`d ${dot}`} />
      <span className="lbl">{label}</span>
      <span className="val">{server || "standalone"}</span>
      <span className="ping">{formatRTT(link.rtt_ms)}</span>
    </span>
  );
}

/**
 * StatusBar is the bottom application status bar, ported from the design
 * prototype's `shell.jsx` StatusBar.
 *
 * It renders the prototype's `.dual-pill` UNCONDITIONALLY, not only in
 * distributed mode. Standalone genuinely has two independently-failing
 * transports — a gRPC control plane and a UDP voice plane — and the
 * ConnBanner's `control-only` / `voice-only` variants are only legible if the
 * pill can show which half is down. The prototype's single-dot standalone
 * branch cannot express that. Phase 9 fills in real per-host names and adds
 * hosts; it does not restructure this widget.
 *
 * Both segments show the same server address today: `srs.proto` carries no
 * server display name (PROTO_GAPS #10), so `.val` is the host from
 * `server_url`. The prototype's region item is omitted rather than rendered
 * as a permanent em dash — there is no data source for it.
 *
 * classNames are byte-identical to the design so the ported CSS applies.
 */
export function StatusBar({ onNavigate }: StatusBarProps) {
  const conn = useConnection((s) => s.conn);
  const build = useBuildInfo();
  const goServer = () => onNavigate?.("server");

  return (
    <div className="statusbar">
      <span className="sb-item">v{build?.client_version ?? "—"}</span>
      <span className="sb-divider"></span>

      <div className="dual-pill" onClick={goServer} title="Open Server Details">
        <Segment
          kind="ctrl"
          label="CTRL"
          link={conn.control}
          dot={controlDot(conn.control)}
          server={conn.server}
        />
        <Segment
          kind="voice"
          label="VOICE"
          link={conn.voice}
          dot={voiceDot(conn.voice)}
          server={conn.server}
        />
      </div>

      <span className="sb-spacer"></span>

      <span className="sb-btn" onClick={goServer}>
        <Icon name="server" size={11} /> NETWORK
      </span>
      <span className="sb-btn">
        <Icon name="help" size={11} /> HELP
      </span>
      <span className="sb-btn">
        <Icon name="bell" size={11} />
        ALERTS
      </span>
    </div>
  );
}
```

- [ ] **Step 5: Add the `.d.off` rule to the design CSS**

`.d.off` is the one class the prototype does not define, because the prototype has no unavailable state. Add it immediately after the existing `.d.alert` rule in `frontend/src/shared/styles/` (locate the ported `.dual-pill` block first with `grep -rn "dual-pill" frontend/src/shared/styles/`):

```css
/* Not in the design prototype: it has no "voice was never going to run"
   state. Dimmed and static -- deliberately not .alert, which would put a red
   dot on every CGO-less Windows release build as its NORMAL rendering. */
.dual-pill .d.off { background: var(--tx-4); box-shadow: none; }
```

- [ ] **Step 6: Pass `onNavigate` from MainApp**

In `frontend/src/windows/main/MainApp.tsx`, change `<StatusBar />` to:

```tsx
      <StatusBar onNavigate={setView} />
```

`"server"` is already a `NAV_ITEMS` key ("Server Details") and already falls through to `<Placeholder />` in MainApp's `screen` ternary, so this needs no new screen — Phase 9 replaces the placeholder.

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/shared/components/StatusBar.test.tsx`
Expected: PASS — all tests including the StrictMode block and the zero-RTT case.

- [ ] **Step 8: Typecheck and commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend && npx tsc --noEmit
cd /Users/schiba/Projects/vanguard/vcs-srs-client
git add frontend/src/shared/components/StatusBar.tsx frontend/src/shared/components/StatusBar.test.tsx frontend/src/shared/styles/ frontend/src/windows/main/MainApp.tsx
git commit -m "feat(frontend): status bar dual-pill with real per-plane health

Renders the prototype's .dual-pill unconditionally rather than only in
distributed mode. Standalone genuinely has two independently-failing
transports, and the ConnBanner's control-only / voice-only variants are only
legible if the pill can show WHICH half is down -- the prototype's single-dot
standalone branch cannot express that. Phase 9 fills in per-host names and
adds hosts; it does not restructure the widget.

The status bar has rendered a hardcoded '— ms' since Phase 1, because the
ping ticker master spec §6 DoD 9 promised was never wired. It now shows two
real measurements.

.d.off is the one class the prototype does not define, because it has no
'voice was never going to run' state. Dimmed and static, deliberately not
.alert: on every CGO-less Windows release build that is the NORMAL rendering,
and a red dot there tells the user something is broken that was never going
to run.

The region item is dropped rather than left as a permanent em dash -- there
is no data source for it, and none on the wire. Filed as PROTO_GAPS #10
alongside the missing server display name, which is why both segments show
the same host address today.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Frontend — the ConnBanner

**Files:**
- Modify: `frontend/src/shared/components/ConnBanner.tsx`
- Test: `frontend/src/shared/components/ConnBanner.test.tsx` (create)

**Interfaces:**
- Consumes: `useConnection`, `bannerVariant`, `type BannerVariant` (Task 5); `api.reconnect()`, `api.reconnectVoice()` (Task 5 Step 5).
- Produces: nothing downstream.

- [ ] **Step 1: Write the failing test for the three variants**

Create `frontend/src/shared/components/ConnBanner.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { StrictMode } from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";

const reconnect = vi.fn();
const reconnectVoice = vi.fn();

vi.mock("../api/client", () => ({
  api: {
    reconnect: () => reconnect(),
    reconnectVoice: () => reconnectVoice(),
  },
}));

import { ConnBanner } from "./ConnBanner";
import { useConnection, emptyConnection, type ConnectionState } from "../store/connection";

const ok = { state: "connected", rtt_ms: 8, healthy: true, available: true, error: "" };
const down = { state: "disconnected", rtt_ms: -1, healthy: false, available: true, error: "" };
const voiceDown = { state: "closed", rtt_ms: -1, healthy: false, available: true, error: "" };
const voiceNA = { state: "unavailable", rtt_ms: -1, healthy: false, available: false, error: "" };

const set = (over: Partial<ConnectionState>) =>
  useConnection.setState({ conn: { server: "127.0.0.1:5002", control: ok, voice: ok, ...over } });

describe("ConnBanner", () => {
  beforeEach(() => {
    reconnect.mockReset().mockResolvedValue(undefined);
    reconnectVoice.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });
  afterEach(() => vi.restoreAllMocks());

  it("renders nothing when both planes are healthy", () => {
    set({});
    const { container } = render(<ConnBanner />);
    expect(container.querySelector(".conn-banner")).toBeNull();
  });

  it("renders DISCONNECTED when both are down", () => {
    set({ control: down, voice: voiceDown });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("DISCONNECTED")).toBeInTheDocument();
    expect(screen.getByText(/FULL RECONNECT/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.alert")).not.toBeNull();
  });

  it("renders CONTROL DEGRADED when control alone is down", () => {
    set({ control: down, voice: ok });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("CONTROL DEGRADED")).toBeInTheDocument();
    expect(screen.getByText(/RECONNECT CONTROL/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.warn")).not.toBeNull();
  });

  it("renders VOICE DEGRADED when voice alone is down", () => {
    set({ control: ok, voice: voiceDown });
    const { container } = render(<ConnBanner />);

    expect(screen.getByText("VOICE DEGRADED")).toBeInTheDocument();
    expect(screen.getByText(/RECONNECT VOICE/)).toBeInTheDocument();
    expect(container.querySelector(".conn-banner.warn")).not.toBeNull();
  });

  it("renders nothing when voice is merely unavailable", () => {
    // The normal state on every CGO-less Windows release build. A permanent
    // VOICE DEGRADED there would be advice to reconnect something that was
    // never going to connect.
    set({ control: ok, voice: voiceNA });
    const { container } = render(<ConnBanner />);
    expect(container.querySelector(".conn-banner")).toBeNull();
  });

  it("calls the control reconnect for the control variant", async () => {
    set({ control: down, voice: ok });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/RECONNECT CONTROL/));

    await waitFor(() => expect(reconnect).toHaveBeenCalledTimes(1));
    expect(reconnectVoice).not.toHaveBeenCalled();
  });

  it("calls the voice reconnect for the voice variant", async () => {
    set({ control: ok, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/RECONNECT VOICE/));

    await waitFor(() => expect(reconnectVoice).toHaveBeenCalledTimes(1));
    expect(reconnect).not.toHaveBeenCalled();
  });
});
```

- [ ] **Step 2: Write the failing test for failure feedback**

Append to `frontend/src/shared/components/ConnBanner.test.tsx`:

```tsx
describe("ConnBanner reconnect feedback", () => {
  beforeEach(() => {
    reconnect.mockReset();
    reconnectVoice.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });

  it("surfaces a failed reconnect's reason", async () => {
    // Before this the banner called `void api.reconnect()` and discarded the
    // result, so a failed reconnect was completely invisible: the button
    // appeared to do nothing at all.
    reconnect.mockRejectedValue(new Error("reconnect dial: connection refused"));
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));

    await waitFor(() =>
      expect(screen.getByText(/connection refused/)).toBeInTheDocument(),
    );
  });

  it("clears a previous failure when a retry is started", async () => {
    reconnect.mockRejectedValueOnce(new Error("connection refused"));
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));
    await waitFor(() => expect(screen.getByText(/connection refused/)).toBeInTheDocument());

    reconnect.mockResolvedValueOnce(undefined);
    fireEvent.click(screen.getByText(/FULL RECONNECT/));

    await waitFor(() => expect(screen.queryByText(/connection refused/)).toBeNull());
  });
});
```

- [ ] **Step 3: Write the failing test for the in-flight guard (Review Focus #5)**

Append to `frontend/src/shared/components/ConnBanner.test.tsx`:

```tsx
describe("ConnBanner in-flight guard", () => {
  beforeEach(() => {
    reconnect.mockReset();
    useConnection.setState({ conn: emptyConnection() });
  });

  // Review Focus #5. App.Reconnect re-dials and re-pushes the persisted
  // radios; two in flight race each other's stream generation, and the
  // loser's stream is cancelled under a connection the user thinks is live.
  it("does not fire a second reconnect while the first is in flight", async () => {
    let release!: () => void;
    reconnect.mockReturnValue(
      new Promise<void>((res) => {
        release = res;
      }),
    );
    set({ control: down, voice: voiceDown });
    render(<ConnBanner />);

    const btn = screen.getByText(/FULL RECONNECT/).closest("button")!;
    fireEvent.click(btn);
    fireEvent.click(btn);
    fireEvent.click(btn);

    expect(reconnect).toHaveBeenCalledTimes(1);
    expect(btn).toBeDisabled();

    release();
    await waitFor(() => expect(btn).not.toBeDisabled());
  });
});

/**
 * ConnBanner renders inside StrictMode in the main window root. It holds
 * in-flight and error state across a reconnect, so the double-render must not
 * reset either. See the #30 lesson.
 */
describe("ConnBanner under StrictMode", () => {
  beforeEach(() => {
    reconnect.mockReset().mockResolvedValue(undefined);
    useConnection.setState({ conn: emptyConnection() });
  });

  it("renders one banner and fires one reconnect under the double-render", async () => {
    set({ control: down, voice: voiceDown });
    const { container } = render(
      <StrictMode>
        <ConnBanner />
      </StrictMode>,
    );

    expect(container.querySelectorAll(".conn-banner")).toHaveLength(1);

    fireEvent.click(screen.getByText(/FULL RECONNECT/));
    await waitFor(() => expect(reconnect).toHaveBeenCalledTimes(1));
  });
});
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd frontend && npx vitest run src/shared/components/ConnBanner.test.tsx`
Expected: FAIL — the component still reads `useSession().conn` and renders only the `disconnected` variant.

- [ ] **Step 5: Implement the banner**

Replace `frontend/src/shared/components/ConnBanner.tsx` entirely:

```tsx
import { useState } from "react";
import { Icon } from "./Icon";
import { api } from "../api/client";
import { useConnection, bannerVariant, type BannerVariant } from "../store/connection";

interface VariantConfig {
  kind: "warn" | "alert";
  title: string;
  msg: string;
  action: string;
  /** Which reconnect the action button drives. */
  target: "control" | "voice";
}

/** Copy taken verbatim from the design prototype's `shell.jsx` ConnBanner.
 *  `control-only` means "control is fine, voice is not", matching the
 *  prototype's own naming. */
const VARIANTS: Record<Exclude<BannerVariant, "none">, VariantConfig> = {
  "control-only": {
    kind: "warn",
    title: "VOICE DEGRADED",
    msg: "Connection to voice server lost — control is healthy. You cannot transmit or hear traffic until reconnected.",
    action: "RECONNECT VOICE",
    target: "voice",
  },
  "voice-only": {
    kind: "warn",
    title: "CONTROL DEGRADED",
    msg: "Lost link to control server — voice continues but state is frozen. Profile changes won't persist.",
    action: "RECONNECT CONTROL",
    target: "control",
  },
  disconnected: {
    kind: "alert",
    title: "DISCONNECTED",
    msg: "All servers unreachable. Audio is muted. Verify network and retry.",
    action: "FULL RECONNECT",
    target: "control",
  },
};

/**
 * ConnBanner is the connection-degraded banner, ported from the design
 * prototype's `shell.jsx` ConnBanner.
 *
 * All three of the prototype's variants are reachable as of Phase 6. Before
 * it, only `disconnected` was ported, and it could essentially never fire:
 * both `ConsumeUpdates` goroutines discarded the stream's terminating error,
 * so nothing ever reported a control link that died on its own.
 *
 * A voice plane that is merely UNAVAILABLE — no secret yet, no session, or a
 * build whose codec is the stub — drops out of the banner's input entirely.
 * It is not a failure the user can act on, and on every CGO-less Windows
 * release build it is the normal state.
 *
 * classNames are byte-identical to the design so the ported CSS applies.
 */
export function ConnBanner() {
  const conn = useConnection((s) => s.conn);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  const variant = bannerVariant(conn);
  if (variant === "none") return null;
  const conf = VARIANTS[variant];

  const onAction = () => {
    // Guarded rather than merely disabled: App.Reconnect re-dials and
    // re-pushes the persisted radios, and two in flight race each other's
    // stream generation — the loser's stream is cancelled under a connection
    // the user believes is live.
    if (busy) return;
    setBusy(true);
    setFailure(null);
    const call = conf.target === "voice" ? api.reconnectVoice() : api.reconnect();
    void Promise.resolve(call)
      .catch((err: unknown) => {
        // Surfaced, not discarded. The banner used to call
        // `void api.reconnect()` and drop the result, so a failed reconnect
        // was invisible and the button appeared to do nothing at all.
        setFailure(err instanceof Error ? err.message : String(err));
      })
      .finally(() => setBusy(false));
  };

  return (
    <div className={`conn-banner ${conf.kind}`}>
      <span className="blink" />
      <span style={{ fontWeight: 600, letterSpacing: "0.18em" }}>{conf.title}</span>
      <span
        style={{
          color: "var(--tx-2)",
          letterSpacing: 0,
          textTransform: "none",
          fontFamily: "var(--ff-sans)",
        }}
      >
        {failure ?? conf.msg}
      </span>
      <span style={{ flex: 1 }} />
      <button className="btn btn-sm" onClick={onAction} disabled={busy}>
        <Icon name="refresh" size={10} /> {busy ? "RECONNECTING…" : conf.action}
      </button>
    </div>
  );
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd frontend && npx vitest run src/shared/components/ConnBanner.test.tsx`
Expected: PASS — all tests.

> If the in-flight test fails because the button text changes to `RECONNECTING…` and `getByText(/FULL RECONNECT/)` no longer matches, that is why the test captures `btn` via `.closest("button")` **before** the first click. Keep that ordering.

- [ ] **Step 7: Run the whole frontend suite and typecheck**

Run:
```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client/frontend
npx vitest run
npx tsc --noEmit
npm run build
```
Expected: all green. `Welcome.test.tsx` and `CommsApp.test.tsx` may need their mocks extended if they render `ConnBanner` or `StatusBar` transitively — add `getConnectionState`/`reconnectVoice` to their `api` mocks if so.

- [ ] **Step 8: Commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
git add frontend/src/shared/components/ConnBanner.tsx frontend/src/shared/components/ConnBanner.test.tsx frontend/src/windows/
git commit -m "feat(frontend): all three ConnBanner variants, with working reconnect

Only the disconnected variant was ported, and it could essentially never
fire: both ConsumeUpdates goroutines discarded the stream's terminating
error, so nothing ever reported a control link that died on its own. With
Task 2 reporting it, control-only and voice-only become reachable too.

The reconnect button used to call \`void api.reconnect()\` and discard the
result. A failed reconnect was therefore completely invisible -- the button
appeared to do nothing. It now shows an in-flight state and puts the failure
reason in the banner's existing message slot; no new visual.

The in-flight state is a guard, not just a disabled attribute. App.Reconnect
re-dials and re-pushes the persisted radios, and two in flight race each
other's stream generation, cancelling the loser's stream under a connection
the user believes is live.

A voice plane that is merely UNAVAILABLE drops out of the banner's input
entirely: it is not a failure the user can act on, and on every CGO-less
Windows release build it is the normal state.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Docs, manual checklist, and full verification

**Files:**
- Modify: `docs/PROTO_GAPS.md`
- Modify: `docs/ROADMAP.md`
- Modify: `CLAUDE.md`
- Create: `docs/superpowers/plans/2026-09-26-phase-6-manual-verification.md`

- [ ] **Step 1: Add PROTO_GAPS entry #10**

Insert before the "## How to use this document" section of `docs/PROTO_GAPS.md`:

````markdown
---

## 10. Server identity and region

**Where it shows up:** Status bar `dual-pill` (`.val` per segment) and the prototype's region item.

**Today:** The client renders `host:port` from `server_url` in both segments, and omits the region entirely rather than showing a permanent em dash for a field with no data source.

The design prototype's `StatusBar` renders `vanguard-prime · {latency}ms` and `{app.server.region}`. `srs.proto` carries neither. `SyncResponse.version` is the only server-identifying string on the wire, and it is a version, not a name.

**Suggested proto change:**

```proto
message ServerSyncResult {
  // ... existing fields ...
  string server_name   = 9;  // NEW — human-readable display name, e.g. "vanguard-prime"
  string server_region = 10; // NEW — e.g. "eu-central"
}
```

**Not blocking.** The pill is fully functional with a host address. Phase 9 needs per-voice-host names for the distributed Server Network panel anyway, so this is best negotiated once, alongside `DistributionUpdate`.
````

- [ ] **Step 2: Write the manual verification checklist**

Create `docs/superpowers/plans/2026-09-26-phase-6-manual-verification.md`:

```markdown
# Phase 6 — manual verification checklist

**Status:** outstanding. Phase 6 is code-complete, **not field-verified.**

Every item here needs a human, a real server, and a real network. The
automated suite cannot reach any of them.

## Why this list is longer on assumptions than its predecessors

**Nobody has ever watched this client lose a control connection to a real
server.** The whole phase is built on behaviours read out of source rather
than observed:

- The **5s × 3 failure threshold** is designed, not measured. It is the
  interval already persisted as this client's default and the exact budget
  `internal/voice` uses, so the two planes agree — but no one has seen how
  it behaves on a lossy link.
- The **~70s transport floor** comes from reading the server's
  `KeepaliveParams{Time: 60s, Timeout: 10s}`. Never timed.
- **Voice RTT has never been read by a human.** Phase 5 was never heard by
  one either.

Phases 3, 3.5, 4 and 5 are all code-complete and never field-verified. This
inherits all of it.

## Control-plane detection

- [ ] **Cable pull.** With a live session, pull the network cable or disable
      Wi-Fi. Does the banner appear within ~15s? Which pill dot goes first,
      control or voice? Record the actual elapsed time.
- [ ] **Server restart.** Restart the server under a live client. Banner
      appears; manual reconnect succeeds; radios are still tuned afterwards
      (the Phase 5 I1 re-push path).
- [ ] **Half-open connection.** Drop the link with a stateful firewall rule
      (not a cable pull — the point is that the TCP connection stays open).
      Confirm the ping detector fires at ~15s rather than waiting out the
      transport's ~70s. **This is the single claim the phase rests on that
      has never been observed.**
- [ ] **Flapping link.** Introduce ~20% packet loss. Confirm the pill flickers
      between ok and warn WITHOUT the banner appearing — the failure counter
      must reset on every success.
- [ ] **Laptop sleep/wake.** Sleep the machine mid-session, wake it. What does
      the surface report, and does manual reconnect recover it?

## The two server behaviours this phase makes visible

- [ ] **`SubscribeToUpdates` "already subscribed" race.** Reconnect fast,
      repeatedly (10+ times in quick succession). The server rejects a second
      subscription for the same `clientID` while the old stream's cleanup is
      still pending. Before Phase 6 that error vanished into `_ =`; now it
      surfaces as `disconnected`. Watch for a reconnect bounce. **If this
      fires, it is pre-existing and cross-repo, not a Phase 6 regression.**
- [ ] **Ghost clients.** After a real drop and reconnect, check the player
      list (and the server's admin view) for a duplicate of yourself. The
      server removes a dead stream's client from `s.streams` but not from
      `serverState.Clients`. Reconnect reuses the same token and GUID so it
      *should* overwrite — that is reasoning from code, not an observation.

## Latency

- [ ] **Control RTT** renders a plausible figure against a real server and
      updates every ~5s.
- [ ] **Voice RTT** renders a plausible figure. First time a human has seen
      this number.
- [ ] **Server-side latency map.** Confirm the server's admin view now shows
      a non-zero `LatencyToControlMs` for this client. It has read zero for
      every VCS client since Phase 1.

## The unavailable state

- [ ] **Windows release build.** Confirm the VOICE segment renders dimmed
      (`.d.off`) with `—` latency and **no banner**. This is the normal state
      until Phase 5 issue #1 (`CGO_ENABLED=0`) is fixed, and it must not look
      like an error.
- [ ] **Pre-connect.** Before logging in, confirm both planes read honestly:
      control disconnected, voice unavailable, no banner.

## Reconnect UX

- [ ] **In-flight state.** Click RECONNECT during an outage: the button
      disables and reads `RECONNECTING…`, and rapid repeat clicks fire one
      call.
- [ ] **Failure reason.** Reconnect against a server that is still down. The
      banner's message slot shows the actual reason, not the generic copy.
- [ ] **RECONNECT VOICE.** Kill only the voice path (block UDP, leave gRPC).
      Confirm the `VOICE DEGRADED` banner appears and the button forces a
      re-dial rather than waiting out `voice.Session`'s 15s rung.

## Connection sounds

- [ ] **Still silent.** `connect.wav` / `disconnect.wav` do not exist. Confirm
      transitions are silent and log once, never crash. This item goes green
      only when the sample pack lands — nine files now, not seven.
```

- [ ] **Step 3: Update the ROADMAP Phase 6 row**

Replace the Phase 6 section of `docs/ROADMAP.md`:

```markdown
## Phase 6 — Connection-status surface

**Status:** `[x]` complete 2026-09-26 — control-plane liveness detection, the ping ticker (closing master spec §6 DoD 9), the `connhealth` dual-plane model, `connection:state`, the status bar's dual-pill, all three ConnBanner variants and a working reconnect UX landed on `feat/phase-6-connection-status`.

**Verification status:** the full automated suite is green (`go build`/`go vet`/`go test -race ./...` with `-tags purego`, frontend `vitest`/`tsc --noEmit`/production build). **The phase has NOT been verified on real hardware, and nobody has ever watched this client lose a control connection to a real server.** The 5s × 3 failure threshold, the half-open detection claim and the ~70s transport floor are all designed from the server's source, not measured; voice RTT has never been read by a human. Checklist: [`2026-09-26-phase-6-manual-verification.md`](./superpowers/plans/2026-09-26-phase-6-manual-verification.md).

**Design doc:** [`2026-09-26-vcs-client-phase-6-connection-status-design.md`](./superpowers/specs/2026-09-26-vcs-client-phase-6-connection-status-design.md)

**Four gaps this phase found and closed** (none of them recorded in this file beforehand):
1. **`ConnBanner` was live code that could essentially never fire.** Both `ConsumeUpdates` goroutines discarded the stream's terminating error, so a server restart or network drop emitted nothing and the UI reported `connected` indefinitely.
2. **The Phase 1 ping ticker was never wired.** `PingOnce` had zero callers, `PingIntervalSeconds` zero readers, and the server's `LatencyToControlMs` read zero for every VCS client.
3. **Phase 5's `voice:state` was emitted into the void** — absent from the frontend's `EV` map. `voice.Session.RTT()` was surfaced nowhere at all.
4. **The prototype's status bar rendered a server name and region not on the wire.** Filed as PROTO_GAPS #10.

**Headline deliverables**
- `internal/connhealth`: derived dual-plane model, one ticker, one `connection:state` event
- Stream-termination detection under intent and generation guards; `MarkControlLost`
- Ping ticker at 5s with a 3-failure loss threshold, mirroring `internal/voice`'s own budget
- gRPC client keepalive at 75s, respecting the server's `MinTime: 60s` enforcement floor
- Status bar `dual-pill`, rendered unconditionally (standalone included), with a fourth `unavailable` state for voice
- All three `ConnBanner` variants, in-flight reconnect state, and failure reasons surfaced
- `App.ReconnectVoice()`; connection SFX wired asset-agnostically (ships silent)

**Blocking deps:** none remaining.

**Two server behaviours made visible, not fixed** — both cross-repo, neither confirmed to fire. `SubscribeToUpdates` rejects a duplicate subscription while the old stream's cleanup is pending; a dead stream leaves the client in `serverState.Clients`. Both are on the manual checklist.

**Still outstanding:** the SFX sample pack is now **nine** files, not seven — `connect.wav` and `disconnect.wav` joined the seven from Phase 4.
```

Also update the Phase 4 SFX note and the "Current status" table in `CLAUDE.md` to say nine files, and add a Phase 6 row.

- [ ] **Step 4: Run the complete verification suite**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
GOCACHE=$TMPDIR/vcs-gocache go build -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go vet -tags purego ./...
GOCACHE=$TMPDIR/vcs-gocache go test -tags purego -race ./...
cd frontend && npx vitest run && npx tsc --noEmit && npm run build
```
Expected: every command exits 0. Record the actual output — do not claim green without it.

- [ ] **Step 5: Commit**

```bash
cd /Users/schiba/Projects/vanguard/vcs-srs-client
git add docs/ CLAUDE.md
git commit -m "docs: Phase 6 roadmap, proto gap #10, manual checklist

The checklist is longer on stated assumptions than its predecessors for a
reason: nobody has ever watched this client lose a control connection to a
real server. The 5s × 3 threshold, the half-open detection claim and the
~70s transport floor are all read out of the server's source rather than
measured, and voice RTT has never been seen by a human.

Two items on it are pre-existing server behaviours this phase makes visible
rather than introduces -- the SubscribeToUpdates duplicate-subscription race
and ghost clients in serverState.Clients. Both are flagged so a report is
recognised rather than re-diagnosed as a Phase 6 regression.

PROTO_GAPS #10 records the server display name and region the prototype's
status bar renders and srs.proto does not carry.

The SFX sample pack is now nine files, not seven.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review Notes

Checked against the spec:

- **§3.1 stream termination** → Task 2 Steps 4–9. **§3.2 ping ticker** → Task 1 Steps 9–16 + Task 2 Steps 10–12. **§3.3 keepalive** → Task 2 Steps 13–14.
- **§4 connhealth** → Task 1 entire. **§4.1 why not extend `control:connection`** → documented on `EventConnectionState` in Task 3 Step 3.
- **§5 voice plane** (subscribe, RTT, availability) → Task 3 Steps 10–13 + Task 5 Step 5.
- **§6.1 dual-pill + dot table** → Task 5 Step 3 (derivations), Task 6 (render). **§6.2 banner table** → Task 5 Step 3, Task 7. **§6.3 unavailable** → Task 5/6/7 each have a test. **§6.4 reconnect UX** → Task 7 Steps 2–5, plus `ReconnectVoice` in Task 3 Step 8.
- **§7 SFX** → Task 4. **§8 proto gap** → Task 8 Step 1. **§9 server behaviours** → Task 8 Step 2 (checklist, not code — correct, they are out of scope).
- **§10.1/§10.2 testing** → every task is TDD with StrictMode coverage on the two subscribing/stateful components. **§10.3 manual** → Task 8 Step 2.
- **§11 scope** → the Out list is respected: no automatic reconnect, no distributed UI, no hotkey-notification routing, no TLS, no server fixes, no sample files.
- **§14 DoD** → items 1–3 Task 2; 4 Task 2; 5–6 Tasks 1/3; 7 Tasks 3/5; 8 Task 6; 9 Task 7; 10 Task 4; 11 Tasks 5/6/7; 12 Task 8 Step 4; 13 Task 8; 14 Task 8 Step 3.

Type consistency: `ConnLink`/`ConnectionState` (TS) mirror `ConnLinkDTO`/`ConnectionStateDTO` (Go) mirror `Link`/`Snapshot` (connhealth) — field-for-field, with `rtt_ms` spelled identically across all three. `RTTUnknown = -1` is pinned in Go and as the `ms < 0` branch in `formatRTT`.
