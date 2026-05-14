package crypto

import (
	"bytes"
	"testing"
)

func mustCreateRebootCounter(t *testing.T) *RebootCounter {
	t.Helper()
	rc, err := NewRebootCounter(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}
	return rc
}

func TestAEAD_RoundTrip(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	plaintext := []byte("This is a test BEACON payload with sensitive mesh topology data")

	// Generate nonce
	ng, err := NewNonceGenerator(meshKey, 12345, mustCreateRebootCounter(t))
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	nonce, err := ng.Generate()
	if err != nil {
		t.Fatalf("Failed to generate nonce: %v", err)
	}

	// Convert to [12]byte
	var nonceArray [12]byte
	copy(nonceArray[:], nonce)

	// Encrypt
	ciphertext, err := Encrypt(meshKey, nonceArray, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Verify ciphertext is longer (includes 16-byte MAC)
	if len(ciphertext) != len(plaintext)+16 {
		t.Errorf("Ciphertext length = %d, want %d", len(ciphertext), len(plaintext)+16)
	}

	// Decrypt
	decrypted, err := Decrypt(meshKey, nonceArray, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	// Verify plaintext matches
	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypted plaintext mismatch\nGot:  %q\nWant: %q", decrypted, plaintext)
	}
}

func TestAEAD_TamperedCiphertext(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	plaintext := []byte("Original message")

	ng, err := NewNonceGenerator(meshKey, 12345, mustCreateRebootCounter(t))
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	nonce, err := ng.Generate()
	if err != nil {
		t.Fatalf("Failed to generate nonce: %v", err)
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonce)

	ciphertext, err := Encrypt(meshKey, nonceArray, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Tamper with ciphertext (flip a bit)
	ciphertext[5] ^= 0x01

	// Decrypt should fail
	_, err = Decrypt(meshKey, nonceArray, ciphertext)
	if err == nil {
		t.Fatal("Decrypt should fail with tampered ciphertext")
	}

	expectedMsg := "MAC verification failed"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestAEAD_WrongKey(t *testing.T) {
	meshKey1 := make([]byte, 32)
	meshKey2 := make([]byte, 32)
	for i := range meshKey1 {
		meshKey1[i] = byte(i)
		meshKey2[i] = byte(i + 1) // Different key
	}

	plaintext := []byte("Secret message")

	ng, err := NewNonceGenerator(meshKey1, 12345, mustCreateRebootCounter(t))
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	nonce, err := ng.Generate()
	if err != nil {
		t.Fatalf("Failed to generate nonce: %v", err)
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonce)

	// Encrypt with key1
	ciphertext, err := Encrypt(meshKey1, nonceArray, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Decrypt with key2 should fail
	_, err = Decrypt(meshKey2, nonceArray, ciphertext)
	if err == nil {
		t.Fatal("Decrypt should fail with wrong key")
	}

	expectedMsg := "MAC verification failed"
	if !contains(err.Error(), expectedMsg) {
		t.Errorf("Error = %v, want substring %q", err, expectedMsg)
	}
}

func TestAEAD_InvalidKeyLength(t *testing.T) {
	shortKey := make([]byte, 16) // Invalid, must be 32
	plaintext := []byte("test")

	var nonce [12]byte

	_, err := Encrypt(shortKey, nonce, plaintext)
	if err == nil {
		t.Fatal("Encrypt should fail with invalid key length")
	}

	expectedMsg := "invalid mesh key length: 16 bytes (must be 32)"
	if err.Error() != expectedMsg {
		t.Errorf("Error = %v, want %q", err, expectedMsg)
	}
}

func TestAEAD_NonceUniqueness(t *testing.T) {
	meshKey := make([]byte, 32)
	for i := range meshKey {
		meshKey[i] = byte(i)
	}

	ng, err := NewNonceGenerator(meshKey, 12345, mustCreateRebootCounter(t))
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	// Generate 10,000 nonces and verify no collisions
	seen := make(map[[12]byte]bool)
	const iterations = 10000

	for i := 0; i < iterations; i++ {
		nonceSlice, err := ng.Generate()
		if err != nil {
			t.Fatalf("Generate failed at iteration %d: %v", i, err)
		}

		var nonce [12]byte
		copy(nonce[:], nonceSlice)

		if seen[nonce] {
			t.Fatalf("Nonce collision detected at iteration %d: %x", i, nonce)
		}
		seen[nonce] = true

		// Every 1000 iterations, verify entropy monitor still passes
		if i > 0 && i%1000 == 0 {
			if err := ContinuousEntropyMonitor(); err != nil {
				t.Fatalf("Entropy monitor failed at iteration %d: %v", i, err)
			}
		}
	}

	t.Logf("Successfully generated %d unique nonces with zero collisions", iterations)
}

func TestAEAD_EmptyPlaintext(t *testing.T) {
	meshKey := make([]byte, 32)
	var nonce [12]byte
	plaintext := []byte{}

	ciphertext, err := Encrypt(meshKey, nonce, plaintext)
	if err != nil {
		t.Fatalf("Encrypt failed with empty plaintext: %v", err)
	}

	// Empty plaintext should still produce 16-byte MAC
	if len(ciphertext) != 16 {
		t.Errorf("Ciphertext length = %d, want 16 (MAC only)", len(ciphertext))
	}

	decrypted, err := Decrypt(meshKey, nonce, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if len(decrypted) != 0 {
		t.Errorf("Decrypted length = %d, want 0", len(decrypted))
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
