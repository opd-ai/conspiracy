package crypto

import (
	"testing"
	"time"
)

func TestReplayWindow_FirstFrame(t *testing.T) {
	rw := NewReplayWindow()

	// First frame from node should be accepted
	if !rw.Accept(12345, 0) {
		t.Error("First frame should be accepted")
	}

	if rw.Stats() != 1 {
		t.Errorf("Expected 1 tracked node, got %d", rw.Stats())
	}
}

func TestReplayWindow_InOrderFrames(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frames 0-10 in order
	for seq := uint16(0); seq <= 10; seq++ {
		if !rw.Accept(nodeID, seq) {
			t.Errorf("Frame %d should be accepted", seq)
		}
	}
}

func TestReplayWindow_ReplayDetection(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frames 0-5
	for seq := uint16(0); seq <= 5; seq++ {
		if !rw.Accept(nodeID, seq) {
			t.Errorf("Frame %d should be accepted", seq)
		}
	}

	// Try to replay frame 3
	if rw.Accept(nodeID, 3) {
		t.Error("Replayed frame 3 should be rejected")
	}

	// Try to replay frame 0
	if rw.Accept(nodeID, 0) {
		t.Error("Replayed frame 0 should be rejected")
	}
}

func TestReplayWindow_OutOfOrderWithinWindow(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frames out of order but within window
	sequences := []uint16{100, 99, 101, 98, 102, 95, 90}

	for _, seq := range sequences {
		if !rw.Accept(nodeID, seq) {
			t.Errorf("Frame %d should be accepted (out of order but within window)", seq)
		}
	}

	// Replay should be detected
	if rw.Accept(nodeID, 99) {
		t.Error("Replayed frame 99 should be rejected")
	}
}

func TestReplayWindow_OutsideWindow(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frame 200
	if !rw.Accept(nodeID, 200) {
		t.Error("Frame 200 should be accepted")
	}

	// Frame 50 is more than 128 behind, should be rejected
	if rw.Accept(nodeID, 50) {
		t.Error("Frame 50 should be rejected (outside window)")
	}

	// Frame 73 is exactly at edge (200-127=73), should be accepted
	if !rw.Accept(nodeID, 73) {
		t.Error("Frame 73 should be accepted (at window edge)")
	}

	// Frame 72 is outside window (200-128=72), should be rejected
	if rw.Accept(nodeID, 72) {
		t.Error("Frame 72 should be rejected (just outside window)")
	}
}

func TestReplayWindow_SequenceWraparound(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frames near wrap point
	sequences := []uint16{65530, 65531, 65532, 65533, 65534, 65535, 0, 1, 2, 3}

	for _, seq := range sequences {
		if !rw.Accept(nodeID, seq) {
			t.Errorf("Frame %d should be accepted (wraparound sequence)", seq)
		}
	}

	// Replay detection across wraparound
	if rw.Accept(nodeID, 65535) {
		t.Error("Replayed frame 65535 should be rejected")
	}

	if rw.Accept(nodeID, 0) {
		t.Error("Replayed frame 0 should be rejected")
	}

	// Accept new frame after wraparound
	if !rw.Accept(nodeID, 10) {
		t.Error("Frame 10 should be accepted")
	}
}

func TestReplayWindow_MultipleNodes(t *testing.T) {
	rw := NewReplayWindow()

	// Track multiple nodes independently
	node1 := uint32(11111)
	node2 := uint32(22222)
	node3 := uint32(33333)

	// Each node sends frames
	for seq := uint16(0); seq < 10; seq++ {
		if !rw.Accept(node1, seq) {
			t.Errorf("Node1 frame %d should be accepted", seq)
		}
		if !rw.Accept(node2, seq) {
			t.Errorf("Node2 frame %d should be accepted", seq)
		}
		if !rw.Accept(node3, seq) {
			t.Errorf("Node3 frame %d should be accepted", seq)
		}
	}

	if rw.Stats() != 3 {
		t.Errorf("Expected 3 tracked nodes, got %d", rw.Stats())
	}

	// Replay detection is per-node
	if rw.Accept(node1, 5) {
		t.Error("Node1 replayed frame should be rejected")
	}

	if rw.Accept(node2, 5) {
		t.Error("Node2 replayed frame should be rejected")
	}

	// Node1 replay doesn't affect node2
	if !rw.Accept(node2, 20) {
		t.Error("Node2 new frame should be accepted")
	}
}

