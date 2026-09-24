package voice

import (
	"encoding/binary"
	"fmt"

	"github.com/google/uuid"
)

// Wire-protocol constants for the VCS UDP voice packet. The layout here is
// pinned byte-for-byte against two independent peer implementations: the Go
// server's voice/protocol.go (SerializePacket/ParsePacket) and the C# client's
// DCS-SR-Common/Network/VCSVoicePacket.cs (EncodePacket/DecodePacket). See
// packet_test.go's golden vector.
const (
	// HeaderSize is the fixed number of bytes before the payload:
	// 3 (magic) + 1 (version/type) + 1 (flags) + 3 (sequence) + 3 (frequency)
	// + 16 (uuid) = 27.
	HeaderSize = 27

	// MagicVCS is the fixed 3-byte protocol identifier at offset 0.
	MagicVCS = "VCS"

	// ProtocolVersion is the only version this client speaks. It occupies
	// the high nibble of byte 3; the low nibble carries PacketType.
	ProtocolVersion = 1

	// VoiceSecretLen is the length of the per-session voice secret as it
	// appears in a HELLO payload: raw UTF-8 bytes at payload[0:VoiceSecretLen].
	// The server reads exactly this slice and constant-time-compares it.
	VoiceSecretLen = 43

	// MaxDatagram is the largest UDP datagram this client will send or
	// accept for the voice protocol.
	MaxDatagram = 1024

	// MinVoicePayload is the smallest VOICE payload the server will relay;
	// it discards anything at or below this length.
	MinVoicePayload = 6
)

// wireMask24 restricts the wire's 24-bit sequence and frequency fields;
// values above this are truncated on write so a wrapping counter cannot
// bleed into the field beside it.
const wireMask24 = 1<<24 - 1

// PacketType identifies the kind of VCS packet carried in byte 3's low
// nibble.
type PacketType uint8

const (
	PacketTypeVoice PacketType = iota
	PacketTypeHello
	PacketTypeHelloAck
	PacketTypeKeepalive
	PacketTypeBye
)

// String returns the human-readable name of the packet type, matching the
// Go server's and C# peer's own String()/ToString() output.
func (t PacketType) String() string {
	switch t {
	case PacketTypeVoice:
		return "VOICE"
	case PacketTypeHello:
		return "HELLO"
	case PacketTypeHelloAck:
		return "HELLO_ACK"
	case PacketTypeKeepalive:
		return "KEEPALIVE"
	case PacketTypeBye:
		return "BYE"
	default:
		return "UNKNOWN"
	}
}

// Packet is a parsed or to-be-serialized VCS wire packet. See the package
// doc comment on packet.go for the byte layout.
type Packet struct {
	Type      PacketType
	Flags     uint8
	Sequence  uint32 // low 24 bits significant; masked on write
	Frequency KHz    // low 24 bits significant; masked on write
	SenderID  uuid.UUID
	Payload   []byte
}

// Flag bits within Packet.Flags.
const (
	flagPTT      uint8 = 0x01
	flagIntercom uint8 = 0x02
)

// PTT reports whether the push-to-talk flag is set.
func (p *Packet) PTT() bool { return p.Flags&flagPTT != 0 }

// SetPTT sets or clears the push-to-talk flag.
func (p *Packet) SetPTT(active bool) {
	if active {
		p.Flags |= flagPTT
	} else {
		p.Flags &^= flagPTT
	}
}

// Intercom reports whether the intercom flag is set.
func (p *Packet) Intercom() bool { return p.Flags&flagIntercom != 0 }

// SetIntercom sets or clears the intercom flag.
func (p *Packet) SetIntercom(active bool) {
	if active {
		p.Flags |= flagIntercom
	} else {
		p.Flags &^= flagIntercom
	}
}

