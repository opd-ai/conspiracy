package lora

import (
	"testing"
)

func TestNewRadio_UDP(t *testing.T) {
	cfg := Config{
		Device:    "udp",
		UDPListen: "127.0.0.1:10001",
		UDPPeer:   "127.0.0.1:10002",
		Frequency: 868.1,
		SF:        10,
		Bandwidth: 125,
	}

	radio, err := NewRadio(cfg)
	if err != nil {
		t.Fatalf("NewRadio failed: %v", err)
	}
	defer radio.Close()

	// Verify it's a UDPRadio
	_, ok := radio.(*UDPRadio)
	if !ok {
		t.Error("Expected UDPRadio")
	}
}

func TestNewRadio_InvalidDevice(t *testing.T) {
	cfg := Config{
		Device: "/dev/invalid",
	}

	_, err := NewRadio(cfg)
	if err == nil {
		t.Error("Expected error for invalid device")
	}
}

func TestNewRadio_UART_NotImplemented(t *testing.T) {
	cfg := Config{
		Device: "/dev/ttyUSB0",
	}

	_, err := NewRadio(cfg)
	if err == nil {
		t.Error("Expected error for UART device (not yet implemented)")
	}
}
