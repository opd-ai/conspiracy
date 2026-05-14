// Package lora provides LoRa frame codec for protocol message marshaling/unmarshaling.
package lora

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Frame types per design §3.2
const (
	FrameTypeBEACON     = 0x01
	FrameTypeJOIN_REQ   = 0x02
	FrameTypeJOIN_ACK   = 0x03
	FrameTypeROUTE_HINT = 0x04
	FrameTypePING       = 0x05
	FrameTypePONG       = 0x06
	FrameTypeREKEY      = 0x07
)

// Protocol version
const ProtocolVersion = 0x04 // v1.0 with nonce field (wire-incompatible with v0.3)

// Header represents the common LoRa frame header (25 bytes)
type Header struct {
	FrameType uint8
	Version   uint8
	NodeID    uint32
	Timestamp uint32 // Unix timestamp (seconds since epoch)
	FrameSeq  uint16
	Nonce     [12]byte // ChaCha20-Poly1305 nonce (non-deterministic due to crypto/rand component)
	HMAC      [12]byte // HMAC-SHA256 truncated to 96 bits
}

// HeaderSize is the size of the frame header in bytes
const HeaderSize = 37

// BEACONPayload represents an encrypted BEACON payload (101 bytes plaintext before encryption)
type BEACONPayload struct {
	SSID         [32]byte // Mesh SSID (null-padded if <32 bytes)
	BSSID        [6]byte  // MAC address of mesh interface
	Channel      uint8
	Capabilities uint16 // Bitmask: bit 0 = batman-adv enabled, bit 1 = Yggdrasil, bit 2 = cjdns
	GPSLatitude  int32  // Fixed-point (degrees × 1e7), 0 if GPS disabled
	GPSLongitude int32
	Padding      [32]byte // Fixed-length padding for traffic analysis resistance
	Timestamp    uint32   // Duplicate of Header.Timestamp for anti-precomputation (PoW validation)
}

// BEACONPayloadSize is the size of the BEACON payload in bytes
const BEACONPayloadSize = 101

// JOIN_REQPayload represents a JOIN_REQ frame payload (unencrypted for MVP)
type JOIN_REQPayload struct {
	NodeID    uint32
	Nonce     uint32 // Challenge nonce for PoW (deferred to post-MVP)
	POW       uint32 // Proof-of-work solution (deferred to post-MVP)
	Timestamp uint32
}

// JOIN_ACKPayload represents a JOIN_ACK frame payload (unencrypted for MVP)
type JOIN_ACKPayload struct {
	SSID    [32]byte // Mesh SSID
	BSSID   [6]byte  // MAC address
	Channel uint8
	Status  uint8 // 0 = success, 1 = rejected
}

// REKEYPayload represents the plaintext content of a REKEY frame (before encryption with OLD_KEY).
// Total size: 32 + 4 + 4 + 8 = 48 bytes (before ChaCha20-Poly1305 adds 16-byte tag).
type REKEYPayload struct {
	NewKey          [32]byte // New 256-bit mesh key
	NewKeyID        uint32   // HMAC-SHA256(NEW_KEY, "key-id")[0:4]
	ValidAfter      uint32   // Unix timestamp when new key becomes valid (24-hour transition)
	RekeyGeneration uint64   // Monotonic counter to prevent replay attacks
}

// MarshalHeader serializes a frame header to bytes
func MarshalHeader(hdr *Header) []byte {
	buf := make([]byte, HeaderSize)
	buf[0] = hdr.FrameType
	buf[1] = hdr.Version
	binary.BigEndian.PutUint32(buf[2:6], hdr.NodeID)
	binary.BigEndian.PutUint32(buf[6:10], hdr.Timestamp)
	binary.BigEndian.PutUint16(buf[10:12], hdr.FrameSeq)
	copy(buf[12:24], hdr.Nonce[:])
	copy(buf[24:36], hdr.HMAC[:])
	return buf
}

// UnmarshalHeader parses a frame header from bytes
func UnmarshalHeader(data []byte) (*Header, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("frame too short: %d bytes (min %d)", len(data), HeaderSize)
	}

	hdr := &Header{
		FrameType: data[0],
		Version:   data[1],
		NodeID:    binary.BigEndian.Uint32(data[2:6]),
		Timestamp: binary.BigEndian.Uint32(data[6:10]),
		FrameSeq:  binary.BigEndian.Uint16(data[10:12]),
	}
	copy(hdr.Nonce[:], data[12:24])
	copy(hdr.HMAC[:], data[24:36])

	return hdr, nil
}

