// Package yggdrasil provides a HintConsumer plugin for Yggdrasil overlay network integration.
package yggdrasil

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

// AdminAPIConfig holds Yggdrasil admin API connection settings.
type AdminAPIConfig struct {
	// Address of Yggdrasil admin API (e.g., "127.0.0.1:9001")
	Address string
	// Timeout for admin API requests
	Timeout time.Duration
}

// Consumer implements HintConsumer for Yggdrasil integration.
type Consumer struct {
	config     AdminAPIConfig
	httpClient *http.Client
	peerCache  map[uint32]string // NodeID -> Yggdrasil address
	mu         sync.RWMutex
}

// NewConsumer creates a new Yggdrasil HintConsumer.
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

// Consume processes a routing hint and updates Yggdrasil peer configuration.
// Implements hint.HintConsumer interface.
func (c *Consumer) Consume(h hint.Hint) error {
	switch h.Type {
	case hint.PeerDiscovered, hint.RouteAdded:
		return c.handlePeerDiscovered(h)
	case hint.RouteRemoved:
		return c.handleRouteRemoved(h)
	default:
		slog.Debug("yggdrasil consumer: ignoring hint type", "type", h.Type)
		return nil
	}
}

// handlePeerDiscovered processes a peer discovery hint and adds the peer to Yggdrasil.
func (c *Consumer) handlePeerDiscovered(h hint.Hint) error {
	// Extract IP address from hint
	yggAddr, err := c.extractYggdrasilAddress(h.Addr)
	if err != nil {
		return fmt.Errorf("failed to extract Yggdrasil address: %w", err)
	}

	c.mu.Lock()
	existingAddr, exists := c.peerCache[h.NodeID]
	c.peerCache[h.NodeID] = yggAddr
	c.mu.Unlock()

	// Skip if already added
	if exists && existingAddr == yggAddr {
		slog.Debug("yggdrasil consumer: peer already cached", "node_id", h.NodeID, "addr", yggAddr)
		return nil
	}

	// Add peer via Yggdrasil admin API
	if err := c.addPeer(yggAddr); err != nil {
		slog.Warn("yggdrasil consumer: failed to add peer", "node_id", h.NodeID, "addr", yggAddr, "error", err)
		return err
	}

	slog.Info("yggdrasil consumer: peer added", "node_id", h.NodeID, "addr", yggAddr)
	return nil
}

// handleRouteRemoved processes a route removal hint and removes the peer from Yggdrasil.
func (c *Consumer) handleRouteRemoved(h hint.Hint) error {
	c.mu.Lock()
	yggAddr, exists := c.peerCache[h.NodeID]
	if !exists {
		c.mu.Unlock()
		return nil
	}
	delete(c.peerCache, h.NodeID)
	c.mu.Unlock()

	// Remove peer via Yggdrasil admin API
	if err := c.removePeer(yggAddr); err != nil {
		slog.Warn("yggdrasil consumer: failed to remove peer", "node_id", h.NodeID, "addr", yggAddr, "error", err)
		return err
	}

	slog.Info("yggdrasil consumer: peer removed", "node_id", h.NodeID, "addr", yggAddr)
	return nil
}

// extractYggdrasilAddress converts a net.Addr to a Yggdrasil peer address string.
func (c *Consumer) extractYggdrasilAddress(addr net.Addr) (string, error) {
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

// addPeer adds a peer to Yggdrasil via the admin API.
func (c *Consumer) addPeer(peerAddr string) error {
	return c.callAdminAPI("addPeer", map[string]interface{}{
		"uri": peerAddr,
	})
}

// removePeer removes a peer from Yggdrasil via the admin API.
func (c *Consumer) removePeer(peerAddr string) error {
	return c.callAdminAPI("removePeer", map[string]interface{}{
		"uri": peerAddr,
	})
}

// callAdminAPI makes a request to the Yggdrasil admin API.
func (c *Consumer) callAdminAPI(method string, args map[string]interface{}) error {
	// Build request payload
	payload := map[string]interface{}{
		"request": method,
		"args":    args,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send HTTP POST request
	url := fmt.Sprintf("http://%s/api", c.config.Address)
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

// PingAdminAPI checks if the Yggdrasil admin API is reachable.
func (c *Consumer) PingAdminAPI(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s/api", c.config.Address), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("admin API unreachable: %w", err)
	}
	defer resp.Body.Close()

	return nil
}
