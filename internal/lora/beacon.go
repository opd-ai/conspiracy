// Package lora provides BEACON transmission with adaptive TX intervals and duty-cycle compliance.
package lora

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
)

// BeaconTransmitter manages periodic BEACON frame transmission with duty-cycle enforcement.
type BeaconTransmitter struct {
	radio        PacketRadio
	ng           *crypto.NonceGenerator
	meshKey      []byte
	nodeID       uint32
	payload      *BEACONPayload
	interval     time.Duration
	dutyCycleMax time.Duration // Maximum TX time per hour (EU: 36s, US: 144s)
	txWindow     *DutyCycleWindow
}

// BeaconConfig holds configuration for BEACON transmission.
type BeaconConfig struct {
	Radio        PacketRadio
	NonceGen     *crypto.NonceGenerator
	MeshKey      []byte
	NodeID       uint32
	Payload      *BEACONPayload
	Interval     time.Duration // Default: 60s between BEACONs
	DutyCyclePct float64       // EU: 1.0 (1%), US: 4.0 (4%)
}

// DutyCycleWindow tracks transmission time within a rolling 1-hour window.
type DutyCycleWindow struct {
	maxTXTime   time.Duration
	txLog       []time.Time     // Timestamps of TX events
	txDurations []time.Duration // Duration of each TX event
}

// NewBeaconTransmitter creates a new BEACON transmitter.
func NewBeaconTransmitter(cfg BeaconConfig) (*BeaconTransmitter, error) {
	if cfg.NonceGen == nil {
		return nil, fmt.Errorf("nonce generator cannot be nil")
	}
	if len(cfg.MeshKey) != 32 {
		return nil, fmt.Errorf("invalid mesh key length: %d bytes (must be 32)", len(cfg.MeshKey))
	}
	if cfg.Payload == nil {
		return nil, fmt.Errorf("BEACON payload cannot be nil")
	}
	if cfg.Interval == 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.DutyCyclePct == 0 {
		cfg.DutyCyclePct = 1.0 // Default to EU 1%
	}

	// Calculate max TX time per hour based on duty cycle percentage
	maxTXTime := time.Duration(float64(time.Hour) * cfg.DutyCyclePct / 100.0)

	return &BeaconTransmitter{
		radio:        cfg.Radio,
		ng:           cfg.NonceGen,
		meshKey:      cfg.MeshKey,
		nodeID:       cfg.NodeID,
		payload:      cfg.Payload,
		interval:     cfg.Interval,
		dutyCycleMax: maxTXTime,
		txWindow: &DutyCycleWindow{
			maxTXTime:   maxTXTime,
			txLog:       make([]time.Time, 0, 100),
			txDurations: make([]time.Duration, 0, 100),
		},
	}, nil
}

// Run starts the BEACON transmission loop.
func (bt *BeaconTransmitter) Run(ctx context.Context) error {
	slog.Info("BEACON transmitter starting",
		"interval", bt.interval,
		"duty_cycle_max", bt.dutyCycleMax)

	ticker := time.NewTicker(bt.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("BEACON transmitter stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			if err := bt.transmitBeacon(ctx); err != nil {
				slog.Warn("BEACON transmission failed", "error", err)
			}
		}
	}
}

