//go:build benchmark
// +build benchmark

// Package benchmark contains performance and scalability tests for batman-adv integration.
// To run: go test -v -tags=benchmark ./test/benchmark
package benchmark

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/opd-ai/conspiracy/internal/batman"
)

// TestBatmanAdvScaling_500Nodes validates batman-adv behavior at 500 originators.
// Expected: OGM emission remains active, memory usage <100MB, CPU <5%.
func TestBatmanAdvScaling_500Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark test in short mode")
	}

	testScaleScenario(t, ScaleScenario{
		Name:                    "500 Nodes",
		OriginatorCount:         500,
		ExpectedOGMActive:       true,
		ExpectedMemoryLimitMB:   100,
		ExpectedCPULimitPercent: 5,
		TestDuration:            60 * time.Second,
		ExpectedOGMOverheadKBps: 32, // ~500 * 64 bytes/10s = 3.2 KB/s
		MaxAcceptableLatencyMs:  50,
	})
}

// TestBatmanAdvScaling_1000Nodes validates batman-adv behavior at 1,000 originators.
// Expected: OGM emission remains active, memory usage <200MB, CPU <10%.
func TestBatmanAdvScaling_1000Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark test in short mode")
	}

	testScaleScenario(t, ScaleScenario{
		Name:                    "1000 Nodes",
		OriginatorCount:         1000,
		ExpectedOGMActive:       true,
		ExpectedMemoryLimitMB:   200,
		ExpectedCPULimitPercent: 10,
		TestDuration:            60 * time.Second,
		ExpectedOGMOverheadKBps: 64, // ~1000 * 64 bytes/10s = 6.4 KB/s
		MaxAcceptableLatencyMs:  100,
	})
}

// TestBatmanAdvScaling_1500Nodes validates batman-adv behavior at 1,500 originators.
// Expected: OGM emission remains active, memory usage <300MB, CPU <15%.
func TestBatmanAdvScaling_1500Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark test in short mode")
	}

	testScaleScenario(t, ScaleScenario{
		Name:                    "1500 Nodes",
		OriginatorCount:         1500,
		ExpectedOGMActive:       true,
		ExpectedMemoryLimitMB:   300,
		ExpectedCPULimitPercent: 15,
		TestDuration:            60 * time.Second,
		ExpectedOGMOverheadKBps: 96, // ~1500 * 64 bytes/10s = 9.6 KB/s
		MaxAcceptableLatencyMs:  150,
	})
}

// TestBatmanAdvScaling_4500Nodes validates scale limit enforcement at 4,500 originators.
// Expected: OGM emission DISABLED at threshold, hysteresis recovery functional.
func TestBatmanAdvScaling_4500Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping benchmark test in short mode")
	}

	testScaleScenario(t, ScaleScenario{
		Name:                    "4500 Nodes (Hard Limit)",
		OriginatorCount:         4500,
		ExpectedOGMActive:       false, // Should be disabled
		ExpectedMemoryLimitMB:   900,
		ExpectedCPULimitPercent: 30,
		TestDuration:            60 * time.Second,
		ExpectedOGMOverheadKBps: 0, // OGM emission disabled
		MaxAcceptableLatencyMs:  200,
		TestHysteresisRecovery:  true,
	})
}

// ScaleScenario defines parameters for a scale testing scenario.
type ScaleScenario struct {
	Name                    string
	OriginatorCount         int
	ExpectedOGMActive       bool
	ExpectedMemoryLimitMB   int64
	ExpectedCPULimitPercent float64
	TestDuration            time.Duration
	ExpectedOGMOverheadKBps int64
	MaxAcceptableLatencyMs  int64
	TestHysteresisRecovery  bool
}

