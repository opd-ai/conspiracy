// Package hint provides HintBus pub/sub for layer-3 overlay routing integration.
package hint

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

// HintType identifies the type of routing hint.
type HintType int

const (
	// RouteAdded indicates a new route is available.
	RouteAdded HintType = iota
	// RouteRemoved indicates a route is no longer available.
	RouteRemoved
	// PeerDiscovered indicates a new peer has been discovered.
	PeerDiscovered
)

// Hint represents a routing hint for layer-3 overlays.
type Hint struct {
	Type      HintType
	NodeID    uint32
	Addr      net.Addr
	Metric    uint8
	Timestamp time.Time
}

// HintProvider publishes routing hints to the bus.
type HintProvider interface {
	Publish(hint Hint) error
}

// HintConsumer receives routing hints from the bus.
type HintConsumer interface {
	Consume(hint Hint) error
}

// consumer wraps a HintConsumer with its channel and metadata.
type consumer struct {
	name      string
	impl      HintConsumer
	ch        chan Hint
	bufSize   int
	dropCount uint64
}

// Bus implements a fan-out pub/sub system for routing hints.
type Bus struct {
	mu        sync.RWMutex
	consumers map[string]*consumer
	closed    bool
}

// NewBus creates a new HintBus.
func NewBus() *Bus {
	return &Bus{
		consumers: make(map[string]*consumer),
	}
}

// RegisterConsumer registers a hint consumer with the specified buffer size.
// The buffer size controls how many hints can be queued before drops occur.
func (b *Bus) RegisterConsumer(name string, impl HintConsumer, bufSize int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return fmt.Errorf("bus is closed")
	}

	if _, exists := b.consumers[name]; exists {
		return fmt.Errorf("consumer %s already registered", name)
	}

	if bufSize < 1 {
		bufSize = 64 // Default buffer size
	}
	if bufSize > 256 {
		bufSize = 256 // Cap buffer size to prevent excessive memory use
	}

	c := &consumer{
		name:    name,
		impl:    impl,
		ch:      make(chan Hint, bufSize),
		bufSize: bufSize,
	}

	b.consumers[name] = c
	slog.Info("hint consumer registered", "name", name, "buffer_size", bufSize)

	return nil
}

// UnregisterConsumer removes a consumer from the bus.
func (b *Bus) UnregisterConsumer(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if c, exists := b.consumers[name]; exists {
		close(c.ch)
		delete(b.consumers, name)
		slog.Info("hint consumer unregistered", "name", name, "drops", c.dropCount)
	}
}

// Publish sends a hint to all registered consumers.
// Implements non-blocking send with 100ms timeout per consumer.
func (b *Bus) Publish(hint Hint) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return fmt.Errorf("bus is closed")
	}

	for _, c := range b.consumers {
		select {
		case c.ch <- hint:
			// Successfully sent
		case <-time.After(100 * time.Millisecond):
			// Consumer is slow or blocked, drop hint
			c.dropCount++
			slog.Debug("hint dropped (consumer slow)", "consumer", c.name, "drops", c.dropCount)
		}
	}

	return nil
}

// Run starts the consumer goroutines that process hints.
// It blocks until the context is cancelled.
func (b *Bus) Run(ctx context.Context) error {
	b.mu.RLock()
	consumers := make([]*consumer, 0, len(b.consumers))
	for _, c := range b.consumers {
		consumers = append(consumers, c)
	}
	b.mu.RUnlock()

	var wg sync.WaitGroup
	for _, c := range consumers {
		wg.Add(1)
		go func(cons *consumer) {
			defer wg.Done()
			b.runConsumer(ctx, cons)
		}(c)
	}

	<-ctx.Done()
	wg.Wait()
	return ctx.Err()
}

// runConsumer processes hints for a single consumer.
func (b *Bus) runConsumer(ctx context.Context, c *consumer) {
	for {
		select {
		case <-ctx.Done():
			return
		case hint, ok := <-c.ch:
			if !ok {
				return
			}
			if err := c.impl.Consume(hint); err != nil {
				slog.Error("hint consumer error", "consumer", c.name, "error", err)
			}
		}
	}
}

// Close closes the bus and all consumer channels.
func (b *Bus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}

	b.closed = true
	for name, c := range b.consumers {
		close(c.ch)
		slog.Info("hint consumer closed", "name", name, "drops", c.dropCount)
	}

	return nil
}

// Stats returns statistics about the bus and its consumers.
func (b *Bus) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["consumer_count"] = len(b.consumers)

	consumers := make([]map[string]interface{}, 0, len(b.consumers))
	for _, c := range b.consumers {
		consumers = append(consumers, map[string]interface{}{
			"name":        c.name,
			"buffer_size": c.bufSize,
			"drops":       c.dropCount,
			"queue_len":   len(c.ch),
		})
	}
	stats["consumers"] = consumers

	return stats
}
