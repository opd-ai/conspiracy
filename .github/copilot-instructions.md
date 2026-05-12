# Project Overview

Conspiracy is a zero-configuration, community-owned mesh networking platform that combines IEEE 802.11s Wi-Fi mesh with LoRa (sub-GHz) radio for long-range device discovery and routing coordination. The system enables automatic peer discovery and network joining without manual configuration, using LoRa as an out-of-band control channel while maintaining high-bandwidth data transfer over Wi-Fi mesh with batman-adv routing.

The project targets embedded Linux devices (OpenWrt routers, Raspberry Pi, ARM/RISC-V single-board computers) equipped with LoRa radio modules. The LoRa channel carries only compact routing hints, neighbor summaries, and device-discovery beacons — never bulk payload — keeping duty-cycle within regional limits (EU: 1%, US: 4%). The platform provides a clean `HintProvider`/`HintConsumer` interface allowing layer-3 overlays (cjdns, Yggdrasil) to consume routing hints without modifying the core daemon.

This repository currently contains the comprehensive design specification in `docs/lora-mesh-design.md`. The Go implementation will follow the architecture and build instructions outlined in the specification. The design incorporates critical security enhancements including hybrid nonce construction with persistent reboot counter, entropy audit at startup, ChaCha20-Poly1305 AEAD encryption, proof-of-work anti-flood mechanisms, and comprehensive scaling strategies for deployments up to 5,000 nodes per autonomous mesh.

## Technical Stack

- **Primary Language**: Go ≥ 1.22 (targeting cross-compilation for MIPS, ARM64, RISC-V)
- **Target OS**: Linux kernel ≥ 5.10 (batman-adv module, nl80211 generic netlink support)
- **Hardware**: LoRa radio modules (SX127x/SX126x chipsets via SPI, UART, or USB-Serial)
- **Frameworks**: 
  - `periph.io/x/conn/v3` (Apache-2.0) - SPI hardware abstraction for HAT modules
  - `go.bug.st/serial` (BSD-3-Clause) - Serial and USB-Serial LoRa module support
  - `github.com/vishvananda/netlink` (Apache-2.0) - Interface management and batman-adv control
  - `github.com/mdlayher/netlink` (MIT) - Generic netlink bindings
  - `github.com/mdlayher/wifi` (MIT) - nl80211-based Wi-Fi control without shelling out to iw
  - `github.com/brocaar/lorawan` (MIT) - LoRaWAN frame encoding primitives
  - `github.com/pelletier/go-toml/v2` (MIT) - TOML config file support
  - `golang.org/x/crypto` (BSD-3-Clause) - HKDF key derivation
  - `github.com/prometheus/client_golang` (Apache-2.0) - Metrics exposure
- **Testing**: Go's built-in testing package with table-driven tests; integration tests required for batman-adv fallback mode
- **Build/Deploy**: 
  - Cross-compilation targets: `GOARCH=mipsle GOOS=linux` (OpenWrt), `GOARCH=arm64`, `GOARCH=riscv64`
  - Systemd service deployment
  - Dual-partition A/B rootfs for OTA updates with rollback watchdog

## Code Assistance Guidelines

1. **Use Network Interface Types for Maximum Testability** (see also: Networking Best Practices section): When declaring network variables, ALWAYS use interface types instead of concrete types. This enhances testability and flexibility when working with different network implementations or mocks:
   - Never use `*net.UDPConn`, use `net.PacketConn` instead
   - Never use `*net.TCPConn`, use `net.Conn` instead
   - Never use `*net.TCPListener`, use `net.Listener` instead
   - Never use `*net.UDPAddr`, `*net.IPAddr`, or `*net.TCPAddr` - use `net.Addr` only instead
   - Never use a type switch or type assertion to convert from an interface type to a concrete type. Use the interface methods instead
   - Example: The planned `PacketRadio` interface (to be implemented in `internal/lora/driver.go`) will be satisfied by any LoRa backend while allowing `net.UDPConn` substitution in tests without hardware