// MarshalBEACONPayload serializes a BEACON payload to bytes (before encryption)
func MarshalBEACONPayload(payload *BEACONPayload) []byte {
	buf := make([]byte, BEACONPayloadSize)
	offset := 0

	// SSID (32 bytes)
	copy(buf[offset:offset+32], payload.SSID[:])
	offset += 32

	// BSSID (6 bytes)
	copy(buf[offset:offset+6], payload.BSSID[:])
	offset += 6

	// Channel (1 byte)
	buf[offset] = payload.Channel
	offset++

	// Capabilities (2 bytes)
	binary.BigEndian.PutUint16(buf[offset:offset+2], payload.Capabilities)
	offset += 2

	// GPSLatitude (4 bytes)
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(payload.GPSLatitude))
	offset += 4

	// GPSLongitude (4 bytes)
	binary.BigEndian.PutUint32(buf[offset:offset+4], uint32(payload.GPSLongitude))
	offset += 4

	// Padding (32 bytes)
	copy(buf[offset:offset+32], payload.Padding[:])
	offset += 32

	// Timestamp (4 bytes)
	binary.BigEndian.PutUint32(buf[offset:offset+4], payload.Timestamp)

	return buf
}

// UnmarshalBEACONPayload parses a BEACON payload from bytes (after decryption)
func UnmarshalBEACONPayload(data []byte) (*BEACONPayload, error) {
	if len(data) < BEACONPayloadSize {
		return nil, fmt.Errorf("BEACON payload too short: %d bytes (expected %d)", len(data), BEACONPayloadSize)
	}

	payload := &BEACONPayload{}
	offset := 0

	// SSID (32 bytes)
	copy(payload.SSID[:], data[offset:offset+32])
	offset += 32

	// BSSID (6 bytes)
	copy(payload.BSSID[:], data[offset:offset+6])
	offset += 6

	// Channel (1 byte)
	payload.Channel = data[offset]
	offset++

	// Capabilities (2 bytes)
	payload.Capabilities = binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2

	// GPSLatitude (4 bytes)
	payload.GPSLatitude = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// GPSLongitude (4 bytes)
	payload.GPSLongitude = int32(binary.BigEndian.Uint32(data[offset : offset+4]))
	offset += 4

	// Padding (32 bytes)
	copy(payload.Padding[:], data[offset:offset+32])
	offset += 32

	// Timestamp (4 bytes)
	payload.Timestamp = binary.BigEndian.Uint32(data[offset : offset+4])

	return payload, nil
}

// MarshalJOIN_REQPayload serializes a JOIN_REQ payload to bytes
func MarshalJOIN_REQPayload(payload *JOIN_REQPayload) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, payload)
	return buf.Bytes()
}

// UnmarshalJOIN_REQPayload parses a JOIN_REQ payload from bytes
func UnmarshalJOIN_REQPayload(data []byte) (*JOIN_REQPayload, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("JOIN_REQ payload too short: %d bytes (expected 16)", len(data))
	}

	payload := &JOIN_REQPayload{}
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.BigEndian, payload); err != nil {
		return nil, fmt.Errorf("failed to parse JOIN_REQ payload: %w", err)
	}

	return payload, nil
}

// MarshalJOIN_ACKPayload serializes a JOIN_ACK payload to bytes
func MarshalJOIN_ACKPayload(payload *JOIN_ACKPayload) []byte {
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, payload)
	return buf.Bytes()
}

// UnmarshalJOIN_ACKPayload parses a JOIN_ACK payload from bytes
func UnmarshalJOIN_ACKPayload(data []byte) (*JOIN_ACKPayload, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("JOIN_ACK payload too short: %d bytes (expected 40)", len(data))
	}

	payload := &JOIN_ACKPayload{}
	reader := bytes.NewReader(data)
	if err := binary.Read(reader, binary.BigEndian, payload); err != nil {
		return nil, fmt.Errorf("failed to parse JOIN_ACK payload: %w", err)
	}

	return payload, nil
}

// MarshalFrame assembles a complete frame (header + payload)
func MarshalFrame(hdr *Header, payload []byte) []byte {
	headerBytes := MarshalHeader(hdr)
	return append(headerBytes, payload...)
}

// UnmarshalFrame splits a frame into header and payload
func UnmarshalFrame(data []byte) (*Header, []byte, error) {
	if len(data) < HeaderSize {
		return nil, nil, fmt.Errorf("frame too short: %d bytes (min %d)", len(data), HeaderSize)
	}

	hdr, err := UnmarshalHeader(data[:HeaderSize])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal header: %w", err)
	}

	payload := data[HeaderSize:]
	return hdr, payload, nil
}
