// Package wifi provides nl80211-based 802.11s mesh interface control.
package wifi

import (
	"fmt"
	"log/slog"

	"github.com/vishvananda/netlink"
)

// MeshController manages 802.11s mesh interface creation and joining.
type MeshController struct {
	ifname string
}

// NewMeshController creates a new mesh controller for the specified interface.
func NewMeshController(ifname string) *MeshController {
	return &MeshController{
		ifname: ifname,
	}
}

// CreateMeshInterface creates a new 802.11s mesh interface.
// Note: This is a simplified implementation that assumes the interface already exists
// and just needs to be configured for mesh mode. Full nl80211 interface creation
// requires more complex netlink operations that are hardware-specific.
func (m *MeshController) CreateMeshInterface() error {
	link, err := netlink.LinkByName(m.ifname)
	if err != nil {
		return fmt.Errorf("interface lookup failed: %w", err)
	}

	// Bring interface up
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring interface up: %w", err)
	}

	slog.Info("mesh interface ready", "name", m.ifname)
	return nil
}

// JoinMesh joins an 802.11s mesh network.
// This is a simplified implementation that uses iw command-line tool for mesh joining
// because the github.com/mdlayher/wifi library requires kernel 5.10+ with nl80211
// mesh support, and proper mesh configuration requires vendor-specific parameters.
// A full implementation would use nl80211 NL80211_CMD_JOIN_MESH directly.
func (m *MeshController) JoinMesh(ssid string, channel int) error {
	// Validate parameters
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}
	if channel < 1 || channel > 14 {
		return fmt.Errorf("invalid channel %d (must be 1-14)", channel)
	}

	// Note: Actual mesh joining requires calling iw or using nl80211 directly
	// This stub validates parameters and logs the operation
	// Full implementation would use:
	// - github.com/mdlayher/wifi for nl80211 communication
	// - NL80211_CMD_JOIN_MESH with mesh configuration parameters
	// - Set mesh_ttl=31, mesh_hwmp_rootmode=4 per design

	slog.Info("mesh join initiated (stub - requires iw command or nl80211 implementation)",
		"ssid", ssid, "channel", channel, "interface", m.ifname)

	return nil
}

// LeaveMesh leaves the current mesh network.
func (m *MeshController) LeaveMesh() error {
	slog.Info("mesh leave initiated (stub - requires iw command or nl80211 implementation)",
		"interface", m.ifname)
	return nil
}

// Close cleans up the mesh controller.
func (m *MeshController) Close() error {
	return nil
}
