# Deployment Guide

This guide provides step-by-step instructions for deploying Conspiracy mesh nodes on various hardware platforms. Follow the hardware-specific sections below based on your deployment platform.

---

## Table of Contents

1. [Hardware Selection](#hardware-selection)
2. [GL.iNet Router Deployment (OpenWrt)](#glinet-router-deployment-openwrt)
3. [Raspberry Pi Deployment](#raspberry-pi-deployment)
4. [Generic Linux SBC Deployment](#generic-linux-sbc-deployment)
5. [USB LoRa Module Deployment](#usb-lora-module-deployment)
6. [Configuration](#configuration)
7. [Mesh Key Provisioning](#mesh-key-provisioning)
8. [Testing and Validation](#testing-and-validation)
9. [Troubleshooting](#troubleshooting)

---

## Hardware Selection

### Recommended Hardware Profiles

| Profile | CPU | RAM | Storage | LoRa Module | Wi-Fi | Use Case |
|---------|-----|-----|---------|-------------|-------|----------|
| **OpenWrt Router** | MIPS 750 MHz+ | 128 MB+ | 16 MB flash | SPI HAT (SX127x) | 802.11n/ac | Outdoor nodes, gateway |
| **Raspberry Pi 4** | ARM64 1.5 GHz | 2 GB+ | 16 GB SD card | SPI HAT (SX1302) | USB Wi-Fi dongle | Indoor/outdoor, gateway |
| **ARM SBC** | ARM64 1 GHz+ | 1 GB+ | 8 GB eMMC | UART breakout | Built-in 802.11n | Compact nodes |
| **x86-64 PC** | x86-64 2 GHz+ | 4 GB+ | 32 GB SSD | USB-Serial | Built-in 802.11ac | Indoor, high-traffic gateway |

### LoRa Module Selection

- **SX1276/SX1278** (SPI): Best for outdoor nodes, long range (up to 15 km line-of-sight)
- **SX1302** (SPI): Higher throughput, lower power consumption (Raspberry Pi HAT available)
- **RAK811/RAK811** (USB-Serial): Easy integration, no SPI wiring required
- **Dragino LG02** (USB-Serial): Budget option, plug-and-play on x86 Linux

### Wi-Fi Adapter Requirements

- **802.11s mesh mode support** (required)
- **Monitor mode** (recommended for diagnostics)
- **Chipsets with good Linux support**:
  - Atheros ath9k/ath10k (best batman-adv compatibility)
  - MediaTek MT76 (good performance, modern)
  - Realtek RTL8812AU (USB, widely available)

**Test Wi-Fi adapter compatibility:**
```bash
iw list | grep -A 10 "Supported interface modes" | grep mesh
# Should output: "* mesh point" if 802.11s supported
```

---

## GL.iNet Router Deployment (OpenWrt)

### Supported Models

- **GL-AR750S** (Slate): Atheros AR9531 + AR9887, 128 MB RAM, USB port for LoRa
- **GL-MT300N-V2** (Mango): MediaTek MT7628, 128 MB RAM, compact form factor
- **GL-AXT1800** (Slate AX): WiFi 6, 512 MB RAM, high-performance gateway

### Prerequisites

1. **Flash latest OpenWrt firmware** (if not already running OpenWrt):
   - Download from https://openwrt.org/toh/gl.inet/start
   - Follow GL.iNet flashing instructions (web UI or TFTP recovery)

2. **Install batman-adv kernel module**:
   ```bash
   opkg update
   opkg install kmod-batman-adv batctl
   ```

3. **Install LoRa SPI driver** (if using SPI HAT):
   ```bash
   opkg install kmod-spi-dev kmod-spi-gpio-custom
   ```

### LoRa Module Wiring (SX1276 SPI HAT)

Connect LoRa module to router GPIO pins:

| SX1276 Pin | GL-AR750S GPIO | Wire Color (suggested) |
|------------|----------------|------------------------|
| VCC (3.3V) | 3.3V | Red |
| GND | GND | Black |
| MISO | GPIO 12 (SPI MISO) | Green |
| MOSI | GPIO 13 (SPI MOSI) | Blue |
| SCK | GPIO 14 (SPI CLK) | Yellow |
| NSS (CS) | GPIO 15 (SPI CS0) | Orange |
| RESET | GPIO 16 | White |

**Enable SPI interface:**
```bash
# Edit /etc/modules.d/90-spi
echo "spi-gpio-custom bus0=0,sck=14,miso=12,mosi=13,cs0=15,cs1=16" > /etc/modules.d/90-spi

# Reboot to load SPI driver
reboot

# Verify SPI device exists
ls -l /dev/spidev0.0
# Should show: crw------- 1 root root 153, 0 ...
```

### Build and Install Conspiracy

**Option A: Cross-compile on development machine (recommended)**

```bash
# On your dev machine (Linux/macOS):
git clone https://github.com/opd-ai/conspiracy.git
cd conspiracy

# Cross-compile for MIPS (GL.iNet AR750S)
GOARCH=mipsle GOOS=linux go build -o conspiracyd-mipsle ./cmd/conspiracyd

# Copy to router via SCP
scp conspiracyd-mipsle root@192.168.8.1:/usr/sbin/conspiracyd
scp examples/config-eu868.toml root@192.168.8.1:/etc/conspiracyd/config.toml
```

**Option B: Build on router (slower)**

```bash
# On the router:
opkg install golang git
git clone https://github.com/opd-ai/conspiracy.git
cd conspiracy
go build -o /usr/sbin/conspiracyd ./cmd/conspiracyd
```

### Configuration

```bash
# Create config directory
mkdir -p /etc/conspiracyd

# Generate mesh key
MESH_KEY=$(dd if=/dev/urandom bs=32 count=1 2>/dev/null | hexdump -ve '1/1 "%.2x"')
echo "Generated mesh key: $MESH_KEY"

# Edit /etc/conspiracyd/config.toml
cat > /etc/conspiracyd/config.toml <<EOF
[lora]
device        = "/dev/spidev0.0"
frequency_mhz = 868.1  # EU: 868.1, US: 915
spreading     = 10
bandwidth_khz = 125
mesh_key      = "hex:$MESH_KEY"

[wifi]
mesh_interface = "wlan0"
ssid           = "conspiracy-mesh"
channel        = 6

[batman]
interface = "bat0"
enabled   = true

[plugins]
yggdrasil = false
cjdns     = false
EOF
```

### Install Systemd Service (if using systemd)

```bash
cat > /etc/init.d/conspiracyd <<'EOF'
#!/bin/sh /etc/rc.common
START=99
STOP=10

USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/sbin/conspiracyd -config /etc/conspiracyd/config.toml
    procd_set_param respawn ${respawn_threshold:-3600} ${respawn_timeout:-5} ${respawn_retry:-5}
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}

stop_service() {
    killall conspiracyd
}
EOF

chmod +x /etc/init.d/conspiracyd
/etc/init.d/conspiracyd enable
/etc/init.d/conspiracyd start
```

### Validation

```bash
# Check daemon is running
ps | grep conspiracyd

# Check LoRa device accessible
ls -l /dev/spidev0.0

# Monitor logs
logread -f | grep conspiracyd

# Check batman-adv interface
batctl if
# Should show: wlan0: active

# Check mesh peers
batctl n
# After ~60 seconds, should show neighboring nodes
```

---

## Raspberry Pi Deployment

### Prerequisites

1. **Raspberry Pi 4** (2 GB RAM minimum)
2. **Raspbian OS Lite** (64-bit recommended)
3. **LoRa HAT** (RAK2245 or SX1302 recommended)
4. **External Wi-Fi dongle** (if using HAT that blocks built-in Wi-Fi antenna)

### System Setup

```bash
# Update system
sudo apt update && sudo apt upgrade -y

# Install batman-adv
sudo apt install batman-adv dkms batctl

# Enable SPI interface
sudo raspi-config
# Select: Interfacing Options → SPI → Enable

# Reboot
sudo reboot
```

### LoRa HAT Installation

**RAK2245 (SX1302) HAT:**

```bash
# Verify SPI device
ls -l /dev/spidev0.0

# Install RAK concentrator driver (optional, for diagnostics)
git clone https://github.com/RAKWireless/rak_common_for_gateway.git
cd rak_common_for_gateway
sudo ./install.sh

# Test LoRa module (optional)
cd ~/rak_common_for_gateway/lora/rak2245
sudo ./reset_lgw.sh start
sudo ./test_loragw_hal
```

### Build Conspiracy

```bash
# Install Go (if not present)
wget https://go.dev/dl/go1.22.0.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.22.0.linux-arm64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Clone and build
git clone https://github.com/opd-ai/conspiracy.git
cd conspiracy
go build -o conspiracyd ./cmd/conspiracyd
sudo cp conspiracyd /usr/sbin/
```

### Configuration

```bash
sudo mkdir -p /etc/conspiracyd

# Generate mesh key
MESH_KEY=$(openssl rand -hex 32)
echo "Generated mesh key: $MESH_KEY"

# Create config
sudo tee /etc/conspiracyd/config.toml <<EOF
[lora]
device        = "/dev/spidev0.0"
frequency_mhz = 868.1  # EU: 868.1, US: 915
spreading     = 10
bandwidth_khz = 125
mesh_key      = "hex:$MESH_KEY"

[wifi]
mesh_interface = "wlan0"
ssid           = "conspiracy-mesh"
channel        = 6

[batman]
interface = "bat0"
enabled   = true

[plugins]
yggdrasil = false
cjdns     = false
EOF
```

### Systemd Service

```bash
sudo tee /etc/systemd/system/conspiracyd.service <<EOF
[Unit]
Description=Conspiracy LoRa-Mesh Daemon
After=network.target

[Service]
Type=simple
ExecStart=/usr/sbin/conspiracyd -config /etc/conspiracyd/config.toml
Restart=on-failure
RestartSec=5s
User=root
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable conspiracyd
sudo systemctl start conspiracyd
```

### Validation

```bash
# Check service status
sudo systemctl status conspiracyd

# View logs
sudo journalctl -u conspiracyd -f

# Check batman-adv
sudo batctl if
sudo batctl n

# Check LoRa RX stats (after 60 seconds)
sudo journalctl -u conspiracyd | grep "BEACON received"
```

---

## Generic Linux SBC Deployment

For ARM SBCs like NanoPi R2S, Orange Pi, Rock Pi, etc.

### Prerequisites

- Linux kernel ≥ 5.10
- batman-adv kernel module compiled or available as DKMS
- LoRa module via UART or SPI

### UART LoRa Module Setup

Many SBCs have exposed UART pins (TX/RX/GND). Connect LoRa module via UART:

```bash
# Enable UART (device-specific, check your SBC's documentation)
# Example for NanoPi R2S (Armbian):
sudo armbian-config
# Select: System → Hardware → Enable UART

# Verify UART device
ls -l /dev/ttyS1
# or /dev/ttyAMA0 (Raspberry Pi)
# or /dev/ttyUSB0 (USB-Serial adapter)

# Configure conspiracyd for UART
[lora]
device        = "/dev/ttyS1"  # Adjust to your UART device
# ... rest of config
```

### Build and Configuration

Follow Raspberry Pi instructions above, adjusting device paths as needed.

---

## USB LoRa Module Deployment

**Best for:** x86-64 PCs, laptops, or any Linux system without SPI/UART.

### Supported Modules

- **RAK811 USB** (USB-Serial, AT command interface)
- **Dragino LG02** (USB-Serial gateway module)
- **LoStik** (USB-Serial, open-source)

### Setup

```bash
# Plug in USB LoRa module
lsusb
# Should show: "Future Technology Devices International ..." or similar

# Verify USB-Serial device
ls -l /dev/ttyUSB0
# Should show: crw-rw---- 1 root dialout ...

# Add user to dialout group (for non-root access)
sudo usermod -a -G dialout $USER
# Log out and back in for group change to take effect

# Configure conspiracyd for USB
[lora]
device        = "/dev/ttyUSB0"
frequency_mhz = 915  # US: 915, EU: 868.1
spreading     = 10
bandwidth_khz = 125
mesh_key      = "hex:YOUR_32_BYTE_HEX_KEY"
# ... rest of config
```

---

## Configuration

### Mesh Key Provisioning

**Generate a secure 256-bit mesh key:**

```bash
openssl rand -hex 32
# Output example: a1b2c3d4e5f6...
```

**Distribution to all nodes:**

1. **Pre-provision during imaging** (recommended for large deployments):
   - Embed mesh key in config file during SD card / flash imaging
   - Use Ansible / Salt / other config management for automated provisioning

2. **Manual provisioning** (small deployments):
   - SSH to each node, edit `/etc/conspiracyd/config.toml`
   - Set `mesh_key = "hex:YOUR_KEY"`

3. **Offline key transfer** (no network access):
   - Write key to USB stick, physically carry to each node
   - Script to read key from USB and update config

**CRITICAL SECURITY NOTE:**

- **ALL mesh nodes MUST share the same mesh key** to join the network.
- If a node is compromised, perform key rotation using REKEY protocol (see docs/lora-mesh-design.md §3.6).
- Store mesh key securely; loss requires rekey of entire network.

### Regional Configuration

#### Europe (EU 868 MHz)

```toml
[lora]
frequency_mhz = 868.1
duty_cycle_percent = 1  # 36 seconds per hour
```

#### United States (US 915 MHz)

```toml
[lora]
frequency_mhz = 915
duty_cycle_percent = 4  # 144 seconds per hour
```

#### Asia (AS 923 MHz)

```toml
[lora]
frequency_mhz = 923
duty_cycle_percent = 1  # Check local regulations
```

### Multi-Frequency Zoning (for >90 nodes per area)

If deploying >90 nodes in EU or >360 nodes in US, enable multi-frequency zoning:

```toml
[lora]
frequencies = [868.1, 868.3, 868.5]  # EU: 3-band zoning
bridge_mode = false  # Set true for gateway nodes with multiple radios
```

**Zone assignment is automatic** based on NodeID hash. No manual configuration needed.

---

## Testing and Validation

### Smoke Test (Single Node)

```bash
# Start daemon in foreground for debugging
sudo /usr/sbin/conspiracyd -config /etc/conspiracyd/config.toml

# Expected output:
# [INFO] Entropy audit passed
# [INFO] Reboot counter: 1
# [INFO] LoRa device initialized: /dev/spidev0.0
# [INFO] Listening for BEACON frames...

# Press Ctrl+C to stop
```

### Two-Node Test

1. Deploy two nodes (A and B) within LoRa range (~1-5 km depending on environment)
2. Start both daemons
3. Check logs on Node B:

```bash
sudo journalctl -u conspiracyd -f
# Within 60 seconds, should see:
# [INFO] BEACON received from NodeID=0x12345678 RSSI=-85dBm
# [INFO] JOIN_REQ sent to NodeID=0x12345678
# [INFO] JOIN_ACK received, joining mesh SSID=conspiracy-mesh
# [INFO] 802.11s mesh joined, channel=6
```

4. Check batman-adv peers:

```bash
sudo batctl n
# Should show Node A as neighbor:
# 00:11:22:33:44:55 0.500s (255) wlan0
```

5. Test connectivity:

```bash
# On Node A, check bat0 IPv6 address
ip -6 addr show bat0
# Example output: inet6 fe80::211:22ff:fe33:4455/64

# On Node B, ping Node A
ping6 fe80::211:22ff:fe33:4455%bat0
# Should receive ICMP replies
```

### Three-Node Test

1. Deploy Node C within range of A or B
2. Verify Node C joins mesh via BEACON from A or B
3. Test multi-hop routing:

```bash
# Topology: A ↔ B ↔ C (C not in direct range of A)
# On Node A, ping Node C via batman-adv routing
ping6 fe80::211:22ff:fe33:4466%bat0
# Packets should traverse A → B → C → B → A
```

---

## Troubleshooting

### LoRa Module Not Detected

**Symptom:** `Error: failed to open LoRa device /dev/spidev0.0`

**Solutions:**

1. **SPI not enabled:**
   ```bash
   # Raspberry Pi:
   sudo raspi-config → Interfacing → SPI → Enable
   # OpenWrt:
   Check /etc/modules.d/90-spi exists
   ```

2. **Wrong device path:**
   ```bash
   ls -l /dev/spi* /dev/ttyUSB* /dev/ttyS*
   # Verify device exists, update config.toml
   ```

3. **Permission denied:**
   ```bash
   sudo chmod 666 /dev/spidev0.0
   # Or run daemon as root
   ```

### Wi-Fi Mesh Not Forming

**Symptom:** `batctl n` shows no neighbors

**Solutions:**

1. **Wi-Fi adapter doesn't support 802.11s:**
   ```bash
   iw list | grep -A 10 "Supported interface modes" | grep mesh
   # If no output, adapter incompatible
   ```

2. **Channel mismatch:**
   - Ensure all nodes use same SSID and channel
   - Check `iwconfig wlan0` output on all nodes

3. **batman-adv not loaded:**
   ```bash
   sudo modprobe batman-adv
   sudo batctl if add wlan0
   sudo ip link set bat0 up
   ```

### No BEACON Received

**Symptom:** Logs show no BEACON frames after 5 minutes

**Solutions:**

1. **LoRa frequency mismatch:**
   - Verify all nodes use same frequency (EU: 868.1, US: 915)

2. **Duty-cycle saturation:**
   - Check Prometheus metric `duty_cycle_utilization`
   - If >0.9 (90%), reduce BEACON interval or enable multi-frequency zoning

3. **Out of range:**
   - LoRa range: 1-5 km urban, 5-15 km rural
   - Use RSSI monitoring: `journalctl -u conspiracyd | grep RSSI`
   - RSSI < -120 dBm indicates too far

### High Packet Loss

**Symptom:** `ping6` loss >10%

**Solutions:**

1. **batman-adv scale limit reached:**
   - Check `batctl o` (originator count)
   - If >750, plan federation (see docs/federation.md)

2. **Wi-Fi interference:**
   - Change channel: try 1, 6, 11 (least overlap)
   - Use 5 GHz if available (less congestion)

3. **LoRa collisions:**
   - Enable Listen-Before-Talk (LBT): set `lbt_enabled = true` in config

---

## Security Checklist

Before deploying to production:

- [ ] Generated unique mesh key (not default example key)
- [ ] Mesh key stored securely (encrypted storage, limited access)
- [ ] Regional frequency and duty-cycle configured correctly
- [ ] Firewall rules restrict bat0 interface (if needed)
- [ ] Systemd service runs as non-root user (if possible)
- [ ] Prometheus metrics endpoint restricted to localhost or VPN
- [ ] REKEY procedure documented for key rotation

---

## Next Steps

- **Monitor mesh health**: Set up Prometheus + Grafana dashboard
- **Plan scaling**: See docs/federation.md for >1,000 node deployments
- **Contribute**: Report deployment feedback to https://github.com/opd-ai/conspiracy/issues

---

## Support

- **GitHub Issues**: https://github.com/opd-ai/conspiracy/issues
- **Design Specification**: docs/lora-mesh-design.md
- **Federation Guide**: docs/federation.md
