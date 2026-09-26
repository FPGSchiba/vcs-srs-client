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

	// The commit-and-publish region is scoped in a closure so emitMu's
	// unlock can be deferred within it: a panicking OnChange (invoked from
	// publish, below) must still release emitMu, or every subsequent Tick,
	// SetServer, SetControlState and SetVoiceState deadlocks on it forever.
	// SetServer/SetControlState/SetVoiceState get this for free from their
	// own top-level defer; Tick has to scope it explicitly because OnLoss
	// must fire OUTSIDE the locked region (see below), which rules out a
	// defer across the whole function.
	//
	// emitMu is acquired here, not around the probe above: Ping runs with no
	// lock held so a slow probe never blocks a concurrent SetControlState or
	// SetVoiceState. From here on this is a commit, so it follows the same
	// emitMu -> mu -> mutate -> unlock mu -> publish -> unlock emitMu order
	// as every setter -- see publish's doc comment.
	var fireLoss bool
	func() {
		m.emitMu.Lock()
		defer m.emitMu.Unlock()

		m.mu.Lock()
		// Re-check: Ping and VoiceRTT above ran with no lock held, so a
		// concurrent SetControlState may have moved the control plane away
		// from "connected" while this probe was in flight. A probe
		// answering for a connection that is already gone must not be
		// applied -- not the RTT, not Healthy, not the failure count, and
		// not loss: fireLoss is left false on this path, since it is
		// declared (zero-valued) in the enclosing scope and never assigned
		// here.
		if m.snap.Control.State != StateConnected {
			m.mu.Unlock()
			return
		}
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

		fireLoss = !probeOK && m.failures >= m.opt.FailureThreshold && !m.lossFired
		if fireLoss {
			// Latched so the threshold declares loss once, not on every
			// tick after it. Cleared by the next SetControlState
			// transition.
			m.lossFired = true
		}
		changed := m.snap != before
		snap := m.snap
		m.mu.Unlock()

		if changed {
			m.publish(snap)
		}
	}()

	if fireLoss && m.opt.OnLoss != nil {
		// The Monitor does NOT set disconnected itself: the owner emits the
		// loss through its single normal path, which comes back in via
		// SetControlState. One emission path, two detectors, no disagreement.
		// Called with NEITHER mu NOR emitMu held: the closure above has
		// already returned (and unlocked emitMu via its defer, panic or
		// not) by the time we get here, so SetControlState -- the owner's
		// expected synchronous reaction -- acquires emitMu cleanly.
		m.opt.OnLoss()
	}
}
