package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// LoraPeerCount tracks the number of discovered LoRa peers.
	LoraPeerCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lora_peer_count",
		Help: "Number of discovered LoRa peers",
	})

	// BatmanOriginatorCount tracks batman-adv originator table size.
	BatmanOriginatorCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "batman_originator_count",
		Help: "Number of batman-adv originators",
	})

	// LoraRSSIAvg tracks average RSSI of received LoRa packets.
	LoraRSSIAvg = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "lora_rssi_avg",
		Help: "Average RSSI of received LoRa packets (dBm)",
	})

	// DutyCycleUtilization tracks LoRa duty cycle usage (0.0-1.0).
	DutyCycleUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "duty_cycle_utilization",
		Help: "LoRa duty cycle utilization (0.0 = 0%, 1.0 = 100%)",
	})

	// HintConsumerDrops counts hints dropped due to consumer backpressure.
	HintConsumerDrops = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "hint_consumer_drops_total",
			Help: "Total number of hints dropped per consumer",
		},
		[]string{"consumer"},
	)

	// LoraTXTotal counts total LoRa transmissions by priority and frame type.
	LoraTXTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lora_tx_total",
			Help: "Total number of LoRa transmissions",
		},
		[]string{"priority", "frame_type"},
	)

	// LoraTXDrops counts dropped transmissions by priority and reason.
	LoraTXDrops = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lora_tx_drops_total",
			Help: "Total number of dropped LoRa transmissions",
		},
		[]string{"priority", "reason"},
	)

	// LoraRXTotal counts total LoRa receptions by frame type.
	LoraRXTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "lora_rx_total",
			Help: "Total number of LoRa receptions",
		},
		[]string{"frame_type"},
	)
)

// StartServer starts the Prometheus metrics HTTP server on the specified address.
func StartServer(addr string) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(addr, nil)
}
