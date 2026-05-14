package crypto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
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
	rebootCounter *RebootCounter // Reference to reboot counter for automatic wrap recovery
	frameSeq      uint32         // atomic counter (16-bit, wraps)
	mu            sync.Mutex
	validationCtr uint64 // Incremented on each nonce generation for monitoring
}

// NewNonceGenerator creates a nonce generator.
// meshKey: 32-byte mesh encryption key
// nodeID: unique node identifier
// rc: reboot counter instance for automatic frame sequence wrap recovery
func NewNonceGenerator(meshKey []byte, nodeID uint32, rc *RebootCounter) (*NonceGenerator, error) {
	if len(meshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(meshKey))
	}
	if rc == nil {
		return nil, fmt.Errorf("reboot counter cannot be nil")
	}

	return &NonceGenerator{
		meshKey:       meshKey,
		nodeID:        nodeID,
		rebootCounter: rc,
		frameSeq:      0,
	}, nil
}

// Generate creates a unique 12-byte nonce for AEAD encryption.
//
// Returns error if:
//   - crypto/rand fails (entropy exhaustion)
//   - Frame sequence wraps AND reboot counter increment fails (disk full, read-only FS)
//
// Automatic recovery: When frame sequence exceeds 65,536, the reboot counter is
// automatically incremented and frame sequence reset to 0. This allows continuous
// operation without requiring daemon restart.
func (ng *NonceGenerator) Generate() ([]byte, error) {
	seq := atomic.AddUint32(&ng.frameSeq, 1)

	var err error
	if seq > 0xFFFF {
		seq, err = ng.handleFrameSeqWrap()
		if err != nil {
			return nil, err
		}
	}

	randomBytes, err := ng.generateRandomEntropy()
	if err != nil {
		return nil, err
	}

	nonce := ng.computeNonce(seq, randomBytes)

	if err := ng.performPeriodicEntropyCheck(); err != nil {
		return nil, err
	}

	return nonce, nil
}

// handleFrameSeqWrap manages frame sequence wraparound with reboot counter increment.
func (ng *NonceGenerator) handleFrameSeqWrap() (uint32, error) {
	ng.mu.Lock()
	defer ng.mu.Unlock()

	currentSeq := atomic.LoadUint32(&ng.frameSeq)
	if currentSeq > 0xFFFF {
		if err := ng.rebootCounter.Increment(); err != nil {
			return 0, fmt.Errorf("CRITICAL: Frame sequence wrapped but reboot counter increment failed; LoRa subsystem disabled to prevent nonce reuse: %w", err)
		}

		atomic.StoreUint32(&ng.frameSeq, 1)
		slog.Info("Frame sequence wrapped after 65,536 frames; reboot counter incremented to prevent nonce reuse", "reboot_counter", ng.rebootCounter.Value())
		return 1, nil
	}

	return currentSeq, nil
}

// generateRandomEntropy creates 8 bytes of random data.
func (ng *NonceGenerator) generateRandomEntropy() ([]byte, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return nil, fmt.Errorf("crypto/rand failed: %w", err)
	}
	return randomBytes, nil
}

// computeNonce creates the final nonce via HMAC-SHA256.
func (ng *NonceGenerator) computeNonce(seq uint32, randomBytes []byte) []byte {
	input := make([]byte, 18)
	binary.BigEndian.PutUint32(input[0:4], ng.nodeID)
	binary.BigEndian.PutUint32(input[4:8], ng.rebootCounter.Value())
	binary.BigEndian.PutUint16(input[8:10], uint16(seq))
	copy(input[10:18], randomBytes)

	mac := hmac.New(sha256.New, ng.meshKey)
	mac.Write(input)
	fullMAC := mac.Sum(nil)

	return fullMAC[:12]
}

// performPeriodicEntropyCheck runs continuous monitoring every 1,000 nonces.
func (ng *NonceGenerator) performPeriodicEntropyCheck() error {
	atomic.AddUint64(&ng.validationCtr, 1)

	if atomic.LoadUint64(&ng.validationCtr)%1000 == 0 {
		if err := ContinuousEntropyMonitor(); err != nil {
			return fmt.Errorf("continuous entropy monitor failed: %w", err)
		}
	}
	return nil
}

// FrameSeq returns the current frame sequence number.
func (ng *NonceGenerator) FrameSeq() uint32 {
	return atomic.LoadUint32(&ng.frameSeq)
}

// ValidationCount returns the total number of nonces generated.
func (ng *NonceGenerator) ValidationCount() uint64 {
	return atomic.LoadUint64(&ng.validationCtr)
}
