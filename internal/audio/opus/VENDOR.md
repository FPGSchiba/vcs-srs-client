# Vendored: libopus

- **Upstream:** https://opus-codec.org/ — source tarball
  `https://downloads.xiph.org/releases/opus/opus-1.5.2.tar.gz`
- **Vendored version:** **1.5.2** (release tarball, `package_version` =
  `PACKAGE_VERSION="1.5.2"`, tree dated 2024-04-12). No git checkout was used, so
  there is no upstream commit hash to quote; the tarball is the release artifact.
  - `sha256(opus-1.5.2.tar.gz) = 65c1d2f78b9f2fb20082c38cbe47c951ad5839345876e46941612ee87f9a7ce1`
    — this is the hash of the archive as downloaded during this vendoring pass. It
    was **not** checked against upstream's detached signature (no key material
    available in the sandbox); a future re-vendoring pass should verify
    `opus-1.5.2.tar.gz.sig` against the Xiph release key.
- **Vendored at:** 2026-09-24
- **License:** BSD-3-Clause (Xiph.Org Foundation, Skype Limited, Octasic,
  Jean-Marc Valin, Timothy B. Terriberry, CSIRO, Gregory Maxwell, Mark Borgerding,
  Erik de Castro Lopo, Mozilla, Amazon). Verbatim upstream `COPYING` copied to
  `LICENSE` in this directory. Note that upstream's `COPYING` also carries the
  separate BSD-2-Clause grant covering the Opus **patent** situation and the
  Broadcom/Xiph IPR statements — it is reproduced whole, unedited.

## Why vendored rather than depended upon

Same reasoning as `internal/audio/rnnoise` and `internal/joystick/di8`:

- There is no usable system libopus on any of the three target platforms. macOS has
  none by default; Windows has none at all; Linux distributions ship one but at
  wildly differing versions, and requiring `libopus-dev` on every developer machine
  and every CI leg is a build-reproducibility tax we already refused for RNNoise.
- `pkg-config`-based discovery (`#cgo pkg-config: opus`) would make the build depend
  on the host's pkg-config database, which on the Windows CI leg does not exist.
- The Go ecosystem's usual answer, `gopkg.in/hraban/opus.v2`, either links a system
  libopus (same problem) or vendors a 2015-era 1.1.x fork. Our wire contract is with
  a C# peer using a current `Concentus`/`libopus` build; pinning a decade-old
  encoder is a bitstream-quality risk for no benefit.
- libopus's public C API (`opus_encoder_create` / `opus_encode_float` /
  `opus_encoder_ctl` / `opus_decoder_create` / `opus_decode_float` / the `_destroy`
  pair) has been stable since 1.0 and is what this binding uses — nothing here would
  change if a future pass moves to 1.6.

The codec is on the critical voice path, so removing a supply-chain hop is worth the
2.5 MB of C in the tree.

## Layout — and why it is not the obvious one

```
internal/audio/opus/
  *.c                 137 vendored sources, FLATTENED into the package root
  ctl_shim.c          our variadic-CTL shim (see below)
  ctl_shim.h
  opus.go             the cgo binding (//go:build cgo)
  opus_stub.go        the CGO_ENABLED=0 stub (//go:build !cgo)
  opus_test.go
  VENDOR.md  LICENSE
  include/            6 public headers, upstream paths
  celt/              30 CELT headers, upstream paths
  silk/              21 SILK headers, upstream paths
  silk/float/         3 SILK float-path headers, upstream paths
  src/                4 private headers from upstream src/, upstream paths
```

Two facts force this shape, and the "obvious" alternatives fail on one or the other:

1. **cgo only auto-compiles `.c` files that sit in the SAME directory as the Go file
   containing `import "C"`.** This is the trap `internal/audio/rnnoise/VENDOR.md`
   documents at length — an include path (`-I`) finds *headers*, it does not make cgo
   *compile* sources, and the failure mode is a link error listing every undefined
   Opus symbol. So every `.c` file must be a sibling of `opus.go`, which means
   flattening `src/`, `celt/`, `silk/` and `silk/float/` into one directory.
   Verified before flattening: **there are no basename collisions** across those four
   directories (137 files, 137 distinct basenames).

