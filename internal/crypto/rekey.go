package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RekeyManager handles key rotation with monotonic generation counter to prevent replay attacks.
// Design §3.6: REKEY frames are encrypted with OLD_KEY and contain NEW_KEY + generation counter.
// The generation counter is persisted to prevent accepting old REKEY frames after daemon restart.
type RekeyManager struct {
	storageDir string
	generation uint64
	mu         sync.RWMutex
}

// NewRekeyManager creates a new key rotation manager.
// storageDir: directory for persistent storage (e.g., "/var/lib/conspiracyd")
func NewRekeyManager(storageDir string) (*RekeyManager, error) {
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	rm := &RekeyManager{
		storageDir: storageDir,
	}

	// Load existing generation or initialize to 0
	if err := rm.load(); err != nil {
		return nil, err
	}

	return rm, nil
}

// load reads the generation counter from persistent storage.
func (rm *RekeyManager) load() error {
	path := filepath.Join(rm.storageDir, "rekey_generation")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run, initialize to 0
		rm.generation = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read generation counter: %w", err)
	}

	if len(data) != 8 {
		return fmt.Errorf("invalid generation file size: %d bytes (expected 8)", len(data))
	}

	rm.generation = binary.LittleEndian.Uint64(data)
	return nil
}

// persist saves the generation counter to persistent storage using atomic write-rename.
func (rm *RekeyManager) persist() error {
	path := filepath.Join(rm.storageDir, "rekey_generation")
	tmpPath := path + ".tmp"

	data := make([]byte, 8)
	binary.LittleEndian.PutUint64(data, rm.generation)

	// Write to temporary file
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // Cleanup on failure
		return fmt.Errorf("failed to rename generation file: %w", err)
	}

	return nil
}

// GenerateREKEY creates a new REKEY frame payload with a fresh key and incremented generation.
// Returns: newKey (32 bytes), newKeyID (4 bytes), validAfter (Unix timestamp), generation counter, marshaled payload, error.
//
// The returned payload is ready for encryption with the OLD mesh key.
// Design note: validAfter is set to current_time + 24 hours to allow transition period.
func (rm *RekeyManager) GenerateREKEY() (newKey [32]byte, newKeyID [4]byte, validAfter uint32, generation uint64, payload []byte, err error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Generate new 256-bit mesh key
	if _, err := rand.Read(newKey[:]); err != nil {
		return newKey, newKeyID, 0, 0, nil, fmt.Errorf("failed to generate new key: %w", err)
	}

	// Compute KEY_ID = HMAC-SHA256(NEW_KEY, "key-id")[0:4]
	mac := sha256.New()
	mac.Write(newKey[:])
	mac.Write([]byte("key-id"))
	keyIDHash := mac.Sum(nil)
	copy(newKeyID[:], keyIDHash[:4])

	// Set validAfter to 24 hours from now (transition period)
	validAfter = uint32(time.Now().Unix() + 86400) // 24 hours = 86400 seconds

	// Increment generation counter
	rm.generation++
	generation = rm.generation

	// Persist generation counter (crash-safe)
	if err := rm.persist(); err != nil {
		// CRITICAL: If we can't persist generation, rollback increment
		rm.generation--
		return newKey, newKeyID, 0, 0, nil, fmt.Errorf("CRITICAL: Failed to persist generation counter: %w", err)
	}

	// Marshal payload: NEW_KEY (32) + NEW_KEY_ID (4) + VALID_AFTER (4) + GENERATION (8) = 48 bytes
	payload = make([]byte, 48)
	copy(payload[0:32], newKey[:])
	binary.BigEndian.PutUint32(payload[32:36], binary.BigEndian.Uint32(newKeyID[:]))
	binary.BigEndian.PutUint32(payload[36:40], validAfter)
	binary.BigEndian.PutUint64(payload[40:48], generation)

	return newKey, newKeyID, validAfter, generation, payload, nil
}

// ValidateREKEY validates a REKEY frame payload and updates the generation counter if valid.
// Returns: newKey, newKeyID, validAfter, generation, error.
//
// Validation rules (design §3.6):
// 1. Generation must be > lastSeenGeneration (prevents replay)
// 2. ValidAfter must be in future (sanity check)
//
// If validation succeeds, the generation counter is updated to prevent accepting older REKEY frames.
func (rm *RekeyManager) ValidateREKEY(payload []byte) (newKey [32]byte, newKeyID [4]byte, validAfter uint32, generation uint64, err error) {
	if len(payload) != 48 {
		return newKey, newKeyID, 0, 0, fmt.Errorf("invalid REKEY payload size: %d bytes (expected 48)", len(payload))
	}

	// Unmarshal payload
	copy(newKey[:], payload[0:32])
	copy(newKeyID[:], payload[32:36])
	validAfter = binary.BigEndian.Uint32(payload[36:40])
	generation = binary.BigEndian.Uint64(payload[40:48])

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Rule 1: Reject replay attacks (generation must be > last seen)
	if generation <= rm.generation {
		return newKey, newKeyID, 0, 0, fmt.Errorf("replay attack detected: generation %d <= last seen %d", generation, rm.generation)
	}

	// Rule 2: ValidAfter sanity check (must be in future, but not too far)
	now := uint32(time.Now().Unix())
	if validAfter <= now {
		return newKey, newKeyID, 0, 0, fmt.Errorf("REKEY validAfter %d is in the past (now=%d)", validAfter, now)
	}
	// Sanity: validAfter should not be more than 30 days in future (prevent time-based DoS)
	if validAfter > now+2592000 { // 30 days = 2592000 seconds
		return newKey, newKeyID, 0, 0, fmt.Errorf("REKEY validAfter %d is too far in future (>30 days)", validAfter)
	}

	// Validation passed, update generation counter
	rm.generation = generation
	if err := rm.persist(); err != nil {
		// CRITICAL: If persist fails, we've accepted the REKEY but can't prevent replay
		// Log this as CRITICAL but don't reject the frame (frame is valid)
		return newKey, newKeyID, validAfter, generation, fmt.Errorf("WARNING: REKEY validated but generation persist failed: %w", err)
	}

	return newKey, newKeyID, validAfter, generation, nil
}

// CurrentGeneration returns the current generation counter (for monitoring).
func (rm *RekeyManager) CurrentGeneration() uint64 {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.generation
}

// ComputeKeyID computes the KEY_ID for a given mesh key.
// KEY_ID = HMAC-SHA256(MESH_KEY, "key-id")[0:4]
func ComputeKeyID(meshKey [32]byte) [4]byte {
	mac := sha256.New()
	mac.Write(meshKey[:])
	mac.Write([]byte("key-id"))
	hash := mac.Sum(nil)

	var keyID [4]byte
	copy(keyID[:], hash[:4])
	return keyID
}
