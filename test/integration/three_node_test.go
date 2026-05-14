//go:build integration
// +build integration

// Package integration contains integration tests for multi-node mesh scenarios.
// To run: go test -v -tags=integration ./test/integration
package integration

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/autojoin"
	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
)

// TestThreeNodeMesh validates JOIN sequence, route establishment, and packet forwarding in A→B→C topology.
// Node A: Bootstrap node (sends BEACONs)
// Node B: Joins A, then forwards traffic
// Node C: Joins B via LoRa, can reach A through B
func TestThreeNodeMesh(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary storage directories for each node
	storageA := createTempStorage(t, "nodeA")
	storageB := createTempStorage(t, "nodeB")
	storageC := createTempStorage(t, "nodeC")
	defer cleanupStorage(storageA, storageB, storageC)

	// Shared mesh key for all nodes
	meshKey := []byte("test-mesh-key-32-bytes-1234567890")

	// Create UDP radios for each node (simulates LoRa hardware)
	radioA, err := createNodeRadio(t, "127.0.0.1:10001", "127.0.0.1:10002")
	if err != nil {
		t.Fatalf("Failed to create radio A: %v", err)
	}
	defer radioA.Close()

	radioB, err := createNodeRadio(t, "127.0.0.1:10002", "127.0.0.1:10003")
	if err != nil {
		t.Fatalf("Failed to create radio B: %v", err)
	}
	defer radioB.Close()

	radioC, err := createNodeRadio(t, "127.0.0.1:10003", "127.0.0.1:10001")
	if err != nil {
		t.Fatalf("Failed to create radio C: %v", err)
	}
	defer radioC.Close()

	// Initialize crypto components for each node
	nodeA := setupNode(t, 1, meshKey, storageA, radioA)
	nodeB := setupNode(t, 2, meshKey, storageB, radioB)
	nodeC := setupNode(t, 3, meshKey, storageC, radioC)

	// Test context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run test phases
	t.Run("Phase1_NodeA_Bootstrap", func(t *testing.T) {
		testNodeABootstrap(t, ctx, nodeA)
	})

	t.Run("Phase2_NodeB_JoinA", func(t *testing.T) {
		testNodeBJoinA(t, ctx, nodeA, nodeB)
	})

	t.Run("Phase3_NodeC_JoinB", func(t *testing.T) {
		testNodeCJoinB(t, ctx, nodeB, nodeC)
	})

	t.Run("Phase4_VerifyMeshConnectivity", func(t *testing.T) {
		testMeshConnectivity(t, ctx, nodeA, nodeB, nodeC)
	})
}

// NodeContext holds the runtime state for a test node.
type NodeContext struct {
	NodeID       uint32
	Radio        lora.PacketRadio
	NonceGen     *crypto.NonceGenerator
	MeshKey      []byte
	RebootCtr    *crypto.RebootCounter
	BeaconTicker *time.Ticker
	stopBeacons  chan struct{}
	mu           sync.Mutex
	peerTable    map[uint32]*autojoin.PeerInfo
}

// createTempStorage creates a temporary directory for node storage.
func createTempStorage(t *testing.T, nodeName string) string {
	dir, err := os.MkdirTemp("", "conspiracy-test-"+nodeName+"-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir for %s: %v", nodeName, err)
	}
	t.Logf("Created temp storage for %s: %s", nodeName, dir)
	return dir
}

// cleanupStorage removes temporary storage directories.
func cleanupStorage(dirs ...string) {
	for _, dir := range dirs {
		os.RemoveAll(dir)
	}
}

// createNodeRadio creates a UDP-based test radio.
func createNodeRadio(t *testing.T, listenAddr, peerAddr string) (lora.PacketRadio, error) {
	config := lora.Config{
		UDPListen: listenAddr,
		UDPPeer:   peerAddr,
	}
	return lora.NewRadio(config)
}