2. **Some sources use path-qualified includes** — e.g. `#include "celt/stack_alloc.h"`
   and `#include "celt/cpu_support.h"` in `src/opus_encoder.c` — while others include
   the same headers unqualified (`#include "stack_alloc.h"`). A flat-only layout
   breaks the first spelling; a subtree-only layout breaks the second.

The fix is to keep the **headers** in their upstream directory structure and let the
include path resolve both spellings at once: `-I${SRCDIR}` handles `"celt/…"`, and
`-I${SRCDIR}/celt` (etc.) handles the bare names. Only `.c` files drive compilation,
so keeping headers out of the flat root costs nothing and keeps the root readable.

## File list

The authoritative source list is **upstream's own `*_sources.mk`**, not a hand-picked
set. From `opus_sources.mk`, `celt_sources.mk` and `silk_sources.mk` we take exactly:

| Make variable | Files | What it is |
|---|---:|---|
| `OPUS_SOURCES` | 11 | `src/` — the top-level encoder/decoder/multistream/projection/repacketizer API |
| `OPUS_SOURCES_FLOAT` | 3 | `src/analysis.c`, `src/mlp.c`, `src/mlp_data.c` — the float-only music/speech analysis front-end |
| `CELT_SOURCES` | 18 | `celt/` — generic-C CELT (MDCT layer) |
| `SILK_SOURCES` | 77 | `silk/` — arithmetic-independent SILK (LPC layer) |
| `SILK_SOURCES_FLOAT` | 28 | `silk/float/` — SILK's floating-point analysis path |
| **total** | **137** | |

Headers copied wholesale from `include/`, `celt/*.h`, `silk/*.h`, `silk/float/*.h`
and `src/*.h` (64 headers total). Copying all of them, rather than only the ones
currently reachable, keeps a future upstream bump from turning into a header
scavenger hunt; unreferenced headers cost nothing because they are never compiled.

## Deliberately NOT copied

- **`dnn/` (≈17 MB)** — the LPCNet / DRED / OSCE deep-learning extensions added in
  Opus 1.5. Not needed for plain 20 ms voice, and vendoring it would recreate exactly
  the separate-model-asset problem that made RNNoise's `v0.2` the wrong thing to
  vendor (see `../rnnoise/VENDOR.md`). **Verified safe:** none of the five source
  lists above reference `dnn/`, and every `#include` of a DNN header in the vendored
  sources (`dred_coding.h`, `dred_rdovae_dec.h`, `dred_encoder.h`, `osce.h`,
  `osce_structs.h`, `lpcnet.h`, `lpcnet_private.h` — 13 include sites across 9 files)
  sits inside `#ifdef ENABLE_DEEP_PLC` / `#ifdef ENABLE_DRED` / `#ifdef ENABLE_OSCE`,
  none of which our CFLAGS define.
- **`SILK_SOURCES_FIXED` and `silk/fixed/`** — we build the floating-point path
  (`FIXED_POINT` is never defined). Every desktop target we ship to has an FPU.
- **All architecture-specific SIMD directories** — `celt/arm/`, `celt/x86/`,
  `celt/mips/`, `silk/arm/`, `silk/x86/`, `silk/mips/`, `silk/fixed/x86/`,
  `silk/fixed/arm/`, `silk/float/x86/`, and the corresponding `*_SOURCES_ARM*` /
  `*_SOURCES_X86*` / `*_SSE*` / `*_AVX2` / `*_NEON_INTR` / `*_RTCD` make variables.
  Upstream selects these at configure time via runtime CPU detection (RTCD), which is
  driven by `HAVE_CONFIG_H` + autotools probing. We never define `HAVE_CONFIG_H`
  (all 137 sources guard `#include "config.h"` with `#ifdef HAVE_CONFIG_H`), never
  define `OPUS_HAVE_RTCD` / `OPUS_ARM_*` / `OPUS_X86_*`, and therefore compile the
  generic C paths on every platform. The residual `#include "arm/…"` /
  `#include "x86/…"` lines left in the generic headers all sit inside those same
  guards and are unreachable.
  - Cost: no NEON/AVX2 fast paths. At 48 kbps mono, 20 ms frames, one encoder and a
    handful of decoders, that is irrelevant — a full encode of a 960-sample frame is
    a low-microsecond operation on any machine that can run Star Citizen.
  - The one arch-conditional include that IS reachable is
    `celt/float_cast.h`'s `<xmmintrin.h>`, taken on `__GNUC__ && __SSE__` (i.e. every
    x86-64 gcc/clang build). That header ships with the compiler, not with Opus, so
    it needs nothing from `celt/x86/`.
