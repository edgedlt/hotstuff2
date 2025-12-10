# HotStuff-2 Integration Guide

This guide shows protocols and applications how to integrate the HotStuff-2 consensus library.

## Overview

HotStuff-2 is a two-phase Byzantine Fault Tolerant (BFT) consensus protocol that achieves:
- **Optimistic responsiveness**: Commits in 2 network delays under synchrony
- **Efficient view-change**: O(n) data per message, O(n²) total messages (amortized O(n))
- **Byzantine fault tolerance**: Tolerates up to f < n/3 faulty nodes

## Quick Start

```go
package main

import (
    "github.com/edgedlt/hotstuff2"
    "github.com/edgedlt/hotstuff2/timer"
    "go.uber.org/zap"
)

func main() {
    // 1. Create implementations for your chain
    validators := NewMyValidatorSet(...)
    storage := NewMyStorage(...)
    network := NewMyNetwork(...)
    executor := NewMyExecutor(...)
    privateKey := LoadMyPrivateKey(...)

    // 2. Configure HotStuff2
    cfg, err := hotstuff2.NewConfig[MyHash](
        hotstuff2.WithMyIndex[MyHash](0),
        hotstuff2.WithValidators[MyHash](validators),
        hotstuff2.WithPrivateKey[MyHash](privateKey),
        hotstuff2.WithStorage[MyHash](storage),
        hotstuff2.WithNetwork[MyHash](network),
        hotstuff2.WithExecutor[MyHash](executor),
        hotstuff2.WithTimer[MyHash](timer.NewRealTimer()),
        hotstuff2.WithLogger[MyHash](zap.NewProduction()),
    )
    if err != nil {
        panic(err)
    }

    // 3. Create and start consensus
    // Option A: Simple callback for commits only
    hs, err := hotstuff2.NewHotStuff2(cfg, func(block hotstuff2.Block[MyHash]) {
        log.Printf("Block committed: height=%d hash=%s", block.Height(), block.Hash())
    })
    
    // Option B: Full observability with hooks (recommended)
    // hs, err := hotstuff2.NewHotStuff2WithHooks(cfg, &hotstuff2.Hooks[MyHash]{
    //     OnCommit: func(block hotstuff2.Block[MyHash]) { ... },
    //     OnViewChange: func(old, new uint32) { ... },
    //     // See "Observability" section for all available hooks
    // })
    
    if err != nil {
        panic(err)
    }

    hs.Start()
    defer hs.Stop()

    // Consensus runs in background...
    select {}
}
```

## Interfaces to Implement

You must implement these interfaces to integrate HotStuff-2 with your blockchain.

### 1. Hash

Your hash type for block and transaction identifiers.

```go
type Hash interface {
    Bytes() []byte
    Equals(other Hash) bool
    String() string
}

// Example implementation
type MyHash [32]byte

func (h MyHash) Bytes() []byte { return h[:] }

func (h MyHash) Equals(other hotstuff2.Hash) bool {
    if o, ok := other.(MyHash); ok {
        return h == o
    }
    return false
}

func (h MyHash) String() string {
    return hex.EncodeToString(h[:])
}
```

### 2. Block

Your block type with application-specific payload.

The Block interface is payload-agnostic - consensus treats block content as opaque bytes.
This supports various execution models:
- Traditional transactions (serialize tx list into Payload)
- DAG-based mempools like Narwhal (payload contains vertex references)
- Rollup batches (payload contains batch commitments)

```go
type Block[H Hash] interface {
    Hash() H                // Unique block identifier
    Height() uint32         // Block height (0 for genesis)
    PrevHash() H            // Parent block hash
    Payload() []byte        // Application-specific content (opaque to consensus)
    ProposerIndex() uint16  // Proposer validator index
    Timestamp() uint64      // Block timestamp (ms since epoch)
    Bytes() []byte          // Serialized form
}

// Example: Traditional transaction-based block
type MyBlock struct {
    hash      MyHash
    height    uint32
    prevHash  MyHash
    payload   []byte  // Serialized transactions
    proposer  uint16
    timestamp uint64
}

func (b *MyBlock) Hash() MyHash        { return b.hash }
func (b *MyBlock) Height() uint32      { return b.height }
func (b *MyBlock) PrevHash() MyHash    { return b.prevHash }
func (b *MyBlock) Payload() []byte     { return b.payload }
func (b *MyBlock) ProposerIndex() uint16 { return b.proposer }
func (b *MyBlock) Timestamp() uint64   { return b.timestamp }
func (b *MyBlock) Bytes() []byte       { return serialize(b) }

// Example: Narwhal DAG-based block
type NarwhalBlock struct {
    hash          MyHash
    height        uint32
    prevHash      MyHash
    dagReferences []byte  // Serialized DAG vertex references
    proposer      uint16
    timestamp     uint64
}

func (b *NarwhalBlock) Payload() []byte { return b.dagReferences }
// ... other methods
```

