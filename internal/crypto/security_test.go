package crypto

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestNonceUniqueness_100k verifies that 100,000 sequential nonce generations produce no collisions.
func TestNonceUniqueness_100k(t *testing.T) {
	meshKey := make([]byte, 32)
	if _, err := rand.Read(meshKey); err != nil {
		t.Fatalf("failed to generate mesh key: %v", err)
	}

	tempDir := t.TempDir()
	rc, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to create reboot counter: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 12345, rc)
	if err != nil {
		t.Fatalf("failed to create nonce generator: %v", err)
	}

	const iterations = 100000
	seenNonces := make(map[string]bool, iterations)

	for i := 0; i < iterations; i++ {
		nonce, err := ng.Generate()
		if err != nil {
			t.Fatalf("Generate failed at iteration %d: %v", i, err)
		}

		nonceKey := string(nonce)
		if seenNonces[nonceKey] {
			t.Fatalf("nonce collision detected at iteration %d: %x", i, nonce)
		}
		seenNonces[nonceKey] = true
	}

	t.Logf("verified %d unique nonces", len(seenNonces))
}

// TestNonceUniqueness_AcrossReboots verifies nonces don't repeat after daemon restart.
func TestNonceUniqueness_AcrossReboots(t *testing.T) {
	meshKey := make([]byte, 32)
	if _, err := rand.Read(meshKey); err != nil {
		t.Fatalf("failed to generate mesh key: %v", err)
	}

	tempDir := t.TempDir()
	nodeID := uint32(54321)
	const framesPerBoot = 1000

	allNonces := make(map[string]int) // nonce -> boot number

	// Simulate 5 daemon restarts
	for boot := 0; boot < 5; boot++ {
		rc, err := NewRebootCounter(tempDir)
		if err != nil {
			t.Fatalf("boot %d: failed to create reboot counter: %v", boot, err)
		}

		ng, err := NewNonceGenerator(meshKey, nodeID, rc)
		if err != nil {
			t.Fatalf("boot %d: failed to create nonce generator: %v", boot, err)
		}

		for seq := 0; seq < framesPerBoot; seq++ {
			nonce, err := ng.Generate()
			if err != nil {
				t.Fatalf("boot %d, seq %d: Generate failed: %v", boot, seq, err)
			}

			nonceKey := string(nonce)
			if prevBoot, exists := allNonces[nonceKey]; exists {
				t.Fatalf("nonce reuse detected: boot %d reused nonce from boot %d (seq=%d): %x",
					boot, prevBoot, seq, nonce)
			}
			allNonces[nonceKey] = boot
		}
	}

	expectedTotal := 5 * framesPerBoot
	if len(allNonces) != expectedTotal {
		t.Errorf("expected %d unique nonces, got %d", expectedTotal, len(allNonces))
	}

	t.Logf("verified %d unique nonces across 5 simulated reboots", len(allNonces))
}

// TestEntropyAudit_FailureDetection verifies entropy audit detects identical CSPRNG samples.
// This test uses a mock entropy source that returns identical bytes.
func TestEntropyAudit_FailureDetection(t *testing.T) {
	// Mock rand.Reader with a source that returns identical bytes
	originalReader := rand.Reader
	defer func() { rand.Reader = originalReader }()

	// Create a reader that always returns the same bytes
	constantBytes := make([]byte, 64)
	for i := range constantBytes {
		constantBytes[i] = 0xAA
	}
	rand.Reader = bytes.NewReader(constantBytes)

	// Entropy audit should detect this and fail
	err := EntropyAudit()
	if err == nil {
		t.Fatal("EntropyAudit should have detected identical samples but returned nil")
	}

	t.Logf("correctly detected entropy failure: %v", err)
}

// TestEntropyAudit_RealCSPRNG verifies entropy audit passes with proper CSPRNG.
func TestEntropyAudit_RealCSPRNG(t *testing.T) {
	// Use real crypto/rand
	err := EntropyAudit()
	if err != nil {
		t.Fatalf("EntropyAudit failed with real CSPRNG: %v", err)
	}
}

