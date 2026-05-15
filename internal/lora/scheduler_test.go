package lora

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewTXScheduler(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SchedulerConfig
		wantErr bool
	}{
		{
			name: "valid EU config",
			cfg: SchedulerConfig{
				Radio:           &mockRadio{},
				DutyCyclePct:    1.0,
				SpreadingFactor: 10,
				Bandwidth:       125,
				CodingRate:      1,
			},
			wantErr: false,
		},
		{
			name: "valid US config",
			cfg: SchedulerConfig{
				Radio:           &mockRadio{},
				DutyCyclePct:    4.0,
				SpreadingFactor: 7,
				Bandwidth:       125,
				CodingRate:      1,
			},
			wantErr: false,
		},
		{
			name: "nil radio",
			cfg: SchedulerConfig{
				DutyCyclePct:    1.0,
				SpreadingFactor: 10,
				Bandwidth:       125,
				CodingRate:      1,
			},
			wantErr: true,
		},
		{
			name: "invalid SF",
			cfg: SchedulerConfig{
				Radio:           &mockRadio{},
				DutyCyclePct:    1.0,
				SpreadingFactor: 6,
				Bandwidth:       125,
				CodingRate:      1,
			},
			wantErr: true,
		},
		{
			name: "invalid bandwidth",
			cfg: SchedulerConfig{
				Radio:           &mockRadio{},
				DutyCyclePct:    1.0,
				SpreadingFactor: 10,
				Bandwidth:       100,
				CodingRate:      1,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sched, err := NewTXScheduler(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewTXScheduler() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if sched != nil {
				defer sched.Close()
			}
		})
	}
}

