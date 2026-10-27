package windowstate_test

import (
	"os"
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

// A failed rename must not leave windows.json.tmp behind: the next save
// would overwrite it anyway, but a stray .tmp next to the real file is
// exactly the kind of debris that makes a user doubt which one is current.
func TestSaveRemovesTmpOnRenameFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.json")
	// A non-empty directory at the target path makes the rename fail on
	// every OS while the tmp write itself still succeeds.
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := windowstate.Save(path, map[string]windowstate.Geometry{"comms": {W: 1, H: 1}})
	if err == nil {
		t.Fatal("Save onto a non-empty directory succeeded; want an error")
	}
	if _, statErr := os.Stat(path + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("windows.json.tmp left behind after a failed save (stat err = %v)", statErr)
	}
}