### 3. ValidatorSet

Manages the set of consensus validators.

```go
type ValidatorSet interface {
    Count() int                                  // Total validator count
    GetByIndex(index uint16) (PublicKey, error)  // Get validator public key
    Contains(index uint16) bool                  // Check if index is valid
    GetPublicKeys(indices []uint16) ([]PublicKey, error)  // Batch get
    GetLeader(view uint32) uint16                // Leader for view
    F() int                                      // Max Byzantine faults (n-1)/3
}

// Example implementation
type MyValidatorSet struct {
    validators []PublicKey
}

func (vs *MyValidatorSet) Count() int {
    return len(vs.validators)
}

func (vs *MyValidatorSet) GetByIndex(index uint16) (hotstuff2.PublicKey, error) {
    if int(index) >= len(vs.validators) {
        return nil, fmt.Errorf("invalid index: %d", index)
    }
    return vs.validators[index], nil
}

func (vs *MyValidatorSet) Contains(index uint16) bool {
    return int(index) < len(vs.validators)
}

func (vs *MyValidatorSet) GetPublicKeys(indices []uint16) ([]hotstuff2.PublicKey, error) {
    keys := make([]hotstuff2.PublicKey, len(indices))
    for i, idx := range indices {
        key, err := vs.GetByIndex(idx)
        if err != nil {
            return nil, err
        }
        keys[i] = key
    }
    return keys, nil
}

func (vs *MyValidatorSet) GetLeader(view uint32) uint16 {
    // Round-robin leader selection
    return uint16(view % uint32(len(vs.validators)))
}

func (vs *MyValidatorSet) F() int {
    return (len(vs.validators) - 1) / 3
}
```

### 4. Storage

Persistent storage for blocks and consensus state.

**CRITICAL**: All `Put` operations must be durable before returning. The locked QC and view must be persisted atomically to prevent safety violations after crash recovery.

```go
type Storage[H Hash] interface {
    GetBlock(hash H) (Block[H], error)
    PutBlock(block Block[H]) error
    GetLastBlock() (Block[H], error)
    
    GetQC(nodeHash H) (QuorumCertificate[H], error)
    PutQC(qc QuorumCertificate[H]) error
    
    // SAFETY-CRITICAL: Must persist atomically
    GetHighestLockedQC() (QuorumCertificate[H], error)
    PutHighestLockedQC(qc QuorumCertificate[H]) error
    
    GetView() (uint32, error)
    PutView(view uint32) error
    
    Close() error
}

// Example with LevelDB
type LevelDBStorage struct {
    db *leveldb.DB
}

func (s *LevelDBStorage) PutBlock(block Block[MyHash]) error {
    key := append([]byte("block:"), block.Hash().Bytes()...)
    return s.db.Put(key, block.Bytes(), &opt.WriteOptions{Sync: true})
}

func (s *LevelDBStorage) PutHighestLockedQC(qc QuorumCertificate[MyHash]) error {
    // CRITICAL: Use sync write for safety
    return s.db.Put([]byte("locked_qc"), qc.Bytes(), &opt.WriteOptions{Sync: true})
}
```

### 5. Network

Message broadcasting and delivery between validators.

```go
type Network[H Hash] interface {
    Broadcast(payload ConsensusPayload[H])
    SendTo(validatorIndex uint16, payload ConsensusPayload[H])
    Receive() <-chan ConsensusPayload[H]
    Close() error
}

// Example with gRPC
type GRPCNetwork struct {
    clients  map[uint16]*grpc.ClientConn
    incoming chan ConsensusPayload[MyHash]
}

func (n *GRPCNetwork) Broadcast(payload ConsensusPayload[MyHash]) {
    for _, client := range n.clients {
        go func(c *grpc.ClientConn) {
            // Send to peer
            SendMessage(c, payload.Bytes())
        }(client)
    }
}

func (n *GRPCNetwork) Receive() <-chan ConsensusPayload[MyHash] {
    return n.incoming
}
```

