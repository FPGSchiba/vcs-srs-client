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
