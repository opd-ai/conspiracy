# Testing Methodology

## Overview

This document defines the testing strategy for Conspiracy mesh network deployments, covering test scenarios, acceptance criteria, hardware requirements, and measurement procedures for validating the platform's capabilities in controlled and field environments.

## Test Scenarios

### 1. Node Join/Leave Cycle
**Objective**: Verify zero-configuration automatic mesh joining via LoRa beacons

**Setup**:
- Minimum 3 nodes with LoRa + Wi-Fi hardware
- Controlled RF environment (indoor lab or isolated outdoor space)
- Nodes positioned 50-500m apart for varying RSSI conditions

**Procedure**:
1. Start Node A (bootstrap node) - broadcast BEACON
2. Wait 30s for BEACON stabilization
3. Start Node B - should discover Node A, send JOIN_REQ, receive JOIN_ACK
4. Verify Node B joins 802.11s mesh within 60s
5. Start Node C - should discover existing mesh, join via highest-RSSI peer
6. Verify batman-adv routing table includes all 3 nodes within 30s
7. Stop Node B gracefully
8. Verify Nodes A and C detect timeout (3 missed PINGs), update routes
9. Restart Node B
10. Verify automatic rejoin within 60s

**Acceptance Criteria**:
- JOIN latency (SCANNING → CONNECTED): <60s (95th percentile)
- Routing convergence after 3rd node: <30s
- Route failover after node departure: <3 minutes (3× 60s PING interval)
- Zero manual configuration required
- Join success rate: >95% across 20 trials

**Measurements**:
- Timestamp logs: BEACON received, JOIN_REQ sent, JOIN_ACK received, 802.11s association complete
- RSSI distribution during discovery phase
- batman-adv OGM propagation delay

---

### 2. Multi-Hop Packet Forwarding
**Objective**: Validate layer-2 mesh routing with batman-adv across 3+ hops

**Setup**:
- Linear topology: Node A ↔ Node B ↔ Node C ↔ Node D
- Nodes positioned such that A cannot directly reach C or D (RSSI < -100 dBm)
- Traffic generator: iperf3 for UDP/TCP throughput, ping for latency

**Procedure**:
1. Establish 4-node mesh in linear configuration
2. Verify batman-adv routing table: A has routes to B, C, D via B
3. TCP throughput test: Node A → Node D via iperf3 (30s duration)
4. UDP latency test: Node A → Node D ping flood (1000 packets, 10ms interval)
5. Measure packet loss at each hop (batman-adv stats via batctl)
6. Inject link failure: disable Node B's Wi-Fi interface
7. Verify route convergence: batman-adv re-routes A → C → D (if topology allows)

**Acceptance Criteria**:
- Packet loss: <5% for 3-hop path under normal conditions
- Latency (2-hop A→C): <50ms (median), <100ms (95th percentile)
- Latency (3-hop A→D): <80ms (median), <150ms (95th percentile)
- TCP throughput (3-hop): ≥10 Mbps (assuming 802.11n 54 Mbps theoretical)
- Route convergence after link failure: <60s

**Measurements**:
- Per-hop latency via traceroute equivalent
- batman-adv OGM sequence number progression
- Packet reordering rate
- Interface TX/RX stats from `batctl statistics`

---

### 3. LoRa Range and Duty-Cycle Validation
**Objective**: Measure actual LoRa communication range and verify regulatory duty-cycle compliance

**Setup**:
- 2 nodes with LoRa hardware (SX127x/SX126x chipset)
- Line-of-sight outdoor environment for maximum range test
- Urban/suburban environment for interference testing
- Spectrum analyzer or SDR (Software-Defined Radio) for duty-cycle monitoring
- GPS for distance measurement

**Procedure - Range Test**:
1. Configure both nodes: 868.1 MHz (EU), SF10, 125 kHz bandwidth, 14 dBm TX power
2. Position Node A at reference point (latitude/longitude recorded)
3. Position Node B at varying distances: 1 km, 2 km, 5 km, 10 km, 15 km
4. At each distance:
   - Node A transmits 100 BEACONs (60s interval)
   - Node B logs RSSI, SNR, packet reception success rate
   - Verify HMAC validation and decryption succeed
5. Identify maximum reliable range (>90% packet reception)

