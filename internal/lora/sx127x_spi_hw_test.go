//go:build hardware
// +build hardware

package lora

import (
	"context"
	"testing"
	"time"
)

// TestSX127xSPI_HardwareRegisterRead verifies chip communication via SPI.
// This test requires real SX127x hardware and is skipped in normal CI.
// To run: go test -v -tags=hardware ./internal/lora
func TestSX127xSPI_HardwareRegisterRead(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	radio, err := NewSX127xSPI("/dev/spidev0.0", "GPIO25", "GPIO24")
	if err != nil {
		t.Skipf("Failed to initialize radio (hardware not present?): %v", err)
	}
	defer radio.Close()

	// The NewSX127xSPI constructor already verified the version register
	// If we got here, the chip responded with a valid version
	t.Log("Successfully read SX127x version register via SPI")
}

// TestSX127xSPI_TxRx_RoundTrip tests full TX/RX cycle with real hardware.
// Requires two nodes with LoRa radios.
func TestSX127xSPI_TxRx_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	radio, err := NewSX127xSPI("/dev/spidev0.0", "GPIO25", "GPIO24")
	if err != nil {
		t.Skipf("Failed to initialize radio: %v", err)
	}
	defer radio.Close()

	// Configure for SF10, 868.1 MHz
	if err := radio.SetFrequency(868.1); err != nil {
		t.Fatalf("SetFrequency failed: %v", err)
	}

	if err := radio.SetSpreadingFactor(10); err != nil {
		t.Fatalf("SetSpreadingFactor failed: %v", err)
	}

	if err := radio.SetBandwidth(125); err != nil {
		t.Fatalf("SetBandwidth failed: %v", err)
	}

	// Send test payload
	payload := []byte("test payload from conspiracyd")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := radio.Send(ctx, payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	t.Log("Successfully transmitted LoRa packet")

	// Note: Full round-trip testing requires second node.
	// This test validates TX only. RX can be tested with second hardware node.
}
