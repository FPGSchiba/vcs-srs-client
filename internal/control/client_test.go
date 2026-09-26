package control_test

import (
	"context"
	"strings"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/control"
	"github.com/FPGSchiba/vcs-srs-client/internal/grpctest"
	"github.com/FPGSchiba/vcs-srs-client/internal/state"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestSyncClient_HydratesStore(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncClients = map[string]*srspb.ClientInfo{"g1": {Name: "Alice"}}
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	c := control.New(conn, "tok")
	if err := c.SyncClient(context.Background(), st); err != nil {
		t.Fatalf("SyncClient: %v", err)
	}
	if _, ok := st.Client("g1"); !ok {
		t.Fatal("expected store hydrated with g1")
	}
}

// TestSyncClient_CapturesVoiceCredentials pins that SyncClient stops
// discarding voice_secret, coalition_voice_addr and global_voice_addr --
// today (pre-fix) they never reach the store.
func TestSyncClient_CapturesVoiceCredentials(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncCoalitionVoiceAddr = "10.0.0.9:5002"
	f.SyncGlobalVoiceAddr = "10.0.0.1:5002"
	f.SyncVoiceSecret = strings.Repeat("C", 43)
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	c := control.New(conn, "tok")
	if err := c.SyncClient(context.Background(), st); err != nil {
		t.Fatalf("SyncClient: %v", err)
	}

	secret, coal, global := st.VoiceCredentials()
	if secret != strings.Repeat("C", 43) {
		t.Errorf("secret = %q", secret)
	}
	if coal != "10.0.0.9:5002" {
		t.Errorf("coalition addr = %q, want %q", coal, "10.0.0.9:5002")
	}
	if global != "10.0.0.1:5002" {
		t.Errorf("global addr = %q, want %q", global, "10.0.0.1:5002")
	}
}

// TestSyncClient_EmptyVoiceAddrsAreStoredAsIs pins that a standalone
// server's "" coalition/global addrs come through untouched -- SyncClient
// must not invent a default.
func TestSyncClient_EmptyVoiceAddrsAreStoredAsIs(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncVoiceSecret = strings.Repeat("D", 43)
	// SyncCoalitionVoiceAddr / SyncGlobalVoiceAddr left unset ("").
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	c := control.New(conn, "tok")
	if err := c.SyncClient(context.Background(), st); err != nil {
		t.Fatalf("SyncClient: %v", err)
	}

	_, coal, global := st.VoiceCredentials()
	if coal != "" {
		t.Errorf("coalition addr = %q, want empty", coal)
	}
	if global != "" {
		t.Errorf("global addr = %q, want empty", global)
	}
}

func TestUpdateRadioInfo_SendsPayload(t *testing.T) {
	f := grpctest.NewFake()
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())
	c := control.New(conn, "tok")

	err := c.UpdateRadioInfo(context.Background(), &srspb.RadioInfo{
		Radios: []*srspb.Radio{{Id: 1, Name: "Fleet", Frequency: 118.5, Enabled: true}},
	})
	if err != nil {
		t.Fatalf("UpdateRadioInfo: %v", err)
	}
	got := f.LastRadioInfo()
	if got == nil || len(got.GetRadios()) != 1 || got.GetRadios()[0].GetName() != "Fleet" {
		t.Fatalf("server did not receive radio: %+v", got)
	}
}
