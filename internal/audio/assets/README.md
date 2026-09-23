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
