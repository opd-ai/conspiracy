package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
)

// NonceGenerator generates unique 96-bit nonces for ChaCha20-Poly1305 AEAD.
//
// Hybrid construction (defense-in-depth):
//
//	nonce = HMAC-SHA256(MESH_KEY, NodeID || reboot_counter || frame_seq || crypto/rand(8_bytes))[:12]
//
// Components:
//   - NodeID: 32-bit node identifier
//   - reboot_counter: 32-bit persistent counter incremented on every daemon boot
//   - frame_seq: 16-bit frame sequence number (wraps at 65536)
//   - crypto/rand(8_bytes): 64-bit random entropy per frame
//
// Security properties:
//   - Reboot counter prevents nonce reuse across daemon restarts
//   - Frame sequence provides per-frame uniqueness within boot cycle
//   - Random entropy provides additional collision resistance
//   - HMAC ensures nonces are unpredictable to attackers
//
// CRITICAL: Single nonce reuse breaks all AEAD confidentiality.
type NonceGenerator struct {
	meshKey       []byte
	nodeID        uint32
	rebootCounter uint32
	frameSeq      uint32 // atomic counter (16-bit, wraps)
	mu            sync.Mutex
	validationCtr uint64 // Incremented on each nonce generation for monitoring
}

// NewNonceGenerator creates a nonce generator.
// meshKey: 32-byte mesh encryption key
// nodeID: unique node identifier
// rebootCounter: current reboot counter value
func NewNonceGenerator(meshKey []byte, nodeID, rebootCounter uint32) (*NonceGenerator, error) {
	if len(meshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(meshKey))
	}

	return &NonceGenerator{
		meshKey:       meshKey,
		nodeID:        nodeID,
		rebootCounter: rebootCounter,
		frameSeq:      0,
	}, nil
}

// Generate creates a unique 12-byte nonce for AEAD encryption.
//
// Returns error if:
//   - crypto/rand fails (entropy exhaustion)
//   - Frame sequence wraps (65,536 frames per boot cycle)
func (ng *NonceGenerator) Generate() ([]byte, error) {
	// Increment frame sequence atomically
	seq := atomic.AddUint32(&ng.frameSeq, 1)

	// Check for wrap (unlikely in practice, but fatal if it occurs)
	if seq > 0xFFFF {
		return nil, fmt.Errorf("CRITICAL: Frame sequence exhausted (>65,536 frames in this boot cycle); daemon restart required to prevent nonce reuse")
	}

	// Generate 8 bytes of random entropy
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("crypto/rand failed: %w", err)
	}

	// Construct input for HMAC:
	// NodeID(4) || RebootCounter(4) || FrameSeq(2) || Random(8)
	input := make([]byte, 18)
	binary.BigEndian.PutUint32(input[0:4], ng.nodeID)
	binary.BigEndian.PutUint32(input[4:8], ng.rebootCounter)
	binary.BigEndian.PutUint16(input[8:10], uint16(seq))
	copy(input[10:18], randomBytes)

	// Compute HMAC-SHA256
	mac := hmac.New(sha256.New, ng.meshKey)
	mac.Write(input)
	fullMAC := mac.Sum(nil) // 32 bytes

	// Truncate to 12 bytes for ChaCha20-Poly1305 nonce
	nonce := fullMAC[:12]

	// Increment validation counter for monitoring
	atomic.AddUint64(&ng.validationCtr, 1)

	// Trigger continuous entropy monitor every 1,000 nonces
	if atomic.LoadUint64(&ng.validationCtr)%1000 == 0 {
		if err := ContinuousEntropyMonitor(); err != nil {
			return nil, fmt.Errorf("continuous entropy monitor failed: %w", err)
		}
	}

	return nonce, nil
}

// FrameSeq returns the current frame sequence number.
func (ng *NonceGenerator) FrameSeq() uint32 {
	return atomic.LoadUint32(&ng.frameSeq)
}

// ValidationCount returns the total number of nonces generated.
func (ng *NonceGenerator) ValidationCount() uint64 {
	return atomic.LoadUint64(&ng.validationCtr)
}
