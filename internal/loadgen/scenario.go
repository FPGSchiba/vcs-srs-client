package loadgen

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
	"github.com/FPGSchiba/vcs-srs-client/srspb"
)

//go:embed scenarios/one-big-net.json
var oneBigNet []byte

// Scenario describes one load run.
type Scenario struct {
	Name      string  `json:"name"`
	Version   int     `json:"version"`
	Seed      int64   `json:"seed"`
	Coalition string  `json:"coalition"`
	FreqMHz   float32 `json:"freq_mhz"`
}

// LoadScenario loads the embedded scenario of that name, else reads a file path.
// Unknown keys are an error.
func LoadScenario(nameOrPath string) (Scenario, error) {
	var data []byte
	if nameOrPath == "one-big-net" {
		data = oneBigNet
	} else {
		var err error
		if data, err = os.ReadFile(nameOrPath); err != nil {
			return Scenario{}, fmt.Errorf("loadgen: read scenario: %w", err)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		return Scenario{}, fmt.Errorf("loadgen: decode scenario %q: %w", nameOrPath, err)
	}
	if dec.More() {
		return Scenario{}, fmt.Errorf("loadgen: scenario %q: trailing data after JSON object", nameOrPath)
	}
	if s.Coalition == "" {
		return Scenario{}, fmt.Errorf("loadgen: scenario %q: coalition is empty", nameOrPath)
	}
	if s.FreqKHz() <= 0 {
		return Scenario{}, fmt.Errorf("loadgen: scenario %q: freq_mhz %v is not a positive frequency", nameOrPath, s.FreqMHz)
	}
	return s, nil
}

// FreqKHz is the scenario frequency on the canonical kHz grid.
func (s Scenario) FreqKHz() voice.KHz { return voice.KHzFromMHz32(s.FreqMHz) }

// CheckAgainstServer refuses a frequency the server treats as global or test
// (D12): those bypass the per-frequency relay path a load run must exercise.
func (s Scenario) CheckAgainstServer(st *srspb.ServerSettings) error {
	f := s.FreqKHz().MHz32()
	for _, g := range st.GetGlobalFrequencies() {
		if g == f {
			return fmt.Errorf("loadgen: scenario frequency %v MHz is a global frequency on this server", f)
		}
	}
	for _, g := range st.GetTestFrequencies() {
		if g == f {
			return fmt.Errorf("loadgen: scenario frequency %v MHz is a test frequency on this server", f)
		}
	}
	return nil
}
