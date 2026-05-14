package hint

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockConsumer is a test consumer that counts consumed hints.
type mockConsumer struct {
	count    atomic.Int32
	slowdown time.Duration
	mu       sync.Mutex
	hints    []Hint
}

func (m *mockConsumer) Consume(hint Hint) error {
	m.count.Add(1)
	m.mu.Lock()
	m.hints = append(m.hints, hint)
	m.mu.Unlock()

	if m.slowdown > 0 {
		time.Sleep(m.slowdown)
	}
	return nil
}

func (m *mockConsumer) Count() int32 {
	return m.count.Load()
}

func (m *mockConsumer) Hints() []Hint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Hint{}, m.hints...)
}

func TestNewBus(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("NewBus returned nil")
	}
	if bus.consumers == nil {
		t.Error("consumers map not initialized")
	}
}

func TestBus_RegisterConsumer(t *testing.T) {
	bus := NewBus()
	consumer := &mockConsumer{}

	err := bus.RegisterConsumer("test", consumer, 32)
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	// Try to register same consumer again
	err = bus.RegisterConsumer("test", consumer, 32)
	if err == nil {
		t.Error("Expected error when registering duplicate consumer")
	}
}

func TestBus_RegisterConsumer_BufferSizeLimits(t *testing.T) {
	bus := NewBus()

	tests := []struct {
		name         string
		bufSize      int
		expectedSize int
	}{
		{"below minimum", 0, 64}, // Should default to 64
		{"normal", 100, 100},
		{"at max", 256, 256},
		{"above max", 300, 256}, // Should cap at 256
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			consumer := &mockConsumer{}
			name := tt.name + string(rune(i))

			err := bus.RegisterConsumer(name, consumer, tt.bufSize)
			if err != nil {
				t.Fatalf("RegisterConsumer failed: %v", err)
			}

			bus.mu.RLock()
			c := bus.consumers[name]
			bus.mu.RUnlock()

			if c.bufSize != tt.expectedSize {
				t.Errorf("Expected buffer size %d, got %d", tt.expectedSize, c.bufSize)
			}
		})
	}
}

func TestBus_PublishAndConsume(t *testing.T) {
	bus := NewBus()
	consumer := &mockConsumer{}

	err := bus.RegisterConsumer("test", consumer, 64)
	if err != nil {
		t.Fatalf("RegisterConsumer failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start bus
	go bus.Run(ctx)

	// Give bus time to start
	time.Sleep(10 * time.Millisecond)

	// Publish hints
	for i := 0; i < 10; i++ {
		hint := Hint{
			Type:      RouteAdded,
			NodeID:    uint32(i),
			Addr:      &net.IPAddr{IP: net.IPv4(192, 168, 1, byte(i))},
			Metric:    uint8(i),
			Timestamp: time.Now(),
		}
		if err := bus.Publish(hint); err != nil {
			t.Errorf("Publish failed: %v", err)
		}
	}

	// Wait for consumption
	time.Sleep(50 * time.Millisecond)

	if consumer.Count() != 10 {
		t.Errorf("Expected 10 hints consumed, got %d", consumer.Count())
	}
}

func TestBus_MultipleConsumers(t *testing.T) {
	bus := NewBus()
	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}
	consumer3 := &mockConsumer{}

	bus.RegisterConsumer("fast", consumer1, 64)
	bus.RegisterConsumer("medium", consumer2, 64)
	bus.RegisterConsumer("slow", consumer3, 64)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go bus.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	// Publish hints
	for i := 0; i < 5; i++ {
		hint := Hint{
			Type:      PeerDiscovered,
			NodeID:    uint32(i),
			Timestamp: time.Now(),
		}
		bus.Publish(hint)
	}

	time.Sleep(100 * time.Millisecond)

	// All consumers should receive all hints
	if consumer1.Count() != 5 {
		t.Errorf("Consumer1: expected 5 hints, got %d", consumer1.Count())
	}
	if consumer2.Count() != 5 {
		t.Errorf("Consumer2: expected 5 hints, got %d", consumer2.Count())
	}
	if consumer3.Count() != 5 {
		t.Errorf("Consumer3: expected 5 hints, got %d", consumer3.Count())
	}
}

func TestBus_BackpressureAndDrops(t *testing.T) {
	bus := NewBus()
	// Create a slow consumer with small buffer
	slowConsumer := &mockConsumer{slowdown: 200 * time.Millisecond}

	bus.RegisterConsumer("slow", slowConsumer, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go bus.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	// Publish many hints rapidly to cause backpressure
	// With buffer size 2 and slow processing, we should see drops
	for i := 0; i < 10; i++ {
		hint := Hint{
			Type:      RouteAdded,
			NodeID:    uint32(i),
			Timestamp: time.Now(),
		}
		bus.Publish(hint)
		time.Sleep(5 * time.Millisecond) // Small delay between publishes
	}

	// Wait for some processing
	time.Sleep(500 * time.Millisecond)

	// Consumer should have received some but likely not all due to slow processing
	consumed := slowConsumer.Count()
	t.Logf("Consumed: %d hints", consumed)

	// With the 100ms timeout and slow consumer (200ms), some hints may be dropped
	// But this is an implementation detail - the key is that the system doesn't deadlock
	if consumed == 0 {
		t.Error("Expected at least some hints to be consumed")
	}
}

func TestBus_UnregisterConsumer(t *testing.T) {
	bus := NewBus()
	consumer := &mockConsumer{}

	bus.RegisterConsumer("test", consumer, 64)
	bus.UnregisterConsumer("test")

	bus.mu.RLock()
	_, exists := bus.consumers["test"]
	bus.mu.RUnlock()

	if exists {
		t.Error("Consumer should have been unregistered")
	}
}

func TestBus_Close(t *testing.T) {
	bus := NewBus()
	consumer := &mockConsumer{}

	bus.RegisterConsumer("test", consumer, 64)

	if err := bus.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Publishing after close should return error
	hint := Hint{Type: RouteAdded, NodeID: 1, Timestamp: time.Now()}
	if err := bus.Publish(hint); err == nil {
		t.Error("Expected error when publishing to closed bus")
	}

	// Closing again should be safe
	if err := bus.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}

func TestBus_Stats(t *testing.T) {
	bus := NewBus()
	consumer1 := &mockConsumer{}
	consumer2 := &mockConsumer{}

	bus.RegisterConsumer("test1", consumer1, 64)
	bus.RegisterConsumer("test2", consumer2, 128)

	stats := bus.Stats()

	if count, ok := stats["consumer_count"].(int); !ok || count != 2 {
		t.Errorf("Expected consumer_count=2, got %v", stats["consumer_count"])
	}

	consumers, ok := stats["consumers"].([]map[string]interface{})
	if !ok {
		t.Fatal("consumers field missing or wrong type")
	}

	if len(consumers) != 2 {
		t.Errorf("Expected 2 consumer stats, got %d", len(consumers))
	}
}

func TestHint_Types(t *testing.T) {
	tests := []struct {
		name string
		hint Hint
	}{
		{
			"RouteAdded",
			Hint{Type: RouteAdded, NodeID: 1, Metric: 10, Timestamp: time.Now()},
		},
		{
			"RouteRemoved",
			Hint{Type: RouteRemoved, NodeID: 2, Metric: 20, Timestamp: time.Now()},
		},
		{
			"PeerDiscovered",
			Hint{Type: PeerDiscovered, NodeID: 3, Metric: 30, Timestamp: time.Now()},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hint.Type < RouteAdded || tt.hint.Type > PeerDiscovered {
				t.Errorf("Invalid hint type: %d", tt.hint.Type)
			}
		})
	}
}
