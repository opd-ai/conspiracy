package crypto

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRekeyManager_GenerateREKEY(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Generate first REKEY
	newKey1, keyID1, validAfter1, gen1, payload1, err := rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY failed: %v", err)
	}

	// Validate payload size
	if len(payload1) != 48 {
		t.Errorf("payload size = %d, want 48", len(payload1))
	}

	// Validate generation counter incremented
	if gen1 != 1 {
		t.Errorf("first generation = %d, want 1", gen1)
	}

	// Validate validAfter is ~24 hours in future
	now := uint32(time.Now().Unix())
	expectedValidAfter := now + 86400 // 24 hours
	if validAfter1 < expectedValidAfter-10 || validAfter1 > expectedValidAfter+10 {
		t.Errorf("validAfter = %d, want ~%d (±10s)", validAfter1, expectedValidAfter)
	}

	// Validate newKey is non-zero
	zeroKey := [32]byte{}
	if bytes.Equal(newKey1[:], zeroKey[:]) {
		t.Error("newKey is all zeros (CSPRNG failure)")
	}

	// Validate keyID computation
	expectedKeyID := ComputeKeyID(newKey1)
	if !bytes.Equal(keyID1[:], expectedKeyID[:]) {
		t.Errorf("keyID mismatch: got %x, want %x", keyID1, expectedKeyID)
	}

	// Generate second REKEY, verify generation increments
	_, _, _, gen2, _, err := rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("second GenerateREKEY failed: %v", err)
	}
	if gen2 != 2 {
		t.Errorf("second generation = %d, want 2", gen2)
	}

	// Verify keys are different
	newKey2, _, _, _, _, _ := rm.GenerateREKEY()
	if bytes.Equal(newKey1[:], newKey2[:]) {
		t.Error("consecutive GenerateREKEY produced identical keys")
	}
}

func TestRekeyManager_Persistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create manager and generate REKEY
	rm1, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	_, _, _, gen1, _, err := rm1.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY failed: %v", err)
	}

	if gen1 != 1 {
		t.Errorf("first generation = %d, want 1", gen1)
	}

	// Simulate daemon restart by creating new manager with same storageDir
	rm2, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager after restart failed: %v", err)
	}

	// CurrentGeneration should reflect persisted value
	if rm2.CurrentGeneration() != 1 {
		t.Errorf("after restart, generation = %d, want 1", rm2.CurrentGeneration())
	}

	// Generate another REKEY, verify generation continues from persisted value
	_, _, _, gen2, _, err := rm2.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY after restart failed: %v", err)
	}

	if gen2 != 2 {
		t.Errorf("generation after restart = %d, want 2", gen2)
	}
}

func TestRekeyManager_ValidateREKEY_Success(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Generate REKEY payload
	expectedNewKey, expectedKeyID, expectedValidAfter, expectedGen, payload, err := rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY failed: %v", err)
	}

	// Create second manager (simulates receiving node)
	rm2, err := NewRekeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewRekeyManager (receiver) failed: %v", err)
	}

	// Validate payload
	newKey, keyID, validAfter, gen, err := rm2.ValidateREKEY(payload)
	if err != nil {
		t.Fatalf("ValidateREKEY failed: %v", err)
	}

	// Verify fields
	if !bytes.Equal(newKey[:], expectedNewKey[:]) {
		t.Errorf("newKey mismatch")
	}
	if !bytes.Equal(keyID[:], expectedKeyID[:]) {
		t.Errorf("keyID mismatch")
	}
	if validAfter != expectedValidAfter {
		t.Errorf("validAfter = %d, want %d", validAfter, expectedValidAfter)
	}
	if gen != expectedGen {
		t.Errorf("generation = %d, want %d", gen, expectedGen)
	}

	// Verify generation counter was updated
	if rm2.CurrentGeneration() != expectedGen {
		t.Errorf("receiver generation = %d, want %d", rm2.CurrentGeneration(), expectedGen)
	}
}

func TestRekeyManager_ValidateREKEY_ReplayAttack(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Generate REKEY payload
	_, _, _, _, payload, err := rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY failed: %v", err)
	}

	// Create second manager (simulates receiving node)
	rm2, err := NewRekeyManager(t.TempDir())
	if err != nil {
		t.Fatalf("NewRekeyManager (receiver) failed: %v", err)
	}

	// Validate once (should succeed)
	_, _, _, _, err = rm2.ValidateREKEY(payload)
	if err != nil {
		t.Fatalf("first ValidateREKEY failed: %v", err)
	}

	// Attempt to validate same payload again (replay attack)
	_, _, _, _, err = rm2.ValidateREKEY(payload)
	if err == nil {
		t.Fatal("ValidateREKEY should reject replayed frame")
	}
	if err.Error() != "replay attack detected: generation 1 <= last seen 1" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRekeyManager_ValidateREKEY_InvalidSize(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Invalid payload size
	payload := make([]byte, 32) // Too short
	_, _, _, _, err = rm.ValidateREKEY(payload)
	if err == nil {
		t.Fatal("ValidateREKEY should reject invalid size")
	}
}