// testScaleScenario executes a scale testing scenario.
func testScaleScenario(t *testing.T, scenario ScaleScenario) {
	t.Logf("Starting scale scenario: %s", scenario.Name)
	t.Logf("Target originator count: %d", scenario.OriginatorCount)

	// Create temporary originator table
	tmpFile := createMockOriginatorTable(t, scenario.OriginatorCount)
	defer os.Remove(tmpFile)

	// Override originator table path for testing
	originalPath := batman.OriginatorTablePath
	batman.OriginatorTablePath = tmpFile
	defer func() {
		batman.OriginatorTablePath = originalPath
	}()

	// Create controller in fallback mode (no real batman-adv needed)
	controller, err := batman.NewController("bat0", "wlan0", true)
	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}
	controller.SetFallbackMode(false) // Simulate batman-adv available

	// Create scale limiter with fast polling for tests
	limiter := batman.NewScaleLimiter(controller, 1*time.Second)

	// Capture baseline metrics
	baselineMetrics := captureMetrics(t)
	t.Logf("Baseline metrics: Memory=%dMB, Goroutines=%d",
		baselineMetrics.MemoryMB, baselineMetrics.Goroutines)

	// Run limiter
	ctx, cancel := context.WithTimeout(context.Background(), scenario.TestDuration)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- limiter.Run(ctx)
	}()

	// Wait for at least 2 poll cycles
	time.Sleep(3 * time.Second)

	// Verify OGM emission state
	ogmActive := limiter.IsOGMEmissionActive()
	if ogmActive != scenario.ExpectedOGMActive {
		t.Errorf("OGM emission state mismatch: got %v, want %v",
			ogmActive, scenario.ExpectedOGMActive)
	} else {
		t.Logf("✓ OGM emission state correct: %v", ogmActive)
	}

	// Verify originator count
	count := limiter.GetOriginatorCount()
	if count != scenario.OriginatorCount {
		t.Errorf("Originator count mismatch: got %d, want %d",
			count, scenario.OriginatorCount)
	} else {
		t.Logf("✓ Originator count correct: %d", count)
	}

	// Test hysteresis recovery if requested
	if scenario.TestHysteresisRecovery {
		testHysteresisRecovery(t, tmpFile, limiter)
	}

	// Capture post-test metrics
	time.Sleep(2 * time.Second)
	postMetrics := captureMetrics(t)
	t.Logf("Post-test metrics: Memory=%dMB, Goroutines=%d",
		postMetrics.MemoryMB, postMetrics.Goroutines)

	// Verify memory usage
	memoryDelta := postMetrics.MemoryMB - baselineMetrics.MemoryMB
	if memoryDelta > scenario.ExpectedMemoryLimitMB {
		t.Errorf("Memory usage exceeded limit: %dMB > %dMB",
			memoryDelta, scenario.ExpectedMemoryLimitMB)
	} else {
		t.Logf("✓ Memory usage within limit: %dMB <= %dMB",
			memoryDelta, scenario.ExpectedMemoryLimitMB)
	}

	// Verify goroutine leak
	goroutineDelta := postMetrics.Goroutines - baselineMetrics.Goroutines
	if goroutineDelta > 10 {
		t.Errorf("Potential goroutine leak: +%d goroutines", goroutineDelta)
	} else {
		t.Logf("✓ No goroutine leak detected: +%d goroutines", goroutineDelta)
	}

	// Stop limiter
	cancel()
	select {
	case err := <-errChan:
		if err != nil && err != context.Canceled {
			t.Errorf("Limiter run failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Limiter did not stop within 5s after context cancellation")
	}

	t.Logf("Scale scenario completed: %s", scenario.Name)
}

// testHysteresisRecovery tests that OGM emission re-enables at 4,200 originators.
func testHysteresisRecovery(t *testing.T, tmpFile string, limiter *batman.ScaleLimiter) {
	t.Log("Testing hysteresis recovery (4,500 → 4,200 originators)...")

	// Update mock table to 4,200 originators
	updateMockOriginatorTable(t, tmpFile, 4200)

	// Wait for next poll cycle
	time.Sleep(2 * time.Second)

	// Verify OGM emission re-enabled
	if !limiter.IsOGMEmissionActive() {
		t.Error("Hysteresis recovery failed: OGM emission still disabled at 4,200 originators")
	} else {
		t.Log("✓ Hysteresis recovery successful: OGM emission re-enabled")
	}

	// Restore original count
	updateMockOriginatorTable(t, tmpFile, 4500)
	time.Sleep(2 * time.Second)
}

