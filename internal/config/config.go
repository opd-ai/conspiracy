// Package config provides TOML configuration parsing and validation.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Config represents the complete conspiracyd configuration.
type Config struct {
	LoRa   LoRaConfig   `toml:"lora"`
	WiFi   WiFiConfig   `toml:"wifi"`
	Batman BatmanConfig `toml:"batman"`
	Plugin PluginConfig `toml:"plugins"`
}

// LoRaConfig holds LoRa radio settings.
type LoRaConfig struct {
	Device       string  `toml:"device"`        // "/dev/spidev0.0", "/dev/ttyUSB0", "udp"
	FrequencyMHz float64 `toml:"frequency_mhz"` // EU: 868.1, US: 915
	Spreading    int     `toml:"spreading"`     // SF7-SF12
	BandwidthKHz int     `toml:"bandwidth_khz"` // 125, 250, 500
	MeshKey      string  `toml:"mesh_key"`      // hex:aabbcc... (64 hex chars = 32 bytes)
	ResetPin     string  `toml:"reset_pin"`     // GPIO pin for RESET (e.g., "GPIO25")
	DIO0Pin      string  `toml:"dio0_pin"`      // GPIO pin for DIO0 (e.g., "GPIO24")

	// For UDP testing
	UDPListen string `toml:"udp_listen"` // "host:port"
	UDPPeer   string `toml:"udp_peer"`   // "host:port"
}

// WiFiConfig holds 802.11s mesh settings.
type WiFiConfig struct {
	MeshInterface string `toml:"mesh_interface"` // wlan0
	SSID          string `toml:"ssid"`
	Channel       int    `toml:"channel"` // 1-14
}

// BatmanConfig holds batman-adv settings.
type BatmanConfig struct {
	Interface string `toml:"interface"` // bat0
	Enabled   bool   `toml:"enabled"`
}

// PluginConfig holds layer-3 plugin settings.
type PluginConfig struct {
	Yggdrasil bool `toml:"yggdrasil"`
	CJDNS     bool `toml:"cjdns"`
}

// Load reads and validates configuration from a TOML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate checks configuration correctness.
func (c *Config) Validate() error {
	// Validate LoRa config
	if err := c.LoRa.Validate(); err != nil {
		return fmt.Errorf("lora: %w", err)
	}

	// Validate WiFi config
	if err := c.WiFi.Validate(); err != nil {
		return fmt.Errorf("wifi: %w", err)
	}

	return nil
}

// Validate checks LoRa configuration.
func (lc *LoRaConfig) Validate() error {
	// Validate device path
	if lc.Device == "" {
		return fmt.Errorf("device must be specified")
	}

	// UDP mode has different validation
	if lc.Device == "udp" {
		if lc.UDPListen == "" {
			return fmt.Errorf("udp_listen required for UDP mode")
		}
		if lc.UDPPeer == "" {
			return fmt.Errorf("udp_peer required for UDP mode")
		}
	} else {
		// Hardware device must exist
		if _, err := os.Stat(lc.Device); err != nil {
			return fmt.Errorf("device %s: %w", lc.Device, err)
		}
	}

	// Validate mesh_key
	if lc.MeshKey == "" {
		return fmt.Errorf("mesh_key must be specified")
	}

	keyBytes, err := decodeMeshKey(lc.MeshKey)
	if err != nil {
		return fmt.Errorf("mesh_key: %w", err)
	}

	if len(keyBytes) != 32 {
		return fmt.Errorf("mesh_key must be 32-byte hex string (got %d bytes)", len(keyBytes))
	}

	// Validate frequency
	if err := validateFrequency(lc.FrequencyMHz); err != nil {
		return err
	}

	// Validate spreading factor
	if lc.Spreading < 7 || lc.Spreading > 12 {
		return fmt.Errorf("spreading must be 7-12 (got %d)", lc.Spreading)
	}

	// Validate bandwidth
	if lc.BandwidthKHz != 125 && lc.BandwidthKHz != 250 && lc.BandwidthKHz != 500 {
		return fmt.Errorf("bandwidth_khz must be 125, 250, or 500 (got %d)", lc.BandwidthKHz)
	}

	return nil
}

// Validate checks WiFi configuration.
func (wc *WiFiConfig) Validate() error {
	if wc.MeshInterface == "" {
		return fmt.Errorf("mesh_interface must be specified")
	}

	if wc.SSID == "" {
		return fmt.Errorf("ssid must be specified")
	}

	if wc.Channel < 1 || wc.Channel > 14 {
		return fmt.Errorf("channel must be 1-14 (got %d)", wc.Channel)
	}

	return nil
}

// DecodeMeshKey returns the raw bytes of the mesh key.
func (lc *LoRaConfig) DecodeMeshKey() ([]byte, error) {
	return decodeMeshKey(lc.MeshKey)
}

// decodeMeshKey decodes hex-encoded mesh key.
func decodeMeshKey(key string) ([]byte, error) {
	// Remove "hex:" prefix if present
	key = strings.TrimPrefix(key, "hex:")

	// Decode hex
	decoded, err := hex.DecodeString(key)
	if err != nil {
		return nil, fmt.Errorf("invalid hex encoding: %w", err)
	}

	return decoded, nil
}

// validateFrequency checks if frequency is in valid regional bands.
func validateFrequency(freq float64) error {
	// EU bands: 863-870 MHz
	if freq >= 863 && freq <= 870 {
		return nil
	}

	// US/AU bands: 902-928 MHz
	if freq >= 902 && freq <= 928 {
		return nil
	}

	// Asia bands: 433 MHz or 920 MHz
	if freq >= 430 && freq <= 435 {
		return nil
	}
	if freq >= 915 && freq <= 925 {
		return nil
	}

	return fmt.Errorf("frequency %.1f MHz out of band; valid ranges: EU 863-870, US/AU 902-928, AS 430-435 or 915-925", freq)
}
