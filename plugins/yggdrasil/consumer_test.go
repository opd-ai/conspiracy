package yggdrasil

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/hint"
)

// mockAdminAPI creates a mock Yggdrasil admin API server.
type mockAdminAPI struct {
	server       *httptest.Server
	addedPeers   []string
	removedPeers []string
	shouldFail   bool
}

func newMockAdminAPI() *mockAdminAPI {
	mock := &mockAdminAPI{
		addedPeers:   make([]string, 0),
		removedPeers: make([]string, 0),
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mock.shouldFail {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}

		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		method := req["request"].(string)
		args := req["args"].(map[string]interface{})

		switch method {
		case "addPeer":
			uri := args["uri"].(string)
			mock.addedPeers = append(mock.addedPeers, uri)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
			})
		case "removePeer":
			uri := args["uri"].(string)
			mock.removedPeers = append(mock.removedPeers, uri)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "success",
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "unknown method",
			})
		}
	})

	mock.server = httptest.NewServer(handler)
	return mock
}

func (m *mockAdminAPI) Close() {
	m.server.Close()
}

func (m *mockAdminAPI) Address() string {
	return m.server.Listener.Addr().String()
}

func TestNewConsumer(t *testing.T) {
	cfg := AdminAPIConfig{
		Address: "127.0.0.1:9001",
		Timeout: 10 * time.Second,
	}

	c := NewConsumer(cfg)
	if c == nil {
		t.Fatal("NewConsumer returned nil")
	}

	if c.config.Address != cfg.Address {
		t.Errorf("expected address %s, got %s", cfg.Address, c.config.Address)
	}

	if c.config.Timeout != cfg.Timeout {
		t.Errorf("expected timeout %v, got %v", cfg.Timeout, c.config.Timeout)
	}

	if c.peerCache == nil {
		t.Error("peerCache not initialized")
	}
}

func TestNewConsumer_DefaultTimeout(t *testing.T) {
	cfg := AdminAPIConfig{
		Address: "127.0.0.1:9001",
	}

	c := NewConsumer(cfg)
	if c.config.Timeout != 5*time.Second {
		t.Errorf("expected default timeout 5s, got %v", c.config.Timeout)
	}
}

func TestConsume_PeerDiscovered(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")
	h := hint.Hint{
		Type:      hint.PeerDiscovered,
		NodeID:    12345,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}

	err := c.Consume(h)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	if len(mock.addedPeers) != 1 {
		t.Fatalf("expected 1 added peer, got %d", len(mock.addedPeers))
	}

	expectedAddr := "192.168.1.100:9001"
	if mock.addedPeers[0] != expectedAddr {
		t.Errorf("expected peer %s, got %s", expectedAddr, mock.addedPeers[0])
	}
}

func TestConsume_RouteAdded(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	testAddr, _ := net.ResolveUDPAddr("udp", "192.168.1.200:9001")
	h := hint.Hint{
		Type:      hint.RouteAdded,
		NodeID:    67890,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}

	err := c.Consume(h)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	if len(mock.addedPeers) != 1 {
		t.Fatalf("expected 1 added peer, got %d", len(mock.addedPeers))
	}

	expectedAddr := "192.168.1.200:9001"
	if mock.addedPeers[0] != expectedAddr {
		t.Errorf("expected peer %s, got %s", expectedAddr, mock.addedPeers[0])
	}
}

func TestConsume_RouteRemoved(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")

	// First add the peer
	addHint := hint.Hint{
		Type:      hint.PeerDiscovered,
		NodeID:    12345,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}
	c.Consume(addHint)

	// Then remove it
	removeHint := hint.Hint{
		Type:      hint.RouteRemoved,
		NodeID:    12345,
		Timestamp: time.Now(),
	}

	err := c.Consume(removeHint)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	if len(mock.removedPeers) != 1 {
		t.Fatalf("expected 1 removed peer, got %d", len(mock.removedPeers))
	}

	expectedAddr := "192.168.1.100:9001"
	if mock.removedPeers[0] != expectedAddr {
		t.Errorf("expected removed peer %s, got %s", expectedAddr, mock.removedPeers[0])
	}
}