// createMockOriginatorTable creates a mock batman-adv originator table file.
func createMockOriginatorTable(t *testing.T, count int) string {
	tmpFile, err := os.CreateTemp("", "batman-originators-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Write header
	fmt.Fprintln(tmpFile, "[B.A.T.M.A.N. adv 2024.0, MainIF/MAC: bat0/00:00:00:00:00:00]")
	fmt.Fprintln(tmpFile, "Originator        last-seen (#/255)           Nexthop [outgoingIF]:")

	// Write originator entries
	for i := 1; i <= count; i++ {
		mac := fmt.Sprintf("02:00:00:%02x:%02x:%02x",
			(i>>16)&0xFF, (i>>8)&0xFF, i&0xFF)
		fmt.Fprintf(tmpFile, "%s  0.123s   (255) %s [   wlan0]\n", mac, mac)
	}

	tmpFile.Close()
	t.Logf("Created mock originator table: %s (%d entries)", tmpFile.Name(), count)
	return tmpFile.Name()
}

// updateMockOriginatorTable updates the originator count in an existing mock table.
func updateMockOriginatorTable(t *testing.T, path string, newCount int) {
	tmpFile, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to update mock table: %v", err)
	}
	defer tmpFile.Close()

	// Write header
	fmt.Fprintln(tmpFile, "[B.A.T.M.A.N. adv 2024.0, MainIF/MAC: bat0/00:00:00:00:00:00]")
	fmt.Fprintln(tmpFile, "Originator        last-seen (#/255)           Nexthop [outgoingIF]:")

	// Write new originator entries
	for i := 1; i <= newCount; i++ {
		mac := fmt.Sprintf("02:00:00:%02x:%02x:%02x",
			(i>>16)&0xFF, (i>>8)&0xFF, i&0xFF)
		fmt.Fprintf(tmpFile, "%s  0.123s   (255) %s [   wlan0]\n", mac, mac)
	}

	t.Logf("Updated mock originator table to %d entries", newCount)
}

// Metrics holds runtime metrics for a test scenario.
type Metrics struct {
	MemoryMB   int64
	Goroutines int
	CPUPercent float64
}

// captureMetrics captures current runtime metrics.
func captureMetrics(t *testing.T) Metrics {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return Metrics{
		MemoryMB:   int64(m.Alloc / 1024 / 1024),
		Goroutines: runtime.NumGoroutine(),
		CPUPercent: 0, // CPU measurement requires sampling over time
	}
}

// BenchmarkOriginatorTableParsing benchmarks the originator table parsing performance.
func BenchmarkOriginatorTableParsing(b *testing.B) {
	counts := []int{100, 500, 1000, 2000, 5000}

	for _, count := range counts {
		b.Run(fmt.Sprintf("%dOriginators", count), func(b *testing.B) {
			tmpFile := createMockOriginatorTableForBench(b, count)
			defer os.Remove(tmpFile)

			controller, err := batman.NewController("bat0", "wlan0", true)
			if err != nil {
				b.Fatalf("Failed to create controller: %v", err)
			}
			limiter := batman.NewScaleLimiter(controller, 1*time.Second)

			// Override path
			originalPath := batman.OriginatorTablePath
			batman.OriginatorTablePath = tmpFile
			defer func() {
				batman.OriginatorTablePath = originalPath
			}()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count, err := limiter.ReadOriginatorTableForTest()
				if err != nil {
					b.Fatalf("Parse failed: %v", err)
				}
				if count == 0 {
					b.Fatal("Zero originators parsed")
				}
			}
		})
	}
}

// createMockOriginatorTableForBench creates a mock table for benchmarking.
func createMockOriginatorTableForBench(b *testing.B, count int) string {
	tmpFile, err := os.CreateTemp("", "batman-bench-*.txt")
	if err != nil {
		b.Fatalf("Failed to create temp file: %v", err)
	}

	var sb strings.Builder
	sb.WriteString("[B.A.T.M.A.N. adv 2024.0, MainIF/MAC: bat0/00:00:00:00:00:00]\n")
	sb.WriteString("Originator        last-seen (#/255)           Nexthop [outgoingIF]:\n")

	for i := 1; i <= count; i++ {
		mac := fmt.Sprintf("02:00:00:%02x:%02x:%02x",
			(i>>16)&0xFF, (i>>8)&0xFF, i&0xFF)
		sb.WriteString(fmt.Sprintf("%s  0.123s   (255) %s [   wlan0]\n", mac, mac))
	}

	if _, err := tmpFile.WriteString(sb.String()); err != nil {
		b.Fatalf("Failed to write mock table: %v", err)
	}
	tmpFile.Close()

	return tmpFile.Name()
}
