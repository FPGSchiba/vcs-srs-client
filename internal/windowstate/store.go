// Package windowstate persists per-window geometry to windows.json.
package windowstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Geometry is a window's position and size in screen pixels.
type Geometry struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

// Load reads the geometry map. A missing file returns an empty map (no error).
func Load(path string) (map[string]Geometry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]Geometry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read windows.json: %w", err)
	}
	out := map[string]Geometry{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse windows.json: %w", err)
	}
	return out, nil
}

// Save writes the geometry map atomically (write-temp + rename).
func Save(path string, m map[string]Geometry) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal windows.json: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write windows.json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename windows.json: %w", err)
	}
	return nil
}
