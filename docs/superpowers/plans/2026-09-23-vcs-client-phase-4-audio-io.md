# Phase 4 — Audio I/O Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a working audio engine — real device I/O, noise suppression, AGC, VU metering, PTT/VOX gating, radio effects and bundled SFX — with a local monitoring sink standing in for the Phase 5 network transport.

**Architecture:** Realtime audio callbacks do nothing but copy into and out of lock-free SPSC rings. One owned DSP goroutine runs the entire chain (NS → AGC → gate → voice effect) at a fixed 480-sample / 10 ms cadence and is the sole producer for playback. Everything crossing the OS boundary sits behind a `Backend` interface so the whole manager is testable with no hardware.

**Tech Stack:** Go 1.25, `github.com/gen2brain/malgo` (miniaudio bindings, cgo), vendored RNNoise (cgo), Wails v3 beta.22 bindings, React 18 + TypeScript + vitest.

**Spec:** [`docs/superpowers/specs/2026-09-23-vcs-client-phase-4-audio-io-design.md`](../specs/2026-09-23-vcs-client-phase-4-audio-io-design.md)

## Global Constraints

- **Branch:** `feat/phase-4-audio-io`, currently 1 commit ahead of `origin/main`, no upstream set (first push needs `-u`).
- **Internal audio format:** 48000 Hz, 1 channel, `float32`, frames of exactly **480 samples (10 ms)**. Forced by RNNoise. Never vary it.
- **Every Go invocation carries `-tags purego`.** Omitting it compiles a binary that behaves differently from the one shipped. See the note at the top of `Taskfile.yml`.
- **TDD is mandatory** (CLAUDE.md): failing test first, watch it fail, minimal implementation, watch it pass, commit.
- **Nothing on the realtime path logs, allocates, takes a lock, or calls cgo.** Counters accumulate; the event pump reports them.
- **Task 1 (CI matrix) lands before RNNoise is vendored** — spec D14. Toolchain failures must surface against an empty package.
- **Shared-mode audio devices only, never exclusive** — spec D6. Star Citizen is running alongside.
- **Commit message format:** `<type>: <description>`, types `feat|fix|refactor|docs|test|chore|perf|ci`. End every commit with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- **Go module:** `github.com/FPGSchiba/vcs-srs-client`.

---

## File Structure

**New Go package `internal/audio/`** — one responsibility per file, following the `internal/joystick` precedent:

| File | Responsibility |
|---|---|
| `format.go` | Sample rate / frame size constants; `Frame` helpers |
| `ring.go` | Lock-free SPSC float32 ring with drop/underrun counters |
| `agc.go` | Pure-Go automatic gain control + limiter |
| `gate.go` | Frame-counted PTT / VOX / mute state machine |
| `effect.go` | Voice-effect and clipping DSP presets (built-in, named) |
| `wav.go` | Minimal RIFF/PCM decoder |
| `rnnoise.go` + `rnnoise/` | cgo wrapper and vendored C sources |
| `backend.go` | `Backend` / `Stream` / `DeviceInfo` interfaces |
| `backend_malgo.go` | Real malgo implementation |
| `backend_fake.go` | Test double (scripted input, recorded output) |
| `sfx.go` | Asset manifest, embedded FS, voice pool, preview |
| `mixer.go` | Four level buses + perceptual taper |
| `manager.go` | Lifecycle, DSP goroutine, hot-plug poll, device-loss fallback |
| `assets/` | `manifest.toml` + (later) the user's WAV pack |

**Modified:**

| File | Change |
|---|---|
| `.github/workflows/test.yml` | Go job becomes a 3-OS matrix |
| `internal/config/config.go` | `Audio` struct, `[audio]` table, defaults |
| `internal/events/events.go` | Four `audio:*` event names + emitter methods |
| `internal/app/dto.go` | `AudioSettingsDTO`, `AudioDevicesDTO`, `AudioStateDTO`; `SettingsDTO` gains `Audio` |
| `internal/app/settings.go` | Audio fields in `GetSettings`/`SetSettings`; `SetAudioBackend` |
| `internal/app/audio.go` *(new)* | Audio bindings + PTT/mute action wiring |
| `main.go` | Construct and start the audio manager |
| `frontend/src/shared/components/VU.tsx` *(new)* | VU meter, ported from design |
| `frontend/src/shared/components/Knob.tsx` *(new)* | Rotary knob, ported from design |
| `frontend/src/windows/main/screens/settings/sections/Audio.tsx` *(new)* | Replaces the `Deferred` stub |
| `frontend/src/windows/main/screens/settings/sections/Effects.tsx` *(new)* | Replaces the `Deferred` stub |
| `frontend/src/shared/store/settings.ts` | `Settings` gains `audio`; audio device/state slices |
| `frontend/src/shared/api/client.ts`, `events.ts` | New bindings + event names |
| `build/*/Info.plist` | `NSMicrophoneUsageDescription` |

---

## Task 1: Per-OS CI matrix (closes R3)

Lands **first and alone**, per spec D14 — against a repo with no audio code, so any toolchain failure is unambiguously a CI-scoping problem rather than an audio bug.

**Files:**
- Modify: `.github/workflows/test.yml:12-76` (the `backend` job)

**Interfaces:**
- Consumes: nothing
- Produces: a green 3-OS Go job that every later task relies on for cross-platform signal

- [ ] **Step 1: Read the current job**

Run: `sed -n '11,76p' .github/workflows/test.yml`

Note three Linux-only assumptions that must become conditional: the `apt-get` GUI-deps step, the `xvfb-run -a` test prefix, and the `/tmp/vcs-client` build output path.

- [ ] **Step 2: Rewrite the `backend` job as a matrix**

Replace the job header and the Linux-specific steps with:

```yaml
  backend:
    name: Backend (Go) · ${{ matrix.os }}
    runs-on: ${{ matrix.os }}
    strategy:
      # fail-fast: false so one platform's toolchain problem does not hide
      # the other two. The whole point of this job is per-OS signal.
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
    defaults:
      run:
        # Windows runners default to pwsh; bash keeps one command form for
        # all three platforms (git-bash ships on the Windows runner).
        shell: bash
    steps:
      - uses: actions/checkout@v6

      - name: Setup Go
        uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true

      # Linux only: Wails v3 links against GTK4 + WebKitGTK 6.0 there, and
      # xvfb backs internal/hotkeys' real X11 registration. macOS and Windows
      # need neither -- their Wails backends are system frameworks.
      - name: Install Linux GUI deps
        if: matrix.os == 'ubuntu-latest'
        run: |
          sudo apt-get update
          sudo apt-get install -y libgtk-4-dev libwebkitgtk-6.0-dev libsoup-3.0-dev libx11-dev xvfb

      - name: Install buf
        run: go install github.com/bufbuild/buf/cmd/buf@v1.47.2

      - name: Generate proto bindings
        run: buf generate

      - name: Provide embed placeholder for frontend/dist
        run: |
          mkdir -p frontend/dist
          echo '<!doctype html><title>ci</title>' > frontend/dist/index.html

      - name: Go vet
        run: go vet -tags purego ./...

      # xvfb-run only exists on Linux; elsewhere the tests run bare.
      - name: Go test (race) · Linux
        if: matrix.os == 'ubuntu-latest'
        run: xvfb-run -a go test -tags purego -race ./...

      - name: Go test (race) · macOS / Windows
        if: matrix.os != 'ubuntu-latest'
        run: go test -tags purego -race ./...

      # Relative output: /tmp is not a meaningful path on the Windows runner.
      - name: Go build
        run: go build -tags purego -o ./vcs-client-ci .
```

Keep every existing explanatory comment in the file that still applies — the `-tags purego` note and the embed-placeholder note are both still true on all three platforms.

- [ ] **Step 3: Validate the workflow YAML parses**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/test.yml')); print('yaml ok')"`
Expected: `yaml ok`

- [ ] **Step 4: Commit and push so CI actually runs**

```bash
git add .github/workflows/test.yml
git commit -m "ci: run the Go job on ubuntu, macos and windows

Closes spec risk R3, which the master spec assigned to Phase 3 and which
never landed. Phase 4 adds two cgo dependencies (malgo, RNNoise), so the
per-OS toolchain signal has to exist before they arrive rather than after.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push -u origin feat/phase-4-audio-io
```

- [ ] **Step 5: Watch the three jobs and triage**

Run: `gh run list --branch feat/phase-4-audio-io --limit 1` then `gh run watch <id>`
Expected: all three green.

**If the macOS job fails inside `internal/hotkeys`' registrar tests**, that is the anticipated outcome flagged in spec §15 — a CI runner cannot grant Accessibility. Do **not** disable the matrix. Fix it by making those tests skip when the permission checker reports the grant is unavailable, using the existing `internal/hotkeys/permission*.go` plumbing, and commit that as a separate `test:` commit explaining why the skip is correct rather than a coverage loss.

**If the Windows job fails on cgo**, check that a C toolchain is on PATH; the runner ships mingw-w64. Record whatever you find in the commit message — Task 7 depends on this being understood.

---

## Task 2: Audio format constants and the SPSC ring

**Files:**
- Create: `internal/audio/format.go`
- Create: `internal/audio/ring.go`
- Test: `internal/audio/ring_test.go`

**Interfaces:**
- Consumes: nothing
- Produces:
  - `audio.SampleRate = 48000`, `audio.Channels = 1`, `audio.FrameSamples = 480`, `audio.FrameDuration = 10 * time.Millisecond`
  - `func NewRing(capacityFrames int) *Ring`
  - `func (r *Ring) Write(src []float32) (dropped int)`
  - `func (r *Ring) Read(dst []float32) (n int)`
  - `func (r *Ring) Drain()`
  - `func (r *Ring) Dropped() uint64`, `func (r *Ring) Underruns() uint64`

- [ ] **Step 1: Write `format.go`**

```go
// Package audio implements the client's capture -> process -> playback
// pipeline.
//
// THE INTERNAL FORMAT IS NOT A PREFERENCE. RNNoise accepts only 480-sample
// frames at 48 kHz, so the whole pipeline adopts its granularity; varying
// any of these constants means buffering around the denoiser for no gain.
// 48 kHz is also Opus's native rate, and 10 ms divides both the 20 ms and
// 40 ms frame durations Phase 5 might require -- see the spec's Sink seam.
package audio

import "time"

const (
	SampleRate    = 48000
	Channels      = 1
	FrameSamples  = 480
	FrameDuration = 10 * time.Millisecond
)
```

- [ ] **Step 2: Write the failing ring test**

```go
package audio

import (
	"sync"
	"testing"
)

func TestRingWriteReadRoundTrip(t *testing.T) {
	r := NewRing(4) // 4 frames of capacity
	in := make([]float32, FrameSamples)
	for i := range in {
		in[i] = float32(i)
	}
	if dropped := r.Write(in); dropped != 0 {
		t.Fatalf("Write dropped %d samples into an empty ring", dropped)
	}
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read returned %d samples, want %d", n, FrameSamples)
	}
	for i := range out {
		if out[i] != in[i] {
			t.Fatalf("sample %d = %v, want %v", i, out[i], in[i])
		}
	}
}

// TestRingOverflowDropsNewestAndCounts covers overflow under the strict
// index-ownership design: a Write that doesn't fit is refused whole (not
// partially copied), the dropped counter advances by the refused amount,
// and -- the assertion that would have caught the old lost-update/torn-
// buffer bug -- the frames already buffered before the overflowing Write
// come back out fully intact.
func TestRingOverflowDropsNewestAndCounts(t *testing.T) {
	r := NewRing(2)
	frame0 := make([]float32, FrameSamples)
	frame1 := make([]float32, FrameSamples)
	for i := range frame0 {
		frame0[i] = 1
		frame1[i] = 2
	}
	if dropped := r.Write(frame0); dropped != 0 {
		t.Fatalf("Write(frame0) dropped %d, want 0", dropped)
	}
	if dropped := r.Write(frame1); dropped != 0 {
		t.Fatalf("Write(frame1) dropped %d, want 0", dropped)
	}

	overflow := make([]float32, FrameSamples)
	for i := range overflow {
		overflow[i] = 3
	}
	dropped := r.Write(overflow)
	if dropped == 0 {
		t.Fatal("Write did not report any dropped samples when the ring was full")
	}
	if got := r.Dropped(); got != uint64(dropped) {
		t.Fatalf("Dropped() = %d, want %d", got, dropped)
	}

	// The two frames buffered before the overflow must still be there,
	// untouched and intact -- not overwritten, not torn.
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read #1 returned %d, want %d", n, FrameSamples)
	}
	for i, v := range out {
		if v != 1 {
			t.Fatalf("frame0 sample %d = %v, want 1 (buffered frame was corrupted)", i, v)
		}
	}
	if n := r.Read(out); n != FrameSamples {
		t.Fatalf("Read #2 returned %d, want %d", n, FrameSamples)
	}
	for i, v := range out {
		if v != 2 {
			t.Fatalf("frame1 sample %d = %v, want 2 (buffered frame was corrupted)", i, v)
		}
	}
}

func TestRingUnderrunCounts(t *testing.T) {
	r := NewRing(2)
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != 0 {
		t.Fatalf("Read on an empty ring returned %d, want 0", n)
	}
	if r.Underruns() != 1 {
		t.Fatalf("Underruns() = %d, want 1", r.Underruns())
	}
}

// TestRingDrain verifies that Drain resets the backlog: after filling the
// ring, Drain must advance r to w so the buffered-but-unread count is zero
// and the next Read reports empty.
func TestRingDrain(t *testing.T) {
	r := NewRing(2)
	frame := make([]float32, FrameSamples)
	r.Write(frame)
	r.Write(frame)

	r.Drain()

	if backlog := r.w.Load() - r.r.Load(); backlog != 0 {
		t.Fatalf("w-r = %d after Drain, want 0", backlog)
	}
	out := make([]float32, FrameSamples)
	if n := r.Read(out); n != 0 {
		t.Fatalf("Read after Drain returned %d, want 0", n)
	}
}

func TestRingConcurrentProducerConsumer(t *testing.T) {
	r := NewRing(8)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < 1000; i++ {
			r.Write(f)
		}
	}()
	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < 1000; i++ {
			r.Read(f)
		}
	}()
	wg.Wait()
}

// TestRingConcurrentProducerConsumerFramesStayIntact runs a producer and a
// consumer concurrently for enough iterations to force many overflows, and
// asserts every frame the consumer reads is internally self-consistent:
// every sample in the frame written for iteration N equals N (mod a small
// period, so values repeat and stay easy to compare). A torn frame -- part
// written by one Write, a stale leftover from a previous one, or a slot
// being overwritten mid-Read -- is exactly what the old dual-Store,
// producer-forces-r design could produce. This test would have failed
// against that code; it passes against strict index ownership.
func TestRingConcurrentProducerConsumerFramesStayIntact(t *testing.T) {
	r := NewRing(4)
	const iterations = 20000

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		f := make([]float32, FrameSamples)
		for i := 0; i < iterations; i++ {
			v := float32(i % 997)
			for j := range f {
				f[j] = v
			}
			r.Write(f)
		}
	}()

	go func() {
		defer wg.Done()
		out := make([]float32, FrameSamples)
		for i := 0; i < iterations; i++ {
			n := r.Read(out)
			if n == 0 {
				continue
			}
			first := out[0]
			for j := 1; j < n; j++ {
				if out[j] != first {
					t.Errorf("torn frame: sample %d = %v, want %v (same as sample 0)", j, out[j], first)
					return
				}
			}
		}
	}()

	wg.Wait()
}
```

