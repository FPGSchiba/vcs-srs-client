package app_test

import (
	"context"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	"github.com/FPGSchiba/vcs-srs-client/internal/windowstate"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

type fakeSession struct {
	connected bool
	lastURL   string
}

func (f *fakeSession) Connect(_ context.Context, url, _, _, _ string) error {
	f.connected = true
	f.lastURL = url
	return nil
}
func (f *fakeSession) Disconnect(_ context.Context) error                          { f.connected = false; return nil }
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
