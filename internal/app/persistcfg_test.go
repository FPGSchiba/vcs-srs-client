package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
)

// parkedSave installs a save seam that signals entry and then blocks until
// the returned release func is called. Every call after the first passes
// straight through to the real config.Save.
func parkedSave(t *testing.T, sb *settingsBackend) (entered <-chan struct{}, release func()) {
	t.Helper()
	in := make(chan struct{})
	gate := make(chan struct{})
	first := true
	sb.save = func(path string, c *config.Config) error {
		if first {
			first = false
			close(in)
			<-gate
		}
		return config.Save(path, c)
	}
	var released bool
	rel := func() {
		if !released {
			released = true
			close(gate)
		}
	}
	t.Cleanup(rel)
	return in, rel
}

func waitSaveEntered(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// While config.toml is on disk, sb.mu must be free: the PTT path
// (resolveTXTarget) takes it on every press.
func TestPersistCfgDoesNotHoldMuDuringSave(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, filepath.Join(t.TempDir(), "config.toml"))
	sb := a.settings
	entered, release := parkedSave(t, sb)

	errc := make(chan error, 1)
	go func() {
		errc <- a.persistRadios([]RadioDTO{{ID: 1, Name: "r1", Frequency: 251.0, Enabled: true}})
	}()
	waitSaveEntered(t, entered, "persistRadios to reach the save")

	if !sb.mu.TryLock() {
		release()
		t.Fatal("sb.mu is held while config.toml is being written")
	}
	sb.mu.Unlock()

	done := make(chan struct{})
	go func() { a.resolveTXTarget("radio.1.ptt"); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		release()
		t.Fatal("resolveTXTarget blocked behind a config write")
	}
	release()
	if err := <-errc; err != nil {
		t.Fatalf("persistRadios: %v", err)
	}
}

// A layout drag landing while another write is on disk must survive that
// write's commit (no lost update).
func TestPersistCfgKeepsConcurrentLayoutEdit(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, filepath.Join(t.TempDir(), "config.toml"))
	sb := a.settings
	entered, release := parkedSave(t, sb)

	errc := make(chan error, 1)
	go func() {
		errc <- a.persistRadios([]RadioDTO{{ID: 7, Name: "r7", Frequency: 251.0, Enabled: true}})
	}()
	waitSaveEntered(t, entered, "persistRadios to reach the save")

	layoutDone := make(chan error, 1)
	go func() {
		layoutDone <- a.SetCommsLayout(LayoutDTO{Blocks: []ProfileBlockDTO{{RadioID: 7, Variant: "compact"}}})
	}()
	select {
	case err := <-layoutDone:
		if err != nil {
			t.Fatalf("SetCommsLayout: %v", err)
		}
	case <-time.After(time.Second):
		release()
		t.Fatal("SetCommsLayout blocked behind a config write")
	}
	release()
	if err := <-errc; err != nil {
		t.Fatalf("persistRadios: %v", err)
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()
	if len(sb.cfg.Radios) != 1 || sb.cfg.Radios[0].ID != 7 {
		t.Fatalf("radios = %+v, want the persisted radio 7", sb.cfg.Radios)
	}
	if b := sb.cfg.CommsLayout.Blocks; len(b) != 1 || b[0].RadioID != 7 || b[0].Variant != "compact" {
		t.Fatalf("layout blocks = %+v; the drag that landed during the save was lost", b)
	}
	if !sb.layoutPending {
		t.Fatal("layoutPending cleared; the drag is not on disk yet and must still be flushed")
	}
}

func TestPersistCfgFailureLeavesCfgUnchanged(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, filepath.Join(t.TempDir(), "config.toml"))
	sb := a.settings
	sb.save = func(string, *config.Config) error { return errors.New("disk full") }

	sb.mu.Lock()
	before := sb.cfg
	beforeName := sb.cfg.ProfilesDir
	sb.mu.Unlock()

	committed := false
	ok, err := sb.persistCfg(cfgTxn{
		mutate:   func(c *config.Config) { c.ProfilesDir = "elsewhere" },
		onCommit: func() { committed = true },
	})
	if err == nil || ok {
		t.Fatalf("persistCfg = (%v, %v), want (false, error)", ok, err)
	}
	if committed {
		t.Fatal("onCommit ran after a failed save")
	}
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if sb.cfg != before || sb.cfg.ProfilesDir != beforeName {
		t.Fatalf("sb.cfg changed after a failed save (ProfilesDir = %q)", sb.cfg.ProfilesDir)
	}
}

func TestPersistCfgCheckAbortSkipsSave(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, filepath.Join(t.TempDir(), "config.toml"))
	sb := a.settings
	calls := 0
	sb.save = func(string, *config.Config) error { calls++; return nil }

	ok, err := sb.persistCfg(cfgTxn{
		check:  func(*config.Config) bool { return false },
		mutate: func(c *config.Config) { c.ProfilesDir = "elsewhere" },
	})
	if ok || err != nil {
		t.Fatalf("persistCfg = (%v, %v), want (false, nil)", ok, err)
	}
	if calls != 0 {
		t.Fatalf("save called %d times after check aborted", calls)
	}
}