### 6. Executor

Block execution and validation. The Executor interprets block payloads - consensus treats
them as opaque bytes.

```go
type Executor[H Hash] interface {
    Execute(block Block[H]) (stateHash H, err error)  // Apply block payload
    Verify(block Block[H]) error                      // Validate before voting
    GetStateHash() H                                  // Current state hash
    CreateBlock(height uint32, prevHash H, proposerIndex uint16) (Block[H], error)
}
```

#### Example: Traditional Mempool

```go
type TraditionalExecutor struct {
    state   *StateDB
    mempool *Mempool
}

func (e *TraditionalExecutor) Execute(block Block[MyHash]) (MyHash, error) {
    // Deserialize transactions from payload
    txs := DeserializeTransactions(block.Payload())
    
    for _, tx := range txs {
        if err := e.state.ApplyTransaction(tx); err != nil {
            return MyHash{}, err
        }
    }
    return e.state.Hash(), nil
}

func (e *TraditionalExecutor) Verify(block Block[MyHash]) error {
    if _, err := e.storage.GetBlock(block.PrevHash()); err != nil {
        return fmt.Errorf("parent block not found: %w", err)
    }
    
    // Deserialize and validate transactions
    txs := DeserializeTransactions(block.Payload())
    for _, tx := range txs {
        if err := e.validateTx(tx); err != nil {
            return fmt.Errorf("invalid transaction: %w", err)
        }
    }
    return nil
}

func (e *TraditionalExecutor) CreateBlock(height uint32, prevHash MyHash, proposerIndex uint16) (Block[MyHash], error) {
    txs := e.mempool.GetPending(maxTxsPerBlock)
    payload := SerializeTransactions(txs)
    
    block := &MyBlock{
        height:    height,
        prevHash:  prevHash,
        payload:   payload,
        proposer:  proposerIndex,
        timestamp: uint64(time.Now().UnixMilli()),
    }
    block.hash = computeBlockHash(block)
    return block, nil
}
```

#### Example: Narwhal DAG Mempool

```go
type NarwhalExecutor struct {
    dag   *narwhal.DAG
    state *StateDB
}

func (e *NarwhalExecutor) Execute(block Block[MyHash]) (MyHash, error) {
    // Payload contains DAG vertex references
    refs := DeserializeDAGRefs(block.Payload())
    
    for _, ref := range refs {
        // Fetch transactions from DAG and execute
        txs := e.dag.GetTransactions(ref)
        for _, tx := range txs {
            if err := e.state.ApplyTransaction(tx); err != nil {
                return MyHash{}, err
            }
        }
    }
    return e.state.Hash(), nil
}

func (e *NarwhalExecutor) Verify(block Block[MyHash]) error {
    refs := DeserializeDAGRefs(block.Payload())
    
    // Verify all referenced vertices are certified
    for _, ref := range refs {
        if !e.dag.IsCertified(ref) {
            return fmt.Errorf("uncertified DAG vertex: %s", ref)
        }
    }
    return nil
}

func (e *NarwhalExecutor) CreateBlock(height uint32, prevHash MyHash, proposerIndex uint16) (Block[MyHash], error) {
    // Get certified vertices from DAG instead of raw transactions
    vertices := e.dag.GetCertifiedVertices()
    payload := SerializeDAGRefs(vertices)
    
    block := &NarwhalBlock{
        height:        height,
        prevHash:      prevHash,
        dagReferences: payload,
        proposer:      proposerIndex,
        timestamp:     uint64(time.Now().UnixMilli()),
    }
    block.hash = computeBlockHash(block)
    return block, nil
}
```

### 7. Keys (PublicKey / PrivateKey)

Cryptographic keys for signing and verification.

```go
type PublicKey interface {
    Bytes() []byte
    Verify(message []byte, signature []byte) bool
    Equals(other interface{ Bytes() []byte }) bool
    String() string
}

type PrivateKey interface {
    PublicKey() interface{ ... }
    Sign(message []byte) ([]byte, error)
    Bytes() []byte
}
```

The library provides two signature schemes in `internal/crypto`:

### Ed25519 (Default)

