package windowstate_test

import (
	"path/filepath"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
)

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.json")
	in := map[string]windowstate.Geometry{"comms": {X: 10, Y: 20, W: 540, H: 720}}
	if err := windowstate.Save(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := windowstate.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out["comms"] != in["comms"] {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestLoad_MissingFileReturnsEmpty(t *testing.T) {
	out, err := windowstate.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty map, got %+v", out)
	}
}
