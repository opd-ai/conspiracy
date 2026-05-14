# LONG_TERM_STRATEGY.md
## Conspiracy: Long‑Range, Long‑Term, and Peering Strategy

Version: Draft  
Scope: License‑Free, Automatic, Community‑Owned Deployment  
Constraint: Deployable by ordinary individuals without special regulatory privileges  

---

# 1. Mission

Conspiracy aims to enable a community‑owned physical network capable of:

1. Automatic participation (plug it in and it works)
2. Operating entirely within unlicensed spectrum or owner‑controlled physical media
3. Scaling from neighborhood mesh to metro backbone
4. Federating into Autonomous Systems (ASNs)
5. Becoming a real Internet peer

The network must:
- Avoid licensed spectrum
- Avoid privileged carrier dependencies
- Tolerate weather and congestion
- Self‑configure
- Opportunistically use available hardware

---

# 2. Core Constraints

## 2.1 Regulatory
- Only unlicensed spectrum (2.4 GHz, 5 GHz, 60 GHz, 900 MHz ISM, etc.)
- Free‑space optical permitted
- Privately owned fiber permitted
- No licensed microwave
- No exclusive spectrum

## 2.2 Deployment
- Must function automatically
- No manual RF tuning required
- No manual routing configuration
- Zero‑configuration mesh join
- Self‑forming topology

## 2.3 Hardware
- Commodity OpenWrt‑compatible hardware
- SBCs (ARM, RISC‑V, x86)
- Commodity directional radios
- LoRa modules
- Optional FSO modules
- Mixed capability nodes must coexist

---

# 3. The Core Problem Space

To become peers with the broader Internet, Conspiracy must solve three major problems:

## 3.1 Long‑Range Problem
Unlicensed spectrum limits range, power, and reliability.

Constraints:
- Fog kills optical
- Rain attenuates 60 GHz
- Congestion affects 2.4/5 GHz
- Foliage impacts mmWave
- Duty cycle caps LoRa

No single physical medium is sufficient.

---

## 3.2 Long‑Term Problem
Infrastructure must survive:

- Weather
- Interference
- Regulatory changes
- Hardware heterogeneity
- Node churn
- Adversarial congestion

This requires diversity, redundancy, and routing intelligence.

---

## 3.3 Peering Problem
To become an Internet peer:

- Obtain ASN
- Obtain PI address space
- Participate in IX
- Run BGP
- Maintain stable backbone

This requires:

- Routed core
- Redundant upstream paths
- Metro‑scale backbone

Mesh alone is insufficient.

---

# 4. Architectural Principles

## 4.1 Media Diversity
Every critical link must have at least two independent physical media types.

## 4.2 Layer Separation
- Control plane separated from data plane
- Access separated from backbone
- L2 mesh limited to access
- L3 routing in backbone

## 4.3 Automatic Capability Detection
Nodes must:
- Detect radio capabilities
- Detect antenna orientation
- Detect link quality
- Automatically classify themselves

## 4.4 Opportunistic Enhancement
A node with more hardware becomes:
- Relay
- Supernode
- Backbone participant
- Peering edge

Without manual reconfiguration.

---

# 5. Layered Physical Architecture

## 5.1 Tier 0 – Control Plane

**Medium: LoRa**

Purpose:
- Discovery
- Join
- Rekey
- Coordination
- Fallback signaling

LoRa is never used for bulk traffic.

---

## 5.2 Tier 1 – Access Mesh

Primary:
- 2.4 GHz / 5 GHz 802.11s

Optional:
- Wi‑Fi HaLow (sub‑GHz)

Features:
- batman‑adv for L2 mesh
- Automatic channel selection
- Local density optimization

Plug‑and‑play behavior:
- Node boots
- Broadcasts LoRa presence
- Joins mesh automatically
- Begins forwarding traffic

---

## 5.3 Tier 2 – Aggregation (Supernodes)

Nodes with:
- Directional radios
- Elevated mounting
- Multiple interfaces

Auto‑promote to "supernode" if:
- ≥2 high‑gain links detected
- Stable power
- Elevated signal visibility

Supernode responsibilities:
- Interconnect neighborhoods
- Participate in backbone routing
- Provide redundancy

