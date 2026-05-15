package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/opd-ai/conspiracy/internal/autojoin"
	"github.com/opd-ai/conspiracy/internal/config"
	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
	"github.com/opd-ai/conspiracy/internal/metrics"
)

type frameHandler struct {
	radio   lora.PacketRadio
	ng      *crypto.NonceGenerator
	meshKey []byte
	nodeID  uint32
	cfg     *config.Config
	fsm     *autojoin.FSM

	// Metrics tracking
	mu              sync.RWMutex
	discoveredPeers map[uint32]bool
	rssiSamples     []float64
	maxRSSISamples  int
}

func newFrameHandler(radio lora.PacketRadio, ng *crypto.NonceGenerator, meshKey []byte, nodeID uint32, cfg *config.Config) *frameHandler {
	fsmCfg := autojoin.Config{
		Radio:         radio,
		NonceGen:      ng,
		MeshKey:       meshKey,
		NodeID:        nodeID,
		MeshInterface: cfg.WiFi.MeshInterface,
	}

	return &frameHandler{
		radio:           radio,
		ng:              ng,
		meshKey:         meshKey,
		nodeID:          nodeID,
		cfg:             cfg,
		fsm:             autojoin.NewFSM(fsmCfg),
		discoveredPeers: make(map[uint32]bool),
		rssiSamples:     make([]float64, 0, 100),
		maxRSSISamples:  100,
	}
}

func (h *frameHandler) processFrame(ctx context.Context, data []byte) error {
	hdr, payload, err := lora.UnmarshalFrame(data)
	if err != nil {
		slog.Debug("Failed to unmarshal frame", "error", err)
		return err
	}

	// Increment RX counter with frame type label
	frameTypeStr := getFrameTypeName(hdr.FrameType)
	metrics.LoraRXTotal.WithLabelValues(frameTypeStr).Inc()

	switch hdr.FrameType {
	case lora.FrameTypeBEACON:
		return h.handleBEACON(ctx, hdr, payload)
	case lora.FrameTypeJOIN_REQ:
		return h.handleJOIN_REQ(ctx, hdr, payload)
	case lora.FrameTypeJOIN_ACK:
		return h.handleJOIN_ACK(ctx, hdr, payload)
	default:
		slog.Debug("Unknown frame type", "type", hdr.FrameType)
		return nil
	}
}

// getFrameTypeName converts frame type to string for metrics labels.
func getFrameTypeName(ft uint8) string {
	switch ft {
	case lora.FrameTypeBEACON:
		return "beacon"
	case lora.FrameTypeJOIN_REQ:
		return "join_req"
	case lora.FrameTypeJOIN_ACK:
		return "join_ack"
	default:
		return "unknown"
	}
}

func (h *frameHandler) handleBEACON(ctx context.Context, hdr *lora.Header, payload []byte) error {
	plaintext, err := crypto.Decrypt(h.meshKey, hdr.Nonce, payload)
	if err != nil {
		slog.Debug("BEACON decryption failed", "error", err, "node_id", hdr.NodeID)
		return err
	}

	beacon, err := lora.UnmarshalBEACONPayload(plaintext)
	if err != nil {
		slog.Debug("Failed to parse BEACON payload", "error", err)
		return err
	}

	ssid := string(bytes.TrimRight(beacon.SSID[:], "\x00"))
	rssi, _ := h.radio.RSSI()

	// Update metrics
	h.updatePeerMetrics(hdr.NodeID)
	h.updateRSSIMetrics(float64(rssi))

	slog.Info("BEACON received",
		"node_id", hdr.NodeID,
		"ssid", ssid,
		"channel", beacon.Channel,
		"rssi", rssi,
		"capabilities", beacon.Capabilities)

	return nil
}

// updatePeerMetrics tracks discovered peers and updates lora_peer_count gauge.
func (h *frameHandler) updatePeerMetrics(peerNodeID uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if peerNodeID != h.nodeID {
		h.discoveredPeers[peerNodeID] = true
		metrics.LoraPeerCount.Set(float64(len(h.discoveredPeers)))
	}
}

// updateRSSIMetrics maintains rolling average of last 100 RSSI samples.
func (h *frameHandler) updateRSSIMetrics(rssi float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Add new sample
	h.rssiSamples = append(h.rssiSamples, rssi)

	// Keep only last maxRSSISamples
	if len(h.rssiSamples) > h.maxRSSISamples {
		h.rssiSamples = h.rssiSamples[len(h.rssiSamples)-h.maxRSSISamples:]
	}

	// Calculate average
	if len(h.rssiSamples) > 0 {
		sum := 0.0
		for _, s := range h.rssiSamples {
			sum += s
		}
		avg := sum / float64(len(h.rssiSamples))
		metrics.LoraRSSIAvg.Set(avg)
	}
}

func (h *frameHandler) handleJOIN_REQ(ctx context.Context, hdr *lora.Header, payload []byte) error {
	req, err := lora.UnmarshalJOIN_REQPayload(payload)
	if err != nil {
		slog.Debug("Failed to parse JOIN_REQ payload", "error", err)
		return err
	}

	slog.Info("JOIN_REQ received",
		"requester_node_id", req.NodeID,
		"timestamp", req.Timestamp)

	return h.sendJOIN_ACK(ctx, req.NodeID)
}

func (h *frameHandler) sendJOIN_ACK(ctx context.Context, targetNodeID uint32) error {
	nonceBytes, err := h.ng.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	var nonce [12]byte
	copy(nonce[:], nonceBytes)

	var ssid [32]byte
	copy(ssid[:], h.cfg.WiFi.SSID)

	ackPayload := &lora.JOIN_ACKPayload{
		SSID:    ssid,
		BSSID:   [6]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
		Channel: uint8(h.cfg.WiFi.Channel),
		Status:  0,
	}

	payloadBytes := lora.MarshalJOIN_ACKPayload(ackPayload)

	hdr := &lora.Header{
		FrameType: lora.FrameTypeJOIN_ACK,
		Version:   lora.ProtocolVersion,
		NodeID:    h.nodeID,
		Timestamp: uint32(time.Now().Unix()),
		FrameSeq:  0,
		Nonce:     nonce,
	}

	frame := lora.MarshalFrame(hdr, payloadBytes)

	if err := h.radio.Send(ctx, frame); err != nil {
		return fmt.Errorf("failed to send JOIN_ACK: %w", err)
	}

	// Increment TX counter with priority and frame type labels
	metrics.LoraTXTotal.WithLabelValues("high", "join_ack").Inc()

	slog.Info("JOIN_ACK sent", "target_node_id", targetNodeID, "ssid", h.cfg.WiFi.SSID)
	return nil
}

func (h *frameHandler) handleJOIN_ACK(ctx context.Context, hdr *lora.Header, payload []byte) error {
	ack, err := lora.UnmarshalJOIN_ACKPayload(payload)
	if err != nil {
		slog.Debug("Failed to parse JOIN_ACK payload", "error", err)
		return err
	}

	ssid := string(bytes.TrimRight(ack.SSID[:], "\x00"))

	slog.Info("JOIN_ACK received",
		"node_id", hdr.NodeID,
		"ssid", ssid,
		"channel", ack.Channel,
		"status", ack.Status)

	return nil
}
