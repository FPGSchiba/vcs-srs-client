# Phase 5 — UDP Voice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the VCS client a working Opus-coded, authenticated, per-frequency-multiplexed UDP voice path against the VCS server, including the radio state it needs to have something to transmit on.

**Architecture:** A new `internal/voice` package owns one connected UDP socket per session: a `Session` runs a HELLO/keepalive/BYE state machine, a TX path accumulates Phase 4's 10 ms frames into 20 ms Opus packets, and an RX path demuxes by `(sender, frequency)` into per-stream jitter buffers whose decoded PCM is summed into a new fourth mixer bus. It attaches to Phase 4's pipeline through exactly one Manager-lifetime `Sink` holding an atomic session pointer.

**Tech Stack:** Go 1.25, cgo (vendored libopus, mirroring the vendored RNNoise), `net.UDPConn`, `github.com/google/uuid`, existing gRPC control plane, React 18 + Zustand frontend.

**Spec:** [`docs/superpowers/specs/2026-09-24-vcs-client-phase-5-udp-voice-design.md`](../specs/2026-09-24-vcs-client-phase-5-udp-voice-design.md) — read §4, §5, §7 and §8 before starting. Decisions D1–D12 there are settled and must not be re-litigated.

## Global Constraints

These apply to **every** task. They are not repeated per task.

- **Every Go invocation carries `-tags purego`.** `go build -tags purego ./...`, `go vet -tags purego ./...`, `go test -tags purego -race ./...`. A command without it is wrong even if it passes.
- **Frontend typecheck is `(cd frontend && npx tsc --noEmit)`.** `npx --prefix frontend tsc --noEmit` silently prints a help banner and exits 0 without checking anything. Never use it.
- **Codec geometry is a wire contract, not a preference:** Opus 48 kHz, 20 ms (960 samples), mono, `OPUS_APPLICATION_AUDIO`, bitrate 48000, FEC off, **DTX off**. The C# peer hard-normalises every decoded frame to exactly 960 samples. Changing any of this breaks interop silently.
- **DTX must never be enabled.** Opus DTX emits 1–2 byte frames; the server drops any voice payload of **5 bytes or fewer** without relaying it.
- **Maximum datagram is 1024 bytes.** The server reads into a fixed 1024-byte buffer and silently truncates. Payload ceiling is `1024 - 27 = 997` bytes.
- **Packet header is exactly 27 bytes**, big-endian throughout: magic `VCS` (0–2), version<<4|type (3), flags (4), 24-bit sequence (5–7), 24-bit frequency in kHz (8–10), 16-byte RFC 4122 UUID (11–26), payload (27+).
- **The voice secret is 43 characters** (`base64.RawURLEncoding` of 32 bytes), placed at HELLO payload `[0:43]` as raw UTF-8. It comes from `ServerSyncResult.voice_secret` — **never** `VoiceHostDetails.secret`, which the server never populates.
- **Frequency canonical form is `KHz uint32`.** Never store a frequency as a float. See Task 3.
- **Nothing on the DSP goroutine may block, allocate, log, or take a contended lock.** `Sink.WriteFrame` and `Session.ReadInto` both run there inside a 10 ms budget.
- **Commit after every task.** Conventional commit format (`feat:`, `fix:`, `test:`, `chore:`, `docs:`). End every commit message with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`
- **Branch:** `feat/phase-5-udp-voice`, already created off `origin/main` with the proto sync (`e6f7820`) and design doc (`7bc49de`) committed.

---

## File Structure

**New packages**

| Path | Responsibility |
|---|---|
| `internal/audio/opus/` | Vendored libopus C sources + 1:1 cgo binding. Leaf package, no project imports. Mirrors `internal/audio/rnnoise/` exactly. |
| `internal/voice/freq.go` | `KHz` type and the only two frequency conversions that exist. |
| `internal/voice/packet.go` | VCSPacket encode/parse. Pure, no I/O, no state. |
| `internal/voice/jitter.go` | Per-stream jitter buffer. Pure data structure, no clock of its own. |
| `internal/voice/endpoint.go` | Endpoint resolution precedence chain. Pure. |
| `internal/voice/session.go` | Socket lifecycle, HELLO/keepalive/BYE state machine, binding-loss recovery. |
| `internal/voice/tx.go` | The `Sink` implementation: accumulator, encoder, per-frequency emission. |
| `internal/voice/rx.go` | Demux, decode loop, per-stream PCM rings, per-stream effects, `ReadInto`. |

**Modified**

| Path | Change |
|---|---|
| `internal/audio/manager.go` | Remove TX-side `effect.Process`; follow-ups #1, #3, #4, #5. |
| `internal/audio/mixer.go` | `Mix` gains a fourth (received-voice) bus. |
| `internal/audio/backend.go` | No change — listed to be explicit that the `Backend` seam is untouched. |
| `internal/config/config.go` | New `[voice]` table and `[[radios]]` array. |
| `internal/control/stream.go` | Real `VOICE_ADDRESS_UPDATE` case. |
| `internal/control/client.go` | `SyncClient` captures `voice_secret` and the voice addresses. |
| `internal/state/store.go` | Hold voice secret + addresses; selected-radio state. |
| `internal/app/audio.go` | PTT dispatch feeds the TX routing set. |
| `internal/app/voice.go` (new) | Session lifecycle wiring and bindings. |
| `main.go` | Register the one voice `Sink`; shutdown ordering. |
| `frontend/src/shared/components/LcdFreq.tsx` | Becomes editable. |
| `frontend/src/windows/comms/CommsApp.tsx` | Render the local client's radios, not `[0]`. |
| `frontend/src/windows/comms/RadioCard.tsx` | Selected-radio affordance, live PTT indicator. |
| `.github/workflows/test.yml` | Build the headless server; run integration tests. |

---

## Task 1: Vendor libopus and its cgo binding

**Files:**
- Create: `internal/audio/opus/` (vendored C sources from upstream)
- Create: `internal/audio/opus/opus.go`
- Create: `internal/audio/opus/opus_stub.go`
- Create: `internal/audio/opus/VENDOR.md`
- Create: `internal/audio/opus/LICENSE`
- Test: `internal/audio/opus/opus_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  package opus

  const (
      SampleRate   = 48000
      Channels     = 1
      FrameSamples = 960 // 20 ms at 48 kHz
      MaxPacket    = 997 // 1024 server read buffer - 27 byte header
  )

  type Application int
  const ApplicationAudio Application = 2049 // OPUS_APPLICATION_AUDIO

  type Encoder struct{ /* unexported */ }
  func NewEncoder(bitrate int) (*Encoder, error)
  func (e *Encoder) Encode(pcm []float32, dst []byte) (int, error)
  func (e *Encoder) Close()

  type Decoder struct{ /* unexported */ }
  func NewDecoder() (*Decoder, error)
  // Decode writes FrameSamples floats into pcm. A nil payload requests
  // packet-loss concealment.
  func (d *Decoder) Decode(payload []byte, pcm []float32) error
  func (d *Decoder) Close()

  // Available reports whether this build has a real codec (false under CGO_ENABLED=0).
  func Available() bool
  ```

**Read first:** `internal/audio/rnnoise/VENDOR.md`. It documents, at length, the exact structural trap this task will otherwise hit: **cgo only auto-compiles `.c` files in the same directory as the Go file containing `import "C"`.** The vendored sources and `opus.go` must be siblings. It also documents why Linux needs `#cgo linux LDFLAGS: -lm`.

- [ ] **Step 1: Vendor the upstream sources**

Fetch libopus 1.5.2 (`https://downloads.xiph.org/releases/opus/opus-1.5.2.tar.gz`). Copy into `internal/audio/opus/`, **flattened** — no subdirectories except `include/`:

- `src/*.c`, `src/*.h` (excluding `opus_demo.c`, `repacketizer_demo.c`, `mlp_train.*`)
- `celt/*.c`, `celt/*.h` (excluding `tests/`, `dump_modes/`)
- `silk/*.c`, `silk/*.h` (excluding `tests/`, `fixed/` — we build the float path)
- `silk/float/*.c`, `silk/float/*.h`
- `include/opus.h`, `include/opus_defines.h`, `include/opus_types.h`, `include/opus_multistream.h`, `include/opus_projection.h`
- `COPYING` → `LICENSE`

