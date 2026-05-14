// Package crypto provides anti-replay window for frame sequence validation.
package crypto

import (
	"sync"
	"time"
)

// ReplayWindow implements RFC 6479 anti-replay protection using a 128-bit sliding window.
// Each NodeID maintains its own window to track received frame sequence numbers.
type ReplayWindow struct {
	mu      sync.RWMutex
	windows map[uint32]*nodeWindow // keyed by NodeID
}

// nodeWindow tracks replay state for a single peer.
type nodeWindow struct {
	lastSeq  uint64    // highest sequence number accepted
	bitmap   [2]uint64 // 128-bit bitmap (2 × 64-bit words)
	lastSeen time.Time // for stale entry pruning
}

const (
	windowSize = 128 // RFC 6479 recommended window size
)

// NewReplayWindow creates a new anti-replay window tracker.
func NewReplayWindow() *ReplayWindow {
	return &ReplayWindow{
		windows: make(map[uint32]*nodeWindow),
	}
}

// Accept validates a frame sequence number and updates the window state.
// Returns true if the frame should be accepted, false if it's a replay.
//
// Window convention: bit 0 represents lastSeq, bit N represents lastSeq-N
func (rw *ReplayWindow) Accept(nodeID uint32, seq uint16) bool {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	seqNum := uint64(seq)
	win, exists := rw.windows[nodeID]

	if !exists {
		rw.windows[nodeID] = rw.createNewWindow(seqNum)
		return true
	}

	win.lastSeen = time.Now()
	seqNum, win.lastSeq = rw.normalizeSequenceNumbers(seqNum, win.lastSeq)

	if seqNum > win.lastSeq {
		return rw.acceptFutureSequence(win, seqNum)
	}

	if seqNum == win.lastSeq {
		return win.bitmap[0]&1 == 0
	}

	return rw.acceptPastSequence(win, seqNum)
}

// createNewWindow initializes a window for a new peer.
func (rw *ReplayWindow) createNewWindow(seqNum uint64) *nodeWindow {
	return &nodeWindow{
		lastSeq:  seqNum,
		lastSeen: time.Now(),
		bitmap:   [2]uint64{1, 0},
	}
}

// normalizeSequenceNumbers handles uint16 wraparound.
func (rw *ReplayWindow) normalizeSequenceNumbers(seqNum, lastSeq uint64) (uint64, uint64) {
	if seqNum < lastSeq && (lastSeq-seqNum) > 32768 {
		seqNum += 65536
	} else if seqNum > lastSeq && (seqNum-lastSeq) > 32768 {
		lastSeq += 65536
	}
	return seqNum, lastSeq
}

// acceptFutureSequence processes a sequence number ahead of the window.
func (rw *ReplayWindow) acceptFutureSequence(win *nodeWindow, seqNum uint64) bool {
	diff := seqNum - win.lastSeq
	if diff >= windowSize {
		win.bitmap[0] = 1
		win.bitmap[1] = 0
	} else {
		rw.shiftWindow(win, int(diff))
	}
	win.lastSeq = seqNum
	return true
}

// acceptPastSequence validates and marks a sequence within the window.
func (rw *ReplayWindow) acceptPastSequence(win *nodeWindow, seqNum uint64) bool {
	diff := win.lastSeq - seqNum
	if diff >= windowSize {
		return false
	}

	bitIndex := int(diff)
	wordIndex := bitIndex / 64
	bitOffset := uint(bitIndex % 64)

	if win.bitmap[wordIndex]&(1<<bitOffset) != 0 {
		return false
	}

	win.bitmap[wordIndex] |= 1 << bitOffset
	return true
}

// shiftWindow shifts the bitmap left by n bits and sets bit 0 for the new lastSeq.
func (rw *ReplayWindow) shiftWindow(win *nodeWindow, n int) {
	if n <= 0 {
		return
	}

	if n >= 128 {
		win.bitmap[0] = 1 // Set bit 0 for new lastSeq
		win.bitmap[1] = 0
		return
	}

	if n >= 64 {
		// Shift by full word plus remainder
		shift := uint(n - 64)
		win.bitmap[1] = win.bitmap[0] >> shift
		win.bitmap[0] = 1 // Set bit 0 for new lastSeq
		return
	}

	// Shift within and across words
	shift := uint(n)
	win.bitmap[1] = (win.bitmap[1] << shift) | (win.bitmap[0] >> (64 - shift))
	win.bitmap[0] = (win.bitmap[0] << shift) | 1 // Set bit 0 for new lastSeq
}

// PruneStale removes window entries for nodes that haven't been seen in the last 24 hours.
func (rw *ReplayWindow) PruneStale() {
	rw.mu.Lock()
	defer rw.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for nodeID, win := range rw.windows {
		if win.lastSeen.Before(cutoff) {
			delete(rw.windows, nodeID)
		}
	}
}

// Stats returns the number of tracked nodes.
func (rw *ReplayWindow) Stats() int {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return len(rw.windows)
}
