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
		return createUDPRadio(cfg)
	case strings.HasPrefix(cfg.Device, "/dev/spidev"):
		return createSPIRadio(cfg)
	case strings.HasPrefix(cfg.Device, "/dev/tty"):
		return nil, fmt.Errorf("UART/USB-Serial LoRa modules not yet implemented (device: %s)", cfg.Device)
	default:
		return nil, fmt.Errorf("unsupported device: %s", cfg.Device)
	}
}

// createUDPRadio creates and configures a UDP radio for testing.
func createUDPRadio(cfg Config) (PacketRadio, error) {
	if cfg.UDPListen == "" || cfg.UDPPeer == "" {
		return nil, fmt.Errorf("UDP mode requires UDPListen and UDPPeer")
	}

	radio, err := NewUDPRadio(cfg.UDPListen, cfg.UDPPeer)
	if err != nil {
		return nil, err
	}

	applyRadioConfig(radio, cfg)
	return radio, nil
}

// createSPIRadio creates and configures an SPI-based LoRa radio.
func createSPIRadio(cfg Config) (PacketRadio, error) {
	radio, err := NewSX127xSPI(cfg.Device, cfg.ResetPin, cfg.DIO0Pin)
	if err != nil {
		return nil, err
	}

	if err := applyAndValidateSPIConfig(radio, cfg); err != nil {
		radio.Close()
		return nil, err
	}

	return radio, nil
}

// applyRadioConfig sets frequency, SF, and bandwidth for UDP radios.
func applyRadioConfig(radio *UDPRadio, cfg Config) {
	if cfg.Frequency > 0 {
		radio.SetFrequency(cfg.Frequency)
	}
	if cfg.SF > 0 {
		radio.SetSpreadingFactor(cfg.SF)
	}
	if cfg.Bandwidth > 0 {
		radio.SetBandwidth(cfg.Bandwidth)
	}
}

// applyAndValidateSPIConfig sets and validates parameters for SPI radios.
func applyAndValidateSPIConfig(radio *SX127xSPI, cfg Config) error {
	if err := applyFrequencyConfig(radio, cfg.Frequency); err != nil {
		return err
	}
	if err := applySpreadingFactorConfig(radio, cfg.SF); err != nil {
		return err
	}
	if err := applyBandwidthConfig(radio, cfg.Bandwidth); err != nil {
		return err
	}
	return nil
}

// applyFrequencyConfig sets frequency if configured.
func applyFrequencyConfig(radio *SX127xSPI, frequency float64) error {
	if frequency > 0 {
		return radio.SetFrequency(frequency)
	}
	return nil
}

// applySpreadingFactorConfig sets spreading factor if configured.
func applySpreadingFactorConfig(radio *SX127xSPI, sf int) error {
	if sf > 0 {
		return radio.SetSpreadingFactor(sf)
	}
	return nil
}

// applyBandwidthConfig sets bandwidth if configured.
func applyBandwidthConfig(radio *SX127xSPI, bandwidth int) error {
	if bandwidth > 0 {
		return radio.SetBandwidth(bandwidth)
	}
	return nil
}
