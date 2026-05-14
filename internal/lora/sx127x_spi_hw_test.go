//go:build hardware
// +build hardware

package lora

import (
	"context"
	"os"
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

// TestSX127xSPI_TxRx_RoundTrip tests full TX/RX cycle with real hardware at multiple spreading factors.
// Requires two nodes with LoRa radios or a loopback setup.
// Set LORA_TEST_MODE=tx or LORA_TEST_MODE=rx to run transmitter or receiver side.
func TestSX127xSPI_TxRx_RoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	testMode := os.Getenv("LORA_TEST_MODE")
	if testMode == "" {
		t.Skip("Set LORA_TEST_MODE=tx or LORA_TEST_MODE=rx to run hardware test")
	}

	tests := []struct {
		name string
		sf   int
	}{
		{"SF7", 7},
		{"SF10", 10},
		{"SF12", 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			radio, err := NewSX127xSPI("/dev/spidev0.0", "GPIO25", "GPIO24")
			if err != nil {
				t.Fatalf("Failed to initialize radio: %v", err)
			}
			defer radio.Close()

			if err := radio.SetFrequency(868.1); err != nil {
				t.Fatalf("SetFrequency failed: %v", err)
			}

			if err := radio.SetSpreadingFactor(tt.sf); err != nil {
				t.Fatalf("SetSpreadingFactor failed: %v", err)
			}

			if err := radio.SetBandwidth(125); err != nil {
				t.Fatalf("SetBandwidth failed: %v", err)
			}

			if testMode == "tx" {
				runTransmitter(t, radio, tt.sf)
			} else if testMode == "rx" {
				runReceiver(t, radio, tt.sf)
			} else {
				t.Fatalf("Invalid LORA_TEST_MODE: %s (must be 'tx' or 'rx')", testMode)
			}
		})
	}
}

// runTransmitter sends test packets with the configured spreading factor.
func runTransmitter(t *testing.T, radio *SX127xSPI, sf int) {
	payload := []byte("conspiracyd test SF" + string(rune('0'+sf)))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Logf("Transmitting test packet with SF%d...", sf)
	if err := radio.Send(ctx, payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	t.Logf("Successfully transmitted LoRa packet with SF%d", sf)
}

// runReceiver listens for test packets with the configured spreading factor.
func runReceiver(t *testing.T, radio *SX127xSPI, sf int) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Logf("Listening for packets with SF%d...", sf)
	payload, err := radio.Recv(ctx)
	if err != nil {
		if err == context.DeadlineExceeded {
			t.Fatalf("No packet received within 30s timeout (SF%d)", sf)
		}
		t.Fatalf("Recv failed: %v", err)
	}

	rssi, err := radio.RSSI()
	if err != nil {
		t.Logf("Warning: Failed to read RSSI: %v", err)
	} else {
		t.Logf("Received packet with SF%d, RSSI: %d dBm", sf, rssi)
	}

	t.Logf("Successfully received LoRa packet with SF%d: %q", sf, payload)
}

// TestSX127xSPI_ConfigurationValidation validates radio configuration changes.
func TestSX127xSPI_ConfigurationValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hardware test in short mode")
	}

	radio, err := NewSX127xSPI("/dev/spidev0.0", "GPIO25", "GPIO24")
	if err != nil {
		t.Skipf("Failed to initialize radio: %v", err)
	}
	defer radio.Close()

	tests := []struct {
		name      string
		configure func() error
		wantErr   bool
	}{
		{
			name: "Valid EU frequency",
			configure: func() error {
				return radio.SetFrequency(868.1)
			},
			wantErr: false,
		},
		{
			name: "Valid US frequency",
			configure: func() error {
				return radio.SetFrequency(915.0)
			},
			wantErr: false,
		},
		{
			name: "Valid SF range",
			configure: func() error {
				for sf := 7; sf <= 12; sf++ {
					if err := radio.SetSpreadingFactor(sf); err != nil {
						return err
					}
				}
				return nil
			},
			wantErr: false,
		},
		{
			name: "Invalid SF low",
			configure: func() error {
				return radio.SetSpreadingFactor(6)
			},
			wantErr: true,
		},
		{
			name: "Invalid SF high",
			configure: func() error {
				return radio.SetSpreadingFactor(13)
			},
			wantErr: true,
		},
		{
			name: "Valid bandwidth 125kHz",
			configure: func() error {
				return radio.SetBandwidth(125)
			},
			wantErr: false,
		},
		{
			name: "Valid bandwidth 250kHz",
			configure: func() error {
				return radio.SetBandwidth(250)
			},
			wantErr: false,
		},
		{
			name: "Valid bandwidth 500kHz",
			configure: func() error {
				return radio.SetBandwidth(500)
			},
			wantErr: false,
		},
		{
			name: "Invalid bandwidth",
			configure: func() error {
				return radio.SetBandwidth(100)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.configure()
			if (err != nil) != tt.wantErr {
				t.Errorf("configure() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
