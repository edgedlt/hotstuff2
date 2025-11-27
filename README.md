# HotStuff-2

A production-ready Go implementation of the [HotStuff-2](https://eprint.iacr.org/2023/397.pdf) Byzantine Fault Tolerant consensus protocol.

[![Go Version](https://img.shields.io/badge/Go-1.23+-blue)](https://golang.org/dl/)
[![Tests](https://github.com/edgedlt/hotstuff2/actions/workflows/test.yml/badge.svg)](https://github.com/edgedlt/hotstuff2/actions)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Features

- **Two-phase BFT consensus** - Optimistic responsiveness with 2 network delays
- **Linear view-change** - O(n) data per message during leader changes (O(n²) total messages, amortized O(n))
- **Formally verified** - TLA+ model checked (12 safety invariants, 4 liveness properties)
- **Dual signature schemes** - Ed25519 (O(n) multi-sig) and BLS12-381 (O(1) aggregate sig)
- **Configurable block time** - Target block time from milliseconds to minutes
- **Adaptive pacemaker** - Exponential backoff timeouts for liveness
- **Generic design** - Supports custom hash types and block structures
- **Payload-agnostic** - Opaque block payload supports any execution model (transactions, DAG refs, rollup batches)
- **Byzantine testing** - Twins framework for fault injection scenarios

## Quick Start

### Try the Interactive Demo

See HotStuff-2 in action with real consensus running on a simulated network:

```bash
go run demo/main.go
# Open http://localhost:8080 in your browser
```

**Features:**
- Real-time visualization of nodes and message passing
- Fault injection (crash nodes, create network partitions)
- Configurable block time (optimistic vs 1-second target)
- Event log with CSV export
- Support for 4-16 node networks (f=1 to f=5)

See [demo/README.md](demo/README.md) for details.

### Integration Example

```go
package main

import (
    "time"
    
    "github.com/edgedlt/hotstuff2"
    "github.com/edgedlt/hotstuff2/timer"
    "go.uber.org/zap"
)

func main() {
    // Configure consensus
    cfg, _ := hotstuff2.NewConfig[MyHash](
        hotstuff2.WithMyIndex[MyHash](0),
        hotstuff2.WithValidators[MyHash](validators),
        hotstuff2.WithPrivateKey[MyHash](privateKey),
        hotstuff2.WithStorage[MyHash](storage),
        hotstuff2.WithNetwork[MyHash](network),
        hotstuff2.WithExecutor[MyHash](executor),
        hotstuff2.WithTimer[MyHash](timer.NewRealTimer()),
        hotstuff2.WithLogger[MyHash](zap.NewProduction()),
        
        // Optional: Configure target block time (default is optimistically responsive)
        hotstuff2.WithTargetBlockTime[MyHash](5 * time.Second),
    )

    // Create and start consensus
    hs, _ := hotstuff2.NewHotStuff2(cfg, func(block hotstuff2.Block[MyHash]) {
        log.Printf("Committed block %d", block.Height())
    })

    hs.Start()
    defer hs.Stop()
}
```

See the [Integration Guide](docs/INTEGRATION_GUIDE.md) for complete documentation.

## Installation

```bash
go get github.com/edgedlt/hotstuff2
```

**Requirements**: Go 1.23+

## Architecture

```
hotstuff2/
├── hotstuff2.go          # Core consensus state machine
├── config.go             # Configuration with functional options
├── context.go            # Consensus state management
├── types.go              # Core interfaces (Hash, Block, Storage, Network)
├── vote.go               # Vote creation and verification
├── quorum_cert.go        # Quorum certificate aggregation
├── pacemaker.go          # View synchronization and timeouts
├── payload.go            # Message serialization
├── timer/                # Timer implementations (Real, Mock, Adaptive)
├── internal/crypto/      # Ed25519 (multi-sig) and BLS12-381 (aggregate sig)
├── twins/                # Byzantine fault injection testing
└── formal-models/tla/    # TLA+ formal specification
```

## Interfaces

Implement these interfaces to integrate with your blockchain:

```go
// Your hash type
type Hash interface {
    Bytes() []byte
    Equals(other Hash) bool
    String() string
}

// Your block type (payload is opaque - consensus doesn't interpret it)
type Block[H Hash] interface {
    Hash() H
    Height() uint32
    PrevHash() H
    Payload() []byte  // Opaque: transactions, DAG refs, rollup batches, etc.
    ProposerIndex() uint16
    Timestamp() uint64
    Bytes() []byte
}

// Persistent storage (safety-critical)
type Storage[H Hash] interface {
    GetBlock(hash H) (Block[H], error)
    PutBlock(block Block[H]) error
    GetLastBlock() (Block[H], error)
    GetQC(nodeHash H) (QuorumCertificate[H], error)
    PutQC(qc QuorumCertificate[H]) error
    GetHighestLockedQC() (QuorumCertificate[H], error)
    PutHighestLockedQC(qc QuorumCertificate[H]) error
    GetView() (uint32, error)
    PutView(view uint32) error
    Close() error
}

// Network message delivery
type Network[H Hash] interface {
    Broadcast(payload ConsensusPayload[H])
    SendTo(validatorIndex uint16, payload ConsensusPayload[H])
    Receive() <-chan ConsensusPayload[H]
    Close() error
}

// Block execution
type Executor[H Hash] interface {
    Execute(block Block[H]) (stateHash H, err error)
    Verify(block Block[H]) error
    GetStateHash() H
    CreateBlock(height uint32, prevHash H, proposerIndex uint16) (Block[H], error)
}
```

## Protocol Overview

HotStuff-2 achieves consensus in two phases:

1. **Propose**: Leader broadcasts block with justification QC
2. **Vote**: Replicas vote if proposal satisfies SafeNode rule
3. **QC Formation**: Leader aggregates 2f+1 votes into QC
4. **Commit**: Two consecutive QCs commit the first block (two-chain rule)

**Safety**: Replicas only vote for proposals that extend their locked QC or have a higher justification QC.

**Liveness**: Adaptive timeouts with exponential backoff ensure progress under partial synchrony.

For details, see the [Protocol Guide](docs/PROTOCOL.md).

## Scalability

HotStuff-2's efficient view-change protocol enables scaling to hundreds of validators. During normal operation, communication is O(n) messages per view. View changes require O(n²) messages but occur rarely, keeping amortized complexity low. This makes HotStuff-2 suitable for both consortium networks and public proof-of-stake chains.

| Validators | Max Faults | Quorum | Use Case |
|------------|------------|--------|----------|
| 4          | 1          | 3      | Development, small teams |
| 21         | 7          | 15     | Consortium networks |
| 100        | 33         | 67     | Mid-size PoS chains |
| 200+       | 66+        | 134+   | Large validator sets |

Byzantine fault tolerance: `f < n/3` (tolerates up to f Byzantine validators)

## Formal Verification

The protocol is formally verified using TLA+ model checking:

**Safety Invariants** (12 properties verified):
- Agreement: No conflicting commits at same height
- Validity: Only proposed blocks can be committed
- No double voting: At most one vote per view per replica
- QC integrity: Quorum certificates require 2f+1 distinct signers

**Liveness Properties** (4 properties verified under fairness):
- Eventually some block commits
- View synchronization achieved
- Progress guaranteed under partial synchrony

**Verification Results**:
- 2.6M states explored
- 790K distinct states
- All properties verified

See [formal-models/tla/](formal-models/tla/) for the TLA+ specification.

## Testing

```bash
# Run all tests
go test ./...

# Run with race detection
go test -race ./...

# Run benchmarks
go test -bench=. -benchmem

# Run specific benchmark suite
./bench.sh quick
```

### Test Categories

- **Unit tests**: Individual component testing
- **Integration tests**: Multi-node consensus scenarios
- **Twins tests**: Byzantine fault injection (100+ scenarios)
- **Benchmarks**: Performance validation

## Performance

Benchmarked on AMD Ryzen 9 5900X, Go 1.24:

### Cryptographic Operations

| Operation | Ed25519 | BLS12-381 | Notes |
|-----------|---------|-----------|-------|
| Sign | 50.2K ops/sec (20μs) | 7.3K ops/sec (137μs) | Ed25519 ~7x faster |
| Verify | 21.4K ops/sec (47μs) | 914 ops/sec (1.1ms) | Ed25519 ~23x faster |
| Aggregate | N/A | 190.7K ops/sec (5μs) | BLS-only, O(1) signatures |

### QC Operations

| Operation | 4 nodes | 7 nodes | 10 nodes | 22 nodes |
|-----------|---------|---------|----------|----------|
| QC Formation | 506ns | 682ns | 864ns | 2.5μs |
| QC Validation | 47μs | 47μs | 47μs | 47μs |

### Signature Scheme Selection

| Validators | Recommended | QC Size | Rationale |
|------------|-------------|---------|-----------|
| 4-22 | Ed25519 | O(n) | Faster verification |
| 25-100 | Either | - | Consider bandwidth needs |
| 100+ | BLS | O(1) | 75% bandwidth savings |

Run benchmarks:

```bash
./bench.sh quick   # Quick benchmarks
./bench.sh full    # Full suite with HTML report
```

See [bench/README.md](bench/README.md) for benchmark documentation.

## Documentation

| Document | Description |
|----------|-------------|
| [Protocol Guide](docs/PROTOCOL.md) | Algorithm explanation (quick overview + deep dive) |
| [Integration Guide](docs/INTEGRATION_GUIDE.md) | How to integrate HotStuff-2 into your protocol |
| [Interactive Demo](demo/README.md) | Web-based demo with network simulation and fault injection |
| [TLA+ Specification](formal-models/tla/README.md) | Formal verification details |
| [Implementation Mapping](formal-models/tla/IMPLEMENTATION_MAPPING.md) | TLA+ to Go code mapping |
| [Twins Testing](twins/README.md) | Byzantine fault injection framework |
| [Benchmarks](bench/README.md) | Performance testing guide |

## Use Cases

HotStuff-2 is suitable for any blockchain requiring BFT consensus with fast finality:

**Public Networks**
- Proof-of-stake L1 chains
- Validator-based L2 networks
- Cross-chain bridge committees

**Consortium & Enterprise**
- Multi-organization networks
- Financial settlement systems
- Supply chain coordination

**Why HotStuff-2?**
- Sub-second finality (no probabilistic confirmation)
- Scales to hundreds of validators
- Tolerates up to 1/3 Byzantine faults
- Efficient view-change on leader failure

## Contributing

Contributions welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass (`go test ./...`)
5. Submit a pull request

## References

- [HotStuff-2 Paper](https://eprint.iacr.org/2023/397.pdf) - Protocol specification
- [Original HotStuff](https://arxiv.org/abs/1803.05069) - Foundation protocol
- [nspcc-dev/dbft](https://github.com/nspcc-dev/dbft) - Architecture inspiration

## Acknowledgments

- HotStuff-2 paper authors for the algorithm design
- [nspcc-dev/dbft](https://github.com/nspcc-dev/dbft) for interface patterns
- [relab/hotstuff](https://github.com/relab/hotstuff) for Twins testing approach

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
