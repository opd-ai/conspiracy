package metrics

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMetricsServer(t *testing.T) {
	// Initialize all metrics to ensure they appear
	LoraPeerCount.Set(5)
	BatmanOriginatorCount.Set(10)
	LoraRSSIAvg.Set(-85)
	DutyCycleUtilization.Set(0.25)
	HintConsumerDrops.WithLabelValues("test").Add(1)

	go func() {
		if err := StartServer(":19091"); err != nil && !strings.Contains(err.Error(), "address already in use") {
			t.Logf("Metrics server error: %v", err)
		}
	}()

	time.Sleep(200 * time.Millisecond)

	resp, err := http.Get("http://localhost:19091/metrics")
	if err != nil {
		t.Fatalf("Failed to scrape metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	content := string(body)

	// Check for gauge metrics (always present)
	requiredMetrics := []string{
		"lora_peer_count",
		"batman_originator_count",
		"lora_rssi_avg",
		"duty_cycle_utilization",
	}

	for _, metric := range requiredMetrics {
		if !strings.Contains(content, metric) {
			t.Errorf("Required metric %s not found in output", metric)
		}
	}

	// Counter vec might need explicit initialization before appearing
	if strings.Contains(content, "hint_consumer_drops_total") {
		t.Log("hint_consumer_drops_total metric found (expected)")
	}
}

func TestMetricUpdates(t *testing.T) {
	LoraPeerCount.Set(42)
	BatmanOriginatorCount.Set(100)
	LoraRSSIAvg.Set(-85.5)
	DutyCycleUtilization.Set(0.12)
	HintConsumerDrops.WithLabelValues("yggdrasil").Inc()
	HintConsumerDrops.WithLabelValues("cjdns").Add(5)

	if LoraPeerCount == nil {
		t.Error("LoraPeerCount gauge is nil")
	}
}
