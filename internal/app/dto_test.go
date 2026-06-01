package app_test

import (
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/app"
	srspb "github.com/FPGSchiba/vcs-srs-client/srspb"
)

func TestRadioInfoFromDTO_RoundTrip(t *testing.T) {
	dto := app.RadioInfoDTO{
		Muted:  true,
		Radios: []app.RadioDTO{{ID: 1, Name: "Fleet", Frequency: 118.5, Enabled: true, IsIntercom: false}},
	}
	pb := app.RadioInfoToProto(dto)
	if pb.GetMuted() != true || len(pb.GetRadios()) != 1 || pb.GetRadios()[0].GetName() != "Fleet" {
		t.Fatalf("to-proto mismatch: %+v", pb)
	}
	back := app.RadioInfoFromProto(pb)
	if back.Radios[0].Frequency != 118.5 || !back.Muted {
		t.Fatalf("from-proto mismatch: %+v", back)
	}
}

func TestSnapshotFromState_MapsClients(t *testing.T) {
	pb := map[string]*srspb.ClientInfo{"g1": {Name: "Al", Coalition: "VG"}}
	snap := app.SnapshotFromProto(pb, nil)
	if snap.Clients["g1"].Name != "Al" {
		t.Fatalf("unexpected: %+v", snap)
	}
}