func TestTXScheduler_Enqueue(t *testing.T) {
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           &mockRadio{},
		DutyCyclePct:    1.0,
		SpreadingFactor: 10,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       10,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	tests := []struct {
		name      string
		priority  int
		payload   []byte
		frameType uint8
		wantErr   bool
	}{
		{
			name:      "high priority",
			priority:  PriorityHigh,
			payload:   make([]byte, 100),
			frameType: FrameTypeJOIN_ACK,
			wantErr:   false,
		},
		{
			name:      "medium priority",
			priority:  PriorityMedium,
			payload:   make([]byte, 100),
			frameType: FrameTypeBEACON,
			wantErr:   false,
		},
		{
			name:      "low priority",
			priority:  PriorityLow,
			payload:   make([]byte, 100),
			frameType: FrameTypePING,
			wantErr:   false,
		},
		{
			name:      "invalid priority",
			priority:  99,
			payload:   make([]byte, 100),
			frameType: FrameTypeBEACON,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sched.Enqueue(tt.priority, tt.payload, tt.frameType)
			if (err != nil) != tt.wantErr {
				t.Errorf("Enqueue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTXScheduler_PriorityOrdering(t *testing.T) {
	radio := &mockRadioWithDelay{delay: 50 * time.Millisecond} // Add delay to prevent immediate processing
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    100.0, // No rate limit for this test
		SpreadingFactor: 7,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       100,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Enqueue ALL frames quickly before first TX completes
	// With 50ms radio delay, we have time to enqueue all 4 frames
	if err := sched.Enqueue(PriorityLow, []byte("low1"), FrameTypePING); err != nil {
		t.Fatalf("Enqueue LOW failed: %v", err)
	}
	if err := sched.Enqueue(PriorityLow, []byte("low2"), FrameTypePONG); err != nil {
		t.Fatalf("Enqueue LOW failed: %v", err)
	}
	if err := sched.Enqueue(PriorityMedium, []byte("medium1"), FrameTypeBEACON); err != nil {
		t.Fatalf("Enqueue MEDIUM failed: %v", err)
	}
	if err := sched.Enqueue(PriorityHigh, []byte("high1"), FrameTypeJOIN_ACK); err != nil {
		t.Fatalf("Enqueue HIGH failed: %v", err)
	}

	// Wait for transmissions (4 frames × 50ms delay + ToA + processing)
	time.Sleep(500 * time.Millisecond)

	radio.mu.Lock()
	sent := radio.sentPayloads
	radio.mu.Unlock()

	if len(sent) < 2 {
		t.Fatalf("Expected at least 2 transmissions, got %d", len(sent))
	}

	// Log actual transmission order
	t.Logf("Transmission order:")
	for i, payload := range sent {
		t.Logf("  %d: %s", i+1, string(payload))
	}

	// After first LOW is processed, HIGH should jump ahead of remaining LOW frames
	// Expected order: low1 (already dequeued), high1 (preempts), medium1, low2
	// OR if all were in queue: high1, medium1, low1, low2

	// Test passes if we see proper priority preemption in any position
	// Find positions of frames
	highPos := -1
	mediumPos := -1
	lowPos := -1
	for i, payload := range sent {
		s := string(payload)
		if s == "high1" && highPos == -1 {
			highPos = i
		}
		if s == "medium1" && mediumPos == -1 {
			mediumPos = i
		}
		if (s == "low1" || s == "low2") && lowPos == -1 {
			lowPos = i
		}
	}

	// Verify priority ordering: HIGH < MEDIUM < LOW (lower index = processed earlier)
	if highPos != -1 && mediumPos != -1 && highPos > mediumPos {
		t.Errorf("HIGH priority (pos %d) processed after MEDIUM (pos %d)", highPos, mediumPos)
	}
	if mediumPos != -1 && lowPos != -1 && len(sent) > 2 {
		// Only check if we have multiple low-priority frames
		lastLowPos := -1
		for i, payload := range sent {
			if string(payload) == "low1" || string(payload) == "low2" {
				lastLowPos = i
			}
		}
		if mediumPos > lastLowPos {
			t.Errorf("MEDIUM priority (pos %d) processed after all LOW (last at %d)", mediumPos, lastLowPos)
		}
	}

	t.Logf("Priority ordering verified: HIGH=%d, MEDIUM=%d, LOW=%d", highPos, mediumPos, lowPos)
}

// mockRadioWithDelay adds artificial delay to test priority ordering
type mockRadioWithDelay struct {
	delay        time.Duration
	mu           sync.Mutex
	sentPayloads [][]byte
}

func (m *mockRadioWithDelay) Send(ctx context.Context, payload []byte) error {
	time.Sleep(m.delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentPayloads = append(m.sentPayloads, append([]byte(nil), payload...))
	return nil
}

func (m *mockRadioWithDelay) Recv(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (m *mockRadioWithDelay) Close() error {
	return nil
}

func (m *mockRadioWithDelay) SetFrequency(mhz float64) error {
	return nil
}

func (m *mockRadioWithDelay) SetSpreadingFactor(sf int) error {
	return nil
}

func (m *mockRadioWithDelay) SetBandwidth(khz int) error {
	return nil
}

func (m *mockRadioWithDelay) RSSI() (int8, error) {
	return -80, nil
}

func TestTXScheduler_DutyCycleEnforcement(t *testing.T) {
	radio := &mockRadio{}
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    1.0, // EU 1% = 36 seconds per hour
		SpreadingFactor: 10,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       100,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Enqueue 100 frames (100 bytes each = ~1s ToA per frame @ SF10 BW125)
	for i := 0; i < 100; i++ {
		payload := make([]byte, 100)
		sched.Enqueue(PriorityMedium, payload, FrameTypeBEACON)
	}

	// Wait for some transmissions
	time.Sleep(2 * time.Second)

	// Check that duty cycle utilization is being tracked
	stats := sched.GetStats()
	utilization, ok := stats["duty_cycle_utilization"].(float64)
	if !ok {
		t.Fatalf("duty_cycle_utilization not found in stats")
	}

	// With 1% duty cycle and 1s ToA frames, should not exceed capacity quickly
	if utilization > 1.0 {
		t.Errorf("Duty cycle utilization = %.2f, should not exceed 1.0", utilization)
	}

	t.Logf("Duty cycle utilization: %.2f%%", utilization*100)
}

func TestTXScheduler_QueueFullBackpressure(t *testing.T) {
	radio := &slowMockRadio{delay: 100 * time.Millisecond}
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    1.0,
		SpreadingFactor: 10,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       5, // Small queue
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Fill queue beyond capacity
	var dropCount int
	for i := 0; i < 20; i++ {
		err := sched.Enqueue(PriorityLow, make([]byte, 50), FrameTypePING)
		if err != nil {
			dropCount++
		}
	}

	if dropCount == 0 {
		t.Error("Expected some frames to be dropped due to queue full")
	}

	t.Logf("Dropped %d frames due to backpressure", dropCount)
}

func TestTokenBucket_Refill(t *testing.T) {
	tb := &TokenBucket{
		capacity:     36 * time.Second,
		tokens:       0,                     // Start empty
		refillRate:   10 * time.Millisecond, // 10ms per second
		lastRefill:   time.Now(),
		dutyCyclePct: 1.0,
	}

	// Wait for refill
	time.Sleep(500 * time.Millisecond)

	tb.mu.Lock()
	tb.refill()
	tokens := tb.tokens
	tb.mu.Unlock()

	// Should have refilled approximately 5ms (500ms * 10ms/sec)
	expectedMin := 4 * time.Millisecond
	expectedMax := 6 * time.Millisecond

	if tokens < expectedMin || tokens > expectedMax {
		t.Errorf("Tokens after 500ms = %v, expected %v-%v", tokens, expectedMin, expectedMax)
	}

	t.Logf("Tokens after 500ms refill: %v", tokens)
}

func TestTokenBucket_ConsumeAndWait(t *testing.T) {
	tb := &TokenBucket{
		capacity:     36 * time.Second,
		tokens:       1 * time.Second, // Start with 1 second
		refillRate:   10 * time.Millisecond,
		lastRefill:   time.Now(),
		dutyCyclePct: 1.0,
	}

	ctx := context.Background()

	// Consume 500ms (should succeed immediately)
	if !tb.Wait(ctx, 500*time.Millisecond) {
		t.Fatal("Wait() returned false for available tokens")
	}
	tb.Consume(500 * time.Millisecond)

	// Check remaining tokens
	tb.mu.Lock()
	remaining := tb.tokens
	tb.mu.Unlock()

	if remaining < 400*time.Millisecond || remaining > 600*time.Millisecond {
		t.Errorf("Remaining tokens = %v, expected ~500ms", remaining)
	}
}

func TestTokenBucket_WaitContextCancelled(t *testing.T) {
	tb := &TokenBucket{
		capacity:     36 * time.Second,
		tokens:       0, // Empty
		refillRate:   1 * time.Millisecond,
		lastRefill:   time.Now(),
		dutyCyclePct: 1.0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Wait for large amount (should timeout)
	if tb.Wait(ctx, 10*time.Second) {
		t.Error("Wait() should have returned false due to context cancellation")
	}
}

func TestTXScheduler_Statistics(t *testing.T) {
	radio := &mockRadio{}
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           radio,
		DutyCyclePct:    100.0, // No rate limit
		SpreadingFactor: 7,
		Bandwidth:       125,
		CodingRate:      1,
		QueueSize:       100,
	})
	if err != nil {
		t.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	// Enqueue various priority frames
	sched.Enqueue(PriorityHigh, make([]byte, 50), FrameTypeJOIN_ACK)
	sched.Enqueue(PriorityMedium, make([]byte, 50), FrameTypeBEACON)
	sched.Enqueue(PriorityLow, make([]byte, 50), FrameTypePING)

	time.Sleep(300 * time.Millisecond)

	stats := sched.GetStats()

	if stats["tx_total"] == nil {
		t.Error("tx_total not in stats")
	}
	if stats["tx_drops"] == nil {
		t.Error("tx_drops not in stats")
	}
	if stats["duty_cycle_utilization"] == nil {
		t.Error("duty_cycle_utilization not in stats")
	}

	t.Logf("Stats: %+v", stats)
}

// Mock radio for testing
type mockRadio struct {
	mu           sync.Mutex
	sentPayloads [][]byte
}

func (m *mockRadio) Send(ctx context.Context, payload []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentPayloads = append(m.sentPayloads, append([]byte(nil), payload...))
	return nil
}

func (m *mockRadio) Recv(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (m *mockRadio) Close() error {
	return nil
}

func (m *mockRadio) SetFrequency(mhz float64) error {
	return nil
}

func (m *mockRadio) SetSpreadingFactor(sf int) error {
	return nil
}

func (m *mockRadio) SetBandwidth(khz int) error {
	return nil
}

func (m *mockRadio) RSSI() (int8, error) {
	return -80, nil
}

// Slow mock radio for backpressure testing
type slowMockRadio struct {
	delay        time.Duration
	mu           sync.Mutex
	sentPayloads [][]byte
}

func (m *slowMockRadio) Send(ctx context.Context, payload []byte) error {
	time.Sleep(m.delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentPayloads = append(m.sentPayloads, append([]byte(nil), payload...))
	return nil
}

func (m *slowMockRadio) Recv(ctx context.Context) ([]byte, error) {
	return nil, nil
}

func (m *slowMockRadio) Close() error {
	return nil
}

func (m *slowMockRadio) SetFrequency(mhz float64) error {
	return nil
}

func (m *slowMockRadio) SetSpreadingFactor(sf int) error {
	return nil
}

func (m *slowMockRadio) SetBandwidth(khz int) error {
	return nil
}

func (m *slowMockRadio) RSSI() (int8, error) {
	return -80, nil
}

func BenchmarkEnqueue(b *testing.B) {
	sched, err := NewTXScheduler(SchedulerConfig{
		Radio:           &mockRadio{},
		DutyCyclePct:    1.0,
		SpreadingFactor: 10,
		Bandwidth:       125,
		CodingRate:      1,
	})
	if err != nil {
		b.Fatalf("NewTXScheduler() error = %v", err)
	}
	defer sched.Close()

	payload := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sched.Enqueue(PriorityMedium, payload, FrameTypeBEACON)
	}
}

func BenchmarkTokenBucketRefill(b *testing.B) {
	tb := &TokenBucket{
		capacity:     36 * time.Second,
		tokens:       18 * time.Second,
		refillRate:   10 * time.Millisecond,
		lastRefill:   time.Now(),
		dutyCyclePct: 1.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tb.mu.Lock()
		tb.refill()
		tb.mu.Unlock()
	}
}
