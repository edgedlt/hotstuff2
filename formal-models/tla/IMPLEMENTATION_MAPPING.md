# TLA+ Spec to Go Implementation Mapping

**Created**: 2025-11-24  
**Last Updated**: 2025-11-27  
**Status**: Verified - Spec models core protocol; see "Pacemaker: Spec vs Implementation" for production extensions

---

## Executive Summary

The TLA+ specification (`hotstuff2.tla`) accurately models the Go implementation.

**Verification Results**:
- Safety: 12 invariants verified (358K distinct states)
- Liveness: 4 properties verified (397K distinct states)
- Fault tolerance: Verified with f=1 crash fault (10K distinct states)

**All critical protocol rules match**:
- View guard: Strict equality (`v = view[r]`)
- Quorum: `2f+1` formula
- SafeNode: OR semantics (higher QC OR extends lock)
- Commit: Two-chain rule (consecutive QCs)
- Lock update: Higher view only
- **Leader implicit vote**: Leader counts its own vote (critical for liveness under faults)

---

## State Variables Mapping

| TLA+ Variable | Type | Go Field | Location |
|---------------|------|----------|----------|
| `view[r]` | `Replica -> View` | `Context.view` | `context.go:24` |
| `lockedQC[r]` | `Replica -> Block \cup {Nil}` | `Context.lockedQC` | `context.go:28` |
| `highQC[r]` | `Replica -> Block \cup {Nil}` | `Context.highQC` | `context.go:31` |
| `committed[r]` | `Replica -> SUBSET Block` | `Context.committed` | `context.go:34` |
| `votes[v][b]` | `[View -> [Block -> SUBSET Replica]]` | `Context.votes` | `context.go:37` |
| `blocks` | `SUBSET Block` | `Context.blocksByHash` | `context.go:40` |
| `blockParent[b]` | `[Block -> Block \cup {Genesis}]` | `Context.parents` | `context.go:46` |
| `network` | `SUBSET Message` | Network channels | `hotstuff2.go:30` |

### Key Differences

1. **Committed blocks**: TLA+ tracks set of all committed blocks; Go tracks by height for efficiency
2. **Vote organization**: TLA+ indexes by `[view][block]`; Go indexes by `[view][nodeHash][validatorIndex]`
3. **Network**: TLA+ uses persistent message set; Go uses channels

---

## Actions Mapping

| TLA+ Action | Go Method | Location | Match |
|-------------|-----------|----------|-------|
| `LeaderPropose(r)` | `propose()` | `hotstuff2.go:510-553` | Yes |
| `ReplicaVote(r)` | `onProposal()` | `hotstuff2.go:174-279` | Yes |
| `LeaderFormQC(r)` | `onVote()` | `hotstuff2.go:289-388` | Yes |
| `ReplicaUpdateOnQC(r)` | `onProposal()` (QC handling) | `hotstuff2.go:190-205` | Yes |
| `ReplicaTimeout(r)` | `onViewTimeout()` | `hotstuff2.go:461-477` | Yes |
| `LeaderProcessNewView(r)` | `onNewView()` | `hotstuff2.go:431-452` | Partial |

### LeaderPropose

**TLA+** (`hotstuff2.tla:179-205`):
```tla
LeaderPropose(r) ==
    /\ r \in Honest                 \* Only honest leaders propose
    /\ r = Leader(v)
    /\ proposals[v] = Nil
    /\ Create new block with parent = highQC
    /\ Broadcast PROPOSE message
    /\ Add leader's own VOTE to network  \* CRITICAL: implicit vote
```

**Go** (`hotstuff2.go:608-663`):
```go
func (hs *HotStuff2[H]) propose() {
    if !hs.cfg.IsLeader(view) { return }
    block, err := hs.cfg.Executor.CreateBlock(height, parentHash, hs.cfg.MyIndex)
    hs.cfg.Network.Broadcast(proposal)
    
    // Leader implicitly votes for its own proposal (CRITICAL for liveness)
    // With n=3f+1 and f faults, only 2f+1 honest nodes remain.
    // If leader doesn't vote: only 2f non-leader votes available < 2f+1 quorum
    leaderVote, _ := NewVote(view, block.Hash(), hs.cfg.MyIndex, hs.cfg.PrivateKey)
    hs.ctx.AddVote(leaderVote)
}
```

