// Package lora provides TX scheduler with token bucket rate limiting for duty-cycle compliance.
package lora

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/opd-ai/conspiracy/internal/metrics"
)

// Priority levels for TX scheduler queue
const (
	PriorityHigh   = 0 // JOIN_ACK, JOIN_REQ (critical path)
	PriorityMedium = 1 // BEACON (periodic)
	PriorityLow    = 2 // ROUTE_HINT, PING/PONG (best-effort)
)

// SchedulerConfig holds configuration for the TX scheduler.
type SchedulerConfig struct {
	Radio           PacketRadio
	DutyCyclePct    float64 // EU: 1.0 (1%), US: 4.0 (4%)
	SpreadingFactor int     // SF7-SF12
	Bandwidth       int     // 125, 250, 500 kHz
	CodingRate      int     // 1-4 (typically 1 for 4/5)
	QueueSize       int     // Max entries per priority queue (default: 256)
}

// TXRequest represents a queued transmission request.
type TXRequest struct {
	Payload     []byte
	Priority    int
	EnqueueTime time.Time
	FrameType   uint8 // For metrics and logging
}

// TXScheduler manages transmission scheduling with token bucket rate limiting.
type TXScheduler struct {
	radio  PacketRadio
	sf     int
	bw     int
	cr     int
	bucket *TokenBucket
	queues [3]chan *TXRequest // HIGH, MEDIUM, LOW priority queues
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	txTotal     map[int]int // TX count per priority
	txDrops     map[int]int // Drop count per priority
	metricsLock sync.RWMutex
}

// TokenBucket implements token bucket algorithm for duty-cycle enforcement.
type TokenBucket struct {
	capacity     time.Duration // EU: 36s, US: 144s per hour
	tokens       time.Duration // Available tokens (TX time remaining)
	refillRate   time.Duration // 10ms/sec = capacity/hour
	lastRefill   time.Time
	mu           sync.Mutex
	dutyCyclePct float64
}

