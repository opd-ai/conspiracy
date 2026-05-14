package batman

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestStormMitigator_BasicRateLimit(t *testing.T) {
	sm := NewStormMitigator()
	originatorID := uint32(12345)

	// Should accept first OGM (bucket starts full)
	if !sm.AllowOGM(originatorID) {
		t.Error("Should accept first OGM")
	}

	// Consume all tokens (normal burst = 20)
	accepted := 1
	for i := 0; i < 30; i++ {
		if sm.AllowOGM(originatorID) {
			accepted++
		}
	}

	// Should have accepted ~20 OGMs (initial burst)
	if accepted < 15 || accepted > 25 {
		t.Errorf("Accepted %d OGMs, expected ~20 (burst capacity)", accepted)
	}
}

func TestStormMitigator_RejoinMode(t *testing.T) {
	sm := NewStormMitigator()

	// Start with 50 peers and establish baseline
	sm.UpdatePeerCount(50)
	time.Sleep(50 * time.Millisecond)
	sm.UpdatePeerCount(50) // Ensure lastPeerCount is set
	time.Sleep(100 * time.Millisecond)

	// Simulate partition rejoin: peer count jumps to 100 (100% increase)
	sm.UpdatePeerCount(100)

	// Should enter rejoin mode
	stats := sm.GetStats()
	if !stats.InRejoinMode {
		t.Error("Should enter rejoin mode after 100% peer increase")
	}

	if stats.RejoinModeRemaining <= 0 || stats.RejoinModeRemaining > 61 {
		t.Errorf("RejoinModeRemaining = %.1f, expected ~60s", stats.RejoinModeRemaining)
	}

	// Test enhanced burst capacity
	originatorID := uint32(99999)
	accepted := 0
	for i := 0; i < 60; i++ {
		if sm.AllowOGM(originatorID) {
			accepted++
		}
	}

	// Should accept ~50 OGMs (rejoin burst capacity)
	if accepted < 40 || accepted > 55 {
		t.Errorf("Accepted %d OGMs in rejoin mode, expected ~50 (enhanced burst)", accepted)
	}
}

func TestStormMitigator_RejoinModeExpiry(t *testing.T) {
	sm := NewStormMitigator()

	// Establish baseline with 50 peers
	sm.UpdatePeerCount(50)
	time.Sleep(50 * time.Millisecond)
	sm.UpdatePeerCount(50) // Ensure lastPeerCount >= 10
	time.Sleep(50 * time.Millisecond)

	// Enter rejoin mode
	sm.UpdatePeerCount(100) // 100% increase

	if !sm.GetStats().InRejoinMode {
		t.Fatal("Failed to enter rejoin mode")
	}

	// Manually expire rejoin mode by setting expiry to past
	sm.mu.Lock()
	sm.rejoinModeExpiry = time.Now().Add(-1 * time.Second)
	sm.mu.Unlock()

	// Update should trigger exit from rejoin mode
	sm.UpdatePeerCount(100)

	if sm.GetStats().InRejoinMode {
		t.Error("Should exit rejoin mode after expiry")
	}
}

func TestStormMitigator_ChurnRateDetection(t *testing.T) {
	sm := NewStormMitigator()

	// Record churn events at high rate
	for i := 0; i < 15; i++ {
		sm.RecordChurnEvent()
		time.Sleep(10 * time.Millisecond)
	}

	churnRate := sm.GetChurnRate()

	// 15 events over ~150ms = ~100 events/sec
	if churnRate < 80 || churnRate > 120 {
		t.Errorf("ChurnRate = %.1f events/sec, expected ~100", churnRate)
	}
}

func TestStormMitigator_StaggeredJitter(t *testing.T) {
	sm := NewStormMitigator()

	// Low churn: no jitter
	for i := 0; i < 3; i++ {
		sm.RecordChurnEvent()
		time.Sleep(500 * time.Millisecond)
	}

	jitter := sm.GetStaggeredJitter()
	if jitter != 0 {
		t.Errorf("Low churn should produce 0 jitter, got %v", jitter)
	}

	// High churn: should add jitter
	for i := 0; i < 20; i++ {
		sm.RecordChurnEvent()
		time.Sleep(10 * time.Millisecond)
	}

	jitter = sm.GetStaggeredJitter()
	if jitter <= 0 || jitter > 6*time.Second {
		t.Errorf("High churn should produce 0-5s jitter, got %v", jitter)
	}
}