// AppendTo appends the packet's wire encoding to dst and returns the grown
// slice, so the TX path can reuse one buffer across packets without a
// per-packet allocation.
func (p *Packet) AppendTo(dst []byte) []byte {
	var hdr [HeaderSize]byte

	hdr[0], hdr[1], hdr[2] = 'V', 'C', 'S'
	hdr[3] = (uint8(ProtocolVersion) << 4) | uint8(p.Type)
	hdr[4] = p.Flags

	seq := p.Sequence & wireMask24
	hdr[5] = byte(seq >> 16)
	hdr[6] = byte(seq >> 8)
	hdr[7] = byte(seq)

	freq := uint32(p.Frequency) & wireMask24
	hdr[8] = byte(freq >> 16)
	hdr[9] = byte(freq >> 8)
	hdr[10] = byte(freq)

	// Go's uuid.UUID is already RFC 4122 big-endian ordered; no conversion
	// needed (unlike the C# peer, which hand-converts .NET's little-endian
	// Guid layout in both directions).
	copy(hdr[11:27], p.SenderID[:])

	dst = append(dst, hdr[:]...)
	dst = append(dst, p.Payload...)
	return dst
}

// parseError is a typed error identifying which validation step failed, so
// callers can distinguish "malformed" from "not for us" if that ever
// matters.
type parseError struct {
	reason string
}

func (e *parseError) Error() string { return "voice: " + e.reason }

// Parse decodes a VCS wire packet. It validates the minimum length, magic,
// and protocol version, and returns a typed error for each failure.
//
// Parse copies the payload out of data. The caller's read buffer is reused
// across datagrams, so a shared slice would be silently overwritten while a
// jitter buffer still holds the packet.
func Parse(data []byte) (*Packet, error) {
	if len(data) < HeaderSize {
		return nil, &parseError{fmt.Sprintf("packet too short: %d bytes, want at least %d", len(data), HeaderSize)}
	}
	if string(data[0:3]) != MagicVCS {
		return nil, &parseError{fmt.Sprintf("bad magic: %q", data[0:3])}
	}

	versionType := data[3]
	version := versionType >> 4
	if version != ProtocolVersion {
		return nil, &parseError{fmt.Sprintf("unsupported protocol version: %d", version)}
	}

	p := &Packet{
		Type:  PacketType(versionType & 0x0F),
		Flags: data[4],
	}
	p.Sequence = uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
	p.Frequency = KHz(uint32(data[8])<<16 | uint32(data[9])<<8 | uint32(data[10]))

	senderID, err := uuid.FromBytes(data[11:27])
	if err != nil {
		return nil, &parseError{fmt.Sprintf("invalid sender id: %v", err)}
	}
	p.SenderID = senderID

	if len(data) > HeaderSize {
		p.Payload = append([]byte(nil), data[HeaderSize:]...)
	}

	return p, nil
}

// NewHello builds a HELLO announcing the sender and presenting its voice
// secret. The secret is carried as raw UTF-8 bytes at payload offset 0; the
// server validates it before binding the sender's address.
func NewHello(sender uuid.UUID, secret string) *Packet {
	return &Packet{
		Type:     PacketTypeHello,
		SenderID: sender,
		Payload:  []byte(secret),
	}
}

// NewVoice builds a VOICE packet carrying an Opus frame on freq.
func NewVoice(sender uuid.UUID, seq uint32, freq KHz, opus []byte, ptt, intercom bool) *Packet {
	p := &Packet{
		Type:      PacketTypeVoice,
		Sequence:  seq,
		Frequency: freq,
		SenderID:  sender,
		Payload:   opus,
	}
	p.SetPTT(ptt)
	p.SetIntercom(intercom)
	return p
}

// NewKeepalive builds a KEEPALIVE packet. echo is the server timestamp being
// echoed back for RTT measurement; a zero echo means there is nothing to
// echo yet (before the first server reply), and produces an empty payload.
func NewKeepalive(sender uuid.UUID, echo int64) *Packet {
	p := &Packet{
		Type:     PacketTypeKeepalive,
		SenderID: sender,
	}
	if echo != 0 {
		p.Payload = make([]byte, 8)
		binary.BigEndian.PutUint64(p.Payload, uint64(echo))
	}
	return p
}

// NewBye builds a BYE packet for graceful disconnection.
func NewBye(sender uuid.UUID) *Packet {
	return &Packet{
		Type:     PacketTypeBye,
		SenderID: sender,
	}
}

// KeepaliveTimestamp reads the 8-byte big-endian echo timestamp from a
// KEEPALIVE payload. It returns 0 if payload is too short, matching the
// zero-means-absent convention NewKeepalive uses on write.
func KeepaliveTimestamp(payload []byte) int64 {
	if len(payload) < 8 {
		return 0
	}
	return int64(binary.BigEndian.Uint64(payload[:8]))
}