- `autogen.sh`, `configure`, `configure.ac`, `Makefile.am`/`.in`, `Makefile.unix`,
  `Makefile.mips`, `aclocal.m4`, `m4/`, `compile`, `depcomp`, `install-sh`,
  `ltmain.sh`, `missing`, `config.guess`, `config.sub`, `test-driver`,
  `opus.m4`, `*.pc.in`, `config.h.in` — autotools scaffolding. This package is built
  entirely by `#cgo CFLAGS`; no autoreconf, no make.
- `cmake/`, `CMakeLists.txt`, `meson/`, `meson.build`, `meson_options.txt` — the
  alternative build systems, same reason.
- `tests/`, `doc/`, `README`, `NEWS`, `ChangeLog`, `AUTHORS`, `INSTALL` — test
  harnesses and documentation, not library code. (Upstream's test vectors are worth
  knowing about if a future pass suspects a decode bug, but they are a 100 MB
  separate download and are not needed to build.)
- `lpcnet_sources.mk` / `lpcnet_headers.mk` / `celt_headers.mk` / `opus_headers.mk` /
  `silk_headers.mk` — build metadata; the three `*_sources.mk` files were read during
  vendoring but not copied, since this file records their outcome.

## Required `#cgo` directives, and why each one is there

```
#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/include -I${SRCDIR}/celt -I${SRCDIR}/silk -I${SRCDIR}/silk/float -I${SRCDIR}/src
#cgo CFLAGS: -O2 -DOPUS_BUILD -DHAVE_LRINTF -DVAR_ARRAYS
#cgo linux LDFLAGS: -lm
```

**Include paths.** `-I${SRCDIR}` resolves the path-qualified spellings
(`"celt/stack_alloc.h"`); the four per-directory `-I`s resolve the bare spellings
(`"stack_alloc.h"`, `"main_FLP.h"`, `"opus_private.h"`); `-I${SRCDIR}/include`
resolves the public `<opus.h>` that the cgo preamble itself includes.

**`-DVAR_ARRAYS` — mandatory, not a preference.** `celt/stack_alloc.h:38` is an
outright `#error` unless exactly one of `VAR_ARRAYS`, `USE_ALLOCA` or
`NONTHREADSAFE_PSEUDOSTACK` is defined; it selects how Opus allocates its per-call
scratch. `VAR_ARRAYS` uses C99 variable-length arrays, which is the right choice
here: `USE_ALLOCA` is `alloca()`-based (fine, but no better), and
`NONTHREADSAFE_PSEUDOSTACK` is, as the name says, not thread-safe — unacceptable
when Phase 5 will run one encoder on the capture goroutine and N decoders on the
playback side. Caveat for a future maintainer: MSVC does not support VLAs, so
`VAR_ARRAYS` would have to become `USE_ALLOCA` under MSVC — but cgo cannot use MSVC
at all (it requires a gcc/clang-compatible driver), so on Windows this is always
mingw-w64 gcc, which supports VLAs.

**`-DOPUS_BUILD` — upstream's "I am compiling the library itself" flag,** set by
upstream's own `Makefile.am` and `CMakeLists.txt`. It controls `OPUS_EXPORT` in
`include/opus_defines.h`: with `OPUS_BUILD` and GCC/clang it becomes
`__attribute__((visibility("default")))` rather than a dllimport declaration, which
is what we want when statically compiling into the Go binary. It also suppresses the
`nonnull` attribute inside the library (upstream comment at `opus_defines.h:114`:
NONNULL is not used in `OPUS_BUILD` to avoid the compiler optimizing out internal
null checks). Omitting it would not fail the build outright, but it would build the
sources as if they were a *consumer* of a shared libopus, which is wrong.