**Procedure - Duty-Cycle Compliance**:
1. Deploy 100-node simulation (UDP stubs on single host) with realistic BEACON intervals
2. Configure EU 868 MHz profile (1% duty-cycle = 36s TX per hour)
3. Run for 1 hour, log all TX events with timestamps and air-time
4. Calculate actual duty-cycle: `sum(air_time_ms) / 3600000 × 100%`
5. Verify TX scheduler enforces limit: actual duty-cycle ≤1.0%

**Acceptance Criteria**:
- Range (SF10, 14 dBm, line-of-sight): ≥4 km with >90% reception
- Range (SF10, 14 dBm, suburban): ≥1.5 km with >90% reception
- RSSI at 5 km: ≥-120 dBm (chipset sensitivity: -137 dBm for SF10)
- Duty-cycle compliance: actual ≤ 1.0% (EU), ≤ 4.0% (US) across 1-hour window
- Zero regulatory violations logged

**Measurements**:
- RSSI/SNR vs distance curve
- Packet error rate (PER) vs RSSI
- Air-time per frame type (BEACON: ~500ms at SF10)
- Cumulative TX time per hour

---

### 4. 100-Node Stress Test
**Objective**: Validate batman-adv scalability and identify OGM overhead ceiling

**Setup**:
- VM cluster or cloud testbed (AWS EC2, GCP, local KVM)
- 100 VM instances with virtual Wi-Fi interfaces (mac80211_hwsim)
- LoRa communication via UDP multicast (no hardware required)
- Prometheus + Grafana for metrics collection
- Topology: 10×10 grid with ≤4 neighbors per node

**Procedure**:
1. Spawn 100 daemon instances with staggered start (0-60s jitter)
2. Wait 10 minutes for mesh stabilization
3. Verify all nodes have batman-adv routes to all other nodes (100×99 = 9,900 routes)
4. Measure steady-state metrics for 1 hour:
   - CPU usage per node (%)
   - Memory usage per node (MB)
   - Network bandwidth (batman-adv OGM overhead in KB/sec)
   - Routing table update frequency (OGMs/sec per node)
5. Inject churn: gracefully stop/start 10 random nodes every 5 minutes
6. Measure partition healing time after network split (50 nodes each)

**Acceptance Criteria**:
- Mesh stabilization time (100 nodes): <10 minutes
- CPU usage per node: <5% on embedded hardware (1 GHz ARM Cortex-A53)
- Memory usage per node: <100 MB RSS
- Batman-adv OGM overhead: <100 KB/sec aggregate
- Route convergence after churn: <2 minutes
- Partition healing time: <5 minutes

**Measurements**:
- `batman_originator_count` gauge
- `lora_peer_count` gauge
- `batman_ogm_bytes_per_sec` counter
- CPU/memory via Prometheus node_exporter
- Route table size via `batctl o` output

---

### 5. Fallback Mode Validation
**Objective**: Verify 802.11s-only operation when batman-adv unavailable

**Setup**:
- 3 nodes on Linux systems without `CONFIG_BATMAN_ADV` kernel module
- Virtual Wi-Fi interfaces (mac80211_hwsim) supporting 802.11s but not batman-adv
- LoRa hardware or UDP stubs

**Procedure**:
1. Attempt daemon startup on system without batman-adv module
2. Verify WARNING log: "batman-adv kernel module unavailable; operating in 802.11s-only mode"
3. Verify LoRa discovery still operational (BEACON, JOIN_REQ/ACK)
4. Verify 802.11s mesh association succeeds (via `iw dev mesh0 station dump`)
5. Test packet forwarding via HWMP (802.11s native routing):
   - Ping Node A → Node C via Node B
   - Verify packets forwarded (HWMP establishes path)
6. Test broadcast limitation: Node A sends broadcast, verify Node C does not receive (HWMP limitation: no broadcast forwarding beyond 1 hop)

**Acceptance Criteria**:
- Daemon starts successfully without batman-adv
- WARNING logged exactly once at startup
- LoRa control plane fully operational
- 802.11s unicast forwarding works (ping A→C succeeds)
- Broadcasts limited to 1-hop (documented HWMP limitation)
- Prometheus metric `batman_enabled=0` exposed

**Measurements**:
- Log analysis: grep for "batman-adv unavailable"
- HWMP routing table via `iw dev mesh0 mpath dump`
- Packet capture (tcpdump) showing HWMP path request/reply frames

---

### 6. Crypto Correctness Validation
**Objective**: Verify hybrid nonce construction prevents reuse across all failure modes

