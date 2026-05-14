package crypto

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RebootCounter manages a persistent counter that MUST be incremented
// on every daemon boot to prevent nonce reuse across reboots.
//
// The counter is stored in persistent storage (default: /var/lib/conspiracyd/reboot_counter)
// and uses atomic write-rename to ensure crash-safe updates.
//
// If the counter cannot be persisted (disk full, read-only filesystem, etc.),
// the LoRa subsystem MUST NOT start to prevent catastrophic nonce reuse.
type RebootCounter struct {
	path   string
	value  uint32
	mu     sync.RWMutex
	canary string // Test file to verify write permissions
}

// NewRebootCounter creates a new reboot counter.
// storageDir: directory for persistent storage (e.g., "/var/lib/conspiracyd")
func NewRebootCounter(storageDir string) (*RebootCounter, error) {
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	rc := &RebootCounter{
		path:   filepath.Join(storageDir, "reboot_counter"),
		canary: filepath.Join(storageDir, ".canary"),
	}

	// Test write permissions with canary file
	if err := rc.testWritePermissions(); err != nil {
		return nil, fmt.Errorf("CRITICAL: Failed write permission test; LoRa disabled to prevent nonce reuse: %w", err)
	}

	// Load existing counter or initialize to 0
	if err := rc.load(); err != nil {
		return nil, err
	}

	// Increment for this boot
	if err := rc.Increment(); err != nil {
		return nil, fmt.Errorf("CRITICAL: Failed to persist reboot counter; LoRa disabled to prevent nonce reuse: %w", err)
	}

	return rc, nil
}

// testWritePermissions verifies filesystem write access.
func (rc *RebootCounter) testWritePermissions() error {
	testData := []byte("canary")
	if err := os.WriteFile(rc.canary, testData, 0o600); err != nil {
		return fmt.Errorf("canary write failed: %w", err)
	}

	// Verify read-back
	readData, err := os.ReadFile(rc.canary)
	if err != nil {
		return fmt.Errorf("canary read failed: %w", err)
	}

	if string(readData) != string(testData) {
		return fmt.Errorf("canary data mismatch")
	}

	return nil
}

// load reads the counter from persistent storage.
func (rc *RebootCounter) load() error {
	data, err := os.ReadFile(rc.path)
	if os.IsNotExist(err) {
		// First boot, initialize to 0
		rc.value = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read counter: %w", err)
	}

	if len(data) != 4 {
		return fmt.Errorf("invalid counter file size: %d bytes (expected 4)", len(data))
	}

	rc.value = binary.LittleEndian.Uint32(data)
	return nil
}

// Increment atomically updates the counter and persists to storage.
// Uses write-rename pattern for crash-safety.
func (rc *RebootCounter) Increment() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.value++

	// Serialize counter value
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, rc.value)

	// Atomic write-rename pattern
	tmpPath := rc.path + ".tmp"

	// Write to temporary file
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, rc.path); err != nil {
		os.Remove(tmpPath) // Cleanup on failure
		return fmt.Errorf("failed to rename counter file: %w", err)
	}

	return nil
}

// Value returns the current counter value.
func (rc *RebootCounter) Value() uint32 {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.value
}
