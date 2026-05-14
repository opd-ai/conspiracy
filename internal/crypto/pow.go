// Package crypto provides Proof-of-Work (PoW) challenge generation and validation for anti-flood protection.
package crypto

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"
)

const (
	// PoWDifficulty is the number of leading zero bits required (16 bits = ~65k hashes average)
	PoWDifficulty = 16

	// PoWTimestampTolerance is the maximum age/skew for PoW timestamp validation (±5 minutes)
	PoWTimestampTolerance = 300 // seconds
)

// PoWChallenge represents a Proof-of-Work challenge for JOIN_REQ anti-flood.
type PoWChallenge struct {
	NodeID    uint32
	Timestamp uint32 // Unix timestamp (seconds since epoch)
	Nonce     uint64 // PoW nonce to be found
}

// ComputePoW computes a valid PoW nonce for the given NodeID and timestamp.
// Returns a nonce such that SHA256(NodeID || Timestamp || Nonce) has PoWDifficulty leading zero bits.
// This is computationally expensive by design (anti-flood mechanism).
func ComputePoW(nodeID, timestamp uint32) (uint64, error) {
	// Allocate buffer for hash input: NodeID(4) + Timestamp(4) + Nonce(8) = 16 bytes
	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:4], nodeID)
	binary.BigEndian.PutUint32(buf[4:8], timestamp)

	// Try nonces until we find one that satisfies difficulty
	for nonce := uint64(0); nonce < (1 << 32); nonce++ {
		binary.BigEndian.PutUint64(buf[8:16], nonce)

		hash := sha256.Sum256(buf)

		if hasLeadingZeros(hash[:], PoWDifficulty) {
			return nonce, nil
		}
	}

	return 0, fmt.Errorf("PoW failed after 2^32 attempts (difficulty too high)")
}

// ValidatePoW verifies that a PoW nonce is valid for the given NodeID and timestamp.
// Returns error if:
// - Hash doesn't meet difficulty requirement
// - Timestamp is outside tolerance window (prevents precomputation attacks)
func ValidatePoW(nodeID, timestamp uint32, nonce uint64) error {
	// Check timestamp freshness (prevent precomputation)
	now := uint32(time.Now().Unix())
	diff := int32(now) - int32(timestamp)
	if diff < -PoWTimestampTolerance || diff > PoWTimestampTolerance {
		return fmt.Errorf("PoW timestamp outside tolerance window: %d seconds (max ±%d)", diff, PoWTimestampTolerance)
	}

	// Verify hash
	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:4], nodeID)
	binary.BigEndian.PutUint32(buf[4:8], timestamp)
	binary.BigEndian.PutUint64(buf[8:16], nonce)

	hash := sha256.Sum256(buf)

	if !hasLeadingZeros(hash[:], PoWDifficulty) {
		return fmt.Errorf("PoW validation failed: insufficient leading zeros (required %d bits)", PoWDifficulty)
	}

	return nil
}

// hasLeadingZeros checks if the hash has at least n leading zero bits.
func hasLeadingZeros(hash []byte, n int) bool {
	// Check full bytes
	fullBytes := n / 8
	for i := 0; i < fullBytes; i++ {
		if hash[i] != 0 {
			return false
		}
	}

	// Check remaining bits
	remainingBits := n % 8
	if remainingBits > 0 {
		mask := byte(0xFF << (8 - remainingBits))
		if hash[fullBytes]&mask != 0 {
			return false
		}
	}

	return true
}

// EstimatePoWTime estimates the time required to compute PoW for the current difficulty.
// This is approximate and hardware-dependent (assumes ~1M hashes/sec on embedded ARM).
func EstimatePoWTime() time.Duration {
	// Average attempts for n-bit difficulty: 2^(n-1)
	// For 16 bits: 2^15 = 32,768 attempts
	avgAttempts := 1 << (PoWDifficulty - 1)

	// Assume 1M hashes/second (conservative for ARM Cortex-A53)
	hashesPerSec := 1_000_000
	seconds := avgAttempts / hashesPerSec

	return time.Duration(seconds) * time.Second
}