**Setup**:
- Single node with instrumented crypto subsystem (trace logging enabled)
- Mock filesystem for reboot counter persistence failure simulation
- Mock `crypto/rand` for entropy failure simulation

**Procedure**:
1. **Entropy audit test**:
   - Configure mock `crypto/rand` to return identical samples
   - Attempt daemon startup
   - Verify CRITICAL error logged: "Entropy source failure detected; aborting to prevent nonce reuse"
   - Verify LoRa subsystem never starts (zero TX events)

2. **Reboot counter persistence test**:
   - Configure read-only filesystem for `/var/lib/conspiracyd/`
   - Attempt daemon startup
   - Verify CRITICAL error logged: "Failed to persist reboot counter; LoRa disabled"
   - Verify 802.11s and batman-adv continue operating (data plane unaffected)

3. **Nonce uniqueness test**:
   - Generate 100,000 nonces across 5 simulated reboots (reboot counter increments)
   - Store all nonces in `map[string]bool`
   - Verify zero collisions
   - Verify nonce format: 96-bit HMAC-SHA256 truncated output

4. **AEAD round-trip test**:
   - Encrypt 101-byte BEACON payload with ChaCha20-Poly1305
   - Transmit via LoRa (or UDP stub)
   - Receive and decrypt
   - Verify plaintext matches original
   - Tamper with 1 bit in ciphertext
   - Verify HMAC validation fails, frame dropped

**Acceptance Criteria**:
- Entropy audit detects failure with 100% reliability (zero false negatives)
- Reboot counter failure prevents LoRa TX but allows data plane
- Zero nonce collisions across 100k generations
- AEAD round-trip success rate: 100%
- Tampered ciphertext rejection rate: 100%

**Measurements**:
- Test pass/fail status
- Nonce generation performance: ops/sec
- AEAD encryption overhead: µs per frame

---

### 7. Security Regression Test Suite
**Objective**: Continuous validation of cryptographic security properties

**Tests**:
1. **Anti-replay window (RFC 6479)**:
   - Send frame sequence: [1,2,3,5,4,100,2]
   - Verify [1,2,3,5,4,100] accepted
   - Verify [2] rejected as replay

2. **HMAC validation**:
   - Generate valid BEACON with correct HMAC
   - Verify acceptance
   - Tamper with HMAC (flip 1 bit)
   - Verify rejection with log: "HMAC verification failed; dropping frame"

3. **Timestamp freshness (PoW anti-precomputation)**:
   - Generate JOIN_REQ with valid PoW but timestamp 10 minutes old
   - Verify rejection (tolerance: ±300s)

4. **NodeID collision detection**:
   - Simulate 2 nodes with identical NodeID but different MESH_KEYs
   - Verify first-contact pinning: second node rejected
   - Verify log: "NodeID collision detected; ignoring frames from duplicate"

**Acceptance Criteria**:
- All security tests pass with 100% success rate
- Zero false positives (legitimate frames rejected)
- Zero false negatives (malicious frames accepted)

---

## Hardware Requirements

### Minimum Test Configuration
- **Nodes**: 3× Raspberry Pi 4 Model B (4 GB RAM) or equivalent ARM SBC
- **LoRa Hardware**: 3× SX1276/SX1278 HAT modules (Waveshare SX1302, RAK2245, or Dragino GPS HAT)
- **Wi-Fi**: Built-in Raspberry Pi Wi-Fi (802.11ac with mesh mode support)
- **Networking**: Gigabit Ethernet for management network (out-of-band)
- **Power**: 3× USB-C power supplies (15W each)
- **GPS**: Optional for range testing (USB GPS receivers)

**Estimated Cost**: ~$300 USD (3× RPi + LoRa HATs + accessories)

### Extended Test Configuration (100-node simulation)
- **Compute**: VM cluster with 100 vCPUs total (AWS c5.metal instance or local KVM cluster)
- **Memory**: 16 GB RAM (100 nodes × 150 MB + overhead)
- **Network**: Virtual bridge with mac80211_hwsim (100 virtual Wi-Fi interfaces)
- **Storage**: 50 GB for logs and metrics

**Estimated Cost**: ~$50 USD/hour (AWS EC2 on-demand pricing for c5.metal)

---

## Measurement Procedures

### LoRa RSSI Collection
```bash
# On receiving node, tail daemon logs
journalctl -u conspiracyd -f | grep "BEACON received" | awk '{print $10}' > rssi_samples.txt

# Post-process: calculate distribution
cat rssi_samples.txt | datamash mean 1 sstdev 1 min 1 max 1
```

