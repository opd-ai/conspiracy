// Package lora provides multi-frequency bridge node functionality.
package lora

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
)

// BridgeNode manages frequency scanning and frame forwarding for multi-frequency zoning.
type BridgeNode struct {
	radio         PacketRadio
	ng            *crypto.NonceGenerator
	meshKey       []byte
	nodeID        uint32
	zm            *ZoneManager
	scheduler     *TXScheduler
	scanDuration  time.Duration
	seenFrames    *BloomFilter
	primaryFreq   float64
	bridgeFreqs   []float64
	currentFreqMu sync.RWMutex
	currentFreq   float64
}

// BridgeConfig holds configuration for bridge node operation.
type BridgeConfig struct {
	Radio        PacketRadio
	NonceGen     *crypto.NonceGenerator
	MeshKey      []byte
	NodeID       uint32
	ZoneManager  *ZoneManager
	Scheduler    *TXScheduler
	ScanDuration time.Duration // Time to listen on each frequency (default: 20s)
}

// NewBridgeNode creates a new bridge node instance.
func NewBridgeNode(cfg BridgeConfig) (*BridgeNode, error) {
	if cfg.ZoneManager == nil {
		return nil, fmt.Errorf("zone manager cannot be nil")
	}
	if cfg.ScanDuration == 0 {
		cfg.ScanDuration = 20 * time.Second
	}

	return &BridgeNode{
		radio:        cfg.Radio,
		ng:           cfg.NonceGen,
		meshKey:      cfg.MeshKey,
		nodeID:       cfg.NodeID,
		zm:           cfg.ZoneManager,
		scheduler:    cfg.Scheduler,
		scanDuration: cfg.ScanDuration,
		seenFrames:   NewBloomFilter(10000, 0.01),
		primaryFreq:  cfg.ZoneManager.GetAssignedFrequency(),
		bridgeFreqs:  []float64{},
		currentFreq:  cfg.ZoneManager.GetAssignedFrequency(),
	}, nil
}

// Run starts the bridge node frequency scanning loop.
func (bn *BridgeNode) Run(ctx context.Context) error {
	slog.Info("Bridge node starting",
		"node_id", bn.nodeID,
		"primary_freq", bn.primaryFreq,
		"scan_duration", bn.scanDuration)

	ticker := time.NewTicker(bn.scanDuration)
	defer ticker.Stop()

	frequencies := []float64{bn.primaryFreq}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Bridge node stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			bn.updateBridgeFrequencies(&frequencies)
			bn.cycleFrequency(ctx, frequencies)
		}
	}
}

// updateBridgeFrequencies refreshes the list of frequencies to monitor.
func (bn *BridgeNode) updateBridgeFrequencies(frequencies *[]float64) {
	if !bn.zm.IsBridgeNode() {
		// Not in bridge mode, monitor only primary frequency
		*frequencies = []float64{bn.primaryFreq}
		return
	}

	// In bridge mode, scan primary + bridge frequencies
	bridgeFreqs := bn.zm.GetBridgeFrequencies()
	*frequencies = append([]float64{bn.primaryFreq}, bridgeFreqs...)

	if len(*frequencies) != len(bn.bridgeFreqs)+1 {
		slog.Info("Bridge frequencies updated",
			"primary", bn.primaryFreq,
			"bridge_freqs", bridgeFreqs,
			"total_frequencies", len(*frequencies))
		bn.bridgeFreqs = bridgeFreqs
	}
}

// cycleFrequency switches to the next frequency in the rotation.
func (bn *BridgeNode) cycleFrequency(ctx context.Context, frequencies []float64) {
	if len(frequencies) == 0 {
		return
	}

	// Find current frequency index
	currentIdx := 0
	for i, f := range frequencies {
		if f == bn.getCurrentFrequency() {
			currentIdx = i
			break
		}
	}

	// Move to next frequency
	nextIdx := (currentIdx + 1) % len(frequencies)
	nextFreq := frequencies[nextIdx]

	if err := bn.setFrequency(ctx, nextFreq); err != nil {
		slog.Warn("Failed to set frequency", "freq", nextFreq, "error", err)
	}
}

// setFrequency tunes the radio to the specified frequency.
func (bn *BridgeNode) setFrequency(ctx context.Context, freq float64) error {
	bn.currentFreqMu.Lock()
	defer bn.currentFreqMu.Unlock()

	if freq == bn.currentFreq {
		return nil
	}

	// Note: This requires the radio interface to support frequency tuning
	// For now, log the frequency change
	slog.Debug("Bridge node frequency change",
		"from", bn.currentFreq,
		"to", freq)

	bn.currentFreq = freq
	return nil
}

// getCurrentFrequency returns the currently tuned frequency.
func (bn *BridgeNode) getCurrentFrequency() float64 {
	bn.currentFreqMu.RLock()
	defer bn.currentFreqMu.RUnlock()
	return bn.currentFreq
}

