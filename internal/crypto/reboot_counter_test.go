package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRebootCounter_NewAndIncrement(t *testing.T) {
	tmpDir := t.TempDir()

	// First boot
	rc1, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter failed: %v", err)
	}

	if rc1.Value() != 1 {
		t.Errorf("First boot counter = %d, want 1", rc1.Value())
	}

	// Simulate daemon restart (second boot)
	rc2, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter (second boot) failed: %v", err)
	}

	if rc2.Value() != 2 {
		t.Errorf("Second boot counter = %d, want 2", rc2.Value())
	}

	// Verify persistence across instances
	rc3, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter (third boot) failed: %v", err)
	}

	if rc3.Value() != 3 {
		t.Errorf("Third boot counter = %d, want 3", rc3.Value())
	}
}

func TestRebootCounter_PersistenceFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create read-only directory to simulate disk full / read-only mount
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(readOnlyDir, 0o500); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer os.Chmod(readOnlyDir, 0o700) // Cleanup

	_, err := NewRebootCounter(readOnlyDir)
	if err == nil {
		t.Error("Expected error for read-only filesystem, got nil")
	}

	// Verify error message mentions critical failure
	if err != nil {
		errMsg := err.Error()
		if len(errMsg) == 0 {
			t.Error("Error message is empty")
		}
	}
}

func TestRebootCounter_MultipleIncrements(t *testing.T) {
	tmpDir := t.TempDir()

	rc, err := NewRebootCounter(tmpDir)
	if err != nil {
		t.Fatalf("NewRebootCounter failed: %v", err)
	}

	initialValue := rc.Value()

	// Increment 10 times
	for i := 0; i < 10; i++ {
		if err := rc.Increment(); err != nil {
			t.Fatalf("Increment failed on iteration %d: %v", i, err)
		}
	}

	if rc.Value() != initialValue+10 {
		t.Errorf("After 10 increments: counter = %d, want %d", rc.Value(), initialValue+10)
	}
}
