package lora

import (
	"context"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
)

// TestBeaconTransmitter_Creation verifies beacon transmitter initialization
func TestBeaconTransmitter_Creation(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio, err := NewUDPRadio("127.0.0.1:9101", "127.0.0.1:9102")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))
	copy(payload.BSSID[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})

	tests := []struct {
		name      string
		cfg       BeaconConfig
		wantErr   bool
		errString string
	}{
		{
			name: "valid_config",
			cfg: BeaconConfig{
				Radio:        radio,
				NonceGen:     ng,
				MeshKey:      meshKey,
				NodeID:       0x12345678,
				Payload:      payload,
				Interval:     60 * time.Second,
				DutyCyclePct: 1.0,
			},
			wantErr: false,
		},
		{
			name: "nil_nonce_generator",
			cfg: BeaconConfig{
				Radio:        radio,
				NonceGen:     nil,
				MeshKey:      meshKey,
				NodeID:       0x12345678,
				Payload:      payload,
				Interval:     60 * time.Second,
				DutyCyclePct: 1.0,
			},
			wantErr:   true,
			errString: "nonce generator cannot be nil",
		},
		{
			name: "invalid_mesh_key",
			cfg: BeaconConfig{
				Radio:        radio,
				NonceGen:     ng,
				MeshKey:      make([]byte, 16),
				NodeID:       0x12345678,
				Payload:      payload,
				Interval:     60 * time.Second,
				DutyCyclePct: 1.0,
			},
			wantErr:   true,
			errString: "invalid mesh key length",
		},
		{
			name: "nil_payload",
			cfg: BeaconConfig{
				Radio:        radio,
				NonceGen:     ng,
				MeshKey:      meshKey,
				NodeID:       0x12345678,
				Payload:      nil,
				Interval:     60 * time.Second,
				DutyCyclePct: 1.0,
			},
			wantErr:   true,
			errString: "BEACON payload cannot be nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt, err := NewBeaconTransmitter(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewBeaconTransmitter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil && tt.errString != "" {
				if err.Error() != tt.errString && len(err.Error()) < len(tt.errString) {
					t.Errorf("Expected error containing %q, got %q", tt.errString, err.Error())
				}
			}
			if !tt.wantErr && bt == nil {
				t.Errorf("Expected valid BeaconTransmitter, got nil")
			}
		})
	}
}

