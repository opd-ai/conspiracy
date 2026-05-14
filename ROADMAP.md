# Goal-Achievement Assessment

## Project Context
- **What it claims to do**: Zero-configuration, community-owned mesh networking platform combining IEEE 802.11s Wi-Fi mesh with LoRa (sub-GHz) radio for long-range device discovery and routing coordination. Enables automatic peer discovery and network joining without manual configuration.
- **Target audience**: Community networks, disaster response teams, rural connectivity deployments, censorship-resistant communication, OpenWrt router and Linux SBC operators
- **Architecture**: 
  - **Data Plane**: IEEE 802.11s Wi-Fi mesh (54-300 Mbps, 50-200m urban range) with batman-adv layer-2 routing
  - **Control Plane**: LoRa sub-GHz radio (250 bps - 50 kbps, 1-15 km range) for beacons, routing hints, discovery
  - **Layer-3 Extensibility**: HintProvider/HintConsumer interface for overlays (cjdns, Yggdrasil)
- **Existing CI/quality gates**: None - no CI workflows, no automated tests, no build verification
- **Implementation Status**: **Design-only** - comprehensive 1,312-line specification exists in `docs/lora-mesh-design.md` (v1.0), but **zero Go code has been written**

## Goal-Achievement Summary

| Stated Goal | Status | Evidence | Gap Description |
|-------------|--------|----------|-----------------|
| **Zero-Configuration Join** - Automatic mesh discovery via LoRa beacons | ❌ Missing | No Go code exists | Complete implementation required: LoRa driver, BEACON frame codec, discovery state machine |
| **Hybrid Radio Architecture** - LoRa control + Wi-Fi data plane | ❌ Missing | No Go code exists | Requires: LoRa SPI/UART/USB driver, nl80211 Wi-Fi control, channel coordination |
| **batman-adv Integration** - Layer-2 mesh routing | ❌ Missing | No Go code exists | Requires: netlink integration for batctl operations, OGM listener, route table management |
| **Encrypted Control Protocol** - ChaCha20-Poly1305 AEAD with hybrid nonce | ❌ Missing | No Go code exists | Crypto subsystem unimplemented: AEAD encryption, HMAC auth, hybrid nonce with persistent reboot counter |
| **Proof-of-Work Anti-Flood** - SHA256 PoW for JOIN_REQ (16-bit difficulty) | ❌ Missing | No Go code exists | PoW challenge generation, validation, timestamp freshness checks not implemented |
| **Multi-Frequency Zoning** - 3-4 LoRa sub-bands for 250+ nodes/area | ❌ Missing | No Go code exists | Frequency zoning, hash-based zone assignment, bridge nodes not implemented |
| **Layer-3 Plugin System** - HintConsumer interface for Yggdrasil, cjdns | ❌ Missing | No Go code exists | HintBus pub/sub, plugin architecture, consumer registration not implemented |
| **Automatic Failover** - Mesh continues if LoRa control fails | ❌ Missing | No Go code exists | Batman-adv fallback detection, 802.11s-only mode, runtime config switching not implemented |
| **Key Rotation Protocol** - REKEY frames with replay prevention | ❌ Missing | No Go code exists | Key rotation state machine, replay counter tracking, generation counter monotonicity not implemented |
| **Hardware Abstraction** - SPI, UART, USB-Serial LoRa support | ❌ Missing | No Go code exists | PacketRadio interface design complete but no concrete implementations |
| **Cross-compilation** - OpenWrt (MIPS), ARM64, RISC-V targets | ❌ Missing | No `go.mod`, no build tooling | Cannot verify cross-compilation without code; pure-Go dependency constraints unverified |
| **5,000-node scalability** - Batman-adv with hard limits and monitoring | ❌ Missing | No Go code exists | Originator counting, OGM emission throttling, federation guidance not implemented |
| **Duty-cycle compliance** - EU 1%, US 4% LoRa regulatory limits | ❌ Missing | No Go code exists | TX scheduler, duty-cycle token bucket, adaptive TX intervals not implemented |
| **Systemd integration** - Daemon lifecycle management | ❌ Missing | No systemd unit file | Installation/deployment tooling absent |
| **OTA Updates** - Signed images, A/B partition, rollback watchdog | ❌ Missing | No Go code exists | Update mechanism, minisign verification, partition flipping not implemented |

