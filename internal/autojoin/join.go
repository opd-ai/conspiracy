// Package autojoin implements the JOIN_REQ/ACK state machine for automatic mesh discovery.
package autojoin

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
)

// State represents the current state of the auto-join FSM
type State int

const (
	StateINIT State = iota
	StateSCANNING
	StateJOINING
	StateCONNECTED
	StateFAILED
)

// String returns a string representation of the state
func (s State) String() string {
	switch s {
	case StateINIT:
		return "INIT"
	case StateSCANNING:
		return "SCANNING"
	case StateJOINING:
		return "JOINING"
	case StateCONNECTED:
		return "CONNECTED"
	case StateFAILED:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// PeerInfo stores discovered peer metadata during SCANNING state
type PeerInfo struct {
	NodeID       uint32
	RSSI         int8
	SSID         string
	BSSID        [6]byte
	Channel      uint8
	Capabilities uint16
	Timestamp    uint32
}

// FSM represents the auto-join finite state machine
type FSM struct {
	state        State
	radio        lora.PacketRadio
	ng           *crypto.NonceGenerator
	meshKey      []byte
	nodeID       uint32
	scannedPeers []PeerInfo
	joinAttempts int
	maxAttempts  int
	scanDuration time.Duration
}

// Config holds configuration for the auto-join FSM
type Config struct {
	Radio        lora.PacketRadio
	NonceGen     *crypto.NonceGenerator
	MeshKey      []byte
	NodeID       uint32
	ScanDuration time.Duration // Duration to scan for BEACONs (default: 30s)
	MaxAttempts  int           // Max JOIN_REQ attempts (default: 3)
}

// NewFSM creates a new auto-join finite state machine
func NewFSM(cfg Config) *FSM {
	scanDuration := cfg.ScanDuration
	if scanDuration == 0 {
		scanDuration = 30 * time.Second
	}

	maxAttempts := cfg.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}

	return &FSM{
		state:        StateINIT,
		radio:        cfg.Radio,
		ng:           cfg.NonceGen,
		meshKey:      cfg.MeshKey,
		nodeID:       cfg.NodeID,
		scanDuration: scanDuration,
		maxAttempts:  maxAttempts,
	}
}

// Run executes the auto-join state machine
func (fsm *FSM) Run(ctx context.Context) error {
	slog.Info("Auto-join FSM starting", "state", fsm.state.String())

	for {
		select {
		case <-ctx.Done():
			slog.Info("Auto-join FSM stopped", "reason", ctx.Err())
			return ctx.Err()
		default:
			if err := fsm.step(ctx); err != nil {
				return fmt.Errorf("FSM step failed: %w", err)
			}
		}
	}
}

// step executes one iteration of the FSM
func (fsm *FSM) step(ctx context.Context) error {
	switch fsm.state {
	case StateINIT:
		return fsm.handleInit(ctx)
	case StateSCANNING:
		return fsm.handleScanning(ctx)
	case StateJOINING:
		return fsm.handleJoining(ctx)
	case StateCONNECTED:
		return fsm.handleConnected(ctx)
	case StateFAILED:
		return fsm.handleFailed(ctx)
	default:
		return fmt.Errorf("unknown state: %d", fsm.state)
	}
}

// handleInit transitions from INIT to SCANNING
func (fsm *FSM) handleInit(ctx context.Context) error {
	slog.Info("FSM: INIT → SCANNING")
	fsm.state = StateSCANNING
	return nil
}

// handleScanning collects BEACONs for the configured scan duration
func (fsm *FSM) handleScanning(ctx context.Context) error {
	slog.Info("Scanning for peers", "duration", fsm.scanDuration)
	fsm.scannedPeers = nil

	deadline := time.Now().Add(fsm.scanDuration)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Receive frame with 1s timeout per iteration
		frameCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		data, err := fsm.radio.Recv(frameCtx)
		cancel()

		if err != nil {
			// Timeout or error - continue scanning
			continue
		}

		// Parse frame
		hdr, payload, err := lora.UnmarshalFrame(data)
		if err != nil {
			slog.Debug("Failed to parse frame", "error", err)
			continue
		}

		// Only process BEACON frames
		if hdr.FrameType != lora.FrameTypeBEACON {
			continue
		}

		// Decrypt BEACON payload
		plaintext, err := crypto.Decrypt(fsm.meshKey, hdr.Nonce, payload)
		if err != nil {
			slog.Debug("BEACON decryption failed (wrong MESH_KEY or tampered frame)", "error", err)
			continue
		}

		// Unmarshal BEACON payload
		beacon, err := lora.UnmarshalBEACONPayload(plaintext)
		if err != nil {
			slog.Debug("Failed to parse BEACON payload", "error", err)
			continue
		}

		// Get RSSI for this frame
		rssi, err := fsm.radio.RSSI()
		if err != nil {
			rssi = -128 // Unknown RSSI
		}

		// Extract SSID (trim null bytes)
		ssid := string(bytes.TrimRight(beacon.SSID[:], "\x00"))

		// Add peer to discovered list
		peer := PeerInfo{
			NodeID:       hdr.NodeID,
			RSSI:         rssi,
			SSID:         ssid,
			BSSID:        beacon.BSSID,
			Channel:      beacon.Channel,
			Capabilities: beacon.Capabilities,
			Timestamp:    hdr.Timestamp,
		}
		fsm.scannedPeers = append(fsm.scannedPeers, peer)

		slog.Debug("BEACON received",
			"node_id", hdr.NodeID,
			"rssi", rssi,
			"ssid", ssid,
			"channel", beacon.Channel)
	}

	if len(fsm.scannedPeers) == 0 {
		slog.Warn("No peers discovered, retrying scan...")
		// Stay in SCANNING state, retry after short delay
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
			return nil
		}
	}

	// Rank peers by RSSI (strongest first)
	sort.Slice(fsm.scannedPeers, func(i, j int) bool {
		return fsm.scannedPeers[i].RSSI > fsm.scannedPeers[j].RSSI
	})

	slog.Info("FSM: SCANNING → JOINING",
		"peer_count", len(fsm.scannedPeers),
		"strongest_peer", fsm.scannedPeers[0].NodeID,
		"rssi", fsm.scannedPeers[0].RSSI)

	fsm.state = StateJOINING
	fsm.joinAttempts = 0
	return nil
}