// ProcessFrame handles received frames and determines if forwarding is needed.
func (bn *BridgeNode) ProcessFrame(ctx context.Context, data []byte) error {
	hdr, payload, err := UnmarshalFrame(data)
	if err != nil {
		return err
	}

	// Only forward BEACON and JOIN_ACK frames
	if !bn.zm.ShouldForwardFrame(hdr.FrameType) {
		return nil
	}

	// Check if frame has already been seen (prevent loops)
	frameID := bn.computeFrameID(hdr)
	if bn.seenFrames.Contains(frameID) {
		slog.Debug("Frame already forwarded, dropping", "node_id", hdr.NodeID, "seq", hdr.FrameSeq)
		return nil
	}

	// Decrypt and parse BEACON to check TTL
	if hdr.FrameType == FrameTypeBEACON {
		plaintext, err := crypto.Decrypt(bn.meshKey, hdr.Nonce, payload)
		if err != nil {
			return fmt.Errorf("failed to decrypt BEACON: %w", err)
		}

		beacon, err := UnmarshalBEACONPayload(plaintext)
		if err != nil {
			return fmt.Errorf("failed to parse BEACON: %w", err)
		}

		// Drop if TTL exhausted
		if beacon.TTL == 0 {
			slog.Debug("Frame TTL exhausted, dropping", "node_id", hdr.NodeID)
			return nil
		}

		// Mark frame as seen
		bn.seenFrames.Add(frameID)

		// Forward to other frequencies
		return bn.forwardFrame(ctx, hdr, beacon)
	}

	return nil
}

// forwardFrame re-transmits a BEACON on bridge frequencies.
func (bn *BridgeNode) forwardFrame(ctx context.Context, hdr *Header, beacon *BEACONPayload) error {
	if !bn.zm.IsBridgeNode() {
		return nil
	}

	// Decrement TTL
	beacon.TTL--

	// Get current frequency and determine target frequencies
	currentFreq := bn.getCurrentFrequency()
	targetFreqs := []float64{}

	// Forward to all bridge frequencies except the one we received on
	for _, freq := range append([]float64{bn.primaryFreq}, bn.bridgeFreqs...) {
		if freq != currentFreq {
			targetFreqs = append(targetFreqs, freq)
		}
	}

	if len(targetFreqs) == 0 {
		return nil
	}

	// Re-encrypt BEACON with updated TTL
	plaintext := MarshalBEACONPayload(beacon)
	ciphertext, err := crypto.Encrypt(bn.meshKey, hdr.Nonce, plaintext)
	if err != nil {
		return fmt.Errorf("failed to encrypt forwarded BEACON: %w", err)
	}

	// Rebuild frame
	headerBytes := MarshalHeader(hdr)
	frameData := append(headerBytes, ciphertext...)

	// Forward to each target frequency
	for _, targetFreq := range targetFreqs {
		if err := bn.forwardToFrequency(ctx, frameData, targetFreq); err != nil {
			slog.Warn("Failed to forward to frequency",
				"target_freq", targetFreq,
				"error", err)
		}
	}

	return nil
}

// forwardToFrequency switches to target frequency and transmits the frame.
func (bn *BridgeNode) forwardToFrequency(ctx context.Context, frameData []byte, targetFreq float64) error {
	// Save current frequency
	currentFreq := bn.getCurrentFrequency()

	// Switch to target frequency
	if err := bn.setFrequency(ctx, targetFreq); err != nil {
		return err
	}

	// Transmit frame (duty-cycle accounting handled by scheduler)
	if err := bn.radio.Send(ctx, frameData); err != nil {
		// Restore original frequency even on error
		bn.setFrequency(ctx, currentFreq)
		return err
	}

	slog.Debug("Frame forwarded",
		"from_freq", currentFreq,
		"to_freq", targetFreq,
		"frame_size", len(frameData))

	// Restore original frequency
	return bn.setFrequency(ctx, currentFreq)
}

// computeFrameID generates a unique identifier for a frame to detect duplicates.
func (bn *BridgeNode) computeFrameID(hdr *Header) uint64 {
	h := fnv.New64a()
	binary.Write(h, binary.BigEndian, hdr.NodeID)
	binary.Write(h, binary.BigEndian, hdr.FrameSeq)
	binary.Write(h, binary.BigEndian, hdr.Timestamp)
	return h.Sum64()
}

// BloomFilter provides probabilistic set membership testing for duplicate detection.
type BloomFilter struct {
	bits    []bool
	size    int
	numHash int
	mu      sync.RWMutex
}

// NewBloomFilter creates a new Bloom filter with specified capacity and false positive rate.
func NewBloomFilter(capacity int, fpRate float64) *BloomFilter {
	// Calculate optimal size and number of hash functions
	// m = -n * ln(p) / (ln(2)^2)
	// k = (m / n) * ln(2)
	m := int(math.Ceil(-float64(capacity) * math.Log(fpRate) / (math.Log(2) * math.Log(2))))
	k := int(math.Ceil((float64(m) / float64(capacity)) * math.Log(2)))

	if m < 1000 {
		m = 1000
	}
	if k < 1 {
		k = 1
	}

	return &BloomFilter{
		bits:    make([]bool, m),
		size:    m,
		numHash: k,
	}
}

// Add inserts an element into the Bloom filter.
func (bf *BloomFilter) Add(value uint64) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for i := 0; i < bf.numHash; i++ {
		h := bf.hash(value, i)
		bf.bits[h%bf.size] = true
	}
}

// Contains checks if an element might be in the set.
func (bf *BloomFilter) Contains(value uint64) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for i := 0; i < bf.numHash; i++ {
		h := bf.hash(value, i)
		if !bf.bits[h%bf.size] {
			return false
		}
	}
	return true
}

// hash computes the i-th hash value for the given input using double hashing.
func (bf *BloomFilter) hash(value uint64, i int) int {
	// Use double hashing: h(i) = (h1 + i * h2) mod m
	h1 := fnv.New64a()
	binary.Write(h1, binary.BigEndian, value)
	hash1 := h1.Sum64()

	h2 := fnv.New64a()
	binary.Write(h2, binary.BigEndian, value^0xdeadbeef)
	hash2 := h2.Sum64()

	combined := (hash1 + uint64(i)*hash2) % uint64(bf.size)
	return int(combined)
}
