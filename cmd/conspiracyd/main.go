package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opd-ai/conspiracy/internal/config"
	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "/etc/conspiracyd/config.toml", "Path to configuration file")
	flag.Parse()

	// Initialize structured logging (JSON handler for production)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("conspiracyd starting", "version", "1.0.0-alpha")

	// Load and validate configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err, "path", *configPath)
		os.Exit(1)
	}
	slog.Info("Configuration loaded", "device", cfg.LoRa.Device, "frequency", cfg.LoRa.FrequencyMHz)

	// Perform entropy audit (blocks until /dev/random ready)
	slog.Info("Starting entropy audit (may block 10-30s on first boot)...")
	if err := crypto.EntropyAudit(); err != nil {
		slog.Error("Entropy audit failed", "error", err)
		os.Exit(1)
	}
	slog.Info("Entropy audit passed")

	// Initialize reboot counter (persistent nonce component)
	storageDir := "/var/lib/conspiracyd"
	// For testing/development, allow override via env var
	if testDir := os.Getenv("CONSPIRACYD_STORAGE_DIR"); testDir != "" {
		storageDir = testDir
	}

	rc, err := crypto.NewRebootCounter(storageDir)
	if err != nil {
		slog.Error("Failed to initialize reboot counter; LoRa disabled to prevent nonce reuse", "error", err)
		// Continue in 802.11s-only mode (batman-adv fallback)
		// For MVP: exit with error since other subsystems not yet implemented
		os.Exit(1)
	}
	slog.Info("Reboot counter initialized", "value", rc.Value())

	// Decode mesh key for crypto operations
	meshKey, err := cfg.LoRa.DecodeMeshKey()
	if err != nil {
		slog.Error("Failed to decode mesh key", "error", err)
		os.Exit(1)
	}

	// Create LoRa radio via factory pattern
	loraConfig := lora.Config{
		Device:    cfg.LoRa.Device,
		Frequency: cfg.LoRa.FrequencyMHz,
		SF:        cfg.LoRa.Spreading,
		Bandwidth: cfg.LoRa.BandwidthKHz,
		ResetPin:  cfg.LoRa.ResetPin,
		DIO0Pin:   cfg.LoRa.DIO0Pin,
		UDPListen: cfg.LoRa.UDPListen,
		UDPPeer:   cfg.LoRa.UDPPeer,
	}

	radio, err := lora.NewRadio(loraConfig)
	if err != nil {
		slog.Error("LoRa radio initialization failed", "device", cfg.LoRa.Device, "error", err)
		os.Exit(1)
	}
	defer radio.Close()
	slog.Info("LoRa radio initialized", "device", cfg.LoRa.Device, "frequency", cfg.LoRa.FrequencyMHz)

	// Initialize nonce generator (for BEACON encryption)
	// NodeID generation deferred to Phase 2 - using placeholder for now
	nodeID := uint32(os.Getpid()) // Temporary: use PID as NodeID
	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc.Value())
	if err != nil {
		slog.Error("Failed to initialize nonce generator", "error", err)
		os.Exit(1)
	}
	slog.Info("Nonce generator initialized", "node_id", nodeID, "reboot_counter", rc.Value())

	// Create cancellable context for goroutine coordination
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start LoRa RX goroutine (placeholder for auto-join FSM)
	go loraRxLoop(ctx, radio, ng)

	// Signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Daemon ready")
	<-sigChan
	slog.Info("Shutdown signal received, cleaning up...")
	cancel()

	// Give goroutines time to exit gracefully
	// In production, use sync.WaitGroup for coordination
	slog.Info("Shutdown complete")
}

// loraRxLoop is a placeholder for the LoRa receive loop.
// In Phase 2, this will implement the auto-join FSM (BEACON scanning, JOIN_REQ/ACK).
func loraRxLoop(ctx context.Context, radio lora.PacketRadio, ng *crypto.NonceGenerator) {
	slog.Info("LoRa RX loop started")
	defer slog.Info("LoRa RX loop stopped")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Placeholder: receive frames (will be processed by auto-join FSM)
			_, err := radio.Recv(ctx)
			if err != nil {
				// Log receive errors at debug level (expected timeouts)
				slog.Debug("LoRa receive error", "error", err)
			}
			// TODO: Process received frames through auto-join state machine
		}
	}
}