Ed25519 provides O(n) multi-signatures where each validator's signature is concatenated.
Good for smaller validator sets (< 100 validators).

```go
import "github.com/edgedlt/hotstuff2/internal/crypto"

// Generate new keypair
priv, err := crypto.GenerateEd25519Key()
pub := priv.PublicKey()

// Sign message
sig, err := priv.Sign([]byte("message"))

// Verify signature
valid := pub.Verify([]byte("message"), sig)
```

### BLS12-381 (O(1) Signatures)

BLS provides O(1) aggregate signatures - constant size regardless of signer count.
Ideal for large validator sets (100+ validators).

```go
import "github.com/edgedlt/hotstuff2/internal/crypto"

// Generate new BLS keypair
priv, err := crypto.GenerateBLSKey()
pub := priv.PublicKey()

// Sign message
sig, err := priv.Sign([]byte("message"))

// Verify single signature
valid := pub.Verify([]byte("message"), sig)

// Aggregate multiple signatures (O(1) result)
aggSig, err := crypto.AggregateSignatures([]*crypto.BLSSignature{sig1, sig2, sig3})

// Verify aggregate (all signers signed same message)
err = crypto.VerifyAggregated(message, aggSig, []*crypto.BLSPublicKey{pub1, pub2, pub3})
```

### Choosing a Signature Scheme

| Scheme | QC Size | Verification | Best For |
|--------|---------|--------------|----------|
| Ed25519 | O(n) - 64 bytes × n | Fast, parallel | Small validator sets |
| BLS | O(1) - 48 bytes | Pairing-based | Large validator sets |

Configure via `WithCryptoScheme`:

```go
// Use Ed25519 (default)
hotstuff2.WithCryptoScheme[MyHash]("ed25519")

// Use BLS for O(1) aggregate signatures
hotstuff2.WithCryptoScheme[MyHash]("bls")
```

**Note**: BLS requires all validators to sign the same message (view + nodeHash).
Ed25519 votes include per-validator timestamps for replay protection.

### BLS Validator Registration

When using BLS signatures, implement proof-of-possession (PoP) during validator registration
to prevent rogue public key attacks. A rogue key attack occurs when a malicious validator
registers a crafted public key that can forge aggregate signatures without cooperation from
other validators.

**Recommended approach**: Require validators to submit a signature over their own public key
during registration:

```go
// During validator registration
type ValidatorRegistration struct {
    PublicKey []byte
    ProofOfPossession []byte  // Signature over PublicKey using the validator's private key
}

func (r *ValidatorRegistration) Verify() error {
    pk, err := crypto.BLSPublicKeyFromBytes(r.PublicKey)
    if err != nil {
        return err
    }
    
    sig, err := crypto.BLSSignatureFromBytes(r.ProofOfPossession)
    if err != nil {
        return err
    }
    
    // Verify the validator signed their own public key
    if !pk.Verify(r.PublicKey, sig) {
        return errors.New("invalid proof of possession")
    }
    
    return nil
}
```

The library's `BatchVerify` function uses random linear combination internally, which provides
mitigation during signature verification. However, PoP at registration time provides defense
in depth and is recommended for production deployments.

## Configuration Options

```go
cfg, err := hotstuff2.NewConfig[MyHash](
    // Required
    hotstuff2.WithMyIndex[MyHash](0),           // Your validator index
    hotstuff2.WithValidators[MyHash](valSet),   // Validator set
    hotstuff2.WithPrivateKey[MyHash](privKey),  // Signing key
    hotstuff2.WithStorage[MyHash](storage),     // Persistent storage
    hotstuff2.WithNetwork[MyHash](network),     // Network layer
    hotstuff2.WithExecutor[MyHash](executor),   // Block executor
    hotstuff2.WithTimer[MyHash](timer),         // Timeout timer

    // Optional
    hotstuff2.WithLogger[MyHash](logger),       // Structured logging
    hotstuff2.WithCryptoScheme[MyHash]("ed25519"), // "ed25519" or "bls"
    hotstuff2.WithVerification[MyHash](false),  // TLA+ runtime verification
    
    // Block time configuration (see below)
    hotstuff2.WithTargetBlockTime[MyHash](5 * time.Second),
)
```

## Block Time Configuration

HotStuff-2 provides flexible block time configuration via `PacemakerConfig`. This follows
the standard BFT timing model used by production systems like CometBFT (Tendermint) and Aptos.

