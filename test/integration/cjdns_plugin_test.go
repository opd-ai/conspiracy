//go:build integration
// +build integration

// Package integration contains integration tests for plugin functionality.
// To run: go test -v -tags=integration ./test/integration -run TestCjdns
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/hint"
	"github.com/opd-ai/conspiracy/plugins/cjdns"
)

// TestCjdnsPluginIntegration validates the cjdns plugin's integration with HintBus.
// Tests:
// 1. HintBus publishes RouteAdded hints
// 2. cjdns plugin receives hints and calls admin API
// 3. Peer addition via IpTunnel_allowConnection
// 4. Latency from hint publication to peer addition is <500ms
func TestCjdnsPluginIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Mock cjdns admin API server
	var mu sync.Mutex
	addedPeers := make(map[string]bool)
	removedPeers := make(map[string]bool)
	callTimes := make(map[string]time.Time)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		method, _ := req["q"].(string)
		args, _ := req["args"].(map[string]interface{})

		mu.Lock()
		defer mu.Unlock()

		switch method {
		case "ping":
			// Respond to ping
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"q": "pong",
			})
			return

		case "UDPInterface_beginConnection":
			addr, _ := args["address"].(string)
			addedPeers[addr] = true
			callTimes["addPeer_"+addr] = time.Now()
			t.Logf("Mock API: UDPInterface_beginConnection called for %s", addr)

		case "IpTunnel_removeConnection":
			addr, _ := args["address"].(string)
			removedPeers[addr] = true
			callTimes["removePeer_"+addr] = time.Now()
			t.Logf("Mock API: IpTunnel_removeConnection called for %s", addr)

		case "IpTunnel_listConnections":
			// Return list of added peers
			peers := make([]map[string]interface{}, 0, len(addedPeers))
			for peer := range addedPeers {
				if !removedPeers[peer] {
					peers = append(peers, map[string]interface{}{
						"address": peer,
					})
				}
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"connections": peers,
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer mockServer.Close()

	// Extract host:port from mock server URL
	serverAddr := mockServer.URL[len("http://"):]

	// Create cjdns consumer
	cfg := cjdns.AdminAPIConfig{
		Address:  serverAddr,
		Password: "test-password",
		Timeout:  3 * time.Second,
	}
	consumer := cjdns.NewConsumer(cfg)
	defer consumer.Close()

	// Test 1: Verify admin API connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := consumer.PingAdminAPI(ctx); err != nil {
		t.Fatalf("Failed to ping admin API: %v", err)
	}
	t.Log("✓ Admin API connectivity verified")

	// Create HintBus and register consumer
	bus := hint.NewBus()
	if err := bus.RegisterConsumer("cjdns", consumer, 128); err != nil {
		t.Fatalf("Failed to register consumer: %v", err)
	}

	// Start consumer
	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := bus.Run(consumerCtx); err != nil && err != context.Canceled {
			t.Errorf("Consumer error: %v", err)
		}
	}()

	// Allow consumer to start
	time.Sleep(100 * time.Millisecond)

	// Test 2: Publish RouteAdded hint
	testNodeID := uint32(0xABCDEF12)
	testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.100.50:8080")
	publishTime := time.Now()

	routeHint := hint.Hint{
		Type:      hint.RouteAdded,
		NodeID:    testNodeID,
		Addr:      testAddr,
		Metric:    10,
		Timestamp: publishTime,
	}

	if err := bus.Publish(routeHint); err != nil {
		t.Fatalf("Failed to publish hint: %v", err)
	}
	t.Log("✓ RouteAdded hint published")

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// Test 3: Verify peer was added via admin API
	mu.Lock()
	expectedURI := testAddr.String()
	if !addedPeers[expectedURI] {
		mu.Unlock()
		t.Fatalf("Peer %s was not added via admin API. Added peers: %v", expectedURI, addedPeers)
	}
	addTime, hasTime := callTimes["addPeer_"+expectedURI]
	mu.Unlock()

	t.Logf("✓ Peer %s added via admin API", expectedURI)

	// Test 4: Verify latency is <500ms
	if hasTime {
		latency := addTime.Sub(publishTime)
		t.Logf("Latency from hint publication to peer addition: %v", latency)
		if latency > 500*time.Millisecond {
			t.Errorf("Latency %v exceeds 500ms target", latency)
		} else {
			t.Logf("✓ Latency %v is within 500ms target", latency)
		}
	}

	// Test 5: Publish PeerDiscovered hint (should also trigger addPeer)
	testNodeID2 := uint32(0xFEDCBA98)
	testAddr2, _ := net.ResolveUDPAddr("udp", "192.168.100.51:8081")

	peerHint := hint.Hint{
		Type:      hint.PeerDiscovered,
		NodeID:    testNodeID2,
		Addr:      testAddr2,
		Metric:    5,
		Timestamp: time.Now(),
	}

	if err := bus.Publish(peerHint); err != nil {
		t.Fatalf("Failed to publish peer hint: %v", err)
	}
	t.Log("✓ PeerDiscovered hint published")

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	expectedURI2 := testAddr2.String()
	if !addedPeers[expectedURI2] {
		mu.Unlock()
		t.Fatalf("Peer %s was not added via admin API", expectedURI2)
	}
	mu.Unlock()
	t.Logf("✓ Peer %s added via admin API (from PeerDiscovered)", expectedURI2)

	// Test 6: Publish RouteRemoved hint
	// Note: cjdns handles peer removal via timeout, so this won't trigger actual API call
	removeHint := hint.Hint{
		Type:      hint.RouteRemoved,
		NodeID:    testNodeID,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}

	if err := bus.Publish(removeHint); err != nil {
		t.Fatalf("Failed to publish remove hint: %v", err)
	}
	t.Log("✓ RouteRemoved hint published")

	// Wait for processing
	time.Sleep(300 * time.Millisecond)

	// For cjdns, removal is handled via timeout, so we don't check removedPeers
	// but we verify the peer is removed from cache
	stats := consumer.Stats()
	cachedPeers := stats["cached_peers"].(int)
	// Should have 1 peer remaining (testNodeID2, since testNodeID was removed from cache)
	if cachedPeers != 1 {
		t.Errorf("Expected 1 cached peer, got %d", cachedPeers)
	} else {
		t.Logf("✓ Consumer stats show %d cached peer(s)", cachedPeers)
	}

	// Cleanup
	consumerCancel()
	wg.Wait()
	bus.Close()

	t.Log("✓ cjdns plugin integration test completed successfully")
}