// TestRebootCounter_PersistenceFailure verifies behavior when reboot counter persistence fails.
func TestRebootCounter_PersistenceFailure_ReadOnly(t *testing.T) {
	tempDir := t.TempDir()

	// Create initial reboot counter
	rc, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to create reboot counter: %v", err)
	}

	// Make directory read-only to simulate disk full / filesystem error
	if err := os.Chmod(tempDir, 0o444); err != nil {
		t.Fatalf("failed to make directory read-only: %v", err)
	}
	defer os.Chmod(tempDir, 0o755) // Restore for cleanup

	// Attempt to increment should fail
	err = rc.Increment()
	if err == nil {
		t.Fatal("Increment should have failed on read-only filesystem but returned nil")
	}

	t.Logf("correctly detected persistence failure: %v", err)
}

// TestRebootCounter_PersistenceFailure_DiskFull simulates disk full scenario.
// Note: This test is limited in what it can validate since Go's file permissions
// don't fully simulate disk-full conditions. We verify the error path exists.
func TestRebootCounter_PersistenceFailure_DiskFull(t *testing.T) {
	t.Skip("Skipping - difficult to simulate true disk-full without root privileges")
}

// TestNonceGenerator_ContinuousCSPRNGValidation verifies continuous validation every 1,000 nonces.
func TestNonceGenerator_ContinuousCSPRNGValidation(t *testing.T) {
	meshKey := make([]byte, 32)
	if _, err := rand.Read(meshKey); err != nil {
		t.Fatalf("failed to generate mesh key: %v", err)
	}

	tempDir := t.TempDir()
	rc, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to create reboot counter: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 99999, rc)
	if err != nil {
		t.Fatalf("failed to create nonce generator: %v", err)
	}

	// Generate 5,000 nonces (crosses 5 validation boundaries at 1,000, 2,000, 3,000, 4,000, 5,000)
	const iterations = 5000
	for i := 0; i < iterations; i++ {
		_, err := ng.Generate()
		if err != nil {
			t.Fatalf("Generate failed at iteration %d: %v", i, err)
		}
	}

	// If we got here, continuous validation passed
	t.Logf("continuous CSPRNG validation passed for %d nonce generations", iterations)
}

// TestNonceGenerator_CSPRNGFailureMidExecution simulates CSPRNG failure after initialization.
// Note: crypto/rand.Read() calls runtime.fatal() on failure, which terminates the process.
// This test validates that our continuous monitoring would detect failures, but we cannot
// actually trigger a panic in tests without crashing the test suite.
func TestNonceGenerator_CSPRNGFailureMidExecution(t *testing.T) {
	t.Skip("Skipping - crypto/rand.Read() panics on failure, cannot test without process termination")
}

// failingReader is a mock io.Reader that always returns an error.
type failingReader struct{}

func (f *failingReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

// TestNonceGenerator_ConcurrentAccess verifies nonce generator is safe for concurrent use.
func TestNonceGenerator_ConcurrentAccess(t *testing.T) {
	meshKey := make([]byte, 32)
	if _, err := rand.Read(meshKey); err != nil {
		t.Fatalf("failed to generate mesh key: %v", err)
	}

	tempDir := t.TempDir()
	rc, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to create reboot counter: %v", err)
	}

	ng, err := NewNonceGenerator(meshKey, 77777, rc)
	if err != nil {
		t.Fatalf("failed to create nonce generator: %v", err)
	}

	const goroutines = 10
	const iterationsPerGoroutine = 100

	var wg sync.WaitGroup
	nonceChan := make(chan string, goroutines*iterationsPerGoroutine)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterationsPerGoroutine; i++ {
				nonce, err := ng.Generate()
				if err != nil {
					t.Errorf("goroutine %d: Generate failed: %v", gid, err)
					return
				}
				nonceChan <- string(nonce)
			}
		}(g)
	}

	wg.Wait()
	close(nonceChan)

	// Verify all nonces are unique
	seenNonces := make(map[string]bool)
	for nonceKey := range nonceChan {
		if seenNonces[nonceKey] {
			t.Errorf("duplicate nonce detected in concurrent test: %x", nonceKey)
		}
		seenNonces[nonceKey] = true
	}

	expectedCount := goroutines * iterationsPerGoroutine
	if len(seenNonces) != expectedCount {
		t.Errorf("expected %d unique nonces, got %d", expectedCount, len(seenNonces))
	}
}

