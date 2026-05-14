// Package lora provides multi-frequency zoning for 250+ node deployments.
package lora

import (
	"fmt"
	"log/slog"
	"sort"
)

// FrequencyZone represents a LoRa frequency zone configuration
type FrequencyZone struct {
	Region      string    // "EU868", "US915", "AS923"
	Frequencies []float64 // Available frequencies in MHz
}

// Regional frequency allocations
var (
	// EU868Zone provides 3 non-overlapping frequencies with 150 kHz spacing
	EU868Zone = FrequencyZone{
		Region:      "EU868",
		Frequencies: []float64{868.1, 868.3, 868.5},
	}

	// US915Zone provides 4 non-overlapping frequencies with 400 kHz spacing
	US915Zone = FrequencyZone{
		Region:      "US915",
		Frequencies: []float64{915.2, 915.6, 916.0, 916.4},
	}

	// AS923Zone provides 2 non-overlapping frequencies with 200 kHz spacing
	AS923Zone = FrequencyZone{
		Region:      "AS923",
		Frequencies: []float64{923.2, 923.4},
	}
)

// ZoneManager manages multi-frequency zoning for scalable deployments
type ZoneManager struct {
	zone          FrequencyZone
	nodeID        uint32
	assignedFreq  float64
	isBridgeNode  bool
	bridgeFreqs   []float64
	peerFreqCount map[float64]int // Tracks peer count per frequency
}

// NewZoneManager creates a new zone manager with hash-based frequency assignment
func NewZoneManager(zone FrequencyZone, nodeID uint32) *ZoneManager {
	if len(zone.Frequencies) == 0 {
		slog.Warn("Zone has no frequencies; defaulting to single frequency")
		return nil
	}

	// Hash-based deterministic assignment: zone = NodeID % num_frequencies
	zoneIndex := int(nodeID) % len(zone.Frequencies)
	assignedFreq := zone.Frequencies[zoneIndex]

	zm := &ZoneManager{
		zone:          zone,
		nodeID:        nodeID,
		assignedFreq:  assignedFreq,
		isBridgeNode:  false,
		peerFreqCount: make(map[float64]int),
	}

	slog.Info("Zone assignment", "node_id", nodeID, "frequency", assignedFreq, "zone", zone.Region)
	return zm
}

// GetAssignedFrequency returns the node's primary frequency
func (zm *ZoneManager) GetAssignedFrequency() float64 {
	return zm.assignedFreq
}

// IsBridgeNode returns true if this node operates in bridge mode (dual-frequency)
func (zm *ZoneManager) IsBridgeNode() bool {
	return zm.isBridgeNode
}

// GetBridgeFrequencies returns additional frequencies for bridge node operation
func (zm *ZoneManager) GetBridgeFrequencies() []float64 {
	if !zm.isBridgeNode {
		return nil
	}
	return zm.bridgeFreqs
}

// UpdatePeerFrequency records a peer's frequency from received BEACON
func (zm *ZoneManager) UpdatePeerFrequency(nodeID uint32, frequency float64) {
	// Validate frequency is in zone
	validFreq := false
	for _, f := range zm.zone.Frequencies {
		if f == frequency {
			validFreq = true
			break
		}
	}

	if !validFreq {
		slog.Debug("Ignoring peer on unknown frequency", "peer_id", nodeID, "frequency", frequency)
		return
	}

	zm.peerFreqCount[frequency]++

	// Check bridge node threshold: >10% peers on different frequency
	zm.updateBridgeStatus()
}

// RemovePeer decrements frequency count when peer times out
func (zm *ZoneManager) RemovePeer(frequency float64) {
	if count, exists := zm.peerFreqCount[frequency]; exists && count > 0 {
		zm.peerFreqCount[frequency]--
		zm.updateBridgeStatus()
	}
}

