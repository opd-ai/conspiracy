package lora

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestPerformLBT_ChannelClear tests LBT when channel is clear.
func TestPerformLBT_ChannelClear(t *testing.T) {
	radio := &mockLBTRadio{
		rssi: -100, // Very low RSSI = channel clear
	}

	clear, err := radio.PerformLBT(context.Background(), -80, 5)
	if err != nil {
		t.Fatalf("PerformLBT() error = %v", err)
	}
	if !clear {
		t.Error("PerformLBT() = false, want true (channel should be clear)")
	}

	if radio.lbtAttempts != 1 {
		t.Errorf("LBT attempts = %d, want 1 (should succeed on first try)", radio.lbtAttempts)
	}
}

// TestPerformLBT_ChannelBusy tests LBT when channel is persistently busy.
func TestPerformLBT_ChannelBusy(t *testing.T) {
	radio := &mockLBTRadio{
		rssi: -70, // High RSSI = channel busy
	}

	clear, err := radio.PerformLBT(context.Background(), -80, 5)
	if err != nil {
		t.Fatalf("PerformLBT() error = %v", err)
	}
	if clear {
		t.Error("PerformLBT() = true, want false (channel should be busy)")
	}

	if radio.lbtAttempts != 5 {
		t.Errorf("LBT attempts = %d, want 5 (should retry max times)", radio.lbtAttempts)
	}
}

// TestPerformLBT_ChannelClearsAfterRetry tests LBT with transient interference.
func TestPerformLBT_ChannelClearsAfterRetry(t *testing.T) {
	radio := &mockLBTRadio{
		rssi:           -70,  // Start busy
		rssiAfterRetry: -100, // Clear after 3 attempts
		retriesToClear: 3,
	}

	start := time.Now()
	clear, err := radio.PerformLBT(context.Background(), -80, 5)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("PerformLBT() error = %v", err)
	}
	if !clear {
		t.Error("PerformLBT() = false, want true (channel should clear after retries)")
	}

	if radio.lbtAttempts != 3 {
		t.Errorf("LBT attempts = %d, want 3", radio.lbtAttempts)
	}

	// Should have backoff delays (2 retries × ~30ms average jitter)
	minExpected := 20 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("LBT completed too quickly: %v, expected at least %v", elapsed, minExpected)
	}

	t.Logf("LBT cleared after %d attempts in %v", radio.lbtAttempts, elapsed)
}

// TestPerformLBT_ContextCancellation tests LBT respects context cancellation.
func TestPerformLBT_ContextCancellation(t *testing.T) {
	radio := &mockLBTRadio{
		rssi: -70, // Channel busy
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	clear, err := radio.PerformLBT(ctx, -80, 100)
	if err == nil {
		t.Error("PerformLBT() should return error on context cancellation")
	}
	if clear {
		t.Error("PerformLBT() = true, want false on context cancellation")
	}

	t.Logf("LBT attempts before cancellation: %d", radio.lbtAttempts)
}

// TestScheduler_WithLBT tests scheduler integration with LBT-capable radio.
func TestScheduler_WithLBT(t *testing.T) {
	radio := &mockLBTRadio{
		rssi: -100, // Clear channel
	}

	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    100.0, // No rate limit
		SpreadingFactor: 7,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       10,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Enqueue a frame
	if err := sched.Enqueue(PriorityHigh, make([]byte, 50), FrameTypeJOIN_ACK); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	// Wait for transmission
	time.Sleep(200 * time.Millisecond)

	radio.mu.Lock()
	lbtCalled := radio.lbtAttempts > 0
	txCount := radio.txCount
	radio.mu.Unlock()

	if !lbtCalled {
		t.Error("LBT was not called before transmission")
	}
	if txCount != 1 {
		t.Errorf("TX count = %d, want 1", txCount)
	}

	t.Logf("LBT attempts: %d, TX count: %d", radio.lbtAttempts, txCount)
}

// TestScheduler_LBT_ChannelBusy tests scheduler drops frames when LBT fails.
func TestScheduler_LBT_ChannelBusy(t *testing.T) {
	radio := &mockLBTRadio{
		rssi: -70, // Channel always busy
	}

	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    100.0,
		SpreadingFactor: 7,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       10,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Enqueue frames
	for i := 0; i < 3; i++ {
		if err := sched.Enqueue(PriorityMedium, make([]byte, 50), FrameTypeBEACON); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
	}

	// Wait for LBT attempts and drops
	time.Sleep(500 * time.Millisecond)

	radio.mu.Lock()
	txCount := radio.txCount
	lbtAttempts := radio.lbtAttempts
	radio.mu.Unlock()

	if txCount != 0 {
		t.Errorf("TX count = %d, want 0 (all should be dropped due to LBT)", txCount)
	}
	if lbtAttempts < 3*5 {
		t.Errorf("LBT attempts = %d, want at least 15 (3 frames × 5 retries)", lbtAttempts)
	}

	t.Logf("LBT attempts: %d, TX count: %d (correctly dropped)", lbtAttempts, txCount)
}

// mockLBTRadio implements LBTRadio for testing.
type mockLBTRadio struct {
	rssi           int8 // Current RSSI value (dBm)
	rssiAfterRetry int8 // RSSI after retriesToClear attempts
	retriesToClear int  // Number of retries before channel clears
	lbtAttempts    int  // Counter for LBT attempts
	txCount        int  // Counter for successful transmissions
	sentPayloads   [][]byte
	mu             sync.Mutex
}

func (m *mockLBTRadio) PerformLBT(ctx context.Context, rssiThreshold int8, maxRetries int) (bool, error) {
	const minJitterMs = 10
	const maxJitterMs = 50

	for attempt := 0; attempt < maxRetries; attempt++ {
		m.mu.Lock()
		m.lbtAttempts++
		currentRSSI := m.rssi

		// Simulate channel clearing after N retries
		if m.retriesToClear > 0 && m.lbtAttempts >= m.retriesToClear {
			currentRSSI = m.rssiAfterRetry
		}
		m.mu.Unlock()

		// Channel is clear if RSSI < threshold
		if currentRSSI < rssiThreshold {
			return true, nil
		}

		// Apply backoff jitter
		jitter := time.Duration(minJitterMs+randomInt(maxJitterMs-minJitterMs)) * time.Millisecond
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(jitter):
		}
	}

	return false, nil
}

func (m *mockLBTRadio) Send(ctx context.Context, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.txCount++
	m.sentPayloads = append(m.sentPayloads, append([]byte(nil), payload...))
	return nil
}

func (m *mockLBTRadio) Recv(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (m *mockLBTRadio) Close() error {
	return nil
}

func (m *mockLBTRadio) SetFrequency(mhz float64) error {
	return nil
}

func (m *mockLBTRadio) SetSpreadingFactor(sf int) error {
	return nil
}

func (m *mockLBTRadio) SetBandwidth(khz int) error {
	return nil
}

func (m *mockLBTRadio) RSSI() (int8, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rssi, nil
}
