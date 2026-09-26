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