- [ ] **Step 3: Run the test and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestRing -v`
Expected: FAIL — `undefined: NewRing`

- [ ] **Step 4: Implement `ring.go`**

```go
package audio

import "sync/atomic"

// Ring is a single-producer / single-consumer float32 ring buffer.
//
// It exists because the OS audio callbacks must never block, allocate or
// take a contended lock: a mutex here is a click in the user's headset.
//
// Index ownership is strict and one-directional: Write owns w and only ever
// loads r; Read (and Drain) owns r and only ever loads w. Neither side ever
// stores the other's index. This is load-bearing, not stylistic: an earlier
// version had Write force r forward on overflow to implement drop-oldest,
// which meant two goroutines could unconditionally Store the same atomic
// index (a lost update with no CAS or forward-only guard) and, worse, meant
// the producer could go on to write into buffer slots the consumer was
// concurrently mid-copy on -- an unsynchronized access to the same
// []float32 elements. That corrupts audio silently instead of crashing, and
// the overlap window is narrow and timing-dependent enough that -race does
// not reliably catch it. Strict ownership removes the hazard by
// construction: there is no code path on either side that writes the
// other's index, so there is nothing left to race.
//
// The cost of that guarantee is that overflow now drops the incoming
// (newest) chunk instead of evicting the oldest buffered one, and drops it
// whole rather than partially -- a partial write would tear a frame at the
// boundary, and this package's contract is frame-sized chunks in and out.
// Drop-oldest cannot be reintroduced without the producer touching r, so it
// is not an option here regardless of how much more intuitive "keep the
// newest audio" sounds. What preserves the original intent -- stale
// backlog has no value and latency must recover -- is Drain: the consumer
// (the only side allowed to move r) can jump r to the current w whenever it
// observes Dropped() climbing, clearing the backlog from the side that
// legally owns the index instead of the side that doesn't.
//
// Capacity is rounded up to a power of two so the wrap is a mask rather than
// a modulo.
type Ring struct {
	buf  []float32
	mask uint64

	w atomic.Uint64
	r atomic.Uint64

	dropped   atomic.Uint64
	underruns atomic.Uint64
}

// NewRing allocates a ring holding capacityFrames frames of FrameSamples.
func NewRing(capacityFrames int) *Ring {
	n := uint64(capacityFrames * FrameSamples)
	size := uint64(1)
	for size < n {
		size <<= 1
	}
	return &Ring{buf: make([]float32, size), mask: size - 1}
}

// Write copies src into the ring. If there isn't enough free space for all
// of src, it writes nothing, counts the whole of src as dropped, and
// returns that count: a partial write would tear a frame across the
// overflow boundary, so the incoming chunk is dropped whole or not at all.
//
// Write owns w exclusively; it only loads r to compute free space and never
// stores it. See the Ring doc comment for why that split matters.
func (r *Ring) Write(src []float32) (dropped int) {
	w := r.w.Load()
	rd := r.r.Load()
	free := uint64(len(r.buf)) - (w - rd)
	n := uint64(len(src))
	if n > free {
		r.dropped.Add(n)
		return int(n)
	}
	for i, v := range src {
		r.buf[(w+uint64(i))&r.mask] = v
	}
	r.w.Store(w + n)
	return 0
}

// Read fills dst and returns how many samples were copied. A short or empty
// read increments the underrun counter; the caller emits silence.
//
// Read owns r exclusively; it only loads w to compute available samples and
// never stores it. See the Ring doc comment for why that split matters.
func (r *Ring) Read(dst []float32) int {
	w := r.w.Load()
	rd := r.r.Load()
	avail := w - rd
	if avail < uint64(len(dst)) {
		r.underruns.Add(1)
		if avail == 0 {
			return 0
		}
	}
	n := uint64(len(dst))
	if avail < n {
		n = avail
	}
	for i := uint64(0); i < n; i++ {
		dst[i] = r.buf[(rd+i)&r.mask]
	}
	r.r.Store(rd + n)
	return int(n)
}

// Drain discards everything currently buffered by advancing r to the
// current w, resetting queued latency to zero. It is consumer-side only,
// since only the consumer may legally move r: the DSP goroutine should call
// it when it observes Dropped() advancing, so a backlog left behind by
// dropped Writes doesn't linger. See the Ring doc comment for why this,
// rather than producer-side drop-oldest, is how latency recovers.
func (r *Ring) Drain() {
	r.r.Store(r.w.Load())
}

// Dropped is the cumulative count of samples discarded because a Write
// arrived with insufficient free space, reported via audio:state.
func (r *Ring) Dropped() uint64 { return r.dropped.Load() }

// Underruns is the cumulative short-read count, reported via audio:state.
func (r *Ring) Underruns() uint64 { return r.underruns.Load() }
```

- [ ] **Step 5: Run the tests under race**

Run: `go test -tags purego -race ./internal/audio/ -run TestRing -v`
Expected: PASS, all six tests, no race reports.

- [ ] **Step 6: Commit**

```bash
git add internal/audio/format.go internal/audio/ring.go internal/audio/ring_test.go
git commit -m "feat(audio): add the internal format constants and SPSC ring

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Automatic gain control

**Files:**
- Create: `internal/audio/agc.go`
- Test: `internal/audio/agc_test.go`

**Interfaces:**
- Consumes: `FrameSamples` from Task 2
- Produces:
  - `func NewAGC() *AGC`
  - `func (a *AGC) Process(frame []float32)` — in-place, `len(frame) == FrameSamples`
  - `func (a *AGC) Gain() float32` — current smoothed gain, for tests and diagnostics
  - `func rms(frame []float32) float32` — package-level, reused by the gate in Task 4

- [ ] **Step 1: Write the failing tests**

```go
package audio

import (
	"math"
	"testing"
)

// sine fills a frame with a full-cycle-aligned tone at the given amplitude.
func sine(amp float32) []float32 {
	f := make([]float32, FrameSamples)
	for i := range f {
		f[i] = amp * float32(math.Sin(2*math.Pi*float64(i)/float64(FrameSamples)*10))
	}
	return f
}

func TestAGCRaisesQuietSignalTowardTarget(t *testing.T) {
	a := NewAGC()
	var got float32
	// Feed 2 seconds of a quiet tone; AGC is a smoothed follower, not a
	// per-frame normaliser, so it needs time to converge.
	for i := 0; i < 200; i++ {
		f := sine(0.02)
		a.Process(f)
		got = rms(f)
	}
	if got < agcTargetRMS*0.5 {
		t.Fatalf("quiet signal settled at RMS %v, want at least %v", got, agcTargetRMS*0.5)
	}
}

func TestAGCLowersLoudSignalTowardTarget(t *testing.T) {
	a := NewAGC()
	var got float32
	for i := 0; i < 200; i++ {
		f := sine(0.9)
		a.Process(f)
		got = rms(f)
	}
	if got > agcTargetRMS*2 {
		t.Fatalf("loud signal settled at RMS %v, want at most %v", got, agcTargetRMS*2)
	}
}

func TestAGCNeverExceedsFullScale(t *testing.T) {
	a := NewAGC()
	// Drive the gain up on near-silence, then hit it with a full-scale
	// transient -- the classic way a naive AGC produces a clipped bang.
	for i := 0; i < 500; i++ {
		a.Process(sine(0.001))
	}
	f := sine(1.0)
	a.Process(f)
	for i, v := range f {
		if v > 1.0 || v < -1.0 {
			t.Fatalf("sample %d = %v, outside [-1, 1]", i, v)
		}
	}
}

func TestAGCDoesNotAmplifyDigitalSilence(t *testing.T) {
	a := NewAGC()
	for i := 0; i < 500; i++ {
		f := make([]float32, FrameSamples)
		a.Process(f)
		for _, v := range f {
			if v != 0 {
				t.Fatal("AGC produced non-zero output from digital silence")
			}
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestAGC -v`
Expected: FAIL — `undefined: NewAGC`

- [ ] **Step 3: Implement `agc.go`**

```go
package audio

import "math"

const (
	// agcTargetRMS is roughly -20 dBFS, a conventional speech operating
	// level that leaves plenty of headroom for transients.
	agcTargetRMS = 0.1
	// agcMaxGain caps amplification so a muted or unplugged microphone's
	// noise floor cannot be driven up into audible hiss.
	agcMaxGain = 20.0
	agcMinGain = 0.05
	// Attack is faster than release: coming DOWN from too-loud must happen
	// quickly to avoid clipping, while creeping UP slowly is what stops the
	// between-sentences noise swell.
	agcAttack  = 0.25
	agcRelease = 0.02
	// Below this RMS the frame is treated as silence and the gain is frozen
	// rather than chased. Without it the follower winds up to agcMaxGain
	// during every pause.
	agcSilenceFloor = 1e-4
)

// AGC is a smoothed RMS-targeting gain follower with a hard limiter.
//
// It is deliberately a follower rather than a per-frame normaliser: dividing
// each frame by its own RMS destroys the dynamics that make speech
// intelligible and pumps audibly on every consonant.
type AGC struct {
	gain float32
}

func NewAGC() *AGC { return &AGC{gain: 1.0} }

// Gain reports the current smoothed gain.
func (a *AGC) Gain() float32 { return a.gain }

// Process applies gain in place. len(frame) must be FrameSamples.
func (a *AGC) Process(frame []float32) {
	level := rms(frame)
	if level > agcSilenceFloor {
		desired := agcTargetRMS / level
		if desired > agcMaxGain {
			desired = agcMaxGain
		}
		if desired < agcMinGain {
			desired = agcMinGain
		}
		rate := float32(agcRelease)
		if desired < a.gain {
			rate = agcAttack
		}
		a.gain += (desired - a.gain) * rate
	}
	for i, v := range frame {
		s := v * a.gain
		// Hard limiter. The follower is smoothed, so it cannot react within
		// a single frame to a transient -- this is what guarantees the
		// never-exceeds-full-scale property regardless of gain history.
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		frame[i] = s
	}
}

// rms is the root-mean-square level of a frame. Shared with the gate, which
// compares it against the VOX threshold.
func rms(frame []float32) float32 {
	if len(frame) == 0 {
		return 0
	}
	var sum float64
	for _, v := range frame {
		sum += float64(v) * float64(v)
	}
	return float32(math.Sqrt(sum / float64(len(frame))))
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -tags purego -race ./internal/audio/ -run TestAGC -v`
Expected: PASS, all four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/agc.go internal/audio/agc_test.go
git commit -m "feat(audio): add the pure-Go AGC with a hard limiter

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: PTT / VOX / mute gate

**Files:**
- Create: `internal/audio/gate.go`
- Test: `internal/audio/gate_test.go`

**Interfaces:**
- Consumes: `FrameSamples`, `rms` (Task 3)
- Produces:
  - `type GateConfig struct { VOXEnabled bool; VOXThreshold float32; VOXMinLengthMS, VOXHangMS, PTTStartDelayMS, PTTReleaseDelayMS int }`
  - `type GateInput struct { PTT, Muted bool; Level float32 }`
  - `func NewGate(cfg GateConfig) *Gate`
  - `func (g *Gate) SetConfig(cfg GateConfig)`
  - `func (g *Gate) Step(in GateInput) bool` — call exactly once per frame; returns whether the mic is open

- [ ] **Step 1: Write the failing tests**

```go
package audio

import "testing"

func testGateConfig() GateConfig {
	return GateConfig{
		VOXEnabled:        false,
		VOXThreshold:      0.05,
		VOXMinLengthMS:    50,  // 5 frames
		VOXHangMS:         100, // 10 frames
		PTTStartDelayMS:   0,
		PTTReleaseDelayMS: 0,
	}
}

func TestGatePTTOpensImmediatelyWithNoDelay(t *testing.T) {
	g := NewGate(testGateConfig())
	if g.Step(GateInput{PTT: true}) != true {
		t.Fatal("gate closed on the first PTT frame with zero start delay")
	}
}

func TestGatePTTStartDelayHoldsGateClosed(t *testing.T) {
	cfg := testGateConfig()
	cfg.PTTStartDelayMS = 30 // 3 frames
	g := NewGate(cfg)
	for i := 0; i < 3; i++ {
		if g.Step(GateInput{PTT: true}) {
			t.Fatalf("gate opened at frame %d, want it held until frame 3", i)
		}
	}
	if !g.Step(GateInput{PTT: true}) {
		t.Fatal("gate still closed after the start delay elapsed")
	}
}

func TestGatePTTReleaseDelayKeepsGateOpen(t *testing.T) {
	cfg := testGateConfig()
	cfg.PTTReleaseDelayMS = 30 // 3 frames
	g := NewGate(cfg)
	g.Step(GateInput{PTT: true})
	for i := 0; i < 3; i++ {
		if !g.Step(GateInput{PTT: false}) {
			t.Fatalf("gate closed at tail frame %d, clipping the word ending", i)
		}
	}
	if g.Step(GateInput{PTT: false}) {
		t.Fatal("gate still open after the release tail elapsed")
	}
}

func TestGateVOXRequiresMinLengthBeforeOpening(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	// 5 frames of sustain required; frames 0-3 must stay shut.
	for i := 0; i < 4; i++ {
		if g.Step(GateInput{Level: 0.2}) {
			t.Fatalf("VOX opened at frame %d, before min length", i)
		}
	}
	if !g.Step(GateInput{Level: 0.2}) {
		t.Fatal("VOX did not open once min length was satisfied")
	}
}

func TestGateVOXHangKeepsGateOpenThroughAPause(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	for i := 0; i < 5; i++ {
		g.Step(GateInput{Level: 0.2})
	}
	for i := 0; i < 10; i++ {
		if !g.Step(GateInput{Level: 0.0}) {
			t.Fatalf("VOX closed at hang frame %d", i)
		}
	}
	if g.Step(GateInput{Level: 0.0}) {
		t.Fatal("VOX still open after the hang elapsed")
	}
}

func TestGateMuteOverridesPTTAndVOX(t *testing.T) {
	cfg := testGateConfig()
	cfg.VOXEnabled = true
	g := NewGate(cfg)
	if g.Step(GateInput{PTT: true, Level: 0.9, Muted: true}) {
		t.Fatal("gate opened while muted -- mute must win unconditionally")
	}
}

func TestGateVOXDisabledIgnoresLevel(t *testing.T) {
	g := NewGate(testGateConfig()) // VOXEnabled false
	for i := 0; i < 50; i++ {
		if g.Step(GateInput{Level: 0.9}) {
			t.Fatal("gate opened on level alone with VOX disabled")
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestGate -v`
Expected: FAIL — `undefined: NewGate`

