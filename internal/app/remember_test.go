package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/config"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
)

// failingConnectSession is a sessionAPI whose Connect fails.
type failingConnectSession struct{ fakeControlSession }

func (*failingConnectSession) Connect(context.Context, string, string, string, string) error {
	return errors.New("refused")
}

func TestConnect_RemembersAddressOnSuccess(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, cfgPath)
	a.sess = &fakeControlSession{}
	a.settings.mu.Lock()
	serverURL := a.settings.cfg.ServerURL
	a.settings.mu.Unlock()

	if err := a.Connect("srv.example:5002", "n", "hunter2", "VG12"); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := a.GetLastServerAddress(); got != "srv.example:5002" {
		t.Fatalf("GetLastServerAddress = %q", got)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(raw), `last_server_address = "srv.example:5002"`) {
		t.Fatalf("config.toml lacks the remembered address:\n%s", raw)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("config.toml contains the password")
	}
	got, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerURL != serverURL {
		t.Fatalf("server_url changed %q -> %q", serverURL, got.ServerURL)
	}
}

func TestConnect_FailureDoesNotRemember(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, cfgPath)
	a.sess = &failingConnectSession{}
	if err := a.Connect("srv.example:5002", "n", "p", "u"); err == nil {
		t.Fatal("want error")
	}
	if got := a.GetLastServerAddress(); got != "" {
		t.Fatalf("a failed connect remembered %q", got)
	}
}

func TestConnect_PersistFailureDoesNotFailConnect(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	a, _, _ := newTestAppWithPath(t, cfgPath)
	a.sess = &fakeControlSession{}
	a.settings.save = func(string, *config.Config) error { return errors.New("disk full") }
	if err := a.Connect("srv.example:5002", "n", "p", "u"); err != nil {
		t.Fatalf("a persist failure failed Connect: %v", err)
	}
}

func TestGetLastServerAddress_NoSettings(t *testing.T) {
	a := NewForTest(state.New(), &fakeControlSession{}, nil)
	if got := a.GetLastServerAddress(); got != "" {
		t.Fatalf("GetLastServerAddress = %q with no settings backend", got)
	}
}