### Quick Configuration

For most use cases, use one of the convenience methods:

```go
// Option 1: Set a specific target block time (recommended for production)
hotstuff2.WithTargetBlockTime[MyHash](5 * time.Second)  // ~5 second blocks

// Option 2: Use full pacemaker config for fine-grained control
hotstuff2.WithPacemaker[MyHash](hotstuff2.PacemakerConfig{
    TimeoutPropose:    3 * time.Second,
    TimeoutVote:       2 * time.Second,
    TimeoutCommit:     5 * time.Second,  // Target block time
    BackoffMultiplier: 1.5,
    MaxTimeout:        30 * time.Second,
})
```

### PacemakerConfig Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `TimeoutPropose` | How long to wait for a block proposal before timing out | 1s |
| `TimeoutVote` | How long to wait for votes after receiving a proposal | 1s |
| `TimeoutCommit` | **Minimum delay between blocks (target block time)** | 0 (immediate) |
| `BackoffMultiplier` | Factor by which timeouts increase after failed rounds | 1.5 |
| `MaxTimeout` | Maximum timeout duration (cap on backoff) | 30s |
| `SkipTimeoutCommit` | Skip commit delay when all votes received (fast path) | false |

### Preset Configurations

```go
// 1. Default: Optimistically responsive (fastest possible)
// Blocks are produced as fast as the network allows
config := hotstuff2.DefaultPacemakerConfig()
// TimeoutPropose: 1s, TimeoutCommit: 0

// 2. Production: Target block time for public blockchains
// Pass your desired block time
config := hotstuff2.ProductionPacemakerConfig(5 * time.Second)
// TimeoutPropose: 5s, TimeoutCommit: 5s

// 3. Demo: Visible block production for demos/testing
config := hotstuff2.DemoPacemakerConfig()
// TimeoutPropose: 2s, TimeoutCommit: 1s (~1 block/sec)
```

### Understanding Block Time

The **key parameter for block time is `TimeoutCommit`**. This is the minimum delay
enforced after committing a block before the next view starts proposing.

- `TimeoutCommit = 0`: Optimistically responsive. Blocks produced as fast as network RTT allows.
- `TimeoutCommit = 5s`: Approximately 5-second blocks under normal conditions.

Under degraded conditions (leader failures, network issues), actual block time may be
longer due to view changes and exponential backoff.

### Example: Public Blockchain with 10s Blocks

```go
cfg, err := hotstuff2.NewConfig[MyHash](
    // ... required options ...
    
    hotstuff2.WithPacemaker[MyHash](hotstuff2.PacemakerConfig{
        TimeoutPropose:    10 * time.Second,  // Wait up to 10s for proposal
        TimeoutVote:       5 * time.Second,   // Wait up to 5s for votes  
        TimeoutCommit:     10 * time.Second,  // Target 10s block time
        BackoffMultiplier: 1.5,               // 50% increase on failures
        MaxTimeout:        60 * time.Second,  // Cap at 1 minute
        SkipTimeoutCommit: false,             // Always enforce block time
    }),
)
```

### Example: Low-Latency Private Network

```go
cfg, err := hotstuff2.NewConfig[MyHash](
    // ... required options ...
    
    // Use default config for maximum speed
    // Blocks produced in ~3 RTT (propose + vote + QC)
)
```

## Timer Options

The Timer interface is used by the pacemaker for view timeouts. Two implementations are provided:

```go
import "github.com/edgedlt/hotstuff2/timer"

// 1. Production timer (recommended)
t := timer.NewRealTimer()

// 2. Mock timer for testing
t := timer.NewMockTimer()
t.Fire() // Manually trigger timeout
```

