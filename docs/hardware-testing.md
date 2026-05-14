# Hardware Testing Guide

This guide explains how to run hardware-in-the-loop (HIL) tests for the LoRa driver with real SX127x hardware.

## Prerequisites

### Hardware Requirements

- **LoRa radio module**: SX1276, SX1277, SX1278, SX1279, or SX1272 chipset
- **Connection interface**: SPI (recommended), UART, or USB-Serial
- **Host platform**: Raspberry Pi, OpenWrt router, or any Linux device with GPIO support

### Example Hardware Configurations

| Hardware | Interface | SPI Device | Reset Pin | DIO0 Pin |
|----------|-----------|------------|-----------|----------|
| Raspberry Pi + Adafruit RFM95W | SPI | /dev/spidev0.0 | GPIO25 | GPIO24 |
| Raspberry Pi + RAK2245 HAT | SPI | /dev/spidev0.0 | GPIO17 | GPIO4 |
| GL.iNet router + RAK831 | SPI | /dev/spidev0.0 | GPIO25 | GPIO24 |

### Software Requirements

- Go ≥ 1.22
- Linux kernel with SPI support enabled
- `periph.io` dependencies (automatically installed via `go mod download`)

## Running Hardware Tests Locally

### 1. Enable SPI Interface

On Raspberry Pi (Raspbian/Raspberry Pi OS):
```bash
sudo raspi-config
# Navigate to: Interface Options > SPI > Enable
sudo reboot
```

Verify SPI device exists:
```bash
ls -l /dev/spidev0.0
```

### 2. Set up GPIO Permissions

Add your user to the `gpio` and `spi` groups:
```bash
sudo usermod -a -G gpio,spi $USER
# Log out and log back in for changes to take effect
```

### 3. Run Register Read Test

This test verifies basic SPI communication and chip version detection:
```bash
go test -v -tags=hardware ./internal/lora -run TestSX127xSPI_HardwareRegisterRead
```

Expected output:
```
=== RUN   TestSX127xSPI_HardwareRegisterRead
    sx127x_spi_hw_test.go:28: Successfully read SX127x version register via SPI
--- PASS: TestSX127xSPI_HardwareRegisterRead (0.05s)
```

### 4. Run TX/RX Round-Trip Test

This test requires **two nodes** with LoRa hardware or a loopback setup.

#### Node 1 (Transmitter):
```bash
LORA_TEST_MODE=tx go test -v -tags=hardware ./internal/lora -run TestSX127xSPI_TxRx_RoundTrip
```

#### Node 2 (Receiver):
```bash
LORA_TEST_MODE=rx go test -v -tags=hardware ./internal/lora -run TestSX127xSPI_TxRx_RoundTrip
```

The test suite validates transmission at **SF7, SF10, and SF12** spreading factors as specified in the design requirements.

### 5. Run Configuration Validation Test

This test verifies parameter validation (spreading factor, bandwidth, frequency):
```bash
go test -v -tags=hardware ./internal/lora -run TestSX127xSPI_ConfigurationValidation
```

## CI Integration with Self-Hosted Runners

To enable automated hardware testing in CI/CD, configure a self-hosted GitHub Actions runner with LoRa hardware attached.

### Self-Hosted Runner Setup

1. **Prepare hardware**: Connect LoRa module to Raspberry Pi via SPI
2. **Install runner**:
   ```bash
   # On the Raspberry Pi:
   mkdir actions-runner && cd actions-runner
   curl -o actions-runner-linux-arm64.tar.gz -L \
     https://github.com/actions/runner/releases/download/v2.311.0/actions-runner-linux-arm64-2.311.0.tar.gz
   tar xzf ./actions-runner-linux-arm64.tar.gz
   
   # Configure runner (follow GitHub repository Settings > Actions > Runners)
   ./config.sh --url https://github.com/opd-ai/conspiracy --token YOUR_TOKEN
   
   # Install as systemd service
   sudo ./svc.sh install
   sudo ./svc.sh start
   ```