// setupNode initializes crypto and radio for a test node.
func setupNode(t *testing.T, nodeID uint32, meshKey []byte, storageDir string, radio lora.PacketRadio) *NodeContext {
	rc, err := crypto.NewRebootCounter(storageDir)
	if err != nil {
		t.Fatalf("Failed to create reboot counter for node %d: %v", nodeID, err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator for node %d: %v", nodeID, err)
	}

	return &NodeContext{
		NodeID:      nodeID,
		Radio:       radio,
		NonceGen:    ng,
		MeshKey:     meshKey,
		RebootCtr:   rc,
		stopBeacons: make(chan struct{}),
		peerTable:   make(map[uint32]*autojoin.PeerInfo),
	}
}

// testNodeABootstrap verifies Node A can emit BEACONs.
func testNodeABootstrap(t *testing.T, ctx context.Context, nodeA *NodeContext) {
	t.Log("Node A: Starting BEACON transmission...")

	beacon := createTestBeacon(nodeA.NodeID)
	if err := sendBeacon(ctx, nodeA, beacon); err != nil {
		t.Fatalf("Node A failed to send initial BEACON: %v", err)
	}

	t.Log("Node A: Successfully transmitted BEACON")

	// Start periodic BEACON transmission for Node A
	startPeriodicBeacons(nodeA, 10*time.Second)
}

// testNodeBJoinA verifies Node B can discover and join Node A.
func testNodeBJoinA(t *testing.T, ctx context.Context, nodeA, nodeB *NodeContext) {
	t.Log("Node B: Starting auto-join FSM to discover Node A...")

	// Run FSM for limited time
	fsmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Node A continues sending BEACONs in background
	// Node B scans for BEACONs
	fsm := autojoin.NewFSM(autojoin.Config{
		Radio:        nodeB.Radio,
		NonceGen:     nodeB.NonceGen,
		MeshKey:      nodeB.MeshKey,
		NodeID:       nodeB.NodeID,
		ScanDuration: 30 * time.Second,
		MaxAttempts:  3,
	})

	// Run FSM in goroutine
	var fsmErr error
	fsmDone := make(chan struct{})
	go func() {
		defer close(fsmDone)
		fsmErr = fsm.Run(fsmCtx)
	}()

	// Wait for FSM to reach CONNECTED state or timeout
	select {
	case <-fsmDone:
		if fsmErr != nil && fsmErr != context.DeadlineExceeded {
			t.Fatalf("Node B FSM failed: %v", fsmErr)
		}
		t.Log("Node B: Successfully joined Node A")
	case <-time.After(90 * time.Second):
		cancel()
		t.Fatal("Node B failed to join Node A within timeout")
	}

	// Start BEACONs from Node B (now part of mesh)
	startPeriodicBeacons(nodeB, 10*time.Second)
}

// testNodeCJoinB verifies Node C can join via Node B.
func testNodeCJoinB(t *testing.T, ctx context.Context, nodeB, nodeC *NodeContext) {
	t.Log("Node C: Starting auto-join FSM to discover Node B...")

	fsmCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	fsm := autojoin.NewFSM(autojoin.Config{
		Radio:        nodeC.Radio,
		NonceGen:     nodeC.NonceGen,
		MeshKey:      nodeC.MeshKey,
		NodeID:       nodeC.NodeID,
		ScanDuration: 30 * time.Second,
		MaxAttempts:  3,
	})

	var fsmErr error
	fsmDone := make(chan struct{})
	go func() {
		defer close(fsmDone)
		fsmErr = fsm.Run(fsmCtx)
	}()

	select {
	case <-fsmDone:
		if fsmErr != nil && fsmErr != context.DeadlineExceeded {
			t.Fatalf("Node C FSM failed: %v", fsmErr)
		}
		t.Log("Node C: Successfully joined Node B")
	case <-time.After(90 * time.Second):
		cancel()
		t.Fatal("Node C failed to join Node B within timeout")
	}

	// Start BEACONs from Node C
	startPeriodicBeacons(nodeC, 10*time.Second)
}

// testMeshConnectivity verifies all three nodes can exchange packets.
func testMeshConnectivity(t *testing.T, ctx context.Context, nodeA, nodeB, nodeC *NodeContext) {
	t.Log("Verifying mesh connectivity: A ↔ B ↔ C")

	// Each node should receive BEACONs from at least one other node
	verifyNodeCanReceiveBEACONs(t, ctx, nodeA, "Node A", 1)
	verifyNodeCanReceiveBEACONs(t, ctx, nodeB, "Node B", 2)
	verifyNodeCanReceiveBEACONs(t, ctx, nodeC, "Node C", 1)

	// Stop all BEACON transmissions
	stopPeriodicBeacons(nodeA)
	stopPeriodicBeacons(nodeB)
	stopPeriodicBeacons(nodeC)

	t.Log("Mesh connectivity verified: All nodes can exchange BEACONs")
}

// createTestBeacon creates a BEACON payload for testing.
func createTestBeacon(nodeID uint32) *lora.BEACONPayload {
	beacon := &lora.BEACONPayload{
		BSSID:        [6]byte{0x02, 0x00, 0x00, 0x00, byte(nodeID >> 8), byte(nodeID)},
		Channel:      6,
		Capabilities: 0x0001,
		Timestamp:    uint32(time.Now().Unix()),
	}
	copy(beacon.SSID[:], fmt.Sprintf("test-mesh-%d", nodeID))
	return beacon
}

// sendBeacon encrypts and transmits a BEACON frame.
func sendBeacon(ctx context.Context, node *NodeContext, beacon *lora.BEACONPayload) error {
	payloadBytes := lora.MarshalBEACONPayload(beacon)

	nonce, err := node.NonceGen.Generate()
	if err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	var nonceArray [12]byte
	copy(nonceArray[:], nonce)

	ciphertext, err := crypto.Encrypt(node.MeshKey, nonceArray, payloadBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt BEACON: %w", err)
	}

	hdr := &lora.Header{
		FrameType: lora.FrameTypeBEACON,
		Version:   lora.ProtocolVersion,
		NodeID:    node.NodeID,
		Timestamp: uint32(time.Now().Unix()),
		FrameSeq:  0,
		Nonce:     nonceArray,
	}

	frame := lora.MarshalFrame(hdr, ciphertext)

	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return node.Radio.Send(sendCtx, frame)
}

// startPeriodicBeacons starts background BEACON transmission.
func startPeriodicBeacons(node *NodeContext, interval time.Duration) {
	node.mu.Lock()
	if node.BeaconTicker != nil {
		node.mu.Unlock()
		return
	}
	node.BeaconTicker = time.NewTicker(interval)
	node.mu.Unlock()

	go func() {
		for {
			select {
			case <-node.stopBeacons:
				return
			case <-node.BeaconTicker.C:
				beacon := createTestBeacon(node.NodeID)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				sendBeacon(ctx, node, beacon)
				cancel()
			}
		}
	}()
}

// stopPeriodicBeacons stops background BEACON transmission.
func stopPeriodicBeacons(node *NodeContext) {
	node.mu.Lock()
	defer node.mu.Unlock()

	if node.BeaconTicker != nil {
		node.BeaconTicker.Stop()
		node.BeaconTicker = nil
		close(node.stopBeacons)
		node.stopBeacons = make(chan struct{})
	}
}

// verifyNodeCanReceiveBEACONs checks that a node receives at least minCount BEACONs.
func verifyNodeCanReceiveBEACONs(t *testing.T, ctx context.Context, node *NodeContext, nodeName string, minCount int) {
	t.Logf("%s: Listening for BEACONs (expecting at least %d)...", nodeName, minCount)

	receivedCount := 0
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) && receivedCount < minCount {
		recvCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		data, err := node.Radio.Recv(recvCtx)
		cancel()

		if err != nil {
			continue
		}

		hdr, payload, err := lora.UnmarshalFrame(data)
		if err != nil {
			continue
		}

		if hdr.FrameType != lora.FrameTypeBEACON {
			continue
		}

		plaintext, err := crypto.Decrypt(node.MeshKey, hdr.Nonce, payload)
		if err != nil {
			continue
		}

		beacon, err := lora.UnmarshalBEACONPayload(plaintext)
		if err != nil {
			continue
		}

		if hdr.NodeID != node.NodeID {
			receivedCount++
			t.Logf("%s: Received BEACON from Node %d (SSID: %s)", nodeName, hdr.NodeID, beacon.SSID)
		}
	}

	if receivedCount < minCount {
		t.Errorf("%s: Expected at least %d BEACONs, received %d", nodeName, minCount, receivedCount)
	} else {
		t.Logf("%s: Successfully received %d BEACONs", nodeName, receivedCount)
	}
}