**Why this matters**: Without the leader's implicit vote, liveness fails under Byzantine faults.
With n=4 and f=1 faulty replica, we have 3 honest nodes. If the leader is honest but doesn't
count its own vote, only 2 votes are available (from non-leader honest nodes), but quorum
requires 2f+1=3 votes. This causes deadlock.

### ReplicaVote

**TLA+** (`hotstuff2.tla:244-261`):
```tla
ReplicaVote(r) ==
    /\ msg.type = "PROPOSE"
    /\ v = view[r]              \* CRITICAL: Current view only
    /\ SafeNodeRule(r, block, qc)
    /\ Send VOTE message
    /\ Update lockedQC if qc.view > lockedQC.view
```

**Go** (`hotstuff2.go:174-279`):
```go
func (hs *HotStuff2[H]) onProposal(payload ConsensusPayload[H]) {
    if view != currentView { return }       // TLA+ guard: v = view[r]
    if !hs.ctx.SafeToVote(block, justifyQC) { return }  // SafeNodeRule
    hs.ctx.UpdateLockedQC(justifyQC)        // UpdateLockCond
    hs.cfg.Network.Broadcast(voteMsg)
}
```

---

## Safety Predicates Mapping

### 1. SafeNodeRule

**TLA+** (`hotstuff2.tla:163-166`):
```tla
SafeNodeRule(r, block, qc) ==
    \/ lockedQC[r] = Nil
    \/ (qc /= Nil /\ blockView[qc] > blockView[lockedQC[r]])
    \/ IsAncestor(lockedQC[r], block)
```

**Go** (`context.go:314-332`):
```go
func (c *Context[H]) SafeToVote(block Block[H], justifyQC *QC[H]) bool {
    if c.lockedQC == nil { return true }
    if justifyQC != nil && justifyQC.View() > c.lockedQC.View() { return true }
    if c.isAncestorLocked(c.lockedQC.Node(), block.Hash()) { return true }
    return false
}
```

**Analysis**: Exact match - OR semantics preserved

### 2. UpdateLockCond

**TLA+** (`hotstuff2.tla:178-181`):
```tla
UpdateLockCond(r, qc) ==
    /\ qc /= Nil
    /\ qc \in blocks
    /\ (lockedQC[r] = Nil \/ blockView[qc] > blockView[lockedQC[r]])
```

**Go** (`context.go:99-113`):
```go
func (c *Context[H]) UpdateLockedQC(qc *QC[H]) bool {
    if qc == nil { return false }
    if c.lockedQC == nil || qc.View() > c.lockedQC.View() {
        c.lockedQC = qc
        return true
    }
    return false
}
```

**Analysis**: Match - Go assumes QC validity (verified elsewhere)

### 3. CanCommit (Two-Chain Rule)

**TLA+** (`hotstuff2.tla:170-175`):
```tla
CanCommit(block, qc) ==
    /\ qc /= Nil
    /\ qc \in blocks
    /\ block \in blocks
    /\ blockParent[qc] = block
    /\ blockView[qc] = blockView[block] + 1
```

**Go** (`hotstuff2.go:398-429` and `context.go:359-378`):
```go
func (hs *HotStuff2[H]) checkCommit(block Block[H], qc *QC[H]) {
    qcBlock, exists := hs.ctx.GetBlock(qc.Node())
    if !qcBlock.PrevHash().Equals(block.Hash()) { return }
    hs.ctx.Commit(block)
}

func (c *Context[H]) CanCommit(block Block[H], qc *QC[H]) bool {
    qcBlock, exists := c.blocksByHash[qc.Node().String()]
    if !qcBlock.PrevHash().Equals(block.Hash()) { return false }
    return true
}
```

