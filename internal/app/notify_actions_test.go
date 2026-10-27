package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/notify"
	"github.com/FPGSchiba/vcs-srs-client/internal/session"
	"github.com/FPGSchiba/vcs-srs-client/srspb"
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

// failingSendSession is a fakeControlSession whose UpdateRadioInfo push
// fails, as it does while the control link is down.
type failingSendSession struct {
	fakeControlSession
}

func (f *failingSendSession) UpdateRadioInfo(context.Context, *srspb.RadioInfo) error {
	return errors.New("not connected")
}

// onlyItem asserts the snapshot holds exactly one item with key and title.
func onlyItem(t *testing.T, n *notify.Notifier, key, title string) notify.Item {
	t.Helper()
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Key != key || items[0].Title != title {
		t.Fatalf("items = %+v, want one %q / %q", items, key, title)
	}
	return items[0]
}

func TestSelectRadioPersistFailureRaisesAction(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	if err := a.SelectRadio(3); err == nil {
		t.Fatal("want error")
	}
	onlyItem(t, n, "action:radio.select", "Radio selected, but the selection was not saved")
	if got := a.st.SelectedRadio(); got != 3 {
		t.Fatalf("SelectedRadio = %d, want 3 (applied but not saved)", got)
	}
}

func TestUpdateRadioInfoPersistFailureRaisesActionAndDoesNotSend(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	fc := &fakeControlSession{}
	a.sess = fc
	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 130.000, Enabled: true},
	}}); err == nil {
		t.Fatal("want error")
	}
	onlyItem(t, n, "action:radio.save", "Could not save the radio change")
	if got := fc.updateCount(); got != 0 {
		t.Fatalf("updateCount = %d, want 0 (a failed persist must not send)", got)
	}
}

func TestUpdateRadioInfoSendFailureRaisesAction(t *testing.T) {
	a, _, _ := newTestApp(t)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	a.sess = &failingSendSession{}
	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 130.000, Enabled: true},
	}}); err == nil {
		t.Fatal("want error")
	}
	it := onlyItem(t, n, "action:radio.send", "Radio change saved, but not sent to the server")
	if it.Body != "not connected" {
		t.Fatalf("Body = %q, want %q", it.Body, "not connected")
	}
}

func TestUpdateRadioInfoSuccessResolvesBothKeys(t *testing.T) {
	a, _, _ := newTestApp(t)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	a.reportAction(actRadioSave, errors.New("x"))
	a.reportAction(actRadioSend, errors.New("x"))
	a.sess = &fakeControlSession{}
	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 130.000, Enabled: true},
	}}); err != nil {
		t.Fatal(err)
	}
	items := n.Snapshot().Items
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	for _, it := range items {
		if !it.Resolved {
			t.Fatalf("item %q not resolved: %+v", it.Key, it)
		}
	}
}

func TestResetLayoutFailureRaisesAction(t *testing.T) {
	a, _, _ := newTestAppWithPath(t, unwritablePath(t))
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	if err := a.ResetLayout(); err == nil {
		t.Fatal("want error")
	}
	onlyItem(t, n, "action:layout.reset", "Could not reset the layout")
}

func TestMicTestAndPreviewWithoutBackendRaise(t *testing.T) {
	a, _, _ := newTestApp(t)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	if err := a.StartMicTest(); err == nil {
		t.Fatal("StartMicTest: want error without an audio backend")
	}
	if err := a.PreviewEffect("tx_start"); err == nil {
		t.Fatal("PreviewEffect: want error without an audio backend")
	}
	got := map[string]bool{}
	for _, it := range n.Snapshot().Items {
		got[it.Key] = true
	}
	if len(got) != 2 || !got["action:audio.mictest"] || !got["action:audio.preview"] {
		t.Fatalf("keys = %v, want action:audio.mictest and action:audio.preview", got)
	}
}

func assertDeadlineWithin(t *testing.T, ctx context.Context, start time.Time, max time.Duration) {
	t.Helper()
	if ctx == nil {
		t.Fatal("no call recorded")
	}
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("the RPC context has no deadline")
	}
	if dl.Sub(start) > max {
		t.Fatalf("deadline %v after the call, want <= %v", dl.Sub(start), max)
	}
}

// GO-03: radio pushes are bounded at 5 s (D1).
func TestUpdateRadioInfo_HasDeadline(t *testing.T) {
	a, _, _ := newTestApp(t)
	fs := &fakeControlSession{}
	a.sess = fs
	start := time.Now()
	if err := a.UpdateRadioInfo(RadioInfoDTO{Radios: []RadioDTO{
		{ID: 1, Name: "Radio 1", Frequency: 130.000, Enabled: true},
	}}); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, fs.lastCtx(), start, 5*time.Second)
}

func TestPushPersistedRadios_HasDeadline(t *testing.T) {
	a, _, _ := newTestApp(t)
	fs := &fakeControlSession{}
	a.sess = fs
	start := time.Now()
	if err := a.Connect("localhost:5002", "n", "p", "u"); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, fs.lastCtx(), start, 5*time.Second)
}

// A session the server ended has already told the user; a stale "not sent
// to the server" toast after re-login would be noise.
func TestReportAction_SessionEndedResolves(t *testing.T) {
	a, _, _ := newTestApp(t)
	n := notify.New(notify.Options{})
	a.setNotifier(n)
	a.reportAction(actRadioSend, errors.New("plain"))
	a.reportAction(actRadioSend, fmt.Errorf("x: %w", session.ErrSessionEnded))
	items := n.Snapshot().Items
	if len(items) != 1 || items[0].Key != "action:radio.send" || !items[0].Resolved {
		t.Fatalf("items = %+v, want the one radio.send item resolved", items)
	}
}

// hookSession is a sessionAPI that also accepts the session-ended hook.
type hookSession struct {
	fakeControlSession
	hook func(string)
}

func (h *hookSession) SetSessionEndedHook(fn func(string)) { h.hook = fn }

func TestSetBackend_InstallsSessionEndedHook(t *testing.T) {
	a, _, _ := newTestApp(t)
	hs := &hookSession{}
	SetBackend(a, hs, nil)
	if hs.hook == nil {
		t.Fatal("SetBackend did not install the session-ended hook")
	}
	live := &fakeVoiceSession{}
	a.voice.mu.Lock()
	a.voice.sess = live
	a.voice.mu.Unlock()

	hs.hook("kicked")

	if a.voiceSession() != nil {
		t.Fatal("voice still live after the server ended the session")
	}
}