- [ ] **Step 3: Implement `gate.go`**

```go
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
		// suppressed and the fourth opens.
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
			if g.voxSustain > g.voxMinLen {
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
```

Note on the min-length test: `VOXMinLengthMS: 50` is 5 frames, and `voxSustain > 5` would open on the sixth. The test expects the fifth. Use `>=` for VOX min length if the test disagrees — run it and let the test decide, then make the comparison and its comment consistent.

- [ ] **Step 4: Run and watch it pass**

Run: `go test -tags purego -race ./internal/audio/ -run TestGate -v`
Expected: PASS, all seven tests. If `TestGateVOXRequiresMinLengthBeforeOpening` fails by one frame, fix the comparison as noted above — do **not** adjust the test to match the implementation.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/gate.go internal/audio/gate_test.go
git commit -m "feat(audio): add the frame-counted PTT/VOX/mute gate

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Voice-effect and clipping DSP presets

Spec D12: these are **built-in named configurations in code**, not parsed files. The design's `comms_filter_mid.preset` and `saturated_overdrive.preset` filenames become identifiers.

**Files:**
- Create: `internal/audio/effect.go`
- Test: `internal/audio/effect_test.go`
- Create: `internal/audio/testdata/golden_comms_filter_mid.json`

**Interfaces:**
- Consumes: `FrameSamples`, `rms`
- Produces:
  - `func VoicePresets() []string` / `func ClippingPresets() []string` — ids for the UI selects
  - `func NewEffect(voiceID, clippingID string) *Effect`
  - `func (e *Effect) Process(frame []float32)` — in place
  - `func (e *Effect) IDs() (voice, clipping string)`

- [ ] **Step 1: Write the failing tests**

```go
package audio

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestVoicePresetsIncludeTheDesignsNames(t *testing.T) {
	got := VoicePresets()
	want := map[string]bool{"": true, "comms_filter_low": true, "comms_filter_mid": true, "comms_filter_high": true}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("unexpected voice preset %q", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing voice presets: %v", want)
	}
}

func TestEmptyPresetIsPassthrough(t *testing.T) {
	e := NewEffect("", "")
	in := sine(0.5)
	got := append([]float32(nil), in...)
	e.Process(got)
	for i := range got {
		if got[i] != in[i] {
			t.Fatalf("sample %d changed with no preset selected", i)
		}
	}
}

func TestBandpassAttenuatesOutOfBandEnergy(t *testing.T) {
	e := NewEffect("comms_filter_mid", "")
	// 60 Hz is well below a comms bandpass and must be strongly attenuated.
	low := make([]float32, FrameSamples)
	for i := range low {
		low[i] = 0.5 * float32(math.Sin(2*math.Pi*60*float64(i)/SampleRate))
	}
	before := rms(low)
	// Run several frames so the filter state settles.
	for i := 0; i < 20; i++ {
		e.Process(low)
	}
	if after := rms(low); after >= before*0.5 {
		t.Fatalf("60 Hz RMS %v not attenuated below half of %v", after, before)
	}
}

func TestUnknownPresetIDFallsBackToPassthrough(t *testing.T) {
	e := NewEffect("no_such_preset", "no_such_clip")
	v, c := e.IDs()
	if v != "" || c != "" {
		t.Fatalf("IDs() = (%q, %q), want empty after an unknown id", v, c)
	}
}

// TestCommsFilterMidMatchesGolden pins the preset's response so a tuning
// change shows up as a reviewable diff rather than a silent alteration to
// how every transmission sounds.
func TestCommsFilterMidMatchesGolden(t *testing.T) {
	e := NewEffect("comms_filter_mid", "")
	frame := sine(0.5)
	e.Process(frame)

	const path = "testdata/golden_comms_filter_mid.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		b, _ := json.Marshal(frame)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden updated")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (regenerate with UPDATE_GOLDEN=1)", err)
	}
	var want []float32
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(frame) {
		t.Fatalf("golden has %d samples, frame has %d", len(want), len(frame))
	}
	for i := range frame {
		if math.Abs(float64(frame[i]-want[i])) > 1e-6 {
			t.Fatalf("sample %d = %v, golden %v", i, frame[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run 'TestVoicePresets|TestEmptyPreset|TestBandpass|TestUnknownPreset|TestCommsFilter' -v`
Expected: FAIL — `undefined: VoicePresets`

- [ ] **Step 3: Implement `effect.go`**

```go
package audio

import "math"

// Effect applies radio colouration: a bandpass that makes voice sound like
// it came through a radio, plus optional soft clipping.
//
// Presets are BUILT-IN NAMED CONFIGURATIONS, not files. The design lists
// them with .preset extensions, but there is no preset file format and no
// parser -- the filenames are identifiers. See spec D12.
type Effect struct {
	voiceID    string
	clippingID string

	hp, lp   biquad
	drive    float32
	hasBand  bool
	hasClip  bool
}

type bandSpec struct{ lowHz, highHz float64 }

var voiceBands = map[string]bandSpec{
	"comms_filter_low":  {200, 2400},
	"comms_filter_mid":  {300, 3000},
	"comms_filter_high": {500, 3800},
}

var clippingDrives = map[string]float32{
	"saturated_overdrive": 4.0,
	"soft_limit":          1.8,
}

// VoicePresets returns the selectable voice-effect ids, "" meaning off.
func VoicePresets() []string {
	return []string{"", "comms_filter_low", "comms_filter_mid", "comms_filter_high"}
}

// ClippingPresets returns the selectable clipping ids, "" meaning off.
func ClippingPresets() []string {
	return []string{"", "soft_limit", "saturated_overdrive"}
}

// NewEffect builds the chain. An unrecognised id degrades to passthrough
// rather than erroring: a config written by a newer version must not stop
// audio from working.
func NewEffect(voiceID, clippingID string) *Effect {
	e := &Effect{}
	if b, ok := voiceBands[voiceID]; ok {
		e.voiceID = voiceID
		e.hasBand = true
		e.hp = highpass(b.lowHz, SampleRate)
		e.lp = lowpass(b.highHz, SampleRate)
	}
	if d, ok := clippingDrives[clippingID]; ok {
		e.clippingID = clippingID
		e.hasClip = true
		e.drive = d
	}
	return e
}

// IDs reports the presets actually in effect (empty for passthrough).
func (e *Effect) IDs() (string, string) { return e.voiceID, e.clippingID }

// Process applies the chain in place.
func (e *Effect) Process(frame []float32) {
	if e.hasBand {
		for i, v := range frame {
			frame[i] = e.lp.step(e.hp.step(v))
		}
	}
	if e.hasClip {
		for i, v := range frame {
			// tanh soft clip: saturates smoothly instead of the square-edged
			// distortion a hard clamp produces.
			frame[i] = float32(math.Tanh(float64(v * e.drive)))
		}
	}
}

// biquad is a direct-form-I second-order section.
type biquad struct {
	b0, b1, b2, a1, a2 float32
	x1, x2, y1, y2     float32
}

func (f *biquad) step(x float32) float32 {
	y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
	f.x2, f.x1 = f.x1, x
	f.y2, f.y1 = f.y1, y
	return y
}

// Butterworth Q for a single second-order section.
const butterworthQ = 0.7071067811865476

func lowpass(cutoffHz, sampleRate float64) biquad {
	w0 := 2 * math.Pi * cutoffHz / sampleRate
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * butterworthQ)
	a0 := 1 + alpha
	return biquad{
		b0: float32((1 - cw) / 2 / a0),
		b1: float32((1 - cw) / a0),
		b2: float32((1 - cw) / 2 / a0),
		a1: float32(-2 * cw / a0),
		a2: float32((1 - alpha) / a0),
	}
}

func highpass(cutoffHz, sampleRate float64) biquad {
	w0 := 2 * math.Pi * cutoffHz / sampleRate
	cw, sw := math.Cos(w0), math.Sin(w0)
	alpha := sw / (2 * butterworthQ)
	a0 := 1 + alpha
	return biquad{
		b0: float32((1 + cw) / 2 / a0),
		b1: float32(-(1 + cw) / a0),
		b2: float32((1 + cw) / 2 / a0),
		a1: float32(-2 * cw / a0),
		a2: float32((1 - alpha) / a0),
	}
}
```

- [ ] **Step 4: Generate the golden file, then verify against it**

Run: `mkdir -p internal/audio/testdata && UPDATE_GOLDEN=1 go test -tags purego ./internal/audio/ -run TestCommsFilterMid`
Then: `go test -tags purego -race ./internal/audio/ -run 'TestVoicePresets|TestEmptyPreset|TestBandpass|TestUnknownPreset|TestCommsFilter' -v`
Expected: PASS, all five.

Inspect the golden file before committing it — confirm it is a JSON array of 480 numbers, not an empty array. An all-zero golden would pass forever while testing nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/effect.go internal/audio/effect_test.go internal/audio/testdata/
git commit -m "feat(audio): add built-in voice and clipping DSP presets

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Minimal RIFF/PCM WAV decoder

Spec D13: hand-rolled rather than a dependency.

**Files:**
- Create: `internal/audio/wav.go`
- Test: `internal/audio/wav_test.go`

**Interfaces:**
- Consumes: `SampleRate`, `Channels`
- Produces:
  - `func DecodeWAV(b []byte) (samples []float32, err error)` — always returns 48 kHz mono float32, resampling and downmixing as needed

- [ ] **Step 1: Write the failing tests**

```go
package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildWAV assembles a minimal 16-bit PCM RIFF file.
func buildWAV(sampleRate int, channels int, samples []int16) []byte {
	var b bytes.Buffer
	dataLen := len(samples) * 2
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+dataLen))
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1)) // PCM
	binary.Write(&b, binary.LittleEndian, uint16(channels))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&b, binary.LittleEndian, uint32(sampleRate*channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(dataLen))
	for _, s := range samples {
		binary.Write(&b, binary.LittleEndian, s)
	}
	return b.Bytes()
}

func TestDecodeWAVMono48k(t *testing.T) {
	in := []int16{0, 16384, -16384, 32767}
	got, err := DecodeWAV(buildWAV(48000, 1, in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(in) {
		t.Fatalf("got %d samples, want %d", len(got), len(in))
	}
	if got[1] < 0.49 || got[1] > 0.51 {
		t.Fatalf("16384 decoded to %v, want ~0.5", got[1])
	}
}

func TestDecodeWAVDownmixesStereo(t *testing.T) {
	// Two frames: L/R pairs that average to 0 and to 0.5.
	in := []int16{16384, -16384, 16384, 16384}
	got, err := DecodeWAV(buildWAV(48000, 2, in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d samples, want 2 after downmix", len(got))
	}
	if got[0] < -0.01 || got[0] > 0.01 {
		t.Fatalf("opposed channels averaged to %v, want ~0", got[0])
	}
}

func TestDecodeWAVResamplesOffSpecRate(t *testing.T) {
	in := make([]int16, 24000) // 1 second at 24 kHz
	got, err := DecodeWAV(buildWAV(24000, 1, in))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(got); got < 47000 || got > 49000 {
		t.Fatalf("24 kHz second resampled to %d samples, want ~48000", got)
	}
}

func TestDecodeWAVRejectsTruncatedHeader(t *testing.T) {
	if _, err := DecodeWAV([]byte("RIFF")); err == nil {
		t.Fatal("truncated header decoded without error")
	}
}

func TestDecodeWAVRejectsNonRIFF(t *testing.T) {
	if _, err := DecodeWAV(bytes.Repeat([]byte{0}, 64)); err == nil {
		t.Fatal("non-RIFF bytes decoded without error")
	}
}

func TestDecodeWAVRejectsNonPCM(t *testing.T) {
	w := buildWAV(48000, 1, []int16{0, 1})
	w[20] = 3 // IEEE float format tag
	if _, err := DecodeWAV(w); err == nil {
		t.Fatal("non-PCM format decoded without error")
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestDecodeWAV -v`
Expected: FAIL — `undefined: DecodeWAV`

- [ ] **Step 3: Implement `wav.go`**

```go
package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// DecodeWAV decodes a 16-bit PCM RIFF file into the pipeline's internal
// format: 48 kHz, mono, float32.
//
// The asset contract (spec §12) is 48 kHz mono 16-bit, but off-spec files are
// downmixed and linearly resampled rather than rejected: for short chirps the
// quality cost is inaudible, and a mis-specified asset that plays slightly
// imperfectly is far easier to diagnose than one that silently does nothing.
func DecodeWAV(b []byte) ([]float32, error) {
	if len(b) < 44 {
		return nil, errors.New("wav: too short to contain a header")
	}
	if string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("wav: not a RIFF/WAVE file")
	}

	var (
		format     uint16
		channels   int
		sampleRate int
		bits       uint16
		data       []byte
		haveFmt    bool
	)

	// Walk the chunk list rather than assuming fmt is immediately followed by
	// data -- real files carry LIST/INFO chunks between them.
	for off := 12; off+8 <= len(b); {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(b) {
			return nil, fmt.Errorf("wav: chunk %q overruns the file", id)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("wav: fmt chunk too small")
			}
			format = binary.LittleEndian.Uint16(b[body : body+2])
			channels = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
			sampleRate = int(binary.LittleEndian.Uint32(b[body+4 : body+8]))
			bits = binary.LittleEndian.Uint16(b[body+14 : body+16])
			haveFmt = true
		case "data":
			data = b[body : body+size]
		}
		off = body + size
		if size%2 == 1 {
			off++ // RIFF chunks are word-aligned
		}
	}

	if !haveFmt {
		return nil, errors.New("wav: no fmt chunk")
	}
	if format != 1 {
		return nil, fmt.Errorf("wav: format %d is not 16-bit PCM", format)
	}
	if bits != 16 {
		return nil, fmt.Errorf("wav: %d-bit samples, want 16", bits)
	}
	if channels < 1 {
		return nil, errors.New("wav: zero channels")
	}
	if data == nil {
		return nil, errors.New("wav: no data chunk")
	}

	// Decode and downmix to mono.
	frames := len(data) / 2 / channels
	mono := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			raw := int16(binary.LittleEndian.Uint16(data[(i*channels+c)*2:]))
			sum += float32(raw) / 32768.0
		}
		mono[i] = sum / float32(channels)
	}

	if sampleRate == SampleRate || sampleRate <= 0 {
		return mono, nil
	}
	return resampleLinear(mono, sampleRate, SampleRate), nil
}

// resampleLinear is adequate for short SFX. Voice never passes through here.
func resampleLinear(in []float32, from, to int) []float32 {
	if len(in) == 0 {
		return in
	}
	ratio := float64(to) / float64(from)
	outLen := int(float64(len(in)) * ratio)
	out := make([]float32, outLen)
	for i := range out {
		src := float64(i) / ratio
		i0 := int(src)
		if i0 >= len(in)-1 {
			out[i] = in[len(in)-1]
			continue
		}
		frac := float32(src - float64(i0))
		out[i] = in[i0]*(1-frac) + in[i0+1]*frac
	}
	return out
}
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -tags purego -race ./internal/audio/ -run TestDecodeWAV -v`
Expected: PASS, all six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/audio/wav.go internal/audio/wav_test.go
git commit -m "feat(audio): add a minimal RIFF/PCM WAV decoder

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Vendor RNNoise and wrap it in cgo