**Overall: 0/15 goals fully achieved** (100% gap - project is design-only)

---

## Critical Findings

### 1. **No Implementation Exists**
The repository contains only design documentation (~1.3k-line specification) with **zero executable code**. This represents a 100% implementation gap across all stated features. The README claims "Implementation Status: This repository currently contains the design specification in `docs/lora-mesh-design.md`. The Go implementation will follow..." but provides no timeline or starter code.

### 2. **LoRa Driver Availability Risk**
No maintained pure-Go LoRa driver for SX127x/SX126x chipsets was found as of 2026-05-13 (searched: pkg.go.dev for "sx127x", "sx126x", "lora spi"; periph.io device registry; GitHub topic search "golang lora driver"). The design specifies `periph.io/x/conn/v3` for SPI abstraction, but [periph.io documentation](https://periph.io/) confirms this library provides only low-level SPI/GPIO primitives - not device-specific LoRa register control, packet encoding/decoding, or IRQ handling. The project will require **implementing a complete LoRa radio driver from scratch** (estimated 3,000+ lines of protocol code based on comparable C++ RadioHead library complexity).

### 3. **Batman-adv Scalability Claims Require Validation**
The design claims support for 5,000 nodes but community documentation suggests caution at scale. The [open-mesh.org FAQ](https://www.open-mesh.org/projects/open-mesh/wiki/FAQ) (accessed 2026-05-13) states: "It is not possible to give exact numbers … hundreds of nodes are not rare but thousands in a single broadcast domain is probably not wise." Real-world deployments (Freifunk community networks, documented in mailing list archives) typically report issues above 500-1,000 nodes per contiguous mesh on commodity hardware. The specification's claim of ~640 KB/sec OGM overhead at 10,000 nodes (~50% of 802.11n channel capacity) aligns with these warnings. The 5,000-node architectural ceiling requires field validation to confirm stability; conservative operational limit is likely 500-1,000 nodes on embedded hardware (OpenWrt routers) before federation becomes necessary.

### 4. **Security Implementation Complexity**
The design includes production-grade security features (hybrid nonce construction, persistent reboot counter, entropy audit, PoW anti-flood) that are non-trivial to implement correctly:
- Hybrid nonce construction requires persistent storage with atomic write-rename and failure detection
- Entropy audit at startup (blocking read from /dev/random) may cause 10-30s boot delays on embedded devices without hardware RNG
- Reboot counter persistence failure handling (LoRa subsystem must not start) requires complex initialization flow
- All crypto operations must be validated before first LoRa transmission to prevent catastrophic nonce reuse

### 5. **No Build/Test Infrastructure**
- No `go.mod` - cannot verify dependency versions, vulnerability status, or pure-Go constraint
- No CI/CD (GitHub Actions, etc.) - no automated build, test, lint, or cross-compilation verification
- No integration tests for critical security paths (CSPRNG failure detection, reboot counter persistence)
- No hardware-in-the-loop test infrastructure for LoRa/batman-adv

### 6. **Dependency Verification Gap**
The design specifies permissive-license dependencies but lacks verification:
- `github.com/brocaar/lorawan` (MIT) - LoRaWAN frame primitives exist but may not match raw LoRa needs
- `github.com/mdlayher/wifi` (MIT) - nl80211 bindings exist but mesh mode (802.11s) support unverified
- `github.com/vishvananda/netlink` (Apache-2.0) - batman-adv netlink interface support unverified
- No vulnerability scanning (Dependabot, Snyk) to track CVEs in chosen dependencies

---

## Roadmap

### Priority 1: Establish Minimum Viable Implementation Foundation
**Goal**: Create basic project structure, build system, and prove LoRa driver feasibility

- [x] **Initialize Go module** (`go.mod`) with Go 1.22+ and declare core dependencies
  - **Validation**: `go mod verify` succeeds; license verification via `go-licenses check --allowed_licenses=MIT,Apache-2.0,BSD-3-Clause ./...` (or equivalent tooling) confirms only permissive licenses
  - **Files**: Create `go.mod`, document dependency rationale
  - **Effort**: 1 day

- [x] **Implement basic project structure** matching design §5.2
  - **Validation**: Directory structure exists with placeholder files; `go build ./...` succeeds (even with empty functions)
  - **Files**: Create `cmd/conspiracyd/main.go`, `internal/{lora,wifi,batman,hint,autojoin,crypto,config}/` with stub `*.go` files
  - **Effort**: 2 days

- [x] **Research and prototype LoRa SPI driver** for SX127x chipset
  - **Validation**: Proof-of-concept code reads RegVersion register (0x42) via `periph.io/x/conn/v3/spi` and matches expected chip-specific value: 0x12 (SX1276), 0x11 (SX1272), 0x22 (SX1277), 0x21 (SX1278), or 0x24 (SX1279) per Semtech datasheets
  - **Files**: Create `internal/lora/sx127x.go` with register read/write primitives
  - **Effort**: 5 days (includes datasheet study, SPI protocol implementation, IRQ handling)
  - **Risk**: This is the highest-risk dependency - if pure-Go LoRa driver proves infeasible, entire project architecture collapses

- [x] **Implement PacketRadio interface with UDP test stub**
  - **Validation**: Unit tests use `net.UDPConn` as drop-in replacement for LoRa hardware; frame codec round-trip succeeds
  - **Files**: `internal/lora/driver.go` (interface), `internal/lora/driver_test.go` (UDP stub tests)
  - **Effort**: 3 days

- [x] **Set up CI/CD pipeline** (GitHub Actions)
  - **Validation**: `.github/workflows/ci.yml` runs on every PR: `go build`, `go test ./...`, `go vet`, cross-compilation for `GOARCH=mipsle,arm64,riscv64`
  - **Files**: Create `.github/workflows/ci.yml`
  - **Effort**: 1 day

### Priority 2: Implement Core Security Primitives
**Goal**: Establish cryptographic foundation with correct nonce handling and entropy validation

- [x] **Implement persistent reboot counter** with atomic write-rename
  - **Validation**: Integration test simulates filesystem failures (read-only mount, disk full); daemon refuses to start LoRa subsystem; logs CRITICAL error
  - **Files**: `internal/crypto/reboot_counter.go`, `internal/crypto/reboot_counter_test.go`
  - **Effort**: 4 days (includes failure handling, atomic write, recovery testing)

- [x] **Implement entropy audit** at daemon startup
  - **Validation**: Unit test mocks `crypto/rand` to return identical samples; entropy audit detects failure before first LoRa transmission
  - **Files**: `internal/crypto/entropy.go`, blocking read from `/dev/random`, continuous validation every 1,000 nonce generations
  - **Effort**: 3 days

- [x] **Implement hybrid nonce construction** for ChaCha20-Poly1305
  - **Validation**: Unit test verifies nonce uniqueness across 100k frames; integration test verifies nonce doesn't repeat after daemon restart (reboot counter incremented)
  - **Files**: `internal/crypto/nonce.go`, HMAC-SHA256 of `NodeID || reboot_counter || frame_seq || crypto/rand(8_bytes)`
  - **Effort**: 3 days

- [x] **Implement ChaCha20-Poly1305 AEAD encryption** for BEACON frames
  - **Validation**: Round-trip test encrypts/decrypts BEACON payload; HMAC verification succeeds; tampered ciphertext rejected
  - **Files**: `internal/crypto/aead.go`, HKDF key derivation from MESH_KEY
  - **Effort**: 4 days

- [x] **Implement anti-replay window** (RFC 6479, 128-bit bitmap)
  - **Validation**: Unit test accepts in-order frames, rejects replayed frames, handles out-of-order within window
  - **Files**: `internal/crypto/replay.go`
  - **Effort**: 2 days

### Priority 3: Implement LoRa Control Protocol
**Goal**: Establish basic LoRa frame exchange (BEACON, JOIN_REQ/ACK)

- [x] **Implement LoRa frame codec** (marshal/unmarshal)
  - **Validation**: Round-trip test for all frame types (BEACON, JOIN_REQ, JOIN_ACK, ROUTE_HINT, PING/PONG); size verification (<= 222 bytes on-wire)
  - **Files**: `internal/lora/frame.go`, header parsing, payload serialization
  - **Effort**: 5 days

- [x] **Implement BEACON transmission** with adaptive TX intervals
  - **Validation**: Integration test measures actual on-air time; verifies 1% duty-cycle compliance (EU 868 MHz) with 100 nodes
  - **Files**: `internal/lora/beacon.go`, duty-cycle token bucket scheduler
  - **Effort**: 4 days

- [x] **Implement PoW challenge** for JOIN_REQ anti-flood
  - **Validation**: Unit test verifies SHA256 PoW validation (16-bit difficulty); timestamp freshness check (±300s tolerance)
  - **Files**: `internal/crypto/pow.go`
  - **Effort**: 3 days

- [x] **Implement JOIN_REQ/JOIN_ACK state machine**
  - **Validation**: Integration test with 2 nodes (UDP stubs): new node sends JOIN_REQ, existing node responds with JOIN_ACK containing SSID/BSSID
  - **Files**: `internal/autojoin/join.go`
  - **Effort**: 5 days

### Priority 4: Integrate Wi-Fi and batman-adv
**Goal**: Establish data plane connectivity

- [x] **Implement nl80211 interface** for 802.11s mesh join
  - **Validation**: Integration test on Linux VM with virtual Wi-Fi interfaces; mesh interface created, joined to SSID
  - **Files**: `internal/wifi/mesh.go`, uses `github.com/mdlayher/wifi`
  - **Effort**: 6 days (includes nl80211 API research, mesh mode parameter tuning)

- [x] **Implement batman-adv netlink controller**
  - **Validation**: Integration test adds interface to `bat0`, verifies OGM emission via `batctl n` output
  - **Files**: `internal/batman/controller.go`, uses `github.com/vishvananda/netlink`
  - **Effort**: 5 days

- [x] **Implement batman-adv fallback detection** (802.11s-only mode)
  - **Validation**: Integration test on system without `CONFIG_BATMAN_ADV`; daemon starts, logs WARNING, operates in 802.11s-only mode
  - **Files**: `internal/batman/fallback.go`
  - **Effort**: 3 days

- [x] **Implement originator count monitoring** with 5,000-node hard limit
  - **Validation**: Unit test simulates 4,500 originators; verifies OGM emission stops; logs WARNING; hysteresis recovery at 4,200
  - **Files**: `internal/batman/scale_limit.go`, Prometheus gauge `batman_originator_count`
  - **Effort**: 3 days

### Priority 5: Layer-3 Extensibility (HintBus)
**Goal**: Enable plugin integration for overlay networks

- [x] **Implement HintBus pub/sub** with adaptive consumer buffers
  - **Validation**: Unit test with 3 consumers at different processing speeds; verifies buffer sizing, backpressure handling, no goroutine leaks
  - **Files**: `internal/hint/bus.go`, consumer registration, fan-out broadcast
  - **Effort**: 4 days

- [x] **Implement Yggdrasil HintConsumer plugin**
  - **Validation**: Integration test receives ROUTE_HINT, extracts NodeID→IP mapping, injects as Yggdrasil peer via admin API
  - **Files**: `plugins/yggdrasil/consumer.go`
  - **Effort**: 4 days (includes Yggdrasil admin API integration)

- [x] **Implement cjdns HintConsumer plugin**
  - **Validation**: Integration test receives ROUTE_HINT, injects as cjdns peer via admin interface
  - **Files**: `plugins/cjdns/consumer.go`
  - **Effort**: 4 days

### Priority 6: Production Readiness
**Goal**: Deployment tooling, monitoring, and resilience

- [x] **Implement TOML config parser** with validation
  - **Validation**: Unit test rejects invalid config (missing MESH_KEY, invalid frequency, TTL > 2); logs actionable error messages
  - **Files**: `internal/config/config.go`, uses `github.com/pelletier/go-toml/v2`
  - **Effort**: 2 days

- [x] **Implement Prometheus metrics exporter**
  - **Validation**: Integration test scrapes `/metrics` endpoint; verifies gauge availability: `lora_peer_count`, `batman_originator_count`, `lora_rssi_avg`, `duty_cycle_utilization`
  - **Files**: `cmd/conspiracyd/metrics.go`, uses `github.com/prometheus/client_golang`
  - **Effort**: 2 days

- [x] **Implement structured logging** with slog
  - **Validation**: Unit test verifies log level filtering, structured field output (JSON format), no sensitive data (MESH_KEY) logged
  - **Files**: Replace all `fmt.Printf` with `slog.Info/Warn/Error`
  - **Effort**: 2 days

- [x] **Create systemd unit file** and installation script
  - **Validation**: Manual test on Raspberry Pi; daemon starts on boot, survives crash (RestartSec=5s), logs to journald
  - **Files**: `deployments/systemd/conspiracyd.service`, `scripts/install.sh`
  - **Effort**: 1 day

- [x] **Implement multi-frequency zoning** for 250+ node deployments
  - **Validation**: Integration test with 300 simulated nodes (UDP stubs); verifies hash-based zone assignment, duty-cycle split across 3 frequencies, bridge node forwarding
  - **Files**: `internal/lora/zoning.go`
  - **Effort**: 5 days

- [x] **Implement OGM storm mitigation** during partition rejoin
  - **Validation**: Integration test simulates network split/rejoin (50 nodes each side); verifies OGM rate limiter (10 OGM/sec, burst=50), staggered re-injection (0-5s jitter)
  - **Files**: `internal/batman/storm_mitigation.go`
  - **Effort**: 3 days

- [ ] **Implement key rotation protocol** (REKEY frames)
  - **Validation**: Integration test rotates MESH_KEY across 3 nodes; verifies replay prevention (monotonic generation counter), old key invalidation after 24h
  - **Files**: `internal/crypto/rekey.go`
  - **Effort**: 6 days

### Priority 7: Testing and Validation
**Goal**: Comprehensive test coverage before field deployment

- [ ] **Write hardware-in-the-loop tests** for LoRa driver
  - **Validation**: Automated test on CI runner with LoRa HAT attached (SX1276); verifies TX/RX round-trip at SF7, SF10, SF12
  - **Files**: `internal/lora/driver_hw_test.go`, requires USB LoRa dongle or SPI HAT on CI runner
  - **Effort**: 4 days (includes CI runner hardware setup, test fixture wiring)
  - **Note**: May be deferred if CI hardware provisioning is infeasible; manual testing acceptable for v1.0

- [ ] **Write integration tests** for 3-node mesh topology
  - **Validation**: Docker Compose or VM-based test spawns 3 nodes; verifies JOIN sequence, route establishment, packet forwarding A→B→C
  - **Files**: `test/integration/three_node_test.go`, `test/integration/docker-compose.yml`
  - **Effort**: 5 days

- [x] **Write security regression tests** for nonce reuse prevention
  - **Validation**: Test suite verifies entropy audit failure detection, reboot counter persistence failure handling, continuous CSPRNG validation
  - **Files**: `internal/crypto/security_test.go`
  - **Effort**: 3 days

- [ ] **Run batman-adv scale testing** with 1,000+ nodes
  - **Validation**: Benchmark test measures OGM overhead (KB/sec), memory usage, CPU load at 500/1000/1500 originators; identifies actual stability ceiling
  - **Files**: `test/benchmark/scale_test.go`, requires VM cluster or cloud testbed
  - **Effort**: 5 days (includes testbed provisioning, data collection, analysis)
  - **Expected Outcome**: Confirm or revise 5,000-node claim based on real-world measurements

### Priority 8: Documentation and Community Enablement
**Goal**: Lower barrier to contribution and adoption

- [x] **Write CONTRIBUTING.md** with development workflow
  - **Validation**: New contributor can follow guide to build, test, and submit PR
  - **Files**: `CONTRIBUTING.md`, includes: build instructions, test runner commands, code style guide, PR template
  - **Effort**: 1 day

- [ ] **Write deployment guide** with hardware recommendations
  - **Validation**: Community member can replicate deployment on GL.iNet router or Raspberry Pi using guide
  - **Files**: `docs/deployment-guide.md`, includes: hardware profiles, OpenWrt compilation, LoRa module wiring, MESH_KEY provisioning
  - **Effort**: 2 days

- [ ] **Write federation guide** for >5,000 node deployments
  - **Validation**: Guide explains mesh island architecture, Yggdrasil overlay interconnect, layer-3 route propagation
  - **Files**: `docs/federation.md`
  - **Effort**: 2 days

- [x] **Create example configurations** for EU/US/AS regions
  - **Validation**: Config files pass validation (`conspiracyd -config example.toml -validate`)
  - **Files**: `examples/config-eu868.toml`, `examples/config-us915.toml`, `examples/config-as923.toml`
  - **Effort**: 1 day

---

## Risk Mitigation Recommendations

### 1. **LoRa Driver Development Risk** (Critical)
The absence of pure-Go LoRa drivers for SX127x/SX126x represents the highest project risk. The design assumes `periph.io` will suffice, but this library only provides SPI primitives—not radio-specific protocol logic.

**Mitigation**:
- **Option A (Recommended)**: Allocate 3-4 weeks for LoRa driver development; use RadioHead C++ library as reference implementation; contribute resulting driver to periph.io ecosystem as standalone package
- **Option B**: Pivot to USB-Serial LoRa modules (Dragino LG02, RAK811) with AT command interface; simpler integration via `go.bug.st/serial` but higher latency (100-200ms vs 10-20ms for SPI)
- **Option C**: Use CGo bindings to existing C/C++ LoRa libraries; violates pure-Go constraint but reduces development time by 50%; complicates cross-compilation

### 2. **Batman-adv Scalability Claim Overstatement** (High)
The 5,000-node claim contradicts community reports of instability above ~1,000 nodes. Field testing is required to validate actual operational ceiling.

**Mitigation**:
- Revise README to state "Supports networks up to 1,000 nodes per mesh island (field-tested); architecture accommodates 5,000 nodes with tuning (requires validation)"
- Document federation architecture prominently for deployments >500 nodes
- Implement proactive alerting at 750 originators (75% of conservative limit) to give operators time to plan federation
- Add metric `batman_recommended_federation_threshold` = 750 with Prometheus alert

### 3. **Security Implementation Complexity** (High)
Correct implementation of hybrid nonce construction, entropy audit, and reboot counter persistence is critical—errors lead to catastrophic AEAD nonce reuse.

**Mitigation**:
- Engage cryptography expert for design review before implementation (estimated cost: 4-8 hours consulting)
- Implement comprehensive fuzz testing for crypto subsystem (use `go test -fuzz`)
- Add startup self-test: generate 10k nonces, store in map[string]bool, verify zero collisions (deterministic check, no false positives); abort with CRITICAL error if collision detected
- Log crypto operations at TRACE level during initial releases for post-mortem analysis; **explicitly redact secrets** (MESH_KEY, derived keys, nonces, plaintext) - log only operation type, timestamp, NodeID, and success/failure status

### 4. **No Field Testing** (High)
Design is based on theoretical analysis without hardware validation. Actual LoRa range, duty-cycle behavior, and interference patterns unknown.

**Mitigation**:
- Establish 3-5 node pilot deployment in controlled environment before public release
- Partner with Freifunk, Guifi.net, or similar community network for beta testing
- Implement telemetry collection (opt-in) for real-world RSSI, packet loss, duty-cycle utilization data
- Budget 2-3 months for field testing iteration based on pilot feedback

### 5. **Dependency Vulnerability Tracking** (Medium)
No automated scanning for CVEs in dependencies (golang.org/x/crypto, netlink libraries).

**Mitigation**:
- Enable GitHub Dependabot vulnerability alerts
- Add monthly dependency audit to release checklist
- Subscribe to security advisories for critical dependencies (Go security mailing list, CVE feeds)

---

## Effort Summary

| Priority | Description | Estimated Effort (person-days) | Critical Path? |
|----------|-------------|--------------------------------|----------------|
| P1 | Minimum Viable Implementation | 12 days | Yes (LoRa driver POC) |
| P2 | Core Security Primitives | 16 days | Yes (blocks LoRa TX) |
| P3 | LoRa Control Protocol | 17 days | Yes |
| P4 | Wi-Fi + batman-adv Integration | 17 days | Yes |
| P5 | Layer-3 Extensibility | 12 days | No (can defer plugins) |
| P6 | Production Readiness | 26 days | Partial (config/metrics critical) |
| P7 | Testing and Validation | 17 days | Yes (scale testing validates claims) |
| P8 | Documentation | 6 days | No (can parallelize with dev) |
| **Total** | | **123 person-days (~6 months for 1 developer)** | |

**Realistic Timeline**: 
- Single developer: 6-7 months (accounting for integration debugging, iteration on field feedback)
- Two developers: 3-4 months (parallelizing LoRa driver + Wi-Fi/batman integration)
- Team of 3-4: 2-3 months (additional coordination overhead)

**Minimum Viable Product (MVP)** scope for initial release:
- P1 (foundation) + P2 (security) + P3 (LoRa protocol) + P4 (data plane) + subset of P6 (config, logging, systemd) = **~60 person-days (3 months for 1 developer)**
- Defer: Layer-3 plugins, multi-frequency zoning, key rotation, OTA updates to v1.1

---

## Open Questions Requiring Architect Decision

The design specification flags several decisions for resolution before implementation:

1. **Regional LoRa profiles**: Should v1.0 ship with pre-configured profiles (EU/US/AS/AU/IN) or require manual frequency configuration?
   - **Recommendation**: Ship profiles; reduces user error, improves out-of-box experience (2 days effort)

2. **GPS integration depth**: Manual config only, optional gpsd plugin, or built-in gpsd client?
   - **Recommendation**: Manual config for v1.0; defer gpsd integration to v1.1 after field feedback on privacy concerns

3. **IPv4 addressing**: APIPA, deterministic scheme (10.0.0.0/8 derived from NodeID), or IPv6-only?
   - **Recommendation**: Deterministic IPv4 for v1.0; APIPA collision probability unacceptable at scale; document migration path to IPv6-only in v2.0

4. **Multi-radio configuration**: Manual config only or auto-detect second radio as client AP?
   - **Recommendation**: Manual config for v1.0; auto-detection requires extensive hardware compatibility matrix testing

---

## Conclusion

The Conspiracy project has **comprehensive design documentation** (1,312 lines) outlining an ambitious zero-configuration mesh networking platform, but **zero executable code exists**. This represents a 100% implementation gap across all 15 stated feature goals.

**Key Priorities for Closing the Gap**:
1. **Prove LoRa driver feasibility** (Priority 1) - highest technical risk; failure here invalidates entire approach
2. **Implement security primitives correctly** (Priority 2) - single crypto error causes catastrophic failure
3. **Validate batman-adv scalability claims** (Priority 7) - 5,000-node claim appears overstated based on community reports
4. **Establish CI/CD and testing infrastructure** (Priority 1, 7) - prevent regressions, enable community contributions

**Recommended Next Steps**:
1. Allocate dedicated developer time to LoRa driver proof-of-concept (Priority 1) - do not proceed with broader implementation until this is validated
2. Create `go.mod` and minimal project structure to unblock parallel development
3. Engage cryptography expert for security design review before implementing Priority 2
4. Establish partnership with community network (Freifunk, Guifi.net) for field testing commitment

The project is technically ambitious but achievable with sustained development effort (~6 months for single developer to MVP). The design quality is high, but implementation must validate key assumptions (LoRa driver feasibility, batman-adv scaling limits) early to avoid wasted effort.
