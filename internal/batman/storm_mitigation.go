// Package batman provides OGM storm mitigation during network partition rejoins.
package batman

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// StormMitigator manages OGM rate limiting during partition rejoin events
type StormMitigator struct {
	mu                sync.Mutex
	rateLimiters      map[uint32]*ogmRateLimiter // Per-originator rate limiters
	peerCount         int
	lastPeerCount     int
	lastPeerCheckTime time.Time
	inRejoinMode      bool
	rejoinModeExpiry  time.Time
	churnEvents       []time.Time // Track churn events for detecting storms
}

// ogmRateLimiter implements token bucket rate limiting per originator
type ogmRateLimiter struct {
	tokens        float64
	maxTokens     float64
	refillRate    float64 // tokens per second
	lastRefill    time.Time
	droppedCount  int64
	acceptedCount int64
}

const (
	// Normal operation rate limits
	normalOGMRate = 10.0 // OGM per second
	normalBurst   = 20.0 // burst capacity

	// Partition rejoin enhanced limits
	rejoinOGMRate = 10.0 // Keep same rate but increase burst
	rejoinBurst   = 50.0 // temporary burst allowance

	// Rejoin detection thresholds
	rejoinPeerThreshold = 0.5  // 50% peer count increase
	rejoinDetectWindow  = 10.0 // seconds
	rejoinModeDuration  = 60.0 // seconds

	// Churn detection
	churnRateThreshold = 10.0 // events/sec
	churnWindowSize    = 30   // track last 30 events
	maxJitter          = 5.0  // max jitter in seconds
)

// NewStormMitigator creates a new OGM storm mitigator
func NewStormMitigator() *StormMitigator {
	return &StormMitigator{
		rateLimiters:      make(map[uint32]*ogmRateLimiter),
		lastPeerCheckTime: time.Now(),
		churnEvents:       make([]time.Time, 0, churnWindowSize),
	}
}

// UpdatePeerCount updates the peer count and detects partition rejoin
func (sm *StormMitigator) UpdatePeerCount(count int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(sm.lastPeerCheckTime).Seconds()

	// Check for partition rejoin: >50% peer count increase within 10s
	// Only trigger if we have an established baseline (lastPeerCount >= 10)
	if elapsed < rejoinDetectWindow && sm.lastPeerCount >= 10 {
		increase := float64(count-sm.lastPeerCount) / float64(sm.lastPeerCount)
		if increase > rejoinPeerThreshold && !sm.inRejoinMode {
			sm.enterRejoinMode(count)
		}
	}

	// Update peer count
	sm.lastPeerCount = sm.peerCount
	sm.peerCount = count
	sm.lastPeerCheckTime = now

	// Check if rejoin mode should expire
	if sm.inRejoinMode && now.After(sm.rejoinModeExpiry) {
		sm.exitRejoinMode()
	}
}

// enterRejoinMode activates enhanced burst capacity
func (sm *StormMitigator) enterRejoinMode(newPeerCount int) {
	sm.inRejoinMode = true
	sm.rejoinModeExpiry = time.Now().Add(time.Duration(rejoinModeDuration) * time.Second)

	slog.Info("OGM storm mitigation: entering rejoin mode",
		"old_peer_count", sm.lastPeerCount,
		"new_peer_count", newPeerCount,
		"increase_pct", int(float64(newPeerCount-sm.lastPeerCount)/float64(sm.lastPeerCount+1)*100),
		"burst_capacity", rejoinBurst,
	)

	// Update all existing rate limiters to rejoin parameters
	for _, rl := range sm.rateLimiters {
		rl.maxTokens = rejoinBurst
		rl.refillRate = rejoinOGMRate
	}
}

// exitRejoinMode returns to normal operation
func (sm *StormMitigator) exitRejoinMode() {
	sm.inRejoinMode = false

	slog.Info("OGM storm mitigation: exiting rejoin mode, returning to normal rate limits",
		"current_peer_count", sm.peerCount,
	)

	// Reset all rate limiters to normal parameters
	for _, rl := range sm.rateLimiters {
		rl.maxTokens = normalBurst
		rl.refillRate = normalOGMRate
		// Cap current tokens at new max
		if rl.tokens > normalBurst {
			rl.tokens = normalBurst
		}
	}
}