Do **not** copy: `autogen.sh`, `configure.ac`, `Makefile.am`, `m4/`, `doc/`, `tests/`, `training/`, `dnn/` (that is the LPCNet/DRED extension — not needed for plain voice, and it drags in a separate model asset exactly as RNNoise's `v0.2` does).

Exclude architecture-specific SIMD directories (`celt/arm/`, `celt/x86/`, `silk/arm/`, `silk/x86/`). The generic C path is selected because our `#cgo CFLAGS` never define `OPUS_ARM_*`, `OPUS_X86_*` or `FIXED_POINT`.

- [ ] **Step 2: Write the failing test**

Create `internal/audio/opus/opus_test.go`:

```go
package opus

import (
	"math"
	"testing"
)

// sine fills buf with a 440 Hz tone so encode/decode has real signal to
// preserve. Silence would pass a round-trip test that a broken codec would
// also pass.
func sine(buf []float32) {
	for i := range buf {
		buf[i] = 0.5 * float32(math.Sin(2*math.Pi*440*float64(i)/float64(SampleRate)))
	}
}

func rms(buf []float32) float64 {
	var sum float64
	for _, s := range buf {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(buf)))
}

func TestEncodeDecodeRoundTripPreservesEnergy(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()

	in := make([]float32, FrameSamples)
	sine(in)
	pkt := make([]byte, MaxPacket)

	// Opus needs a few frames to converge; assert on a later one.
	var n int
	out := make([]float32, FrameSamples)
	for i := 0; i < 10; i++ {
		n, err = enc.Encode(in, pkt)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if err := dec.Decode(pkt[:n], out); err != nil {
			t.Fatalf("Decode: %v", err)
		}
	}

	if n <= 5 {
		t.Fatalf("encoded length %d is <= 5 bytes; the server drops those (DTX must be off)", n)
	}
	if n > MaxPacket {
		t.Fatalf("encoded length %d exceeds MaxPacket %d", n, MaxPacket)
	}
	inRMS, outRMS := rms(in), rms(out)
	if outRMS < inRMS*0.5 || outRMS > inRMS*1.5 {
		t.Fatalf("round-trip RMS %.4f not within 50%% of input %.4f", outRMS, inRMS)
	}
}

func TestDecodeNilPayloadDoesPacketLossConcealment(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	dec, err := NewDecoder()
	if err != nil {
		t.Fatalf("NewDecoder: %v", err)
	}
	defer dec.Close()
	out := make([]float32, FrameSamples)
	if err := dec.Decode(nil, out); err != nil {
		t.Fatalf("PLC decode: %v", err)
	}
}

func TestEncodeRejectsWrongFrameLength(t *testing.T) {
	if !Available() {
		t.Skip("built without cgo")
	}
	enc, err := NewEncoder(48000)
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	defer enc.Close()
	if _, err := enc.Encode(make([]float32, 480), make([]byte, MaxPacket)); err == nil {
		t.Fatal("expected an error for a 480-sample frame; only 960 is valid")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -tags purego ./internal/audio/opus/ -run TestEncodeDecode -v`
Expected: FAIL — build error, `undefined: NewEncoder`.

- [ ] **Step 4: Write the cgo binding**

Create `internal/audio/opus/opus.go`:

```go
//go:build cgo

// Package opus provides thin cgo bindings to the vendored libopus rooted in
// this directory (see VENDOR.md for provenance).
//
// It lives here, rather than one directory up in package audio, for the same
// reason internal/audio/rnnoise does: cgo only auto-compiles .c files that sit
// in the SAME directory as the Go file containing `import "C"`. The vendored
// sources are these files' siblings, so the binding has to be too.
//
// The parameters below are a WIRE CONTRACT with the C# peer, not preferences.
// See the Phase 5 design doc §4: the peer hard-normalises every decoded frame
// to exactly 960 samples, and the server drops any voice payload of 5 bytes or
// fewer -- which is why DTX is never enabled.
package opus

/*
#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/include -O2 -DOPUS_BUILD -DHAVE_LRINTF -DFLOATING_POINT -DUSE_ALLOCA
#cgo linux LDFLAGS: -lm
#include <opus.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

const (
	// SampleRate, Channels and FrameSamples are fixed by the peer contract.
	SampleRate   = 48000
	Channels     = 1
	FrameSamples = 960 // 20 ms at 48 kHz

	// MaxPacket is the largest payload that survives the server's fixed
	// 1024-byte read buffer, which TRUNCATES rather than rejecting.
	MaxPacket = 997
)

// Application mirrors libopus's application constants.
type Application int

// ApplicationAudio matches the C# peer's OpusEncoder.Create(48000, 1, Application.Audio).
const ApplicationAudio Application = C.OPUS_APPLICATION_AUDIO

// Available reports whether this build has a real codec.
func Available() bool { return true }

// Encoder wraps an OpusEncoder configured for the peer contract.
type Encoder struct{ st *C.OpusEncoder }

// NewEncoder allocates an encoder at the given bitrate in bits per second.
// FEC and DTX are both explicitly disabled: DTX would emit 1-2 byte frames
// that the server silently drops, presenting as speech that cuts out only
// during pauses.
func NewEncoder(bitrate int) (*Encoder, error) {
	var cerr C.int
	st := C.opus_encoder_create(C.opus_int32(SampleRate), C.int(Channels),
		C.int(ApplicationAudio), &cerr)
	if cerr != C.OPUS_OK || st == nil {
		return nil, fmt.Errorf("opus: encoder_create failed: %d", int(cerr))
	}
	e := &Encoder{st: st}
	set := func(req C.int, v C.opus_int32) error {
		if r := C.opus_encoder_ctl_set(st, req, v); r != C.OPUS_OK {
			return fmt.Errorf("opus: encoder_ctl %d failed: %d", int(req), int(r))
		}
		return nil
	}
	if err := set(C.OPUS_SET_BITRATE_REQUEST, C.opus_int32(bitrate)); err != nil {
		e.Close()
		return nil, err
	}
	if err := set(C.OPUS_SET_INBAND_FEC_REQUEST, 0); err != nil {
		e.Close()
		return nil, err
	}
	if err := set(C.OPUS_SET_DTX_REQUEST, 0); err != nil {
		e.Close()
		return nil, err
	}
	return e, nil
}

// Encode compresses exactly FrameSamples float32 samples into dst, returning
// the number of bytes written.
func (e *Encoder) Encode(pcm []float32, dst []byte) (int, error) {
	if e == nil || e.st == nil {
		return 0, errors.New("opus: encoder closed")
	}
	if len(pcm) != FrameSamples {
		return 0, fmt.Errorf("opus: frame must be %d samples, got %d", FrameSamples, len(pcm))
	}
	if len(dst) == 0 {
		return 0, errors.New("opus: empty destination")
	}
	n := C.opus_encode_float(e.st,
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(FrameSamples),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.opus_int32(len(dst)))
	if n < 0 {
		return 0, fmt.Errorf("opus: encode failed: %d", int(n))
	}
	return int(n), nil
}

// Close frees the encoder. Safe on a nil or already-closed receiver.
func (e *Encoder) Close() {
	if e != nil && e.st != nil {
		C.opus_encoder_destroy(e.st)
		e.st = nil
	}
}

// Decoder wraps an OpusDecoder at the peer's fixed 48 kHz output rate.
type Decoder struct{ st *C.OpusDecoder }

// NewDecoder allocates a decoder.
func NewDecoder() (*Decoder, error) {
	var cerr C.int
	st := C.opus_decoder_create(C.opus_int32(SampleRate), C.int(Channels), &cerr)
	if cerr != C.OPUS_OK || st == nil {
		return nil, fmt.Errorf("opus: decoder_create failed: %d", int(cerr))
	}
	return &Decoder{st: st}, nil
}

// Decode writes exactly FrameSamples samples into pcm. A nil or empty payload
// requests packet-loss concealment, which is how the jitter buffer fills a gap.
func (d *Decoder) Decode(payload []byte, pcm []float32) error {
	if d == nil || d.st == nil {
		return errors.New("opus: decoder closed")
	}
	if len(pcm) != FrameSamples {
		return fmt.Errorf("opus: output must be %d samples, got %d", FrameSamples, len(pcm))
	}
	var data *C.uchar
	var length C.opus_int32
	fec := C.int(0)
	if len(payload) > 0 {
		data = (*C.uchar)(unsafe.Pointer(&payload[0]))
		length = C.opus_int32(len(payload))
	}
	n := C.opus_decode_float(d.st, data, length,
		(*C.float)(unsafe.Pointer(&pcm[0])), C.int(FrameSamples), fec)
	if n < 0 {
		return fmt.Errorf("opus: decode failed: %d", int(n))
	}
	return nil
}

// Close frees the decoder. Safe on a nil or already-closed receiver.
func (d *Decoder) Close() {
	if d != nil && d.st != nil {
		C.opus_decoder_destroy(d.st)
		d.st = nil
	}
}
```

`opus_encoder_ctl` is variadic in C and cgo cannot call variadic C functions. Add a tiny non-variadic shim as a sibling `.c` file, `internal/audio/opus/ctl_shim.c`:

```c
#include <opus.h>

int opus_encoder_ctl_set(OpusEncoder *st, int request, opus_int32 value) {
    return opus_encoder_ctl(st, request, value);
}
```

and `internal/audio/opus/ctl_shim.h`:

```c
#ifndef VCS_OPUS_CTL_SHIM_H
#define VCS_OPUS_CTL_SHIM_H
#include <opus.h>
int opus_encoder_ctl_set(OpusEncoder *st, int request, opus_int32 value);
#endif
```

Add `#include "ctl_shim.h"` to the cgo preamble in `opus.go`.

- [ ] **Step 5: Write the CGO_ENABLED=0 stub**

Create `internal/audio/opus/opus_stub.go`, mirroring `internal/audio/rnnoise_stub.go`'s discipline — a `CGO_ENABLED=0` release build must still link and run, just without voice:

```go
//go:build !cgo

package opus

import "errors"

const (
	SampleRate   = 48000
	Channels     = 1
	FrameSamples = 960
	MaxPacket    = 997
)

type Application int

const ApplicationAudio Application = 2049

var errNoCGO = errors.New("opus: built without cgo; voice is unavailable")

// Available reports whether this build has a real codec.
func Available() bool { return false }

type Encoder struct{}

func NewEncoder(int) (*Encoder, error)              { return nil, errNoCGO }
func (*Encoder) Encode([]float32, []byte) (int, error) { return 0, errNoCGO }
func (*Encoder) Close()                             {}

type Decoder struct{}

func NewDecoder() (*Decoder, error)          { return nil, errNoCGO }
func (*Decoder) Decode([]byte, []float32) error { return errNoCGO }
func (*Decoder) Close()                      {}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test -tags purego ./internal/audio/opus/ -v`
Expected: PASS, 3 tests.

Then verify the stub build links:
Run: `CGO_ENABLED=0 go build -tags purego ./internal/audio/opus/`
Expected: no output, exit 0.

- [ ] **Step 7: Write VENDOR.md**

Create `internal/audio/opus/VENDOR.md` following `internal/audio/rnnoise/VENDOR.md`'s structure exactly: upstream URL, vendored version and commit, vendoring date, license, why vendored rather than depended upon (no system library or pkg-config on any of the three platforms; the alternatives either require `libopus-dev` everywhere or pin a 2015-era fork), the exact file list copied, the list deliberately not copied **with reasons** (especially `dnn/` and the SIMD directories), the required `#cgo CFLAGS` and why each define is present, the `#cgo linux LDFLAGS: -lm` requirement and why only Linux needs it, and the `ctl_shim.c` rationale (cgo cannot call variadic C functions).

- [ ] **Step 8: Verify the whole tree still builds**

Run: `go build -tags purego ./... && go vet -tags purego ./...`
Expected: no output, exit 0.

- [ ] **Step 9: Commit**

```bash
git add internal/audio/opus/
git commit -m "feat(audio): vendor libopus with a thin cgo binding

48 kHz / 20 ms / mono / Application.Audio / 48 kbps, FEC and DTX both
explicitly off -- a wire contract with the C# peer, which hard-normalises
every decoded frame to exactly 960 samples, and with the server, which
silently drops voice payloads of 5 bytes or fewer.

Vendored rather than depended upon, mirroring internal/audio/rnnoise: no
system library or pkg-config on any of the three target platforms. The
binding is a sibling of the C sources because cgo only auto-compiles .c
files in the same directory as the file doing import \"C\".

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Phase 4 follow-ups

**Files:**
- Modify: `internal/audio/manager.go` (comment at ~614; `emitStateIfChanged` at 381; `closeSupersededStreams` at ~1024; `dspLoop` ticker at ~795)
- Modify: `main_wiring_test.go`
- Test: `internal/audio/manager_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Manager.emitStateIfChanged(epoch uint64)` (signature change, internal); new unexported field `inputTargetID`, `outputTargetID string` on `Manager`.

These land **before** the voice work because every one of them is in a file or a shutdown path the voice session will depend on. Read the spec's §10 table for why each is here.

- [ ] **Step 1: Fix #1, the false safety comment**

In `internal/audio/manager.go`, the block ending at ~line 614 claims a use-after-free hazard was *removed* by not closing a backend the Manager does not own. It was **relocated**, not removed. Replace the final paragraph of that comment with:

```go
//     Not closing a resource we do not own removes the hazard FROM THIS
//     FUNCTION. It does not remove it from the process. main.go still does
//     `defer backend.Close()` before `defer am.Stop()`, so the backend is
//     closed AFTER Stop() returns -- and Stop()'s joins below are BOUNDED,
//     so they can return while dspLoop is still running. A slow
//     Sink.WriteFrame is exactly what makes that happen, and Phase 5
//     registers the first Sink that can be slow (it writes to a socket).
//     The ordering in main.go is therefore load-bearing and is pinned by
//     TestMainWiringClosesBackendAfterManagerStop. If you reorder those
//     defers, an abandoned dspLoop can touch a freed malgo context.
```

- [ ] **Step 2: Write the failing test for #2, defer ordering**

The existing `main_wiring_test.go` asserts nothing about ordering — swapping the two defers compiles and passes. Add to `main_wiring_test.go`:

```go
// TestMainWiringClosesBackendAfterManagerStop pins the shutdown ordering
// main.go depends on. Stop()'s joins are bounded, so dspLoop can still be
// running when Stop() returns; closing the backend before Stop() would let
// an abandoned dspLoop touch a freed malgo context. Phase 5's socket-owning
// sink is what makes a slow WriteFrame -- and therefore an abandoned
// dspLoop -- reachable in practice.
//
// This reads main.go's source rather than executing it, because the
// ordering being asserted is the order of two `defer` statements inside
// func main(), which no test can observe at runtime without launching the
// real GUI.
func TestMainWiringClosesBackendAfterManagerStop(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)
	stopIdx := strings.Index(text, "defer am.Stop()")
	closeIdx := strings.Index(text, "defer backend.Close()")
	if stopIdx < 0 {
		t.Fatal("main.go no longer contains `defer am.Stop()`; update this test deliberately, not reflexively")
	}
	if closeIdx < 0 {
		t.Fatal("main.go no longer contains `defer backend.Close()`; update this test deliberately, not reflexively")
	}
	// defers run LIFO, so the one registered FIRST runs LAST.
	// backend.Close() must run last, so it must be registered first.
	if closeIdx > stopIdx {
		t.Fatalf("`defer backend.Close()` (offset %d) must be registered BEFORE `defer am.Stop()` (offset %d) so it runs after it; see Stop()'s doc on bounded joins", closeIdx, stopIdx)
	}
}
```

- [ ] **Step 3: Run it to verify it passes against correct ordering, then fails when swapped**

Run: `go test -tags purego -race -run TestMainWiringClosesBackendAfterManagerStop . -v`
Expected: PASS.

Now **mutation-verify it**: swap the two `defer` lines in `main.go`, re-run, observe FAIL, then restore. A test that passes against broken code is not evidence.

- [ ] **Step 4: Write the failing test for #3, the missing epoch check**

Add to `internal/audio/manager_test.go`:

```go
// TestEmitStateIfChangedHonoursEpoch pins that an abandoned generation
// cannot publish a stale state over the current one. pollOnce can outlive
// the generation that launched it (Stop()'s bounded joins), and without an
// epoch check its emitState would overwrite m.lastState and fire OnState
// with a snapshot belonging to a dead generation.
func TestEmitStateIfChangedHonoursEpoch(t *testing.T) {
	var mu sync.Mutex
	var seen []State
	m := NewManager(NewFakeBackend(), ManagerOptions{
		OnState: func(st State) {
			mu.Lock()
			seen = append(seen, st)
			mu.Unlock()
		},
		VUInterval: time.Hour,
	})

	m.mu.Lock()
	current := m.epoch
	m.mu.Unlock()

	// An emit tagged with a stale epoch must be discarded entirely.
	m.emitStateIfChanged(current - 1)

	mu.Lock()
	n := len(seen)
	mu.Unlock()
	if n != 0 {
		t.Fatalf("stale-epoch emit published %d state(s); expected 0", n)
	}

	// The current epoch still emits.
	m.emitStateIfChanged(current)
	mu.Lock()
	n = len(seen)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("current-epoch emit published %d state(s); expected 1", n)
	}
}
```

- [ ] **Step 5: Run it to verify it fails**

Run: `go test -tags purego -race ./internal/audio/ -run TestEmitStateIfChangedHonoursEpoch -v`
Expected: FAIL — compile error, `too many arguments in call to m.emitStateIfChanged`.

- [ ] **Step 6: Implement #3**

Change `emitStateIfChanged` (line 381) to take the epoch and check it inside the same critical section that writes `lastState`:

```go
// emitStateIfChanged publishes a state snapshot, suppressing an unchanged
// one. epoch identifies the generation the caller belongs to: an abandoned
// generation (Stop()'s bounded joins) must not publish over the current
// one, so a stale epoch discards the emit entirely rather than racing
// m.lastState. This mirrors the epoch discipline at every other Manager
// field write-back site in this file.
func (m *Manager) emitStateIfChanged(epoch uint64) {
	if m.opts.OnState == nil {
		return
	}
	st := m.State()
	m.mu.Lock()
	if m.epoch != epoch {
		m.mu.Unlock()
		return
	}
	unchanged := m.lastState != nil && *m.lastState == st
	if !unchanged {
		s := st
		m.lastState = &s
	}
	m.mu.Unlock()
	if !unchanged {
		m.opts.OnState(st)
	}
}
```

Update every call site to pass its epoch. In `pollOnce` (~line 989) the call becomes `m.emitStateIfChanged(epoch)`. For call sites that are not epoch-scoped (e.g. `Start`'s tail), read the current epoch under `m.mu` immediately before the call and pass that.

- [ ] **Step 7: Run to verify it passes**

Run: `go test -tags purego -race ./internal/audio/ -run TestEmitStateIfChangedHonoursEpoch -v`
Expected: PASS.

- [ ] **Step 8: Write the failing test for #4, the backoff reset branch**

The reset at ~line 1031 lives inside `if m.captureStream != nil`. The case that needs it is the opposite one: the stream is **nil** because opening failed and the direction is in backoff, and the user then picks a *different* device. Nothing resets the backoff, so the new selection waits up to 30 s.

Add to `internal/audio/manager_test.go`:

```go
// TestDeviceChangeDuringBackoffIsNotDelayed pins that selecting a different
// device clears a backoff accumulated against the PREVIOUS device. The
// reset used to live only in the branch that requires an open stream, so
// the one case that needed it -- no stream, because opening kept failing --
// was the one case it never ran for, stranding the user's new selection
// behind up to 30 seconds of backoff for a device they are no longer asking
// for.
func TestDeviceChangeDuringBackoffIsNotDelayed(t *testing.T) {
	be := NewFakeBackend()
	be.SetInputs([]DeviceInfo{{ID: "bad", Name: "Bad"}, {ID: "good", Name: "Good", IsDefault: true}})
	be.FailCaptureFor("bad", errors.New("device is wedged"))

	m := NewManager(be, ManagerOptions{VUInterval: time.Hour})
	m.SetConfig(Config{InputDevice: "bad"})
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	// Drive polls until a backoff has accumulated against "bad".
	for i := 0; i < 3; i++ {
		m.pollOnceForTest()
	}
	m.mu.Lock()
	backoff := m.inputBackoff
	m.mu.Unlock()
	if backoff == 0 {
		t.Fatal("expected a non-zero input backoff after repeated open failures")
	}

	// The user picks a device that works.
	m.SetConfig(Config{InputDevice: "good"})
	m.pollOnceForTest()

	m.mu.Lock()
	stream, id, resetBackoff := m.captureStream, m.inputID, m.inputBackoff
	m.mu.Unlock()
	if stream == nil {
		t.Fatalf("capture did not open on the newly selected device; backoff is still %v", resetBackoff)
	}
	if id != "good" {
		t.Fatalf("opened device %q, want \"good\"", id)
	}
}
```

If `FailCaptureFor` and `pollOnceForTest` do not exist on the fake backend / Manager, add them — `FailCaptureFor(id string, err error)` in `backend_fake.go`, and `pollOnceForTest()` as a thin test-only wrapper in a `_test.go` file that calls `pollOnce` with the current epoch and enumeration.

- [ ] **Step 9: Run it to verify it fails**

Run: `go test -tags purego -race ./internal/audio/ -run TestDeviceChangeDuringBackoffIsNotDelayed -v`
Expected: FAIL — capture does not open, because the backoff from "bad" is still in force.

- [ ] **Step 10: Implement #4**

Add two fields to `Manager`:

```go
	// inputTargetID/outputTargetID record the device id the LAST open
	// ATTEMPT was made against, as opposed to inputID/outputID which record
	// the device currently open. They exist so a backoff accumulated against
	// one device can be recognised as irrelevant when the user selects a
	// different one -- the case where no stream is open, which is precisely
	// when a backoff is in force and precisely when the old reset (which
	// required an open stream) never ran.
	inputTargetID, outputTargetID string
