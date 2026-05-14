# Implementation Gaps — 2026-05-14

This document analyzes the gaps between Conspiracy's stated goals (documented in README.md and design specification) and the current implementation status. Each gap includes the stated goal, actual implementation status, user impact, and concrete steps to close the gap.

**Update from Previous Version (2026-05-14):** This revision reflects actual implementation progress discovered during functional audit. Previous GAPS.md (dated 2026-05-14 earlier) assumed zero implementation; audit reveals **substantial foundational work completed**: crypto subsystem ~80% complete (nonce generation, entropy audit, reboot counter persistence operational with passing tests), LoRa hardware abstraction functional (SX127x SPI driver, factory pattern, UDP test stub), cross-compilation verified for all targets. Primary gaps are integration (main daemon, state machines) and missing subsystems (batman-adv, Wi-Fi, HintBus, AEAD encryption, duty-cycle enforcement).

---

## Gap 1: Main Daemon Integration and End-to-End Initialization

**Stated Goal**: Executable daemon that initializes subsystems, loads configuration, starts LoRa/Wi-Fi radios, and enters operational mesh mode. Users run `conspiracyd -config /etc/conspiracyd/config.toml` and daemon joins mesh automatically (README Installation section, systemd service example lines 84-96).

**Current State**: cmd/conspiracyd/main.go:8-11 contains only stub printing "Implementation in progress" and exits. No config parser invocation, no subsystem initialization, no LoRa RX goroutine, no main event loop. All foundational components exist (crypto, LoRa driver, PacketRadio interface) but are disconnected. Daemon is non-functional.

**Impact**: Software cannot be deployed. Users cannot test mesh functionality even with hardware. All implementation work to date (crypto primitives, LoRa driver) is inaccessible without integration layer. Community cannot contribute or field-test.

**Closing the Gap**:
1. **Configuration Parser (2 days)**: Implement internal/config/config.go TOML parser using github.com/pelletier/go-toml/v2 (go.mod:17). Define Config struct matching README example (lines 52-72): LoRa{Device, FrequencyMHz, Spreading, BandwidthKHz, MeshKey}, WiFi{MeshInterface, SSID, Channel}, Batman{Interface, Enabled}, Plugins{Yggdrasil, CJDNS}. Validate: mesh_key hex-encoded 32 bytes, frequency in regional bands (EU 868.1, US 915), spreading 7-12, TTL ≤2. Unit test rejects invalid config with actionable errors.

2. **Main Daemon Initialization (3 days)**: Implement main.go initialization sequence: (a) Parse config via internal/config.Load(), (b) Initialize structured logging with slog JSON handler, (c) Run entropy audit crypto.EntropyAudit() (blocks until /dev/random ready), (d) Load reboot counter rc := crypto.NewRebootCounter("/var/lib/conspiracyd"), increment for this boot, (e) Create LoRa radio via factory radio := lora.NewRadio(cfg.LoRa), (f) Initialize nonce generator ng := crypto.NewNonceGenerator(cfg.LoRa.MeshKey, nodeID, rc.Value()), (g) Start LoRa RX goroutine listening for BEACON/JOIN_REQ frames, (h) Enter main loop with signal handling (SIGINT/SIGTERM graceful shutdown). Integration test: daemon starts with UDP radio, logs "Ready", listens on UDP port, exits cleanly on SIGINT.

3. **Graceful Shutdown (1 day)**: Implement shutdown coordinator using context.WithCancel(): (a) Propagate cancellation to all goroutines (LoRa RX, TX scheduler, HintBus), (b) Wait for goroutines to complete via sync.WaitGroup, (c) Close LoRa radio connection, (d) Flush logs. Unit test: send SIGTERM, verify all goroutines exit within 5 seconds, no leaked resources.

**Validation**: Smoke test on Raspberry Pi: daemon starts with SPI LoRa module, parses config, logs "Entropy audit passed", increments reboot counter to 1, creates SX127x radio, listens for BEACONs, exits cleanly on Ctrl+C. No mesh joining yet (requires Gap 2 FSM), but subsystem integration proven.

**Estimated Effort**: 6 person-days. **Priority: CRITICAL** — blocks all end-to-end testing.

---

## Gap 2: Auto-Join State Machine (BEACON RX → JOIN_REQ/ACK → 802.11s)