3. **Verify GPIO/SPI access**: Ensure runner service user has GPIO/SPI permissions
   ```bash
   sudo usermod -a -G gpio,spi $(whoami)
   ```

### Triggering Hardware Tests in CI

The hardware test workflow (`.github/workflows/hardware-test.yml`) is triggered manually via GitHub Actions UI:

1. Navigate to **Actions** tab in GitHub repository
2. Select **Hardware Tests** workflow
3. Click **Run workflow**
4. Choose:
   - **Test mode**: `tx` (transmitter) or `rx` (receiver)
   - **Spreading factors**: Comma-separated list (default: `7,10,12`)

For round-trip testing, trigger two workflow runs simultaneously:
- **Runner 1**: Test mode = `tx`
- **Runner 2**: Test mode = `rx`

## Troubleshooting

### Error: `/dev/spidev0.0` not found
- **Cause**: SPI interface not enabled
- **Fix**: Enable SPI in `raspi-config` or kernel boot parameters

### Error: Permission denied accessing `/dev/spidev0.0`
- **Cause**: User lacks SPI group membership
- **Fix**: Add user to `spi` group: `sudo usermod -a -G spi $USER`

### Error: Unknown chip version
- **Cause**: SPI wiring incorrect, chip not powered, or incompatible chipset
- **Fix**: 
  1. Verify physical wiring (MOSI, MISO, CLK, CS, GND, VCC)
  2. Check module power supply (3.3V, sufficient current)
  3. Confirm chipset is SX127x family (not SX126x, RFM69, etc.)

### Error: No packet received within 30s timeout
- **Cause**: Transmitter and receiver not synchronized, or RF signal too weak
- **Fix**:
  1. Verify both nodes use same frequency (868.1 MHz EU, 915 MHz US)
  2. Verify both nodes use same spreading factor
  3. Reduce distance between nodes (<5m for initial testing)
  4. Check antenna connection on both nodes

### Error: CRC error on received payload
- **Cause**: RF interference, misconfigured parameters, or antenna mismatch
- **Fix**:
  1. Move away from RF noise sources (Wi-Fi routers, Bluetooth devices)
  2. Verify bandwidth matches between TX and RX (default: 125 kHz)
  3. Check antenna tuning for target frequency (868/915 MHz)

## Test Coverage Summary

| Test | Purpose | Hardware Required | Duration |
|------|---------|-------------------|----------|
| `TestSX127xSPI_HardwareRegisterRead` | Verify SPI communication and chip version | 1 node | ~0.05s |
| `TestSX127xSPI_TxRx_RoundTrip` | Validate TX/RX at SF7/10/12 | 2 nodes | ~3-5 min |
| `TestSX127xSPI_ConfigurationValidation` | Verify parameter validation | 1 node | ~1s |

## Expected Test Results

Successful hardware tests confirm:
- ✅ SPI communication with SX127x chipset functional
- ✅ Chip version register matches expected values (0x12, 0x22, 0x21, 0x24)
- ✅ Frequency configuration (868.1 MHz EU, 915 MHz US) accepted
- ✅ Spreading factor configuration (SF7, SF10, SF12) accepted
- ✅ Bandwidth configuration (125, 250, 500 kHz) accepted
- ✅ Transmission completes without errors
- ✅ Reception detects packets with valid CRC
- ✅ RSSI measurement returns reasonable values (-30 to -120 dBm)

## Deferred Tests (Manual Only)

The following tests require manual execution due to CI infrastructure constraints:

- **Long-range field testing**: Validate 1-15 km range claims in real-world environments
- **Duty-cycle compliance**: Measure actual on-air time over 1-hour period
- **Multi-node mesh**: Test 10+ node network with packet forwarding
- **Interference resilience**: Measure packet loss in presence of Wi-Fi/Bluetooth/LTE

These tests should be performed during field trials (Priority 7 roadmap item) before production deployment.

## References

- [SX1276/77/78/79 Datasheet](https://www.semtech.com/products/wireless-rf/lora-core/sx1276)
- [periph.io Documentation](https://periph.io/)
- [LoRa Modem Calculator](https://www.semtech.com/design-support/lora-calculator)
