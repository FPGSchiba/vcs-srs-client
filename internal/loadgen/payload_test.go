package loadgen

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/FPGSchiba/vcs-srs-client/internal/voice"
)

func TestSyntheticFrame(t *testing.T) {
	f := SyntheticFrame(7)
	if len(f) != 120 || len(f) < voice.MinVoicePayload {
		t.Fatalf("len = %d", len(f))
	}
	if !bytes.Equal(f, SyntheticFrame(7)) {
		t.Fatal("not deterministic")
	}
	if bytes.Equal(f, SyntheticFrame(8)) {
		t.Fatal("seq 7 and 8 equal")
	}
	for _, seq := range []uint32{0, 1, 7, 1 << 31} {
		g := SyntheticFrame(seq)
		allEq := true
		for _, b := range g[5:] {
			if b != g[4] {
				allEq = false
			}
		}
		if allEq {
			t.Fatalf("seq %d: bytes 4+ all equal", seq)
		}
	}
	if binary.BigEndian.Uint32(f[:4]) != 7 {
		t.Fatal("seq prefix wrong")
	}
	if SyntheticFrame(0)[4] == 0 && SyntheticFrame(0)[5] == 0 && SyntheticFrame(0)[6] == 0 {
		t.Fatal("seq 0 degenerate xorshift")
	}
}
