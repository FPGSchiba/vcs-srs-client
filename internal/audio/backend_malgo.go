//go:build cgo

package audio

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

// malgoBackend is the real device layer.
//
// SHARED MODE, NEVER EXCLUSIVE (spec D6). The user is running Star Citizen
// at the same time; taking exclusive hold of the microphone or output would
// break the game's audio exactly the way DISCL_EXCLUSIVE would have broken
// its force feedback in Phase 3.5. malgo.ShareMode's zero value is
// malgo.Shared (see enumerations.go), and DefaultDeviceConfig never touches
// the field, so leaving Capture.ShareMode / Playback.ShareMode unset below
// -- rather than assigning malgo.Exclusive -- is what keeps this shared.
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

// setDeviceID resolves a persisted device ID string to the malgo.DeviceID
// miniaudio needs to open that specific device.
//
// An empty id leaves the config's device pointer nil, which is miniaudio's
// spelling for "use the system default" -- DeviceInfo.ID being the empty
// value is documented (backend.go) as a first-class "follow the default"
// choice, not an absent one, so this is not a fallback path, it is the
// literal meaning of that value.
//
// A non-empty id is matched by string against Enumerate's results for the
// given device kind so we can recover the malgo.DeviceID needed by
// DeviceConfig.{Capture,Playback}.DeviceID. If nothing matches -- the
// device was unplugged since it was persisted -- we return an error rather
// than silently falling back to the default, so the caller's retry/fallback
// policy (Task 10) decides what happens next instead of us deciding for it.
func (b *malgoBackend) setDeviceID(cfg *malgo.DeviceConfig, kind malgo.DeviceType, id string) error {
	if id == "" {
		return nil
	}

	devices, err := b.ctx.Devices(kind)
	if err != nil {
		return fmt.Errorf("audio: enumerate for device id %q: %w", id, err)
	}
	for i := range devices {
		if devices[i].ID.String() != id {
			continue
		}
		ptr := devices[i].ID.Pointer()
		if kind == malgo.Capture {
			cfg.Capture.DeviceID = ptr
		} else {
			cfg.Playback.DeviceID = ptr
		}
		return nil
	}
	return fmt.Errorf("audio: device %q not found", id)
}

// maxDeviceFrames bounds the pre-allocated byte<->float32 conversion
// scratch buffer. PeriodSizeInFrames (FrameSamples, 480) is only a hint to
// miniaudio, so a callback's actual frame count can differ -- but backends
// don't hand out chunks anywhere near this large in practice. Sizing the
// scratch buffer generously here means the accumulate/fill adaptation below
// (frameAccumulator/frameFiller) never has to grow anything at callback
// time; a chunk that somehow exceeded this bound would be clamped rather
// than crash, which is an intentionally defensive edge far outside any
// observed hardware behavior, not the mismatch this fix targets.
const maxDeviceFrames = 65536

func (b *malgoBackend) OpenCapture(id string, onFrame func([]float32)) (Stream, error) {
	cfg := b.deviceConfig(malgo.Capture)
	if err := b.setDeviceID(&cfg, malgo.Capture, id); err != nil {
		return nil, err
	}
	// Scratch buffers owned by the callback goroutine; allocated ONCE here
	// so the realtime path never allocates. acc re-slices whatever length
	// miniaudio actually delivers into exact FrameSamples-sized frames --
	// see frame_buffer.go -- so onFrame's "exactly FrameSamples" contract
	// holds regardless of what the period-size hint actually yields.
	scratch := make([]float32, maxDeviceFrames)
	acc := newFrameAccumulator(FrameSamples, onFrame)
	dev, err := malgo.InitDevice(b.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			n := int(frames)
			if n > len(scratch) {
				n = len(scratch)
			}
			bytesToFloat32(in, scratch[:n])
			acc.push(scratch[:n])
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
	if err := b.setDeviceID(&cfg, malgo.Playback, id); err != nil {
		return nil, err
	}
	// Same discipline as OpenCapture: filler serves the hardware's
	// requested length out of fixed FrameSamples-sized frames it pulls
	// from fill on demand, so the entire out buffer is always written in
	// full even when frames != FrameSamples.
	scratch := make([]float32, maxDeviceFrames)
	filler := newFrameFiller(FrameSamples, fill)
	dev, err := malgo.InitDevice(b.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(out, _ []byte, frames uint32) {
			n := int(frames)
			if n > len(scratch) {
				n = len(scratch)
			}
			filler.pull(scratch[:n])
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

type malgoStream struct {
	dev *malgo.Device

	// stopOnce guards teardown. backend.go's Stream.Stop contract says
	// "idempotent", and idempotent must mean safe under CONCURRENT
	// invocation here, not merely safe to call twice in sequence: a plain
	// "if s.dev == nil { return }" nil-check has a window where two
	// goroutines both read a non-nil s.dev before either clears it, so
	// both reach dev.Uninit() -> ma_device_uninit + ma_free on the same C
	// pointer -- a double-free that crashes the process, not a Go panic.
	// sync.Once makes exactly one caller run the teardown body; every
	// other concurrent (or later) caller blocks until it finishes and
	// then returns without touching dev again.
	stopOnce sync.Once
}

func (s *malgoStream) Stop() error {
	s.stopOnce.Do(func() {
		_ = s.dev.Stop()
		s.dev.Uninit()
	})
	return nil
}

// bytesToFloat32 reinterprets the raw PCM bytes miniaudio hands the capture
// callback (already f32-native, since deviceConfig requests malgo.FormatF32)
// as float32 samples and copies them into dst.
//
// This mirrors malgo's own idiom in device.go's goDataCallback, which builds
// the []byte malgo hands us the same way: unsafe.Slice over a pointer cast,
// not encoding/binary -- there is no endian conversion to do because the
// bytes are already this machine's native float32 layout.
func bytesToFloat32(src []byte, dst []float32) {
	if len(src) == 0 || len(dst) == 0 {
		return
	}
	n := len(src) / 4
	if n > len(dst) {
		n = len(dst)
	}
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&src[0])), n)
	copy(dst, samples)
}

// float32ToBytes is the inverse of bytesToFloat32: it writes src's samples
// into dst's raw bytes via the same unsafe.Slice-over-pointer-cast idiom.
func float32ToBytes(src []float32, dst []byte) {
	if len(src) == 0 || len(dst) == 0 {
		return
	}
	n := len(dst) / 4
	if n > len(src) {
		n = len(src)
	}
	samples := unsafe.Slice((*float32)(unsafe.Pointer(&dst[0])), n)
	copy(samples, src)
}