2. **Implement Strict Security Validation Before Cryptographic Operations**: All cryptographic operations MUST be preceded by comprehensive validation. Before any nonce generation, key derivation, or encryption:
   - Perform blocking read from `/dev/random` or `getrandom(GRND_RANDOM)` to ensure kernel entropy pool initialization (may block 10-30s on first boot on embedded devices without hardware RNG)
   - Validate crypto/rand output by generating two 32-byte samples and verifying they differ (`bytes.Equal` check) before first LoRa transmission
   - Re-validate every 1,000 nonce generations with continuous monitoring - if identical samples detected, abort with CRITICAL error to prevent nonce reuse
   - If reboot counter write fails (disk full, filesystem corruption, read-only mount), daemon MUST NOT start LoRa subsystem; continue in 802.11s-only mode with batman-adv operational
   - Test write permission at startup with canary file to `/var/lib/conspiracyd/`
   - Log CRITICAL: "Failed to persist reboot counter; LoRa disabled to prevent nonce reuse" if persistence fails

3. **Use Hybrid Nonce Construction for AEAD to Prevent Catastrophic Nonce Reuse**: ChaCha20-Poly1305 nonces MUST use the documented hybrid construction pattern: `HMAC-SHA256(MESH_KEY, NodeID || reboot_counter || frame_seq || crypto/rand(8_bytes))[:12]`:
   - `reboot_counter`: 32-bit counter stored in persistent storage (NVRAM/flash at `/var/lib/conspiracyd/reboot_counter`), incremented on every daemon boot using atomic write-rename
   - `frame_seq`: 16-bit frame sequence number from common header
   - `crypto/rand(8_bytes)`: 64-bit random entropy per frame
   - This hybrid approach provides defense-in-depth: the reboot counter prevents nonce reuse across reboots, while crypto/rand provides per-frame uniqueness
   - **Critical assumption**: crypto/rand MUST produce varying output (validated via startup check per guideline #2). If CSPRNG completely fails (constant output), nonces can still repeat after frame_seq wraps (~65k frames) within the same reboot cycle
   - Single nonce reuse breaks all BEACON confidentiality - this is a critical security requirement

4. **Enforce Batman-adv Architectural Limits with Hard Caps and Federation Guidance**: The maximum supported deployment size is 5,000 nodes per autonomous mesh due to batman-adv OGM flooding overhead (~640 KB/sec at 10,000 nodes). Implementation MUST enforce:
   - Hard limit at 4,500 originators (10% safety margin)
   - At limit: Stop emitting OGMs entirely (disable batman-adv OGM broadcast; node becomes passive relay, continues forwarding traffic and relaying others' OGMs but stops advertising itself)
   - Log WARNING at 4,500: "Approaching batman-adv scale limit (4,500/5,000 peers). Plan federation migration."
   - Log INFO at 4,000 with guidance: "Network has 4,000 nodes. Consider deploying second mesh island (see docs/federation.md)"
   - Expose `batman_originator_count` gauge; alert at >3,500 (75% capacity)
   - Hysteresis recovery at 4,200 originators re-enables OGM emission
   - For larger deployments, document federated mesh islands with layer-3 interconnect (Yggdrasil overlay routing)

5. **Implement Goroutine-Safe Concurrency with Bounded Resources and Explicit Leak Prevention**: Structure the daemon around long-running goroutines with typed channels, following these patterns:
   - LoRa RX goroutine: Dedicated I/O that never blocks on processing; use buffered channel (64 entries) with non-blocking send; drop frames on overflow with metrics increment
   - Worker pool: Fixed size = 2 × runtime.NumCPU() for frame processing; prevents OOM under flood
   - Use sharded `sync.RWMutex` (16 shards by `NodeID & 0xF`) for peer table access to reduce lock contention
   - Implement watchdog goroutine: Sample `runtime.NumGoroutine()` every 60s; alert if >1,000 (indicates leak)
   - Channels should only be closed by their owner/sender; when multiple goroutines send to shared channels, use `sync.WaitGroup` to coordinate shutdown before closing
   - All state structures MUST have bounded size with explicit eviction policies (LRU, TTL-based, or tiered storage)
   - HintBus consumer channels use adaptive sizing: profile consumer latency at startup; size = `latency_ms × expected_hint_rate × 2`, capped at 256

6. **Avoid Hot-Path Allocations and Optimize Frame Serialization**: Performance-critical paths MUST minimize allocations to reduce GC pressure:
   - DO NOT use `time.After()` in loops or hot paths - it allocates a new timer on every call; use non-blocking select with default case or reusable timers
   - Marshal/unmarshal operations SHOULD use pre-allocated buffer pools (`sync.Pool` with 256-byte buffers)
   - Profile allocation rate with `go test -benchmem` first; target <1,000 allocs/sec
   - Consider zero-copy serialization via `unsafe.Slice` for header structs if profiling shows GC overhead >1% CPU
   - Implement proper backpressure: if queue full, drop low-priority frames (PING/PONG, ROUTE_HINT) before medium (BEACON) before high (JOIN_ACK)

7. **Implement Protocol Version Bumps for All Wire Format Changes**: When making wire-incompatible changes (adding fields, changing payload sizes), ALWAYS bump the protocol version number in the common header (§3.2):
   - Current version: `0x3` for v1.0 (49-byte BEACON payload with Timestamp field)
   - Version `0x2` was 45-byte BEACON payload without Timestamp
   - Prevents misinterpretation by older parsers
   - Document rolling upgrade behavior: specify transition period where new nodes can parse old protocol, validation rules during upgrade
   - Example: v1.0 nodes can parse v0.2 BEACONs during transition by skipping Timestamp validation; after 24h, enable strict validation via config flag

8. **Implement Tiered Storage and OGM Storm Mitigation for Scalability**: Prevent memory exhaustion and network storms during partition rejoins:
   - **Tiered peer storage**: Active peers (full metadata: 128 bytes, <5 min activity) + Passive peers (minimal: 16 bytes = NodeID + last-seen, inactive). Max: 1,000 active + 9,000 passive = 272 KB total
   - **OGM rate limiter**: Per-originator token bucket: 10 OGM/sec, burst=20 (normal); during partition rejoin (detect: peer count +50% within 10s), temporarily increase burst to 50 for 60s
   - **Staggered re-injection**: If churn rate >10 events/sec, add per-node random jitter (0-5s) before broadcasting first OGM to new partition
   - **ROUTE_HINT TTL hardening**: Max TTL=2 on transmission; probabilistic forwarding with `P = 1 / (lora_peer_count + 1)`; limits amplification from 258× to ~42×
   - **Netlink buffer sizing**: Set `SO_RCVBUF=1MB` for batman-adv OGM multicast receiver to prevent kernel buffer drops

9. **Complete Feature Implementations**: Always prefer completing the full implementation of any feature rather than leaving partial or placeholder code. When a complete implementation is not feasible, insert clear inline `TODO` comments describing what remains, why it was deferred, and any known constraints (e.g., `// TODO: Implement retry logic once the error categorization schema is finalized`). Never leave code in a silently incomplete state.

## Project Context

- **Domain**: Decentralized mesh networking for community-owned infrastructure; operates without central authority, gateway, or DNS servers. Target use cases include disaster response, rural connectivity, community networks, and censorship-resistant communication.
- **Architecture**: Hybrid radio architecture with clear data/control plane separation:
  - **Data Plane**: IEEE 802.11s Wi-Fi mesh (54-300 Mbps, 50-200m urban range) with batman-adv layer-2 routing
  - **Control Plane**: LoRa sub-GHz radio (250 bps - 50 kbps, 1-15 km range) for beacons, routing hints, discovery
  - **Layer-3 Extensibility**: Clean `HintProvider`/`HintConsumer` interface for overlays (cjdns, Yggdrasil) via in-process pub/sub HintBus
  - **Fallback mode**: If batman-adv unavailable, continue with 802.11s HWMP routing only (no layer-2 broadcast forwarding beyond 1 hop)
- **Key Directories**:
  - `docs/lora-mesh-design.md` - Comprehensive design specification (v1.0) with protocol details, security model, and implementation guidance
  - `cmd/conspiracyd/` - Daemon entry point (to be implemented)
  - `internal/lora/` - LoRa radio driver and frame codec (SPI/UART/USB abstraction, duty-cycle scheduler, LBT)
  - `internal/wifi/` - nl80211 / wpa_supplicant control (802.11s mesh join/leave)
  - `internal/batman/` - batman-adv netlink control (batctl operations, OGM event listener)
  - `internal/hint/` - HintBus, HintProvider, HintConsumer interfaces
  - `internal/autojoin/` - Discovery state machine
  - `internal/crypto/` - HMAC helpers, key management
  - `plugins/` - HintConsumer plugins (cjdns, yggdrasil)
- **Configuration**: TOML config at `/etc/conspiracyd/config.toml`:
  - LoRa: device path (SPI/UART/USB), frequency (regional: EU 868.1 MHz, US 915 MHz), spreading factor (SF7-SF12), 256-bit mesh key
  - Wi-Fi: mesh interface, SSID, channel
  - Batman-adv: interface (bat0), enable/disable flag
  - Plugins: Yggdrasil, cjdns enable flags
  - Duty-cycle limits: Regional (EU 1% = 36s/hour, US 4% = 144s/hour)
  - Multi-frequency zoning for 250+ nodes per area (3-4 sub-bands)
- **Critical Security Requirements**:
  - ChaCha20-Poly1305 AEAD encryption for all BEACON frames with hybrid nonce construction
  - HMAC-SHA256 truncated to 96 bits for frame authentication
  - Proof-of-Work (SHA256, 16-bit difficulty) for JOIN_REQ anti-flood
  - RFC 6479-style anti-replay window (128-bit bitmap per NodeID)
  - Key rotation protocol (REKEY frames) with replay prevention via monotonic generation counter
  - NodeID collision detection with first-contact pinning
  - Fixed-length BEACON padding (32 bytes) for traffic analysis resistance
  - Timestamp validation (±300s tolerance) for PoW anti-precomputation
- **License**: GNU Affero General Public License v3.0 (AGPL-3.0) - network use is distribution; operators running modified versions MUST publish source

## Quality Standards

- **Testing Requirements**: 
  - Use Go's built-in testing package with table-driven tests for business logic functions
  - Integration tests MUST validate batman-adv fallback mode: kernel without `CONFIG_BATMAN_ADV`, verify daemon starts, log contains "batman-adv unavailable; operating in 802.11s-only mode" WARNING
  - Test 3-node triangle topology with packet forwarding in fallback mode
  - Security tests: CSPRNG failure detection, reboot counter persistence failure, entropy audit, nonce uniqueness validation
  - Performance tests: Profile allocation rate (<1,000 allocs/sec target), goroutine leak detection (alert at >1,000), duty-cycle enforcement
  - Hardware-in-the-loop tests for LoRa drivers (SPI/UART/USB) with mock hardware using `net.UDPConn` substitution
- **Code Review Criteria**: 
  - All network types use interfaces (net.Conn, net.PacketConn, net.Addr) - no concrete types
  - Cryptographic operations preceded by entropy validation
  - Goroutine spawning includes explicit shutdown coordination (context cancellation, WaitGroup)
  - Bounded data structures with documented eviction policies
  - Protocol version bumped for wire format changes with upgrade path documented
  - Error handling includes context (structured logging with slog)
  - No CGo dependencies (pure Go for cross-compilation)
- **Documentation Standards**: 
  - Update `docs/lora-mesh-design.md` for protocol changes with rationale and security analysis
  - Document all tunable parameters with ranges and performance implications
  - Include deployment guidance for scale limits (federation at >5,000 nodes)
  - Regional compliance tables (LoRa frequency, duty-cycle, spreading factor)
  - Wire format diagrams for all frame types with byte-level field specifications
  - Security model documentation: threat model, attack surface, mitigation strategies
  - Append to Appendix A (Consistency Review Notes) for cross-reference harmonization
  - Use RFC 2119 keywords (MUST/MUST NOT/SHOULD) for normative requirements

## Networking Best Practices (for Go projects)

When declaring network variables, always use interface types (see Code Assistance Guideline #1 for detailed rationale):
- Never use `*net.UDPAddr`, `*net.IPAddr`, or `*net.TCPAddr`. Use `net.Addr` only instead.
- Never use `*net.UDPConn`, use `net.PacketConn` instead
- Never use `*net.TCPConn`, use `net.Conn` instead
- Never use `*net.TCPListener`, use `net.Listener` instead
- Never use a type switch or type assertion to convert from an interface type to a concrete type. Use the interface methods instead.

This approach enhances testability and flexibility when working with different network implementations or mocks.