**Prerequisite: Task 1 must be green on all three runners.** Spec D14 exists so that a failure here is unambiguously about RNNoise rather than about CI scoping.

**Files:**
- Create: `internal/audio/rnnoise/` (vendored C sources + `LICENSE` + `VENDOR.md`)
- Create: `internal/audio/rnnoise.go` (cgo wrapper)
- Create: `internal/audio/rnnoise_stub.go` (`//go:build !cgo`)
- Test: `internal/audio/rnnoise_test.go`

**Interfaces:**
- Consumes: `FrameSamples`
- Produces:
  - `func NewDenoiser() (*Denoiser, error)` — returns a working denoiser, or one whose `Process` is a no-op when cgo is unavailable
  - `func (d *Denoiser) Process(frame []float32)` — in place, `len(frame) == FrameSamples`
  - `func (d *Denoiser) Available() bool`
  - `func (d *Denoiser) Close()`

- [ ] **Step 1: Vendor the sources**

Clone RNNoise (`https://gitlab.xiph.org/xiph/rnnoise`, MIT/BSD-3) into a scratch directory, then copy **only** the library sources and headers into `internal/audio/rnnoise/` — `src/*.c`, `src/*.h`, `include/rnnoise.h`, and the pre-trained model data file. Do **not** copy the autotools scaffolding, the demo program, or the training scripts.

Write `internal/audio/rnnoise/VENDOR.md` recording: upstream URL, the exact commit hash vendored, the date, the license, and this rationale —

> Vendored rather than depended upon, following the `internal/joystick/di8` precedent: it removes a supply-chain risk on the critical audio path, needs no system library or pkg-config on any of the three platforms, and RNNoise's C API is stable. The pre-trained model is part of the library, not a separate asset.

Copy the upstream `COPYING`/`LICENSE` verbatim to `internal/audio/rnnoise/LICENSE`.

- [ ] **Step 2: Write the failing test**

```go
package audio

import "testing"

func TestDenoiserProcessesAFrameWithoutPanicking(t *testing.T) {
	d, err := NewDenoiser()
	if err != nil {
		t.Fatalf("NewDenoiser: %v", err)
	}
	defer d.Close()

	frame := sine(0.3)
	before := rms(frame)
	d.Process(frame)
	if len(frame) != FrameSamples {
		t.Fatalf("Process changed the frame length to %d", len(frame))
	}
	if !d.Available() {
		t.Skip("cgo unavailable; Process is a documented no-op")
	}
	// A clean tone must survive denoising with most of its energy. This is a
	// sanity check that the model ran, not a quality assertion.
	if after := rms(frame); after < before*0.1 {
		t.Fatalf("denoiser destroyed a clean tone: RMS %v -> %v", before, after)
	}
}

func TestDenoiserSuppressesWhiteNoiseMoreThanTone(t *testing.T) {
	d, err := NewDenoiser()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.Available() {
		t.Skip("cgo unavailable")
	}
	// RNNoise needs a few frames of context before it settles.
	var noiseAfter, toneAfter float32
	for i := 0; i < 50; i++ {
		n := whiteNoise(0.3, int64(i))
		d.Process(n)
		noiseAfter = rms(n)
	}
	d2, _ := NewDenoiser()
	defer d2.Close()
	for i := 0; i < 50; i++ {
		s := sine(0.3)
		d2.Process(s)
		toneAfter = rms(s)
	}
	if noiseAfter >= toneAfter {
		t.Fatalf("noise RMS %v not suppressed below tone RMS %v", noiseAfter, toneAfter)
	}
}
```

Add the `whiteNoise` helper next to `sine` in `agc_test.go`:

```go
// whiteNoise fills a frame with deterministic pseudo-random noise.
func whiteNoise(amp float32, seed int64) []float32 {
	r := rand.New(rand.NewSource(seed))
	f := make([]float32, FrameSamples)
	for i := range f {
		f[i] = amp * float32(r.Float64()*2-1)
	}
	return f
}
```

- [ ] **Step 3: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestDenoiser -v`
Expected: FAIL — `undefined: NewDenoiser`

- [ ] **Step 4: Write the cgo wrapper**

```go
//go:build cgo

package audio

/*
#cgo CFLAGS: -I${SRCDIR}/rnnoise -I${SRCDIR}/rnnoise/include -O2 -DCOMPILE_OPUS
#include <stdlib.h>
#include "rnnoise.h"
*/
import "C"

import (
	"errors"
	"unsafe"
)

// Denoiser wraps a vendored RNNoise state.
//
// RNNoise accepts ONLY 480-sample frames at 48 kHz, which is why the whole
// pipeline uses that granularity (see format.go). It also works in int16
// range expressed as float, not in [-1, 1], so the wrapper scales on the way
// in and back on the way out.
type Denoiser struct {
	st  *C.DenoiseState
	buf []C.float
}

func NewDenoiser() (*Denoiser, error) {
	st := C.rnnoise_create(nil)
	if st == nil {
		return nil, errors.New("rnnoise: create failed")
	}
	return &Denoiser{st: st, buf: make([]C.float, FrameSamples)}, nil
}

// Available reports whether denoising actually runs.
func (d *Denoiser) Available() bool { return d != nil && d.st != nil }

// Process denoises the frame in place.
func (d *Denoiser) Process(frame []float32) {
	if d == nil || d.st == nil || len(frame) != FrameSamples {
		return
	}
	for i, v := range frame {
		d.buf[i] = C.float(v * 32768.0)
	}
	C.rnnoise_process_frame(d.st, (*C.float)(unsafe.Pointer(&d.buf[0])), (*C.float)(unsafe.Pointer(&d.buf[0])))
	for i := range frame {
		frame[i] = float32(d.buf[i]) / 32768.0
	}
}

func (d *Denoiser) Close() {
	if d != nil && d.st != nil {
		C.rnnoise_destroy(d.st)
		d.st = nil
	}
}
```

And the no-cgo stub, so the package still builds anywhere cgo is off:

```go
//go:build !cgo

package audio

// Denoiser is a no-op when the build has no cgo. Noise suppression is a
// quality feature, not a correctness one: the client must still capture and
// transmit audio without it.
type Denoiser struct{}

func NewDenoiser() (*Denoiser, error) { return &Denoiser{}, nil }
func (d *Denoiser) Available() bool   { return false }
func (d *Denoiser) Process([]float32) {}
func (d *Denoiser) Close()            {}
```

- [ ] **Step 5: Build and test on this machine**

Run: `go build -tags purego ./internal/audio/ && go test -tags purego -race ./internal/audio/ -run TestDenoiser -v`
Expected: PASS both tests.

If the C sources fail to compile, fix the `#cgo CFLAGS` include paths and the set of vendored `.c` files — do not start deleting sources until it links. Record whatever adjustment was needed in `VENDOR.md`.

- [ ] **Step 6: Commit and confirm all three CI platforms build it**

```bash
git add internal/audio/rnnoise/ internal/audio/rnnoise.go internal/audio/rnnoise_stub.go internal/audio/rnnoise_test.go internal/audio/agc_test.go
git commit -m "feat(audio): vendor RNNoise and wrap it for the DSP chain

Vendored rather than depended upon, following the internal/joystick/di8
precedent: no system library, no pkg-config, and no supply-chain risk on
the critical audio path.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push
gh run watch "$(gh run list --branch feat/phase-4-audio-io --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: ubuntu, macOS and Windows all green. **This is the moment D14 was protecting** — a red job here is a real RNNoise portability problem and must be fixed before proceeding, not deferred.

---

## Task 8: Backend interface, fake backend, malgo backend

**Files:**
- Create: `internal/audio/backend.go`
- Create: `internal/audio/backend_fake.go`
- Create: `internal/audio/backend_malgo.go`
- Test: `internal/audio/backend_fake_test.go`
- Modify: `go.mod` (adds `github.com/gen2brain/malgo`)

**Interfaces:**
- Consumes: `SampleRate`, `Channels`, `FrameSamples`
- Produces:
  - `type DeviceInfo struct { ID, Name string; IsDefault bool }`
  - `type Stream interface { Stop() error }`
  - `type Backend interface { Enumerate() (inputs, outputs []DeviceInfo, err error); OpenCapture(id string, onFrame func([]float32)) (Stream, error); OpenPlayback(id string, fill func([]float32)) (Stream, error); Close() }`
  - `func NewFakeBackend() *FakeBackend` with `SetDevices`, `PushFrame`, `Captured() [][]float32`, `FailNextOpen(error)`
  - `func NewMalgoBackend() (Backend, error)`

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/gen2brain/malgo@latest && go mod tidy`
Then record the resolved version: `grep malgo go.mod`

- [ ] **Step 2: Write `backend.go`**

```go
package audio

// DeviceInfo identifies one audio endpoint.
//
// ID is the backend's stable identifier and is what gets persisted. An EMPTY
// ID means "follow the system default" and is a first-class value, not an
// absent one: a user who chose System Default wants to track the OS, not to
// be silently pinned to whatever happened to be default that day.
type DeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

// Stream is an open device. Stop is idempotent.
type Stream interface {
	Stop() error
}

// Backend is the seam between the pipeline and the OS.
//
// It exists so the entire manager -- lifecycle, hot-plug, device-loss
// fallback, backoff -- is testable with no hardware, on a CI runner that has
// no sound card and a development machine that only covers one of the three
// target platforms.
type Backend interface {
	Enumerate() (inputs, outputs []DeviceInfo, err error)
	// OpenCapture delivers exactly FrameSamples per call to onFrame. The
	// callback runs on an OS audio thread: it must not block, allocate or
	// take a contended lock.
	OpenCapture(id string, onFrame func([]float32)) (Stream, error)
	// OpenPlayback asks fill to populate exactly FrameSamples. Same thread
	// discipline as OpenCapture.
	OpenPlayback(id string, fill func([]float32)) (Stream, error)
	Close()
}
```

- [ ] **Step 3: Write the failing fake-backend test**

```go
package audio

import (
	"errors"
	"testing"
)

func TestFakeBackendRoundTripsFrames(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Test Mic", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Test Out", IsDefault: true}},
	)

	var got [][]float32
	st, err := b.OpenCapture("mic-1", func(f []float32) {
		got = append(got, append([]float32(nil), f...))
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stop()

	b.PushFrame(sine(0.5))
	if len(got) != 1 {
		t.Fatalf("capture callback fired %d times, want 1", len(got))
	}
	if len(got[0]) != FrameSamples {
		t.Fatalf("callback got %d samples, want %d", len(got[0]), FrameSamples)
	}
}

func TestFakeBackendRecordsPlayback(t *testing.T) {
	b := NewFakeBackend()
	st, err := b.OpenPlayback("", func(f []float32) {
		for i := range f {
			f[i] = 0.25
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Stop()

	b.PullFrame()
	rec := b.Captured()
	if len(rec) != 1 || rec[0][0] != 0.25 {
		t.Fatalf("playback recording = %v, want one frame of 0.25", rec)
	}
}

func TestFakeBackendCanFailAnOpen(t *testing.T) {
	b := NewFakeBackend()
	want := errors.New("device busy")
	b.FailNextOpen(want)
	if _, err := b.OpenCapture("mic-1", func([]float32) {}); !errors.Is(err, want) {
		t.Fatalf("OpenCapture err = %v, want %v", err, want)
	}
	// The failure is one-shot: the next open succeeds, which is what lets
	// the manager's retry path be tested.
	if _, err := b.OpenCapture("mic-1", func([]float32) {}); err != nil {
		t.Fatalf("second OpenCapture failed: %v", err)
	}
}
```

- [ ] **Step 4: Run and watch it fail**

Run: `go test -tags purego ./internal/audio/ -run TestFakeBackend -v`
Expected: FAIL — `undefined: NewFakeBackend`

- [ ] **Step 5: Implement `backend_fake.go`**

```go
package audio

import "sync"

// FakeBackend is an in-memory Backend for tests. It is compiled into the
// package (not a _test.go file) so other packages' tests can build a manager
// without hardware.
type FakeBackend struct {
	mu sync.Mutex

	inputs  []DeviceInfo
	outputs []DeviceInfo
	enumErr error

	onFrame  func([]float32)
	fill     func([]float32)
	recorded [][]float32
	failNext error
	closed   bool
}

func NewFakeBackend() *FakeBackend { return &FakeBackend{} }

// SetDevices replaces the enumeration result, simulating hot-plug.
func (b *FakeBackend) SetDevices(inputs, outputs []DeviceInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.inputs, b.outputs = inputs, outputs
}

// FailEnumerate makes the next and subsequent Enumerate calls fail.
func (b *FakeBackend) FailEnumerate(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enumErr = err
}

// FailNextOpen makes exactly the next open call fail, so retry paths can be
// exercised without wedging the backend permanently.
func (b *FakeBackend) FailNextOpen(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failNext = err
}

func (b *FakeBackend) Enumerate() ([]DeviceInfo, []DeviceInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.enumErr != nil {
		return nil, nil, b.enumErr
	}
	return append([]DeviceInfo(nil), b.inputs...), append([]DeviceInfo(nil), b.outputs...), nil
}

func (b *FakeBackend) OpenCapture(id string, onFrame func([]float32)) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	b.onFrame = onFrame
	return &fakeStream{b: b, capture: true}, nil
}

func (b *FakeBackend) OpenPlayback(id string, fill func([]float32)) (Stream, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.takeFailure(); err != nil {
		return nil, err
	}
	b.fill = fill
	return &fakeStream{b: b}, nil
}

// takeFailure consumes the one-shot open failure. Caller holds b.mu.
func (b *FakeBackend) takeFailure() error {
	if b.failNext != nil {
		err := b.failNext
		b.failNext = nil
		return err
	}
	return nil
}

func (b *FakeBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}

// PushFrame delivers one frame to the capture callback, as the OS would.
func (b *FakeBackend) PushFrame(frame []float32) {
	b.mu.Lock()
	cb := b.onFrame
	b.mu.Unlock()
	if cb != nil {
		cb(frame)
	}
}

// PullFrame asks the playback callback for one frame and records it.
func (b *FakeBackend) PullFrame() {
	b.mu.Lock()
	cb := b.fill
	b.mu.Unlock()
	if cb == nil {
		return
	}
	f := make([]float32, FrameSamples)
	cb(f)
	b.mu.Lock()
	b.recorded = append(b.recorded, f)
	b.mu.Unlock()
}

// Captured returns every frame the playback side produced.
func (b *FakeBackend) Captured() [][]float32 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([][]float32(nil), b.recorded...)
}

type fakeStream struct {
	b       *FakeBackend
	capture bool
	stopped bool
}

func (s *fakeStream) Stop() error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if s.stopped {
		return nil
	}
	s.stopped = true
	if s.capture {
		s.b.onFrame = nil
	} else {
		s.b.fill = nil
	}
	return nil
}
```

