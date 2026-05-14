package crypto

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"time"
)

// EntropyAudit performs startup validation of the system entropy source.
// This MUST be called before any cryptographic operations to prevent
// catastrophic nonce reuse from a failed CSPRNG.
//
// The audit performs two checks:
// 1. Blocking read from /dev/random to ensure kernel entropy pool is initialized
// 2. Generate two samples and verify they differ (detect constant CSPRNG output)
//
// On embedded devices without hardware RNG, the blocking read may delay
// startup by 10-30 seconds. This is acceptable for security.
func EntropyAudit() error {
	// Step 1: Block until kernel entropy pool is initialized
	// This ensures crypto/rand has sufficient entropy before first use
	f, err := os.Open("/dev/random")
	if err != nil {
		return fmt.Errorf("failed to open /dev/random: %w", err)
	}
	defer f.Close()

	// Read 32 bytes from /dev/random (may block on first boot)
	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read from /dev/random: %w", err)
	}
	if n != 32 {
		return fmt.Errorf("short read from /dev/random: got %d bytes, want 32", n)
	}

	// Step 2: Validate crypto/rand produces varying output
	sample1 := make([]byte, 32)
	sample2 := make([]byte, 32)

	if _, err := rand.Read(sample1); err != nil {
		return fmt.Errorf("crypto/rand first sample failed: %w", err)
	}

	// Small delay to ensure different output even if CSPRNG is weakly seeded
	time.Sleep(1 * time.Millisecond)

	if _, err := rand.Read(sample2); err != nil {
		return fmt.Errorf("crypto/rand second sample failed: %w", err)
	}

	// CRITICAL: Verify samples differ
	if bytes.Equal(sample1, sample2) {
		return fmt.Errorf("CRITICAL: crypto/rand entropy failure detected - identical samples generated; aborting to prevent nonce reuse")
	}

	return nil
}

// ContinuousEntropyMonitor validates CSPRNG output periodically.
// Call this every 1,000 nonce generations to detect runtime CSPRNG failures.
//
// Returns error if identical samples are generated, indicating entropy source failure.
func ContinuousEntropyMonitor() error {
	sample1 := make([]byte, 32)
	sample2 := make([]byte, 32)

	if _, err := rand.Read(sample1); err != nil {
		return fmt.Errorf("entropy monitor sample1 failed: %w", err)
	}

	if _, err := rand.Read(sample2); err != nil {
		return fmt.Errorf("entropy monitor sample2 failed: %w", err)
	}

	if bytes.Equal(sample1, sample2) {
		return fmt.Errorf("CRITICAL: Runtime entropy failure detected - identical samples; CSPRNG may be compromised")
	}

	return nil
}