// updateBridgeStatus determines if node should operate in bridge mode
func (zm *ZoneManager) updateBridgeStatus() {
	totalPeers := 0
	for _, count := range zm.peerFreqCount {
		totalPeers += count
	}

	if totalPeers < 10 {
		// Insufficient peers to determine bridge status
		if zm.isBridgeNode {
			slog.Info("Bridge mode disabled: insufficient peers", "total_peers", totalPeers)
			zm.isBridgeNode = false
			zm.bridgeFreqs = nil
		}
		return
	}

	// Calculate peers on different frequencies
	otherFreqPeers := 0
	var otherFreqs []float64

	for freq, count := range zm.peerFreqCount {
		if freq != zm.assignedFreq && count > 0 {
			otherFreqPeers += count
			otherFreqs = append(otherFreqs, freq)
		}
	}

	// Bridge threshold: >10% peers on different frequency
	threshold := float64(totalPeers) * 0.10
	shouldBridge := float64(otherFreqPeers) > threshold

	if shouldBridge && !zm.isBridgeNode {
		// Enter bridge mode
		zm.isBridgeNode = true
		zm.bridgeFreqs = otherFreqs
		slog.Info("Bridge mode enabled",
			"node_id", zm.nodeID,
			"primary_freq", zm.assignedFreq,
			"bridge_freqs", otherFreqs,
			"other_freq_peers", otherFreqPeers,
			"total_peers", totalPeers,
		)
	} else if !shouldBridge && zm.isBridgeNode {
		// Exit bridge mode
		zm.isBridgeNode = false
		zm.bridgeFreqs = nil
		slog.Info("Bridge mode disabled",
			"node_id", zm.nodeID,
			"other_freq_peers", otherFreqPeers,
			"total_peers", totalPeers,
		)
	}
}

// GetRecommendedSF returns recommended spreading factor based on peer density
func (zm *ZoneManager) GetRecommendedSF(currentSF int) int {
	totalPeers := 0
	for _, count := range zm.peerFreqCount {
		totalPeers += count
	}

	// High-density zone (>100 visible peers): switch to SF7 for bandwidth efficiency
	if totalPeers > 100 {
		if currentSF != 7 {
			slog.Info("High-density zone detected; recommending SF7", "peer_count", totalPeers)
		}
		return 7
	}

	// Sparse deployment (<20 peers): use SF10 for extended range
	if totalPeers < 20 {
		if currentSF != 10 {
			slog.Info("Sparse deployment detected; recommending SF10", "peer_count", totalPeers)
		}
		return 10
	}

	// Medium density (20-100 peers): maintain current SF
	return currentSF
}

// GetZoneForRegion returns the appropriate frequency zone for a region
func GetZoneForRegion(frequency float64) (FrequencyZone, error) {
	// EU bands: 863-870 MHz
	if frequency >= 863 && frequency <= 870 {
		return EU868Zone, nil
	}

	// US/AU bands: 902-928 MHz
	if frequency >= 902 && frequency <= 928 {
		return US915Zone, nil
	}

	// Asia bands: 920-925 MHz
	if frequency >= 920 && frequency <= 925 {
		return AS923Zone, nil
	}

	return FrequencyZone{}, fmt.Errorf("no zone available for frequency %.1f MHz", frequency)
}

// ShouldForwardFrame determines if a frame should be forwarded across frequency zones
func (zm *ZoneManager) ShouldForwardFrame(frameType uint8) bool {
	if !zm.isBridgeNode {
		return false
	}

	// Bridge nodes forward critical frames between frequencies
	switch frameType {
	case FrameTypeBEACON, FrameTypeJOIN_ACK:
		return true
	case FrameTypeROUTE_HINT:
		// Do NOT forward ROUTE_HINT (amplification risk)
		return false
	default:
		return false
	}
}

// GetPeerCountByFrequency returns peers per frequency for diagnostics
func (zm *ZoneManager) GetPeerCountByFrequency() map[float64]int {
	// Return copy to prevent external modification
	counts := make(map[float64]int, len(zm.peerFreqCount))
	for freq, count := range zm.peerFreqCount {
		counts[freq] = count
	}
	return counts
}

