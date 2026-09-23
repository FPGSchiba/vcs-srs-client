package joystick

import (
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/trigger"
)

// TestPollFailuresRateLimitsLikeNoteOpenFailure pins the log-rate-limiting
// contract, which is deliberately the SAME shape winSource.noteOpenFailure
// already uses for open failures: one line per device per TRANSITION, never
// one line per tick.
//
// The rate limit is not cosmetic. Poll runs every DefaultPollInterval (10ms),
// so an unconditional Warn on a dead handle would write 100 lines a second
// for the life of the process -- which is why the previous code swallowed
// per-device poll errors with a bare `continue` instead, and why a device
// whose handle had gone stale was completely undiagnosable from the log.
func TestPollFailuresRateLimitsLikeNoteOpenFailure(t *testing.T) {
	p := newPollFailures()
	const id trigger.DeviceID = "stick"

	if logIt, n := p.note(id, "State"); !logIt || n != 1 {
		t.Fatalf("first failure: logIt=%v n=%d, want true 1", logIt, n)
	}
	// Same failing call again: counted, but NOT logged.
	for i := 2; i <= 5; i++ {
		logIt, n := p.note(id, "State")
		if logIt {
			t.Errorf("failure %d of the same call logged again; that is the 100-lines-a-second bug", i)
		}
		if n != i {
			t.Errorf("failure %d: n=%d, want %d", i, n, i)
		}
	}
	// A DIFFERENT failing call is new information, so it logs -- and the
	// consecutive count keeps running, because the device has not recovered.
	if logIt, n := p.note(id, "AbsInfos"); !logIt || n != 6 {
		t.Errorf("changed call: logIt=%v n=%d, want true 6", logIt, n)
	}
	// A clean poll resets everything, so the NEXT failure logs again rather
	// than being suppressed as a repeat.
	p.ok(id)
	if logIt, n := p.note(id, "AbsInfos"); !logIt || n != 1 {
		t.Errorf("failure after recovery: logIt=%v n=%d, want true 1", logIt, n)
	}
}

// TestPollFailuresAreTrackedPerDevice guards the obvious: one stick going
// dead must not silence the log or spend the recreate budget of another.
func TestPollFailuresAreTrackedPerDevice(t *testing.T) {
	p := newPollFailures()
	if logIt, n := p.note("a", "GetDeviceState"); !logIt || n != 1 {
		t.Fatalf("device a: logIt=%v n=%d, want true 1", logIt, n)
	}
	if logIt, n := p.note("b", "GetDeviceState"); !logIt || n != 1 {
		t.Errorf("device b: logIt=%v n=%d, want true 1 -- device a must not suppress it", logIt, n)
	}
	if _, n := p.note("a", "GetDeviceState"); n != 2 {
		t.Errorf("device a second failure: n=%d, want 2", n)
	}
	if _, n := p.note("b", "GetDeviceState"); n != 2 {
		t.Errorf("device b second failure: n=%d, want 2", n)
	}
}

// TestPollFailuresForgetDropsTheDeviceEntirely covers the unplug path. A
// device that goes away while failing must not carry its history back in on
// replug: the re-created handle is a different object, and its first failure
// is new information that has to reach the log.
//
// It is also a leak guard. Devices() runs forever every 3s; without forget()
// a long session that saw a dozen sticks come and go would keep an entry for
// each one alive for the life of the process.
func TestPollFailuresForgetDropsTheDeviceEntirely(t *testing.T) {
	p := newPollFailures()
	p.note("gone", "GetDeviceState")
	p.note("gone", "GetDeviceState")
	p.forget("gone")
	if got := p.len(); got != 0 {
		t.Errorf("len() = %d after forget, want 0 -- entries for departed devices leak", got)
	}
	if logIt, n := p.note("gone", "GetDeviceState"); !logIt || n != 1 {
		t.Errorf("after replug: logIt=%v n=%d, want true 1", logIt, n)
	}
}

