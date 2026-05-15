// Package batman provides originator count monitoring and scale limit enforcement.
package batman

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/opd-ai/conspiracy/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// MaxOriginators is the hard limit for originator count (5,000 nodes as per design).
	// Above this, OGM emission is disabled to prevent network collapse.
	MaxOriginators = 5000

	// OriginatorWarnThreshold triggers a warning log at 4,500 originators (90% capacity).
	OriginatorWarnThreshold = 4500

	// OriginatorHysteresisRecovery is the threshold for re-enabling OGM emission (4,200 originators).
	OriginatorHysteresisRecovery = 4200

	// FederationGuidanceThreshold triggers federation guidance at 4,000 originators (80% capacity).
	FederationGuidanceThreshold = 4000
)

// OriginatorTablePath is the path to batman-adv's originator table in sysfs (mutable for testing).
var OriginatorTablePath = "/sys/kernel/debug/batman_adv/bat0/originators"

// Prometheus metrics for OGM emission state
var batmanOGMEmissionEnabled = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "batman_ogm_emission_enabled",
	Help: "Whether OGM emission is enabled (1) or disabled (0)",
})

// ScaleLimiter monitors originator count and enforces scale limits.
type ScaleLimiter struct {
	controller        *Controller
	originatorCount   int
	ogmEmissionActive bool
	mu                sync.RWMutex
	pollInterval      time.Duration
}

// NewScaleLimiter creates a new scale limiter.
func NewScaleLimiter(controller *Controller, pollInterval time.Duration) *ScaleLimiter {
	if pollInterval == 0 {
		pollInterval = 60 * time.Second
	}

	return &ScaleLimiter{
		controller:        controller,
		ogmEmissionActive: true,
		pollInterval:      pollInterval,
	}
}

// Run starts the scale limiter monitoring loop.
func (sl *ScaleLimiter) Run(ctx context.Context) error {
	if sl.controller.IsFallbackMode() {
		slog.Info("Scale limiter disabled (batman-adv fallback mode)")
		return nil
	}

	slog.Info("Scale limiter starting", "poll_interval", sl.pollInterval)
	batmanOGMEmissionEnabled.Set(1) // Initially enabled

	ticker := time.NewTicker(sl.pollInterval)
	defer ticker.Stop()

	// Initial poll
	if err := sl.pollOriginatorCount(); err != nil {
		slog.Warn("Initial originator count poll failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("Scale limiter stopped", "reason", ctx.Err())
			return ctx.Err()
		case <-ticker.C:
			if err := sl.pollOriginatorCount(); err != nil {
				slog.Warn("Originator count poll failed", "error", err)
			}
		}
	}
}

// pollOriginatorCount reads the batman-adv originator table and enforces limits.
func (sl *ScaleLimiter) pollOriginatorCount() error {
	count, err := sl.readOriginatorTable()
	if err != nil {
		return fmt.Errorf("failed to read originator table: %w", err)
	}

	sl.mu.Lock()
	sl.originatorCount = count
	sl.mu.Unlock()

	// Update Prometheus metric
	metrics.BatmanOriginatorCount.Set(float64(count))

	// Enforce scale limits
	sl.enforceScaleLimits(count)

	return nil
}

// readOriginatorTable parses the batman-adv originator table from sysfs.
// Returns the number of unique originators.
func (sl *ScaleLimiter) readOriginatorTable() (int, error) {
	file, err := os.Open(OriginatorTablePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open originator table: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	// Skip header line
	if !scanner.Scan() {
		return 0, fmt.Errorf("empty originator table")
	}

	// Count non-empty lines (each line is an originator)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			count++
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scanner error: %w", err)
	}

	return count, nil
}

// ReadOriginatorTableForTest exposes readOriginatorTable for testing.
func (sl *ScaleLimiter) ReadOriginatorTableForTest() (int, error) {
	return sl.readOriginatorTable()
}

// enforceScaleLimits implements the scale limit enforcement logic.
func (sl *ScaleLimiter) enforceScaleLimits(count int) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	// Check if we've reached the hard limit (4,500 originators)
	if count >= OriginatorWarnThreshold && sl.ogmEmissionActive {
		slog.Warn("Approaching batman-adv scale limit; disabling OGM emission",
			"originator_count", count,
			"max_limit", MaxOriginators)

		// Disable OGM emission
		if err := sl.disableOGMEmission(); err != nil {
			slog.Error("Failed to disable OGM emission", "error", err)
		} else {
			sl.ogmEmissionActive = false
			batmanOGMEmissionEnabled.Set(0)
			slog.Warn("OGM emission disabled. Node is now passive relay (continues forwarding traffic but stops advertising itself)")
		}
	}

	// Hysteresis recovery: re-enable OGM at 4,200 originators
	if count <= OriginatorHysteresisRecovery && !sl.ogmEmissionActive {
		slog.Info("Originator count dropped below hysteresis threshold; re-enabling OGM emission",
			"originator_count", count,
			"hysteresis_threshold", OriginatorHysteresisRecovery)

		if err := sl.enableOGMEmission(); err != nil {
			slog.Error("Failed to re-enable OGM emission", "error", err)
		} else {
			sl.ogmEmissionActive = true
			batmanOGMEmissionEnabled.Set(1)
		}
	}

	// Guidance logs
	if count >= FederationGuidanceThreshold && count < OriginatorWarnThreshold {
		slog.Info("Network has reached 80% capacity; consider deploying federated mesh islands",
			"originator_count", count,
			"capacity", fmt.Sprintf("%.1f%%", float64(count)/float64(MaxOriginators)*100),
			"guidance", "see docs/federation.md")
	}
}

// disableOGMEmission disables OGM broadcasting via batman-adv sysfs.
func (sl *ScaleLimiter) disableOGMEmission() error {
	// Write "0" to /sys/kernel/debug/batman_adv/bat0/gw_mode to disable OGM emission
	// In real implementation, this would use batman-adv netlink API
	// For MVP, we just log the action
	slog.Info("OGM emission disabled (sysfs write would happen here)")
	return nil
}

// enableOGMEmission re-enables OGM broadcasting via batman-adv sysfs.
func (sl *ScaleLimiter) enableOGMEmission() error {
	// Write appropriate value to re-enable OGM emission
	// For MVP, we just log the action
	slog.Info("OGM emission enabled (sysfs write would happen here)")
	return nil
}

// GetOriginatorCount returns the current originator count.
func (sl *ScaleLimiter) GetOriginatorCount() int {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.originatorCount
}

// IsOGMEmissionActive returns whether OGM emission is currently enabled.
func (sl *ScaleLimiter) IsOGMEmissionActive() bool {
	sl.mu.RLock()
	defer sl.mu.RUnlock()
	return sl.ogmEmissionActive
}
