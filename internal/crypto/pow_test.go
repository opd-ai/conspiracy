package crypto

import (
	"testing"
	"time"
)

func TestComputePoW_ValidNonce(t *testing.T) {
	nodeID := uint32(12345)
	timestamp := uint32(time.Now().Unix())

	nonce, err := ComputePoW(nodeID, timestamp)
	if err != nil {
		t.Fatalf("ComputePoW failed: %v", err)
	}

	// Validate the computed nonce
	if err := ValidatePoW(nodeID, timestamp, nonce); err != nil {
		t.Errorf("Computed PoW nonce failed validation: %v", err)
	}
}

func TestValidatePoW_InvalidNonce(t *testing.T) {
	nodeID := uint32(12345)
	timestamp := uint32(time.Now().Unix())
	invalidNonce := uint64(0) // Highly unlikely to be valid

	err := ValidatePoW(nodeID, timestamp, invalidNonce)
	if err == nil {
		t.Error("ValidatePoW should reject invalid nonce")
	}
}

func TestValidatePoW_TimestampFreshness(t *testing.T) {
	nodeID := uint32(12345)
	now := uint32(time.Now().Unix())

	tests := []struct {
		name      string
		timestamp uint32
		shouldErr bool
	}{
		{
			name:      "current time",
			timestamp: now,
			shouldErr: false,
		},
		{
			name:      "4 minutes ago",
			timestamp: now - 240,
			shouldErr: false,
		},
		{
			name:      "4 minutes future",
			timestamp: now + 240,
			shouldErr: false,
		},
		{
			name:      "6 minutes ago",
			timestamp: now - 361,
			shouldErr: true,
		},
		{
			name:      "6 minutes future",
			timestamp: now + 361,
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute valid nonce for the timestamp
			nonce, err := ComputePoW(nodeID, tt.timestamp)
			if err != nil {
				t.Fatalf("ComputePoW failed: %v", err)
			}

			// Validate with current time check
			err = ValidatePoW(nodeID, tt.timestamp, nonce)
			if tt.shouldErr && err == nil {
				t.Error("Expected timestamp validation to fail")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Unexpected validation error: %v", err)
			}
		})
	}
}

func TestValidatePoW_DifferentNodeID(t *testing.T) {
	nodeID1 := uint32(12345)
	nodeID2 := uint32(54321)
	timestamp := uint32(time.Now().Unix())

	// Compute PoW for nodeID1
	nonce, err := ComputePoW(nodeID1, timestamp)
	if err != nil {
		t.Fatalf("ComputePoW failed: %v", err)
	}

	// Try to validate with nodeID2 (should fail - nonce is bound to NodeID)
	err = ValidatePoW(nodeID2, timestamp, nonce)
	if err == nil {
		t.Error("ValidatePoW should reject nonce from different NodeID")
	}
}

func TestValidatePoW_DifferentTimestamp(t *testing.T) {
	nodeID := uint32(12345)
	timestamp1 := uint32(time.Now().Unix())
	timestamp2 := timestamp1 + 60 // 1 minute later

	// Compute PoW for timestamp1
	nonce, err := ComputePoW(nodeID, timestamp1)
	if err != nil {
		t.Fatalf("ComputePoW failed: %v", err)
	}

	// Try to validate with timestamp2 (should fail - nonce is bound to timestamp)
	err = ValidatePoW(nodeID, timestamp2, nonce)
	if err == nil {
		t.Error("ValidatePoW should reject nonce from different timestamp")
	}
}

