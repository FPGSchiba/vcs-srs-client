# Vendored: RNNoise

- **Upstream:** https://gitlab.xiph.org/xiph/rnnoise
- **Vendored commit:** `6cbfd53eb348a8d394e0757b4025c6ded34eb2b6` (tag `v0.1.1`, "use rnn_
  prefix for non-exported symbols", authored 2024-03-22)
- **Vendored at:** 2026-09-23
- **License:** BSD-3-Clause (Xiph.Org Foundation / Mozilla / Jean-Marc Valin / Mark
  Borgerding). Verbatim upstream `COPYING` copied to `LICENSE` in this directory.

Vendored rather than depended upon, following the `internal/joystick/di8` precedent:
it removes a supply-chain risk on the critical audio path, needs no system library or
pkg-config on any of the three platforms, and RNNoise's C API is stable. The
pre-trained model is part of the library, not a separate asset.

## Why `v0.1.1` and not upstream `main` / `v0.2`

Upstream's unreleased `main` branch (and the `v0.2` tag, 2024-04-14) rewrote the model
as an LPCNet-derived network. Its pretrained weights are **not checked into the
upstream git tree at all** — `autogen.sh` shells out to `download_model.sh`, which
fetches `rnnoise_data-<hash>.tar.gz` from `media.xiph.org` at build time. That tarball
was downloaded and inspected during this vendoring pass: the extracted
`src/rnnoise_data.c` is **78 MB** of decimal float-literal arrays (`src/rnnoise_data_little.c`,
the smaller alternative model, is still 30 MB). Committing either would (a) contradict
the brief's rationale that "the pre-trained model is part of the library, not a
separate asset" — on `main`/`v0.2` it demonstrably is a separate, separately-hashed,
separately-fetched asset — and (b) bloat this repo for a feature this task's own
constraints call "a QUALITY feature, not a correctness one."

`v0.1.1` is the last tagged release of the original (pre-LPCNet-rewrite) RNNoise: the
well-known, widely-integrated version whose pretrained model (`src/rnn_data.c`, ~415
KiB of quantized `short` weights) has been checked directly into upstream git since
2018. Its public C API (`rnnoise_create`/`rnnoise_init`/`rnnoise_process_frame`/
`rnnoise_destroy`/`rnnoise_get_frame_size`, all taking an optional `RNNModel *`) is
**byte-identical in signature** to the same functions on current upstream `main` —
confirmed by diffing `include/rnnoise.h` at both revisions — so "RNNoise's C API is
stable" holds regardless of which line is vendored, and nothing about this wrapper
would need to change if a future vendoring pass moves to the newer model.

## Files copied (flattened from upstream `src/`, plus `include/`)

```
internal/audio/rnnoise/
  denoise.c  denoise's DenoiseState + rnnoise_create/init/destroy/process_frame
  rnn.c      dense/GRU forward pass
  rnn.h
  rnn_data.c pre-trained model weights (quantized, ~415 KiB) -- generated from Keras,
             not hand-written
  rnn_data.h
  rnn_reader.c  RNNModel (de)serialization (rnnoise_model_from_file/model_free)
  pitch.c    pitch analysis feeding the RNN's feature vector
  pitch.h
  kiss_fft.c FFT (Mark Borgerding's KISS FFT, BSD-3, bundled by upstream itself)
  kiss_fft.h
  _kiss_fft_guts.h
  celt_lpc.c LPC analysis (lifted from Opus/CELT by upstream itself)
  celt_lpc.h
  arch.h        common CELT/Opus scalar-arch macros (float path only; FIXED_POINT
                and the ARM/x86 asm branches are never defined by our #cgo CFLAGS)
  common.h
  opus_types.h
  tansig_table.h  tanh() lookup table for the RNN activation
  include/rnnoise.h  public C API (RNNOISE_EXPORT resolves to nothing under our
                      build: WIN32/RNNOISE_BUILD are never defined, so there is no
                      dllexport/dllimport mismatch to worry about when statically
                      compiling into the Go binary via cgo)
  LICENSE    verbatim copy of upstream COPYING (BSD-3-Clause)
```

## Deliberately NOT copied

- `autogen.sh`, `configure.ac`, `Makefile.am`, `m4/`, `*.pc.in` — autotools
  scaffolding; this package is built entirely by `#cgo CFLAGS`/cgo, no autoreconf/make.
- `examples/rnnoise_demo.c` — demo program, not library code.
- `src/rnn_train.py`, `src/compile.sh`, `training/`, `TRAINING-README` — model
  training scripts, irrelevant to runtime inference.
- `doc/`, `README`, `AUTHORS`, `datasets.txt` — documentation, not source.
- `download_model.sh`, `model_version` — belong to the newer (`main`/`v0.2`) model
  pipeline this vendoring pass deliberately did not adopt (see above).

## Adjustments needed to build

All seven `.c` files compile as portable C99 (verified against clang on macOS, this
repo's native toolchain during this vendoring pass): every `#include "config.h"` is
guarded by `#ifdef HAVE_CONFIG_H`, which our `#cgo CFLAGS` never defines, and none of
the fixed-point (`FIXED_POINT`), ARM-NEON (`OPUS_ARM_*`), or x86-SIMD paths in
`arch.h` are reachable without macros this build never sets — this v0.1.1 tree
predates upstream's later per-arch `vec_avx.h`/`vec_neon.h`/`x86/` runtime dispatch,
so there is no architecture-specific source to select or exclude.

**One structural fix was required, beyond CFLAGS/file-set tweaking.** The brief's
draft wrapper put `import "C"` directly in `internal/audio/rnnoise.go` (package
`audio`, one directory above the vendored `.c` files) with `#cgo CFLAGS:
-I${SRCDIR}/rnnoise -I${SRCDIR}/rnnoise/include`. That CFLAGS path is correct for
finding the *headers*, but cgo does not compile arbitrary `.c` files reachable via an
include path — it only auto-compiles `.c`/`.h` files that sit in the **same
directory** as the Go file doing `import "C"`. With the vendored sources one level
down, `go build` produced object code that referenced `rnnoise_create` /
`rnnoise_process_frame` / `rnnoise_destroy` but never compiled their definitions,
failing at link time:

```
Undefined symbols for architecture arm64:
  "_rnnoise_create", referenced from: ...
  "_rnnoise_destroy", referenced from: ...
  "_rnnoise_process_frame", referenced from: ...
ld: symbol(s) not found for architecture arm64
```

Fix: split the cgo binding into its own package, `internal/audio/rnnoise`
(`rnnoise.go` in *this* directory, package `rnnoise`), colocated with the `.c`/`.h`
files it binds — `#cgo CFLAGS: -I${SRCDIR} -I${SRCDIR}/include` now resolves
relative to this directory, and the sibling `.c` files are compiled and linked as
part of that package's own build, per normal cgo rules. `internal/audio/rnnoise.go`
(package `audio`, one level up) no longer contains `import "C"` at all — it imports
this package and calls its exported `rnnoise.New()` / `(*State).ProcessFrame` /
`(*State).Close()`, doing the int16-magnitude scaling and `FrameSamples` validation
itself. The public API this task's brief specifies
(`audio.NewDenoiser`/`Process`/`Available`/`Close`) is unchanged; only the internal
split changed. This is the same reason other cgo-vendoring Go projects (e.g.
`mattn/go-sqlite3`) keep bound `.c` sources directly alongside the `import "C"` file
that binds them, rather than in a location only reachable via an include path.

The brief's suggested `#cgo CFLAGS` also included `-DCOMPILE_OPUS`. That macro does not
appear anywhere in the `v0.1.1` sources vendored here (it belongs to the newer,
not-vendored upstream tree, where RNNoise's build can optionally fold in Opus's own
CELT/pitch code instead of RNNoise's bundled copies). It has been **dropped** from
the wrapper's `#cgo CFLAGS` rather than carried over as a harmless-but-meaningless
define, since a future reader would otherwise reasonably go looking for what it does
in code that never references it.

## Cross-platform compilation reasoning (no CI run performed by this task; see report)

- **Linux (ubuntu-latest):** native gcc, already required for the existing Wails
  GTK4/WebKitGTK cgo build in this repo's CI job. No new toolchain dependency.
- **macOS (macos-latest):** native Xcode clang. Verified directly on this machine
  (Darwin/arm64, Xcode toolchain) — see report for the exact build/test transcript.
- **Windows (windows-latest):** GitHub's `windows-latest` runner image ships
  `gcc 14.2.0` pre-installed and (per `actions/runner-images`' own Windows2022 readme,
  which lists it under "Tools" rather than the explicitly-flagged-as-"not on PATH"
  MSYS2/Miniconda entries) on `PATH`, so `go build` with `CGO_ENABLED=1` (native,
  non-cross-compiled Windows CI matrix leg) should invoke it without an extra
  install step. This was **not** empirically run on a Windows runner as part of this
  task — the user pushes and watches CI themselves. `build/windows/Taskfile.yml`
  defaults `CGO_ENABLED=0` for release builds, which is exactly why
  `rnnoise_stub.go`'s no-op behavior matters: a `CGO_ENABLED=0` Windows release build
  must still link and run, just without noise suppression.