```

In `maybeReopenCapture`, record the attempt before calling the backend, inside the existing `m.mu` section that computes `ready`... and in `closeSupersededStreams`, extend the reset to the nil-stream case:

```go
	wantIn := resolveDevice(cfg.InputDevice, inputs)
	if m.captureStream != nil && wantIn != m.inputID {
		capStream = m.captureStream
		m.captureStream = nil
		m.inputBackoff, m.inputRetryAt = 0, time.Time{}
	} else if m.captureStream == nil && m.inputTargetID != "" && wantIn != m.inputTargetID {
		// No stream to close, but the target moved: the accumulated backoff
		// belongs to a device the user is no longer asking for.
		m.inputBackoff, m.inputRetryAt = 0, time.Time{}
	}
```

Mirror it for playback with `wantOut` / `m.outputTargetID`.

- [ ] **Step 11: Run to verify it passes**

Run: `go test -tags purego -race ./internal/audio/ -run TestDeviceChangeDuringBackoffIsNotDelayed -v`
Expected: PASS.

- [ ] **Step 12: Fix #5, the DSP loop latency ratchet**

`dspLoop` is `time.Ticker`-driven and reads exactly one frame per tick, so a dropped tick permanently adds a frame of backlog; latency ratchets until `Drain()` discards ~160 ms in one audible jump. This shapes our TX cadence directly — a stalled loop makes us bursty for every receiver's jitter buffer.

Replace the one-frame-per-tick read with a catch-up read bounded by the ring's available depth. In `dspLoop`, after the existing `Dropped()`/`Drain()` block, the loop currently does a single `captureRing.Read(inFrame)`. Change it to process every whole frame the ring currently holds, up to a bounded catch-up limit, feeding each through the chain:

```go
	// maxCatchUpFrames bounds how much backlog one tick may absorb. A
	// time.Ticker coalesces missed ticks into one, so after a scheduling
	// stall the ring holds several frames and a strict one-frame-per-tick
	// read can NEVER recover -- the backlog is permanent latency until
	// Drain() dumps it in one audible ~160 ms jump. Draining a bounded
	// number of extra frames per tick lets latency decay smoothly instead.
	// The bound exists so a pathological stall cannot turn one tick into an
	// unbounded burst of encode work on this goroutine.
	const maxCatchUpFrames = 3
```

and drive the per-frame chain body (NS → AGC → gate → sinks → monitor) in an inner loop over `frames := 1 + min(captureRing.Available()/FrameSamples, maxCatchUpFrames)` iterations, while the playback side (`mixer.Mix` / `playbackRing.Write`) still runs **exactly once** per tick — playback is paced by the output device, not by capture backlog.

Add a test in `internal/audio/manager_test.go` that writes 4 frames into the capture ring, runs a single tick, and asserts the sink saw more than one frame:

```go
// TestDSPLoopCatchesUpAfterAStall pins that a capture backlog decays instead
// of becoming permanent latency. A time.Ticker coalesces missed ticks, so
// one-frame-per-tick could never drain a backlog it did not cause.
func TestDSPLoopCatchesUpAfterAStall(t *testing.T) {
	// ... build a Manager on the fake backend with a counting Sink,
	// PTT held so the gate is open, write 4 frames into the capture ring,
	// advance one tick, and assert the sink received >1 frame and <=4.
}
```

- [ ] **Step 13: Run the full audio suite**

Run: `go test -tags purego -race ./internal/audio/... -v`
Expected: PASS, no regressions.

- [ ] **Step 14: Commit**

```bash
git add internal/audio/manager.go internal/audio/manager_test.go internal/audio/backend_fake.go main_wiring_test.go
git commit -m "fix(audio): close the five accepted Phase 4 follow-ups

Landed before the voice work because every one is in a file or a shutdown
path the voice session depends on.

1. manager.go's Stop() comment claimed a use-after-free hazard was removed.
   It was RELOCATED: main.go closes the backend after Stop() returns, and
   Stop()'s joins are bounded, so a slow sink can leave dspLoop running.
   Phase 5 registers the first sink that can be slow.
2. main_wiring_test.go asserted nothing about defer ordering -- swapping the
   two defers compiled and passed. Now pinned, and mutation-verified.
3. emitStateIfChanged had no epoch check, so an abandoned generation could
   publish a stale state over the current one.
4. The backoff reset lived only in the branch requiring an open stream, so
   the one case needing it -- no stream, because opening kept failing --
   never ran it. A device change could wait up to 30 s.
5. dspLoop read one frame per tick, so a coalesced tick left permanent
   latency until Drain() dumped ~160 ms at once. Bounded catch-up lets it
   decay smoothly, which matters because it shapes TX packet cadence.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Frequency canonicalisation

**Files:**
- Create: `internal/voice/freq.go`
- Test: `internal/voice/freq_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  package voice

  type KHz uint32
  func KHzFromMHz32(f float32) KHz
  func (k KHz) MHz32() float32
  func (k KHz) Valid() bool // fits in 24 bits
  ```

Read spec §5 first. Three components compare frequencies three different ways; this file is how we satisfy all three at once.

- [ ] **Step 1: Write the failing test**

Create `internal/voice/freq_test.go`:

```go
package voice

import "testing"

// TestMHz32MatchesServerComputation is the load-bearing test of this file.
//
// The server decides whether to relay a packet with an EXACT float32
// equality: `radio.Frequency == float32(pkt.Frequency)/1000.0`, comparing
// the float32 MHz we advertised via UpdateRadioInfo against the float32 it
// computes from the 24-bit kHz on the wire. If those two float32 values
// differ by one bit, the packet is silently not relayed and no component
// logs anything.
//
// We make them equal BY CONSTRUCTION: MHz32 evaluates the identical
// expression the server evaluates. This test pins that, and would catch a
// future "optimisation" to float64 intermediates.
func TestMHz32MatchesServerComputation(t *testing.T) {
	for _, khz := range []KHz{
		118500, 251300, 243000, 305750, 127125, 140625,
		121500, 156800, 100001, 399999, 1, 16777215,
	} {
		advertised := khz.MHz32()               // what we send in UpdateRadioInfo
		server := float32(uint32(khz)) / 1000.0 // what the server computes from the packet
		if advertised != server {
			t.Errorf("kHz %d: advertised %v != server-computed %v", khz, advertised, server)
		}
	}
}

func TestKHzFromMHz32RoundTrips(t *testing.T) {
	for _, khz := range []KHz{118500, 251300, 243000, 305750, 127125, 100001, 399999} {
		if got := KHzFromMHz32(khz.MHz32()); got != khz {
			t.Errorf("round trip of %d kHz gave %d", khz, got)
		}
	}
}

// TestKHzFromMHz32Rounds pins that we ROUND where the C# peer truncates
// (VcsVoicePacket.SetFrequencyHz does `(uint)(freqHz / 1000.0)`). On the kHz
// grid the two agree; off-grid, rounding is the only rule that round-trips,
// so a float32 that is a hair under its intended kHz must not lose a kHz.
func TestKHzFromMHz32Rounds(t *testing.T) {
	if got := KHzFromMHz32(251.2999); got != 251300 {
		t.Errorf("251.2999 MHz gave %d kHz, want 251300 (must round, not truncate)", got)
	}
	if got := KHzFromMHz32(251.3001); got != 251300 {
		t.Errorf("251.3001 MHz gave %d kHz, want 251300", got)
	}
}

func TestValidRejectsAbove24Bits(t *testing.T) {
	if !KHz(16777215).Valid() {
		t.Error("16777215 kHz is the largest 24-bit value and must be valid")
	}
	if KHz(16777216).Valid() {
		t.Error("16777216 kHz does not fit in 24 bits and must be invalid")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/voice/ -v`
