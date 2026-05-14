package crypto

import (
	"testing"
)

func TestNonceGenerator_UniquenessAcross1000Generations(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	ng, err := NewNonceGenerator(meshKey, 0x12345678, 1)
	if err != nil {
		t.Fatalf("NewNonceGenerator failed: %v", err)
	}

	// Generate 1,000 nonces and verify uniqueness
	seen := make(map[string]bool)

	for i := 0; i < 1000; i++ {
		nonce, err := ng.Generate()
		if err != nil {
			t.Fatalf("Generate failed on iteration %d: %v", i, err)
		}

		if len(nonce) != 12 {
			t.Errorf("Nonce length = %d, want 12", len(nonce))
		}

		nonceStr := string(nonce)
		if seen[nonceStr] {
			t.Errorf("Duplicate nonce detected at iteration %d", i)
		}
		seen[nonceStr] = true
	}

	if len(seen) != 1000 {
		t.Errorf("Generated %d unique nonces, want 1000", len(seen))
	}
}

func TestNonceGenerator_RebootCounterDifferentiation(t *testing.T) {
	meshKey := make([]byte, 32)

	// Generate nonces with different reboot counters
	ng1, _ := NewNonceGenerator(meshKey, 0x12345678, 1)
	ng2, _ := NewNonceGenerator(meshKey, 0x12345678, 2)

	nonce1, _ := ng1.Generate()
	nonce2, _ := ng2.Generate()

	// Nonces from different reboot cycles should differ
	if string(nonce1) == string(nonce2) {
		t.Error("Nonces from different reboot counters are identical")
	}
}

func TestNonceGenerator_NodeIDDifferentiation(t *testing.T) {
	meshKey := make([]byte, 32)

	// Generate nonces with different node IDs
	ng1, _ := NewNonceGenerator(meshKey, 0x11111111, 1)
	ng2, _ := NewNonceGenerator(meshKey, 0x22222222, 1)

	nonce1, _ := ng1.Generate()
	nonce2, _ := ng2.Generate()

	// Nonces from different nodes should differ
	if string(nonce1) == string(nonce2) {
		t.Error("Nonces from different node IDs are identical")
	}
}

func TestNonceGenerator_SequenceIncrement(t *testing.T) {
	meshKey := make([]byte, 32)
	ng, _ := NewNonceGenerator(meshKey, 0x12345678, 1)

	initialSeq := ng.FrameSeq()

	// Generate 10 nonces
	for i := 0; i < 10; i++ {
		if _, err := ng.Generate(); err != nil {
			t.Fatalf("Generate failed: %v", err)
		}
	}

	finalSeq := ng.FrameSeq()
	if finalSeq != initialSeq+10 {
		t.Errorf("Frame sequence = %d, want %d", finalSeq, initialSeq+10)
	}
}

func TestNonceGenerator_ValidationCounter(t *testing.T) {
	meshKey := make([]byte, 32)
	ng, _ := NewNonceGenerator(meshKey, 0x12345678, 1)

	// Generate 1,001 nonces to trigger continuous entropy monitor
	for i := 0; i < 1001; i++ {
		if _, err := ng.Generate(); err != nil {
			t.Fatalf("Generate failed at iteration %d: %v", i, err)
		}
	}

	if ng.ValidationCount() != 1001 {
		t.Errorf("Validation count = %d, want 1001", ng.ValidationCount())
	}
}

func TestNonceGenerator_InvalidMeshKeyLength(t *testing.T) {
	shortKey := make([]byte, 16)

	_, err := NewNonceGenerator(shortKey, 0x12345678, 1)
	if err == nil {
		t.Error("Expected error for invalid mesh key length")
	}
}