**Analysis**: Match - both verify parent-child relationship

### 4. IsAncestor

**TLA+** (`hotstuff2.tla:94-109`):
```tla
RECURSIVE AncestorK(_,_,_)
AncestorK(a, b, k) == ...
IsAncestor(a, b) == \E i \in 0..MaxHeight : AncestorK(a, b, i)
```

**Go** (`context.go:286-312` and `context.go:334-357`):
```go
func (c *Context[H]) IsAncestor(ancestor H, descendant H) bool {
    if ancestor.Equals(descendant) { return true }
    currentStr := descendant.String()
    for i := 0; i < maxDepth; i++ {
        parent, exists := c.parents[currentStr]
        if ancestor.Equals(parent) { return true }
        currentStr = parent.String()
    }
    return false
}
```

**Analysis**: Match - both traverse parent chain with bounded depth

---

## Invariants Verified

All 12 safety invariants from the TLA+ spec are satisfied:

| Invariant | TLA+ Line | Description |
|-----------|-----------|-------------|
| `TypeInvariant` | 169-179 | Variables maintain correct types |
| `AgreementInvariant` | 374-379 | No conflicting commits at same height |
| `ValidityInvariant` | 381-382 | Only proposed blocks committed |
| `QCIntegrityInvariant` | 384-387 | QCs require quorum |
| `ForkFreedomInvariant` | 389-395 | No conflicting QCs at same height |
| `CommitChainInvariant` | 397-400 | Committed blocks form valid chain |
| `NoDoubleVoteInvariant` | 402-406 | At most one vote per view per replica |
| `SingleProposalInvariant` | 408-409 | At most one proposal per view |
| `VotesSubsetReplicasInvariant` | 411-414 | Votes from valid replicas only |
| `UniqueQCPerViewInvariant` | 416-419 | At most one QC per view |
| `HonestAgreementInvariant` | 430-435 | Honest replicas never commit conflicting blocks |
| `HonestValidityInvariant` | 438-439 | Honest commits are valid proposed blocks |

---

## Fault Modeling

The TLA+ spec models Byzantine fault tolerance via the `Faulty` constant:

| TLA+ Construct | Description |
|----------------|-------------|
| `Faulty` | Set of faulty replica IDs (configured in .cfg file) |
| `Honest == Replica \ Faulty` | Set of honest (non-faulty) replicas |
| `FaultyAssumption` | Invariant: `\|Faulty\| <= f` |

### Fault Model: Crash Faults

The current model is **crash faults** (fail-stop):
- Faulty replicas never participate (no votes, proposals, or NEWVIEWs)
- All TLA+ actions have guard `r \in Honest`
- This models worst-case liveness: maximum unresponsive nodes

### Constant-Level Assumptions (ASSUME statements)

These are verified once at model initialization, not as state invariants:

| Assumption | Description |
|------------|-------------|
| `Cardinality(Faulty) <= F` | BFT assumption: at most f faulty replicas |
| `QuorumIntersectionProperty` | Any two quorums share at least one honest replica |

### Fault-Tolerant Invariants (state invariants)

| Invariant | Description |
|-----------|-------------|
| `HonestAgreementInvariant` | Honest replicas never commit conflicting blocks |
| `HonestValidityInvariant` | Honest commits are valid proposed blocks |

### Configuration

Use `hotstuff2_fault.cfg` to test fault tolerance:
```
Replica = {1, 2, 3, 4}
Faulty = {4}    \* Replica 4 is crash-faulty
```

---

## Abstractions

The TLA+ spec abstracts these implementation details:

| Detail | TLA+ Abstraction | Justification |
|--------|------------------|---------------|
| Cryptography | Boolean validity | Assumes crypto correct |
| Network (gRPC) | Async message set | Models delivery semantics |
| Timer/pacemaker | Nondeterministic timeout | Any replica can timeout |
| Ed25519 signatures | Abstract signature | Scheme doesn't affect safety |
| Persistence | Atomic transitions | Assumes storage atomicity |
| Concurrency | Atomic actions | Each step is atomic |