Expected: FAIL — no such package / undefined `KHz`.

- [ ] **Step 3: Implement**

Create `internal/voice/freq.go`:

```go
// Package voice implements the client's UDP voice path: the VCS wire
// protocol, Opus transport, and the session that carries them.
package voice

import "math"

// KHz is a frequency in whole kilohertz and is the client's ONLY canonical
// frequency representation. Nothing in this client stores a frequency as a
// float.
//
// Three components compare frequencies three different ways:
//
//   - the Go server, deciding whether to relay, uses EXACT float32 equality
//     between the radio frequency we advertised and float32(kHz)/1000.0
//   - the C# peer's receive filter uses a 1 Hz tolerance in Hz
//   - the wire carries 24-bit unsigned kHz, big-endian
//
// Holding the integer and deriving every other form from it satisfies all
// three at once, and in particular makes the server's exact-equality match
// true by construction rather than by luck. See the Phase 5 design doc §5.
type KHz uint32

// maxKHz is the largest value the 24-bit wire field can carry.
const maxKHz = 1<<24 - 1

// KHzFromMHz32 converts a float32 MHz frequency -- the form the server sends
// in RadioInfo -- to canonical kHz.
//
// It ROUNDS where the C# peer's SetFrequencyHz truncates. On the kHz grid the
// two agree; off the grid, rounding is the only rule that round-trips, so a
// float32 sitting a hair below its intended kHz does not lose a whole kHz.
func KHzFromMHz32(f float32) KHz {
	if f <= 0 {
		return 0
	}
	return KHz(math.Round(float64(f) * 1000))
}

// MHz32 returns the float32 MHz value to advertise in UpdateRadioInfo.
//
// The expression is deliberately IDENTICAL to the one voice/server.go
// evaluates on a received packet (`float32(pkt.Frequency) / 1000.0`). Do not
// "improve" it into float64 intermediates: the server compares the two
// results with ==, and a one-bit difference silently stops relaying with no
// error anywhere.
func (k KHz) MHz32() float32 {
	return float32(uint32(k)) / 1000.0
}

// Valid reports whether k fits in the 24-bit wire field.
func (k KHz) Valid() bool { return uint32(k) <= maxKHz }
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags purego ./internal/voice/ -v`
Expected: PASS, 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/voice/freq.go internal/voice/freq_test.go
git commit -m "feat(voice): add KHz, the client's only canonical frequency form

MHz32 evaluates the identical expression voice/server.go evaluates on a
received packet, so the server's exact-float32 relay match holds by
construction. Rounds where the C# peer truncates, which agrees on the kHz
grid and round-trips off it.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: VCS packet codec

**Files:**
- Create: `internal/voice/packet.go`
- Test: `internal/voice/packet_test.go`
- Modify: `go.mod`, `go.sum` (add `github.com/google/uuid`)

**Interfaces:**
- Consumes: `KHz` (Task 3).
- Produces:
  ```go
  const (
      HeaderSize      = 27
      MagicVCS        = "VCS"
      ProtocolVersion = 1
      VoiceSecretLen  = 43
      MaxDatagram     = 1024
      MinVoicePayload = 6 // server relays only len(payload) > 5
  )

  type PacketType uint8
  const (
      PacketTypeVoice PacketType = iota
      PacketTypeHello
      PacketTypeHelloAck
      PacketTypeKeepalive
      PacketTypeBye
  )
  func (t PacketType) String() string

  type Packet struct {
      Type      PacketType
      Flags     uint8
      Sequence  uint32 // 24-bit
      Frequency KHz
      SenderID  uuid.UUID
      Payload   []byte
  }
  func (p *Packet) PTT() bool
  func (p *Packet) SetPTT(bool)
  func (p *Packet) Intercom() bool
  func (p *Packet) SetIntercom(bool)
  func (p *Packet) AppendTo(dst []byte) []byte
  func Parse(data []byte) (*Packet, error)

  func NewHello(sender uuid.UUID, secret string) *Packet
  func NewVoice(sender uuid.UUID, seq uint32, freq KHz, opus []byte, ptt, intercom bool) *Packet
  func NewKeepalive(sender uuid.UUID, echo int64) *Packet
  func NewBye(sender uuid.UUID) *Packet

  func KeepaliveTimestamp(payload []byte) int64
  ```

- [ ] **Step 1: Add the uuid dependency**

Run: `go get github.com/google/uuid@v1.6.0`
This is the version the server uses, so both ends parse identically. Approved in the spec's §14.

- [ ] **Step 2: Write the failing test**

Create `internal/voice/packet_test.go`. The golden vector is the heart of it — it pins the byte layout against both peers:

```go
package voice

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// goldenSender is a fixed UUID so the golden byte vector below is stable.
var goldenSender = uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")

// TestVoicePacketGoldenBytes pins the exact 27-byte header layout against
// BOTH peers: the Go server's voice/protocol.go SerializePacket and the C#
// VcsVoicePacket.EncodePacket. Every offset here was read from those two
// implementations, not inferred.
//
// The UUID bytes are the RFC 4122 big-endian form. This matters: .NET's
// Guid.ToByteArray() is little-endian in its first three fields, which is
// why the C# side hand-converts in both directions. Go's uuid.UUID is
// already RFC 4122 ordered, so MarshalBinary needs no conversion -- but a
// future refactor that reaches for Guid-style ordering would break interop
// silently, and this vector is what catches it.
func TestVoicePacketGoldenBytes(t *testing.T) {
	p := NewVoice(goldenSender, 0x0102_03, KHz(251300), []byte{0xAA, 0xBB, 0xCC}, true, false)
	got := p.AppendTo(nil)

	want := []byte{
		'V', 'C', 'S', // 0-2   magic
		0x10,                   // 3     version 1 << 4 | type 0 (VOICE)
		0x01,                   // 4     flags: PTT
		0x01, 0x02, 0x03,       // 5-7   sequence 0x010203
		0x03, 0xD5, 0x64,       // 8-10  frequency 251300 kHz
		0x01, 0x23, 0x45, 0x67, // 11-26 sender uuid, RFC 4122 order
		0x89, 0xab,
		0xcd, 0xef,
		0x01, 0x23,
		0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0xAA, 0xBB, 0xCC, // 27+   payload
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch\n got %x\nwant %x", got, want)
	}
	if len(got) != HeaderSize+3 {
		t.Fatalf("length %d, want %d", len(got), HeaderSize+3)
	}
}

func TestParseRoundTrip(t *testing.T) {
	orig := NewVoice(goldenSender, 0xFFFFFF, KHz(16777215), bytes.Repeat([]byte{0x7F}, 100), true, true)
	got, err := Parse(orig.AppendTo(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != PacketTypeVoice {
		t.Errorf("Type = %v", got.Type)
	}
	if got.Sequence != 0xFFFFFF {
		t.Errorf("Sequence = %#x, want 0xffffff", got.Sequence)
	}
	if got.Frequency != KHz(16777215) {
		t.Errorf("Frequency = %d", got.Frequency)
	}
	if got.SenderID != goldenSender {
		t.Errorf("SenderID = %v", got.SenderID)
	}
	if !got.PTT() || !got.Intercom() {
		t.Errorf("flags lost: ptt=%v intercom=%v", got.PTT(), got.Intercom())
	}
	if !bytes.Equal(got.Payload, orig.Payload) {
		t.Error("payload mismatch")
	}
}

// TestHelloCarriesSecretAtOffsetZero pins the one thing that decides whether
// the server accepts us at all. The secret is 43 raw UTF-8 bytes at payload
// [0:43]; the server reads exactly that slice and constant-time-compares it.
func TestHelloCarriesSecretAtOffsetZero(t *testing.T) {
	secret := strings.Repeat("A", VoiceSecretLen)
	p := NewHello(goldenSender, secret)
	if len(p.Payload) < VoiceSecretLen {
		t.Fatalf("HELLO payload is %d bytes, server requires at least %d", len(p.Payload), VoiceSecretLen)
	}
	if string(p.Payload[:VoiceSecretLen]) != secret {
		t.Fatalf("secret not at payload[0:%d]", VoiceSecretLen)
	}
	if p.Type != PacketTypeHello {
		t.Fatalf("Type = %v, want HELLO", p.Type)
	}
}

// TestKeepaliveEchoRoundTrip pins the 8-byte big-endian echo. This is the
// only input to the server's per-client latency map, so an empty or
// wrongly-ordered payload silently degrades server-side telemetry.
func TestKeepaliveEchoRoundTrip(t *testing.T) {
	const ts int64 = 1_700_000_000_123
	p := NewKeepalive(goldenSender, ts)
	if len(p.Payload) != 8 {
		t.Fatalf("payload is %d bytes, want 8", len(p.Payload))
	}
	if got := int64(binary.BigEndian.Uint64(p.Payload)); got != ts {
		t.Fatalf("echo = %d, want %d", got, ts)
	}
	if got := KeepaliveTimestamp(p.Payload); got != ts {
		t.Fatalf("KeepaliveTimestamp = %d, want %d", got, ts)
	}
	// Before the first server reply there is nothing to echo.
	if n := len(NewKeepalive(goldenSender, 0).Payload); n != 0 {
		t.Fatalf("zero echo produced a %d-byte payload, want empty", n)
	}
	if got := KeepaliveTimestamp(nil); got != 0 {
		t.Fatalf("KeepaliveTimestamp(nil) = %d, want 0", got)
	}
}

func TestParseRejects(t *testing.T) {
	good := NewBye(goldenSender).AppendTo(nil)

	t.Run("short", func(t *testing.T) {
		if _, err := Parse(good[:HeaderSize-1]); err == nil {
			t.Fatal("expected an error for a short packet")
		}
	})
	t.Run("bad magic", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[0] = 'X'
		if _, err := Parse(bad); err == nil {
			t.Fatal("expected an error for bad magic")
		}
	})
	t.Run("bad version", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[3] = (9 << 4) | byte(PacketTypeBye)
		if _, err := Parse(bad); err == nil {
			t.Fatal("expected an error for an unsupported version")
		}
	})
}

// TestSequenceIsMaskedTo24Bits pins that a counter wrapping past 2^24 cannot
// corrupt the frequency field that sits immediately after it.
func TestSequenceIsMaskedTo24Bits(t *testing.T) {
	p := NewVoice(goldenSender, 0x01_FF_FF_FF, KHz(251300), []byte{1, 2, 3, 4, 5, 6}, true, false)
	raw := p.AppendTo(nil)
	if raw[5] != 0xFF || raw[6] != 0xFF || raw[7] != 0xFF {
		t.Fatalf("sequence bytes %x %x %x, want ff ff ff", raw[5], raw[6], raw[7])
	}
	if raw[8] != 0x03 || raw[9] != 0xD5 || raw[10] != 0x64 {
		t.Fatalf("frequency corrupted by sequence overflow: %x %x %x", raw[8], raw[9], raw[10])
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test -tags purego ./internal/voice/ -run 'TestVoicePacket|TestParse|TestHello|TestKeepalive|TestSequence' -v`
Expected: FAIL — undefined `NewVoice`, `Parse`, etc.

- [ ] **Step 4: Implement `internal/voice/packet.go`**

Write the full implementation matching the Interfaces block. Requirements that the tests pin, restated so they are not lost:

- `AppendTo` appends to `dst` and returns the grown slice, so the TX path can reuse one buffer and never allocate per packet.
- Sequence and frequency are masked to 24 bits on write.
- `Parse` validates length ≥ `HeaderSize`, magic == `"VCS"`, version == `ProtocolVersion`, and returns a typed error for each.
- `Parse` **copies** the payload out of the caller's buffer. The RX loop reuses its read buffer; a shared slice would be overwritten by the next datagram while the jitter buffer still holds it.
- `NewKeepalive(sender, 0)` produces an **empty** payload — before the first server reply there is nothing to echo, and the server treats a zero timestamp as absent.
- Flags: bit 0 (`0x01`) PTT, bit 1 (`0x02`) intercom.

- [ ] **Step 5: Run to verify it passes**

