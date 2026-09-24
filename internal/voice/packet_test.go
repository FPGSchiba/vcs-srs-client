package voice

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// goldenSender is a fixed UUID so the golden byte vector below is stable.
var goldenSender = uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")

// TestVoicePacketGoldenBytes pins the exact 27-byte header layout against
// BOTH peers: the Go server's voice/protocol.go SerializePacket and the C#
// VcsVoicePacket.EncodePacket. Every offset here was read from those two
// implementations, not inferred.
//
// The UUID bytes are the RFC 4122 big-endian form. This matters: .NET's
// Guid.ToByteArray() is little-endian in its first three fields, which is
// why the C# side hand-converts in both directions. Go's uuid.UUID is
// already RFC 4122 ordered, so MarshalBinary needs no conversion -- but a
// future refactor that reaches for Guid-style ordering would break interop
// silently, and this vector is what catches it.
func TestVoicePacketGoldenBytes(t *testing.T) {
	p := NewVoice(goldenSender, 0x0102_03, KHz(251300), []byte{0xAA, 0xBB, 0xCC}, true, false)
	got := p.AppendTo(nil)

	want := []byte{
		'V', 'C', 'S', // 0-2   magic
		0x10,             // 3     version 1 << 4 | type 0 (VOICE)
		0x01,             // 4     flags: PTT
		0x01, 0x02, 0x03, // 5-7   sequence 0x010203
		0x03, 0xD5, 0xA4, // 8-10  frequency 251300 kHz
		0x01, 0x23, 0x45, 0x67, // 11-26 sender uuid, RFC 4122 order
		0x89, 0xab,
		0xcd, 0xef,
		0x01, 0x23,
		0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		0xAA, 0xBB, 0xCC, // 27+   payload
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch\n got %x\nwant %x", got, want)
	}
	if len(got) != HeaderSize+3 {
		t.Fatalf("length %d, want %d", len(got), HeaderSize+3)
	}
}

func TestParseRoundTrip(t *testing.T) {
	orig := NewVoice(goldenSender, 0xFFFFFF, KHz(16777215), bytes.Repeat([]byte{0x7F}, 100), true, true)
	got, err := Parse(orig.AppendTo(nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Type != PacketTypeVoice {
		t.Errorf("Type = %v", got.Type)
	}
	if got.Sequence != 0xFFFFFF {
		t.Errorf("Sequence = %#x, want 0xffffff", got.Sequence)
	}
	if got.Frequency != KHz(16777215) {
		t.Errorf("Frequency = %d", got.Frequency)
	}
	if got.SenderID != goldenSender {
		t.Errorf("SenderID = %v", got.SenderID)
	}
	if !got.PTT() || !got.Intercom() {
		t.Errorf("flags lost: ptt=%v intercom=%v", got.PTT(), got.Intercom())
	}
	if !bytes.Equal(got.Payload, orig.Payload) {
		t.Error("payload mismatch")
	}
}

// TestHelloCarriesSecretAtOffsetZero pins the one thing that decides whether
// the server accepts us at all. The secret is 43 raw UTF-8 bytes at payload
// [0:43]; the server reads exactly that slice and constant-time-compares it.
func TestHelloCarriesSecretAtOffsetZero(t *testing.T) {
	secret := strings.Repeat("A", VoiceSecretLen)
	p := NewHello(goldenSender, secret)
	if len(p.Payload) < VoiceSecretLen {
		t.Fatalf("HELLO payload is %d bytes, server requires at least %d", len(p.Payload), VoiceSecretLen)
	}
	if string(p.Payload[:VoiceSecretLen]) != secret {
		t.Fatalf("secret not at payload[0:%d]", VoiceSecretLen)
	}
	if p.Type != PacketTypeHello {
		t.Fatalf("Type = %v, want HELLO", p.Type)
	}
}

// TestKeepaliveEchoRoundTrip pins the 8-byte big-endian echo. This is the
// only input to the server's per-client latency map, so an empty or
// wrongly-ordered payload silently degrades server-side telemetry.
func TestKeepaliveEchoRoundTrip(t *testing.T) {
	const ts int64 = 1_700_000_000_123
	p := NewKeepalive(goldenSender, ts)
	if len(p.Payload) != 8 {
		t.Fatalf("payload is %d bytes, want 8", len(p.Payload))
	}
	if got := int64(binary.BigEndian.Uint64(p.Payload)); got != ts {
		t.Fatalf("echo = %d, want %d", got, ts)
	}
	if got := KeepaliveTimestamp(p.Payload); got != ts {
		t.Fatalf("KeepaliveTimestamp = %d, want %d", got, ts)
	}
	// Before the first server reply there is nothing to echo.
	if n := len(NewKeepalive(goldenSender, 0).Payload); n != 0 {
		t.Fatalf("zero echo produced a %d-byte payload, want empty", n)
	}
	if got := KeepaliveTimestamp(nil); got != 0 {
		t.Fatalf("KeepaliveTimestamp(nil) = %d, want 0", got)
	}
}

func TestParseRejects(t *testing.T) {
	good := NewBye(goldenSender).AppendTo(nil)

	t.Run("short", func(t *testing.T) {
		if _, err := Parse(good[:HeaderSize-1]); err == nil {
			t.Fatal("expected an error for a short packet")
		}
	})
	t.Run("bad magic", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[0] = 'X'
		if _, err := Parse(bad); err == nil {
			t.Fatal("expected an error for bad magic")
		}
	})
	t.Run("bad version", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[3] = (9 << 4) | byte(PacketTypeBye)
		if _, err := Parse(bad); err == nil {
			t.Fatal("expected an error for an unsupported version")
		}
	})
}

// TestSequenceIsMaskedTo24Bits pins that a counter wrapping past 2^24 cannot
// corrupt the frequency field that sits immediately after it.
func TestSequenceIsMaskedTo24Bits(t *testing.T) {
	p := NewVoice(goldenSender, 0x01_FF_FF_FF, KHz(251300), []byte{1, 2, 3, 4, 5, 6}, true, false)
	raw := p.AppendTo(nil)
	if raw[5] != 0xFF || raw[6] != 0xFF || raw[7] != 0xFF {
		t.Fatalf("sequence bytes %x %x %x, want ff ff ff", raw[5], raw[6], raw[7])
	}
	if raw[8] != 0x03 || raw[9] != 0xD5 || raw[10] != 0xA4 {
		t.Fatalf("frequency corrupted by sequence overflow: %x %x %x", raw[8], raw[9], raw[10])
	}
}