// GetZoneStats returns diagnostic statistics for zone manager
type ZoneStats struct {
	NodeID              uint32
	AssignedFrequency   float64
	IsBridgeNode        bool
	BridgeFrequencies   []float64
	TotalPeers          int
	PeersByFrequency    map[float64]int
	RecommendedSF       int
	BridgeThresholdMet  bool
	OtherFrequencyRatio float64
}

// GetStats returns current zone manager statistics
func (zm *ZoneManager) GetStats(currentSF int) ZoneStats {
	totalPeers := 0
	for _, count := range zm.peerFreqCount {
		totalPeers += count
	}

	otherFreqPeers := 0
	for freq, count := range zm.peerFreqCount {
		if freq != zm.assignedFreq {
			otherFreqPeers += count
		}
	}

	var otherFreqRatio float64
	if totalPeers > 0 {
		otherFreqRatio = float64(otherFreqPeers) / float64(totalPeers)
	}

	return ZoneStats{
		NodeID:              zm.nodeID,
		AssignedFrequency:   zm.assignedFreq,
		IsBridgeNode:        zm.isBridgeNode,
		BridgeFrequencies:   zm.GetBridgeFrequencies(),
		TotalPeers:          totalPeers,
		PeersByFrequency:    zm.GetPeerCountByFrequency(),
		RecommendedSF:       zm.GetRecommendedSF(currentSF),
		BridgeThresholdMet:  otherFreqRatio > 0.10,
		OtherFrequencyRatio: otherFreqRatio,
	}
}

// FrequencyPlan calculates optimal frequency allocation for a deployment
type FrequencyPlan struct {
	TotalNodes         int
	NodesPerZone       int
	EstimatedBridges   int
	DutyCycleReduction float64
}

// CalculateFrequencyPlan provides deployment guidance for large networks
func CalculateFrequencyPlan(totalNodes int, zone FrequencyZone) FrequencyPlan {
	numFreqs := len(zone.Frequencies)
	if numFreqs == 0 {
		return FrequencyPlan{}
	}

	nodesPerZone := totalNodes / numFreqs
	if totalNodes%numFreqs != 0 {
		nodesPerZone++
	}

	// Estimate ~8% bridge nodes at zone boundaries
	estimatedBridges := int(float64(totalNodes) * 0.08)

	// Duty-cycle reduction factor
	dutyCycleReduction := float64(numFreqs)

	return FrequencyPlan{
		TotalNodes:         totalNodes,
		NodesPerZone:       nodesPerZone,
		EstimatedBridges:   estimatedBridges,
		DutyCycleReduction: dutyCycleReduction,
	}
}

// ValidateZoneConfiguration checks if zone configuration is valid for deployment
func ValidateZoneConfiguration(totalNodes int, zone FrequencyZone) error {
	if len(zone.Frequencies) == 0 {
		return fmt.Errorf("zone must have at least one frequency")
	}

	// Validate frequencies are properly spaced
	if len(zone.Frequencies) > 1 {
		sorted := make([]float64, len(zone.Frequencies))
		copy(sorted, zone.Frequencies)
		sort.Float64s(sorted)

		for i := 1; i < len(sorted); i++ {
			spacing := sorted[i] - sorted[i-1]
			minSpacing := 0.1 // 100 kHz minimum

			if spacing < minSpacing {
				return fmt.Errorf("frequency spacing too narrow: %.3f MHz between %.1f and %.1f MHz (min %.1f MHz)",
					spacing, sorted[i-1], sorted[i], minSpacing)
			}
		}
	}

	// Warn if deploying >250 nodes on single frequency
	if totalNodes > 250 && len(zone.Frequencies) == 1 {
		slog.Warn("Deploying >250 nodes on single frequency may violate duty-cycle limits",
			"total_nodes", totalNodes,
			"recommended_zones", (totalNodes/250)+1,
		)
	}

	return nil
}
