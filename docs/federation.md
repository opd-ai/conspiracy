# Federation Guide: Scaling Beyond 1,000 Nodes

## Overview

Batman-adv mesh networks operate efficiently up to approximately 1,000 nodes per contiguous mesh island. Beyond this threshold, OGM (Originator Message) flooding overhead consumes significant bandwidth and CPU resources, leading to increased packet loss and routing instability.

For deployments requiring more than 1,000 nodes, **federated mesh architecture** provides a scalable solution by segmenting the network into multiple independent mesh islands interconnected at layer-3.

## When to Federate

Consider federation when you observe any of the following conditions:

### Network Size Thresholds
- **750+ nodes**: Start planning federation migration (75% of conservative limit)
- **1,000+ nodes**: Federation strongly recommended
- **1,500+ nodes**: Federation required for stable operation

### Performance Indicators
- OGM overhead consuming >10% of CPU time
- Packet loss >5% under normal traffic load
- Route convergence time >60 seconds after topology change
- `batman_originator_count` Prometheus metric approaching 4,000

### Duty-Cycle Saturation (LoRa Control Channel)
- ROUTE_HINT frames dropped due to 1% EU duty-cycle limit (36 sec/hour)
- Beacon interval approaching 600 seconds (adaptive backoff ceiling)
- Multi-frequency zoning insufficient for area density

## Federation Architecture

### Mesh Island Design

Each mesh island operates as an autonomous batman-adv broadcast domain:

```
Island A (500 nodes)          Island B (500 nodes)
┌─────────────────────┐      ┌─────────────────────┐
│  batman-adv (bat0)  │      │  batman-adv (bat0)  │
│  802.11s mesh peers │      │  802.11s mesh peers │
│  LoRa control plane │      │  LoRa control plane │
└──────────┬──────────┘      └──────────┬──────────┘
           │                             │
      Gateway Node                  Gateway Node
     (Layer-3 Bridge)              (Layer-3 Bridge)
           │                             │
           └─────────────────────────────┘
                 Yggdrasil Overlay
               (Encrypted Tunnel)
```

### Gateway Node Requirements

Gateway nodes bridge mesh islands at layer-3 and require:

1. **Dual Network Interfaces**:
   - Interface 1: batman-adv participant in local mesh island
   - Interface 2: WAN/backbone link to other gateways (wireguard, fiber, etc.)

2. **Yggdrasil Overlay** (Recommended):
   - Mesh island routes advertised as Yggdrasil peer hints
   - Encrypted end-to-end tunnels between gateways
   - Automatic route propagation without BGP complexity

3. **Resource Requirements**:
   - 2 GB RAM minimum (handles OGM processing + routing table)
   - Dual-core CPU recommended (separate LoRa and gateway forwarding)
   - Persistent storage for reboot counter (SSD preferred for gateway uptime)

## Implementation Steps

### 1. Identify Island Boundaries

Partition network topology based on:
- **Geographic segmentation**: Dense urban areas become separate islands
- **Administrative domains**: Community-owned vs municipal infrastructure
- **Radio coverage**: Natural partitions at range boundaries

Goal: ~500-750 nodes per island for optimal performance headroom.

### 2. Deploy Gateway Nodes

Select 2-3 nodes per island to serve as gateways:

```toml
# /etc/conspiracyd/config-gateway.toml
[lora]
device = "/dev/spidev0.0"
frequency_mhz = 868.1
spreading = 10
mesh_key = "hex:aabbcc..."

[wifi]
mesh_interface = "wlan0"
ssid = "conspiracy-island-a"
channel = 6

[batman]
interface = "bat0"
enabled = true

[plugins]
yggdrasil = true  # Enable for gateway role
cjdns = false

[gateway]
enabled = true
island_id = "island-a"
peer_gateways = ["10.0.1.2", "10.0.2.2"]  # Other island gateways
```

### 3. Configure Yggdrasil Overlay

Install Yggdrasil on gateway nodes:

```bash
# Debian/Ubuntu
wget https://github.com/yggdrasil-network/yggdrasil-go/releases/latest/download/yggdrasil-linux-amd64
chmod +x yggdrasil-linux-amd64
./yggdrasil-linux-amd64 -genconf > /etc/yggdrasil.conf
systemctl enable yggdrasil
systemctl start yggdrasil
```

Configure peering between gateway nodes:

```json
{
  "Peers": [
    "tcp://gateway-island-b.example.com:9001",
    "tcp://gateway-island-c.example.com:9001"
  ],
  "InterfacePeers": {
    "bat0": ["yggdrasil://[auto]"]
  }
}
```

### 4. Enable Route Propagation