// TestCjdnsPluginReconnection validates that the plugin handles admin API failures gracefully.
func TestCjdnsPluginReconnection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Mock server that fails initially then succeeds
	failureCount := 0
	var mu sync.Mutex

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count := failureCount
		failureCount++
		mu.Unlock()

		// Fail first 2 requests
		if count < 2 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}

		// Succeed afterwards
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}))
	defer mockServer.Close()

	serverAddr := mockServer.URL[len("http://"):]

	cfg := cjdns.AdminAPIConfig{
		Address:  serverAddr,
		Password: "test-password",
		Timeout:  1 * time.Second,
	}
	consumer := cjdns.NewConsumer(cfg)
	defer consumer.Close()

	// Create HintBus
	bus := hint.NewBus()
	if err := bus.RegisterConsumer("cjdns", consumer, 128); err != nil {
		t.Fatalf("Failed to register consumer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Publish hints - first two should fail, third should succeed
	for i := 0; i < 3; i++ {
		testAddr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("192.168.100.%d:8080", 50+i))
		h := hint.Hint{
			Type:      hint.RouteAdded,
			NodeID:    uint32(0xABCD0000 + i),
			Addr:      testAddr,
			Metric:    10,
			Timestamp: time.Now(),
		}

		if err := bus.Publish(h); err != nil {
			t.Fatalf("Failed to publish hint %d: %v", i, err)
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Check failure count
	mu.Lock()
	count := failureCount
	mu.Unlock()

	if count < 3 {
		t.Errorf("Expected at least 3 admin API calls, got %d", count)
	} else {
		t.Logf("✓ Plugin handled API failures gracefully (%d calls)", count)
	}

	cancel()
	wg.Wait()
	bus.Close()

	t.Log("✓ cjdns plugin reconnection test completed")
}

// TestCjdnsPluginHighLoad validates plugin behavior under high hint publication rate.
func TestCjdnsPluginHighLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	var mu sync.Mutex
	receivedCount := 0

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCount++
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
	}))
	defer mockServer.Close()

	serverAddr := mockServer.URL[len("http://"):]

	cfg := cjdns.AdminAPIConfig{
		Address:  serverAddr,
		Password: "test-password",
		Timeout:  1 * time.Second,
	}
	consumer := cjdns.NewConsumer(cfg)
	defer consumer.Close()

	bus := hint.NewBus()
	if err := bus.RegisterConsumer("cjdns", consumer, 128); err != nil {
		t.Fatalf("Failed to register consumer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		bus.Run(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Publish 100 hints rapidly
	const hintCount = 100
	startTime := time.Now()

	for i := 0; i < hintCount; i++ {
		testAddr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("192.168.%d.%d:8080", 100+(i/254), (i%254)+1))
		h := hint.Hint{
			Type:      hint.RouteAdded,
			NodeID:    uint32(0xA0000000 + i),
			Addr:      testAddr,
			Metric:    10,
			Timestamp: time.Now(),
		}

		if err := bus.Publish(h); err != nil {
			t.Fatalf("Failed to publish hint %d: %v", i, err)
		}
	}

	publishDuration := time.Since(startTime)
	t.Logf("Published %d hints in %v (%v per hint)", hintCount, publishDuration, publishDuration/hintCount)

	// Wait for processing
	time.Sleep(2 * time.Second)

	mu.Lock()
	count := receivedCount
	mu.Unlock()

	// Should process all hints (or most due to deduplication)
	if count < hintCount/2 {
		t.Errorf("Expected at least %d API calls, got %d", hintCount/2, count)
	} else {
		t.Logf("✓ Processed %d/%d hints under high load", count, hintCount)
	}

	cancel()
	wg.Wait()
	bus.Close()

	t.Log("✓ cjdns plugin high load test completed")
}
