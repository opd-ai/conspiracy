package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opd-ai/conspiracy/internal/config"
	"github.com/opd-ai/conspiracy/internal/crypto"
	"github.com/opd-ai/conspiracy/internal/lora"
	"github.com/opd-ai/conspiracy/internal/metrics"
)

func main() {
	configPath := flag.String("config", "/etc/conspiracyd/config.toml", "Path to configuration file")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	slog.Info("conspiracyd starting", "version", "1.0.0-alpha")

	cfg, err := loadAndValidateConfig(*configPath)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err, "path", *configPath)
		os.Exit(1)
	}

	if err := performSecurityChecks(); err != nil {
		slog.Error("Security checks failed", "error", err)
		os.Exit(1)
	}

	rc, meshKey, err := initializeCryptoComponents(cfg)
	if err != nil {
		slog.Error("Crypto initialization failed", "error", err)
		os.Exit(1)
	}

	radio, ng, err := initializeLoRaSubsystem(cfg, meshKey, rc)
	if err != nil {
		slog.Error("LoRa initialization failed", "error", err)
		os.Exit(1)
	}
	defer radio.Close()

	runDaemon(radio, ng)
}

// loadAndValidateConfig loads configuration from file.
func loadAndValidateConfig(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	slog.Info("Configuration loaded", "device", cfg.LoRa.Device, "frequency", cfg.LoRa.FrequencyMHz)
	return cfg, nil
}

// performSecurityChecks validates entropy sources.
func performSecurityChecks() error {
	slog.Info("Starting entropy audit (may block 10-30s on first boot)...")
	if err := crypto.EntropyAudit(); err != nil {
		return err
	}
	slog.Info("Entropy audit passed")
	return nil
}

// initializeCryptoComponents sets up reboot counter and mesh key.
func initializeCryptoComponents(cfg *config.Config) (*crypto.RebootCounter, []byte, error) {
	storageDir := "/var/lib/conspiracyd"
	if testDir := os.Getenv("CONSPIRACYD_STORAGE_DIR"); testDir != "" {
		storageDir = testDir
	}

	rc, err := crypto.NewRebootCounter(storageDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize reboot counter: %w", err)
	}
	slog.Info("Reboot counter initialized", "value", rc.Value())

	meshKey, err := cfg.LoRa.DecodeMeshKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode mesh key: %w", err)
	}

	return rc, meshKey, nil
}

// initializeLoRaSubsystem creates radio and nonce generator.
func initializeLoRaSubsystem(cfg *config.Config, meshKey []byte, rc *crypto.RebootCounter) (lora.PacketRadio, *crypto.NonceGenerator, error) {
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
		return nil, nil, fmt.Errorf("radio initialization failed: %w", err)
	}
	slog.Info("LoRa radio initialized", "device", cfg.LoRa.Device, "frequency", cfg.LoRa.FrequencyMHz)

	nodeID := uint32(os.Getpid())
	ng, err := crypto.NewNonceGenerator(meshKey, nodeID, rc)
	if err != nil {
		radio.Close()
		return nil, nil, fmt.Errorf("nonce generator initialization failed: %w", err)
	}
	slog.Info("Nonce generator initialized", "node_id", nodeID, "reboot_counter", rc.Value())

	return radio, ng, nil
}

// runDaemon starts background goroutines and waits for shutdown.
func runDaemon(radio lora.PacketRadio, ng *crypto.NonceGenerator) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go loraRxLoop(ctx, radio, ng)

	go func() {
		slog.Info("Starting metrics server", "addr", ":9090")
		if err := metrics.StartServer(":9090"); err != nil {
			slog.Error("Metrics server failed", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Daemon ready")
	<-sigChan
	slog.Info("Shutdown signal received, cleaning up...")
	cancel()

	slog.Info("Shutdown complete")
}

// loraRxLoop is a placeholder for the LoRa receive loop.
// In Phase 2, this will implement the auto-join FSM (BEACON scanning, JOIN_REQ/ACK).
func loraRxLoop(ctx context.Context, radio lora.PacketRadio, ng *crypto.NonceGenerator) {
	slog.Info("LoRa RX loop started")
	defer slog.Info("LoRa RX loop stopped")

	for {
		if shouldStopRxLoop(ctx) {
			return
		}
		receiveAndLogFrame(ctx, radio)
	}
}

// shouldStopRxLoop checks if context is cancelled.
func shouldStopRxLoop(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// receiveAndLogFrame receives a frame and logs receive errors at debug level.
func receiveAndLogFrame(ctx context.Context, radio lora.PacketRadio) {
	_, err := radio.Recv(ctx)
	if err != nil {
		slog.Debug("LoRa receive error", "error", err)
	}
	// TODO: Process received frames through auto-join state machine
}