// NewTXScheduler creates a new transmission scheduler.
func NewTXScheduler(cfg SchedulerConfig) (*TXScheduler, error) {
	if cfg.Radio == nil {
		return nil, fmt.Errorf("radio cannot be nil")
	}
	if cfg.SpreadingFactor < 7 || cfg.SpreadingFactor > 12 {
		return nil, fmt.Errorf("invalid spreading factor: %d (must be 7-12)", cfg.SpreadingFactor)
	}
	if cfg.Bandwidth != 125 && cfg.Bandwidth != 250 && cfg.Bandwidth != 500 {
		return nil, fmt.Errorf("invalid bandwidth: %d (must be 125, 250, or 500)", cfg.Bandwidth)
	}
	if cfg.CodingRate < 1 || cfg.CodingRate > 4 {
		return nil, fmt.Errorf("invalid coding rate: %d (must be 1-4)", cfg.CodingRate)
	}
	if cfg.DutyCyclePct <= 0 {
		cfg.DutyCyclePct = 1.0 // Default to EU 1%
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 256
	}

	// Calculate token bucket capacity (max TX time per hour)
	capacity := time.Duration(float64(time.Hour) * cfg.DutyCyclePct / 100.0)
	refillRate := capacity / time.Hour // tokens per second

	ctx, cancel := context.WithCancel(context.Background())

	sched := &TXScheduler{
		radio: cfg.Radio,
		sf:    cfg.SpreadingFactor,
		bw:    cfg.Bandwidth,
		cr:    cfg.CodingRate,
		bucket: &TokenBucket{
			capacity:     capacity,
			tokens:       capacity, // Start full
			refillRate:   refillRate,
			lastRefill:   time.Now(),
			dutyCyclePct: cfg.DutyCyclePct,
		},
		queues: [3]chan *TXRequest{
			make(chan *TXRequest, cfg.QueueSize), // HIGH
			make(chan *TXRequest, cfg.QueueSize), // MEDIUM
			make(chan *TXRequest, cfg.QueueSize), // LOW
		},
		ctx:     ctx,
		cancel:  cancel,
		txTotal: make(map[int]int),
		txDrops: make(map[int]int),
	}

	// Initialize Prometheus counters
	if err := sched.initMetrics(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	// Start scheduler worker goroutine
	sched.wg.Add(1)
	go sched.run()

	slog.Info("TX scheduler started",
		"duty_cycle_pct", cfg.DutyCyclePct,
		"capacity_per_hour", capacity,
		"refill_rate_per_sec", refillRate,
		"sf", cfg.SpreadingFactor,
		"bw", cfg.Bandwidth,
		"queue_size", cfg.QueueSize)

	return sched, nil
}

// Enqueue adds a transmission request to the appropriate priority queue.
func (s *TXScheduler) Enqueue(priority int, payload []byte, frameType uint8) error {
	if priority < PriorityHigh || priority > PriorityLow {
		return fmt.Errorf("invalid priority: %d (must be 0-2)", priority)
	}

	req := &TXRequest{
		Payload:     payload,
		Priority:    priority,
		EnqueueTime: time.Now(),
		FrameType:   frameType,
	}

	select {
	case s.queues[priority] <- req:
		return nil
	default:
		// Queue full - apply backpressure by dropping lower priority frames first
		if priority == PriorityLow {
			s.recordDrop(priority, frameType)
			return fmt.Errorf("LOW priority queue full, frame dropped")
		}
		if priority == PriorityMedium {
			// Try to drop one LOW priority frame to make room
			select {
			case <-s.queues[PriorityLow]:
				s.recordDrop(PriorityLow, 0)
				slog.Debug("Dropped LOW priority frame to make room for MEDIUM priority")
			default:
			}
			// Try enqueue again
			select {
			case s.queues[priority] <- req:
				return nil
			default:
				s.recordDrop(priority, frameType)
				return fmt.Errorf("MEDIUM priority queue full, frame dropped")
			}
		}
		// HIGH priority - unconditional enqueue attempt
		s.recordDrop(priority, frameType)
		return fmt.Errorf("HIGH priority queue full, frame dropped")
	}
}

// run is the main scheduler worker loop.
func (s *TXScheduler) run() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			slog.Info("TX scheduler stopped")
			return
		default:
		}

		// Select next frame from priority queues (HIGH > MEDIUM > LOW)
		// Use explicit priority ordering instead of non-deterministic select
		var req *TXRequest
		select {
		case req = <-s.queues[PriorityHigh]:
			// HIGH priority takes precedence
		default:
			// If no HIGH priority, try MEDIUM
			select {
			case req = <-s.queues[PriorityMedium]:
				// MEDIUM priority
			default:
				// If no MEDIUM, try LOW or wait
				select {
				case req = <-s.queues[PriorityLow]:
					// LOW priority
				case <-s.ctx.Done():
					slog.Info("TX scheduler stopped")
					return
				case <-time.After(10 * time.Millisecond):
					// No frames available, retry
					continue
				}
			}
		}

		if req == nil {
			continue
		}

		// Calculate time-on-air
		toa, err := Calculate(len(req.Payload), s.sf, s.bw, s.cr)
		if err != nil {
			slog.Error("ToA calculation failed", "error", err, "payload_size", len(req.Payload))
			s.recordDrop(req.Priority, req.FrameType)
			continue
		}

		// Wait for tokens to become available
		if !s.bucket.Wait(s.ctx, toa) {
			slog.Warn("Token bucket wait interrupted", "frame_type", frameTypeName(req.FrameType))
			s.recordDrop(req.Priority, req.FrameType)
			continue
		}

		// Transmit frame
		if err := s.transmit(req, toa); err != nil {
			slog.Warn("Transmission failed", "error", err, "frame_type", frameTypeName(req.FrameType))
			s.recordDrop(req.Priority, req.FrameType)
		} else {
			s.recordTX(req.Priority, req.FrameType)
		}

		// Update duty cycle utilization metric
		s.updateDutyCycleMetric()
	}
}

// transmit sends a frame via radio and records TX time.
func (s *TXScheduler) transmit(req *TXRequest, toa time.Duration) error {
	// Perform LBT (Listen Before Talk) if radio supports it
	if lbtRadio, ok := s.radio.(LBTRadio); ok {
		const rssiThreshold = -80 // dBm; channel busy if RSSI > -80 dBm
		const maxRetries = 5

		clear, err := lbtRadio.PerformLBT(s.ctx, rssiThreshold, maxRetries)
		if err != nil {
			return fmt.Errorf("LBT check failed: %w", err)
		}
		if !clear {
			slog.Warn("LoRa channel busy after LBT retries; frame dropped",
				"frame_type", frameTypeName(req.FrameType),
				"priority", priorityName(req.Priority))
			metrics.LoraTXDrops.WithLabelValues(priorityName(req.Priority), "lbt_failed").Inc()
			return fmt.Errorf("LBT failed: channel busy")
		}
	}

	txStart := time.Now()
	if err := s.radio.Send(s.ctx, req.Payload); err != nil {
		return fmt.Errorf("radio send failed: %w", err)
	}
	actualTxTime := time.Since(txStart)

	// Use ToA estimate (more accurate than actual TX time for duty-cycle accounting)
	s.bucket.Consume(toa)

	queueLatency := time.Since(req.EnqueueTime)
	slog.Debug("Frame transmitted",
		"frame_type", frameTypeName(req.FrameType),
		"priority", priorityName(req.Priority),
		"payload_size", len(req.Payload),
		"toa_ms", toa.Milliseconds(),
		"actual_tx_ms", actualTxTime.Milliseconds(),
		"queue_latency_ms", queueLatency.Milliseconds())

	return nil
}

