# Conspiracy

## Description

Conspiracy is a zero-configuration, community-owned mesh networking platform that combines IEEE 802.11s Wi-Fi mesh with LoRa (sub-GHz) radio for long-range device discovery and routing coordination. The system enables automatic peer discovery and network joining without manual configuration, using LoRa as an out-of-band control channel while maintaining high-bandwidth data transfer over Wi-Fi mesh with batman-adv routing.

---

## Installation

Conspiracy is designed for deployment on OpenWrt routers and Linux single-board computers (ARM, RISC-V) equipped with LoRa radio modules.

### System Requirements

- Linux kernel ≥ 5.10 with batman-adv module support
- Go ≥ 1.22 for building from source
- LoRa hardware: SX127x or SX126x chipset via SPI, UART, or USB interface
- Wi-Fi adapter supporting 802.11s mesh mode

### Building

```bash
git clone https://github.com/opd-ai/conspiracy.git
cd conspiracy
go build -o conspiracyd ./cmd/conspiracyd
```

### Cross-compilation for OpenWrt (MIPS):

```bash
GOARCH=mipsle GOOS=linux go build -o conspiracyd ./cmd/conspiracyd
```

### Cross-compilation for ARM devices:

```bash
GOARCH=arm64 GOOS=linux go build -o conspiracyd ./cmd/conspiracyd
```

---

## Usage

### Configuration

Create a configuration file at `/etc/conspiracyd/config.toml`:

```toml
[lora]
device        = "/dev/spidev0.0"    # SPI: /dev/spidev0.0 | UART: /dev/ttyS1 | USB: /dev/ttyUSB0
frequency_mhz = 868.1               # EU: 868.1 | US: 915
spreading     = 10                  # SF10: ~980 bps, ~4 km range
bandwidth_khz = 125
mesh_key      = "hex:aabbcc..."     # 32-byte hex; MUST be changed

[wifi]
mesh_interface = "wlan0"
ssid           = "conspiracy-mesh"
channel        = 6

[batman]
interface      = "bat0"
enabled        = true

[plugins]
yggdrasil = true
cjdns     = false
```

### Generating a Mesh Key

```bash
openssl rand -hex 32
```

### Running as a System Service

Install as a systemd service:

```ini
[Unit]
Description=Conspiracy LoRa-Mesh Daemon
After=network.target

[Service]
ExecStart=/usr/sbin/conspiracyd -config /etc/conspiracyd/config.toml
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
systemctl enable conspiracyd
systemctl start conspiracyd
```

---

## Features

- **Zero-Configuration Join** - Nodes automatically discover and join mesh networks via LoRa beacons without manual configuration
- **Hybrid Radio Architecture** - LoRa provides 1-15 km range control channel while Wi-Fi mesh handles high-bandwidth data (54-300 Mbps)
- **batman-adv Integration** - Layer-2 mesh routing with B.A.T.M.A.N. Advanced protocol for efficient path selection
- **Encrypted Control Protocol** - ChaCha20-Poly1305 AEAD encryption protects LoRa beacons and routing hints with hybrid nonce construction
- **Proof-of-Work Anti-Flood** - JOIN_REQ includes SHA256-based PoW (16-bit difficulty) to prevent spam attacks
- **Multi-Frequency Zoning** - Supports 3-4 LoRa sub-bands for dense deployments (250+ nodes per area)
- **Layer-3 Plugin System** - HintConsumer interface enables integration with overlay networks (Yggdrasil, cjdns) without core modifications
- **Automatic Failover** - Mesh continues operating if LoRa control channel fails; batman-adv OGM protocol maintains routing
- **Key Rotation Protocol** - REKEY frames enable surgical key updates without network rebuild
- **Hardware Abstraction** - Supports SPI, UART, and USB-Serial LoRa modules (SX127x/SX126x chipsets)

---

## Requirements

### Hardware Support

| Profile | Example Hardware | Interface |
|---------|-----------------|-----------|
| OpenWrt router | GL.iNet GL-AR750S + RAK831 HAT | SPI |
| Raspberry Pi | RPi 4 + RAK2245/SX1302 HAT | SPI |
| ARM SBC | NanoPi R2S + SX1276 breakout | UART |
| USB LoRa | Any Linux + Dragino LG02/RAK811 USB | USB-Serial |
| RISC-V | Sipeed Lichee Pi 4A | SPI/UART |

### Dependencies

The project uses pure-Go libraries with permissive licenses:

- `go.bug.st/serial` (BSD-3-Clause) - Serial and USB-Serial LoRa module support
- `periph.io/x/conn/v3` (Apache-2.0) - SPI hardware abstraction for HAT modules
- `github.com/brocaar/lorawan` (MIT) - LoRaWAN frame encoding primitives
- `github.com/vishvananda/netlink` (Apache-2.0) - Interface management and batman-adv control
- `github.com/mdlayher/netlink` (MIT) - Generic netlink bindings

---

## Configuration

### Regional Frequency Bands

Configure LoRa frequency based on regulatory region:

- **Europe**: 868.1 MHz
- **United States**: 915 MHz
- **Asia**: 433 MHz or 920 MHz
- **Australia**: 915-928 MHz

### Scaling Limits

- **Maximum nodes per mesh**: 5,000 nodes (batman-adv architectural limit)
- **Larger deployments**: Use federated mesh islands with layer-3 overlay interconnect
- **Dense area support**: Multi-frequency zoning supports 250+ nodes per geographic area

### Duty-Cycle Compliance

The daemon enforces regional LoRa duty-cycle limits:

- **EU**: 1% duty cycle (36 seconds/hour at SF10)
- **US**: FCC Part 15 unlicensed operation
- Strict TX scheduler prevents regulatory violations

---

## Contributing

Conspiracy follows standard Go project conventions. The design specification is maintained in `docs/lora-mesh-design.md` with complete protocol details, security considerations, and implementation guidelines.

---

## License

This project is licensed under the GNU Affero General Public License v3.0 (AGPL-3.0). Network server operators must make modified source code available to users. See the LICENSE file for complete terms.
