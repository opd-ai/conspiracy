// Package wifi provides nl80211-based 802.11s mesh interface control.
package wifi

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

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

// JoinMesh joins an 802.11s mesh network using the iw command-line tool.
// This implementation uses os/exec to call iw as an interim solution.
// A full nl80211 implementation using github.com/mdlayher/wifi is deferred to v1.1.
func (m *MeshController) JoinMesh(ssid string, channel int) error {
	if ssid == "" {
		return fmt.Errorf("SSID cannot be empty")
	}
	if channel < 1 || channel > 14 {
		return fmt.Errorf("invalid channel %d (must be 1-14)", channel)
	}

	if err := m.setInterfaceType("mp"); err != nil {
		return fmt.Errorf("failed to set mesh point mode: %w", err)
	}

	freq := channelToFrequency(channel)
	if err := m.joinMeshNetwork(ssid, freq); err != nil {
		return fmt.Errorf("failed to join mesh network: %w", err)
	}

	slog.Info("mesh join successful", "ssid", ssid, "channel", channel, "interface", m.ifname)
	return nil
}

// LeaveMesh leaves the current mesh network.
func (m *MeshController) LeaveMesh() error {
	cmd := exec.Command("iw", "dev", m.ifname, "mesh", "leave")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iw mesh leave failed: %w (output: %s)", err, string(output))
	}

	slog.Info("mesh leave successful", "interface", m.ifname)
	return nil
}

// Close cleans up the mesh controller.
func (m *MeshController) Close() error {
	return nil
}

// setInterfaceType sets the interface type to mesh point (mp).
func (m *MeshController) setInterfaceType(iftype string) error {
	cmd := exec.Command("iw", "dev", m.ifname, "set", "type", iftype)
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := string(output)
		if strings.Contains(outputStr, "nl80211 not supported") {
			return fmt.Errorf("nl80211 not supported by driver")
		}
		if strings.Contains(outputStr, "Device or resource busy") {
			return fmt.Errorf("interface busy (may already be in use)")
		}
		return fmt.Errorf("failed to set interface type: %w (output: %s)", err, outputStr)
	}
	return nil
}

// joinMeshNetwork joins the mesh network with specified SSID and frequency.
func (m *MeshController) joinMeshNetwork(ssid string, freq int) error {
	cmd := exec.Command("iw", "dev", m.ifname, "mesh", "join", ssid, "freq", fmt.Sprintf("%d", freq))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iw mesh join failed: %w (output: %s)", err, string(output))
	}
	return nil
}

// channelToFrequency converts 2.4 GHz channel number to frequency in MHz.
func channelToFrequency(channel int) int {
	if channel >= 1 && channel <= 13 {
		return 2407 + (channel * 5)
	}
	if channel == 14 {
		return 2484
	}
	return 0
}
