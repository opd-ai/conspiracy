// Package batman provides batman-adv netlink controller and OGM event handling.
package batman

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/vishvananda/netlink"
)

// Controller manages batman-adv lifecycle and OGM monitoring.
type Controller struct {
	batInterface  string
	meshInterface string
	enabled       bool
	fallbackMode  bool
}

// NewController creates a new batman-adv controller.
// It probes for batman-adv kernel module and initializes the bat0 interface.
// If batman-adv is unavailable, it enables fallback mode (802.11s-only with HWMP routing).
func NewController(batInterface, meshInterface string, enabled bool) (*Controller, error) {
	c := &Controller{
		batInterface:  batInterface,
		meshInterface: meshInterface,
		enabled:       enabled,
	}

	if !enabled {
		slog.Info("batman-adv disabled via config")
		c.fallbackMode = true
		return c, nil
	}

	// Probe for batman-adv kernel module
	if _, err := os.Stat("/sys/module/batman_adv"); os.IsNotExist(err) {
		slog.Warn("batman-adv kernel module not loaded; operating in 802.11s-only mode (HWMP routing)")
		c.fallbackMode = true
		return c, nil
	}

	// Create bat0 interface
	if err := c.createBatInterface(); err != nil {
		slog.Error("batman-adv interface creation failed; falling back to 802.11s-only mode", "error", err)
		c.fallbackMode = true
		return c, nil
	}

	// Add mesh interface to bat0
	if err := c.addInterfaceToBat(); err != nil {
		slog.Error("adding interface to batman-adv failed", "error", err)
		c.fallbackMode = true
		return c, nil
	}

	slog.Info("batman-adv operational", "interface", batInterface)
	return c, nil
}

// createBatInterface creates the batman-adv interface via netlink.
func (c *Controller) createBatInterface() error {
	// Check if interface already exists
	if _, err := netlink.LinkByName(c.batInterface); err == nil {
		slog.Info("batman-adv interface already exists", "name", c.batInterface)
		return nil
	}

	// Create bat0 via netlink
	bat := &netlink.GenericLink{
		LinkAttrs: netlink.LinkAttrs{
			Name: c.batInterface,
			MTU:  1500,
		},
		LinkType: "batadv",
	}
	if err := netlink.LinkAdd(bat); err != nil {
		return fmt.Errorf("netlink.LinkAdd failed: %w", err)
	}

	// Bring interface up
	link, err := netlink.LinkByName(c.batInterface)
	if err != nil {
		return fmt.Errorf("link lookup failed: %w", err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("link up failed: %w", err)
	}

	slog.Info("batman-adv interface created", "name", c.batInterface)
	return nil
}

// addInterfaceToBat adds the mesh interface to the batman-adv interface.
// Equivalent to: batctl if add <meshInterface>
func (c *Controller) addInterfaceToBat() error {
	mesh, err := netlink.LinkByName(c.meshInterface)
	if err != nil {
		return fmt.Errorf("mesh interface lookup failed: %w", err)
	}

	bat, err := netlink.LinkByName(c.batInterface)
	if err != nil {
		return fmt.Errorf("bat interface lookup failed: %w", err)
	}

	// Add mesh interface to bat0 (equivalent to: batctl if add)
	if err := netlink.LinkSetMaster(mesh, bat); err != nil {
		return fmt.Errorf("netlink.LinkSetMaster failed: %w", err)
	}

	slog.Info("mesh interface added to batman-adv", "mesh", c.meshInterface, "bat", c.batInterface)
	return nil
}

// IsFallbackMode returns true if operating in 802.11s-only mode (no batman-adv).
func (c *Controller) IsFallbackMode() bool {
	return c.fallbackMode
}

// SetFallbackMode sets the fallback mode state (used for testing).
func (c *Controller) SetFallbackMode(fallback bool) {
	c.fallbackMode = fallback
}

// SubscribeOGMEvents starts listening for batman-adv originator messages via netlink multicast.
// This is a placeholder for future originator count monitoring and scale limit enforcement.
// The implementation monitors OGM events and maintains an originator count for the mesh.
func (c *Controller) SubscribeOGMEvents(ctx context.Context) error {
	if c.fallbackMode {
		return nil // No-op in fallback mode
	}

	// Placeholder: OGM event subscription will be implemented in a future version
	// to support originator count monitoring, scale limit enforcement (4,500 node ceiling),
	// and Prometheus metrics exposure.
	slog.Info("OGM event subscription placeholder (deferred to post-MVP)")

	// Keep goroutine alive until context cancellation
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Future: Poll originator table, update metrics
		}
	}
}

// Close cleans up the batman-adv controller.
func (c *Controller) Close() error {
	if c.fallbackMode {
		return nil
	}

	// Remove mesh interface from batman-adv
	mesh, err := netlink.LinkByName(c.meshInterface)
	if err != nil {
		return fmt.Errorf("mesh interface lookup failed: %w", err)
	}

	if err := netlink.LinkSetNoMaster(mesh); err != nil {
		return fmt.Errorf("netlink.LinkSetNoMaster failed: %w", err)
	}

	slog.Info("mesh interface removed from batman-adv", "mesh", c.meshInterface)
	return nil
}
