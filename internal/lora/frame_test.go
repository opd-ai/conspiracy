package lora

import (
	"bytes"
	"testing"
)

func TestMarshalUnmarshalHeader(t *testing.T) {
	tests := []struct {
		name string
		hdr  Header
	}{
		{
			name: "basic header",
			hdr: Header{
				FrameType: FrameTypeBEACON,
				Version:   ProtocolVersion,
				NodeID:    0x12345678,
				Timestamp: 1704067200,
				FrameSeq:  42,
				Nonce:     [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
				HMAC:      [12]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
			},
		},
		{
			name: "JOIN_REQ header",
			hdr: Header{
				FrameType: FrameTypeJOIN_REQ,
				Version:   ProtocolVersion,
				NodeID:    0xdeadbeef,
				Timestamp: 1704067200,
				FrameSeq:  1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data := MarshalHeader(&tt.hdr)
			if len(data) != HeaderSize {
				t.Errorf("MarshalHeader() size = %d, want %d", len(data), HeaderSize)
			}

			// Unmarshal
			hdr, err := UnmarshalHeader(data)
			if err != nil {
				t.Fatalf("UnmarshalHeader() error = %v", err)
			}

			// Verify fields
			if hdr.FrameType != tt.hdr.FrameType {
				t.Errorf("FrameType = %d, want %d", hdr.FrameType, tt.hdr.FrameType)
			}
			if hdr.Version != tt.hdr.Version {
				t.Errorf("Version = %d, want %d", hdr.Version, tt.hdr.Version)
			}
			if hdr.NodeID != tt.hdr.NodeID {
				t.Errorf("NodeID = %x, want %x", hdr.NodeID, tt.hdr.NodeID)
			}
			if hdr.Timestamp != tt.hdr.Timestamp {
				t.Errorf("Timestamp = %d, want %d", hdr.Timestamp, tt.hdr.Timestamp)
			}
			if hdr.FrameSeq != tt.hdr.FrameSeq {
				t.Errorf("FrameSeq = %d, want %d", hdr.FrameSeq, tt.hdr.FrameSeq)
			}
			if !bytes.Equal(hdr.Nonce[:], tt.hdr.Nonce[:]) {
				t.Errorf("Nonce mismatch")
			}
			if !bytes.Equal(hdr.HMAC[:], tt.hdr.HMAC[:]) {
				t.Errorf("HMAC mismatch")
			}
		})
	}
}

func TestUnmarshalHeaderTooShort(t *testing.T) {
	data := make([]byte, HeaderSize-1)
	_, err := UnmarshalHeader(data)
	if err == nil {
		t.Error("UnmarshalHeader() expected error for short data, got nil")
	}
}

func TestMarshalUnmarshalBEACONPayload(t *testing.T) {
	payload := &BEACONPayload{
		SSID:         [32]byte{'c', 'o', 'n', 's', 'p', 'i', 'r', 'a', 'c', 'y', '-', 'm', 'e', 's', 'h'},
		BSSID:        [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		Channel:      6,
		Capabilities: 0x01, // batman-adv enabled
		GPSLatitude:  -33850000,
		GPSLongitude: 151217000,
		Timestamp:    1704067200,
	}

	// Marshal
	data := MarshalBEACONPayload(payload)
	if len(data) != BEACONPayloadSize {
		t.Errorf("MarshalBEACONPayload() size = %d, want %d", len(data), BEACONPayloadSize)
	}

	// Unmarshal
	decoded, err := UnmarshalBEACONPayload(data)
	if err != nil {
		t.Fatalf("UnmarshalBEACONPayload() error = %v", err)
	}

	// Verify fields
	if !bytes.Equal(decoded.SSID[:], payload.SSID[:]) {
		t.Errorf("SSID mismatch")
	}
	if !bytes.Equal(decoded.BSSID[:], payload.BSSID[:]) {
		t.Errorf("BSSID mismatch")
	}
	if decoded.Channel != payload.Channel {
		t.Errorf("Channel = %d, want %d", decoded.Channel, payload.Channel)
	}
	if decoded.Capabilities != payload.Capabilities {
		t.Errorf("Capabilities = %x, want %x", decoded.Capabilities, payload.Capabilities)
	}
	if decoded.GPSLatitude != payload.GPSLatitude {
		t.Errorf("GPSLatitude = %d, want %d", decoded.GPSLatitude, payload.GPSLatitude)
	}
	if decoded.GPSLongitude != payload.GPSLongitude {
		t.Errorf("GPSLongitude = %d, want %d", decoded.GPSLongitude, payload.GPSLongitude)
	}
	if decoded.Timestamp != payload.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, payload.Timestamp)
	}
}