The `HintConsumer` plugin for Yggdrasil automatically:
1. Receives `ROUTE_HINT` frames from local mesh island
2. Extracts NodeID → IPv6 mapping
3. Injects routes into Yggdrasil as peer hints
4. Establishes encrypted tunnels to remote gateways

No manual route configuration required.

### 5. Monitor Federation Health

Track per-island metrics:

```promql
# Prometheus queries
sum(batman_originator_count) by (island_id)
rate(gateway_tunnel_bytes_sent[5m]) by (island_id)
histogram_quantile(0.95, gateway_cross_island_latency_seconds)
```

Alerts:
- `batman_originator_count{island_id="X"} > 750` — Warn: approaching island capacity
- `gateway_tunnel_down{island_id="X"} == 1` — Critical: island isolated
- `gateway_cross_island_latency_seconds > 0.100` — Warn: high inter-island latency

## Layer-3 Route Propagation

### IPv6 Addressing (Recommended)

Use deterministic IPv6 derived from NodeID:

```
fd00:c0n5:p1r4:c7::/64 prefix + NodeID(64-bit)
```

Yggdrasil overlay automatically routes `fd00::/8` prefixes between islands.

### IPv4 Addressing (Legacy Support)

For IPv4-only clients, use island-specific subnets with NAT at gateways:

```
Island A: 10.0.0.0/16
Island B: 10.1.0.0/16
Island C: 10.2.0.0/16
```

Configure SNAT at gateways for cross-island traffic.

## Operational Considerations

### Island Merges

If two islands physically connect (new node bridges coverage gap):
1. Monitor for sudden spike in `batman_originator_count` (alert: `rate(batman_originator_count[1m]) > 50`)
2. Implement hysteresis: temporary increase in `batman_hard_limit_originators` to 5,000 for 60 seconds
3. Log event: "Island merge detected; temporary OGM burst tolerance enabled"
4. If merge is permanent and exceeds 1,000 nodes, notify operators to split topology

### Island Splits

Network partition (e.g., gateway failure, radio interference):
1. Each sub-island continues operating independently with batman-adv
2. Yggdrasil overlay maintains connectivity if WAN/backbone links operational
3. Auto-rejoin when partition heals (batman-adv OGM propagation resumes)

### Gateway Redundancy

Deploy 2-3 gateways per island for failover:
- Active-active mode: all gateways forward traffic
- Yggdrasil uses latency-based path selection
- No single point of failure

## Performance Characteristics

### Expected Latency Overhead

- **Intra-island**: 5-20 ms (batman-adv hop count × 5 ms)
- **Inter-island (same datacenter)**: +2-5 ms (Yggdrasil tunnel overhead)
- **Inter-island (geographically distributed)**: +10-100 ms (WAN latency)

### Throughput

Gateway nodes should sustain:
- **Intra-island traffic**: Wire-speed forwarding (54-300 Mbps 802.11n)
- **Inter-island traffic**: Limited by WAN uplink (typically 10-100 Mbps)

Bottleneck is WAN link, not gateway CPU.

## Troubleshooting

### Symptom: Gateway drops packets

**Diagnosis**:
```bash
# Check gateway CPU utilization
top -p $(pgrep conspiracyd)

# Check Yggdrasil tunnel status
yggdrasilctl getPeers
```

**Solutions**:
- Increase gateway CPU allocation (dedicated core for forwarding)
- Enable hardware offload: `ethtool -K bat0 gso off gro off`
- Add gateway node to distribute load

### Symptom: High cross-island latency

**Diagnosis**:
```bash
# Trace route through Yggdrasil overlay
yggdrasilctl getRoutes

# Measure RTT between gateway nodes
ping6 -c 10 [remote_gateway_ipv6]
```

**Solutions**:
- Optimize Yggdrasil peering (prefer direct peering over multi-hop)
- Upgrade WAN link bandwidth (if congested)
- Check for packet loss: `mtr --report [remote_gateway_ip]`

## References

- Batman-adv scaling discussion: https://www.open-mesh.org/projects/open-mesh/wiki/FAQ
- Freifunk federation architecture: https://wiki.freifunk.net/Federation
- Yggdrasil network documentation: https://yggdrasil-network.github.io/
- Conspiracy HintConsumer plugin API: `docs/hint-consumer-api.md` (TODO)

## Future Enhancements

- **Automatic island detection**: Daemon detects when local mesh exceeds 750 nodes and suggests split points based on betweenness centrality
- **BGP integration**: For large deployments requiring full layer-3 routing control
- **Multi-home gateways**: Gateway participates in multiple islands simultaneously (bridge role)
