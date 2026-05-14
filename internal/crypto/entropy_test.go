package crypto

import (
	"testing"
)

func TestEntropyAudit_Success(t *testing.T) {
	// This test verifies the entropy audit passes on systems with proper CSPRNG
	if err := EntropyAudit(); err != nil {
		t.Fatalf("EntropyAudit failed: %v", err)
	}
}

func TestContinuousEntropyMonitor_Success(t *testing.T) {
	// Verify continuous monitoring detects no issues under normal operation
	if err := ContinuousEntropyMonitor(); err != nil {
		t.Fatalf("ContinuousEntropyMonitor failed: %v", err)
	}
}

// Note: Testing entropy FAILURE scenarios requires mocking crypto/rand,
// which is complex and beyond the scope of this initial implementation.
// Production security testing should use integration tests that inject
// controlled entropy failures to verify daemon abort behavior.