func TestMarshalUnmarshalJOIN_REQPayload(t *testing.T) {
	payload := &JOIN_REQPayload{
		NodeID:    0xdeadbeef,
		Nonce:     0x12345678,
		POW:       0,
		Timestamp: 1704067200,
	}

	// Marshal
	data := MarshalJOIN_REQPayload(payload)
	if len(data) != 16 {
		t.Errorf("MarshalJOIN_REQPayload() size = %d, want 16", len(data))
	}

	// Unmarshal
	decoded, err := UnmarshalJOIN_REQPayload(data)
	if err != nil {
		t.Fatalf("UnmarshalJOIN_REQPayload() error = %v", err)
	}

	if decoded.NodeID != payload.NodeID {
		t.Errorf("NodeID = %x, want %x", decoded.NodeID, payload.NodeID)
	}
	if decoded.Timestamp != payload.Timestamp {
		t.Errorf("Timestamp = %d, want %d", decoded.Timestamp, payload.Timestamp)
	}
}

func TestMarshalUnmarshalJOIN_ACKPayload(t *testing.T) {
	payload := &JOIN_ACKPayload{
		SSID:    [32]byte{'c', 'o', 'n', 's', 'p', 'i', 'r', 'a', 'c', 'y', '-', 'm', 'e', 's', 'h'},
		BSSID:   [6]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
		Channel: 6,
		Status:  0,
	}

	// Marshal
	data := MarshalJOIN_ACKPayload(payload)
	if len(data) != 40 {
		t.Errorf("MarshalJOIN_ACKPayload() size = %d, want 40", len(data))
	}

	// Unmarshal
	decoded, err := UnmarshalJOIN_ACKPayload(data)
	if err != nil {
		t.Fatalf("UnmarshalJOIN_ACKPayload() error = %v", err)
	}

	if !bytes.Equal(decoded.SSID[:], payload.SSID[:]) {
		t.Errorf("SSID mismatch")
	}
	if !bytes.Equal(decoded.BSSID[:], payload.BSSID[:]) {
		t.Errorf("BSSID mismatch")
	}
	if decoded.Channel != payload.Channel {
		t.Errorf("Channel = %d, want %d", decoded.Channel, payload.Channel)
	}
	if decoded.Status != payload.Status {
		t.Errorf("Status = %d, want %d", decoded.Status, payload.Status)
	}
}

func TestMarshalUnmarshalFrame(t *testing.T) {
	hdr := &Header{
		FrameType: FrameTypeBEACON,
		Version:   ProtocolVersion,
		NodeID:    0x12345678,
		Timestamp: 1704067200,
		FrameSeq:  42,
	}
	payload := []byte{0x01, 0x02, 0x03, 0x04}

	// Marshal
	frame := MarshalFrame(hdr, payload)
	expectedLen := HeaderSize + len(payload)
	if len(frame) != expectedLen {
		t.Errorf("MarshalFrame() size = %d, want %d", len(frame), expectedLen)
	}

	// Unmarshal
	decodedHdr, decodedPayload, err := UnmarshalFrame(frame)
	if err != nil {
		t.Fatalf("UnmarshalFrame() error = %v", err)
	}

	if decodedHdr.FrameType != hdr.FrameType {
		t.Errorf("FrameType = %d, want %d", decodedHdr.FrameType, hdr.FrameType)
	}
	if !bytes.Equal(decodedPayload, payload) {
		t.Errorf("Payload mismatch")
	}
}

func TestUnmarshalFrameTooShort(t *testing.T) {
	data := make([]byte, HeaderSize-1)
	_, _, err := UnmarshalFrame(data)
	if err == nil {
		t.Error("UnmarshalFrame() expected error for short data, got nil")
	}
}
