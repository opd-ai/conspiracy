package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Create test device file
	devicePath := filepath.Join(tmpDir, "test_device")
	if err := os.WriteFile(devicePath, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	configContent := `
[lora]
device = "` + devicePath + `"
frequency_mhz = 868.1
spreading = 10
bandwidth_khz = 125
mesh_key = "hex:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
reset_pin = "GPIO25"
dio0_pin = "GPIO24"

[wifi]
mesh_interface = "wlan0"
ssid = "conspiracy-mesh"
channel = 6

[batman]
interface = "bat0"
enabled = true

[plugins]
yggdrasil = true
cjdns = false
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Validate LoRa config
	if cfg.LoRa.Device != devicePath {
		t.Errorf("Device = %s, want %s", cfg.LoRa.Device, devicePath)
	}
	if cfg.LoRa.FrequencyMHz != 868.1 {
		t.Errorf("FrequencyMHz = %f, want 868.1", cfg.LoRa.FrequencyMHz)
	}
	if cfg.LoRa.Spreading != 10 {
		t.Errorf("Spreading = %d, want 10", cfg.LoRa.Spreading)
	}
	if cfg.LoRa.BandwidthKHz != 125 {
		t.Errorf("BandwidthKHz = %d, want 125", cfg.LoRa.BandwidthKHz)
	}

	// Validate mesh key decodes correctly
	keyBytes, err := cfg.LoRa.DecodeMeshKey()
	if err != nil {
		t.Fatalf("DecodeMeshKey failed: %v", err)
	}
	if len(keyBytes) != 32 {
		t.Errorf("Mesh key length = %d, want 32", len(keyBytes))
	}

	// Validate WiFi config
	if cfg.WiFi.SSID != "conspiracy-mesh" {
		t.Errorf("SSID = %s, want conspiracy-mesh", cfg.WiFi.SSID)
	}
	if cfg.WiFi.Channel != 6 {
		t.Errorf("Channel = %d, want 6", cfg.WiFi.Channel)
	}

	// Validate Batman config
	if !cfg.Batman.Enabled {
		t.Error("Batman.Enabled = false, want true")
	}

	// Validate plugins
	if !cfg.Plugin.Yggdrasil {
		t.Error("Plugin.Yggdrasil = false, want true")
	}
	if cfg.Plugin.CJDNS {
		t.Error("Plugin.CJDNS = true, want false")
	}
}

func TestLoad_InvalidMeshKey(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Create test device file
	devicePath := filepath.Join(tmpDir, "test_device")
	if err := os.WriteFile(devicePath, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	// 16-byte key (invalid, must be 32)
	configContent := `
[lora]
device = "` + devicePath + `"
frequency_mhz = 868.1
spreading = 10
bandwidth_khz = 125
mesh_key = "hex:0102030405060708090a0b0c0d0e0f10"

[wifi]
mesh_interface = "wlan0"
ssid = "test"
channel = 6
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load should fail with invalid mesh key")
	}

	expectedMsg := "mesh_key must be 32-byte hex string (got 16 bytes)"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestLoad_InvalidFrequency(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Create test device file
	devicePath := filepath.Join(tmpDir, "test_device")
	if err := os.WriteFile(devicePath, []byte{}, 0o644); err != nil {
		t.Fatalf("Failed to create test device: %v", err)
	}

	configContent := `
[lora]
device = "` + devicePath + `"
frequency_mhz = 999.0
spreading = 10
bandwidth_khz = 125
mesh_key = "hex:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"

[wifi]
mesh_interface = "wlan0"
ssid = "test"
channel = 6
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Fatal("Load should fail with invalid frequency")
	}

	expectedMsg := "frequency 999.0 MHz out of band"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestLoad_UDPMode(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	configContent := `
[lora]
device = "udp"
frequency_mhz = 868.1
spreading = 10
bandwidth_khz = 125
mesh_key = "hex:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
udp_listen = "127.0.0.1:9000"
udp_peer = "127.0.0.1:9001"

[wifi]
mesh_interface = "wlan0"
ssid = "test"
channel = 6
`

	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.LoRa.Device != "udp" {
		t.Errorf("Device = %s, want udp", cfg.LoRa.Device)
	}
	if cfg.LoRa.UDPListen != "127.0.0.1:9000" {
		t.Errorf("UDPListen = %s, want 127.0.0.1:9000", cfg.LoRa.UDPListen)
	}
	if cfg.LoRa.UDPPeer != "127.0.0.1:9001" {
		t.Errorf("UDPPeer = %s, want 127.0.0.1:9001", cfg.LoRa.UDPPeer)
	}
}

func TestValidate_InvalidSpreadingFactor(t *testing.T) {
	lc := LoRaConfig{
		Device:       "udp",
		FrequencyMHz: 868.1,
		Spreading:    13, // Invalid, must be 7-12
		BandwidthKHz: 125,
		MeshKey:      "hex:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		UDPListen:    "127.0.0.1:9000",
		UDPPeer:      "127.0.0.1:9001",
	}

	err := lc.Validate()
	if err == nil {
		t.Fatal("Validate should fail with invalid spreading factor")
	}

	expectedMsg := "spreading must be 7-12"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestValidate_InvalidBandwidth(t *testing.T) {
	lc := LoRaConfig{
		Device:       "udp",
		FrequencyMHz: 868.1,
		Spreading:    10,
		BandwidthKHz: 100, // Invalid, must be 125/250/500
		MeshKey:      "hex:0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		UDPListen:    "127.0.0.1:9000",
		UDPPeer:      "127.0.0.1:9001",
	}

	err := lc.Validate()
	if err == nil {
		t.Fatal("Validate should fail with invalid bandwidth")
	}

	expectedMsg := "bandwidth_khz must be 125, 250, or 500"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestValidate_InvalidChannel(t *testing.T) {
	wc := WiFiConfig{
		MeshInterface: "wlan0",
		SSID:          "test",
		Channel:       15, // Invalid, must be 1-14
	}

	err := wc.Validate()
	if err == nil {
		t.Fatal("Validate should fail with invalid channel")
	}

	expectedMsg := "channel must be 1-14"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
