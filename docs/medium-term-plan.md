# MEDIUM_TERM.md
## Conspiracy: Medium-Term Overlay Services Strategy

Version: Draft  
Scope: Valuable Even Without Global Internet Reach  
Constraint: Automatic, Optional, Self‑Healing, Community‑Owned  

---

# 1. Purpose

The medium-term objective is to ensure that the Conspiracy mesh is **intrinsically valuable**, even if:

- There is no upstream Internet access
- Peering is intermittent
- The network is partitioned
- Federation with other cities is unavailable

The network must provide services that:

- People can *own* on their own devices
- Work in disconnected mode
- Discover peers automatically
- Self-heal after partitions
- Require no centralized authority
- Are optional to run

The overlay should make the mesh worth joining **before** it becomes a global peer.

---

# 2. Core Principles

## 2.1 Ownership
Users host their own data where possible.

- No mandatory central servers
- No required custodians
- Personal devices are first-class nodes

## 2.2 Optional Participation
Running services is opt-in.

- A node may forward packets but host nothing
- A node may host services but not relay
- Roles are decoupled

## 2.3 Automatic Discovery
Services must:

- Announce themselves via mesh
- Be discoverable via DHT or gossip
- Reappear after reboots
- Rejoin after partitions

## 2.4 Partition Tolerance
If the mesh splits:

- Each partition continues functioning
- Data syncs when partitions reconnect
- Conflicts resolve deterministically

---

# 3. Service Categories

Medium-term services fall into five categories:

1. Asynchronous communication
2. Content distribution
3. Identity and trust
4. Local coordination
5. Software distribution

Each must be P2P-friendly and federation-capable.

---

# 4. Asynchronous Communication

## 4.1 Mesh News (Usenet-like)

Purpose:
- Distributed discussion
- Topic-based groups
- Offline-capable

Architecture:
- NNTP-like protocol over mesh
- Articles replicated opportunistically
- CRDT or append-only log structure
- No central moderator required

Properties:
- Store-and-forward
- Partial replication allowed
- Nodes choose which groups to mirror

Users can:
- Run a full node
- Mirror selected groups
- Act as a leaf reader

Automatic behavior:
- Nodes discover peers
- Exchange article indexes
- Sync missing articles
- Heal after partition

---

## 4.2 Mesh Mail (Store-and-Forward)

Purpose:
- Reliable asynchronous messaging

Design:
- Public-key addressed inboxes
- Messages routed via DHT lookup
- Store-and-forward relays
- TTL-based retention

Ownership:
- Mailboxes hosted by user devices
- Optional mirror nodes

Works without global DNS.

---

# 5. Content Distribution

## 5.1 Mesh Torrent (P2P File Sharing)

Purpose:
- Efficient large file distribution
- Software updates
- Media sharing

Features:
- Local DHT
- Peer exchange
- LAN-optimized swarming
- Automatic reseeding incentives

Nodes may:
- Seed
- Leech
- Cache popular content

No trackers required.

---

## 5.2 Distributed Archive

Purpose:
- Preserve community knowledge

Design:
- Content-addressed storage
- Hash-based retrieval
- Deduplicated blobs
- Optional pinning

Inspired by:
- IPFS-style systems
- But optimized for LAN-scale mesh

Partition-safe:
- Replicas spread opportunistically
- Sync on reconnection

---

# 6. Identity and Trust

## 6.1 Self-Certifying Identity

Each node:
- Generates its own keypair
- Identity = public key hash
- No central CA

Supports:
- Signed messages
- Signed service advertisements
- Web-of-trust overlays

---

## 6.2 Local Name Resolution

Instead of global DNS:

- Mesh-wide DHT-based name registry
- Optional human-readable aliases
- Conflict resolution via CRDT

Names are:
- Local first
- Federated later

---

# 7. Local Coordination Services

## 7.1 Event Board

Purpose:
- Announcements
- Local meetups
- Alerts

Distributed append-only feed.

Auto-expiring entries prevent clutter.

---

## 7.2 Shared Resource Registry

Example:
- Tools
- Solar capacity
- Local services

P2P discovery:
- Query by tag
- No central server

---

# 8. Software Distribution

## 8.1 Mesh Package Mirror

Purpose:
- Distribute firmware and updates
- Avoid upstream dependency

Mechanism:
- P2P package cache
- Signed release metadata
- Delta updates

Nodes:
- Cache frequently requested packages
- Auto-prune unused data

---

# 9. Service Advertisement Layer

All services must:

- Broadcast capability via control plane
- Advertise via gossip protocol
- Register in distributed index

Minimal metadata:
- Service type
- Public key
- Reachability status
- Resource limits

No central registry required.

---

# 10. Self-Healing Mechanisms

## 10.1 Gossip Sync
Nodes exchange:

- Service lists
- Content hashes
- Peer reachability

## 10.2 CRDT State Models
For mutable data:

- Conflict-free replicated data types
- Deterministic merges
- No global clock required

## 10.3 Automatic Reconciliation
When partitions reconnect:

- Exchange missing blocks
- Merge logs
- Resolve conflicts automatically

No operator intervention required.

---

# 11. Incentives Without Centralization

To encourage hosting:

- Reputation tracking
- Contribution scoring
- Optional resource accounting
- Prioritized routing for contributors

But:

- No mandatory enforcement
- Network remains usable without hosting

---

# 12. Resource Sensitivity

Services must respect:

- Bandwidth caps
- Battery power
- Storage limits
- CPU constraints

Nodes may advertise:

- "Low power mode"
- "Relay only"
- "Host only"
- "Full participant"

Automatic throttling required.

---

# 13. Security Model

- End-to-end encryption by default
- Forward secrecy preferred
- Metadata minimization
- Onion-style routing optional for privacy-sensitive services

Trust is layered:
- Transport encryption
- Identity signing
- Optional web-of-trust

---

# 14. What Not to Build (Medium-Term)

Avoid:

- Centralized social networks
- Mandatory cloud storage
- Single authoritative directories
- Hard dependencies on upstream DNS
- Services requiring global IP reachability

The network must remain useful in isolation.

---

# 15. Evolution Toward Long-Term

Medium-term services prepare for peering by:

- Training users to host services
- Distributing storage responsibility
- Exercising routing paths
- Building identity graph
- Creating local economic value

When upstream peering exists:

- Mesh News can federate outward
- Mail can gateway to SMTP
- Torrent can bridge to global swarm
- Identity can interoperate

But none require it.

---

# 16. Summary

In the medium term, Conspiracy must be:

- Valuable without the Internet
- Self-healing
- Distributed
- Optional
- Automatic

Services should be:

- P2P by default
- Discoverable automatically
- Partition-tolerant
- Owned by users
- Resource-aware

The goal is to create a network people want to join even if it never peers.

Peering becomes expansion — not survival.

---

End of Document