**`-DHAVE_LRINTF` — real effect on arm64, inert on x86.** `celt/float_cast.h` picks
its float→int32 conversion in a chain: on `__GNUC__ && __SSE__` it uses
`_mm_cvt_ss2si` (so every x86-64 build takes that branch regardless of this define);
otherwise, at `float_cast.h:102`, `HAVE_LRINTF` + C99 selects `lrintf()`. Without it,
arm64 (this machine, and Apple Silicon users) falls through to the portable
`(int)floor(x + 0.5)` fallback, which is both slower and rounds differently from the
C# peer's encoder. Upstream's configure probes for `lrintf` and sets this; we assert
it, which is safe because all three toolchains are C99 and provide `lrintf`.

**`-O2`** — the C here is DSP inner loops; the default `-O0` that cgo would otherwise
use makes encode/decode roughly an order of magnitude slower for no benefit. Matches
`internal/audio/rnnoise`.

**No `-ffast-math`,** deliberately: `celt/arch.h:201` is an `#error` against building
with `-ffast-math` unless `FLOAT_APPROX` is defined, because it "could result in
crashes on extreme (e.g. NaN) input". We define neither.

**`#cgo linux LDFLAGS: -lm` — required on Linux only.** glibc splits the math
functions (`sin`, `cos`, `sqrt`, `pow`, `log`, `exp`, `lrintf`, …) into a separate
`libm` that must be linked explicitly; macOS's libSystem and mingw-w64's
msvcrt-based libc both fold them into libc, so those two legs need no LDFLAGS at all.
This is **not** a hypothetical: the identical omission broke this repo's Linux CI leg
during the RNNoise vendoring pass (`undefined reference to 'sin' / 'sincos' /
'sqrt'`, see `../rnnoise/VENDOR.md`), and libopus calls strictly more libm than
RNNoise does. The directive is GOOS-scoped rather than unscoped so that it documents
*why* at the platform boundary where it matters; it is verified inert on darwin
(everything below still builds and passes with it present).

### One define from the task brief was dropped: `-DFLOATING_POINT`

The brief's draft CFLAGS included `-DFLOATING_POINT`. **That macro does not appear
anywhere in libopus 1.5.2** — `grep -rn FLOATING_POINT` over the entire upstream tree
returns zero hits. (In Opus, the fixed/float selection is `FIXED_POINT`, by its
absence; `FLOATING_POINT` is a SILK-reference-implementation-ism that predates SILK's
merge into Opus.) It has been dropped rather than carried as a
harmless-but-meaningless define, for exactly the reason `../rnnoise/VENDOR.md` gives
for dropping `-DCOMPILE_OPUS`: a future reader would otherwise reasonably go looking
for what it does in code that never references it. Dropping it is provably a no-op —
verified by re-running the full test suite before and after with identical results.

No vendored C source was patched. Every file is byte-identical to the 1.5.2 tarball.

## `ctl_shim.c` — why a shim exists at all

`opus_encoder_ctl()` is **variadic** (`int opus_encoder_ctl(OpusEncoder *st, int
request, ...)`), and **cgo cannot call variadic C functions**. There is no way to
reach `OPUS_SET_BITRATE` / `OPUS_SET_INBAND_FEC` / `OPUS_SET_DTX` from Go directly.
`ctl_shim.c` is four lines of our own C that expose the one CTL shape we need — a
single `opus_int32` setter — as an ordinary non-variadic function:

```c
int opus_encoder_ctl_set(OpusEncoder *st, int request, opus_int32 value) {
    return opus_encoder_ctl(st, request, value);
}
```

It is our code, not vendored, and is the only non-upstream `.c` in this directory.
If a future task needs a getter, add `opus_encoder_ctl_get(…, opus_int32 *out)`
alongside it rather than reaching for `unsafe` tricks.

## The wire contract this binding encodes

48000 Hz, mono, 960 samples per frame (20 ms), `OPUS_APPLICATION_AUDIO` (2049),
48 kbps, in-band FEC **off**, DTX **off**. These are a contract with the C# peer and
the server, not tuning knobs — see the Phase 5 design doc.

**Why DTX must never be enabled** was confirmed empirically during this pass, by
encoding 200 consecutive frames of each kind and histogramming the payload sizes:

| input | DTX=1 | DTX=0 (what we ship) |
|---|---|---|
| digital silence | **1 byte** ×181, 3 bytes ×19 | 3 bytes ×200 |
| low-level noise | 68–132 bytes (mode ≈121) | 68–132 bytes (mode ≈121), identical |
| 440 Hz tone | — | 121 bytes steady state |

With DTX on, the encoder emits 1-byte frames during pauses; the server drops any
voice payload of ≤5 bytes, so the symptom would be speech that cuts out only in the
gaps — a maddening bug to chase. With DTX off the same input yields a uniform 3-byte
frame and no size-dependent behaviour on real signal at all.

**Related caveat for the Phase 5 transmit path:** note from the same table that
*even with DTX off*, a frame of bit-exact digital silence still encodes to 3 bytes,
which the server would also drop. That is Opus's own digital-silence handling, not
DTX. It is harmless (dropping silence changes nothing audible), but it means callers
must not treat "encode a zero frame" as a keepalive or heartbeat — it will not arrive.

`MaxPacket = 997` is the server's 1024-byte read buffer minus its 27-byte header. The
server **truncates** larger datagrams rather than rejecting them, so an oversized
packet would corrupt silently; the encoder is always handed a `dst` of at most this
size, which makes libopus itself do the rate-limiting. At 48 kbps a 20 ms frame is
~120 bytes, so there is roughly 8× headroom.

## Cross-platform reasoning (only the macOS leg was actually run)

This vendoring pass was performed on **Darwin 25.6.0 / arm64** with Go 1.26.1 and the
Xcode clang toolchain. Only that leg was executed. cgo cannot be cross-compiled
without a cross toolchain, and none is installed here, so the Linux and Windows legs
below are reasoned, not observed — the user pushes and watches CI.

- **macOS (arm64, this machine):** verified. Builds clean — libopus 1.5.2 emitted
  **zero compiler warnings** under `-O2` with the CFLAGS above. Takes the
  `HAVE_LRINTF`/`lrintf()` branch in `float_cast.h`. No LDFLAGS needed; libSystem
  folds in libm. macOS CI uses `macos-latest`, also arm64, same toolchain.
- **macOS (amd64):** not run. Would take the `__GNUC__ && __SSE__` branch in
  `float_cast.h` instead; `<xmmintrin.h>` ships with clang. No other divergence.
- **Linux (ubuntu-latest, amd64):** native gcc, already required for this repo's
  existing Wails GTK4/WebKitGTK cgo build — no new toolchain dependency. Takes the
  SSE branch in `float_cast.h`. **`-lm` is why this leg is expected to link**; it is
  the one platform-specific directive in the file and it was added a priori here
  precisely because its absence broke this same CI leg during the RNNoise pass.
- **Windows (windows-latest, amd64):** GitHub's runner image ships gcc 14.2.0 on
  `PATH`, which is what `CGO_ENABLED=1 go build` invokes; mingw-w64's libc folds in
  libm, so no LDFLAGS. Risks specific to this leg, none observed but none excluded:
  (a) VLA support — fine under gcc, would break only under MSVC, which cgo cannot
  use; (b) `OPUS_EXPORT` dllimport/dllexport mismatch — avoided by `-DOPUS_BUILD`
  (see above), and in any case `DLL_EXPORT` is never defined so `OPUS_EXPORT`
  collapses to a plain visibility attribute; (c) compile time — 137 translation
  units is materially slower than RNNoise's 7, and the Windows runner is the slowest
  leg. A second, untested Windows path exists: `build/windows/Taskfile.yml`'s
  `build:docker` target cross-compiles with **Zig** inside the `wails-cross` image
  when `CGO_ENABLED=1` on a non-Windows host. `zig cc` is a clang driver and should
  take these sources (C99, VLAs, `<xmmintrin.h>`) the same way clang does here, but
  that path was not exercised either.
- **`CGO_ENABLED=0` any platform:** `opus_stub.go` takes over and every constructor
  returns `errNoCGO`, with `Available()` reporting `false`. Verified locally.
  `build/windows/Taskfile.yml` defaults `CGO_ENABLED=0` for release builds, which is
  exactly why the stub matters: such a build must still link and run, just without
  voice. Phase 5 callers must gate on `opus.Available()`.