// Wait blocks until sufficient tokens are available, returns false if context cancelled.
func (tb *TokenBucket) Wait(ctx context.Context, required time.Duration) bool {
	tb.mu.Lock()
	tb.refill()

	for tb.tokens < required {
		tb.mu.Unlock()

		// Wait before retry
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}

		tb.mu.Lock()
		tb.refill()
	}

	tb.mu.Unlock()
	return true
}

// Consume removes tokens from the bucket.
func (tb *TokenBucket) Consume(amount time.Duration) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	tb.tokens -= amount
	if tb.tokens < 0 {
		tb.tokens = 0
	}
}

// refill adds tokens based on elapsed time since last refill.
func (tb *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)

	newTokens := time.Duration(float64(elapsed) * float64(tb.refillRate) / float64(time.Second))
	tb.tokens += newTokens

	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	tb.lastRefill = now
}

// Utilization returns current duty cycle utilization (0.0-1.0).
func (tb *TokenBucket) Utilization() float64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.refill()
	used := tb.capacity - tb.tokens
	if used < 0 {
		used = 0
	}
	return float64(used) / float64(tb.capacity)
}

// recordTX increments TX counter.
func (s *TXScheduler) recordTX(priority int, frameType uint8) {
	s.metricsLock.Lock()
	s.txTotal[priority]++
	s.metricsLock.Unlock()

	// Update Prometheus counter
	metrics.LoraTXTotal.WithLabelValues(priorityName(priority), frameTypeName(frameType)).Inc()
}

// recordDrop increments drop counter.
func (s *TXScheduler) recordDrop(priority int, frameType uint8) {
	s.metricsLock.Lock()
	s.txDrops[priority]++
	s.metricsLock.Unlock()

	// Update Prometheus counter
	metrics.LoraTXDrops.WithLabelValues(priorityName(priority), dropReasonName(priority)).Inc()
}

// updateDutyCycleMetric updates Prometheus gauge.
func (s *TXScheduler) updateDutyCycleMetric() {
	utilization := s.bucket.Utilization()
	metrics.DutyCycleUtilization.Set(utilization)

	if utilization > 0.9 {
		slog.Warn("Duty cycle utilization high", "utilization_pct", utilization*100)
	}
}

// GetStats returns scheduler statistics.
func (s *TXScheduler) GetStats() map[string]interface{} {
	s.metricsLock.RLock()
	defer s.metricsLock.RUnlock()

	return map[string]interface{}{
		"tx_total":               s.txTotal,
		"tx_drops":               s.txDrops,
		"duty_cycle_utilization": s.bucket.Utilization(),
		"queue_depth_high":       len(s.queues[PriorityHigh]),
		"queue_depth_medium":     len(s.queues[PriorityMedium]),
		"queue_depth_low":        len(s.queues[PriorityLow]),
	}
}

// Close stops the scheduler and waits for goroutines to exit.
func (s *TXScheduler) Close() error {
	slog.Info("Shutting down TX scheduler")
	s.cancel()
	s.wg.Wait()
	return nil
}

// initMetrics initializes Prometheus counters.
func (s *TXScheduler) initMetrics() error {
	// Counters are created in metrics package
	return nil
}

// Helper functions for string conversion
func priorityName(priority int) string {
	switch priority {
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	default:
		return "unknown"
	}
}

func frameTypeName(frameType uint8) string {
	switch frameType {
	case FrameTypeBEACON:
		return "beacon"
	case FrameTypeJOIN_REQ:
		return "join_req"
	case FrameTypeJOIN_ACK:
		return "join_ack"
	case FrameTypeROUTE_HINT:
		return "route_hint"
	case FrameTypePING:
		return "ping"
	case FrameTypePONG:
		return "pong"
	case FrameTypeREKEY:
		return "rekey"
	default:
		return "unknown"
	}
}

func dropReasonName(priority int) string {
	switch priority {
	case PriorityHigh:
		return "queue_full_high"
	case PriorityMedium:
		return "queue_full_medium"
	case PriorityLow:
		return "queue_full_low"
	default:
		return "unknown"
	}
}
