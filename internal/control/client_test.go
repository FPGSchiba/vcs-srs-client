package control_test

import (
	"context"
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