### Batman-adv OGM Overhead
```bash
# Monitor batman-adv multicast traffic on bat0 interface
tcpdump -i bat0 -nn -e 'ether proto 0x4305' -w ogm_capture.pcap &
sleep 300  # 5-minute capture
killall tcpdump

# Calculate bandwidth
capinfos ogm_capture.pcap | grep "Data byte rate"
```

### Duty-Cycle Verification
```bash
# Query Prometheus metrics endpoint
curl -s http://localhost:9090/metrics | grep lora_duty_cycle_utilization
# Expected output: lora_duty_cycle_utilization_percent{region="eu868"} 0.95

# Verify <1.0% for EU, <4.0% for US
```

### Route Convergence Time
```bash
# Node A: continuous ping to Node C
ping -i 0.2 10.0.0.3 > ping_log.txt &

# Node B: gracefully shutdown at T=60s
# Node B: restart at T=120s

# Post-process ping_log.txt:
# - Identify first packet loss after T=60s (route invalidation latency)
# - Identify last packet loss before T=120s+X (route recovery latency X)
```

---

## Test Environment Recommendations

### Controlled Indoor Lab
- **Advantages**: Reproducible RF conditions, easy node access, power/network infrastructure
- **Disadvantages**: Limited range (<50m typically), non-representative multipath/interference
- **Use Cases**: Functional validation, crypto testing, short-range integration tests

### Outdoor Field Site
- **Advantages**: Realistic range (1-15 km), real-world interference patterns, line-of-sight testing
- **Disadvantages**: Weather dependency, logistics (power, transport), RF licensing considerations
- **Use Cases**: Range validation, long-term stability testing, interference analysis

### Hybrid Approach (Recommended)
1. **Phase 1 (Lab)**: Functional correctness, crypto validation, 3-node integration (2 weeks)
2. **Phase 2 (Outdoor)**: Range testing, 5-10 node mesh in controlled site (1 week)
3. **Phase 3 (Beta)**: Partner with community network for 30-day continuous deployment (3 months)

---

## Acceptance Gates for Release

### v1.0 MVP Release
- ✅ 3-node integration test passes (JOIN, routing, forwarding)
- ✅ Crypto correctness test suite: 100% pass rate
- ✅ Duty-cycle compliance: <1% EU, <4% US across 1-hour window
- ✅ LoRa range: ≥1 km suburban with >90% reception at SF10
- ✅ Fallback mode test passes (802.11s-only without batman-adv)
- ✅ Security regression tests: 100% pass rate
- ✅ Documentation complete: README, deployment guide, testing methodology

### v1.1 Production Release
- ✅ 100-node stress test passes: <5% packet loss, <5% CPU per node
- ✅ Field deployment: 30-day continuous operation with community partner
- ✅ Telemetry data validates claims: range, duty-cycle, scalability
- ✅ Batman-adv scale limit validated: operational ceiling documented with field data
- ✅ Zero critical security vulnerabilities (CVE scan + expert audit)

---

## Community Partner Engagement

### Target Partners
1. **Freifunk** (Germany): https://freifunk.net - 400+ community networks, batman-adv expertise
2. **Guifi.net** (Spain): https://guifi.net - 37,000+ nodes, largest community network globally
3. **NYC Mesh** (USA): https://nycmesh.net - Urban mesh deployment experience
4. **LibreRouter Project**: https://librerouter.org - OpenWrt + mesh routing focus

### Beta Testing Request Template
```
Subject: Beta Testing Partnership - Conspiracy LoRa-Mesh Platform

Dear [Community Network Operator],

The Conspiracy project is seeking beta testing partners for our zero-configuration 
mesh networking platform combining 802.11s Wi-Fi with LoRa long-range discovery.

We offer:
- Pre-configured hardware nodes (3-5 units)
- Technical support during 90-day pilot
- Contribution of improvements back to open-source codebase (AGPL-3.0)

We request:
- Deployment in representative environment (urban/rural)
- Weekly feedback on stability, range, and usability
- Optional telemetry data sharing (opt-in, privacy-preserving)

Interested? Contact: [email] or GitHub: https://github.com/opd-ai/conspiracy

Best regards,
Conspiracy Development Team
```

---

## Revision History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-05-14 | Initial methodology document |