func TestReplayWindow_LargeGap(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frame 100
	if !rw.Accept(nodeID, 100) {
		t.Error("Frame 100 should be accepted")
	}

	// Large gap (more than window size)
	if !rw.Accept(nodeID, 1000) {
		t.Error("Frame 1000 should be accepted (large gap)")
	}

	// Old frames should be rejected
	if rw.Accept(nodeID, 100) {
		t.Error("Old frame 100 should be rejected after large gap")
	}

	// Recent frames should work
	if !rw.Accept(nodeID, 950) {
		t.Error("Frame 950 should be accepted (within new window)")
	}
}

func TestReplayWindow_PruneStale(t *testing.T) {
	rw := NewReplayWindow()

	// Add some nodes
	rw.Accept(11111, 0)
	rw.Accept(22222, 0)
	rw.Accept(33333, 0)

	if rw.Stats() != 3 {
		t.Errorf("Expected 3 nodes before pruning, got %d", rw.Stats())
	}

	// Manually set lastSeen to old time for one node
	rw.mu.Lock()
	rw.windows[11111].lastSeen = time.Now().Add(-25 * time.Hour)
	rw.windows[22222].lastSeen = time.Now().Add(-25 * time.Hour)
	rw.mu.Unlock()

	// Prune stale entries
	rw.PruneStale()

	if rw.Stats() != 1 {
		t.Errorf("Expected 1 node after pruning, got %d", rw.Stats())
	}

	// Node 33333 should still be tracked
	rw.mu.RLock()
	_, exists := rw.windows[33333]
	rw.mu.RUnlock()

	if !exists {
		t.Error("Node 33333 should still exist after pruning")
	}
}

func TestReplayWindow_BitShifting(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Accept frame 100
	if !rw.Accept(nodeID, 100) {
		t.Error("Frame 100 should be accepted")
	}

	// Accept frame 105 (shifts window by 5)
	if !rw.Accept(nodeID, 105) {
		t.Error("Frame 105 should be accepted")
	}

	// Frame 100 should still be in window (105 - 100 = 5 < 128)
	if rw.Accept(nodeID, 100) {
		t.Error("Replayed frame 100 should be rejected")
	}

	// Accept frame 101-104 out of order
	for seq := uint16(101); seq <= 104; seq++ {
		if !rw.Accept(nodeID, seq) {
			t.Errorf("Frame %d should be accepted", seq)
		}
	}

	// All should be marked as received
	for seq := uint16(100); seq <= 105; seq++ {
		if rw.Accept(nodeID, seq) {
			t.Errorf("Replayed frame %d should be rejected", seq)
		}
	}
}

func TestReplayWindow_EdgeCases(t *testing.T) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	// Exactly at window boundary
	if !rw.Accept(nodeID, 0) {
		t.Error("Frame 0 should be accepted")
	}

	if !rw.Accept(nodeID, 128) {
		t.Error("Frame 128 should be accepted (shift window by exactly 128)")
	}

	// Frame 0 should now be outside window
	if rw.Accept(nodeID, 0) {
		t.Error("Frame 0 should be rejected (outside window after shift)")
	}

	// Frame 1 should be at exact edge
	if !rw.Accept(nodeID, 1) {
		t.Error("Frame 1 should be accepted (at window edge)")
	}
}

func BenchmarkReplayWindow_Accept(b *testing.B) {
	rw := NewReplayWindow()
	nodeID := uint32(12345)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rw.Accept(nodeID, uint16(i%65536))
	}
}

func BenchmarkReplayWindow_AcceptMultipleNodes(b *testing.B) {
	rw := NewReplayWindow()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodeID := uint32(i % 100) // 100 different nodes
		rw.Accept(nodeID, uint16(i%65536))
	}
}