---

## 5.4 Tier 3 – Metro Backbone

Media (all unlicensed):

- 60 GHz directional (primary high‑speed)
- 5 GHz directional (mid‑range fallback)
- 900 MHz directional (degraded fallback)
- Optional FSO (parallel optical path)

Backbone topology:
- Ring or multi‑ring
- No tree topology
- At least dual‑path routing

Routing:
- L3 only
- OSPF or IS‑IS internally
- BGP at core

---

# 6. Fog‑Tolerant Design

Fog severely attenuates optical links.

Therefore:

- FSO must never be sole link
- Each FSO link must have RF parallel
- Each 60 GHz link must have sub‑6 GHz fallback
- Routing must dynamically shift traffic

Failure isolation strategy:

If optical fails:
    Traffic moves to 5 GHz or 900 MHz
If 60 GHz degrades in rain:
    Traffic shifts to 5 GHz
If 5 GHz congested:
    Traffic shifts to 60 GHz or optical

Redundancy is achieved via media diversity, not signal strength alone.

---

# 7. Automatic Role Determination

Each node evaluates:

- Number of radios
- Antenna gain
- Link stability
- CPU capability
- Uptime

Roles:

| Condition | Role |
|-----------|------|
| Single omni radio | Access node |
| Multiple directional radios | Relay |
| ≥2 backbone links | Supernode |
| Stable uplink + ASN config | Peering edge |

No manual promotion required.

---

# 8. Routing Evolution Path

## Phase 1 – Neighborhood
- batman‑adv only
- L2 mesh

## Phase 2 – Multi‑Neighborhood
- L3 between supernodes
- batman only at edge

## Phase 3 – Metro Scale
- OSPF/IS‑IS backbone
- ECMP multipath
- Link weighting by medium reliability

## Phase 4 – Peering
- BGP enabled nodes
- ASN
- IX participation
- Transit agreements

Nodes that detect valid BGP config auto‑advertise upstream.

---

# 9. Plug‑and‑Play Requirements

To meet “plug it into the wall and it works”:

On boot:
1. Detect hardware capabilities
2. Load appropriate drivers
3. Scan spectrum
4. Join existing mesh via LoRa beacon
5. Negotiate encryption
6. Classify node role
7. Advertise capabilities
8. Begin routing

No CLI configuration required for baseline operation.

---

# 10. Hardware Opportunism

Nodes must support:

- Mixed radio generations
- Partial capability participation
- Graceful degradation

Example:

A node with only:
- 5 GHz omni

Becomes:
- Access relay

A node with:
- 60 GHz + 5 GHz + 900 MHz

Becomes:
- Backbone supernode

The network must dynamically incorporate new capacity.

---

# 11. Peering Strategy

To become a real peer:

1. Form nonprofit or cooperative entity
2. Obtain ASN
3. Obtain PI address space
4. Deploy BGP at backbone edge
5. Connect to IX via:
   - Cooperative fiber
   - Shared datacenter presence
   - Community colocation

This does not violate unlicensed mandate.

---

# 12. Scaling Limits

Unlicensed spectrum imposes limits:

- Congestion in dense areas
- Interference unpredictability
- No guaranteed QoS

Mitigation strategies:

- High node density
- Media diversity
- Short hops
- Ring topology
- Geographic federation

Large deployments must form:

Federated mesh islands  
Interconnected via backbone rings  

---

# 13. Long‑Term Survivability

To endure:

- Avoid single vendor dependency
- Use open drivers
- Maintain hardware abstraction
- Support future radios
- Enable cryptographic agility
- Enable automatic rekey

Resilience requires protocol flexibility above physical layer.

---

# 14. Summary

Conspiracy’s path to long‑range, long‑term, peering‑capable infrastructure requires:

- Media diversity
- Automatic capability detection
- Layer separation
- Ring backbone topology
- L3 routing core
- BGP peering edge
- Zero‑configuration deployment

The network must evolve automatically as hardware improves.

The strategy does not attempt to replace all corporate infrastructure.

It seeks to:

- Own the access layer
- Own the metro backbone
- Federate communities
- Participate as a peer

All within unlicensed, deployable, community‑owned constraints.

---

End of Document