func TestRekeyManager_ValidateREKEY_PastValidAfter(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Craft payload with validAfter in the past
	payload := make([]byte, 48)
	// newKey (32 bytes) - all zeros for simplicity
	// keyID (4 bytes) - zeros
	// validAfter (4 bytes) - set to past timestamp
	pastTime := uint32(time.Now().Unix() - 3600) // 1 hour ago
	binary.BigEndian.PutUint32(payload[36:40], pastTime)
	// generation (8 bytes) - set to 1
	binary.BigEndian.PutUint64(payload[40:48], 1)

	_, _, _, _, err = rm.ValidateREKEY(payload)
	if err == nil {
		t.Fatal("ValidateREKEY should reject past validAfter")
	}
}

func TestRekeyManager_ValidateREKEY_FarFutureValidAfter(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Craft payload with validAfter too far in future (>30 days)
	payload := make([]byte, 48)
	// newKey (32 bytes) - all zeros
	// keyID (4 bytes) - zeros
	// validAfter (4 bytes) - set to 60 days in future
	futureTime := uint32(time.Now().Unix() + 5184000) // 60 days
	binary.BigEndian.PutUint32(payload[36:40], futureTime)
	// generation (8 bytes) - set to 1
	binary.BigEndian.PutUint64(payload[40:48], 1)

	_, _, _, _, err = rm.ValidateREKEY(payload)
	if err == nil {
		t.Fatal("ValidateREKEY should reject far-future validAfter")
	}
}

func TestRekeyManager_PersistenceFailure(t *testing.T) {
	// Create read-only directory to simulate persistence failure
	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0o700); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create manager and generate initial REKEY (should succeed)
	rm, err := NewRekeyManager(readOnlyDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	_, _, _, _, _, err = rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("first GenerateREKEY failed: %v", err)
	}

	// Make directory read-only
	if err := os.Chmod(readOnlyDir, 0o500); err != nil {
		t.Fatalf("chmod failed: %v", err)
	}
	defer os.Chmod(readOnlyDir, 0o700) // Cleanup

	// Attempt to generate REKEY (should fail due to read-only filesystem)
	_, _, _, _, _, err = rm.GenerateREKEY()
	if err == nil {
		t.Fatal("GenerateREKEY should fail on read-only filesystem")
	}

	// Verify generation counter was NOT incremented (rollback on failure)
	if rm.CurrentGeneration() != 1 {
		t.Errorf("generation after failed persist = %d, want 1 (rollback)", rm.CurrentGeneration())
	}
}

func TestComputeKeyID(t *testing.T) {
	// Test vector: compute KEY_ID for known key
	var testKey [32]byte
	for i := range testKey {
		testKey[i] = byte(i)
	}

	keyID := ComputeKeyID(testKey)

	// Verify KEY_ID is 4 bytes
	if len(keyID) != 4 {
		t.Errorf("keyID length = %d, want 4", len(keyID))
	}

	// Verify KEY_ID is non-zero
	zeroID := [4]byte{}
	if bytes.Equal(keyID[:], zeroID[:]) {
		t.Error("keyID is all zeros")
	}

	// Verify determinism (same key produces same KEY_ID)
	keyID2 := ComputeKeyID(testKey)
	if !bytes.Equal(keyID[:], keyID2[:]) {
		t.Error("ComputeKeyID is not deterministic")
	}

	// Verify different keys produce different KEY_IDs
	var testKey2 [32]byte
	for i := range testKey2 {
		testKey2[i] = byte(255 - i)
	}
	keyID3 := ComputeKeyID(testKey2)
	if bytes.Equal(keyID[:], keyID3[:]) {
		t.Error("different keys produced same KEY_ID (collision)")
	}
}

func TestRekeyManager_HighGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	rm, err := NewRekeyManager(tmpDir)
	if err != nil {
		t.Fatalf("NewRekeyManager failed: %v", err)
	}

	// Manually set high generation counter
	rm.mu.Lock()
	rm.generation = 1000000
	rm.mu.Unlock()
	if err := rm.persist(); err != nil {
		t.Fatalf("persist failed: %v", err)
	}

	// Generate REKEY with high generation
	_, _, _, gen, _, err := rm.GenerateREKEY()
	if err != nil {
		t.Fatalf("GenerateREKEY with high generation failed: %v", err)
	}

	if gen != 1000001 {
		t.Errorf("generation = %d, want 1000001", gen)
	}
}
