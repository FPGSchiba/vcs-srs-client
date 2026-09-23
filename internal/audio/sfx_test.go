package audio

import "testing"

func TestSFXReportsManifestSlots(t *testing.T) {
	s := NewSFX()
	ids := s.EffectIDs()
	want := []string{"tx_start", "tx_end", "rx_start", "rx_end", "intercom_start", "intercom_end", "encryption_beep"}
	if len(ids) != len(want) {
		t.Fatalf("EffectIDs() = %v, want %d slots", ids, len(want))
	}
}

func TestSFXMissingAssetIsSilentNotFatal(t *testing.T) {
	s := NewSFX()
	// No pack is vendored, so every slot is unavailable.
	if s.Available("tx_start") {
		t.Skip("an asset pack is present; this test covers the empty case")
	}
	s.Play("tx_start") // must not panic
	dst := make([]float32, FrameSamples)
	s.MixInto(dst)
	for _, v := range dst {
		if v != 0 {
			t.Fatal("a missing asset produced audio")
		}
	}
}

func TestSFXUnknownIDIsIgnored(t *testing.T) {
	s := NewSFX()
	s.Play("no_such_effect") // must not panic
}