// TestPollFailuresReachTheRecreateThreshold is the I1 guard on the counting
// half: the Windows backend drops and re-creates a device object after
// winPollFailuresBeforeRecreate consecutive failures, because a DirectInput
// device that returned DIERR_UNPLUGGED is NOT recovered by Poll's
// `_ = d.dev.Acquire()` retry -- it stays in the cache forever, reporting
// nothing held, while Devices() keeps listing it as attached and the UI keeps
// rendering its chips connected.
//
// The threshold must be high enough that an ordinary DIERR_INPUTLOST (focus
// change; the very next tick's Acquire fixes it) never trips it, and low
// enough that the drop lands well inside one 3s rediscover so the re-create
// follows promptly.
func TestPollFailuresReachTheRecreateThreshold(t *testing.T) {
	if winPollFailuresBeforeRecreate < 10 {
		t.Errorf("winPollFailuresBeforeRecreate = %d: too low -- a routine "+
			"DIERR_INPUTLOST on focus change would drop a healthy device",
			winPollFailuresBeforeRecreate)
	}
	if d := time.Duration(winPollFailuresBeforeRecreate) * DefaultPollInterval; d >= DefaultRediscoverInterval {
		t.Errorf("winPollFailuresBeforeRecreate = %d is %v at a %v poll interval, "+
			"which is not inside the %v rediscover interval -- the drop would not be "+
			"followed by a re-create promptly", winPollFailuresBeforeRecreate, d,
			DefaultPollInterval, DefaultRediscoverInterval)
	}

	p := newPollFailures()
	var n int
	for i := 0; i < winPollFailuresBeforeRecreate; i++ {
		_, n = p.note("dead", "GetDeviceState")
	}
	if n != winPollFailuresBeforeRecreate {
		t.Fatalf("after %d failures n=%d", winPollFailuresBeforeRecreate, n)
	}
	// One clean poll in between must reset the run: the threshold counts
	// CONSECUTIVE failures, not lifetime ones.
	p.ok("dead")
	if _, n := p.note("dead", "GetDeviceState"); n != 1 {
		t.Errorf("n=%d after a clean poll, want 1 -- the count is consecutive, not cumulative", n)
	}
}

// TestReuseCachedHandle is the I1 guard on the Linux cache-keying rule.
//
// linuxSource.Devices used to take a cache hit on the DeviceID alone:
//
//	if existing, ok := s.devices[id]; ok { dev.Close(); ... continue }
//
// The id is path-INDEPENDENT on purpose -- it comes from the
// /dev/input/by-id name so that bindings survive a replug -- which meant a
// device re-enumerated inside one 3s rediscover window (suspend/resume, cable
// reseat, hub glitch) had its FRESH handle closed and its DEAD one kept, with
// seen[id] true forever so the staleness sweep never dropped it.
//
// Both conditions are required, and neither alone closes the hole:
//
//   - The path alone misses a re-enumeration that lands back on the SAME
//     eventN node, which is the common case on a machine with one stick.
//   - Liveness alone would be enough in principle, but only if every dead fd
//     reliably failed the ioctl; the path check is free and makes the common
//     re-enumeration (the node moves) independent of that assumption.
func TestReuseCachedHandle(t *testing.T) {
	cases := []struct {
		name        string
		cached      string
		found       string
		alive       bool
		wantReuse   bool
		explanation string
	}{
		{
			name: "same node, live handle", cached: "/dev/input/event5",
			found: "/dev/input/event5", alive: true, wantReuse: true,
			explanation: "the ordinary steady-state pass; reopening every 3s would be waste",
		},
		{
			name: "node moved", cached: "/dev/input/event5",
			found: "/dev/input/event14", alive: true, wantReuse: false,
			explanation: "re-enumeration at a new node -- the exact I1 scenario",
		},
		{
			name: "same node, dead handle", cached: "/dev/input/event5",
			found: "/dev/input/event5", alive: false, wantReuse: false,
			explanation: "re-enumerated back onto the same number; only the ioctl sees this",
		},
		{
			name: "node moved AND handle dead", cached: "/dev/input/event5",
			found: "/dev/input/event14", alive: false, wantReuse: false,
			explanation: "both signals agree",
		},
	}
	for _, tc := range cases {
		if got := reuseCachedHandle(tc.cached, tc.found, tc.alive); got != tc.wantReuse {
			t.Errorf("reuseCachedHandle(%q, %q, alive=%v) = %v, want %v -- %s",
				tc.cached, tc.found, tc.alive, got, tc.wantReuse, tc.explanation)
		}
	}
}