func TestStormMitigator_MultipleOriginators(t *testing.T) {
	sm := NewStormMitigator()

	// Test rate limiting for multiple originators
	originators := []uint32{100, 200, 300, 400, 500}

	for _, id := range originators {
		accepted := 0
		for i := 0; i < 30; i++ {
			if sm.AllowOGM(id) {
				accepted++
			}
		}

		// Each originator should have independent rate limit
		if accepted < 15 || accepted > 25 {
			t.Errorf("Originator %d: accepted %d OGMs, expected ~20", id, accepted)
		}
	}

	stats := sm.GetStats()
	if stats.TotalOriginators != len(originators) {
		t.Errorf("TotalOriginators = %d, want %d", stats.TotalOriginators, len(originators))
	}
}

func TestStormMitigator_TokenRefill(t *testing.T) {
	sm := NewStormMitigator()
	originatorID := uint32(77777)

	// Consume burst
	for i := 0; i < 25; i++ {
		sm.AllowOGM(originatorID)
	}

	// Should be rate limited now
	if sm.AllowOGM(originatorID) {
		t.Error("Should be rate limited after burst")
	}

	// Wait for refill (10 tokens/sec)
	time.Sleep(200 * time.Millisecond)

	// Should accept ~2 more OGMs (0.2s × 10 tokens/sec)
	accepted := 0
	for i := 0; i < 5; i++ {
		if sm.AllowOGM(originatorID) {
			accepted++
		}
	}

	if accepted < 1 || accepted > 3 {
		t.Errorf("After refill, accepted %d OGMs, expected 1-3", accepted)
	}
}

func TestStormMitigator_GetStats(t *testing.T) {
	sm := NewStormMitigator()

	// Set up state with baseline
	sm.UpdatePeerCount(70)
	time.Sleep(50 * time.Millisecond)
	sm.UpdatePeerCount(75)

	// Generate some OGM traffic
	for i := uint32(0); i < 10; i++ {
		for j := 0; j < 5; j++ {
			sm.AllowOGM(i)
		}
	}

	stats := sm.GetStats()

	if stats.PeerCount != 75 {
		t.Errorf("PeerCount = %d, want 75", stats.PeerCount)
	}

	if stats.TotalOriginators != 10 {
		t.Errorf("TotalOriginators = %d, want 10", stats.TotalOriginators)
	}

	if stats.TotalOGMsAccepted < 40 || stats.TotalOGMsAccepted > 60 {
		t.Errorf("TotalOGMsAccepted = %d, expected ~50", stats.TotalOGMsAccepted)
	}
}

func TestStormMitigator_Reset(t *testing.T) {
	sm := NewStormMitigator()

	// Build up state with established baseline
	sm.UpdatePeerCount(90)
	time.Sleep(50 * time.Millisecond)
	sm.UpdatePeerCount(100)

	for i := uint32(0); i < 5; i++ {
		sm.AllowOGM(i)
	}
	sm.RecordChurnEvent()
	sm.RecordChurnEvent()

	stats := sm.GetStats()
	if stats.TotalOriginators == 0 || stats.TotalOGMsAccepted == 0 {
		t.Fatal("Failed to build up state")
	}

	// Reset
	sm.Reset()

	// All state should be cleared
	stats = sm.GetStats()
	if stats.TotalOriginators != 0 {
		t.Errorf("After reset, TotalOriginators = %d, want 0", stats.TotalOriginators)
	}
	if stats.TotalOGMsAccepted != 0 {
		t.Errorf("After reset, TotalOGMsAccepted = %d, want 0", stats.TotalOGMsAccepted)
	}
	if stats.PeerCount != 0 {
		t.Errorf("After reset, PeerCount = %d, want 0", stats.PeerCount)
	}
}