- [ ] **Step 6: Run and watch it pass**

Run: `go test -tags purego -race ./internal/audio/ -run TestFakeBackend -v`
Expected: PASS, all three.

- [ ] **Step 7: Implement `backend_malgo.go`**

```go
package audio

import (
	"fmt"

	"github.com/gen2brain/malgo"
)

// malgoBackend is the real device layer.
//
// SHARED MODE, NEVER EXCLUSIVE (spec D6). The user is running Star Citizen
// at the same time; taking exclusive hold of the microphone or output would
// break the game's audio exactly the way DISCL_EXCLUSIVE would have broken
// its force feedback in Phase 3.5.
//
// Format conversion is miniaudio's job, not ours: the DeviceConfig requests
// f32 / 1 channel / 48000 and miniaudio's built-in data converter resamples
// from whatever the hardware natively offers.
type malgoBackend struct {
	ctx *malgo.AllocatedContext
}

func NewMalgoBackend() (Backend, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}
	return &malgoBackend{ctx: ctx}, nil
}

func (b *malgoBackend) Enumerate() ([]DeviceInfo, []DeviceInfo, error) {
	capture, err := b.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, nil, fmt.Errorf("audio: enumerate capture: %w", err)
	}
	playback, err := b.ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, nil, fmt.Errorf("audio: enumerate playback: %w", err)
	}
	return toDeviceInfo(capture), toDeviceInfo(playback), nil
}

func toDeviceInfo(in []malgo.DeviceInfo) []DeviceInfo {
	out := make([]DeviceInfo, 0, len(in))
	for _, d := range in {
		out = append(out, DeviceInfo{
			ID:        d.ID.String(),
			Name:      d.Name(),
			IsDefault: d.IsDefault != 0,
		})
	}
	return out
}

func (b *malgoBackend) deviceConfig(kind malgo.DeviceType) malgo.DeviceConfig {
	cfg := malgo.DefaultDeviceConfig(kind)
	cfg.SampleRate = SampleRate
	cfg.PeriodSizeInFrames = FrameSamples
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = Channels
	cfg.Playback.Format = malgo.FormatF32
	cfg.Playback.Channels = Channels
	return cfg
}

func (b *malgoBackend) OpenCapture(id string, onFrame func([]float32)) (Stream, error) {
	cfg := b.deviceConfig(malgo.Capture)
	if err := setDeviceID(&cfg, malgo.Capture, id); err != nil {
		return nil, err
	}
	// Scratch buffer owned by the callback goroutine; allocated ONCE here so
	// the realtime path never allocates.
	scratch := make([]float32, FrameSamples)
	dev, err := malgo.InitDevice(b.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			n := int(frames)
			if n > FrameSamples {
				n = FrameSamples
			}
			bytesToFloat32(in, scratch[:n])
			onFrame(scratch[:n])
		},
	})
	if err != nil {
		return nil, fmt.Errorf("audio: open capture: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return nil, fmt.Errorf("audio: start capture: %w", err)
	}
	return &malgoStream{dev: dev}, nil
}

func (b *malgoBackend) OpenPlayback(id string, fill func([]float32)) (Stream, error) {
	cfg := b.deviceConfig(malgo.Playback)
	if err := setDeviceID(&cfg, malgo.Playback, id); err != nil {
		return nil, err
	}
	scratch := make([]float32, FrameSamples)
	dev, err := malgo.InitDevice(b.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, frames uint32) {
			n := int(frames)
			if n > FrameSamples {
				n = FrameSamples
			}
			fill(scratch[:n])
			float32ToBytes(scratch[:n], out)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("audio: open playback: %w", err)
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		return nil, fmt.Errorf("audio: start playback: %w", err)
	}
	return &malgoStream{dev: dev}, nil
}

func (b *malgoBackend) Close() {
	if b.ctx != nil {
		_ = b.ctx.Uninit()
		b.ctx.Free()
		b.ctx = nil
	}
}

type malgoStream struct{ dev *malgo.Device }

func (s *malgoStream) Stop() error {
	if s.dev == nil {
		return nil
	}
	_ = s.dev.Stop()
	s.dev.Uninit()
	s.dev = nil
	return nil
}
```

`setDeviceID`, `bytesToFloat32` and `float32ToBytes` are small helpers in the same file: an empty `id` leaves the config's device pointer nil (which is miniaudio's "use the default"), and a non-empty one is matched against `Enumerate`'s results to recover the `malgo.DeviceID`. The byte conversions use `unsafe.Slice` over the callback's buffer — check malgo's own examples for the exact idiom against the version resolved in Step 1 and follow it rather than inventing one.

- [ ] **Step 8: Verify the whole package still builds and passes**

Run: `go build -tags purego ./... && go vet -tags purego ./internal/audio/ && go test -tags purego -race ./internal/audio/ -v`
Expected: build clean, vet clean, all tests pass. **No test may open a real device** — if the suite hangs or produces sound, a test is constructing `NewMalgoBackend`; fix the test.

- [ ] **Step 9: Commit**

```bash
git add go.mod go.sum internal/audio/backend.go internal/audio/backend_fake.go internal/audio/backend_malgo.go internal/audio/backend_fake_test.go
git commit -m "feat(audio): add the Backend seam with fake and malgo implementations

Shared-mode devices only: Star Citizen is running alongside, so exclusive
capture is off the table for the same reason Phase 3.5 refused exclusive
DirectInput.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push
```

---

## Task 9: Level buses, mixer and the SFX voice pool

**Files:**
- Create: `internal/audio/mixer.go`
- Create: `internal/audio/sfx.go`
- Create: `internal/audio/assets/manifest.toml`
- Create: `internal/audio/assets/README.md`
- Test: `internal/audio/mixer_test.go`
- Test: `internal/audio/sfx_test.go`

**Interfaces:**
- Consumes: `DecodeWAV` (Task 6), `FrameSamples`
- Produces:
  - `type Levels struct { Master, Voice, SFX, Notification float32 }`
  - `func taper(position float32) float32`
  - `type Mixer struct{ ... }`, `func NewMixer() *Mixer`, `func (m *Mixer) SetLevels(Levels)`, `func (m *Mixer) Mix(out []float32, monitor []float32, sfx []float32, notif []float32)`
  - `func NewSFX() *SFX`, `func (s *SFX) EffectIDs() []string`, `func (s *SFX) Available(id string) bool`, `func (s *SFX) Play(id string)`, `func (s *SFX) MixInto(dst []float32)`

- [ ] **Step 1: Write the asset manifest and its README**

`internal/audio/assets/manifest.toml` — the nine slots from the design. Seven are samples; `voice_effect` and `clipping_effect` are code presets (spec D12) and deliberately absent here.

```toml
# Effect slots, matching the design prototype's Radio Effects panel.
# `file` is the DEFAULT sample name; a user's config.toml may override it.
# A slot whose file is absent from this directory plays silence and logs
# once -- never a crash, never a dialog.

[tx_start]
label = "TX Start"
file = "transmit_open.wav"

[tx_end]
label = "TX End"
file = "transmit_close.wav"

[rx_start]
label = "RX Start"
file = "receive_open.wav"

[rx_end]
label = "RX End"
file = "receive_close.wav"

[intercom_start]
label = "Intercom Start"
file = "intercom_open.wav"

[intercom_end]
label = "Intercom End"
file = "intercom_close.wav"

[encryption_beep]
label = "Encryption Beep"
file = "crypto_handshake.wav"
```

`internal/audio/assets/README.md`:

```markdown
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
```

- [ ] **Step 2: Write the failing mixer and SFX tests**

```go
package audio

import "testing"

func TestTaperIsMonotonicAndBounded(t *testing.T) {
	prev := float32(-1)
	for i := 0; i <= 100; i++ {
		g := taper(float32(i) / 100)
		if g < 0 || g > 1 {
			t.Fatalf("taper(%v) = %v, outside [0,1]", float32(i)/100, g)
		}
		if g < prev {
			t.Fatalf("taper not monotonic at %v: %v < %v", float32(i)/100, g, prev)
		}
		prev = g
	}
}

func TestTaperGivesFinerControlLowDown(t *testing.T) {
	// The whole point of a taper: half-way on the knob must be well below
	// half the amplitude, or the top of the travel does nothing audible.
	if g := taper(0.5); g >= 0.4 {
		t.Fatalf("taper(0.5) = %v, want clearly below 0.4", g)
	}
}

func TestMixerAppliesMasterAndBusGains(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})

	out := make([]float32, FrameSamples)
	monitor := make([]float32, FrameSamples)
	sfx := make([]float32, FrameSamples)
	notif := make([]float32, FrameSamples)
	for i := range monitor {
		monitor[i] = 0.5
		sfx[i] = 0.5
	}
	m.Mix(out, monitor, sfx, notif)
	// SFX bus is at zero, so only the monitor survives.
	if out[0] <= 0 {
		t.Fatalf("monitor did not reach the output: %v", out[0])
	}
	m.SetLevels(Levels{Master: 0, Voice: 1, SFX: 1, Notification: 1})
	m.Mix(out, monitor, sfx, notif)
	if out[0] != 0 {
		t.Fatalf("master at zero still produced %v", out[0])
	}
}

func TestMixerNeverClipsTheOutput(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 1, Notification: 1})
	out := make([]float32, FrameSamples)
	full := make([]float32, FrameSamples)
	for i := range full {
		full[i] = 1.0
	}
	m.Mix(out, full, full, full)
	for i, v := range out {
		if v > 1.0 || v < -1.0 {
			t.Fatalf("sample %d = %v, outside [-1,1]", i, v)
		}
	}
}

func TestSFXReportsManifestSlots(t *testing.T) {
	s := NewSFX()
	ids := s.EffectIDs()
	want := []string{"tx_start", "tx_end", "rx_start", "rx_end", "intercom_start", "intercom_end", "encryption_beep"}
	if len(ids) != len(want) {
		t.Fatalf("EffectIDs() = %v, want %d slots", ids, len(want))
	}
}

func TestSFXMissingAssetIsSilentNotFatal(t *testing.T) {
	s := NewSFX()
	// No pack is vendored, so every slot is unavailable.
	if s.Available("tx_start") {
		t.Skip("an asset pack is present; this test covers the empty case")
	}
	s.Play("tx_start") // must not panic
	dst := make([]float32, FrameSamples)
	s.MixInto(dst)
	for _, v := range dst {
		if v != 0 {
			t.Fatal("a missing asset produced audio")
		}
	}
}

func TestSFXUnknownIDIsIgnored(t *testing.T) {
	s := NewSFX()
	s.Play("no_such_effect") // must not panic
}
```

- [ ] **Step 3: Run and watch them fail**

Run: `go test -tags purego ./internal/audio/ -run 'TestTaper|TestMixer|TestSFX' -v`
Expected: FAIL — `undefined: taper`

- [ ] **Step 4: Implement `mixer.go`**

```go
package audio

// Levels are the four bus positions, as stored in [audio.levels]. They are
// KNOB POSITIONS in [0,1], not gains -- see taper.
type Levels struct {
	Master       float32
	Voice        float32
	SFX          float32
	Notification float32
}

// taper converts a knob position to a gain.
//
// A linear position wired straight to amplitude makes the top third of every
// knob inaudible as a change, because loudness perception is roughly
// logarithmic. Cubing approximates a ~60 dB fader law closely enough for a
// comms client and costs nothing.
func taper(position float32) float32 {
	if position <= 0 {
		return 0
	}
	if position >= 1 {
		return 1
	}
	return position * position * position
}

// Mixer sums the buses into the playback frame.
type Mixer struct {
	levels Levels
}

func NewMixer() *Mixer {
	return &Mixer{levels: Levels{Master: 0.75, Voice: 1, SFX: 0.8, Notification: 0.8}}
}

// SetLevels replaces the bus positions. Called from the DSP goroutine only.
func (m *Mixer) SetLevels(l Levels) { m.levels = l }

// Mix writes master × (voice×monitor + sfx×sfx + notification×notif) into
// out, hard-limited to [-1, 1].
//
// Phase 5 adds received voice to the voice bus alongside monitor; the bus
// structure is here now so that is an addition rather than a reshape.
func (m *Mixer) Mix(out, monitor, sfx, notif []float32) {
	master := taper(m.levels.Master)
	gv := taper(m.levels.Voice) * master
	gs := taper(m.levels.SFX) * master
	gn := taper(m.levels.Notification) * master

	for i := range out {
		var s float32
		if i < len(monitor) {
			s += monitor[i] * gv
		}
		if i < len(sfx) {
			s += sfx[i] * gs
		}
		if i < len(notif) {
			s += notif[i] * gn
		}
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = s
	}
}
```

- [ ] **Step 5: Implement `sfx.go`**

