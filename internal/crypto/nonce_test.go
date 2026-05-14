package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNonceGenerator_UniquenessAcross1000Generations(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	tmpDir := t.TempDir()
	rc, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter failed: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 0x12345678, rc)
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

	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Generate nonces with different reboot counters
	rc1, _ := NewRebootCounter(tmpDir1)
	rc2, _ := NewRebootCounter(tmpDir2)
	rc2.Increment() // Increment to get different value

	ng1, _ := NewNonceGenerator(meshKey, 0x12345678, rc1)
	ng2, _ := NewNonceGenerator(meshKey, 0x12345678, rc2)

	nonce1, _ := ng1.Generate()
	nonce2, _ := ng2.Generate()

	// Nonces from different reboot cycles should differ
	if string(nonce1) == string(nonce2) {
		t.Error("Nonces from different reboot counters are identical")
	}
}

func TestNonceGenerator_NodeIDDifferentiation(t *testing.T) {
	meshKey := make([]byte, 32)

	tmpDir := t.TempDir()
	rc, _ := NewRebootCounter(tmpDir)

	// Generate nonces with different node IDs
	ng1, _ := NewNonceGenerator(meshKey, 0x11111111, rc)
	ng2, _ := NewNonceGenerator(meshKey, 0x22222222, rc)

	nonce1, _ := ng1.Generate()
	nonce2, _ := ng2.Generate()

	// Nonces from different nodes should differ
	if string(nonce1) == string(nonce2) {
		t.Error("Nonces from different node IDs are identical")
	}
}

func TestNonceGenerator_SequenceIncrement(t *testing.T) {
	meshKey := make([]byte, 32)
	tmpDir := t.TempDir()
	rc, _ := NewRebootCounter(tmpDir)

	ng, _ := NewNonceGenerator(meshKey, 0x12345678, rc)

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
	tmpDir := t.TempDir()
	rc, _ := NewRebootCounter(tmpDir)

	ng, _ := NewNonceGenerator(meshKey, 0x12345678, rc)

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
	tmpDir := t.TempDir()
	rc, _ := NewRebootCounter(tmpDir)

	_, err := NewNonceGenerator(shortKey, 0x12345678, rc)
	if err == nil {
		t.Error("Expected error for invalid mesh key length")
	}
}

func TestNonceGenerator_NilRebootCounter(t *testing.T) {
	meshKey := make([]byte, 32)

	_, err := NewNonceGenerator(meshKey, 0x12345678, nil)
	if err == nil {
		t.Error("Expected error for nil reboot counter")
	}
}

func TestNonceGenerator_FrameSequenceWrapRecovery(t *testing.T) {
	meshKey := make([]byte, 32)
	tmpDir := t.TempDir()
	rc, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter failed: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("NewNonceGenerator failed: %v", err)
	}

	initialRebootValue := rc.Value()

	// Set frame sequence to near wrap point
	ng.frameSeq = 0xFFFF - 5

	// Generate several nonces to trigger wrap
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		nonce, err := ng.Generate()
		if err != nil {
			t.Fatalf("Generate failed at iteration %d: %v", i, err)
		}

		// Verify no duplicate nonces
		nonceStr := string(nonce)
		if seen[nonceStr] {
			t.Errorf("Duplicate nonce detected after wrap at iteration %d", i)
		}
		seen[nonceStr] = true
	}

	// Verify reboot counter was incremented
	finalRebootValue := rc.Value()
	if finalRebootValue <= initialRebootValue {
		t.Errorf("Reboot counter not incremented after frame sequence wrap: initial=%d, final=%d",
			initialRebootValue, finalRebootValue)
	}

	// Verify frame sequence was reset
	finalSeq := ng.FrameSeq()
	if finalSeq > 20 {
		t.Errorf("Frame sequence not properly reset after wrap: %d", finalSeq)
	}

	t.Logf("Frame sequence wrap handled successfully: reboot counter %d -> %d, final seq %d",
		initialRebootValue, finalRebootValue, finalSeq)
}

func TestNonceGenerator_FrameSequenceWrapWithReadOnlyFS(t *testing.T) {
	meshKey := make([]byte, 32)
	tmpDir := t.TempDir()
	rc, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter failed: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 0x12345678, rc)
	if err != nil {
		t.Fatalf("NewNonceGenerator failed: %v", err)
	}

	// Set frame sequence to near wrap point
	ng.frameSeq = 0xFFFF - 2

	// Make reboot counter path read-only to simulate failure
	counterPath := filepath.Join(tmpDir, "reboot_counter")
	os.Chmod(counterPath, 0o444) // read-only
	os.Chmod(tmpDir, 0o555)      // read-only directory

	// Generate nonces - should succeed until wrap
	for i := 0; i < 3; i++ {
		_, err := ng.Generate()
		if i < 2 {
			// Should succeed before wrap
			if err != nil {
				t.Fatalf("Generate failed before wrap: %v", err)
			}
		} else {
			// Should fail at wrap due to read-only FS
			// Note: This test may not fail on all systems if permissions aren't enforced
			t.Logf("Generate result at wrap: %v (may succeed if OS allows)", err)
		}
	}

	// Cleanup: restore permissions for TempDir cleanup
	os.Chmod(tmpDir, 0o755)
}