// TestBeaconTransmitter_SingleTransmission verifies a single BEACON transmission
func TestBeaconTransmitter_SingleTransmission(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio, err := NewUDPRadio("127.0.0.1:9103", "127.0.0.1:9104")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))
	copy(payload.BSSID[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})

	cfg := BeaconConfig{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		Payload:      payload,
		Interval:     1 * time.Second,
		DutyCyclePct: 100.0, // No duty-cycle limit for testing
	}

	bt, err := NewBeaconTransmitter(cfg)
	if err != nil {
		t.Fatalf("Failed to create BeaconTransmitter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Transmit single BEACON
	if err := bt.transmitBeacon(ctx); err != nil {
		t.Fatalf("transmitBeacon() failed: %v", err)
	}

	// Verify transmission was recorded
	if len(bt.txWindow.txLog) != 1 {
		t.Errorf("Expected 1 TX event, got %d", len(bt.txWindow.txLog))
	}
}

// TestDutyCycleWindow_Enforcement verifies duty-cycle limit enforcement
func TestDutyCycleWindow_Enforcement(t *testing.T) {
	// 1% duty cycle = 36 seconds per hour
	maxTX := 36 * time.Second
	window := &DutyCycleWindow{
		maxTXTime:   maxTX,
		txLog:       make([]time.Time, 0),
		txDurations: make([]time.Duration, 0),
	}

	// Test initial state
	if !window.CanTransmit() {
		t.Errorf("Expected CanTransmit() = true initially")
	}

	// Record 30s of TX time (should still be under limit)
	now := time.Now()
	for i := 0; i < 30; i++ {
		window.RecordTX(now.Add(time.Duration(i)*time.Second), 1*time.Second)
	}

	if !window.CanTransmit() {
		t.Errorf("Expected CanTransmit() = true after 30s TX (limit: 36s)")
	}

	// Record another 10s (total 40s, exceeds limit)
	for i := 0; i < 10; i++ {
		window.RecordTX(now.Add(time.Duration(30+i)*time.Second), 1*time.Second)
	}

	if window.CanTransmit() {
		t.Errorf("Expected CanTransmit() = false after 40s TX (limit: 36s)")
	}

	// Verify current TX time
	currentTX := window.currentTXTime()
	if currentTX != 40*time.Second {
		t.Errorf("Expected currentTXTime() = 40s, got %v", currentTX)
	}

	// Verify remaining time is zero
	remaining := window.RemainingTXTime()
	if remaining != 0 {
		t.Errorf("Expected RemainingTXTime() = 0, got %v", remaining)
	}
}

// TestDutyCycleWindow_Pruning verifies old TX events are pruned after 1 hour
func TestDutyCycleWindow_Pruning(t *testing.T) {
	window := &DutyCycleWindow{
		maxTXTime:   36 * time.Second,
		txLog:       make([]time.Time, 0),
		txDurations: make([]time.Duration, 0),
	}

	now := time.Now()

	// Record TX events: 20 from 2 hours ago, 30 from 30 minutes ago
	for i := 0; i < 20; i++ {
		window.txLog = append(window.txLog, now.Add(-2*time.Hour))
		window.txDurations = append(window.txDurations, 1*time.Second)
	}

	for i := 0; i < 30; i++ {
		window.txLog = append(window.txLog, now.Add(-30*time.Minute))
		window.txDurations = append(window.txDurations, 1*time.Second)
	}

	// Prune old entries
	window.pruneOldEntries()

	// Only events from last hour (30 events from 30 min ago) should remain
	if len(window.txLog) != 30 {
		t.Errorf("Expected 30 TX events after pruning, got %d", len(window.txLog))
	}

	// Current TX time should be 30s
	currentTX := window.currentTXTime()
	if currentTX != 30*time.Second {
		t.Errorf("Expected currentTXTime() = 30s after pruning, got %v", currentTX)
	}
}

// TestBeaconTransmitter_AdaptiveBackoff verifies interval increases when duty-cycle exceeded
func TestBeaconTransmitter_AdaptiveBackoff(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio, err := NewUDPRadio("127.0.0.1:9105", "127.0.0.1:9106")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))

	cfg := BeaconConfig{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		Payload:      payload,
		Interval:     60 * time.Second,
		DutyCyclePct: 1.0,
	}

	bt, err := NewBeaconTransmitter(cfg)
	if err != nil {
		t.Fatalf("Failed to create BeaconTransmitter: %v", err)
	}

	initialInterval := bt.interval

	// Simulate duty-cycle limit reached by filling TX window
	now := time.Now()
	for i := 0; i < 40; i++ {
		bt.txWindow.RecordTX(now.Add(time.Duration(i)*time.Second), 1*time.Second)
	}

	// Trigger adaptive backoff
	bt.adaptiveBackoff()

	// Interval should have doubled
	expectedInterval := initialInterval * 2
	if bt.interval != expectedInterval {
		t.Errorf("Expected interval = %v after backoff, got %v", expectedInterval, bt.interval)
	}

	// Trigger again
	bt.adaptiveBackoff()
	expectedInterval = initialInterval * 4
	if bt.interval != expectedInterval {
		t.Errorf("Expected interval = %v after 2nd backoff, got %v", expectedInterval, bt.interval)
	}
}