// handleJoining sends JOIN_REQ and awaits JOIN_ACK
func (fsm *FSM) handleJoining(ctx context.Context) error {
	if len(fsm.scannedPeers) == 0 {
		slog.Error("No peers available for joining")
		fsm.state = StateFAILED
		return nil
	}

	peer := fsm.scannedPeers[0]
	slog.Info("Sending JOIN_REQ", "peer", peer.NodeID, "attempt", fsm.joinAttempts+1)

	// Send JOIN_REQ
	if err := fsm.sendJoinRequest(ctx, peer); err != nil {
		slog.Error("JOIN_REQ transmission failed", "error", err)
		fsm.joinAttempts++
		if fsm.joinAttempts >= fsm.maxAttempts {
			slog.Warn("FSM: JOINING → FAILED (max attempts reached)")
			fsm.state = StateFAILED
		}
		return nil
	}

	// Wait for JOIN_ACK (30s timeout)
	ack, err := fsm.awaitJoinAck(ctx, 30*time.Second)
	if err != nil {
		slog.Warn("JOIN_ACK timeout", "error", err)
		fsm.joinAttempts++
		if fsm.joinAttempts >= fsm.maxAttempts {
			slog.Warn("FSM: JOINING → FAILED (max attempts reached)")
			fsm.state = StateFAILED
		}
		return nil
	}

	// Extract SSID from JOIN_ACK
	ssid := string(bytes.TrimRight(ack.SSID[:], "\x00"))
	slog.Info("JOIN_ACK received", "ssid", ssid, "channel", ack.Channel, "status", ack.Status)

	if ack.Status != 0 {
		slog.Warn("JOIN_ACK rejected", "status", ack.Status)
		fsm.joinAttempts++
		if fsm.joinAttempts >= fsm.maxAttempts {
			fsm.state = StateFAILED
		}
		return nil
	}

	slog.Info("FSM: JOINING → CONNECTED")
	fsm.state = StateCONNECTED
	// TODO: Trigger 802.11s association (internal/wifi integration)
	return nil
}