Run: `go test -tags purego -race ./internal/voice/ -v`
Expected: PASS, all tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/voice/packet.go internal/voice/packet_test.go
git commit -m "feat(voice): add the VCS packet codec

27-byte header pinned by a golden byte vector read from both peers: the Go
server's SerializePacket and the C# VcsVoicePacket.EncodePacket. Covers the
RFC 4122 UUID ordering the C# side hand-converts, the 43-byte HELLO secret
at payload offset 0, the 8-byte big-endian keepalive echo that feeds the
server's latency map, and 24-bit masking so a wrapping sequence counter
cannot corrupt the frequency field beside it.

Adds github.com/google/uuid v1.6.0, the version the server uses, so both
ends parse identically by construction. Approved in the design doc's §14.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Jitter buffer

**Files:**
- Create: `internal/voice/jitter.go`
- Test: `internal/voice/jitter_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  type jitterFrame struct {
      seq     uint32
      payload []byte
      addedAt time.Time
  }

  type jitter struct{ /* unexported */ }

  // newJitter builds a buffer holding `target` of audio before playout starts
  // and discarding oldest beyond `max`.
  func newJitter(target, max time.Duration, frameDur time.Duration) *jitter
  func (j *jitter) push(seq uint32, payload []byte, now time.Time) bool
  // pop returns the next frame to decode. ok=false means "nothing to play
  // yet". lost=true with ok=true means a gap expired: decode nil for PLC.
  func (j *jitter) pop(now time.Time) (payload []byte, lost bool, ok bool)
  func (j *jitter) depth() int
  func (j *jitter) reset()
  ```

This is a pure data structure with **no clock of its own** (D8) — `now` is always passed in, which is what makes every case below testable without sleeping.

- [ ] **Step 1: Write the failing test**

Create `internal/voice/jitter_test.go` covering, each as its own `t.Run`:

- **primes before playing:** push one frame, `pop` at the same instant returns `ok=false`; `pop` after `target` returns it.
- **reorders:** push seq 3 then 1 then 2; after priming, `pop` returns them in order 1, 2, 3.
- **drops duplicates:** push seq 1 twice; `depth()` is 1.
- **conceals a gap:** push seq 1 and 3; after priming, pop yields 1, then `lost=true, ok=true` for the missing 2, then 3.
- **ignores a late arrival:** pop seq 1 and 2, then push seq 1 again; it is refused (`push` returns false) rather than replayed.
- **bounds depth:** with `max` = 500 ms and a 20 ms frame, push 40 frames; `depth()` never exceeds 25 and the OLDEST are the ones discarded.
- **wraps at 2^24:** push seq `0xFFFFFE`, `0xFFFFFF`, `0x000000`, `0x000001`; they pop in that order, not reordered as if `0x000000` preceded `0xFFFFFE`.

Write real assertions with fixed `now` values (`base := time.Unix(0, 0)`, then `base.Add(...)`). No `time.Sleep` anywhere in this file.

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/voice/ -run TestJitter -v`
Expected: FAIL — undefined `newJitter`.

- [ ] **Step 3: Implement**

Key design points to honour:

- Sequence comparison must be **wraparound-aware**: compare `int32(a-b)` sign rather than `a < b`, over the 24-bit space (shift left 8 before comparing, or mask consistently).
- The buffer holds **encoded** payloads; decoding happens in the RX decode loop, not here.
- `target` default 60 ms, `max` default 500 ms — both come from config, defaults defined in Task 8.
- A gap is only concealed once its delay budget expires, so a genuinely reordered packet still has time to arrive.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags purego -race ./internal/voice/ -run TestJitter -v`
Expected: PASS, 7 subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/voice/jitter.go internal/voice/jitter_test.go
git commit -m "feat(voice): add the per-stream jitter buffer

Holds encoded payloads ordered by 24-bit sequence with wraparound-aware
comparison, primes before playout, conceals expired gaps by yielding a
lost marker the decoder turns into Opus PLC, refuses late arrivals, and
bounds depth by discarding oldest.

Takes `now` as a parameter and owns no clock of its own -- dspLoop's tick
is the only clock on the RX path (design doc D8) -- which also makes every
reorder, loss and wraparound case testable without sleeping.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Endpoint resolution

**Files:**
- Create: `internal/voice/endpoint.go`
- Test: `internal/voice/endpoint_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  // Sources carries every place a voice address can come from, in no
  // particular order; Resolve applies the precedence.
  type Sources struct {
      Update     string // VoiceAddressUpdate.coalition_voice_addr (live)
      Sync       string // ServerSyncResult.coalition_voice_addr
      ConfigHost string // [voice] host
      ConfigPort int    // [voice] port
      ServerURL  string // [server_url], for host derivation
  }

  const DefaultVoicePort = 5002

  // Resolve returns a dialable host:port.
  func Resolve(s Sources) (string, error)
  ```

Read spec §6. The non-obvious fact this encodes: **standalone servers return empty strings for both `Sync` and `Update`**, because those are served from a registry only distributed voice nodes populate. The fallback is not a nicety.

- [ ] **Step 1: Write the failing test**

Create `internal/voice/endpoint_test.go`:

```go
package voice

import "testing"

func TestResolvePrecedence(t *testing.T) {
	full := Sources{
		Update:     "10.0.0.1:6000",
		Sync:       "10.0.0.2:6001",
		ConfigHost: "10.0.0.3",
		ConfigPort: 6002,
		ServerURL:  "vcs.example.com:14447",
	}

	tests := []struct {
		name string
		mut  func(*Sources)
		want string
	}{
		{"update wins", func(s *Sources) {}, "10.0.0.1:6000"},
		{"sync when no update", func(s *Sources) { s.Update = "" }, "10.0.0.2:6001"},
		{"config when no server address", func(s *Sources) { s.Update, s.Sync = "", "" }, "10.0.0.3:6002"},
		{"derived from server_url when nothing else", func(s *Sources) {
			s.Update, s.Sync, s.ConfigHost = "", "", ""
		}, "vcs.example.com:5002"},
		{"config host with no port uses the default", func(s *Sources) {
			s.Update, s.Sync, s.ConfigPort = "", "", 0
		}, "10.0.0.3:5002"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := full
			tc.mut(&s)
			got, err := Resolve(s)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveTreatsEmptyServerAddressesAsAbsent is the case that actually
// happens today. getVoiceAddresses delegates to a registry populated ONLY by
// distributed voice nodes calling RegisterVoiceServer; a standalone server
// never registers itself, so both fields come back as empty strings on every
// standalone deployment. Treating "" as a usable address would make the
// client dial ":0".
func TestResolveTreatsEmptyServerAddressesAsAbsent(t *testing.T) {
	got, err := Resolve(Sources{Update: "", Sync: "", ServerURL: "localhost:14447"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != "localhost:5002" {
		t.Fatalf("Resolve = %q, want localhost:5002", got)
	}
}

func TestResolveStripsSchemeFromServerURL(t *testing.T) {
	for _, in := range []string{"http://vcs.example.com:14447", "vcs.example.com:14447", "vcs.example.com"} {
		got, err := Resolve(Sources{ServerURL: in})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", in, err)
		}
		if got != "vcs.example.com:5002" {
			t.Fatalf("Resolve(%q) = %q, want vcs.example.com:5002", in, got)
		}
	}
}

func TestResolveErrorsWithNothingToGoOn(t *testing.T) {
	if _, err := Resolve(Sources{}); err == nil {
		t.Fatal("expected an error when no source yields a host")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/voice/ -run TestResolve -v`
Expected: FAIL — undefined `Resolve`.

- [ ] **Step 3: Implement `internal/voice/endpoint.go`**

Precedence exactly as the spec's §6 lists it. Strip any `scheme://` prefix and any existing port from `ServerURL` before appending `DefaultVoicePort`. Empty strings are absent at every level.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags purego -race ./internal/voice/ -run TestResolve -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/voice/endpoint.go internal/voice/endpoint_test.go
git commit -m "feat(voice): resolve the voice endpoint by precedence

VoiceAddressUpdate > SyncClient's coalition_voice_addr > [voice] config >
host of server_url on port 5002.

The fallback is load-bearing, not a nicety: getVoiceAddresses serves both
server-supplied addresses from a registry that only distributed voice nodes
populate, so a standalone server -- which is every deployment today --
returns empty strings for both.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Session lifecycle — handshake, keepalive, binding loss

**Files:**
- Create: `internal/voice/session.go`
- Test: `internal/voice/session_test.go`
- Test helper: `internal/voice/testserver_test.go`

**Interfaces:**
- Consumes: `Packet`, `Parse`, `NewHello`, `NewKeepalive`, `NewBye`, `KeepaliveTimestamp` (Task 4); `Resolve`, `Sources` (Task 6).
- Produces:
  ```go
  type State int
  const (
      StateIdle State = iota
      StateResolving
      StateHandshaking
      StateConnected
      StateRebinding
      StateRetrying
      StateClosed
  )
  func (s State) String() string

  type Options struct {
      Log        *slog.Logger
      OnState    func(State, error) // may be called concurrently
      Clock      func() time.Time   // nil means time.Now
      Keepalive  time.Duration      // 0 means 5s
      JitterMS   int                // 0 means 60
  }

  type Session struct{ /* unexported */ }

  // Dial resolves the endpoint, opens a connected UDP socket and runs the
  // HELLO handshake in the background. It returns as soon as the socket is
  // open; handshake progress arrives through OnState.
  func Dial(src Sources, self uuid.UUID, secret string, opt Options) (*Session, error)
  func (s *Session) State() State
  func (s *Session) RTT() time.Duration
  func (s *Session) Close() error
  ```

**On the test helper.** D11 rules out a hand-written fake as the *integration* story, because it can only encode our own reading of the protocol. It is still the right fixture for **state-machine timing** — the retry ladder and the unanswered-keepalive counter cannot be provoked against a real server without sleeping for minutes. `testserver_test.go` therefore implements only what those tests need, and carries a comment saying exactly that: it is not a protocol authority, and Task 12's real-server tests are.

- [ ] **Step 1: Write the test UDP responder**

Create `internal/voice/testserver_test.go`:

```go
package voice

import (
	"net"
	"sync"
	"testing"
	"time"
)

// testServer is a minimal UDP responder for SESSION STATE-MACHINE tests
// only: the retry ladder, the unanswered-keepalive counter, and re-HELLO
// recovery, none of which can be provoked against a real server without
// sleeping for minutes.
//
// It is deliberately NOT a protocol authority. It encodes our own reading of
// the server, so it can never catch a MISREADING of it -- which is exactly
// why the design doc (D11) puts the real headless server behind the
// integration tests in Task 12. Do not grow this into a second server
// implementation; if you find yourself wanting to, write an integration test
// instead.
type testServer struct {
	conn *net.UDPConn

	mu        sync.Mutex
	bound     map[string]bool // source addr -> bound
	acceptSec string          // the only secret it accepts
	dropHello bool            // refuse to ACK, to drive the retry ladder
	dropKeep  bool            // ignore keepalives, to drive binding-loss detection
	helloes   int
	keepalive int
}

func newTestServer(t *testing.T, secret string) *testServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ts := &testServer{conn: conn, bound: map[string]bool{}, acceptSec: secret}
	go ts.loop()
	t.Cleanup(func() { conn.Close() })
	return ts
}

func (ts *testServer) addr() string { return ts.conn.LocalAddr().String() }

func (ts *testServer) setDropHello(v bool) { ts.mu.Lock(); ts.dropHello = v; ts.mu.Unlock() }
func (ts *testServer) setDropKeepalive(v bool) { ts.mu.Lock(); ts.dropKeep = v; ts.mu.Unlock() }
func (ts *testServer) helloCount() int { ts.mu.Lock(); defer ts.mu.Unlock(); return ts.helloes }

func (ts *testServer) loop() {
	buf := make([]byte, MaxDatagram)
	for {
		n, from, err := ts.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		pkt, err := Parse(buf[:n])
		if err != nil {
			continue
		}
		ts.mu.Lock()
		switch pkt.Type {
		case PacketTypeHello:
			ts.helloes++
			ok := len(pkt.Payload) >= VoiceSecretLen &&
				string(pkt.Payload[:VoiceSecretLen]) == ts.acceptSec
			drop := ts.dropHello
			ts.mu.Unlock()
			if ok && !drop {
				ts.mu.Lock()
				ts.bound[from.String()] = true
				ts.mu.Unlock()
				ack := &Packet{Type: PacketTypeHelloAck, SenderID: pkt.SenderID}
				ts.conn.WriteToUDP(ack.AppendTo(nil), from)
			}
			continue
		case PacketTypeKeepalive:
			ts.keepalive++
			bound, drop := ts.bound[from.String()], ts.dropKeep
			ts.mu.Unlock()
			if bound && !drop {
				reply := NewKeepalive(pkt.SenderID, time.Now().UnixMilli())
				ts.conn.WriteToUDP(reply.AppendTo(nil), from)
			}
			continue
		}
		ts.mu.Unlock()
	}
}
```