```go
package audio

import (
	"embed"
	"log/slog"
	"sort"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed assets
var assetFS embed.FS

// maxVoices caps simultaneous one-shots. Eight is generous for radio chirps
// and bounds the mix cost; the oldest is evicted rather than refusing the
// newest, so the most recent event is always audible.
const maxVoices = 8

type effectSlot struct {
	Label string `toml:"label"`
	File  string `toml:"file"`
}

type voice struct {
	samples []float32
	pos     int
}

// SFX owns the decoded sample set and the playing voice pool.
//
// A missing asset is a first-class, expected state: the sample pack is
// supplied separately (spec D11) and the client must work fully without it.
type SFX struct {
	mu sync.Mutex

	order   []string
	slots   map[string]effectSlot
	samples map[string][]float32
	voices  []voice

	warnedOnce map[string]bool
	log        *slog.Logger
}

func NewSFX() *SFX {
	s := &SFX{
		slots:      map[string]effectSlot{},
		samples:    map[string][]float32{},
		warnedOnce: map[string]bool{},
		log:        slog.Default(),
	}
	if err := s.loadManifest(); err != nil {
		s.log.Error("audio: sfx manifest unreadable; all effects silent", "err", err)
	}
	s.loadSamples()
	return s
}

func (s *SFX) loadManifest() error {
	b, err := assetFS.ReadFile("assets/manifest.toml")
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(b, &s.slots); err != nil {
		return err
	}
	for id := range s.slots {
		s.order = append(s.order, id)
	}
	sort.Strings(s.order)
	return nil
}

func (s *SFX) loadSamples() {
	for id, slot := range s.slots {
		if slot.File == "" {
			continue
		}
		b, err := assetFS.ReadFile("assets/" + slot.File)
		if err != nil {
			// Expected until the pack lands. Logged once per asset, at info:
			// this is a known pending dependency, not a malfunction.
			s.log.Info("audio: sfx sample absent; slot silent", "effect", id, "file", slot.File)
			continue
		}
		samples, err := DecodeWAV(b)
		if err != nil {
			s.log.Warn("audio: sfx sample undecodable", "effect", id, "file", slot.File, "err", err)
			continue
		}
		s.samples[id] = samples
	}
}

// EffectIDs returns the manifest slots in stable order, for the UI.
func (s *SFX) EffectIDs() []string {
	return append([]string(nil), s.order...)
}

// Label returns an effect's display name, or the id when unknown.
func (s *SFX) Label(id string) string {
	if slot, ok := s.slots[id]; ok && slot.Label != "" {
		return slot.Label
	}
	return id
}

// Available reports whether a slot has a decoded sample behind it.
func (s *SFX) Available(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.samples[id]) > 0
}

// Play starts a one-shot. Unknown or absent ids are silently ignored.
func (s *SFX) Play(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	samples := s.samples[id]
	if len(samples) == 0 {
		return
	}
	if len(s.voices) >= maxVoices {
		s.voices = s.voices[1:] // evict oldest
	}
	s.voices = append(s.voices, voice{samples: samples})
}

// MixInto sums every active voice into dst and retires finished ones.
// Called from the DSP goroutine.
func (s *SFX) MixInto(dst []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := s.voices[:0]
	for _, v := range s.voices {
		n := copy(make([]float32, 0), dst) // placeholder to keep len semantics clear
		_ = n
		remaining := len(v.samples) - v.pos
		count := len(dst)
		if remaining < count {
			count = remaining
		}
		for i := 0; i < count; i++ {
			dst[i] += v.samples[v.pos+i]
		}
		v.pos += count
		if v.pos < len(v.samples) {
			live = append(live, v)
		}
	}
	s.voices = live
}
```

**Remove the two placeholder lines** (`n := copy(...)` and `_ = n`) when implementing — they are shown only to mark where the per-voice loop begins. The loop body must allocate nothing.

- [ ] **Step 6: Run and watch them pass**

Run: `go test -tags purego -race ./internal/audio/ -run 'TestTaper|TestMixer|TestSFX' -v`
Expected: PASS, all seven.

- [ ] **Step 7: Commit**

```bash
git add internal/audio/mixer.go internal/audio/sfx.go internal/audio/assets/ internal/audio/mixer_test.go internal/audio/sfx_test.go
git commit -m "feat(audio): add the four-bus mixer and the SFX voice pool

The sample pack is a separate, tracked dependency: every slot degrades to
silence with a single log line rather than failing.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: The Manager — lifecycle, DSP goroutine, hot-plug, device-loss fallback

This is the task everything else has been building toward, and the one the `Backend` seam exists to make testable.

**Files:**
- Create: `internal/audio/manager.go`
- Test: `internal/audio/manager_test.go`

**Interfaces:**
- Consumes: every type from Tasks 2–9
- Produces:
  - `type Config struct { InputDevice, OutputDevice string; MicPassthrough, AGC, NoiseSuppression bool; Gate GateConfig; Levels Levels; VoiceEffect, ClippingEffect string; VOXNoiseCancel bool }`
  - `type State struct { Running bool; InputError, OutputError string; Overruns, Underruns uint64 }`
  - `type VU struct { Input, Output float32 }`
  - `func NewManager(b Backend, opts ManagerOptions) *Manager`
  - `func (m *Manager) Start() error`, `Stop()`, `SetConfig(Config)`, `SetPTT(bool)`, `SetMuted(bool)`, `State() State`, `Devices() (inputs, outputs []DeviceInfo)`, `PlayEffect(id string)`, `AddSink(Sink)`, `Muted() bool`
  - `type Sink interface { WriteFrame([]float32); Close() error }`
  - `ManagerOptions{ Log *slog.Logger; OnVU func(VU); OnState func(State); OnDevices func(inputs, outputs []DeviceInfo); PollInterval time.Duration; VUInterval time.Duration }`

- [ ] **Step 1: Write the failing manager tests**

```go
package audio

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func newTestManager(t *testing.T, b *FakeBackend) *Manager {
	t.Helper()
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One", IsDefault: true}},
	)
	m := NewManager(b, ManagerOptions{
		PollInterval: 10 * time.Millisecond,
		VUInterval:   10 * time.Millisecond,
	})
	t.Cleanup(m.Stop)
	return m
}

func TestManagerStartOpensBothDevices(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if st := m.State(); !st.Running {
		t.Fatalf("State().Running = false after Start: %+v", st)
	}
}

func TestManagerCapturedAudioReachesTheSinkOnlyWhenGateIsOpen(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}

	// Gate shut: PTT not held, VOX off.
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "sink received audio with the gate shut")

	m.SetPTT(true)
	for i := 0; i < 5; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() > 0 }, "sink received nothing with PTT held")
}

func TestManagerMuteSilencesTheSink(t *testing.T) {
	b := NewFakeBackend()
	m := newTestManager(t, b)
	sink := &recordingSink{}
	m.AddSink(sink)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	m.SetPTT(true)
	m.SetMuted(true)
	for i := 0; i < 10; i++ {
		b.PushFrame(sine(0.5))
	}
	waitFor(t, func() bool { return sink.count() == 0 }, "muted mic still reached the sink")
}

func TestManagerHotPlugEmitsDeviceChange(t *testing.T) {
	b := NewFakeBackend()
	var mu sync.Mutex
	changed := 0
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One"}}, []DeviceInfo{{ID: "out-1", Name: "Out One"}})
	m := NewManager(b, ManagerOptions{
		PollInterval: 5 * time.Millisecond,
		VUInterval:   time.Hour,
		OnDevices: func(_, _ []DeviceInfo) {
			mu.Lock()
			changed++
			mu.Unlock()
		},
	})
	t.Cleanup(m.Stop)
	if err := m.Start(); err != nil {
		t.Fatal(err)
	}
	b.SetDevices(
		[]DeviceInfo{{ID: "mic-1", Name: "Mic One"}, {ID: "mic-2", Name: "Mic Two"}},
		[]DeviceInfo{{ID: "out-1", Name: "Out One"}},
	)
	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return changed > 0
	}, "hot-plug produced no OnDevices callback")
}

func TestManagerFallsBackToDefaultWhenSavedDeviceIsGone(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", Name: "Mic One", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	m := NewManager(b, ManagerOptions{PollInterval: 5 * time.Millisecond, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	m.SetConfig(Config{InputDevice: "mic-vanished", OutputDevice: ""})
	if err := m.Start(); err != nil {
		t.Fatalf("Start with an absent saved device must fall back, got: %v", err)
	}
	if st := m.State(); !st.Running {
		t.Fatalf("not running after fallback: %+v", st)
	}
}

func TestManagerReportsInputOpenFailure(t *testing.T) {
	b := NewFakeBackend()
	b.SetDevices([]DeviceInfo{{ID: "mic-1", IsDefault: true}}, []DeviceInfo{{ID: "out-1", IsDefault: true}})
	b.FailNextOpen(errors.New("device busy"))
	m := NewManager(b, ManagerOptions{PollInterval: time.Hour, VUInterval: time.Hour})
	t.Cleanup(m.Stop)
	_ = m.Start()
	if st := m.State(); st.InputError == "" {
		t.Fatalf("State().InputError empty after a failed open: %+v", st)
	}
}

// recordingSink counts frames written to it.
type recordingSink struct {
	mu sync.Mutex
	n  int
}

func (s *recordingSink) WriteFrame(f []float32) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
}
func (s *recordingSink) Close() error { return nil }
func (s *recordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// waitFor polls cond for up to a second. The DSP goroutine is asynchronous,
// so tests synchronise on observable state rather than on sleeps.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(msg)
}
```

- [ ] **Step 2: Run and watch them fail**

Run: `go test -tags purego ./internal/audio/ -run TestManager -v`
Expected: FAIL — `undefined: NewManager`

- [ ] **Step 3: Implement `manager.go`**

Build it to satisfy the tests, following these rules — they are the spec's architecture, not suggestions:

1. `Start()` enumerates, resolves the configured device IDs (empty or missing ⇒ the entry with `IsDefault`, else the first), opens capture and playback, then launches the DSP goroutine, the poll ticker and the VU ticker.
2. The **capture callback** does nothing but `captureRing.Write(frame)`. When the DSP goroutine observes `Dropped()` advancing on a ring, it should call that ring's `Drain()` so the backlog left behind by refused writes doesn't linger as latency.
3. The **playback callback** does nothing but `playbackRing.Read(dst)`, zero-filling a short read.
4. The **DSP goroutine** loops: drain one 480-sample frame from `captureRing`; if `VOXNoiseCancel` is set, denoise before measuring the level, else measure first; run `Denoiser` (when `NoiseSuppression`), `AGC` (when `AGC`), `Gate.Step`; when the gate is open, write to every `Sink` and, when `MicPassthrough`, into the monitor buffer; ask `SFX.MixInto`; `Mixer.Mix` into the playback ring. Accumulate peak VU for input and output.
5. **Device resolution never fails the start.** A saved ID that no longer enumerates falls back to the default and records the substitution in `State`, per spec §13.
6. **A failed open records the error in `State` and leaves the manager running** so the other direction still works — a broken microphone must not take playback down with it.
7. `Stop()` is idempotent and orders teardown as: stop DSP goroutine → stop streams → `Backend.Close()`.
8. Nothing in the DSP loop or either callback logs, allocates, or takes a contended lock. All scratch buffers are allocated once in `Start`.

- [ ] **Step 4: Run under race and iterate until green**

Run: `go test -tags purego -race ./internal/audio/ -run TestManager -v`
Expected: PASS, all six tests, no race reports.

- [ ] **Step 5: Run the whole package**

Run: `go test -tags purego -race ./internal/audio/ -v && go vet -tags purego ./internal/audio/`
Expected: every test passes, vet clean.

- [ ] **Step 6: Commit**

```bash
git add internal/audio/manager.go internal/audio/manager_test.go
git commit -m "feat(audio): add the manager, DSP goroutine and hot-plug poll

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push
```

---

## Task 11: `[audio]` config schema

**Files:**
- Modify: `internal/config/config.go` (add `Audio`, extend `Config` and `Default()`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing from `internal/audio` — `internal/config` stays a raw-values layer and must **not** import the audio package, the same discipline that keeps it ignorant of `internal/trigger`'s grammar.
- Produces: `config.Audio`, `config.AudioLevels`, `config.AudioEffect`, and `Config.Audio`

- [ ] **Step 1: Write the failing tests**

```go
func TestDefaultAudioMatchesTheSpec(t *testing.T) {
	a := Default().Audio
	if a.Levels.Master != 0.75 || a.Levels.Voice != 1.0 || a.Levels.SFX != 0.8 || a.Levels.Notification != 0.8 {
		t.Fatalf("default levels = %+v", a.Levels)
	}
	if !a.AGC || !a.NoiseSuppression {
		t.Fatal("AGC and noise suppression must default on")
	}
	if a.VOX {
		t.Fatal("VOX must default off")
	}
	if a.PTTReleaseDelayMS != 120 {
		t.Fatalf("ptt_release_delay_ms = %d, want 120", a.PTTReleaseDelayMS)
	}
	if a.Effects != nil {
		t.Fatal("Effects must default nil, not an empty map -- see the KeybindDevices comment")
	}
}

func TestLoadConfigWithoutAudioTableGetsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	// A pre-Phase-4 config file.
	if err := os.WriteFile(path, []byte("log_level = \"INFO\"\nserver_url = \"\"\nping_interval_seconds = 5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Audio.Levels.Master != 0.75 {
		t.Fatalf("missing [audio] did not fall back to defaults: %+v", cfg.Audio)
	}
}

func TestAudioRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	in := Default()
	in.Audio.InputDevice = "mic-7"
	in.Audio.InputDeviceName = "Procyon Headset"
	in.Audio.VOX = true
	in.Audio.VOXThreshold = 0.42
	in.Audio.Levels.SFX = 0.5
	in.Audio.Effects = map[string]AudioEffect{"tx_start": {Enabled: false, File: "custom.wav"}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Audio.InputDevice != "mic-7" || out.Audio.VOXThreshold != 0.42 || out.Audio.Levels.SFX != 0.5 {
		t.Fatalf("round trip lost values: %+v", out.Audio)
	}
	if e := out.Audio.Effects["tx_start"]; e.Enabled || e.File != "custom.wav" {
		t.Fatalf("effect round trip = %+v", e)
	}
}
```

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/config/ -run 'TestDefaultAudio|TestLoadConfigWithoutAudio|TestAudioRoundTrips' -v`
Expected: FAIL — `undefined: AudioEffect`

- [ ] **Step 3: Implement the schema**

```go
// AudioLevels holds the four mixer bus positions from Settings > Audio.
// These are KNOB POSITIONS in [0,1], not gains: internal/audio applies the
// perceptual taper. Storing gains here would bake a UI decision into the
// file format.
type AudioLevels struct {
	Master       float32 `toml:"master"`
	Voice        float32 `toml:"voice"`
	SFX          float32 `toml:"sfx"`
	Notification float32 `toml:"notification"`
}

// AudioEffect is one Radio Effects slot's persisted state.
type AudioEffect struct {
	Enabled bool   `toml:"enabled"`
	File    string `toml:"file"`
}

// Audio holds Settings > Audio & Sounds.
//
// Device identity is stored as ID PLUS display name, the same shape as
// KeybindDevices and for the same reason: a device that is unplugged right
// now must still render a meaningful name rather than a raw id. An EMPTY
// device id means "follow the system default" and is a real choice, not a
// missing value.
type Audio struct {
	InputDevice      string `toml:"input_device"`
	OutputDevice     string `toml:"output_device"`
	InputDeviceName  string `toml:"input_device_name"`
	OutputDeviceName string `toml:"output_device_name"`

	MicPassthrough   bool `toml:"mic_passthrough"`
	AGC              bool `toml:"agc"`
	NoiseSuppression bool `toml:"noise_suppression"`

	VOX             bool    `toml:"vox"`
	VOXThreshold    float32 `toml:"vox_threshold"`
	VOXMinLengthMS  int     `toml:"vox_min_length_ms"`
	VOXNoiseCancel  bool    `toml:"vox_noise_cancel"`
	VOXHangMS       int     `toml:"vox_hang_ms"`

	PTTStartDelayMS   int `toml:"ptt_start_delay_ms"`
	PTTReleaseDelayMS int `toml:"ptt_release_delay_ms"`

	VoiceEffect    string `toml:"voice_effect"`
	ClippingEffect string `toml:"clipping_effect"`

	Levels AudioLevels `toml:"levels"`

	// Effects is nil until the user customises a slot. Deliberately NOT an
	// empty map: the TOML encoder writes table headers for an empty-but-
	// non-nil map, which would add noise to every config file on first save
	// for no gain. Same reasoning as KeybindDevices.
	Effects map[string]AudioEffect `toml:"effects"`
}
```

