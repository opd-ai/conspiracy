package lora

import (
	"fmt"
	"strings"
)

// Config holds LoRa radio configuration.
type Config struct {
	Device    string  // "/dev/spidev0.0", "/dev/ttyUSB0", or "udp"
	Frequency float64 // MHz (e.g., 868.1 for EU, 915 for US)
	SF        int     // Spreading factor 7-12
	Bandwidth int     // Bandwidth in kHz (125, 250, 500)

	// For UDP testing
	UDPListen string // "host:port"
	UDPPeer   string // "host:port"

	// For SPI
	ResetPin string // GPIO pin for RESET (e.g., "GPIO25")
	DIO0Pin  string // GPIO pin for DIO0 interrupt (e.g., "GPIO24")
}

// NewRadio creates a PacketRadio based on device configuration.
// It auto-detects the device type:
//   - "udp" -> UDPRadio (test stub)
//   - "/dev/spidev*" -> SX127xSPI
//   - "/dev/tty*" -> SX127xUART (future)
func NewRadio(cfg Config) (PacketRadio, error) {
	switch {
	case cfg.Device == "udp":
		if cfg.UDPListen == "" || cfg.UDPPeer == "" {
			return nil, fmt.Errorf("UDP mode requires UDPListen and UDPPeer")
		}
		radio, err := NewUDPRadio(cfg.UDPListen, cfg.UDPPeer)
		if err != nil {
			return nil, err
		}

		// Configure parameters
		if cfg.Frequency > 0 {
			radio.SetFrequency(cfg.Frequency)
		}
		if cfg.SF > 0 {
			radio.SetSpreadingFactor(cfg.SF)
		}
		if cfg.Bandwidth > 0 {
			radio.SetBandwidth(cfg.Bandwidth)
		}

		return radio, nil

	case strings.HasPrefix(cfg.Device, "/dev/spidev"):
		radio, err := NewSX127xSPI(cfg.Device, cfg.ResetPin, cfg.DIO0Pin)
		if err != nil {
			return nil, err
		}

		// Configure parameters
		if cfg.Frequency > 0 {
			if err := radio.SetFrequency(cfg.Frequency); err != nil {
				radio.Close()
				return nil, err
			}
		}
		if cfg.SF > 0 {
			if err := radio.SetSpreadingFactor(cfg.SF); err != nil {
				radio.Close()
				return nil, err
			}
		}
		if cfg.Bandwidth > 0 {
			if err := radio.SetBandwidth(cfg.Bandwidth); err != nil {
				radio.Close()
				return nil, err
			}
		}

		return radio, nil

	case strings.HasPrefix(cfg.Device, "/dev/tty"):
		// UART/USB-Serial support deferred to Phase 2
		return nil, fmt.Errorf("UART/USB-Serial LoRa modules not yet implemented (device: %s)", cfg.Device)

	default:
		return nil, fmt.Errorf("unsupported device: %s", cfg.Device)
	}
}