// TestIntegration_PartitionRejoin simulates a network split and rejoin
func TestIntegration_PartitionRejoin(t *testing.T) {
	sm := NewStormMitigator()

	// Initial network: 50 nodes
	sm.UpdatePeerCount(50)
	time.Sleep(50 * time.Millisecond)

	// Network partitions, each side has 25 nodes
	sm.UpdatePeerCount(25)
	time.Sleep(50 * time.Millisecond)

	// Partitions rejoin: peer count jumps to 50+50 = 100 (300% increase from current)
	sm.UpdatePeerCount(100)

	stats := sm.GetStats()

	// Should enter rejoin mode due to large increase
	if !stats.InRejoinMode {
		t.Error("Should enter rejoin mode after partition rejoin")
	}

	// Simulate high OGM traffic from rejoining nodes
	originatorIDs := make([]uint32, 50)
	for i := 0; i < 50; i++ {
		originatorIDs[i] = uint32(1000 + i)
	}

	// Each originator tries to send 60 OGMs (simulating burst)
	totalAccepted := 0
	totalDropped := 0

	for _, id := range originatorIDs {
		for i := 0; i < 60; i++ {
			if sm.AllowOGM(id) {
				totalAccepted++
			} else {
				totalDropped++
			}
		}
	}

	t.Logf("Partition rejoin: accepted %d OGMs, dropped %d OGMs (%.1f%% drop rate)",
		totalAccepted, totalDropped, float64(totalDropped)/float64(totalAccepted+totalDropped)*100)

	// With rejoin burst=50, should accept ~50 OGMs per originator
	avgAccepted := float64(totalAccepted) / float64(len(originatorIDs))
	if avgAccepted < 40 || avgAccepted > 55 {
		t.Errorf("Average accepted per originator = %.1f, expected ~50 (rejoin burst)", avgAccepted)
	}

	// Total drops should be significant (>20% of attempts)
	dropRate := float64(totalDropped) / float64(totalAccepted+totalDropped)
	if dropRate < 0.15 {
		t.Errorf("Drop rate = %.1f%%, expected >15%% (rate limiting working)", dropRate*100)
	}
}

func TestStormMitigator_NoFalsePositive(t *testing.T) {
	sm := NewStormMitigator()

	// Gradual peer count increase should not trigger rejoin mode
	counts := []int{10, 12, 14, 16, 18, 20}
	for _, count := range counts {
		sm.UpdatePeerCount(count)
		time.Sleep(100 * time.Millisecond)
	}

	stats := sm.GetStats()
	if stats.InRejoinMode {
		t.Error("Gradual increase should not trigger rejoin mode")
	}
}

func TestStormMitigator_MonitorPeerCount(t *testing.T) {
	sm := NewStormMitigator()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var peerCountMu sync.Mutex
	peerCount := 15 // Start above baseline threshold
	getPeerCount := func() int {
		peerCountMu.Lock()
		defer peerCountMu.Unlock()
		return peerCount
	}
	setPeerCount := func(count int) {
		peerCountMu.Lock()
		defer peerCountMu.Unlock()
		peerCount = count
	}

	// Start monitoring
	go sm.MonitorPeerCount(ctx, getPeerCount)

	// Wait for initial baseline updates (at least 2 updates to establish lastPeerCount)
	time.Sleep(2200 * time.Millisecond)

	stats := sm.GetStats()
	if stats.PeerCount < 10 {
		t.Errorf("PeerCount = %d, want >= 10 for baseline", stats.PeerCount)
	}

	// Simulate partition rejoin (>50% increase from baseline)
	// Must happen quickly (within 10s detection window)
	setPeerCount(70) // ~350% increase from 15

	// Wait for next update cycle (1 second) plus a bit more for processing
	time.Sleep(1500 * time.Millisecond)

	stats = sm.GetStats()
	if !stats.InRejoinMode {
		t.Errorf("Should detect rejoin mode via monitoring (peer_count=%d, last=%d)",
			stats.PeerCount, sm.lastPeerCount)
	}

	if stats.PeerCount != 70 {
		t.Errorf("PeerCount = %d, want 70", stats.PeerCount)
	}

	// Stop monitoring
	cancel()
	time.Sleep(100 * time.Millisecond)
}