Add `Audio Audio \`toml:"audio"\`` to `Config`, and to `Default()`:

```go
		Audio: Audio{
			AGC:               true,
			NoiseSuppression:  true,
			VOX:               false,
			VOXThreshold:      0.35,
			VOXMinLengthMS:    220,
			VOXHangMS:         300,
			VOXNoiseCancel:    true,
			PTTStartDelayMS:   0,
			PTTReleaseDelayMS: 120,
			VoiceEffect:       "comms_filter_mid",
			ClippingEffect:    "",
			Levels: AudioLevels{
				Master:       0.75,
				Voice:        1.0,
				SFX:          0.8,
				Notification: 0.8,
			},
			Effects: nil,
		},
```

- [ ] **Step 4: Run and watch it pass**

Run: `go test -tags purego -race ./internal/config/ -v`
Expected: PASS, including every pre-existing config test — the `[audio]` addition must not disturb the keybind round-trip tests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add the [audio] table with spec defaults

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Audio events

**Files:**
- Modify: `internal/events/events.go`
- Test: `internal/events/events_test.go`
- Modify: `frontend/src/shared/api/events.ts`

**Interfaces:**
- Produces: `EventAudioDevicesChanged`, `EventAudioVU`, `EventAudioState`, `EventAudioMicMuted`, and `Tagged` methods `AudioDevicesChanged(payload any)`, `AudioVU(payload any)`, `AudioState(payload any)`, `AudioMicMuted(bool)`

- [ ] **Step 1: Write the failing test**

```go
func TestAudioEventNames(t *testing.T) {
	cases := map[string]string{
		EventAudioDevicesChanged: "audio:devices_changed",
		EventAudioVU:             "audio:vu",
		EventAudioState:          "audio:state",
		EventAudioMicMuted:       "audio:mic_muted",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("event name %q, want %q", got, want)
		}
	}
}

func TestAudioEmittersForwardPayloads(t *testing.T) {
	rec := &recordingEmitter{} // existing test double in this file
	tg := NewTagged(rec)
	tg.AudioMicMuted(true)
	if rec.last().name != EventAudioMicMuted {
		t.Fatalf("emitted %q", rec.last().name)
	}
}
```

Match the existing test double's name and constructor in `events_test.go` rather than introducing a new one.

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./internal/events/ -run TestAudio -v`
Expected: FAIL — `undefined: EventAudioDevicesChanged`

- [ ] **Step 3: Add the constants and emitters**

```go
	// EventAudioDevicesChanged carries the FULL input and output device
	// lists, not a single device -- which is why it is plural, departing
	// from the master spec §5.4's predicted "audio:device_changed".
	EventAudioDevicesChanged = "audio:devices_changed"
	// EventAudioVU is the ~20 Hz meter update. It is SUPPRESSED WHEN
	// UNCHANGED: an idle or muted mic emitting twenty identical zeros per
	// second to every open window is pure bus noise.
	EventAudioVU = "audio:vu"
	// EventAudioState is the audio subsystem's health, the sibling of
	// EventHotkeysState and EventJoystickState. Phase 7's notification
	// channel absorbs all three uniformly, so the shape stays parallel.
	EventAudioState = "audio:state"
	// EventAudioMicMuted reports the push-to-mute / mute-toggle state.
	EventAudioMicMuted = "audio:mic_muted"
```

Plus four `Tagged` methods in the style of `SettingsChanged`.

- [ ] **Step 4: Mirror the names in the frontend**

Add to `EV` in `frontend/src/shared/api/events.ts`:

```ts
  audioDevicesChanged: "audio:devices_changed",
  audioVU: "audio:vu",
  audioState: "audio:state",
  audioMicMuted: "audio:mic_muted",
```

- [ ] **Step 5: Run and commit**

Run: `go test -tags purego -race ./internal/events/ -v`
Expected: PASS.

```bash
git add internal/events/events.go internal/events/events_test.go frontend/src/shared/api/events.ts
git commit -m "feat(events): add the audio:* event channel

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: App bindings, DTOs and PTT/mute wiring

**Files:**
- Create: `internal/app/audio.go`
- Modify: `internal/app/dto.go` (`SettingsDTO` gains `Audio`; three new DTOs)
- Modify: `internal/app/settings.go` (`GetSettings`/`SetSettings` carry audio)
- Test: `internal/app/audio_test.go`
- Test: `internal/app/settings_test.go` (extend)

**Interfaces:**
- Consumes: `audio.Manager`, `config.Audio`, the `events` additions
- Produces:
  - `func (a *App) SetAudioBackend(m *audio.Manager)`
  - `func (a *App) GetAudioDevices() AudioDevicesDTO`
  - `func (a *App) GetAudioState() AudioStateDTO`
  - `func (a *App) StartMicTest() error` / `StopMicTest() error`
  - `func (a *App) PreviewEffect(id string) error`
  - `AudioSettingsDTO`, `AudioDevicesDTO`, `AudioDeviceDTO`, `AudioStateDTO`

- [ ] **Step 1: Write the failing tests**

```go
func TestSetSettingsPersistsAudio(t *testing.T) {
	a, path := newTestApp(t) // existing helper in settings_test.go
	s := a.GetSettings()
	s.Audio.VOX = true
	s.Audio.Levels.SFX = 0.25
	s.Audio.InputDevice = "mic-9"
	if err := a.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Audio.VOX || cfg.Audio.Levels.SFX != 0.25 || cfg.Audio.InputDevice != "mic-9" {
		t.Fatalf("audio settings not persisted: %+v", cfg.Audio)
	}
}

func TestGetSettingsReflectsAudioDefaults(t *testing.T) {
	a, _ := newTestApp(t)
	s := a.GetSettings()
	if !s.Audio.AGC || s.Audio.Levels.Master != 0.75 {
		t.Fatalf("audio defaults missing from SettingsDTO: %+v", s.Audio)
	}
}

func TestMuteToggleActionFlipsManagerMute(t *testing.T) {
	a, _ := newTestApp(t)
	m := audio.NewManager(audio.NewFakeBackend(), audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)
	a.SetAudioBackend(m)

	a.onActionPressed("global.mute_toggle")
	if !m.Muted() {
		t.Fatal("mute_toggle did not mute")
	}
	a.onActionPressed("global.mute_toggle")
	if m.Muted() {
		t.Fatal("mute_toggle did not unmute on the second press")
	}
}

func TestPushToMuteHoldsMuteOnlyWhileHeld(t *testing.T) {
	a, _ := newTestApp(t)
	m := audio.NewManager(audio.NewFakeBackend(), audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)
	a.SetAudioBackend(m)

	a.onActionPressed("global.push_to_mute")
	if !m.Muted() {
		t.Fatal("push_to_mute did not mute on press")
	}
	a.onActionReleased("global.push_to_mute")
	if m.Muted() {
		t.Fatal("push_to_mute stayed muted after release")
	}
}

func TestPTTActionReachesTheManager(t *testing.T) {
	a, _ := newTestApp(t)
	m := audio.NewManager(audio.NewFakeBackend(), audio.ManagerOptions{
		PollInterval: time.Hour, VUInterval: time.Hour,
	})
	t.Cleanup(m.Stop)
	a.SetAudioBackend(m)

	a.onActionPressed("global.ptt")
	if !m.PTT() {
		t.Fatal("global.ptt did not reach the manager")
	}
	a.onActionReleased("global.ptt")
	if m.PTT() {
		t.Fatal("global.ptt release did not reach the manager")
	}
}
```

`onActionPressed` / `onActionReleased` are the **existing** hotkey/joystick dispatch entry points in `internal/app`. Find their real names first with `grep -n 'HotkeyPressed\|pressCount\|func (a \*App) on' internal/app/*.go` and use those, rather than adding a parallel path. Add `PTT()` and `Muted()` accessors to `audio.Manager` if Task 10 did not already produce them.

- [ ] **Step 2: Run and watch them fail**

Run: `go test -tags purego ./internal/app/ -run 'TestSetSettingsPersistsAudio|TestGetSettingsReflectsAudio|TestMuteToggle|TestPushToMute|TestPTTAction' -v`
Expected: FAIL.

- [ ] **Step 3: Add the DTOs**

```go
// AudioSettingsDTO is the binding-facing shape of config.Audio. Levels and
// the VOX threshold are normalized 0-1 here; the UI renders the design's
// 0-100 scale. Keeping the scale conversion in one place (the React
// component) stops the two representations from drifting.
type AudioSettingsDTO struct {
	InputDevice       string                     `json:"input_device"`
	OutputDevice      string                     `json:"output_device"`
	InputDeviceName   string                     `json:"input_device_name"`
	OutputDeviceName  string                     `json:"output_device_name"`
	MicPassthrough    bool                       `json:"mic_passthrough"`
	AGC               bool                       `json:"agc"`
	NoiseSuppression  bool                       `json:"noise_suppression"`
	VOX               bool                       `json:"vox"`
	VOXThreshold      float32                    `json:"vox_threshold"`
	VOXMinLengthMS    int                        `json:"vox_min_length_ms"`
	VOXHangMS         int                        `json:"vox_hang_ms"`
	VOXNoiseCancel    bool                       `json:"vox_noise_cancel"`
	PTTStartDelayMS   int                        `json:"ptt_start_delay_ms"`
	PTTReleaseDelayMS int                        `json:"ptt_release_delay_ms"`
	VoiceEffect       string                     `json:"voice_effect"`
	ClippingEffect    string                     `json:"clipping_effect"`
	Levels            AudioLevelsDTO             `json:"levels"`
	Effects           map[string]AudioEffectDTO  `json:"effects"`
}

type AudioLevelsDTO struct {
	Master       float32 `json:"master"`
	Voice        float32 `json:"voice"`
	SFX          float32 `json:"sfx"`
	Notification float32 `json:"notification"`
}

type AudioEffectDTO struct {
	Enabled bool   `json:"enabled"`
	File    string `json:"file"`
	Label   string `json:"label"`
	// Available is false when no sample is behind the slot -- expected
	// until the sample pack lands. The UI greys the row rather than
	// presenting a PREVIEW button that does nothing.
	Available bool `json:"available"`
}

// AudioDeviceDTO is one selectable endpoint.
type AudioDeviceDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
}

type AudioDevicesDTO struct {
	Inputs  []AudioDeviceDTO `json:"inputs"`
	Outputs []AudioDeviceDTO `json:"outputs"`
}

// AudioStateDTO reports the audio subsystem's health, mirroring
// HotkeyStateDTO and JoystickStateDTO so Phase 7's notification channel can
// absorb all three the same way.
type AudioStateDTO struct {
	Running     bool   `json:"running"`
	InputError  string `json:"input_error"`
	OutputError string `json:"output_error"`
	Overruns    uint64 `json:"overruns"`
	Underruns   uint64 `json:"underruns"`
}
```

Add `Audio AudioSettingsDTO \`json:"audio"\`` to `SettingsDTO`.

- [ ] **Step 4: Extend `GetSettings` / `SetSettings`**

Fill and read `s.Audio` alongside the existing General fields, **inside the existing copy-persist-swap under `writeMu`**. Do not add a second persistence path. After a successful save, push the new audio config into the manager with `m.SetConfig(...)` so the running engine follows the settings — but do that **after** `sb.mu.Unlock()`, never while holding the lock.

- [ ] **Step 5: Write `internal/app/audio.go`**

Bindings plus the action wiring. The action hook must sit in whatever existing dispatch function Step 1's grep identified:

- `global.ptt` and `radio.N.ptt` → `m.SetPTT(true/false)` on press/release
- `global.push_to_mute` → `m.SetMuted(true)` on press, `false` on release
- `global.mute_toggle` → `m.SetMuted(!m.Muted())` on press only

All three must be **no-ops when no audio backend is set**, so every existing test that runs `App` without audio keeps passing.

- [ ] **Step 6: Run and watch them pass**

Run: `go test -tags purego -race ./internal/app/ -v`
Expected: PASS — the new tests and every pre-existing one.

- [ ] **Step 7: Commit**

```bash
git add internal/app/audio.go internal/app/dto.go internal/app/settings.go internal/app/audio_test.go internal/app/settings_test.go
git commit -m "feat(app): expose audio settings, devices and state to the frontend

Gives global.push_to_mute and global.mute_toggle their first consumers
since Phase 3 shipped them bound but inert.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Wire the manager into `main.go`

**Files:**
- Modify: `main.go`
- Test: `main_wiring_test.go`

- [ ] **Step 1: Extend the wiring test**

Follow the existing assertions in `main_wiring_test.go` and add one proving the audio manager is constructed and handed to `App` during startup, and that a failure to construct the malgo backend does **not** abort startup — the client must still run, with `audio:state` reporting the failure.

- [ ] **Step 2: Run and watch it fail**

Run: `go test -tags purego ./ -run TestWiring -v`

- [ ] **Step 3: Wire it**

Construct `audio.NewMalgoBackend()`; on error, log and continue with a nil manager. On success build `audio.NewManager` with `ManagerOptions` whose callbacks emit the four `audio:*` events through the existing emitter, call `Start()`, hand it to `app.SetAudioBackend`, and register `Stop()` in the existing shutdown path so devices are released cleanly.

- [ ] **Step 4: Run, vet, commit**

Run: `go vet -tags purego ./... && go test -tags purego -race ./... `
Expected: whole tree green.

```bash
git add main.go main_wiring_test.go
git commit -m "feat: start the audio engine at launch

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push
```

---

## Task 15: VU and Knob React components

Ported from the design prototype's component gallery. Both are shared components because Comms (Phase 5/6) needs them too.

**Files:**
- Create: `frontend/src/shared/components/VU.tsx`
- Create: `frontend/src/shared/components/Knob.tsx`
- Test: `frontend/src/shared/components/VU.test.tsx`
- Test: `frontend/src/shared/components/Knob.test.tsx`

**Interfaces:**
- Produces:
  - `<VU level={number} segs?={number} />` — `level` in 0–1, default 16 segments
  - `<Knob value={number} onChange={(v:number)=>void} label?={string} size?={number} min?={number} max?={number} />` — `value` in the design's 0–100 scale

- [ ] **Step 1: Read the design's implementations**

Run: `grep -n 'function VU\|function Knob' -A 40 design/vcs/project/components.jsx 2>/dev/null || grep -rn 'function VU' -A 40 design/vcs/project/`

Port the markup, class names and token usage faithfully — CLAUDE.md requires the real UI to match the prototype visually.

- [ ] **Step 2: Write the failing tests**

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { VU } from "./VU";
import { Knob } from "./Knob";

describe("VU", () => {
  it("renders the requested number of segments", () => {
    const { container } = render(<VU level={0.5} segs={16} />);
    expect(container.querySelectorAll("[data-vu-seg]")).toHaveLength(16);
  });

  it("lights a proportion of segments matching the level", () => {
    const { container } = render(<VU level={0.5} segs={16} />);
    const lit = container.querySelectorAll("[data-vu-seg][data-lit='true']");
    expect(lit.length).toBeGreaterThan(6);
    expect(lit.length).toBeLessThan(10);
  });

  it("clamps out-of-range levels instead of overflowing", () => {
    const { container } = render(<VU level={5} segs={16} />);
    expect(container.querySelectorAll("[data-vu-seg][data-lit='true']")).toHaveLength(16);
  });
});

describe("Knob", () => {
  it("exposes its value to assistive tech", () => {
    render(<Knob value={70} onChange={() => {}} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    expect(knob).toHaveAttribute("aria-valuenow", "70");
  });

  it("changes value on arrow keys", async () => {
    const onChange = vi.fn();
    render(<Knob value={70} onChange={onChange} label="MASTER" />);
    const knob = screen.getByRole("slider", { name: /master/i });
    knob.focus();
    await import("@testing-library/user-event").then(({ default: ue }) => ue.setup().keyboard("{ArrowUp}"));
    expect(onChange).toHaveBeenCalledWith(71);
  });
});
```

