package app

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
)

func TestSetSettingsFailureRaisesActionNotification(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	if err := a.SetSettings(a.GetSettings()); err == nil {
		t.Fatal("want error")
	}
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Key != "action:settings.save" ||
		items[0].Title != "Could not save settings" || items[0].Category != "system" ||
		items[0].Severity != notify.SeverityError || items[0].Body == "" {
		t.Fatalf("items = %+v", items)
	}
}

func TestReportActionDedupesIdenticalFailure(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	var publishes atomic.Int32
	a.setNotifier(notify.New(notify.Options{OnChange: func(notify.Snapshot) { publishes.Add(1) }}))
	_ = a.SetSettings(a.GetSettings())
	_ = a.SetSettings(a.GetSettings())
	if got := publishes.Load(); got != 1 {
		t.Fatalf("publishes = %d, want 1 (fingerprint dedupe)", got)
	}
}

func TestSetSettingsSuccessResolvesThenRefailureIsNews(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfgdir")
	a, _, _ := newTestAppWithPath(t, filepath.Join(dir, "config.toml"))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	_ = a.SetSettings(a.GetSettings()) // dir missing -> fail
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSettings(a.GetSettings()); err != nil {
		t.Fatal(err)
	}
	if it := n.Snapshot().Items[0]; !it.Resolved {
		t.Fatalf("success must resolve: %+v", it)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	_ = a.SetSettings(a.GetSettings())
	if it := n.Snapshot().Items[0]; it.Resolved || !it.Unread {
		t.Fatalf("a recurrence after a success must be news: %+v", it)
	}
}

func TestReportActionWithoutNotifierIsNoop(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	_ = a.SetSettings(a.GetSettings()) // must not panic
}

func TestActionNotifyRunsOutsideSettingsLocks(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	a.setNotifier(notify.New(notify.Options{OnChange: func(notify.Snapshot) {
		// Re-enter both settings locks from the publish callback.
		a.settings.writeMu.Lock()
		a.settings.writeMu.Unlock()
		_ = a.GetSettings()
	}}))
	done := make(chan struct{})
	go func() { _ = a.SetSettings(a.GetSettings()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("deadlock: reportAction ran under writeMu")
	}
}
