package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

type fakeSession struct {
	connected       bool
	lastURL         string
	disconnects     int
	disconnectCtx   context.Context
	disconnectError error
}

func (f *fakeSession) Connect(_ context.Context, url, _, _, _ string) error {
	f.connected = true
	f.lastURL = url
	return nil
}
func (f *fakeSession) Disconnect(ctx context.Context) error {
	f.connected = false
	f.disconnects++
	f.disconnectCtx = ctx
	return f.disconnectError
}

func (f *fakeSession) Reconnect(_ context.Context) error                           { return nil }
func (f *fakeSession) UpdateRadioInfo(_ context.Context, _ *srspb.RadioInfo) error { return nil }

type fakeWindows struct{ opened []string }

func (f *fakeWindows) Open(id string)                           { f.opened = append(f.opened, id) }
func (f *fakeWindows) Close(string)                             {}
func (f *fakeWindows) Toggle(id string)                         { f.opened = append(f.opened, id) }
func (f *fakeWindows) OpenWindows() []string                    { return nil }
func (f *fakeWindows) Geometry(string) windowstate.Geometry     { return windowstate.Geometry{} }
func (f *fakeWindows) SetGeometry(string, windowstate.Geometry) {}

func TestBinding_ConnectDelegates(t *testing.T) {
	fs := &fakeSession{}
	a := app.NewForTest(state.New(), fs, &fakeWindows{})
	if err := a.Connect("localhost:5002", "n", "p", "u"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !fs.connected || fs.lastURL != "localhost:5002" {
		t.Fatalf("Connect did not delegate: %+v", fs)
	}
}

func TestBinding_OpenWindowDelegates(t *testing.T) {
	fw := &fakeWindows{}
	a := app.NewForTest(state.New(), &fakeSession{}, fw)
	a.OpenWindow("comms")
	if len(fw.opened) != 1 || fw.opened[0] != "comms" {
		t.Fatalf("OpenWindow did not delegate: %+v", fw.opened)
	}
}

func TestBinding_GetClientState_ReturnsSnapshot(t *testing.T) {
	st := state.New()
	a := app.NewForTest(st, &fakeSession{}, &fakeWindows{})
	snap := a.GetClientState()
	if snap.Clients == nil || snap.Radios == nil {
		t.Fatalf("expected non-nil maps, got %+v", snap)
	}
}

// TestServiceShutdown_DisconnectsCleanly is the I3 guard. The clean leave used
// to hang off the tray menu's Quit item alone, so Cmd+Q and an ordinary
// window close with minimize_to_tray off (the normal quit on Windows and
// Linux) both dropped the stream instead. ServiceShutdown is the one hook
// every quit path runs through.
func TestServiceShutdown_DisconnectsCleanly(t *testing.T) {
	fs := &fakeSession{}
	a := app.NewForTest(state.New(), fs, &fakeWindows{})

	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	if fs.disconnects != 1 {
		t.Fatalf("Disconnect called %d times on shutdown, want 1", fs.disconnects)
	}
}

// TestServiceShutdown_DisconnectIsBounded is M1: a context.Background()
// disconnect against an unresponsive server would hang the quit forever.
func TestServiceShutdown_DisconnectIsBounded(t *testing.T) {
	fs := &fakeSession{}
	a := app.NewForTest(state.New(), fs, &fakeWindows{})

	if err := a.ServiceShutdown(); err != nil {
		t.Fatalf("ServiceShutdown: %v", err)
	}
	deadline, ok := fs.disconnectCtx.Deadline()
	if !ok {
		t.Fatal("the shutdown disconnect must carry a deadline, or an unresponsive server hangs quit forever")
	}
	if d := time.Until(deadline); d <= 0 || d > 2*time.Second {
		t.Errorf("shutdown disconnect deadline is %v away, want (0, 2s]", d)
	}
}

// TestServiceShutdown_SurvivesDisconnectFailure: a server that refuses the
// leave must not block the quit.
func TestServiceShutdown_SurvivesDisconnectFailure(t *testing.T) {
	fs := &fakeSession{disconnectError: errors.New("server gone")}
	a := app.NewForTest(state.New(), fs, &fakeWindows{})

	if err := a.ServiceShutdown(); err != nil {
		t.Errorf("ServiceShutdown() = %v, want nil even when the disconnect fails", err)
	}
}

// TestServiceShutdown_NoSession: shutdown before any backend was wired must
// not panic.
func TestServiceShutdown_NoSession(t *testing.T) {
	a := app.NewForTest(state.New(), nil, &fakeWindows{})
	if err := a.ServiceShutdown(); err != nil {
		t.Errorf("ServiceShutdown() = %v, want nil with no session", err)
	}
}
