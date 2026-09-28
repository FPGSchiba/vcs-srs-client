# Audio assets

Drop the SFX pack's WAV files here. They are embedded into the binary at
build time by `internal/audio/sfx.go`.

**Contract: 48 kHz, mono, 16-bit PCM WAV.** Off-spec files are downmixed and
linearly resampled at load with a logged warning rather than rejected, but
shipping them on-spec avoids the conversion entirely.

Filenames must match the `file` entries in `manifest.toml`.

The pack is supplied by the project (credited in the design as
`AUDIO · Spaceharvest, JohnMckeel`) and is a **blocking dependency tracked in
the Phase 4 spec**, not something to substitute. Until it lands every effect
slot is silent and the app works normally.

## Notification sounds

`notify_alert.wav` backs the notification channel's audible alert
(Phase 7.2). It is **not** listed in `manifest.toml`: that file drives the
Radio Effects panel's row set, and a notification sound does not belong
there. `internal/audio/notifsfx.go` embeds it by name instead.

It plays for error-severity notifications only — a dead microphone, an
output device that will not open, global hotkeys that failed to register.
It should be short and unobtrusive: it can fire while a transmission is
being received.

**This brings the outstanding pack to ten files, not nine.**
