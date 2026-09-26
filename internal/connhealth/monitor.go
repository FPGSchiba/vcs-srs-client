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

	// emitMu is acquired here, not around the probe above: Ping runs with no
	// lock held so a slow probe never blocks a concurrent SetControlState or
	// SetVoiceState. From here on this is a commit, so it follows the same
	// emitMu -> mu -> mutate -> unlock mu -> publish -> unlock emitMu order
	// as every setter -- see publish's doc comment.
	//
	// NOT deferred: emitMu must be released before OnLoss fires below, not
	// held across it. The owner's expected reaction to OnLoss is a
	// synchronous SetControlState call from the SAME goroutine, and that
	// call acquires emitMu itself -- sync.Mutex is not reentrant, so holding
	// it here across OnLoss would deadlock this goroutine against itself on
	// the first real control-plane loss. See Options.OnLoss's doc comment.
	m.emitMu.Lock()

	m.mu.Lock()
	// Re-check: Ping and VoiceRTT above ran with no lock held, so a
	// concurrent SetControlState may have moved the control plane away from
	// "connected" while this probe was in flight. A probe answering for a
	// connection that is already gone must not be applied -- not the RTT,
	// not Healthy, not the failure count, not loss.
	if m.snap.Control.State != StateConnected {
		m.mu.Unlock()
		m.emitMu.Unlock()
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
	// emitMu is released here, BEFORE OnLoss fires -- see the comment above
	// where it was acquired. Holding it one line longer is the whole defect.
	m.emitMu.Unlock()

	if fireLoss && m.opt.OnLoss != nil {
		// The Monitor does NOT set disconnected itself: the owner emits the
		// loss through its single normal path, which comes back in via
		// SetControlState. One emission path, two detectors, no disagreement.
		// Called with NEITHER mu NOR emitMu held: SetControlState (the
		// owner's expected synchronous reaction) acquires emitMu itself.
		m.opt.OnLoss()
	}
}
