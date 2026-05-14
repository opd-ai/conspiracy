package lora

import (
	"context"
	"testing"
	"time"
)

func TestUDPRadio_RoundTrip(t *testing.T) {
	radio1, err := NewUDPRadio("127.0.0.1:9001", "127.0.0.1:9002")
	if err != nil {
		t.Fatalf("Failed to create radio1: %v", err)
	}
	defer radio1.Close()

	radio2, err := NewUDPRadio("127.0.0.1:9002", "127.0.0.1:9001")
	if err != nil {
		t.Fatalf("Failed to create radio2: %v", err)
	}
	defer radio2.Close()

	// Test sending from radio1 to radio2
	payload := []byte("test payload")
	ctx := context.Background()

	if err := radio1.Send(ctx, payload); err != nil {
		t.Fatalf("Failed to send: %v", err)
	}

	// Receive with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	received, err := radio2.Recv(ctx)
	if err != nil {
		t.Fatalf("Failed to receive: %v", err)
	}

	if string(received) != string(payload) {
		t.Errorf("Payload mismatch: got %q, want %q", received, payload)
	}
}

func TestUDPRadio_SetParameters(t *testing.T) {
	radio, err := NewUDPRadio("127.0.0.1:9003", "127.0.0.1:9004")
	if err != nil {
		t.Fatalf("Failed to create radio: %v", err)
	}
	defer radio.Close()

	// Test valid parameters
	if err := radio.SetFrequency(915.0); err != nil {
		t.Errorf("SetFrequency failed: %v", err)
	}

	if err := radio.SetSpreadingFactor(7); err != nil {
		t.Errorf("SetSpreadingFactor(7) failed: %v", err)
	}

	if err := radio.SetBandwidth(250); err != nil {
		t.Errorf("SetBandwidth(250) failed: %v", err)
	}

	// Test invalid spreading factor
	if err := radio.SetSpreadingFactor(6); err == nil {
		t.Error("SetSpreadingFactor(6) should fail")
	}

	if err := radio.SetSpreadingFactor(13); err == nil {
		t.Error("SetSpreadingFactor(13) should fail")
	}

	// Test invalid bandwidth
	if err := radio.SetBandwidth(100); err == nil {
		t.Error("SetBandwidth(100) should fail")
	}

	// Test RSSI
	rssi, err := radio.RSSI()
	if err != nil {
		t.Errorf("RSSI failed: %v", err)
	}
	if rssi >= 0 {
		t.Errorf("RSSI should be negative, got %d", rssi)
	}
}

func TestUDPRadio_Close(t *testing.T) {
	radio, err := NewUDPRadio("127.0.0.1:9005", "127.0.0.1:9006")
	if err != nil {
		t.Fatalf("Failed to create radio: %v", err)
	}

	if err := radio.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Verify operations fail after close
	ctx := context.Background()
	if err := radio.Send(ctx, []byte("test")); err == nil {
		t.Error("Send should fail after Close")
	}

	if _, err := radio.Recv(ctx); err == nil {
		t.Error("Recv should fail after Close")
	}
}
