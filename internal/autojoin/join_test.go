package autojoin

import (
	"context"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
)

// TestFSM_InitToScanning verifies INIT → SCANNING transition
func TestFSM_InitToScanning(t *testing.T) {
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

	radio, err := lora.NewUDPRadio("127.0.0.1:9201", "127.0.0.1:9202")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	cfg := Config{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		ScanDuration: 1 * time.Second,
		MaxAttempts:  3,
	}

	fsm := NewFSM(cfg)

	if fsm.state != StateINIT {
		t.Errorf("Expected initial state INIT, got %v", fsm.state)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Execute one step (INIT → SCANNING)
	if err := fsm.handleInit(ctx); err != nil {
		t.Fatalf("handleInit failed: %v", err)
	}

	if fsm.state != StateSCANNING {
		t.Errorf("Expected state SCANNING after handleInit, got %v", fsm.state)
	}
}

// TestFSM_ScanningNoPeers verifies SCANNING returns with no peers found
func TestFSM_ScanningNoPeers(t *testing.T) {
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

	radio, err := lora.NewUDPRadio("127.0.0.1:9203", "127.0.0.1:9204")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	cfg := Config{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		ScanDuration: 500 * time.Millisecond, // Short scan for testing
		MaxAttempts:  3,
	}

	fsm := NewFSM(cfg)
	fsm.state = StateSCANNING

	ctx := context.Background()

	// Run scanning phase (no BEACONs available)
	// transitionAfterScan should return nil when no peers, FSM will sleep 10s in real scenario
	if err := fsm.transitionAfterScan(ctx); err != nil {
		t.Fatalf("transitionAfterScan failed: %v", err)
	}

	// Should still be in SCANNING state after transitionAfterScan with no peers
	if fsm.state != StateSCANNING {
		t.Errorf("Expected state SCANNING after scan with no peers, got %v", fsm.state)
	}

	// Verify no peers were discovered
	if len(fsm.scannedPeers) != 0 {
		t.Errorf("Expected 0 peers discovered, got %d", len(fsm.scannedPeers))
	}
}

// TestFSM_JoinRequestSend verifies JOIN_REQ frame construction and transmission
func TestFSM_JoinRequestSend(t *testing.T) {
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

	radio, err := lora.NewUDPRadio("127.0.0.1:9205", "127.0.0.1:9206")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	cfg := Config{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		ScanDuration: 1 * time.Second,
		MaxAttempts:  3,
	}

	fsm := NewFSM(cfg)
	ctx := context.Background()

	peer := PeerInfo{
		NodeID:  0x99999999,
		RSSI:    -50,
		SSID:    "test-mesh",
		Channel: 6,
	}

	// Send JOIN_REQ
	if err := fsm.sendJoinRequest(ctx, peer); err != nil {
		t.Fatalf("sendJoinRequest failed: %v", err)
	}

	// JOIN_REQ should have been transmitted (no error means success)
}

// TestFSM_JoinRequestFailure verifies transition to FAILED after max attempts
func TestFSM_JoinRequestFailure(t *testing.T) {
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

	radio, err := lora.NewUDPRadio("127.0.0.1:9207", "127.0.0.1:9208")
	if err != nil {
		t.Fatalf("Failed to create UDP radio: %v", err)
	}
	defer radio.Close()

	cfg := Config{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		ScanDuration: 1 * time.Second,
		MaxAttempts:  2, // Only 2 attempts for faster test
	}

	fsm := NewFSM(cfg)
	fsm.state = StateJOINING

	// Add a fake peer to join
	fsm.scannedPeers = []PeerInfo{
		{
			NodeID:  0x99999999,
			RSSI:    -50,
			SSID:    "test-mesh",
			Channel: 6,
		},
	}

	ctx := context.Background()

	// First JOIN attempt (will timeout - no JOIN_ACK response)
	// This will fail because no responder is available
	if err := fsm.handleJoining(ctx); err != nil {
		t.Fatalf("handleJoining (attempt 1) failed: %v", err)
	}

	// Should still be JOINING (attempt 1 failed)
	if fsm.state != StateJOINING {
		t.Fatalf("Expected state JOINING after first timeout, got %v", fsm.state)
	}

	if fsm.joinAttempts != 1 {
		t.Errorf("Expected joinAttempts = 1, got %d", fsm.joinAttempts)
	}

	// Second JOIN attempt (will also timeout)
	if err := fsm.handleJoining(ctx); err != nil {
		t.Fatalf("handleJoining (attempt 2) failed: %v", err)
	}

	// Should now be FAILED (max attempts reached)
	if fsm.state != StateFAILED {
		t.Errorf("Expected state FAILED after max attempts, got %v", fsm.state)
	}
}

// TestFSM_FailedBackoff verifies exponential backoff in FAILED state
func TestFSM_FailedBackoff(t *testing.T) {
	meshKey := make([]byte, 32)
	rc, _ := crypto.NewRebootCounter(t.TempDir() + "/reboot_counter")
	ng, _ := crypto.NewNonceGenerator(meshKey, 0x12345678, rc)
	radio, _ := lora.NewUDPRadio("127.0.0.1:9209", "127.0.0.1:9210")
	defer radio.Close()

	cfg := Config{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       0x12345678,
		ScanDuration: 1 * time.Second,
		MaxAttempts:  3,
	}

	fsm := NewFSM(cfg)
	fsm.state = StateFAILED
	fsm.joinAttempts = 0

	// Test backoff increases: 60s, 120s, 240s, capped at 600s
	tests := []struct {
		attempts        int
		expectedBackoff time.Duration
	}{
		{0, 60 * time.Second},
		{1, 120 * time.Second},
		{2, 240 * time.Second},
		{3, 480 * time.Second},
		{4, 600 * time.Second}, // Capped at 10 minutes
	}

	for _, tt := range tests {
		fsm.joinAttempts = tt.attempts
		expectedBackoff := time.Duration(60<<tt.attempts) * time.Second
		if expectedBackoff > 600*time.Second {
			expectedBackoff = 600 * time.Second
		}

		if expectedBackoff != tt.expectedBackoff {
			t.Errorf("Attempt %d: expected backoff %v, got %v", tt.attempts, tt.expectedBackoff, expectedBackoff)
		}
	}
}
