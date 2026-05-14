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
	if err := waitForKernelEntropyPool(); err != nil {
		return err
	}
	return validateCSPRNGOutput()
}

// waitForKernelEntropyPool blocks until kernel entropy is initialized.
func waitForKernelEntropyPool() error {
	f, err := os.Open("/dev/random")
	if err != nil {
		return fmt.Errorf("failed to open /dev/random: %w", err)
	}
	defer f.Close()

	buf := make([]byte, 32)
	n, err := f.Read(buf)
	if err != nil {
		return fmt.Errorf("failed to read from /dev/random: %w", err)
	}
	if n != 32 {
		return fmt.Errorf("short read from /dev/random: got %d bytes, want 32", n)
	}

	return nil
}

// validateCSPRNGOutput ensures crypto/rand produces varying output.
func validateCSPRNGOutput() error {
	sample1, err := generateEntropySample()
	if err != nil {
		return fmt.Errorf("crypto/rand first sample failed: %w", err)
	}

	time.Sleep(1 * time.Millisecond)

	sample2, err := generateEntropySample()
	if err != nil {
		return fmt.Errorf("crypto/rand second sample failed: %w", err)
	}

	if bytes.Equal(sample1, sample2) {
		return fmt.Errorf("CRITICAL: crypto/rand entropy failure detected - identical samples generated; aborting to prevent nonce reuse")
	}

	return nil
}

// generateEntropySample generates a 32-byte random sample.
func generateEntropySample() ([]byte, error) {
	sample := make([]byte, 32)
	if _, err := rand.Read(sample); err != nil {
		return nil, err
	}
	return sample, nil
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
