package control_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	if _, err := c.SyncClient(context.Background(), st); err != nil {
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
	if _, err := c.SyncClient(context.Background(), st); err != nil {
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
	if _, err := c.SyncClient(context.Background(), st); err != nil {
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

func TestSyncClient_ReplacesRoster(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncClients = map[string]*srspb.ClientInfo{"a": {Name: "A"}}
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	st.UpdateClient("ghost", &srspb.ClientInfo{Name: "Ghost"})
	st.SetRadios("ghost", &srspb.RadioInfo{})
	if _, err := control.New(conn, "tok").SyncClient(context.Background(), st); err != nil {
		t.Fatalf("SyncClient: %v", err)
	}
	snap := st.Snapshot()
	if len(snap.Clients) != 1 || snap.Clients["a"] == nil {
		t.Fatalf("clients = %v, want only a", snap.Clients)
	}
	if _, ok := snap.Radios["ghost"]; ok {
		t.Fatal("ghost radios survived SyncClient")
	}
}

func TestSyncClient_EmptySecretKeepsStoredSecret(t *testing.T) {
	f := grpctest.NewFake()
	f.SyncCoalitionVoiceAddr = "x:1"
	// SyncVoiceSecret left "".
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	st.SetVoiceCredentials("s1", "", "")
	hadSecret, err := control.New(conn, "tok").SyncClient(context.Background(), st)
	if err != nil {
		t.Fatalf("SyncClient: %v", err)
	}
	if hadSecret {
		t.Fatal("hadSecret = true for an empty voice_secret")
	}
	secret, coal, _ := st.VoiceCredentials()
	if secret != "s1" {
		t.Fatalf("secret = %q, want the stored s1 kept", secret)
	}
	if coal != "x:1" {
		t.Fatalf("coalition addr = %q, want x:1", coal)
	}
}

func TestSyncClient_FailureLeavesRoster(t *testing.T) {
	f := grpctest.NewFake()
	f.Fail("SyncClient", status.Error(codes.Internal, "boom"))
	dial, cleanup := grpctest.Start(t, f)
	defer cleanup()
	conn, _ := dial(context.Background())

	st := state.New()
	st.UpdateClient("a", &srspb.ClientInfo{Name: "A"})
	if _, err := control.New(conn, "tok").SyncClient(context.Background(), st); err == nil {
		t.Fatal("expected SyncClient to fail")
	}
	if _, ok := st.Client("a"); !ok {
		t.Fatal("a failed SyncClient must leave the roster untouched")
	}
}