func TestConsume_DuplicatePeer(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")
	h := hint.Hint{
		Type:      hint.PeerDiscovered,
		NodeID:    12345,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}

	// Add peer twice
	c.Consume(h)
	c.Consume(h)

	// Should only add once
	if len(mock.addedPeers) != 1 {
		t.Errorf("expected 1 added peer (duplicate should be skipped), got %d", len(mock.addedPeers))
	}
}

func TestConsume_AdminAPIError(t *testing.T) {
	mock := newMockAdminAPI()
	mock.shouldFail = true
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")
	h := hint.Hint{
		Type:      hint.PeerDiscovered,
		NodeID:    12345,
		Addr:      testAddr,
		Timestamp: time.Now(),
	}

	err := c.Consume(h)
	if err == nil {
		t.Error("expected error when admin API fails, got nil")
	}
}

func TestExtractYggdrasilAddress_TCP(t *testing.T) {
	c := NewConsumer(AdminAPIConfig{Address: "127.0.0.1:9001"})

	addr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")
	result, err := c.extractYggdrasilAddress(addr)
	if err != nil {
		t.Fatalf("extractYggdrasilAddress failed: %v", err)
	}

	expected := "192.168.1.100:9001"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestExtractYggdrasilAddress_UDP(t *testing.T) {
	c := NewConsumer(AdminAPIConfig{Address: "127.0.0.1:9001"})

	addr, _ := net.ResolveUDPAddr("udp", "192.168.1.200:9001")
	result, err := c.extractYggdrasilAddress(addr)
	if err != nil {
		t.Fatalf("extractYggdrasilAddress failed: %v", err)
	}

	expected := "192.168.1.200:9001"
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestExtractYggdrasilAddress_Nil(t *testing.T) {
	c := NewConsumer(AdminAPIConfig{Address: "127.0.0.1:9001"})

	_, err := c.extractYggdrasilAddress(nil)
	if err == nil {
		t.Error("expected error for nil address, got nil")
	}
}

func TestPingAdminAPI_Success(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	ctx := context.Background()
	err := c.PingAdminAPI(ctx)
	if err != nil {
		t.Errorf("PingAdminAPI failed: %v", err)
	}
}

func TestPingAdminAPI_Unreachable(t *testing.T) {
	c := NewConsumer(AdminAPIConfig{
		Address: "127.0.0.1:99999",
		Timeout: 100 * time.Millisecond,
	})

	ctx := context.Background()
	err := c.PingAdminAPI(ctx)
	if err == nil {
		t.Error("expected error for unreachable API, got nil")
	}
}

func TestStats(t *testing.T) {
	mock := newMockAdminAPI()
	defer mock.Close()

	c := NewConsumer(AdminAPIConfig{
		Address: mock.Address(),
		Timeout: 5 * time.Second,
	})

	// Add some peers
	for i := 0; i < 3; i++ {
		testAddr, _ := net.ResolveTCPAddr("tcp", "192.168.1.100:9001")
		h := hint.Hint{
			Type:      hint.PeerDiscovered,
			NodeID:    uint32(i),
			Addr:      testAddr,
			Timestamp: time.Now(),
		}
		c.Consume(h)
	}

	stats := c.Stats()
	if cachedPeers, ok := stats["cached_peers"].(int); !ok || cachedPeers != 3 {
		t.Errorf("expected 3 cached peers, got %v", stats["cached_peers"])
	}
}

func TestClose(t *testing.T) {
	c := NewConsumer(AdminAPIConfig{
		Address: "127.0.0.1:9001",
		Timeout: 5 * time.Second,
	})

	err := c.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
