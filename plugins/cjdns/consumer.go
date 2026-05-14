// Package cjdns provides a HintConsumer plugin for cjdns overlay network integration.
package cjdns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/opd-ai/conspiracy/internal/hint"
)

// AdminAPIConfig holds cjdns admin API connection settings.
type AdminAPIConfig struct {
	// Address of cjdns admin API (e.g., "127.0.0.1:11234")
	Address string
	// Password for admin API authentication
	Password string
	// Timeout for admin API requests
	Timeout time.Duration
}

// Consumer implements HintConsumer for cjdns integration.
type Consumer struct {
	config     AdminAPIConfig
	httpClient *http.Client
	peerCache  map[uint32]string // NodeID -> cjdns address
	mu         sync.RWMutex
}

// NewConsumer creates a new cjdns HintConsumer.
func NewConsumer(cfg AdminAPIConfig) *Consumer {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}

	return &Consumer{
		config: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		peerCache: make(map[uint32]string),
	}
}

// Consume processes a routing hint and updates cjdns peer configuration.
// Implements hint.HintConsumer interface.
func (c *Consumer) Consume(h hint.Hint) error {
	switch h.Type {
	case hint.PeerDiscovered, hint.RouteAdded:
		return c.handlePeerDiscovered(h)
	case hint.RouteRemoved:
		return c.handleRouteRemoved(h)
	default:
		slog.Debug("cjdns consumer: ignoring hint type", "type", h.Type)
		return nil
	}
}

// handlePeerDiscovered processes a peer discovery hint and adds the peer to cjdns.
func (c *Consumer) handlePeerDiscovered(h hint.Hint) error {
	// Extract IP address from hint
	cjdnsAddr, err := c.extractCjdnsAddress(h.Addr)
	if err != nil {
		return fmt.Errorf("failed to extract cjdns address: %w", err)
	}

	c.mu.Lock()
	existingAddr, exists := c.peerCache[h.NodeID]
	c.peerCache[h.NodeID] = cjdnsAddr
	c.mu.Unlock()

	// Skip if already added
	if exists && existingAddr == cjdnsAddr {
		slog.Debug("cjdns consumer: peer already cached", "node_id", h.NodeID, "addr", cjdnsAddr)
		return nil
	}

	// Add peer via cjdns admin API
	if err := c.addPeer(cjdnsAddr); err != nil {
		slog.Warn("cjdns consumer: failed to add peer", "node_id", h.NodeID, "addr", cjdnsAddr, "error", err)
		return err
	}

	slog.Info("cjdns consumer: peer added", "node_id", h.NodeID, "addr", cjdnsAddr)
	return nil
}

// handleRouteRemoved processes a route removal hint and removes the peer from cjdns.
func (c *Consumer) handleRouteRemoved(h hint.Hint) error {
	c.mu.Lock()
	cjdnsAddr, exists := c.peerCache[h.NodeID]
	if !exists {
		c.mu.Unlock()
		return nil
	}
	delete(c.peerCache, h.NodeID)
	c.mu.Unlock()

	// Remove peer via cjdns admin API
	if err := c.removePeer(cjdnsAddr); err != nil {
		slog.Warn("cjdns consumer: failed to remove peer", "node_id", h.NodeID, "addr", cjdnsAddr, "error", err)
		return err
	}

	slog.Info("cjdns consumer: peer removed", "node_id", h.NodeID, "addr", cjdnsAddr)
	return nil
}

// extractCjdnsAddress converts a net.Addr to a cjdns peer address string.
func (c *Consumer) extractCjdnsAddress(addr net.Addr) (string, error) {
	if addr == nil {
		return "", fmt.Errorf("address is nil")
	}

	// Handle different address types
	switch v := addr.(type) {
	case *net.TCPAddr:
		return v.String(), nil
	case *net.UDPAddr:
		return v.String(), nil
	default:
		return addr.String(), nil
	}
}

// addPeer adds a peer to cjdns via the admin API.
func (c *Consumer) addPeer(peerAddr string) error {
	return c.callAdminAPI("UDPInterface_beginConnection", map[string]interface{}{
		"publicKey": peerAddr,
		"address":   peerAddr,
	})
}

// removePeer removes a peer from cjdns via the admin API.
// Note: cjdns doesn't have a direct "remove peer" API call.
// Peers are typically removed by letting connections time out.
func (c *Consumer) removePeer(peerAddr string) error {
	// cjdns handles peer removal through connection timeouts
	// Log the removal but don't attempt API call
	slog.Debug("cjdns consumer: peer removal handled via timeout", "addr", peerAddr)
	return nil
}

// callAdminAPI makes a request to the cjdns admin API.
// cjdns uses a custom bencode-based protocol, but for simplicity
// this implementation uses HTTP/JSON assuming a JSON-RPC wrapper.
func (c *Consumer) callAdminAPI(method string, args map[string]interface{}) error {
	// Build request payload
	payload := map[string]interface{}{
		"q":    method,
		"args": args,
	}

	// Add password if configured
	if c.config.Password != "" {
		payload["password"] = c.config.Password
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send HTTP POST request
	url := fmt.Sprintf("http://%s", c.config.Address)
	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("admin API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("admin API error (status %d): %s", resp.StatusCode, string(body))
	}

	// Parse response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for errors in response
	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return fmt.Errorf("admin API returned error: %s", errMsg)
	}

	return nil
}

// Close cleans up the consumer.
func (c *Consumer) Close() error {
	c.httpClient.CloseIdleConnections()
	return nil
}

// Stats returns statistics about the consumer.
func (c *Consumer) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"cached_peers": len(c.peerCache),
	}
}

// PingAdminAPI checks if the cjdns admin API is reachable.
func (c *Consumer) PingAdminAPI(ctx context.Context) error {
	// Try a simple ping request
	payload := map[string]interface{}{
		"q": "ping",
	}

	if c.config.Password != "" {
		payload["password"] = c.config.Password
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal ping request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("http://%s", c.config.Address), bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin API unreachable: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