**Stated Goal**: "Zero-Configuration Join — Nodes automatically discover and join mesh networks via LoRa beacons without manual configuration" (README.md Feature #1). Design §4.2 specifies 5-state FSM: INIT → SCANNING (collect BEACONs for 30s, rank by RSSI) → JOINING (send JOIN_REQ with PoW, await JOIN_ACK timeout 30s) → CONNECTED (monitor peer liveness, re-BEACON every 60s) → FAILED (exponential backoff 60s-600s, retry).

**Current State**: internal/autojoin/join.go:1-2 is package stub. No FSM implementation. No BEACON parsing. No JOIN_REQ transmission. No JOIN_ACK reception handling. BEACON frame codec missing (no marshal/unmarshal for design §3.3 wire format: Header 13 bytes + Payload 101 bytes encrypted). PoW challenge generation/validation missing. Cannot discover peers or obtain SSID/BSSID for 802.11s association.

**Impact**: Core value proposition non-functional. Nodes cannot auto-join mesh. Manual SSID/BSSID configuration required, defeating zero-config promise. Target users (disaster response, non-technical operators) cannot deploy without networking expertise.

**Closing the Gap**:
1. **LoRa Frame Codec (3 days)**: Implement internal/lora/frame.go with marshal/unmarshal for 7 frame types (BEACON, JOIN_REQ, JOIN_ACK, ROUTE_HINT, PING, PONG, REKEY). Wire format per design §3.2: Header (FrameType 1 byte, Version 1 byte, NodeID 4 bytes, Timestamp 4 bytes, FrameSeq 2 bytes, HMAC 12 bytes) + Payload (variable, encrypted if FrameType=BEACON). Size verification: reject frames >222 bytes (LoRa payload limit). Unit test: round-trip all frame types, verify byte-level format, detect truncated/oversized frames.

2. **BEACON Transmission (2 days)**: Implement internal/lora/beacon.go Transmitter: (a) Generate BEACON frame (NodeID, SSID hint, GPS optional, capabilities bitmask) every 60s, (b) Encrypt payload with ChaCha20-Poly1305 using nonce from NonceGenerator, (c) Compute HMAC-SHA256 over header+ciphertext, truncate to 12 bytes, (d) Marshal to wire format, (e) Send via PacketRadio.Send(). No duty-cycle enforcement yet (deferred to Gap 7). Integration test: UDP radio transmits BEACON every 60s, receiver parses NodeID correctly.

3. **JOIN_REQ/ACK State Machine (5 days)**: Implement internal/autojoin/join.go FSM:
   - **SCANNING**: Collect BEACONs for 30s, store {NodeID, RSSI, SSID, Timestamp} in slice, rank by RSSI descending, select strongest peer.
   - **JOINING**: Generate PoW nonce (SHA256 hash of NodeID || Timestamp || nonce has 16 leading zero bits, timestamp freshness ±300s), send JOIN_REQ(NodeID, PoW nonce, capabilities), set 30s timeout for JOIN_ACK, retry 3 times with exponential backoff (30s, 60s, 120s).
   - **CONNECTED**: On JOIN_ACK received, extract SSID/BSSID/channel, trigger 802.11s association (requires Gap 3 nl80211), log "Joined mesh SSID={ssid}", monitor peer liveness (expect BEACON every 120s, mark FAILED if >300s silence).
   - **FAILED**: Log "Join failed after 3 attempts", exponential backoff 60s-600s, return to SCANNING.
   
   Unit test: mock LoRa radio, inject BEACON, verify FSM transitions INIT→SCANNING→JOINING, sends JOIN_REQ with valid PoW, transitions CONNECTED on JOIN_ACK, transitions FAILED on timeout. Integration test: 2 daemons with UDP radios, Node B discovers Node A BEACON, sends JOIN_REQ, Node A validates PoW and responds JOIN_ACK with SSID "test-mesh", Node B logs "Joined mesh".

**Validation**: 2-node integration test (Raspberry Pi + GL.iNet router, UDP stubs): Node A starts first, broadcasts BEACON every 60s. Node B starts 10s later, enters SCANNING, collects Node A BEACON, sends JOIN_REQ with PoW. Node A validates PoW (<5s computation), responds JOIN_ACK. Node B logs "Joined mesh SSID=conspiracy-mesh". No 802.11s association yet (requires Gap 3), but discovery+JOIN handshake proven.

**Estimated Effort**: 10 person-days (3 frame codec + 2 BEACON TX + 5 FSM). **Priority: CRITICAL** — enables basic mesh formation.

---

## Gap 3: Wi-Fi Mesh Control and Batman-adv Integration

**Stated Goal**: "batman-adv Integration — Layer-2 mesh routing with B.A.T.M.A.N. Advanced protocol for efficient path selection" (README Feature #3). Design §2.4 specifies nl80211 802.11s mesh interface creation + batman-adv enrollment. Nodes forward traffic beyond 1-hop via layer-2 OGM routing. Fallback mode: if batman-adv unavailable, continue with 802.11s HWMP routing only.

**Current State**: internal/wifi/mesh.go:1-2 is package stub (no nl80211 interface creation). internal/batman/controller.go:1-2 is package stub (no netlink batctl operations, no OGM monitoring). Dependencies exist (github.com/mdlayher/wifi@v0.7.2, github.com/vishvananda/netlink@v1.3.1 in go.mod) but unused. No layer-2 routing. Nodes cannot forward traffic beyond direct Wi-Fi neighbors.

**Impact**: Mesh network does not form. Packets cannot traverse multi-hop paths. Deployment limited to single-hop star topology (all nodes must be within direct Wi-Fi range of each other, defeats mesh architecture). No resilience to node failures (no alternate paths).

**Closing the Gap**:
1. **nl80211 Mesh Interface Creation (4 days)**: Implement internal/wifi/mesh.go using github.com/mdlayher/wifi: (a) Create mesh interface via nl80211.Client.NewInterface(name="mesh0", iftype=nl80211.InterfaceTypeMeshPoint), (b) Join mesh SSID via nl80211.Client.JoinMesh(ssid, channel) with parameters from JOIN_ACK, (c) Configure MESH_CONF (mesh_ttl=31, mesh_hwmp_rootmode=4, mesh_hwmp_max_preq_retries=4), (d) Bring interface up via ip link set mesh0 up. Integration test: creates virtual Wi-Fi interface using cfg80211_hwsim kernel module, joins mesh SSID "test", verifies interface state via nl80211.Client.Interfaces().

2. **Batman-adv Netlink Controller (5 days)**: Implement internal/batman/controller.go using github.com/vishvananda/netlink: (a) Probe for batman-adv kernel module via /sys/module/batman_adv/ existence check, (b) If missing: log WARNING "batman-adv unavailable; operating in 802.11s-only mode", set fallback flag, skip remaining steps, (c) Create bat0 interface via netlink.LinkAdd(&netlink.GenericLink{LinkAttrs: netlink.LinkAttrs{Name: "bat0", MTU: 1500}, LinkType: "batadv"}), (d) Add mesh0 to bat0 via netlink.LinkSetMaster(mesh0, bat0), (e) Subscribe to RTNLGRP_BATMAN_ADV netlink multicast group for event-driven OGM updates (<100ms latency vs 0-5s polling), (f) Parse OGM events and increment originator count. Integration test: adds wlan0 to bat0, verifies bat0 interface exists, confirms OGM emission via netlink events (requires batman-adv kernel module or mock).

3. **Originator Count Monitoring and Scale Limits (3 days)**: Implement internal/batman/scale_limit.go: (a) Maintain originator counter from netlink OGM events (increment on new originator, decrement on timeout), (b) At 4,500 originators: disable OGM emission entirely via netlink BATADV_CMD_SET_MESH with originator_interval=0 (node becomes passive relay, forwards traffic but stops advertising routes), (c) Hysteresis recovery: re-enable OGM emission when count drops to 4,200, (d) Log warnings: INFO at 4,000 ("Network has 4,000 nodes. Consider deploying second mesh island, see docs/federation.md"), WARNING at 4,500 ("Approaching batman-adv scale limit (4,500/5,000 peers). Plan federation migration."), (e) Expose Prometheus gauge batman_originator_count with alert threshold >3,500 (75% capacity). Unit test: simulates 4,500 originators, verifies OGM stops, logs WARNING, hysteresis recovery at 4,200.

**Validation**: 3-node topology (Raspberry Pi x3, 802.11s mesh, batman-adv enabled): Node A joins mesh, batman controller adds mesh0 to bat0, OGM emission starts. Node B joins, discovers Node A via OGM, routing table populates. Node C joins, discovers both A+B. Ping A→C succeeds via B relay (2-hop path). Packet forwarding verified with tcpdump showing batman-adv encapsulation. Fallback test: disable CONFIG_BATMAN_ADV in kernel, daemon starts, logs WARNING, 802.11s HWMP routing operational (ping A→B succeeds, A→C fails due to lack of multi-hop forwarding).

**Estimated Effort**: 12 person-days (4 nl80211 + 5 batman controller + 3 scale limits). **Priority: HIGH** — required for multi-hop mesh routing.

---

## Gap 4: ChaCha20-Poly1305 AEAD Encryption Implementation

**Stated Goal**: "Encrypted Control Protocol — ChaCha20-Poly1305 AEAD encryption protects LoRa beacons and routing hints with hybrid nonce construction" (README Feature #4). Design §3.6 specifies AEAD encryption of BEACON payload using nonce from NonceGenerator, HKDF key derivation from MESH_KEY, HMAC-SHA256 frame authentication.

**Current State**: internal/crypto/aead.go:1-3 contains only package declaration. No Encrypt()/Decrypt() functions. Hybrid nonce generation operational (internal/crypto/nonce.go passes tests), entropy audit functional (internal/crypto/entropy.go), reboot counter persistence proven (internal/crypto/reboot_counter_test.go passes), but actual AEAD encryption unimplemented. LoRa control channel transmits plaintext. BEACON frames contain GPS coordinates, mesh topology, node capabilities visible to passive eavesdroppers.

**Impact**: Privacy breach (location tracking via BEACON GPS), authenticity failure (forged BEACON/JOIN_ACK injection), confidentiality compromise (mesh topology visible). Unsuitable for adversarial environments (censorship resistance use case compromised). Nonce generation infrastructure exists but unused.

**Closing the Gap**:
1. **AEAD Encryption Implementation (4 days)**: Implement internal/crypto/aead.go using golang.org/x/crypto/chacha20poly1305: (a) HKDF key derivation: derive 32-byte encryption key from MESH_KEY using crypto.hkdf.New(sha256.New, meshKey, salt="conspiracyd-aead-v1", info="beacon-encryption"), (b) Encrypt() function: accepts plaintext + nonce from NonceGenerator, returns ciphertext+tag (16-byte Poly1305 MAC appended), (c) Decrypt() function: accepts ciphertext+tag + nonce, verifies MAC, returns plaintext or error, (d) HMAC frame authentication: compute HMAC-SHA256 over header+ciphertext, truncate to 12 bytes per design §3.6.2. Unit test: round-trip encrypt/decrypt BEACON payload (101 bytes), verify MAC validation rejects tampered ciphertext, confirm nonce uniqueness across 10k frames.

2. **Anti-Replay Window (2 days)**: Implement internal/crypto/replay.go per RFC 6479: (a) Maintain 128-bit bitmap per NodeID (tracks last 128 frame sequence numbers), (b) Accept() function: if frameSeq within window and not replayed → accept and set bitmap bit, if frameSeq > window → slide window and accept, if frameSeq replayed → reject, (c) Goroutine to prune stale NodeID entries (>24 hour inactivity). Unit test: accepts in-order frames, rejects replayed frames, handles out-of-order within window, correctly wraps at sequence 65535→0.

3. **Integration with BEACON Transmitter (1 day)**: Modify internal/lora/beacon.go Transmitter to encrypt payload: (a) Generate nonce via NonceGenerator.Generate(), (b) Encrypt BEACON payload via aead.Encrypt(payload, nonce), (c) Insert nonce into frame header (no need to transmit nonce; both sides derive from NodeID+rebootCounter+frameSeq which are in cleartext header + crypto/rand component reconstructable at receiver), **CORRECTION**: nonce MUST be transmitted or derived deterministically; crypto/rand component makes nonce non-deterministic. Design §3.6 hybrid construction includes 8-byte random component per frame, so nonce cannot be reconstructed by receiver. **Resolution**: Transmit 12-byte nonce in frame header (increases overhead 12 bytes), OR remove crypto/rand component and derive nonce deterministically from NodeID||rebootCounter||frameSeq||HMAC truncation (reduces entropy but enables receiver reconstruction). **Recommendation**: Transmit nonce (safer, aligns with AEAD best practices; 12-byte overhead acceptable given 222-byte LoRa payload budget). Update frame codec to include 12-byte nonce field in header (increases header size 13→25 bytes). Unit test: transmit encrypted BEACON, receiver decrypts with transmitted nonce, plaintext matches.

**Validation**: End-to-end crypto test: Node A generates BEACON with GPS coordinates, encrypts with AEAD, transmits via LoRa. Passive observer captures frame, cannot decrypt without MESH_KEY. Node B receives frame, decrypts with shared MESH_KEY, extracts GPS correctly. Tampered frame (flip bit in ciphertext) rejected by Node B with "HMAC verification failed" error. Replay attack (retransmit captured frame) rejected by anti-replay window. Nonce uniqueness validated: generate 100k BEACONs, verify zero nonce collisions via map[string]bool check.

**Estimated Effort**: 7 person-days (4 AEAD + 2 replay window + 1 integration). **Priority: CRITICAL** — required for production security.

---

## Gap 5: Duty-Cycle Enforcement and TX Scheduler

**Stated Goal**: "The daemon enforces regional LoRa duty-cycle limits: EU 868 MHz: 1% duty cycle (36 seconds/hour), US 915 MHz: 4% duty cycle (144 seconds/hour). Strict TX scheduler with priority queue prevents regulatory violations" (README Duty-Cycle Compliance section).

**Current State**: No TX scheduler implementation. No duty-cycle token bucket. No time-on-air (ToA) calculation. No priority queue (JOIN_ACK > BEACON > ROUTE_HINT). No LBT (Listen Before Talk) collision avoidance. BEACON transmission in Gap 2 implementation transmits every 60s without rate limiting. At 100 nodes × 60s BEACON interval: duty-cycle = 100 nodes × 370ms ToA / 60s = 61.7% >> 1% EU limit (61× regulatory violation).

**Impact**: Regulatory violations (potential fines from telecom authorities in EU countries, up to €500k per violation). Network performance degradation: at >10% duty-cycle, LoRa channel saturates, packet loss >50%, discovery fails, JOIN_ACK responses lost. No priority enforcement means low-priority ROUTE_HINTs can starve high-priority JOIN_ACKs during congestion.

**Closing the Gap**:
1. **Time-on-Air Calculator (1 day)**: Implement internal/lora/toa.go: Calculate ToA per LoRa formula: `ToA = preamble_time + ((8 + 4.25) × (8 + max(ceil[(8 × payload_bytes - 4 × SF + 28 + 16) / (4 × SF)] × (CR + 4), 0))) / symbol_rate` where symbol_rate = BW / (2^SF). Example: 100-byte payload, SF10, BW125, CR=4/5 → ToA ≈ 370ms. Unit test: verifies ToA calculation matches Semtech datasheet tables for various SF/BW/payload combinations.

2. **TX Scheduler with Token Bucket (4 days)**: Implement internal/lora/scheduler.go: (a) Token bucket: capacity = 36,000 ms (EU 1% of 3,600,000 ms/hour), refill rate = 10 ms/sec (36,000 ms/hour), (b) Before transmitting: compute ToA via toa.Calculate(payload), check if tokens ≥ ToA, if yes decrement tokens and transmit, else enqueue, (c) Priority queue (3 levels): HIGH (JOIN_ACK, JOIN_REQ), MEDIUM (BEACON), LOW (ROUTE_HINT, PING/PONG), (d) Dequeue highest-priority frame with sufficient tokens every 10ms, (e) Backpressure: if queue full (256 entries per priority), drop lowest-priority frames, increment Prometheus counter lora_tx_drops{priority="low"}. Unit test: simulates 100 nodes transmitting 60s BEACONs, verifies duty-cycle <1% over 1-hour simulation (3.6M ms simulated time).

3. **LBT Collision Avoidance (2 days)**: Implement internal/lora/lbt.go: (a) Before transmitting: perform Channel Activity Detection (CAD) via SX127x register read for 5ms, (b) If RSSI > -80 dBm (channel busy): defer transmission by random jitter 10-50ms and retry (max 5 retries), (c) If channel idle: transmit immediately, (d) After successful transmission: add random jitter 50-200ms before next transmission (prevents synchronized collisions when multiple nodes use same TX schedule). Integration test: 10 nodes transmit simultaneously, measure collision rate (expect <10% with LBT vs >40% without via spectrum analyzer or packet loss ratio).

4. **Adaptive BEACON Intervals (included in scheduler, 1 day)**: Modify internal/lora/beacon.go to implement adaptive intervals per design §3.3.2: `interval = 60s × (1 + peer_count / 100)` capped at 600s (10 min). Example: 0 peers → 60s, 100 peers → 120s, 500 peers → 360s. Rationale: at 500 nodes with 60s intervals and 370ms ToA, duty-cycle = 500 × 370ms / 60s = 308% >> 1% limit. With 360s intervals: duty-cycle = 500 × 370ms / 360s = 51.4% still >> 1%. **Design Issue**: Even with adaptive intervals, large networks (500+ nodes) cannot meet 1% duty-cycle without multi-frequency zoning (Gap 6). Update internal/lora/beacon.go to log WARNING at 100 nodes: "Peer count 100 exceeds single-frequency capacity. Enable multi-frequency zoning or expect duty-cycle violations."

**Validation**: Duty-cycle compliance test: 100 nodes (simulated via UDP radios), measure actual ToA over 1 hour via TX scheduler metrics, verify sum <36 seconds (EU 1% limit). Collision test: 50 nodes transmit simultaneously with LBT enabled, measure packet delivery ratio >90% (vs <60% without LBT). Priority queue test: send 100 ROUTE_HINTs + 1 JOIN_ACK when duty-cycle budget 90% depleted, verify JOIN_ACK transmitted first, ROUTE_HINTs queued/dropped.

**Estimated Effort**: 8 person-days (1 ToA + 4 scheduler + 2 LBT + 1 adaptive intervals). **Priority: HIGH** — regulatory compliance required for legal operation.

---

## Gap 6: Multi-Frequency LoRa Zoning for Dense Deployments

**Stated Goal**: "Multi-Frequency Zoning — Supports 3-4 LoRa sub-bands for dense deployments (250+ nodes per area)" (README Feature #6). Design §3.9 specifies hash-based zone assignment `zone = FNV-1a-32(NodeID) % 3` maps nodes to 3 frequencies (EU: 868.1, 868.3, 868.5 MHz), bridge nodes monitor all 3 frequencies sequentially, BEACON forwarding with TTL field and frequency annotation.

**Current State**: No zone assignment logic. No bridge node frequency scanning. No BEACON forwarding between zones. All nodes limited to single frequency. At 90+ nodes on single EU frequency: duty-cycle exceeds 1% limit (90 nodes × 60s BEACON / 3600s = 1.5% >> 1%). Multi-frequency support required for deployments >90 nodes (EU) or >360 nodes (US).

**Impact**: Deployments >90 nodes violate EU duty-cycle regulations (1.5%+ vs 1% limit). At 250 nodes: duty-cycle = 4.2% (EU limit 1%), resulting in regulatory violations, persistent LoRa frame collisions (40-60% packet loss), discovery failures (new nodes cannot receive BEACONs reliably). Cannot scale to stated 250+ node capacity without multi-frequency zoning.

**Closing the Gap**:
1. **Hash-Based Zone Assignment (2 days)**: Implement internal/lora/zoning.go: (a) Config: [lora] frequencies = [868.1, 868.3, 868.5] (EU 3-band zoning), (b) At startup: calculate zone = FNV-1a-32(NodeID) % len(frequencies), select frequency = frequencies[zone], (c) Persist zone to /var/lib/conspiracyd/lora_zone (survives restarts), (d) Expose Prometheus gauge lora_zone_id. Unit test: generates 300 NodeIDs, verifies uniform distribution across 3 zones (each zone 90-110 nodes, chi-squared test p>0.05).

2. **Bridge Node Implementation (3 days)**: Implement bridge mode in internal/lora/bridge.go: (a) Config: [lora] bridge_mode = true (default: false; only nodes with multiple LoRa radios or USB dongles enable this), (b) Monitor all 3 frequencies sequentially (20 sec per frequency = 60 sec cycle), (c) On BEACON received from zone X: if not already forwarded (check Bloom filter), re-transmit on zones Y, Z with Forwarded flag set and TTL--, (d) Drop if TTL=0 or Forwarded=true (prevents amplification loops), (e) Duty-cycle accounting: forwarded BEACONs count toward bridge node's budget, not original sender's. Integration test: 6 nodes (2 per zone) + 1 bridge node, verify all 6 nodes discover each other via bridge forwarding within 5 minutes.

3. **BEACON Wire Format Extension (1 day)**: Add 2-byte extension to BEACON frame (increases on-wire size 101→103 bytes): 1 byte Frequency (EU: 0=868.1, 1=868.3, 2=868.5), 1 byte Flags (bit 0: Forwarded, bits 1-7: reserved). Protocol version bump to 0x4 (wire-incompatible with v0.3). Rolling upgrade strategy: v0.4 nodes can parse v0.3 BEACONs (assume Frequency=0, Forwarded=false if extension missing); deploy to ≥50% nodes before enabling multi-frequency mode.

4. **Hardware Feasibility Validation (deferred to field testing)**: Validate assumption that bridge nodes can monitor 3 frequencies sequentially. SX127x modules require 100-200ms to retune frequency (per datasheet settling time). At 20 sec per frequency × 3 = 60 sec cycle, bridge node spends 33% duty-cycle just monitoring (leaves 3% for transmissions if EU 1% transmit + 1% receive budget interpretation). **Risk**: May be infeasible with SX127x hardware. SX126x chipsets have faster frequency switching (10-20ms) making this more viable. **Recommendation**: Defer multi-frequency zoning to v1.1 after field testing validates hardware feasibility. For v1.0 MVP, document 90-node (EU) / 360-node (US) single-frequency limit in README.

**Validation**: Hash distribution test: 1,000 NodeIDs across 3 zones, chi-squared test confirms uniform distribution (p>0.05). Bridge forwarding test: 6 nodes + 1 bridge, all discover each other, packet loss <10%. Duty-cycle compliance: 300 nodes across 3 zones, measure actual on-air time over 1 hour, verify each zone <1% (EU) or <4% (US) via spectrum analyzer.

**Estimated Effort**: 6 person-days (2 zoning + 3 bridge + 1 wire format). **Priority: MEDIUM** — can defer to v1.1; document 90-node single-frequency limit for v1.0. **Hardware risk: HIGH** (requires validation that SX127x frequency switching is fast enough for bridge mode).

---

## Gap 7: Layer-3 Plugin System (HintBus Architecture)

**Stated Goal**: "Layer-3 Plugin System — HintConsumer interface enables integration with overlay networks (Yggdrasil, cjdns) without core modifications" (README Feature #7). Design §6 specifies HintProvider/HintConsumer interfaces on in-process pub/sub HintBus with adaptive consumer buffers.

**Current State**: internal/hint/bus.go:1-2 is package stub. No HintProvider interface producing routing hints from batman-adv OGM events. No HintConsumer interface for plugins. No Yggdrasil plugin (plugins/yggdrasil/consumer.go) extracting NodeID→IP mappings. No cjdns plugin. No adaptive buffer sizing. No goroutine leak prevention.

**Impact**: No extensibility for layer-3 overlays. Users wanting encrypted end-to-end tunnels (Yggdrasil) or IPv6 overlay routing (cjdns) must manually configure peer connections, negating zero-config promise. Community networks using existing overlay infrastructure (e.g., Freifunk uses cjdns/Yggdrasil in many deployments) cannot integrate Conspiracy mesh without manual bridge configuration.

**Closing the Gap**:
1. **HintBus Implementation (4 days)**: Implement internal/hint/bus.go: (a) Define Hint struct (Type: RouteAdded/RouteRemoved/PeerDiscovered, NodeID uint32, Addr net.Addr, Metric uint8, Timestamp time.Time), (b) HintProvider interface Publish(hint Hint) error, (c) HintConsumer interface Consume(hint Hint) error, (d) RegisterConsumer(name string, consumer HintConsumer, bufSize int), (e) Fan-out broadcast: HintBus maintains slice of consumers, publishes hint to all consumer channels in parallel goroutines (non-blocking send with 100ms timeout), (f) Backpressure: if consumer channel full, log WARNING "HintConsumer '{name}' slow; dropped hint", increment Prometheus counter hint_consumer_drops{consumer="name"}, (g) Adaptive buffer sizing: profile consumer latency at startup (send 100 test hints, measure p95 latency), calculate bufSize = latency_ms × expected_hint_rate × 2 capped at 256, (h) Goroutine leak watchdog: sample runtime.NumGoroutine() every 60s, alert if >1,000. Unit test: 3 consumers at different speeds (1ms, 50ms, 200ms), verify buffer sizing, backpressure, no leaks after 10k hints.

2. **Batman-adv HintProvider Integration (2 days)**: Modify internal/batman/controller.go to implement HintProvider: (a) When netlink event RTNLGRP_BATMAN_ADV received with new originator: extract NodeID from batman-adv originator MAC address (lower 32 bits), (b) Publish RouteAdded hint to HintBus with NodeID, Addr (batman-adv best-next-hop IPv6 link-local), Metric (TQ value 0-255), (c) On originator timeout: publish RouteRemoved hint. Integration test: start batman controller, inject mock netlink OGM event, verify RouteAdded hint published to HintBus with correct NodeID/Addr/Metric.

3. **Yggdrasil Plugin (4 days, optional for v1.0)**: Implement plugins/yggdrasil/consumer.go (can defer to v1.1): (a) Implements HintConsumer interface, (b) On RouteAdded hint: extract NodeID (32-bit) → IPv6 mapping (deterministic: fd00::/8 prefix + NodeID in lower 32 bits), connect to Yggdrasil admin API (Unix socket /var/run/yggdrasil.sock), send addPeer command with IPv6, (c) On RouteRemoved hint: send removePeer command, (d) Config: [plugins] yggdrasil = true enables plugin, yggdrasil_socket = "/var/run/yggdrasil.sock" specifies admin API path. Integration test: receives ROUTE_HINT, injects Yggdrasil peer, verifies peer appears in yggdrasilctl getPeers output.

4. **cjdns Plugin (4 days, optional for v1.0)**: Implement plugins/cjdns/consumer.go (can defer to v1.1): (a) Implements HintConsumer interface, (b) On RouteAdded hint: extract NodeID → cjdns IPv6 (fc00::/8), connect to cjdns admin interface (UDP 127.0.0.1:11234 with bencode protocol), send IpTunnel_allowConnection command, (c) Config: [plugins] cjdns = true enables plugin. Integration test: receives ROUTE_HINT, injects cjdns peer, verifies tunnel established via cjdnslog output.

**Validation**: Unit test: HintBus with 3 consumers (fast/medium/slow), verify backpressure, adaptive buffers, no goroutine leaks after 24-hour soak test. Integration test: 3-node mesh, Node A joins, batman-adv OGM detected on Node B → RouteAdded hint published → mock HintConsumer receives hint with correct NodeID/Addr/Metric. Performance: HintBus sustains 1,000 hints/sec with 10 consumers without dropping hints (requires buffer size ≥200 for 200ms consumer latency).

**Estimated Effort**: 10 person-days (4 HintBus + 2 batman integration + 4 Yggdrasil/cjdns plugins). **Priority: LOW** for v1.0 (can defer plugins to v1.1 if MVP focuses on basic mesh without overlays). HintBus foundation (4 days) recommended for v1.0 to enable community plugin contributions.

---

## Gap 8: Production Monitoring and Operational Tooling

**Stated Goal**: Production-ready deployment tooling including Prometheus metrics, structured logging, systemd service, and configuration management as implied by README systemd example (lines 84-96) and design §2.11 metrics specification.

**Current State**: No Prometheus metrics despite dependency (go.mod:18). No structured logging (uses fmt.Printf). No systemd service file. No configuration validation. No deployment scripts. Cannot monitor mesh health, duty-cycle compliance, or batman-adv OGM overhead in production.

**Impact**: Operators cannot diagnose issues (no metrics showing peer count, RSSI, duty-cycle utilization, OGM flooding). No proactive alerting (cannot detect approaching scale limits at 4,000 nodes). No log aggregation (plain text logs without structured fields). Manual daemon restart required (no systemd auto-restart on crash). Increases operational burden for community networks.

**Closing the Gap**:
1. **Prometheus Metrics Exporter (2 days)**: Implement cmd/conspiracyd/metrics.go using github.com/prometheus/client_golang: (a) Create Prometheus registry, (b) Register gauges: lora_peer_count (discovered nodes), batman_originator_count (OGM count), lora_rssi_avg (average RSSI in dBm), duty_cycle_utilization (0.0-1.0), (c) Register counters: lora_tx_total{frame_type="beacon"}, lora_rx_total{frame_type="beacon"}, hint_consumer_drops{consumer="name"}, lora_tx_drops{priority="low"}, (d) Expose /metrics HTTP endpoint on port 9090, (e) Update metrics from: LoRa RX handler (peer count via BEACON tracking, RSSI from PacketRadio.RSSI()), batman controller (originator count from netlink events), TX scheduler (duty-cycle from token bucket state), HintBus (consumer drops). Integration test: start daemon, scrape http://localhost:9090/metrics, verify gauge presence and non-zero values (lora_peer_count>0 after BEACON received).

2. **Structured Logging with slog (2 days)**: Replace all fmt.Printf with slog.Info/Warn/Error: (a) Initialize slog.Logger in main.go with JSON handler (slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})), (b) Pass logger to subsystems via constructor injection (e.g., lora.NewRadio(cfg, logger)), (c) Add structured fields: slog.String("node_id", nodeID), slog.String("peer_id", peerID), slog.String("frame_type", frameType), slog.Int("rssi", rssi), (d) Sensitive data redaction: if field name contains "key" or "secret", log only first 8 hex chars + "..." suffix (e.g., "mesh_key": "aabbccdd..."), (e) Log level filtering via config [logging] level = "info" | "debug" | "warn". Unit test: verifies no sensitive data in logs (scan output for full MESH_KEY hex), confirms JSON format parseable by jq.

3. **Systemd Service File (1 day)**: Create deployments/systemd/conspiracyd.service: (a) [Unit] After=network.target, Wants=network-online.target, (b) [Service] Type=notify (requires sd_notify support in Go daemon), ExecStart=/usr/sbin/conspiracyd -config /etc/conspiracyd/config.toml, Restart=on-failure, RestartSec=5s, (c) [Install] WantedBy=multi-user.target. Add sd_notify integration to main.go using github.com/coreos/go-systemd/daemon: send READY=1 after initialization complete, send STOPPING=1 on SIGTERM. Manual test: install service on Raspberry Pi, systemctl enable conspiracyd, systemctl start conspiracyd, verify daemon starts, survives crash (systemctl status shows restart), logs to journald (journalctl -u conspiracyd).

4. **Configuration Validation (1 day)**: Add -validate flag to daemon: (a) Parse config, (b) Validate: mesh_key hex-encoded 32 bytes, frequency in regional bands (EU 868.1±0.5 MHz, US 902-928 MHz, AS 433 or 920 MHz), spreading 7-12, bandwidth 125/250/500, batman.interface exists (ip link show), lora.device exists (/dev/spidev0.0 or /dev/ttyUSB0), (c) Print validation results to stdout, (d) Exit with code 0 if valid, 1 if invalid. Usage: conspiracyd -config config.toml -validate. CI integration: add validation step to GitHub Actions workflow to catch config errors in example files.

**Validation**: Metrics test: start daemon with UDP radio, scrape /metrics, verify lora_peer_count=1 after BEACON RX, duty_cycle_utilization<0.01 (1%) after 1-hour run. Logging test: grep logs for "mesh_key", verify only "aabbccdd..." appears (full key redacted). Systemd test: kill -9 daemon PID, verify systemd restarts within 5 seconds, journalctl shows "Restarted after crash" message. Config validation: run with invalid config (frequency=999 MHz), verify exits with actionable error "frequency 999.0 out of band; EU: 863-870 MHz, US: 902-928 MHz".

**Estimated Effort**: 6 person-days (2 metrics + 2 logging + 1 systemd + 1 validation). **Priority: MEDIUM** — improves operability but not blocking for basic mesh formation.

---

## Summary and Revised Effort Estimates

The Conspiracy project has **substantially more implementation than previously documented**: 2/10 goals fully achieved (hardware abstraction, cross-compilation), 3/10 partially achieved (crypto foundation ~80% complete, LoRa driver functional, integration pending). Primary gaps are:

**CRITICAL Path to MVP (3-4 weeks, 1 developer):**
1. Gap 1 (Main Daemon Integration): 6 days
2. Gap 2 (Auto-Join State Machine): 10 days
3. Gap 3 (Wi-Fi + batman-adv): 12 days
4. Gap 4 (AEAD Encryption): 7 days
**Total MVP**: ~35 person-days (crypto primitives already complete, saves 16 days from original ROADMAP estimate)

**HIGH Priority (Production Readiness):**
5. Gap 5 (Duty-Cycle Enforcement): 8 days
6. Gap 8 (Monitoring Tooling): 6 days

**MEDIUM Priority (Scalability, can defer to v1.1):**
7. Gap 6 (Multi-Frequency Zoning): 6 days (hardware risk: HIGH)
8. Gap 7 (HintBus, can defer plugins): 4-10 days (4 for foundation, +6 for Yggdrasil/cjdns)

**Revised Total Effort**: 49-55 person-days for MVP + production tooling (vs 123 days in original ROADMAP, 68-day reduction due to completed crypto/LoRa foundation). **Realistic timeline: 10-12 weeks for 1 developer, 5-6 weeks for 2 developers.**

**Immediate Action Items:**
1. **Zero effort**: Revise README line 159 batman-adv scalability claim (1,000 nodes field-tested vs 5,000 theoretical) to prevent misleading users.
2. **Week 1 priority**: Complete Gap 1 (main.go integration) + Gap 4 (AEAD encryption) to enable encrypted BEACON transmission.
3. **Week 2-3 priority**: Complete Gap 2 (auto-join FSM) + Gap 3 (batman-adv) to enable multi-hop mesh formation.
4. **Field testing**: Deploy 3-node pilot (Raspberry Pi + GL.iNet routers) to validate LoRa range, duty-cycle behavior, batman-adv stability at 10-50 nodes before public release.

The project is **closer to MVP than previously assessed**; existing crypto and hardware abstraction foundations are production-quality. Focus on integration work (state machines, batman-adv controller, nl80211 interface) will unlock end-to-end functionality rapidly.