// TestBeaconTransmitter_EncryptionIntegrity verifies BEACON encryption/HMAC
func TestBeaconTransmitter_EncryptionIntegrity(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	// Create a receiver radio to verify transmission
	receiverRadio, err := NewUDPRadio("127.0.0.1:9108", "127.0.0.1:9107")
	if err != nil {
		t.Fatalf("Failed to create receiver radio: %v", err)
	}
	senderRadio, err := NewUDPRadio("127.0.0.1:9107", "127.0.0.1:9108")
	if err != nil {
		t.Fatalf("Failed to create sender radio: %v", err)
	}

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))
	copy(payload.BSSID[:], []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff})

	cfg := BeaconConfig{
		Radio:        senderRadio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		Payload:      payload,
		Interval:     60 * time.Second,
		DutyCyclePct: 100.0,
	}

	bt, err := NewBeaconTransmitter(cfg)
	if err != nil {
		t.Fatalf("Failed to create BeaconTransmitter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Transmit BEACON
	if err := bt.transmitBeacon(ctx); err != nil {
		t.Fatalf("transmitBeacon() failed: %v", err)
	}

	// Receive and verify
	rxCtx, rxCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer rxCancel()

	data, err := receiverRadio.Recv(rxCtx)
	if err != nil {
		t.Fatalf("Failed to receive BEACON: %v", err)
	}

	// Parse frame
	hdr, encPayload, err := UnmarshalFrame(data)
	if err != nil {
		t.Fatalf("Failed to unmarshal frame: %v", err)
	}

	if hdr.FrameType != FrameTypeBEACON {
		t.Errorf("Expected FrameType = %d, got %d", FrameTypeBEACON, hdr.FrameType)
	}

	// Decrypt payload
	plaintext, err := crypto.Decrypt(meshKey, hdr.Nonce, encPayload)
	if err != nil {
		t.Fatalf("Failed to decrypt BEACON payload: %v", err)
	}

	// Parse BEACON
	rxBeacon, err := UnmarshalBEACONPayload(plaintext)
	if err != nil {
		t.Fatalf("Failed to parse BEACON payload: %v", err)
	}

	// Verify SSID
	if string(rxBeacon.SSID[:9]) != "test-mesh" {
		t.Errorf("Expected SSID = 'test-mesh', got '%s'", string(rxBeacon.SSID[:9]))
	}

	// Verify channel
	if rxBeacon.Channel != 6 {
		t.Errorf("Expected Channel = 6, got %d", rxBeacon.Channel)
	}
}

// TestBeaconTransmitter_AdaptiveInterval verifies adaptive interval calculation based on peer count
func TestBeaconTransmitter_AdaptiveInterval(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio, err := NewUDPRadio("127.0.0.1:9201", "127.0.0.1:9202")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))

	bt, err := NewBeaconTransmitter(BeaconConfig{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		Payload:      payload,
		Interval:     60 * time.Second,
		DutyCyclePct: 1.0,
	})
	if err != nil {
		t.Fatalf("Failed to create beacon transmitter: %v", err)
	}

	tests := []struct {
		name               string
		peerCount          int
		expectedInterval   time.Duration
		expectedMultiplier float64
	}{
		{
			name:               "0 peers",
			peerCount:          0,
			expectedInterval:   60 * time.Second,
			expectedMultiplier: 1.0,
		},
		{
			name:               "100 peers",
			peerCount:          100,
			expectedInterval:   120 * time.Second,
			expectedMultiplier: 2.0,
		},
		{
			name:               "200 peers",
			peerCount:          200,
			expectedInterval:   180 * time.Second,
			expectedMultiplier: 3.0,
		},
		{
			name:               "500 peers",
			peerCount:          500,
			expectedInterval:   360 * time.Second,
			expectedMultiplier: 6.0,
		},
		{
			name:               "1000 peers (capped at 600s)",
			peerCount:          1000,
			expectedInterval:   600 * time.Second,
			expectedMultiplier: 11.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bt.UpdatePeerCount(tt.peerCount)

			interval := bt.GetInterval()
			if interval != tt.expectedInterval {
				t.Errorf("Expected interval = %v, got %v", tt.expectedInterval, interval)
			}

			peerCount := bt.GetPeerCount()
			if peerCount != tt.peerCount {
				t.Errorf("Expected peer count = %d, got %d", tt.peerCount, peerCount)
			}
		})
	}
}

// TestBeaconTransmitter_PeerCountWarning verifies warning log at 100 nodes
func TestBeaconTransmitter_PeerCountWarning(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	rc, err := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio, err := NewUDPRadio("127.0.0.1:9301", "127.0.0.1:9302")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	payload := &BEACONPayload{
		Channel:      6,
		Capabilities: 0x01,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(payload.SSID[:], []byte("test-mesh"))

	bt, err := NewBeaconTransmitter(BeaconConfig{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		Payload:      payload,
		Interval:     60 * time.Second,
		DutyCyclePct: 1.0,
	})
	if err != nil {
		t.Fatalf("Failed to create beacon transmitter: %v", err)
	}

	// Warning should not be logged yet
	if bt.peerCountWarn {
		t.Error("Expected peerCountWarn = false initially")
	}

	// Update to 50 peers - no warning
	bt.UpdatePeerCount(50)
	if bt.peerCountWarn {
		t.Error("Expected peerCountWarn = false at 50 peers")
	}

	// Update to 100 peers - warning should be logged
	bt.UpdatePeerCount(100)
	if !bt.peerCountWarn {
		t.Error("Expected peerCountWarn = true at 100 peers")
	}

	// Update to 150 peers - warning should not be logged again
	oldWarn := bt.peerCountWarn
	bt.UpdatePeerCount(150)
	if bt.peerCountWarn != oldWarn {
		t.Error("Expected peerCountWarn to remain true (no duplicate warning)")
	}
}