- [ ] **Step 2: Write the failing session tests**

Create `internal/voice/session_test.go` covering:

- **handshake succeeds:** `Dial` against `testServer`, `OnState` reaches `StateConnected` within a second, and the server saw exactly 1 HELLO.
- **wrong secret never connects:** dial with a bad secret; state never reaches `StateConnected`; an error is delivered.
- **retry ladder resends HELLO:** `setDropHello(true)`, dial, advance the injected clock past the first three rungs, assert `helloCount() == 3` — resending on each attempt, matching the C# ladder.
- **exhaustion retries rather than surrendering:** after 5 failed attempts, state is `StateRetrying` and a further HELLO follows within 15 s of clock time. Assert the session did **not** reach `StateClosed`.
- **binding loss triggers re-HELLO:** connect, then `setDropKeepalive(true)`, advance the clock past three keepalive intervals, assert `helloCount()` increased — this is D6, the case the whole mechanism exists for.
- **RTT is measured:** after a keepalive round trip, `RTT()` is non-zero.
- **Close sends BYE:** assert the server received a BYE from the bound address.

Use `Options.Clock` injection throughout so no test sleeps for more than a few milliseconds of real time.

- [ ] **Step 3: Run to verify they fail**

Run: `go test -tags purego ./internal/voice/ -run TestSession -v`
Expected: FAIL — undefined `Dial`.

- [ ] **Step 4: Implement `internal/voice/session.go`**

Requirements, restated from spec §7 so they are not lost:

- **One connected socket** (`net.DialUDP`) for the whole session. Connected, not unconnected: the server's binding model needs a stable source address, and a connected socket surfaces ICMP port-unreachable as a write error.
- **HELLO ladder:** 5 attempts at 1.5 / 3 / 4.5 / 6 / 7.5 s, **resending HELLO each attempt**.
- **On exhaustion:** emit `StateRetrying` with the reason and re-run the whole ladder every 15 s. Do **not** fall through to a keepalive path — C# does, and it cannot work here, because keepalives from an unbound address are dropped.
- **Keepalive** every 5 s, echoing the server's last timestamp (empty before the first reply).
- **Binding loss:** 3 consecutive unanswered keepalives → re-HELLO on the same socket. Ladder also failing → close the socket, dial a fresh one (new source port), HELLO again.
- **BYE** on `Close`, best-effort.
- Goroutines: `rxLoop` (blocking read, parse, dispatch) and `keepaliveLoop`. Both exit on a `done` channel.

- [ ] **Step 5: Run to verify they pass**

Run: `go test -tags purego -race ./internal/voice/ -run TestSession -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/voice/session.go internal/voice/session_test.go internal/voice/testserver_test.go
git commit -m "feat(voice): add the session state machine

One connected UDP socket per session. HELLO ladder matching the C# peer's
timings and resend-per-attempt, but WITHOUT its fall-through to a keepalive
path on exhaustion -- keepalives from an unbound address are dropped, so
that branch cannot work against this server. Exhaustion retries the whole
ladder every 15 s instead of surrendering, because the usual causes are
transient.

Keepalives double as the liveness probe. A source-address change is
otherwise completely silent: our writes succeed, the server drops
everything, and nothing anywhere reports an error. Three unanswered
keepalives trigger a re-HELLO; a failed ladder re-dials on a fresh source
port.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: TX path

**Files:**
- Create: `internal/voice/tx.go`
- Test: `internal/voice/tx_test.go`
- Modify: `internal/voice/session.go` (expose `SetTXFrequencies`, `WriteFrame`)

**Interfaces:**
- Consumes: `opus.Encoder` (Task 1), `Packet`/`NewVoice` (Task 4), `Session` (Task 7), `KHz` (Task 3).
- Produces:
  ```go
  // TXTarget is one frequency to transmit on this press.
  type TXTarget struct {
      Freq     KHz
      Intercom bool
  }

  // SetTXFrequencies replaces the set of frequencies the next encoded frame
  // is emitted on. Safe to call from any goroutine.
  func (s *Session) SetTXFrequencies(targets []TXTarget)

  // EndTransmission marks a gate close so a partially-filled accumulator is
  // discarded rather than glued to the front of the next transmission.
  func (s *Session) EndTransmission()

  // WriteFrame implements audio.Sink. It runs ON THE DSP GOROUTINE inside a
  // 10 ms budget: memcpy and a non-blocking channel send, nothing else.
  func (s *Session) WriteFrame(frame []float32)

  // TXStats reports counters for diagnostics.
  type TXStats struct{ Sent, DroppedFull, DroppedNoTarget uint64 }
  func (s *Session) TXStats() TXStats
  ```

- [ ] **Step 1: Write the failing test**

Create `internal/voice/tx_test.go` covering:

- **two 10 ms frames make one 20 ms packet:** write 480-sample frames; after the second, exactly one datagram arrives at the test server. After the first, none.
- **one frame per target frequency:** set two targets, write two frames, assert two datagrams with the same payload and different `Frequency`.
- **per-frequency sequence counters:** set two targets, write six frames (3 packets each); each frequency's packets carry sequences 0,1,2 — not 0,2,4.
- **PTT flag set, intercom honoured per target.**
- **`EndTransmission` discards a partial frame:** write one 480-frame, `EndTransmission()`, write two more; assert the resulting packet contains only the later audio (assert on datagram count — exactly one — not on decoded samples).
- **`WriteFrame` never blocks:** fill the channel by not draining, then time 1000 `WriteFrame` calls and assert the total is under 10 ms; assert `TXStats().DroppedFull > 0`.
- **no targets means no packets:** `WriteFrame` with an empty target set produces nothing and increments `DroppedNoTarget`.
- **encoded length is checked against the ceiling.**

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/voice/ -run TestTX -v`
Expected: FAIL — undefined `SetTXFrequencies`.

- [ ] **Step 3: Implement `internal/voice/tx.go`**

Structure, from spec §8.1:

1. `WriteFrame` (DSP goroutine): append into a 960-sample accumulator; on full, take a buffer from a free list, copy, **non-blocking** send to `txPCM` (capacity 8 ≈ 160 ms), drop-and-count when full. Before appending, read an atomic generation counter and reset the accumulator if it changed.
2. `EndTransmission` bumps that generation counter.
3. `txLoop` goroutine: receive PCM, encode **once**, then emit one packet per target, each with its own 24-bit sequence counter. Return the buffer to the free list.
4. Targets held in an `atomic.Pointer[[]TXTarget]`.
5. Assert `n <= opus.MaxPacket` before sending; log once and drop if exceeded.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags purego -race ./internal/voice/ -run TestTX -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/voice/tx.go internal/voice/tx_test.go internal/voice/session.go
git commit -m "feat(voice): add the transmit path

WriteFrame runs on the DSP goroutine inside a 10 ms budget, so it does
memcpy and a non-blocking channel send and nothing else; encoding and the
socket write happen on txLoop. Two 10 ms frames accumulate into one 20 ms
Opus packet, emitted once per active frequency with per-frequency 24-bit
sequence counters -- receivers key jitter buffers by (sender, frequency),
so a shared counter would show them artificial gaps.

Gate close is a problem the Sink interface cannot express: WriteFrame is
only called while the gate is open, so a partially-filled accumulator would
be glued onto the front of the NEXT transmission. An atomic generation
counter bumped on release resets it, costing at most 10 ms of stale audio.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: RX path and the fourth mixer bus

**Files:**
- Create: `internal/voice/rx.go`
- Test: `internal/voice/rx_test.go`
- Modify: `internal/audio/mixer.go`
- Test: `internal/audio/mixer_test.go`
- Modify: `internal/audio/manager.go` (remove TX-side effect; call `ReadInto`)

**Interfaces:**
- Consumes: `jitter` (Task 5), `opus.Decoder` (Task 1), `audio.Effect`, `Session` (Task 7).
- Produces:
  ```go
  // ReadInto sums all active received streams into buf (FrameSamples long),
  // overwriting it. Runs ON THE DSP GOROUTINE: it only sums pre-decoded
  // rings -- no decode, no allocation, no lock held across a syscall.
  func (s *Session) ReadInto(buf []float32)

  // SetRXContext tells the RX path which frequencies to accept and which are
  // global (global frequencies bypass radio effects, matching the C# peer).
  func (s *Session) SetRXContext(accept []KHz, global []KHz, testFreqs []KHz)

  // SetEffects configures the per-stream radio effect ids.
  func (s *Session) SetEffects(voiceEffect, clippingEffect string)
  ```
  And in `internal/audio`:
  ```go
  // Mix now takes a fourth bus.
  func (m *Mixer) Mix(out, monitor, received, sfx, notif []float32)
  ```

- [ ] **Step 1: Write the failing mixer test**

Add to `internal/audio/mixer_test.go`:

```go
// TestMixSumsReceivedOntoTheVoiceBus pins that received voice shares the
// voice bus with the local monitor, as mixer.go's own doc promised: "Phase 5
// adds received voice to the voice bus alongside monitor".
func TestMixSumsReceivedOntoTheVoiceBus(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})

	out := make([]float32, 4)
	monitor := []float32{0.1, 0.1, 0.1, 0.1}
	received := []float32{0.2, 0.2, 0.2, 0.2}
	silent := make([]float32, 4)

	m.Mix(out, monitor, received, silent, silent)
	for i, got := range out {
		if got < 0.29 || got > 0.31 {
			t.Fatalf("out[%d] = %v, want ~0.3 (monitor 0.1 + received 0.2)", i, got)
		}
	}
}

// TestMixClampsCombinedVoice pins that the hard limiter still applies once a
// second source feeds the voice bus.
func TestMixClampsCombinedVoice(t *testing.T) {
	m := NewMixer()
	m.SetLevels(Levels{Master: 1, Voice: 1, SFX: 0, Notification: 0})
	out := make([]float32, 2)
	loud := []float32{0.9, -0.9}
	silent := make([]float32, 2)
	m.Mix(out, loud, loud, silent, silent)
	if out[0] != 1 || out[1] != -1 {
		t.Fatalf("out = %v, want [1 -1] (clamped)", out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/audio/ -run TestMix -v`
Expected: FAIL — `too many arguments in call to m.Mix`.

- [ ] **Step 3: Implement the mixer change**

Add the `received` bus to `Mix`, summed at the voice gain alongside `monitor`. Update the doc comment to state that the Phase 5 addition has landed. Update the `dspLoop` call site to pass a new `rxBuf` scratch buffer allocated once alongside the others.

- [ ] **Step 4: Move the effect from TX to RX**

In `dspLoop`, the chain currently runs `effect.Process(inFrame)` **before** the sink loop, so we transmit pre-effected audio. Per D7, remove that call from the transmit path and keep a separate instance for mic passthrough only:

```go
	// The radio effect used to run here, before the sinks -- which meant we
	// TRANSMITTED pre-effected audio. The C# peer applies effects on
	// RECEIVE, so that left a C# listener hearing us double-effected while
	// we heard them dry, and made the "no effects on global frequencies"
	// rule unimplementable from the sending side (we cannot know each
	// listener's frequency). Effects now live on the RX path, one instance
	// per stream, in internal/voice/rx.go. This instance colours ONLY the
	// local monitor, so self-monitoring still sounds like the radio.
	if gateOpen {
		sinks := *m.sinks.Load()
		for _, s := range sinks {
			s.WriteFrame(inFrame)
		}
	}
	if gateOpen && cfg.MicPassthrough {
		copy(monitorBuf, inFrame)
		monitorEffect.Process(monitorBuf)
	} else {
		clear(monitorBuf)
	}
```

- [ ] **Step 5: Write the failing RX tests**

Create `internal/voice/rx_test.go` covering:

- **demux by (sender, frequency):** two senders on one frequency produce two independent streams; one sender on two frequencies likewise.
- **own packets ignored, except on a test frequency:** a packet with our own `SenderID` is dropped; the same packet on a configured test frequency is accepted (the intended loopback).
- **unmatched frequencies dropped:** a frequency in neither `accept` nor `global` yields silence.
- **global frequencies bypass effects:** assert the effect was not applied (compare against a stream on a non-global frequency with the same input).
- **`ReadInto` sums two concurrent streams.**
- **`ReadInto` never blocks and never allocates:** wrap in `testing.AllocsPerRun` and assert 0 allocations.
- **streams are torn down after idle timeout.**