func TestHasLeadingZeros(t *testing.T) {
	tests := []struct {
		name     string
		hash     []byte
		bits     int
		expected bool
	}{
		{
			name:     "8 zero bits",
			hash:     []byte{0x00, 0xFF, 0xFF, 0xFF},
			bits:     8,
			expected: true,
		},
		{
			name:     "16 zero bits",
			hash:     []byte{0x00, 0x00, 0xFF, 0xFF},
			bits:     16,
			expected: true,
		},
		{
			name:     "4 zero bits",
			hash:     []byte{0x0F, 0xFF, 0xFF, 0xFF},
			bits:     4,
			expected: true,
		},
		{
			name:     "insufficient zeros",
			hash:     []byte{0x00, 0x01, 0xFF, 0xFF},
			bits:     16,
			expected: false,
		},
		{
			name:     "no zeros",
			hash:     []byte{0xFF, 0xFF, 0xFF, 0xFF},
			bits:     1,
			expected: false,
		},
		{
			name:     "partial byte zeros",
			hash:     []byte{0x00, 0x00, 0x3F, 0xFF}, // 10 zero bits (8 + 2)
			bits:     10,
			expected: true,
		},
		{
			name:     "partial byte insufficient",
			hash:     []byte{0x00, 0x00, 0x3F, 0xFF}, // 18 zero bits (16 + 2), not enough for 19
			bits:     19,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasLeadingZeros(tt.hash, tt.bits)
			if result != tt.expected {
				t.Errorf("hasLeadingZeros(%v, %d) = %v, expected %v",
					tt.hash[:4], tt.bits, result, tt.expected)
			}
		})
	}
}

func TestEstimatePoWTime(t *testing.T) {
	duration := EstimatePoWTime()

	// For 16-bit difficulty, should be approximately 32ms (2^15 / 1M hashes/sec)
	// Allow wide tolerance since it's an estimate
	if duration < 0 || duration > 1*time.Second {
		t.Errorf("EstimatePoWTime() = %v, expected ~32ms (max 1s)", duration)
	}

	t.Logf("Estimated PoW time: %v", duration)
}

func TestPoW_AntiPrecomputation(t *testing.T) {
	nodeID := uint32(12345)
	oldTimestamp := uint32(time.Now().Unix() - 400) // 6:40 minutes ago

	// Compute PoW for old timestamp
	nonce, err := ComputePoW(nodeID, oldTimestamp)
	if err != nil {
		t.Fatalf("ComputePoW failed: %v", err)
	}

	// Should fail validation due to timestamp being too old
	err = ValidatePoW(nodeID, oldTimestamp, nonce)
	if err == nil {
		t.Error("ValidatePoW should reject old timestamp (anti-precomputation)")
	}
}

func TestPoW_MultipleNodes(t *testing.T) {
	timestamp := uint32(time.Now().Unix())

	// Different nodes have different PoW solutions
	node1 := uint32(11111)
	node2 := uint32(22222)

	nonce1, err := ComputePoW(node1, timestamp)
	if err != nil {
		t.Fatalf("ComputePoW for node1 failed: %v", err)
	}

	nonce2, err := ComputePoW(node2, timestamp)
	if err != nil {
		t.Fatalf("ComputePoW for node2 failed: %v", err)
	}

	// Nonces should be different (extremely high probability)
	if nonce1 == nonce2 {
		t.Error("Different nodes should have different PoW nonces")
	}

	// Each nonce only validates for its own node
	if err := ValidatePoW(node1, timestamp, nonce1); err != nil {
		t.Errorf("Node1 nonce validation failed: %v", err)
	}

	if err := ValidatePoW(node2, timestamp, nonce2); err != nil {
		t.Errorf("Node2 nonce validation failed: %v", err)
	}

	// Cross-validation should fail
	if ValidatePoW(node1, timestamp, nonce2) == nil {
		t.Error("Node2 nonce should not validate for node1")
	}
}

// BenchmarkComputePoW measures PoW computation time (should be ~32ms on modern hardware)
func BenchmarkComputePoW(b *testing.B) {
	nodeID := uint32(12345)
	timestamp := uint32(time.Now().Unix())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := ComputePoW(nodeID, timestamp)
		if err != nil {
			b.Fatalf("ComputePoW failed: %v", err)
		}
	}
}

// BenchmarkValidatePoW measures validation time (should be <1ms)
func BenchmarkValidatePoW(b *testing.B) {
	nodeID := uint32(12345)
	timestamp := uint32(time.Now().Unix())

	nonce, err := ComputePoW(nodeID, timestamp)
	if err != nil {
		b.Fatalf("ComputePoW failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := ValidatePoW(nodeID, timestamp, nonce)
		if err != nil {
			b.Fatalf("ValidatePoW failed: %v", err)
		}
	}
}
