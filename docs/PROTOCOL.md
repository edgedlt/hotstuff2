# HotStuff-2 Protocol Guide

This guide explains the HotStuff-2 consensus protocol at two levels: a quick overview for getting started, and a comprehensive deep-dive for implementers.

**Based on**: [HotStuff-2: Optimal Two-Phase Responsive BFT](https://eprint.iacr.org/2023/397.pdf) by Malkhi & Nayak.

---

## Quick Overview (5 minutes)

### What is HotStuff-2?

HotStuff-2 is a Byzantine Fault Tolerant (BFT) consensus protocol that allows a network of nodes to agree on a sequence of blocks, even when up to 1/3 of nodes are malicious.

**Key properties:**
- **Fast**: Commits in 2 network round-trips (optimistic case)
- **Safe**: Never commits conflicting blocks
- **Live**: Always makes progress when network is stable
- **Scalable**: O(n) communication in normal operation

### How It Works (30-Second Version)

```
1. Leader proposes a block
2. Replicas vote if the block is valid
3. Leader collects 2f+1 votes → forms a Quorum Certificate (QC)
4. When a block has TWO consecutive QCs → it commits
```

### The Two-Chain Commit Rule

A block commits only when it has a "two-chain" of QCs:

```
Block A ← QC_A ← Block B ← QC_B
                            ↓
                    Block A commits!
```

**Why?** One QC proves 2f+1 nodes accepted a block. Two consecutive QCs prove the network is *locked* on that chain, preventing forks.

### View Changes

If a leader fails, nodes timeout and elect a new leader:

```
Timeout → Send NEWVIEW → New leader collects 2f+1 NEWVIEWs → Proposes
```

That's it! The rest is details.

---

## Comprehensive Reference

### 1. System Model

| Parameter | Meaning | Typical Value |
|-----------|---------|---------------|
| `n` | Total validators | 4, 7, 21, 100+ |
| `f` | Max Byzantine faults | `⌊(n-1)/3⌋` |
| `2f+1` | Quorum size | Minimum votes for QC |
| `Δ` | Network delay bound (after GST) | 1-4 seconds |
| `τ` | View timeout | ≥ 2Δ |

**Partial Synchrony**: The protocol assumes an unknown Global Stabilization Time (GST) after which messages arrive within Δ. Safety holds always; liveness requires GST.

### 2. Data Structures

```go
// Block in the chain
type Block struct {
    Height    uint32
    PrevHash  Hash          // Parent block
    Payload   []byte        // Transactions
    Proposer  uint16        // Leader who proposed
}

// Proof that 2f+1 nodes voted for a block
type QuorumCertificate struct {
    View      uint32
    BlockHash Hash
    Signers   []uint16      // Who signed
    Signature []byte        // Aggregated signature
}

// Replica state
type Replica struct {
    view      uint32        // Current view number
    lockedQC  *QC           // Highest QC we're locked on
    highQC    *QC           // Highest QC we've seen
}
```

### 3. Normal Operation (Happy Path)

#### Leader Actions

```
func Leader.Propose(view):
    1. Build block extending highQC.block
    2. Broadcast PROPOSE(view, block, highQC)
    3. Add own implicit vote
    4. Collect 2f+1 votes
    5. Form QC, broadcast to next leader
```

**Important**: The leader counts its own vote. With n=3f+1 and f faulty nodes, only 2f+1 honest nodes exist. If the leader didn't self-vote, only 2f votes would be available, breaking liveness.

#### Replica Actions

```
func Replica.OnProposal(proposal):
    1. Verify proposal.view == my.view
    2. Verify block extends a valid chain
    3. Check SafeNode rule (see below)
    4. Vote and send to leader
    5. Update lockedQC if proposal.justifyQC is higher
```

#### SafeNode Voting Rule

A replica votes for a block only if:

```go
func SafeToVote(block, justifyQC) bool {
    return lockedQC == nil ||                        // No lock yet
           justifyQC.View > lockedQC.View ||         // Higher QC supersedes
           block.extends(lockedQC.Block)             // Extends locked chain
}
```

**Why this ensures safety:**
- If 2f+1 nodes lock on block B, any conflicting block B' needs 2f+1 votes
- Quorum intersection: at least one honest node would need to vote for both
- SafeNode prevents this unless B' extends B
- Therefore: no conflicting commits possible

See implementation: `context.go:344-361`

### 4. The Two-Chain Commit Rule

**Critical**: Forming a QC does NOT commit a block!

A block commits when two consecutive QCs exist:

```
View v:   Block B proposed → QC_v formed (B NOT committed)
View v+1: Block B' proposed (extends B) → QC_{v+1} formed
          NOW Block B commits ✓
```

**Commit detection:**

```go
func checkCommit(qc):
    block = getBlock(qc.blockHash)
    parent = getBlock(block.prevHash)
    grandparent = getBlock(parent.prevHash)
    
    // Two-chain: parent's QC view == grandparent's QC view + 1
    if parent.qc.view == grandparent.qc.view + 1:
        commit(grandparent)
```

See implementation: `hotstuff2.go:501-554`

**Example timeline:**

| View | Action | Commits |
|------|--------|---------|
| 5 | Leader proposes Block₅, gets QC₅ | - |
| 6 | Leader proposes Block₆ (extends Block₅), gets QC₆ | Block₅ ✓ |
| 7 | Leader proposes Block₇ (extends Block₆), gets QC₇ | Block₆ ✓ |

Blocks commit with a 1-view lag (2 network delays in happy path).

### 5. View Changes

When a leader fails (timeout), replicas synchronize to a new view:

#### Timeout Handling

```
func Replica.OnTimeout(view):
    1. Increment view
    2. Broadcast NEWVIEW(view, highQC) to all
    3. Start exponential backoff timer
```

#### New Leader Actions

```
func Leader.OnNewViewQuorum(view, newviews):
    1. Collect 2f+1 NEWVIEW messages
    2. Select highest QC from all NEWVIEWs
    3. If have recent double-QC: propose immediately
    4. Else: wait P_pc + Δ ≈ 2Δ for synchronization
    5. Propose extending highest QC
```

See implementation: `hotstuff2.go:593-721`

#### View Synchronization

Nodes may be at different views. To sync:

1. **Forward sync**: Node receives NEWVIEW for higher view with valid QC → advances
2. **Leader sync-back**: Leader at view V+1 receives quorum NEWVIEWs for view V → can sync back if hasn't voted

### 6. Pacemaker & Timeouts

The pacemaker ensures liveness through adaptive timeouts:

```go
type Pacemaker struct {
    baseTimeout    time.Duration  // e.g., 2 seconds
    currentTimeout time.Duration  // Increases on failures
    maxTimeout     time.Duration  // e.g., 30 seconds
    multiplier     float64        // e.g., 1.5
}

func (p *Pacemaker) OnTimeout():
    p.currentTimeout = min(p.currentTimeout * p.multiplier, p.maxTimeout)

func (p *Pacemaker) OnProgress():
    p.currentTimeout = p.baseTimeout  // Reset on successful QC
```

**Timer values:**
- `baseTimeout` ≥ 2Δ (must allow round-trip)
- `maxTimeout` caps growth (prevents indefinite waits)
- `multiplier` typically 1.5-2.0

See implementation: `pacemaker.go`

### 7. Communication Complexity

| Scenario | Messages per View | Notes |
|----------|-------------------|-------|
| Happy path | O(n) | All nodes → leader |
| View change | O(n²) | NEWVIEW all-to-all |
| Amortized | O(n) | When Byzantine activity is low |

**Comparison to PBFT**: PBFT requires O(n²) every round. HotStuff-2 only incurs this during leader failures.

### 8. Performance Characteristics

**Happy path (honest leader, good network):**
- Latency: 2Δ to commit
- Communication: O(n) messages
- Throughput: Limited by block execution

**Worst case (Byzantine leader):**
- Latency: O(f × Δ) until honest leader
- Communication: O(n²) per view change
- New leader may wait 2Δ before proposing

**HotStuff vs HotStuff-2:**

| | HotStuff | HotStuff-2 |
|---|----------|------------|
| Phases | 3 | 2 |
| Commit rule | 3-chain | 2-chain |
| Happy path latency | 3Δ | 2Δ |
| View change wait | 0 | Up to 2Δ (if no double-QC) |

HotStuff-2 is faster in normal operation but slightly slower on view changes without a recent double-QC.

### 9. Corner Cases

#### Leader Crash After GST

1. Honest replicas timeout (τ ≥ 2Δ)
2. Send NEWVIEW to all validators
3. New leader collects 2f+1 NEWVIEWs
4. If has recent double-QC: proposes immediately
5. Else: waits 2Δ for synchronization

#### Cascading Byzantine Leaders

- Worst case: f Byzantine leaders in sequence
- Honest leader guaranteed within f+1 views
- Each view change: O(n²) communication
- Maximum delay: O(f × Δ)
- Matches theoretical lower bounds

#### Network Partition

- **Safety**: Never violated (quorum intersection)
- **Liveness**: Only after GST (partition must heal)
- Protocol stalls but remains safe

#### Crash Recovery

Requirements for safe restart:
1. Persist `lockedQC` before voting
2. Persist `view` before advancing
3. On restart: replay WAL, re-sync via NEWVIEWs

See initialization: `hotstuff2.go:42-127`

### 10. Security Properties

**Safety (Agreement)**:
- Two conflicting commits require two double-QCs
- Double-QCs require 2f+1 votes each
- Quorum intersection: at least one honest node in both
- SafeNode rule: honest node can't vote for conflicting blocks
- Therefore: conflicting commits impossible

**Liveness (Progress)**:
- After GST, messages arrive within Δ
- Honest leader within f+1 views (only f Byzantine)
- Proposal extends highest honest lock
- All honest nodes vote (SafeNode satisfied)
- QC forms within 2Δ

### 11. Signature Schemes

HotStuff-2 supports two signature schemes:

| | Ed25519 | BLS12-381 |
|---|---------|-----------|
| Sign | 50K ops/sec | 7K ops/sec |
| Verify | 21K ops/sec | 900 ops/sec |
| QC size | O(n) | O(1) |
| Best for | 4-25 validators | 100+ validators |

**Selection guide:**
- Small networks: Ed25519 (faster verification)
- Large networks: BLS (bandwidth savings)
- Crossover: ~50-100 validators

### 12. Implementation Checklist

- [ ] Implement `Hash`, `Block`, `Storage` interfaces
- [ ] Implement `Network` with authenticated channels
- [ ] Implement `Executor` for block building/execution
- [ ] Choose signature scheme (Ed25519 or BLS)
- [ ] Configure pacemaker timeouts for your network
- [ ] Persist `lockedQC` and `view` durably (WAL or transactional DB)
- [ ] Add metrics: view duration, commit latency, view changes
- [ ] Test with Byzantine leaders and network delays
- [ ] Run Twins framework for fault injection testing

See [INTEGRATION_GUIDE.md](./INTEGRATION_GUIDE.md) for step-by-step setup.

---

## Quick Reference Card

```
CONSTANTS
  n = validators, f = max faults, n ≥ 3f+1
  Quorum = 2f+1 votes

HAPPY PATH
  Leader: PROPOSE(block, justifyQC) → collect 2f+1 votes → QC
  Replica: verify → SafeNode check → VOTE

COMMIT RULE
  Block commits when: QC_v exists AND QC_{v+1} exists (two-chain)

SAFENODE RULE
  Vote if: no lock OR justifyQC.view > lockedQC.view OR extends locked

VIEW CHANGE
  Timeout → NEWVIEW(highQC) → new leader collects 2f+1 → propose

SAFETY: Quorum intersection + SafeNode = no conflicting commits
LIVENESS: Exponential backoff + honest leader rotation = progress
```

---

## Further Reading

- **Integration Guide**: [INTEGRATION_GUIDE.md](./INTEGRATION_GUIDE.md) - Step-by-step setup
- **TLA+ Specification**: [formal-models/tla/README.md](../formal-models/tla/README.md) - Formal proofs
- **Implementation Mapping**: [formal-models/tla/IMPLEMENTATION_MAPPING.md](../formal-models/tla/IMPLEMENTATION_MAPPING.md) - TLA+ to Go
- **Original Paper**: [HotStuff-2 (ePrint)](https://eprint.iacr.org/2023/397.pdf)