A rotary control must be keyboard operable — `role="slider"` with arrow-key handling is the accessible baseline, and the prototype's pointer-drag behaviour is added on top of it, not instead of it.

- [ ] **Step 3: Run and watch them fail**

Run: `npm test --prefix frontend -- VU Knob`
Expected: FAIL — module not found.

- [ ] **Step 4: Implement both components**

Port the visuals from the prototype; add `data-vu-seg` / `data-lit` attributes for testability, `role="slider"` with `aria-valuenow` / `aria-valuemin` / `aria-valuemax` / `aria-label` on the knob, and arrow-key handling (±1, ±10 with Page keys).

- [ ] **Step 5: Run, typecheck, commit**

Run: `npm test --prefix frontend -- VU Knob && npx --prefix frontend tsc --noEmit`
Expected: PASS, no type errors.

```bash
git add frontend/src/shared/components/VU.tsx frontend/src/shared/components/Knob.tsx frontend/src/shared/components/VU.test.tsx frontend/src/shared/components/Knob.test.tsx
git commit -m "feat(ui): add the VU meter and Knob components

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 16: Settings → Audio & Sounds section

**Files:**
- Create: `frontend/src/windows/main/screens/settings/sections/Audio.tsx`
- Test: `frontend/src/windows/main/screens/settings/sections/Audio.test.tsx`
- Modify: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx` (drop the `audio` stub)
- Modify: `frontend/src/shared/store/settings.ts` (`Settings.audio`, device + audio-state slices)
- Modify: `frontend/src/shared/api/client.ts` (new bindings)
- Modify: `frontend/src/shared/store/useSettingsSync.ts` (subscribe to the audio events)

- [ ] **Step 1: Extend the store and API client**

Add to `settings.ts`: `AudioSettings`, `AudioLevels`, `AudioEffect`, `AudioDevice`, `AudioState` interfaces mirroring the Go DTOs field-for-field; `Settings` gains `audio: AudioSettings`; the store gains `audioDevices`, `audioState`, `vu`, with setters. Default `audioState` to `{ running: false, input_error: "", output_error: "", overruns: 0, underruns: 0 }` — honest, not optimistic, matching the reasoning already written into the `joystick` default.

Add to `client.ts`: `getAudioDevices`, `getAudioState`, `startMicTest`, `stopMicTest`, `previewEffect`.

Subscribe in `useSettingsSync.ts` to `EV.audioDevicesChanged`, `EV.audioState`, `EV.audioVU`, `EV.audioMicMuted`, writing each into the store — following exactly the pattern already used for `joystickState`.

- [ ] **Step 2: Write the failing section tests**

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { Audio } from "./Audio";
import { useSettings } from "../../../../../shared/store/settings";

const setSettings = vi.fn();
vi.mock("../../../../../shared/api/client", () => ({
  api: {
    setSettings: (...a: unknown[]) => setSettings(...a),
    getAudioDevices: () => Promise.resolve({ inputs: [], outputs: [] }),
    startMicTest: () => Promise.resolve(),
    stopMicTest: () => Promise.resolve(),
  },
}));

function seed() {
  useSettings.setState({
    settings: {
      start_minimized: false, minimize_to_tray: true, show_transmitter_name: true,
      play_connection_sounds: true, radio_switch_as_ptt: false,
      audio: {
        input_device: "", output_device: "", input_device_name: "", output_device_name: "",
        mic_passthrough: false, agc: true, noise_suppression: true,
        vox: false, vox_threshold: 0.35, vox_min_length_ms: 220, vox_hang_ms: 300,
        vox_noise_cancel: true, ptt_start_delay_ms: 0, ptt_release_delay_ms: 120,
        voice_effect: "comms_filter_mid", clipping_effect: "",
        levels: { master: 0.75, voice: 1, sfx: 0.8, notification: 0.8 },
        effects: {},
      },
    },
    audioDevices: {
      inputs: [{ id: "", name: "System Default", is_default: true }, { id: "mic-1", name: "Procyon Headset", is_default: false }],
      outputs: [{ id: "", name: "System Default", is_default: true }],
    },
  });
}

describe("Audio settings", () => {
  beforeEach(() => { setSettings.mockClear(); seed(); });

  it("lists enumerated input devices including System Default", () => {
    render(<Audio />);
    expect(screen.getByRole("option", { name: "System Default" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Procyon Headset" })).toBeInTheDocument();
  });

  it("persists a device change through setSettings", async () => {
    render(<Audio />);
    await userEvent.selectOptions(screen.getByLabelText(/microphone/i), "mic-1");
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.input_device).toBe("mic-1");
  });

  it("hides the VOX detail rows until VOX is enabled", async () => {
    render(<Audio />);
    expect(screen.queryByLabelText(/vox threshold/i)).not.toBeInTheDocument();
    await userEvent.click(screen.getByLabelText(/voice activation/i));
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.vox).toBe(true);
  });

  it("renders all four level knobs", () => {
    render(<Audio />);
    for (const name of [/master/i, /voice/i, /sfx/i, /notification/i]) {
      expect(screen.getByRole("slider", { name })).toBeInTheDocument();
    }
  });

  it("renders the live VU meter from store state", () => {
    useSettings.setState({ vu: { input: 0.5, output: 0 } });
    const { container } = render(<Audio />);
    expect(container.querySelectorAll("[data-vu-seg]").length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 3: Run and watch them fail**

Run: `npm test --prefix frontend -- Audio`

- [ ] **Step 4: Implement `Audio.tsx`**

Four panels in this order — **DEVICES**, **LEVELS**, **MIC TEST**, **PROCESSING** — per the spec's approved deviation (LEVELS is new; MIC TEST keeps only its button and VU).

Follow `General.tsx` exactly on state flow: **Go is the single source of truth.** Every control calls `api.setSettings({...settings, audio: {...settings.audio, ...patch}})` and never writes the store itself; the row re-renders only when `settings:changed` lands. No local mirror state.

Convert between the persisted normalized 0–1 and the design's 0–100 presentation **in this component**, the one place that decision lives.

- [ ] **Step 5: Drop the stub and run everything**

Replace `case "audio":` in `SettingsScreen.tsx` with `<Audio />`, and update `SettingsScreen.test.tsx` if it asserts on the stub's text.

Run: `npm test --prefix frontend && npx --prefix frontend tsc --noEmit`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/windows/main/screens/settings/ frontend/src/shared/store/ frontend/src/shared/api/client.ts
git commit -m "feat(ui): build the Audio & Sounds settings section

Adds the LEVELS panel holding all four bus knobs, an approved deviation
from the prototype which put MASTER inside MIC TEST.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 17: Settings → Radio Effects section

**Files:**
- Create: `frontend/src/windows/main/screens/settings/sections/Effects.tsx`
- Test: `frontend/src/windows/main/screens/settings/sections/Effects.test.tsx`
- Modify: `frontend/src/windows/main/screens/settings/SettingsScreen.tsx`

- [ ] **Step 1: Write the failing tests**

```tsx
describe("Radio Effects settings", () => {
  it("renders a row per manifest slot plus the two preset rows", () => {
    render(<Effects />);
    expect(screen.getByText("TX Start")).toBeInTheDocument();
    expect(screen.getByText("Encryption Beep")).toBeInTheDocument();
    expect(screen.getByLabelText(/voice effect/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/clipping effect/i)).toBeInTheDocument();
  });

  it("disables PREVIEW for a slot with no sample behind it", () => {
    render(<Effects />);
    expect(screen.getByRole("button", { name: /preview tx start/i })).toBeDisabled();
  });

  it("persists a preset change through setSettings", async () => {
    render(<Effects />);
    await userEvent.selectOptions(screen.getByLabelText(/voice effect/i), "comms_filter_high");
    await waitFor(() => expect(setSettings).toHaveBeenCalled());
    expect(setSettings.mock.calls[0][0].audio.voice_effect).toBe("comms_filter_high");
  });
});
```

Seed the store the same way Task 16's tests do, with `effects` carrying `available: false` entries so the disabled-PREVIEW case is the default one — which is the state the app genuinely ships in until the sample pack arrives.

- [ ] **Step 2: Run, implement, run**

Seven sample rows (label, sample select, PREVIEW, enable toggle) plus two preset rows. **PREVIEW is disabled when `available` is false** — offering a button that does nothing is worse than showing an honestly inert one. Same single-source-of-truth state flow as Task 16.

Run: `npm test --prefix frontend -- Effects && npx --prefix frontend tsc --noEmit`

- [ ] **Step 3: Commit**

```bash
git add frontend/src/windows/main/screens/settings/
git commit -m "feat(ui): build the Radio Effects settings section

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 18: macOS microphone permission, docs, and phase close-out

**Files:**
- Modify: the macOS `Info.plist` under `build/`
- Create: `docs/superpowers/plans/2026-09-23-phase-4-manual-verification.md`
- Modify: `docs/ROADMAP.md` (Phase 4 row)
- Modify: `docs/superpowers/specs/2026-05-31-vcs-client-design.md` (retire R3)
- Modify: `CLAUDE.md` (current-status table)

- [ ] **Step 1: Add the microphone usage description**

Run: `find build -name 'Info.plist*'`

Add:

```xml
	<key>NSMicrophoneUsageDescription</key>
	<string>VCS needs microphone access to transmit your voice over the radio.</string>
```

Without it macOS **terminates the process** on first capture rather than prompting. This is a second, independent TCC surface from Phase 3's Accessibility grant — denying one says nothing about the other.

- [ ] **Step 2: Write the manual verification checklist**

Create `docs/superpowers/plans/2026-09-23-phase-4-manual-verification.md` in the shape of the Phase 3 and 3.5 checklists, covering every item automated tests cannot reach:

- Device enumeration and selection on each of Windows, macOS, Linux
- macOS: first-launch microphone prompt; behaviour when **denied**; recovery after granting in System Settings
- Unplug/replug the selected input mid-session; unplug the output mid-session
- Bluetooth headset connect/disconnect mid-session
- `System Default` following an OS default change
- **Coexistence with Star Citizen**: game audio unaffected while VCS holds both devices, in both launch orders
- PTT from keyboard and from joystick, including both held then one released (Phase 3.5's refcount, now audible)
- PTT start/release delay perceptually correct; VOX threshold usable in a noisy room
- AGC and NS audibly doing what their toggles claim
- All four level knobs audibly independent
- Latency and glitching under load (game running, several apps open); overrun/underrun counters staying at zero
- Effect PREVIEW once the sample pack lands

- [ ] **Step 3: Retire R3 and update the phase trackers**

In the master spec's §9 table, strike R3 the way R1 was struck, noting it was closed by Phase 4's per-OS CI matrix.

In `docs/ROADMAP.md`, set Phase 4 to `[x]` with the completion date, link both the design doc and the manual checklist, and state plainly — as the Phase 3 and 3.5 rows do — that the phase is **code-complete, not field-verified**, and that the SFX sample pack is still outstanding.

In `CLAUDE.md`'s status table, update the Phase 4 row and set Phase 5 as next, repeating that it is blocked on the server protocol reference.

- [ ] **Step 4: Full verification before claiming done**

Run every one of these and paste the output into the final report:

```bash
go build -tags purego ./...
go vet -tags purego ./...
go test -tags purego -race ./...
npm ci --prefix frontend
npm test --prefix frontend
npx --prefix frontend tsc --noEmit
npm run build --prefix frontend
```

Expected: all green. Then push and confirm all three CI platforms pass.

- [ ] **Step 5: Commit**

```bash
git add build docs CLAUDE.md
git commit -m "docs: close out Phase 4 and retire spec risk R3

Phase 4 is code-complete, not field-verified: the manual hardware
checklist is written and waiting for a human, and the SFX sample pack is
still outstanding.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
git push
```

---

## Plan self-review

**Spec coverage.** Every §3 in-scope item maps to a task: malgo lifecycle → 8, 10; device picker → 16; NS → 7; AGC → 3; VU → 10, 15, 16; VOX → 4, 16; PTT delays → 4; push-to-mute/mute-toggle → 13; mic passthrough → 10, 16; DSP presets → 5, 17; SFX engine → 9, 17; four level buses → 9, 16; CI matrix → 1. Spec §10 persistence → 11; §11 bindings and events → 12, 13; §9 macOS TCC → 18; §14 testing is distributed across every task; §18 DoD → 18.

**Known soft spots, called out rather than hidden:**

- **Task 8's malgo byte-conversion helpers** are described rather than written, because the exact `unsafe.Slice` idiom depends on the malgo version resolved in Step 1. The step says to follow malgo's own examples. This is the one place the plan defers to upstream rather than specifying.
- **Task 10's manager implementation** is specified as eight numbered rules plus a test suite rather than as finished code. The rules are exact and the tests are complete, so the behaviour is pinned; the assembly is left to the implementer because a 300-line hand-written manager in a plan document would be copied without being understood.
- **Task 4's VOX min-length comparison** (`>` vs `>=`) is deliberately left for the test to settle, with an explicit instruction not to edit the test to match the implementation.
- **Task 7's RNNoise file list** depends on the upstream tree at the commit vendored; the step says to record any adjustment in `VENDOR.md`.

**Type consistency.** `GateConfig`, `Levels`, `Config`, `State`, `VU`, `Sink`, `Backend`, `DeviceInfo` and `Stream` are each defined once and referenced with the same names and fields throughout. The Go DTOs in Task 13 and the TypeScript interfaces in Task 16 mirror each other field-for-field, including the `input_error` / `output_error` snake-case JSON tags.

**Ordering constraint honoured.** Task 1 (CI matrix) precedes Task 7 (RNNoise), per spec D14, and Task 7's Step 6 explicitly re-checks all three platforms at the moment the cgo dependency lands.