- [ ] **Step 6: Run to verify they fail, then implement `internal/voice/rx.go`**

Structure, from spec §8.2:

- `rxLoop` only parses and enqueues — it must never block, or the kernel drops datagrams.
- `decodeLoop` keeps each stream's PCM ring topped up to a target depth, woken by packet arrival and by ring drain, **never by a ticker of its own**.
- Each stream owns its own `opus.Decoder` and its own `audio.Effect` (biquad state is per-stream).
- `ReadInto` only sums pre-decoded rings.

- [ ] **Step 7: Run the full suite**

Run: `go test -tags purego -race ./internal/voice/ ./internal/audio/... -v`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/voice/rx.go internal/voice/rx_test.go internal/audio/mixer.go internal/audio/mixer_test.go internal/audio/manager.go
git commit -m "feat(voice): add the receive path and a fourth mixer bus

Demux by (sender, frequency) into per-stream jitter buffers; decodeLoop
tops up per-stream PCM rings woken by arrival and drain rather than by a
ticker of its own, so dspLoop's tick stays the only clock on the RX path
(D8) -- two unsynchronised 10 ms clocks would drift into periodic
underruns. ReadInto only sums pre-decoded rings, so the DSP budget carries
no decode and no allocation.

Radio effects move from TX to RX (D7), one instance per stream since biquad
state is per-stream, suppressed on global frequencies as the C# peer does.
Transmitting pre-effected audio left a C# listener hearing us
double-effected while we heard them dry. dspLoop keeps a separate instance
for mic passthrough so self-monitoring still sounds right.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Config — `[voice]` and `[[radios]]`

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing (config must not import `internal/voice` — it is a raw-values layer, same discipline as `Keybinds` and `Audio`).
- Produces:
  ```go
  type Voice struct {
      Host           string `toml:"host"`
      Port           int    `toml:"port"`
      JitterBufferMS int    `toml:"jitter_buffer_ms"`
      MaxBufferMS    int    `toml:"max_buffer_ms"`
  }

  type Radio struct {
      ID           uint32 `toml:"id"`
      Name         string `toml:"name"`
      FrequencyKHz uint32 `toml:"frequency_khz"`
      Enabled      bool   `toml:"enabled"`
      IsIntercom   bool   `toml:"is_intercom"`
  }

  // On Config:
  //   Voice  Voice   `toml:"voice"`
  //   Radios []Radio `toml:"radios"`
  ```

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
// TestDefaultSeedsRadios pins that a first run produces a usable radio set.
// The server creates every client with ZERO radios (state.AddClient sets
// Radios: []), and nothing else in the client can create one, so without a
// seed there is nothing to transmit on and the Comms window stays empty
// forever.
func TestDefaultSeedsRadios(t *testing.T) {
	c := Default()
	if len(c.Radios) == 0 {
		t.Fatal("Default() seeded no radios; there would be nothing to transmit on")
	}
	seen := map[uint32]bool{}
	for _, r := range c.Radios {
		if seen[r.ID] {
			t.Fatalf("duplicate radio id %d", r.ID)
		}
		seen[r.ID] = true
		if r.FrequencyKHz == 0 {
			t.Errorf("radio %d has no frequency", r.ID)
		}
		if r.FrequencyKHz > 1<<24-1 {
			t.Errorf("radio %d frequency %d does not fit the 24-bit wire field", r.ID, r.FrequencyKHz)
		}
		if r.Name == "" {
			t.Errorf("radio %d has no name", r.ID)
		}
	}
}

func TestDefaultVoiceSettings(t *testing.T) {
	c := Default()
	if c.Voice.JitterBufferMS != 60 {
		t.Errorf("JitterBufferMS = %d, want 60", c.Voice.JitterBufferMS)
	}
	if c.Voice.MaxBufferMS != 500 {
		t.Errorf("MaxBufferMS = %d, want 500", c.Voice.MaxBufferMS)
	}
	// Host and Port are deliberately empty/zero: an unset [voice] table means
	// "derive from server_url on port 5002", which is what every standalone
	// deployment needs.
	if c.Voice.Host != "" || c.Voice.Port != 0 {
		t.Errorf("Voice host/port default to %q/%d, want empty/0 so resolution falls through to server_url", c.Voice.Host, c.Voice.Port)
	}
}

// TestRadiosRoundTrip pins that an existing config without a [[radios]]
// array still loads, and that a saved one comes back byte-identical.
func TestRadiosRoundTrip(t *testing.T) {
	// ... write a config with radios, Load it, Save it, compare.
}