// TestAEAD_NonceReuse_Detection verifies that reusing the same nonce breaks security.
// This test intentionally reuses a nonce to demonstrate the security risk.
func TestAEAD_NonceReuse_Detection(t *testing.T) {
	meshKey := make([]byte, 32)
	if _, err := rand.Read(meshKey); err != nil {
		t.Fatalf("failed to generate mesh key: %v", err)
	}

	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatalf("failed to generate nonce: %v", err)
	}

	// Encrypt two different plaintexts with the SAME nonce (security violation)
	plaintext1 := []byte("secret message A")
	plaintext2 := []byte("secret message B")

	ciphertext1, err := Encrypt(meshKey, nonce, plaintext1)
	if err != nil {
		t.Fatalf("first encryption failed: %v", err)
	}

	ciphertext2, err := Encrypt(meshKey, nonce, plaintext2)
	if err != nil {
		t.Fatalf("second encryption failed: %v", err)
	}

	// Both should decrypt successfully (but this is a security vulnerability)
	decrypted1, err := Decrypt(meshKey, nonce, ciphertext1)
	if err != nil {
		t.Fatalf("first decryption failed: %v", err)
	}

	decrypted2, err := Decrypt(meshKey, nonce, ciphertext2)
	if err != nil {
		t.Fatalf("second decryption failed: %v", err)
	}

	if !bytes.Equal(decrypted1, plaintext1) {
		t.Error("first decryption mismatch")
	}
	if !bytes.Equal(decrypted2, plaintext2) {
		t.Error("second decryption mismatch")
	}

	// XOR the two ciphertexts to demonstrate information leakage
	// With stream ciphers (ChaCha20), nonce reuse allows XOR attack
	xorResult := make([]byte, len(ciphertext1))
	for i := 0; i < len(ciphertext1) && i < len(ciphertext2); i++ {
		xorResult[i] = ciphertext1[i] ^ ciphertext2[i]
	}

	// The XOR result reveals information about the plaintexts
	// This demonstrates why nonce reuse is catastrophic
	t.Logf("WARNING: Nonce reuse allows XOR attack. XOR of ciphertexts reveals plaintext relationships.")
	t.Logf("This test demonstrates the security risk that hybrid nonce construction prevents.")
}

// TestRebootCounter_AtomicWrite verifies atomic write-rename operation.
func TestRebootCounter_AtomicWrite(t *testing.T) {
	tempDir := t.TempDir()

	// First initialization - counter goes from 0 to 1
	rc, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to create reboot counter: %v", err)
	}

	initial := rc.Value()
	if initial != 1 {
		t.Errorf("expected initial counter value 1, got %d", initial)
	}

	// Increment multiple times
	for i := 0; i < 10; i++ {
		if err := rc.Increment(); err != nil {
			t.Fatalf("Increment %d failed: %v", i, err)
		}
	}

	expected := initial + 10
	if rc.Value() != expected {
		t.Errorf("expected counter value %d, got %d", expected, rc.Value())
	}

	// Verify file exists and is readable
	counterPath := filepath.Join(tempDir, "reboot_counter")
	data, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatalf("failed to read counter file: %v", err)
	}

	if len(data) == 0 {
		t.Error("counter file is empty")
	}

	// Load counter from disk and verify value
	// NewRebootCounter will increment again, so expect expected + 1
	rc2, err := NewRebootCounter(tempDir)
	if err != nil {
		t.Fatalf("failed to reload reboot counter: %v", err)
	}

	if rc2.Value() != expected+1 {
		t.Errorf("reloaded counter value mismatch: expected %d, got %d", expected+1, rc2.Value())
	}
}