// transmitBeacon sends a single BEACON frame with duty-cycle enforcement.
func (bt *BeaconTransmitter) transmitBeacon(ctx context.Context) error {
	// Check duty-cycle compliance
	if !bt.txWindow.CanTransmit() {
		bt.adaptiveBackoff()
		return fmt.Errorf("duty-cycle limit reached, deferring transmission")
	}

	// Generate nonce
	nonceSlice, err := bt.ng.Generate()
	if err != nil {
		return fmt.Errorf("nonce generation failed: %w", err)
	}

	// Convert nonce slice to array
	var nonce [12]byte
	copy(nonce[:], nonceSlice)

	// Update BEACON payload timestamp to current time
	bt.payload.Timestamp = uint32(time.Now().Unix())

	// Marshal BEACON payload (plaintext)
	plaintext := MarshalBEACONPayload(bt.payload)

	// Encrypt payload with ChaCha20-Poly1305 AEAD
	ciphertext, err := crypto.Encrypt(bt.meshKey, nonce, plaintext)
	if err != nil {
		return fmt.Errorf("BEACON encryption failed: %w", err)
	}

	// Build frame header
	hdr := &Header{
		FrameType: FrameTypeBEACON,
		Version:   ProtocolVersion,
		NodeID:    bt.nodeID,
		Timestamp: bt.payload.Timestamp,
		FrameSeq:  0, // Will be populated by nonce generator's frame sequence
		Nonce:     nonce,
		HMAC:      [12]byte{}, // Will be computed below
	}

	// Assemble frame (header + encrypted payload)
	headerBytes := MarshalHeader(hdr)
	frameData := append(headerBytes, ciphertext...)

	// Compute HMAC over header + ciphertext (HMAC field must be zeroed first)
	hmac := crypto.ComputeHMAC(bt.meshKey, frameData)
	copy(hdr.HMAC[:], hmac[:])

	// Re-marshal header with HMAC
	headerBytes = MarshalHeader(hdr)
	frameData = append(headerBytes, ciphertext...)

	// Transmit frame
	txStart := time.Now()
	if err := bt.radio.Send(ctx, frameData); err != nil {
		return fmt.Errorf("radio transmission failed: %w", err)
	}
	txDuration := time.Since(txStart)

	// Record transmission for duty-cycle tracking
	bt.txWindow.RecordTX(txStart, txDuration)

	slog.Debug("BEACON transmitted",
		"node_id", bt.nodeID,
		"frame_size", len(frameData),
		"tx_duration_ms", txDuration.Milliseconds(),
		"duty_cycle_remaining", bt.txWindow.RemainingTXTime())

	return nil
}

// adaptiveBackoff increases BEACON interval when duty-cycle limit is reached.
func (bt *BeaconTransmitter) adaptiveBackoff() {
	newInterval := bt.interval * 2
	if newInterval > 10*time.Minute {
		newInterval = 10 * time.Minute
	}

	if newInterval != bt.interval {
		slog.Warn("Duty-cycle limit reached, increasing BEACON interval",
			"old_interval", bt.interval,
			"new_interval", newInterval)
		bt.interval = newInterval
	}
}

// CanTransmit checks if transmission is allowed under duty-cycle constraints.
func (w *DutyCycleWindow) CanTransmit() bool {
	w.pruneOldEntries()
	return w.currentTXTime() < w.maxTXTime
}

// RecordTX records a transmission event for duty-cycle tracking.
func (w *DutyCycleWindow) RecordTX(timestamp time.Time, duration time.Duration) {
	w.txLog = append(w.txLog, timestamp)
	w.txDurations = append(w.txDurations, duration)
	w.pruneOldEntries()
}

// pruneOldEntries removes TX events older than 1 hour.
func (w *DutyCycleWindow) pruneOldEntries() {
	cutoff := time.Now().Add(-1 * time.Hour)
	validIdx := 0

	for i := 0; i < len(w.txLog); i++ {
		if w.txLog[i].After(cutoff) {
			w.txLog[validIdx] = w.txLog[i]
			w.txDurations[validIdx] = w.txDurations[i]
			validIdx++
		}
	}

	w.txLog = w.txLog[:validIdx]
	w.txDurations = w.txDurations[:validIdx]
}

// currentTXTime returns the total TX time in the current 1-hour window.
func (w *DutyCycleWindow) currentTXTime() time.Duration {
	var total time.Duration
	for _, d := range w.txDurations {
		total += d
	}
	return total
}

// RemainingTXTime returns the remaining TX time before duty-cycle limit.
func (w *DutyCycleWindow) RemainingTXTime() time.Duration {
	remaining := w.maxTXTime - w.currentTXTime()
	if remaining < 0 {
		return 0
	}
	return remaining
}