// TestLoadWithoutRadiosSectionGetsDefaults pins backward compatibility with
// every config.toml written before this phase.
func TestLoadWithoutRadiosSectionGetsDefaults(t *testing.T) {
	// ... write a minimal config.toml with no [[radios]] and no [voice],
	// Load it, assert radios and voice settings are populated from Default().
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -tags purego ./internal/config/ -v`
Expected: FAIL — `c.Radios` undefined.

- [ ] **Step 3: Implement**

Add both types and both `Config` fields, with defaults in `Default()`. The seed set — three radios plus an intercom, on plausible VHF/UHF frequencies, all enabled — is a **starting point the user edits**, and the doc comment must say so rather than implying the frequencies are meaningful.

Follow the existing merge discipline: a config file missing `[[radios]]` or `[voice]` must load and get defaults, so every config written before this phase keeps working.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -tags purego -race ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): add [voice] settings and a persisted [[radios]] set

Server-side radio state is created empty by AddClient and lost on
disconnect, and nothing in the client could create a radio, so a connected
client had nothing to transmit on. Radios now persist locally and are
re-pushed on every connect -- which also makes the radio stack survive
restarts, and is the honest seed of Phase 7's profiles without doing
profiles.

An unset [voice] host/port means 'derive from server_url on port 5002',
which is what every standalone deployment needs. Configs written before
this phase load unchanged and pick up defaults.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: App wiring — control plumbing, selected radio, TX routing, session lifecycle

**Files:**
- Modify: `internal/control/client.go` (capture `voice_secret` + addresses in `SyncClient`)
- Modify: `internal/control/stream.go` (real `VOICE_ADDRESS_UPDATE` case)
- Modify: `internal/state/store.go` (hold voice credentials; selected radio)
- Create: `internal/app/voice.go`
- Modify: `internal/app/audio.go` (PTT dispatch feeds the TX set)
- Modify: `internal/app/bindings.go`, `internal/app/dto.go`
- Modify: `main.go`
- Test: `internal/control/client_test.go`, `internal/control/stream_test.go`, `internal/state/store_test.go`, `internal/app/voice_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3–10.
- Produces:
  ```go
  // internal/state
  func (s *Store) SetVoiceCredentials(secret, coalitionAddr, globalAddr string)
  func (s *Store) VoiceCredentials() (secret, coalitionAddr, globalAddr string)
  func (s *Store) SetSelectedRadio(id uint32)
  func (s *Store) SelectedRadio() uint32

  // internal/app
  func (a *App) SelectRadio(id uint32) error   // Wails binding
  func (a *App) VoiceState() VoiceStateDTO     // Wails binding
  ```

- [ ] **Step 1: Write the failing control tests**

In `internal/control/client_test.go`, assert `SyncClient` stores `voice_secret`, `coalition_voice_addr` and `global_voice_addr` into the store — today it discards all three.

In `internal/control/stream_test.go`, assert a `VOICE_ADDRESS_UPDATE` is routed rather than ignored:

```go
// TestRouteVoiceAddressUpdate pins that VOICE_ADDRESS_UPDATE reaches the
// store. stream.go's default branch silently discarded it, which was correct
// while there was no voice path and is a dropped redirect now.
func TestRouteVoiceAddressUpdate(t *testing.T) {
	st := state.New()
	em := &fakeEmitter{}
	route(&srspb.ServerUpdate{
		Type: srspb.ServerUpdate_VOICE_ADDRESS_UPDATE,
		Update: &srspb.ServerUpdate_VoiceAddressUpdate{
			VoiceAddressUpdate: &srspb.VoiceAddressUpdate{
				CoalitionVoiceAddr: "10.0.0.9:5002",
				GlobalVoiceAddr:    "10.0.0.1:5002",
				VoiceSecret:        strings.Repeat("A", 43),
			},
		},
	}, st, events.New(em))

	secret, coal, global := st.VoiceCredentials()
	if coal != "10.0.0.9:5002" {
		t.Errorf("coalition addr = %q", coal)
	}
	if global != "10.0.0.1:5002" {
		t.Errorf("global addr = %q", global)
	}
	if len(secret) != 43 {
		t.Errorf("secret length = %d, want 43", len(secret))
	}
}
```

- [ ] **Step 2: Run to verify they fail, then implement the control + state changes**

`SyncClient` reads the three new fields into the store. `route` gains a `VOICE_ADDRESS_UPDATE` case that stores them and emits a typed event the voice session subscribes to. Remove `VOICE_ADDRESS_UPDATE` from the `default:` comment.

- [ ] **Step 3: Write the failing TX-routing test**

In `internal/app/voice_test.go`:

```go
// TestPTTRoutesToTheSelectedRadio pins that global.ptt transmits on the
// SELECTED radio, which is what its own action description has claimed since
// Phase 3 while nothing implemented it.
func TestPTTRoutesToTheSelectedRadio(t *testing.T) { /* ... */ }

// TestPerRadioPTTRoutesToThatRadio pins that radio.N.ptt ignores the
// selection.
func TestPerRadioPTTRoutesToThatRadio(t *testing.T) { /* ... */ }

// TestHoldingBothTransmitsOnBoth pins the set union. Phase 3.5's refcount
// already lets two sources hold one action; this is two DIFFERENT actions
// held at once, which must widen the frequency set rather than replace it.
func TestHoldingBothTransmitsOnBoth(t *testing.T) { /* ... */ }

// TestReleasingOneKeepsTheOther pins that releasing one PTT does not cut
// transmission on the other -- the same class of bug Phase 3.5's press
// refcount exists to prevent, one level up.
func TestReleasingOneKeepsTheOther(t *testing.T) { /* ... */ }

// TestGateStaysOpenWhileAnyTargetIsHeld pins that Manager.SetPTT keeps its
// exact current meaning: the TX set being non-empty.
func TestGateStaysOpenWhileAnyTargetIsHeld(t *testing.T) { /* ... */ }
```

Fill each in with real assertions against the refcounted set.

- [ ] **Step 4: Implement the TX routing set and session lifecycle**

Create `internal/app/voice.go` holding:
- the refcounted active-TX set (action id → radio id), driven from `dispatchAudioPressed` / `dispatchAudioReleased`
- selected-radio state with its binding and event
- voice session start on connect (after `SyncClient` yields the secret), stop on disconnect
- `SetTXFrequencies` / `EndTransmission` calls as the set changes
- `SetRXContext` refreshed whenever radios or server settings change
- re-push of the persisted radio set via `UpdateRadioInfo` on every connect

In `internal/app/audio.go`, the two PTT cases now also update the TX set. `m.SetPTT` is driven by set emptiness, not by the individual action.

- [ ] **Step 5: Register the single Sink in `main.go`**

One `Sink` at startup, never removed (D5), holding an atomic session pointer. Keep `defer backend.Close()` registered **before** `defer am.Stop()` — Task 2's test now pins this.

- [ ] **Step 6: Run the full Go suite**

Run: `go build -tags purego ./... && go vet -tags purego ./... && go test -tags purego -race ./...`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/control/ internal/state/ internal/app/ main.go
git commit -m "feat(voice): wire the session into the control plane and PTT

SyncClient captured none of voice_secret, coalition_voice_addr or
global_voice_addr; stream.go silently discarded VOICE_ADDRESS_UPDATE. Both
now reach the store, and a redirect re-points the voice socket without
disturbing the gRPC session.

PTT stops being a single boolean: a refcounted TX set maps global.ptt to
the selected radio and radio.N.ptt to radio N, holding both widens the set
rather than replacing it, and releasing one does not cut the other.
Manager.SetPTT keeps its exact meaning -- the set being non-empty -- so
Phase 3.5's multi-source press refcount is untouched.

radio.N.select finally has a consumer, three phases after it was defined.

Exactly one Sink is registered at startup and never removed, holding an
atomic session pointer: nil when disconnected. Phase 4 has no RemoveSink
and never calls Close(), so a per-connection sink would leak on every
reconnect.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Integration tests against the real server, in CI

**Files:**
- Create: `internal/voice/integration_test.go`
- Create: `internal/voice/testdata/server_fixture.go` (config + key generation helpers)
- Modify: `.github/workflows/test.yml`

**Interfaces:**
- Consumes: everything.
- Produces: nothing consumed by later tasks.

Read spec §11.1. This is the only fixture that can catch a **misreading** of the protocol; everything hand-written encodes our own reading and passes regardless.

- [ ] **Step 1: Write the server fixture helper**

A helper that, given `t`, produces a running server:

- generate an ECDSA P-256 keypair, PEM-encode private (`x509.MarshalECPrivateKey`, block type `PRIVATE KEY`) and public (`x509.MarshalPKIXPublicKey`, block type `PUBLIC KEY`) into `t.TempDir()` — this is exactly the format `utils/auth.go`'s `decode` expects
- write a `config.yaml` with one coalition (name + **plaintext** password — the server bcrypt-compares the client's hash against the stored plaintext), `enableGuestAuth: true`, and free ports for the client gRPC and voice UDP listeners
- exec `$VCS_SERVER_BIN --mode standalone --autostart --config <path> --banned <path> --log-folder <path>`
- poll until both ports accept, with a bounded timeout
- `t.Cleanup` kills the process and dumps its log on failure

Gate the whole file:

```go
func requireServer(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("VCS_SERVER_BIN")
	if bin == "" {
		t.Skip("VCS_SERVER_BIN not set; skipping real-server integration tests")
	}
	return bin
}
```

- [ ] **Step 2: Write the integration tests**

Each case from spec §11.1, as its own test:

1. `TestIntegrationRelayBetweenTwoClients` — two sessions on one frequency; client A transmits, client B receives decodable audio.
2. `TestIntegrationWrongSecretRejected` — no binding, no ACK, state never reaches Connected.
3. `TestIntegrationVoiceFromUnboundAddressDropped` — send a VOICE packet from a second socket using a bound session's UUID; it is not relayed.
4. `TestIntegrationKeepaliveEchoFeedsLatency` — after several keepalives, the session's RTT is non-zero.
5. `TestIntegrationTestFrequencyLoopback` — configure a test frequency; a packet sent on it comes back to the sender.
6. `TestIntegrationByeDisconnects` — after `Close`, the server stops relaying to that client.
7. `TestIntegrationReHelloAfterSourceAddressChange` — **the one worth the exercise.** Connect, force the session onto a new source port, assert audio flows again without user action.

- [ ] **Step 3: Run them locally**

```bash
git -C "$TMPDIR" clone --depth 1 https://github.com/FPGSchiba/vcs-srs-server "$TMPDIR/vcs-srs-server"
(cd "$TMPDIR/vcs-srs-server" && go build -tags headless -o "$TMPDIR/vcs-server" .)
VCS_SERVER_BIN="$TMPDIR/vcs-server" go test -tags purego -race ./internal/voice/ -run TestIntegration -v
```
Expected: PASS, 7 tests.

Then confirm they **skip** cleanly without the env var:
Run: `go test -tags purego -race ./internal/voice/ -run TestIntegration -v`
Expected: 7 SKIP, exit 0.

- [ ] **Step 4: Add the CI job**

In `.github/workflows/test.yml`, add to the `backend` job, **after** Setup Go and before the test step:

```yaml
      # Phase 5's voice tests run against the REAL server rather than a
      # hand-written fake. A fake can only encode our own reading of the
      # protocol, so it cannot catch a MISREADING of it -- which is the
      # largest single risk in an interop phase spanning three
      # independently-written implementations. Both repos are public, so
      # this needs no credentials.
      #
      # -tags headless excludes the server's Wails GUI, so no GTK/WebKit
      # is required on any runner for this build.
      - name: Check out the VCS server
        uses: actions/checkout@v7
        with:
          repository: FPGSchiba/vcs-srs-server
          ref: main
          path: .vcs-srs-server

      - name: Build the headless VCS server
        run: |
          cd .vcs-srs-server
          go build -tags headless -o "${{ runner.temp }}/vcs-server${{ runner.os == 'Windows' && '.exe' || '' }}" .

      - name: Export the server binary path
        run: echo "VCS_SERVER_BIN=${{ runner.temp }}/vcs-server${{ runner.os == 'Windows' && '.exe' || '' }}" >> "$GITHUB_ENV"
```

The existing `go test` steps then pick `VCS_SERVER_BIN` up from the environment and stop skipping.

Add `.vcs-srs-server/` to `.gitignore`.

- [ ] **Step 5: Verify the workflow parses**

Run: `npx --yes yaml-lint .github/workflows/test.yml` (or any YAML validator available).
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/voice/integration_test.go internal/voice/testdata/ .github/workflows/test.yml .gitignore
git commit -m "test(voice): integration tests against the real headless server

Builds vcs-srs-server with -tags headless in CI and drives the real client
stack end-to-end against it: guest login, SyncClient, voice_secret, HELLO,
relay, decode. Both repos are public, so this needs no credentials.

A hand-written fake encodes our own reading of the protocol and therefore
passes whether or not that reading is correct. With three independently
written implementations on this wire, a plausible misreading is the largest
risk in the phase, and only a real server can catch one.

Includes the case the binding-loss machinery exists for: re-HELLO recovery
after a source-address change, which is otherwise completely silent.

Skips cleanly when VCS_SERVER_BIN is unset, so local `go test ./...` stays
green with no server.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: Frontend — tunable radios, selection, live transmit state

**Files:**
- Modify: `frontend/src/shared/components/LcdFreq.tsx`
- Modify: `frontend/src/windows/comms/CommsApp.tsx`
- Modify: `frontend/src/windows/comms/RadioCard.tsx`
- Modify: `frontend/src/shared/store/radios.ts`
- Modify: `frontend/src/shared/api/client.ts`, `frontend/src/shared/api/events.ts`
- Test: `frontend/src/shared/components/LcdFreq.test.tsx`, `frontend/src/windows/comms/RadioCard.test.tsx`, `frontend/src/windows/comms/CommsApp.test.tsx`

- [ ] **Step 1: Write the failing `LcdFreq` tests**

```tsx
// LcdFreq is read-only today only because Phase 1 had nothing to tune. The
// design prototype always specified drag/wheel/type editing.
describe("LcdFreq", () => {
  it("commits a typed frequency on Enter", async () => { /* ... */ });
  it("reverts on Escape", async () => { /* ... */ });
  it("steps by wheel", async () => { /* ... */ });
  it("clamps to the 24-bit kHz wire range", async () => { /* ... */ });
  it("stays read-only when no onChange is given", async () => { /* ... */ });
});
```

- [ ] **Step 2: Run to verify they fail**

Run: `(cd frontend && npx vitest run src/shared/components/LcdFreq.test.tsx)`
Expected: FAIL.

- [ ] **Step 3: Implement the editable `LcdFreq`**

Keep the existing `.lcd-screen` / `.lcd-digits` / `.lcd-digit` markup so the ported CSS still applies. Editing is opt-in via an optional `onChange` prop, so existing read-only usages are unaffected.

- [ ] **Step 4: Fix `CommsApp`'s radio selection**

```tsx
// Was: Object.values(radios)[0] -- the first entry of the WHOLE radios map,
// which is any client's radios, not ours. Harmless while nobody else was
// connected; wrong the moment voice makes multi-client sessions real.
const selfGuid = useSession((s) => s.selfGuid);
const entry = selfGuid ? radios[selfGuid] : undefined;
```

Write a test that puts two clients in the store and asserts the local client's radios render.

- [ ] **Step 5: Add selection and live transmit state to `RadioCard`**

- clicking a card selects it (`api.selectRadio(id)`), with the selected card visually marked
- the PTT button stops being `disabled` and reflects live transmit state from the voice event
- the frequency LCD becomes editable and commits through `api.updateRadioInfo`

- [ ] **Step 6: Run the frontend suite**

```bash
(cd frontend && npx vitest run)
(cd frontend && npx tsc --noEmit)
(cd frontend && npm run build)
```
Expected: all PASS. Remember `npx --prefix frontend tsc --noEmit` does **not** typecheck.

- [ ] **Step 7: Commit**

```bash
git add frontend/
git commit -m "feat(comms): tunable radios, selection, and live transmit state

LcdFreq becomes editable, as the design prototype always specified -- it
was read-only only because Phase 1 had nothing to tune. Editing is opt-in
via an optional onChange, so read-only usages are unaffected.

CommsApp rendered Object.values(radios)[0], the first entry of the whole
radios map rather than the local client's. Harmless while nobody else was
connected; wrong the moment voice makes multi-client sessions real.

RadioCard gains selection (driving global.ptt) and a PTT button that is no
longer display-only.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Documentation and the manual verification checklist

**Files:**
- Create: `docs/superpowers/plans/2026-09-24-phase-5-manual-verification.md`
- Modify: `docs/ROADMAP.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: Write the manual verification checklist**

Follow `docs/superpowers/plans/2026-09-23-phase-4-manual-verification.md`'s structure. Cover every item that automated tests cannot reach:

- real mouth-to-ear latency, measured, against a stated target
- audio quality across a real network, including a lossy link
- **cross-client interop with the C# peer** — explicitly blocked on RV1 until PR #253 adds the secret to its HELLO; record it as blocked rather than untested
- a genuine network change mid-session (Wi-Fi → Ethernet, VPN toggle), asserting recovery within ~15 s with no user action
- Star Citizen audio coexistence in both launch orders
- PTT from keyboard and joystick, including holding two PTTs at once
- multi-client session with 3+ participants on overlapping frequencies
- the test-frequency loopback as a self-check
- radio tuning persisting across a restart

- [ ] **Step 2: Update ROADMAP**

Mark Phase 5 `[x]` complete with the date, its verification status (automated green, hardware **not** verified), and a link to the checklist — matching how Phases 3, 3.5 and 4 are recorded.

- [ ] **Step 3: Update CLAUDE.md**

Update the Current status table: Phase 5 complete-but-unverified, Phase 6 next. Add a short note that the codec geometry is a wire contract with the C# peer and must not be changed unilaterally.

- [ ] **Step 4: Final full verification**

```bash
go build -tags purego ./...
go vet -tags purego ./...
go test -tags purego -race ./...
VCS_SERVER_BIN="$TMPDIR/vcs-server" go test -tags purego -race ./internal/voice/ -v
(cd frontend && npx vitest run)
(cd frontend && npx tsc --noEmit)
(cd frontend && npm run build)
```
Expected: everything green.

- [ ] **Step 5: Commit**

```bash
git add docs/ CLAUDE.md
git commit -m "docs: Phase 5 manual verification checklist and status

Automated suite is green; the phase is NOT hardware-verified. Cross-client
interop with the C# peer is blocked on VNGD-SimpleRadioStandalone PR #253
adding the voice secret to its HELLO, recorded as blocked rather than
untested.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage.** §4 codec → Task 1. §4.1 datagram budget → Tasks 1, 8. §5 frequency → Task 3. §6 endpoint → Task 6. §7 lifecycle/handshake/keepalive/binding-loss → Task 7. §8.1 TX → Task 8. §8.2 RX, §8.3 jitter → Tasks 5, 9. §8.4 sink attachment → Task 11 Step 5. §9.1 TX routing → Task 11. §9.2 radio bootstrap → Tasks 10, 11, 13. §9.3 CommsApp bug → Task 13. §10 follow-ups → Task 2. §11.1 integration → Task 12. §11.2 unit → Tasks 1, 3, 4, 5, 6, 8, 9. §11.3 manual → Task 14. §12 proto → already committed (`e6f7820`). D1–D12 all land. No gaps.

**Type consistency.** `KHz` is used identically in Tasks 3, 4, 8, 9. `TXTarget` is defined in Task 8 and consumed in Task 11. `Session` methods introduced across Tasks 7–9 do not collide. `Mixer.Mix`'s new five-argument form is defined in Task 9 and its only call site is updated in the same task. `emitStateIfChanged`'s signature change in Task 2 precedes every task that touches `manager.go`.

**Ordering.** Task 2 lands the follow-ups before any voice code touches `manager.go`. Task 1 precedes every task needing the codec. Tasks 3–6 are pure and independent. Task 7 needs 4 and 6; Task 8 needs 1, 3, 4, 7; Task 9 needs 1, 5, 7. Task 11 needs 3–10. Task 12 needs everything. Tasks 13 and 14 are last.