Note: Timeout durations and exponential backoff are now configured via `PacemakerConfig`
rather than the timer itself. See [Block Time Configuration](#block-time-configuration).

## Message Types

HotStuff-2 uses three message types:

| Type | Description | Sender |
|------|-------------|--------|
| `PROPOSAL` | Block proposal with justification QC | Leader |
| `VOTE` | Vote for a proposal | Replica |
| `NEWVIEW` | View change with highQC | All |

### Message Codec

Use `MessageCodec` for convenient message serialization in your network layer:

```go
// Create codec once with your deserializers
codec := hotstuff2.NewMessageCodec[MyHash](
    func(b []byte) (MyHash, error) {
        if len(b) != 32 {
            return MyHash{}, fmt.Errorf("invalid hash length")
        }
        var h MyHash
        copy(h[:], b)
        return h, nil
    },
    func(b []byte) (hotstuff2.Block[MyHash], error) {
        return MyBlockFromBytes(b)
    },
)

// Encode outgoing messages
func (n *MyNetwork) Broadcast(payload hotstuff2.ConsensusPayload[MyHash]) {
    data := codec.Encode(payload.(*hotstuff2.ConsensusMessage[MyHash]))
    for _, peer := range n.peers {
        peer.Send(data)
    }
}

// Decode incoming messages
func (n *MyNetwork) handleMessage(data []byte) {
    msg, err := codec.Decode(data)
    if err != nil {
        log.Warn("invalid message", "error", err)
        return
    }
    n.incoming <- msg
}
```

### Low-Level Message API

For more control, use the message constructors directly:

```go
// Creating messages (internal use)
proposal := hotstuff2.NewProposeMessage(view, myIndex, block, justifyQC)
vote := hotstuff2.NewVoteMessage(view, myIndex, voteObj)
newview := hotstuff2.NewNewViewMessage(view, myIndex, highQC)

// Deserializing received messages
msg, err := hotstuff2.MessageFromBytes(data, hashFromBytes, blockFromBytes)
switch msg.Type() {
case hotstuff2.MessageProposal:
    block := msg.Block()
    qc := msg.JustifyQC()
case hotstuff2.MessageVote:
    vote := msg.Vote()
case hotstuff2.MessageNewView:
    highQC := msg.HighQC()
}
```

## Network Requirements

### Scalability

HotStuff-2's efficient view-change protocol scales to hundreds of validators. Normal operation
uses O(n) messages per view, and view changes use O(n²) messages but occur rarely:

| Validators (n) | Max Faults (f) | Quorum (2f+1) | Use Case |
|----------------|----------------|---------------|----------|
| 4 | 1 | 3 | Development, testing |
| 21 | 7 | 15 | Consortium networks |
| 100 | 33 | 67 | Mid-size PoS chains |
| 200+ | 66+ | 134+ | Large validator sets |

Byzantine fault tolerance: `n >= 3f + 1` (tolerates up to f < n/3 Byzantine validators)

### Message Delivery

- Messages may be delayed but must eventually be delivered (partial synchrony)
- Messages may be reordered
- Messages must not be corrupted (use authenticated channels)

## Safety Guarantees

HotStuff-2 provides these safety properties (formally verified in TLA+):

1. **Agreement**: No two honest replicas commit different blocks at the same height
2. **Validity**: Only proposed blocks can be committed
3. **No Double Vote**: Replicas vote at most once per view

### Critical Safety Rules

1. **View Guard**: Votes are only accepted for the current view (exact equality)
2. **SafeNode Rule**: Vote only if proposal QC supersedes lock OR block extends lock
3. **QC Validation**: Always validate QCs have 2f+1 distinct signers

## Liveness Guarantees

Under partial synchrony (after GST), HotStuff-2 guarantees:

1. **Eventually Commit**: Some block is eventually committed
2. **View Synchronization**: All replicas eventually reach the same view
3. **View Completion**: Each view either forms a QC or times out

## Example: 4-Node Test Network

```go
func setupTestNetwork() ([]*hotstuff2.HotStuff2[TestHash], error) {
    n := 4
    validators, privKeys := NewTestValidatorSetWithKeys(n)
    
    // Create shared network channels
    networks := make([]*TestNetwork, n)
    for i := 0; i < n; i++ {
        networks[i] = NewTestNetwork()
    }
    
    // Create nodes
    nodes := make([]*hotstuff2.HotStuff2[TestHash], n)
    for i := 0; i < n; i++ {
        cfg, _ := hotstuff2.NewConfig[TestHash](
            hotstuff2.WithMyIndex[TestHash](uint16(i)),
            hotstuff2.WithValidators[TestHash](validators),
            hotstuff2.WithPrivateKey[TestHash](privKeys[i]),
            hotstuff2.WithStorage[TestHash](NewTestStorage()),
            hotstuff2.WithNetwork[TestHash](networks[i]),
            hotstuff2.WithExecutor[TestHash](NewTestExecutor()),
            hotstuff2.WithTimer[TestHash](timer.NewMockTimer()),
        )
        
        nodes[i], _ = hotstuff2.NewHotStuff2(cfg, func(b hotstuff2.Block[TestHash]) {
            log.Printf("Node %d committed block %d", i, b.Height())
        })
    }
    
    return nodes, nil
}
```

## State Management and Pruning

The consensus library maintains in-memory state for active consensus operations. Integrators are responsible for:

### Storage Layer Responsibilities

1. **Durability**: All `Put` operations must be durable before returning. Use sync writes for safety-critical state (`PutView`, `PutHighestLockedQC`).

2. **Checkpointing**: Implement periodic checkpoints of committed state. The library calls `onCommit` for each finalized block - use this to track what's safe to checkpoint.

3. **Pruning**: The library does not prune old blocks or QCs from your storage. Implement pruning behind finalized checkpoints based on your retention requirements.

4. **Recovery Loading**: On restart, only load recent state into the consensus context. The library needs:
   - Current view (`GetView`)
   - Locked QC (`GetHighestLockedQC`) 
   - Recent blocks for ancestry checking

### Memory Considerations

The library's `Context` maintains maps for:
- `blocksByHash`: Blocks seen during consensus
- `qcsByNode`: QCs received
- `parents`: Parent relationships for ancestry checks
- `votes`: Vote tracking (automatically pruned after 10 views)
- `newviews`: NEWVIEW tracking (automatically pruned after 10 views)

For long-running nodes, implement checkpointing to bound memory growth. The library only needs blocks within the "danger zone" (uncommitted chain) - typically a few views worth.

### Single-Node Testing

For n=1 test setups, consensus works correctly - the leader's single vote forms a quorum. No special configuration needed.

## Crash Recovery

```go
// On restart, load persisted state
view, _ := storage.GetView()
lockedQC, _ := storage.GetHighestLockedQC()
lastBlock, _ := storage.GetLastBlock()

// HotStuff2 automatically recovers from persisted state
hs, err := hotstuff2.NewHotStuff2(cfg, onCommit)
hs.Start() // Resumes from last known state
```

**Critical**: Always use sync writes for:
- `PutView()`
- `PutHighestLockedQC()`

## Observability

### Event Hooks

HotStuff-2 provides optional hooks for monitoring consensus events. Use `NewHotStuff2WithHooks` for full observability:

```go
hooks := &hotstuff2.Hooks[MyHash]{
    // Called when this node proposes a block (leader only)
    OnPropose: func(view uint32, block hotstuff2.Block[MyHash]) {
        metrics.ProposalsMade.Inc()
        log.Debug("proposed block", "view", view, "height", block.Height())
    },
    
    // Called when this node votes for a proposal
    OnVote: func(view uint32, blockHash MyHash) {
        metrics.VotesCast.Inc()
    },
    
    // Called when a quorum certificate is formed
    OnQCFormed: func(view uint32, qc hotstuff2.QuorumCertificate[MyHash]) {
        metrics.QCsFormed.Inc()
        log.Debug("QC formed", "view", view, "signers", len(qc.Signers()))
    },
    
    // Called when a block is committed (finalized)
    OnCommit: func(block hotstuff2.Block[MyHash]) {
        metrics.BlocksCommitted.Inc()
        metrics.CommittedHeight.Set(float64(block.Height()))
    },
    
    // Called when the view changes
    OnViewChange: func(oldView, newView uint32) {
        metrics.ViewChanges.Inc()
        metrics.CurrentView.Set(float64(newView))
    },
    
    // Called when a view times out
    OnTimeout: func(view uint32) {
        metrics.ViewTimeouts.Inc()
        log.Warn("view timeout", "view", view)
    },
}

hs, err := hotstuff2.NewHotStuff2WithHooks(cfg, hooks)
```

All hooks are optional - set only the ones you need. Hooks are invoked synchronously, so implementations should be fast or dispatch to a goroutine to avoid blocking consensus.

For backward compatibility, `NewHotStuff2(cfg, onCommit)` is still supported but deprecated.

### Read-Only State Access

Use `State()` to get a read-only snapshot of consensus state for monitoring dashboards:

```go
state := hs.State()

// Current view number
view := state.View()

// Height of last committed block
height := state.Height()

// View of the locked QC (safety lock)
lockedView := state.LockedQCView()

// View of the highest QC seen
highView := state.HighQCView()

// Total committed blocks
committed := state.CommittedCount()

// Example: Prometheus metrics
currentViewGauge.Set(float64(state.View()))
committedHeightGauge.Set(float64(state.Height()))
lockedViewGauge.Set(float64(state.LockedQCView()))
```

### Detailed Stats

For more detailed internal statistics, use the context stats:

```go
stats := hs.ctx.Stats()
// {
//   "view": 42,
//   "locked_qc_view": 40,
//   "high_qc_view": 41,
//   "committed_count": 38,
//   "blocks_count": 45,
//   "qcs_count": 42,
//   "total_votes": 156,
// }
```

## Error Handling

HotStuff-2 uses a small set of error classes for programmatic error handling. This design allows integrators to handle errors by category rather than matching dozens of specific error types.

### Error Classes

| Error Class | Type | When Returned | Recommended Action |
|-------------|------|---------------|-------------------|
| `ErrConfig` | Hard | Startup configuration invalid | Fix config and restart |
| `ErrInvalidMessage` | Soft | Malformed message from peer | Log and drop message |
| `ErrByzantine` | Soft | Potential Byzantine behavior | Log, possibly penalize peer |
| `ErrInternal` | Hard | Internal invariant violation | Investigate - likely a bug |

### Usage

Use `errors.Is()` to check error classes:

```go
import "errors"

cfg, err := hotstuff2.NewConfig[MyHash](...)
if err != nil {
    if errors.Is(err, hotstuff2.ErrConfig) {
        // Configuration error - must fix and restart
        log.Fatal("invalid configuration", "err", err)
    }
    log.Fatal("unexpected error", "err", err)
}
```

### Handling Different Error Types

```go
func handleConsensusError(err error) {
    switch {
    case errors.Is(err, hotstuff2.ErrConfig):
        log.Fatal("configuration error", "err", err)
    case errors.Is(err, hotstuff2.ErrInvalidMessage):
        log.Debug("dropping invalid message", "err", err)
    case errors.Is(err, hotstuff2.ErrByzantine):
        log.Warn("potential byzantine behavior", "err", err)
        metrics.ByzantineErrors.Inc()
    case errors.Is(err, hotstuff2.ErrInternal):
        log.Error("internal error", "err", err)
    }
}
```

Errors include descriptive messages - inspect `err.Error()` for details like `"configuration error: validators is required"`.

### Error Classification Reference

| Error Class | Example Conditions |
|-------------|-------------------|
| `ErrConfig` | Missing validators, private key, storage, network, executor, timer; invalid validator index; unsupported crypto scheme; insufficient validators |
| `ErrInvalidMessage` | Message too short; unknown message type; deserialization failure |
| `ErrByzantine` | Invalid signature; vote timestamp outside acceptable window |
| `ErrInternal` | QC created with mismatched votes; insufficient quorum (shouldn't happen in normal operation) |

## Performance Tuning

### Timeout Configuration

Configure timeouts via `PacemakerConfig`:

```go
// For fast networks (local/LAN) - low latency, fast recovery
hotstuff2.WithPacemaker[MyHash](hotstuff2.PacemakerConfig{
    TimeoutPropose:    200 * time.Millisecond,
    TimeoutVote:       100 * time.Millisecond,
    TimeoutCommit:     0,  // Optimistically responsive
    BackoffMultiplier: 1.5,
    MaxTimeout:        5 * time.Second,
})

// For slow networks (WAN/Internet) - tolerant of delays
hotstuff2.WithPacemaker[MyHash](hotstuff2.PacemakerConfig{
    TimeoutPropose:    2 * time.Second,
    TimeoutVote:       1 * time.Second,
    TimeoutCommit:     0,  // Or set target block time
    BackoffMultiplier: 2.0,
    MaxTimeout:        60 * time.Second,
})
```

### Network Buffer Sizes

```go
// Increase channel buffers for high throughput
msgChan := make(chan ConsensusPayload[H], 1000)
```

## References

- [HotStuff-2 Paper](https://eprint.iacr.org/2023/397.pdf)
- [Protocol Guide](./PROTOCOL.md)
- [TLA+ Specification](../formal-models/tla/hotstuff2.tla)
- [Implementation Mapping](../formal-models/tla/IMPLEMENTATION_MAPPING.md)
