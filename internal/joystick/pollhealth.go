package joystick

import "github.com/FPGSchiba/vcs-srs-client/internal/trigger"

// winPollFailuresBeforeRecreate is how many CONSECUTIVE per-device poll
// failures the Windows backend tolerates before it drops the DirectInput
// device object entirely, so the next Devices() re-creates it from scratch.
//
// WHY A THRESHOLD EXISTS AT ALL. Poll's `_ = d.dev.Acquire()` retry recovers
// exactly one failure mode -- DIERR_INPUTLOST, which is what a cooperative
// non-exclusive device reports after a focus change and which the very next
// tick's Acquire fixes. It does NOT recover DIERR_UNPLUGGED: once the
// underlying device is gone, that COM object is dead for good, and no amount
// of re-acquiring brings it back. Before this, such a device sat in
// s.devices forever reporting nothing held while Devices() -- which
// short-circuits on the cache before any liveness check -- kept listing it as
// attached, so every binding on it was silently dead for the session and the
// UI still rendered its chips connected. Re-enumeration alone cannot fix that
// either, because the device is keyed on its instance GUID, which is stable
// across a replug BY DESIGN (that is what makes bindings survive one).
//
// WHY 100. At DefaultPollInterval (10ms) that is ~1 second of unbroken
// failure. The two bounds it has to sit between:
//
//   - Well above a transient. An ordinary DIERR_INPUTLOST clears on the next
//     tick, so it can never reach a run of 2, let alone 100. Dropping and
//     re-creating a healthy device on every alt-tab would churn COM objects
//     for no reason and briefly blank the device's chips.
//   - Well below DefaultRediscoverInterval (3s). The re-create happens in
//     Devices(), so the drop is only useful if it lands inside the current
//     rediscover window; at 1s the device is gone from the cache in time for
//     the enumeration that follows, giving a worst case of ~4s from unplug
//     to a working re-created handle. A threshold at or beyond 3s would
//     regularly waste a whole rediscover cycle.
//
// The Linux backend does not need this knob: it revalidates every cached
// handle during enumeration (see linuxSource.Devices), which catches a dead
// fd directly rather than inferring one from a failure count.
const winPollFailuresBeforeRecreate = 100

// pollFailures rate-limits per-device Poll failure logging and counts
// consecutive failures.
//
// It is deliberately the SAME shape as winSource.noteOpenFailure's openFailed
// map, keyed on the failing CALL so that one line is written per device per
// TRANSITION rather than one per tick: Poll runs every DefaultPollInterval
// (10ms), so an unconditional Warn would write 100 lines a second for the
// life of the process. That cost is precisely why both backends used to
// swallow per-device poll errors with a bare `continue` -- which is what made
// a stale handle undiagnosable, since from outside the process "the stick
// reports nothing held" and "the stick's handle is dead" look identical.
//
// It carries NO lock of its own. Both callers mutate it only from inside
// their source's Poll/Devices, which hold the source's own mutex for the
// whole call.
type pollFailures struct {
	m map[trigger.DeviceID]pollFailureState
}

type pollFailureState struct {
	// call is the last call that failed, e.g. "GetDeviceState".
	call string
	// n is the number of consecutive failures, reset by ok.
	n int
}

func newPollFailures() *pollFailures {
	return &pollFailures{m: map[trigger.DeviceID]pollFailureState{}}
}

// note records one failure of call for id. It reports whether this failure
// should be LOGGED -- true on the first failure, when the failing call
// changes, and on any failure after a recovery -- and how many consecutive
// failures the device has now had, including this one.
//
// The count keeps running when the failing call changes, because the device
// has not recovered; only ok resets it.
func (p *pollFailures) note(id trigger.DeviceID, call string) (logIt bool, consecutive int) {
	prev := p.m[id]
	st := pollFailureState{call: call, n: prev.n + 1}
	p.m[id] = st
	return prev.call != call, st.n
}

// ok records a clean poll, so the next failure is logged rather than
// suppressed as a repeat and the consecutive count starts over.
func (p *pollFailures) ok(id trigger.DeviceID) {
	if _, ok := p.m[id]; ok {
		delete(p.m, id)
	}
}

// forget drops a device's history entirely, for use when the device leaves.
// A replugged device gets a brand new handle, so its first failure is new
// information; and without this, every stick that came and went during a
// long session would keep an entry alive for the life of the process.
func (p *pollFailures) forget(id trigger.DeviceID) {
	delete(p.m, id)
}

// len reports how many devices are currently in a failing state. Test-facing
// leak guard; it changes no behaviour.
func (p *pollFailures) len() int { return len(p.m) }

// reuseCachedHandle reports whether an already-open device handle, opened at
// cachedPath, may be reused for a node just enumerated at foundPath, given
// whether the cached handle still answers an ioctl.
//
// This is the whole cache-keying rule, factored out so it is checkable
// without a Linux host. Both conjuncts are required; see the Devices() call
// site and handleAlive in source_linux.go for why either alone leaves a hole,
// and TestReuseCachedHandle for the decision table.
//
// The rule is about the HANDLE, never the DeviceID. The id stays
// path-independent -- that is what makes a user's bindings survive a replug
// -- and this function is what stops that same property from also making a
// dead file descriptor survive one.
func reuseCachedHandle(cachedPath, foundPath string, alive bool) bool {
	return cachedPath == foundPath && alive
}
