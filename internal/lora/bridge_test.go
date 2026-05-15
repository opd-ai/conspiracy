package lora

import (
	"context"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/crypto"
)

func TestNewBridgeNode(t *testing.T) {
	meshKey := make([]byte, 32)
	nodeID := uint32(12345)

	rc, err := crypto.NewRebootCounter(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio := &bridgeMockRadio{freq: 868.1}
	zm := NewZoneManager(EU868Zone, nodeID)

	cfg := BridgeConfig{
		Radio:       radio,
		NonceGen:    ng,
		MeshKey:     meshKey,
		NodeID:      nodeID,
		ZoneManager: zm,
		Scheduler:   nil,
	}

	bn, err := NewBridgeNode(cfg)
	if err != nil {
		t.Fatalf("NewBridgeNode failed: %v", err)
	}

	if bn.primaryFreq != 868.1 {
		t.Errorf("Expected primary freq 868.1, got %.1f", bn.primaryFreq)
	}

	if bn.scanDuration != 20*time.Second {
		t.Errorf("Expected scan duration 20s, got %v", bn.scanDuration)
	}
}

func TestBridgeNode_FrequencyCycling(t *testing.T) {
	meshKey := make([]byte, 32)
	nodeID := uint32(0)

	rc, err := crypto.NewRebootCounter(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio := &bridgeMockRadio{freq: 868.1}
	zm := NewZoneManager(EU868Zone, nodeID)

	for i := uint32(0); i < 50; i++ {
		zm.UpdatePeerFrequency(i, 868.1)
	}
	for i := uint32(50); i < 60; i++ {
		zm.UpdatePeerFrequency(i, 868.3)
	}

	if !zm.IsBridgeNode() {
		t.Fatal("Zone manager should be in bridge mode")
	}

	cfg := BridgeConfig{
		Radio:        radio,
		NonceGen:     ng,
		MeshKey:      meshKey,
		NodeID:       nodeID,
		ZoneManager:  zm,
		Scheduler:    nil,
		ScanDuration: 100 * time.Millisecond,
	}

	bn, err := NewBridgeNode(cfg)
	if err != nil {
		t.Fatalf("NewBridgeNode failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	go bn.Run(ctx)

	time.Sleep(300 * time.Millisecond)

	bn.bridgeFreqsMu.RLock()
	freqLen := len(bn.bridgeFreqs)
	bn.bridgeFreqsMu.RUnlock()

	if freqLen == 0 {
		t.Error("Bridge frequencies should be populated")
	}
}

func TestBloomFilter(t *testing.T) {
	bf := NewBloomFilter(1000, 0.01)

	bf.Add(12345)
	if !bf.Contains(12345) {
		t.Error("Bloom filter should contain added element")
	}

	if bf.Contains(99999) {
		t.Error("Bloom filter should not contain element that was not added")
	}

	for i := uint64(0); i < 100; i++ {
		bf.Add(i)
	}

	falsePositives := 0
	for i := uint64(100); i < 1000; i++ {
		if bf.Contains(i) {
			falsePositives++
		}
	}

	fpRate := float64(falsePositives) / 900.0
	if fpRate > 0.05 {
		t.Errorf("False positive rate too high: %.2f%% (expected <5%%)", fpRate*100)
	}
}

func TestBridgeNode_FrameIDComputation(t *testing.T) {
	meshKey := make([]byte, 32)
	nodeID := uint32(12345)

	rc, err := crypto.NewRebootCounter(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create reboot counter: %v", err)
	}

	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc)
	if err != nil {
		t.Fatalf("Failed to create nonce generator: %v", err)
	}

	radio := &bridgeMockRadio{freq: 868.1}
	zm := NewZoneManager(EU868Zone, nodeID)

	cfg := BridgeConfig{
		Radio:       radio,
		NonceGen:    ng,
		MeshKey:     meshKey,
		NodeID:      nodeID,
		ZoneManager: zm,
		Scheduler:   nil,
	}

	bn, err := NewBridgeNode(cfg)
	if err != nil {
		t.Fatalf("NewBridgeNode failed: %v", err)
	}

	hdr1 := &Header{
		NodeID:    12345,
		FrameSeq:  100,
		Timestamp: 1000000,
	}
	hdr2 := &Header{
		NodeID:    12345,
		FrameSeq:  100,
		Timestamp: 1000000,
	}

	id1 := bn.computeFrameID(hdr1)
	id2 := bn.computeFrameID(hdr2)

	if id1 != id2 {
		t.Error("Same headers should produce same frame ID")
	}

	hdr3 := &Header{
		NodeID:    12345,
		FrameSeq:  101,
		Timestamp: 1000000,
	}

	id3 := bn.computeFrameID(hdr3)
	if id1 == id3 {
		t.Error("Different headers should produce different frame IDs")
	}
}

type bridgeMockRadio struct {
	freq float64
}

func (m *bridgeMockRadio) Send(ctx context.Context, data []byte) error {
	return nil
}

func (m *bridgeMockRadio) Recv(ctx context.Context) ([]byte, error) {
	return nil, context.Canceled
}

func (m *bridgeMockRadio) RSSI() (int8, error) {
	return -80, nil
}

func (m *bridgeMockRadio) SetFrequency(mhz float64) error {
	m.freq = mhz
	return nil
}

func (m *bridgeMockRadio) SetSpreadingFactor(sf int) error {
	return nil
}

func (m *bridgeMockRadio) SetBandwidth(khz int) error {
	return nil
}

func (m *bridgeMockRadio) Close() error {
	return nil
}