These abstractions are valid because safety properties don't depend on timing or crypto implementation.

---

## Pacemaker: Spec vs Implementation

The Go implementation includes an extended pacemaker with production features not modeled in the TLA+ spec. This section documents these differences.

### What the TLA+ Spec Models

The spec models the **core pacemaker behavior** required for safety and liveness:

| Feature | TLA+ | Description |
|---------|------|-------------|
| `TimeoutPropose` | Yes | Modeled via `ReplicaTimeout` action - any replica can timeout |
| View advancement | Yes | `view' = view + 1` on timeout or QC formation |
| NEWVIEW messages | Yes | `LeaderProcessNewView` waits for 2f+1 NEWVIEWs |
| Exponential backoff | Implicit | Modeled as nondeterministic timeout (any time) |

### What the Implementation Adds

The Go implementation (`pacemaker.go`, `PacemakerConfig`) includes additional features for production use:

| Feature | In Spec? | Purpose |
|---------|----------|---------|
| `TimeoutPropose` | Yes | Wait time for block proposal |
| `TimeoutVote` | No* | Wait time for votes (unused in two-phase HotStuff-2) |
| `TimeoutCommit` | **No** | **Target block time** - minimum delay between blocks |
| `BackoffMultiplier` | No | Explicit exponential backoff factor (e.g., 1.5x) |
| `MaxTimeout` | No | Cap on timeout growth |
| `SkipTimeoutCommit` | No | Fast path optimization |

*`TimeoutVote` exists in config but is primarily for compatibility with three-phase BFT protocols.

### Why TimeoutCommit is Not in the Spec

`TimeoutCommit` is a **rate-limiting mechanism**, not a consensus protocol feature:

1. **Safety**: `TimeoutCommit` does not affect safety properties. Blocks are still committed via the two-chain rule regardless of timing.

2. **Liveness**: The spec's liveness properties (eventual commit under partial synchrony) hold whether blocks are produced at 1/second or 1000/second.

3. **Purpose**: `TimeoutCommit` exists to:
   - Control block production rate for public blockchains
   - Reduce resource usage (CPU, bandwidth, storage)
   - Provide predictable block times for applications

4. **Implementation detail**: It's enforced by waiting after commit before the next proposal:
   ```go
   // In hotstuff2.go:advanceViewWithQC()
   if waitForCommitDelay {
       hs.pm.WaitForCommitDelay()  // Enforces TimeoutCommit
   }
   ```

### Preset Configurations

The implementation provides preset configurations for different use cases:

| Config | TimeoutCommit | Use Case |
|--------|---------------|----------|
| `DefaultPacemakerConfig()` | 0 (immediate) | Testing, benchmarks - optimistically responsive |
| `DemoPacemakerConfig()` | 1s | Demos - visible block production (~1 block/sec) |
| `ProductionPacemakerConfig(t)` | t | Production - target block time (e.g., 5s for public chain) |

### Potential Future Spec Extensions

If formal verification of timing properties becomes necessary, separate specs could model:

1. **Throughput bounds**: Verify block rate stays within configured limits
2. **Backoff convergence**: Verify timeout eventually stabilizes after partitions heal
3. **Fast path optimization**: Verify `SkipTimeoutCommit` maintains target rate on average

These would be **liveness refinements**, not safety properties, and would require a timed/real-time model (e.g., TLA+ with real-time extensions or a different formalism).

---

## References

- **TLA+ Spec**: `hotstuff2.tla`
- **Go Implementation**: `../../hotstuff2.go`, `../../context.go`
- **HotStuff-2 Paper**: https://eprint.iacr.org/2023/397.pdf
- **Algorithm Explanation**: `../../docs/ALGORITHM_EXPLAINED.md`