// handleConnected monitors peer liveness
func (fsm *FSM) handleConnected(ctx context.Context) error {
	// For MVP: just stay in CONNECTED state
	// In production: monitor BEACON liveness, transition to FAILED after 300s silence
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(60 * time.Second):
		// Periodic health check placeholder
		return nil
	}
}

// handleFailed implements exponential backoff before retry
func (fsm *FSM) handleFailed(ctx context.Context) error {
	backoff := time.Duration(60<<fsm.joinAttempts) * time.Second
	if backoff > 600*time.Second {
		backoff = 600 * time.Second
	}

	slog.Info("FSM: FAILED, retrying after backoff", "backoff_sec", backoff.Seconds())

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		slog.Info("FSM: FAILED → SCANNING (retry)")
		fsm.state = StateSCANNING
		fsm.joinAttempts = 0
		return nil
	}
}

// sendJoinRequest transmits a JOIN_REQ frame
func (fsm *FSM) sendJoinRequest(ctx context.Context, peer PeerInfo) error {
	// Generate nonce for this frame
	nonceBytes, err := fsm.ng.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Convert to fixed-size array
	var nonce [12]byte
	copy(nonce[:], nonceBytes)

	// Create JOIN_REQ payload
	reqPayload := &lora.JOIN_REQPayload{
		NodeID:    fsm.nodeID,
		Nonce:     0, // PoW nonce (deferred to post-MVP)
		POW:       0, // PoW solution (deferred to post-MVP)
		Timestamp: uint32(time.Now().Unix()),
	}

	// Marshal payload
	payloadBytes := lora.MarshalJOIN_REQPayload(reqPayload)

	// Create frame header
	hdr := &lora.Header{
		FrameType: lora.FrameTypeJOIN_REQ,
		Version:   lora.ProtocolVersion,
		NodeID:    fsm.nodeID,
		Timestamp: uint32(time.Now().Unix()),
		FrameSeq:  0, // TODO: track sequence number
		Nonce:     nonce,
	}

	// Assemble frame
	frame := lora.MarshalFrame(hdr, payloadBytes)

	// Transmit
	if err := fsm.radio.Send(ctx, frame); err != nil {
		return fmt.Errorf("failed to send JOIN_REQ: %w", err)
	}

	slog.Debug("JOIN_REQ transmitted", "peer", peer.NodeID, "size", len(frame))
	return nil
}

// awaitJoinAck waits for a JOIN_ACK response
func (fsm *FSM) awaitJoinAck(ctx context.Context, timeout time.Duration) (*lora.JOIN_ACKPayload, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Receive frame with 1s timeout per iteration
		frameCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		data, err := fsm.radio.Recv(frameCtx)
		cancel()

		if err != nil {
			continue // Timeout or error - keep waiting
		}

		// Parse frame
		hdr, payload, err := lora.UnmarshalFrame(data)
		if err != nil {
			slog.Debug("Failed to parse frame", "error", err)
			continue
		}

		// Only process JOIN_ACK frames
		if hdr.FrameType != lora.FrameTypeJOIN_ACK {
			continue
		}

		// Unmarshal JOIN_ACK payload
		ack, err := lora.UnmarshalJOIN_ACKPayload(payload)
		if err != nil {
			slog.Debug("Failed to parse JOIN_ACK payload", "error", err)
			continue
		}

		return ack, nil
	}

	return nil, fmt.Errorf("JOIN_ACK timeout after %v", timeout)
}