// AllowOGM checks if an OGM from the specified originator should be accepted
func (sm *StormMitigator) AllowOGM(originatorID uint32) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	rl, exists := sm.rateLimiters[originatorID]
	if !exists {
		// Create new rate limiter for this originator
		maxTokens := normalBurst
		refillRate := normalOGMRate

		if sm.inRejoinMode {
			maxTokens = rejoinBurst
			refillRate = rejoinOGMRate
		}

		rl = &ogmRateLimiter{
			tokens:     maxTokens, // Start with full bucket
			maxTokens:  maxTokens,
			refillRate: refillRate,
			lastRefill: time.Now(),
		}
		sm.rateLimiters[originatorID] = rl
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(rl.lastRefill).Seconds()
	rl.tokens = min(rl.maxTokens, rl.tokens+elapsed*rl.refillRate)
	rl.lastRefill = now

	// Check if we have tokens available
	if rl.tokens >= 1.0 {
		rl.tokens -= 1.0
		rl.acceptedCount++
		return true
	}

	// Drop OGM due to rate limit
	rl.droppedCount++

	if rl.droppedCount%100 == 1 {
		// Log every 100 drops to avoid log spam
		slog.Warn("OGM rate limit exceeded",
			"originator_id", originatorID,
			"dropped_count", rl.droppedCount,
			"accepted_count", rl.acceptedCount,
			"in_rejoin_mode", sm.inRejoinMode,
		)
	}

	return false
}

// RecordChurnEvent records a peer join/leave event for churn rate tracking
func (sm *StormMitigator) RecordChurnEvent() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	sm.churnEvents = append(sm.churnEvents, now)

	// Keep only recent events (within time window)
	cutoff := now.Add(-time.Second * time.Duration(churnWindowSize))
	validEvents := 0
	for i := len(sm.churnEvents) - 1; i >= 0; i-- {
		if sm.churnEvents[i].After(cutoff) {
			validEvents++
		} else {
			break
		}
	}

	// Trim old events
	if validEvents < len(sm.churnEvents) {
		sm.churnEvents = sm.churnEvents[len(sm.churnEvents)-validEvents:]
	}
}

// GetChurnRate returns current churn rate (events per second)
func (sm *StormMitigator) GetChurnRate() float64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.GetChurnRateUnsafe()
}

// GetStaggeredJitter returns random jitter for staggered OGM re-injection
// Returns 0 if churn rate is below threshold, otherwise 0-5s random jitter
func (sm *StormMitigator) GetStaggeredJitter() time.Duration {
	churnRate := sm.GetChurnRate()

	if churnRate < churnRateThreshold {
		return 0
	}

	// High churn detected: add random jitter to spread OGM broadcasts
	jitterSec := rand.Float64() * maxJitter
	slog.Debug("High churn rate detected; applying staggered jitter",
		"churn_rate", churnRate,
		"jitter_sec", jitterSec,
	)

	return time.Duration(jitterSec * float64(time.Second))
}

// GetStats returns current storm mitigation statistics
type StormStats struct {
	InRejoinMode        bool
	RejoinModeRemaining float64 // seconds
	PeerCount           int
	ChurnRate           float64 // events/sec
	TotalOriginators    int
	TotalOGMsAccepted   int64
	TotalOGMsDropped    int64
}

// GetStats returns current statistics
func (sm *StormMitigator) GetStats() StormStats {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var totalAccepted, totalDropped int64
	for _, rl := range sm.rateLimiters {
		totalAccepted += rl.acceptedCount
		totalDropped += rl.droppedCount
	}

	remaining := 0.0
	if sm.inRejoinMode {
		remaining = time.Until(sm.rejoinModeExpiry).Seconds()
		if remaining < 0 {
			remaining = 0
		}
	}

	return StormStats{
		InRejoinMode:        sm.inRejoinMode,
		RejoinModeRemaining: remaining,
		PeerCount:           sm.peerCount,
		ChurnRate:           sm.GetChurnRateUnsafe(),
		TotalOriginators:    len(sm.rateLimiters),
		TotalOGMsAccepted:   totalAccepted,
		TotalOGMsDropped:    totalDropped,
	}
}

// GetChurnRateUnsafe is like GetChurnRate but assumes caller holds lock
func (sm *StormMitigator) GetChurnRateUnsafe() float64 {
	if len(sm.churnEvents) < 2 {
		return 0
	}

	now := time.Now()
	cutoff := now.Add(-time.Second * time.Duration(churnWindowSize))

	count := 0
	for i := len(sm.churnEvents) - 1; i >= 0; i-- {
		if sm.churnEvents[i].After(cutoff) {
			count++
		}
	}

	if count < 2 {
		return 0
	}

	oldest := sm.churnEvents[len(sm.churnEvents)-count]
	duration := now.Sub(oldest).Seconds()
	if duration <= 0 {
		return 0
	}

	return float64(count) / duration
}

// Reset clears all rate limiters (for testing)
func (sm *StormMitigator) Reset() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.rateLimiters = make(map[uint32]*ogmRateLimiter)
	sm.peerCount = 0
	sm.lastPeerCount = 0
	sm.lastPeerCheckTime = time.Now()
	sm.inRejoinMode = false
	sm.churnEvents = sm.churnEvents[:0]
}

// MonitorPeerCount starts a goroutine to periodically update peer count
func (sm *StormMitigator) MonitorPeerCount(ctx context.Context, getPeerCount func() int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count := getPeerCount()
			sm.UpdatePeerCount(count)
		}
	}
}

// Helper function for min
func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
