# HotStuff-2 Interactive Demo

A web-based visualization of the HotStuff-2 Byzantine Fault Tolerant consensus protocol running with a simulated network layer.

## Quick Start

```bash
# From the repository root
go run demo/main.go
```

Then open http://localhost:8080 in your browser.

## Features

### Real-Time Visualization
- Watch consensus nodes propose, vote, and commit blocks
- See the leader (crown icon) rotate through views
- Observe QC formation and the two-chain commit rule in action

### Configurable Parameters

**Network Size**
- Choose f=1 to f=5 (4 to 16 nodes)
- Demonstrates scaling from small to medium-sized validator sets
- Formula: n = 3f + 1 nodes, quorum = 2f + 1

**Block Time Mode**
- **Fast (Optimistic)**: Blocks produced as fast as possible (testing mode)
- **Slow (1s target)**: ~1 second block time for better visibility
- Helps visualize the two-phase commit process

**Difficulty Levels**
- **Level 1: Happy Path** - Perfect network, demonstrates normal consensus
- **Level 2: Byzantine Weather** - Occasional packet loss and delays
- **Level 3: Chaos Mode** - High packet loss and latency

### Fault Injection Controls
- **Crash Random Node**: Simulate a node failure (consensus continues if ≤f faults)
- **Create Partition**: Split the network (consensus halts until healed)
- **Heal All**: Recover all crashed nodes and clear partitions

### Event Log Features
- Real-time log of all consensus events
- **Clear**: Reset the event log
- **Copy Log**: Export events as CSV (paste into Excel/Sheets for analysis)
- Events include: commits, crashes, view changes, partitions

## Architecture

```
demo/
├── main.go                 # Entry point - starts HTTP server
├── server/
│   └── server.go           # HTTP API and static file serving
├── simulator/
│   ├── simulator.go        # Core simulation engine
│   ├── clock.go            # Deterministic time control
│   ├── network.go          # Network with fault injection
│   └── types.go            # Simulation-specific types
└── web/
    ├── index.html          # Main page
    ├── style.css           # Styling
    └── app.js              # Frontend logic and rendering
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/state` | GET | Get current demo state |
| `/api/start` | POST | Start the demo |
| `/api/stop` | POST | Stop the demo |
| `/api/reset?level=N&f=N&blocktime=N` | POST | Reset with parameters |
| `/api/level?value=N` | POST | Set difficulty level (0-2) |
| `/api/crash?node=N` | POST | Crash a specific node |
| `/api/recover?node=N` | POST | Recover a crashed node |
| `/api/partition` | POST | Set network partitions (JSON body) |
| `/api/heal` | POST | Heal all faults (crashes + partitions) |

## Understanding the Visualization

### Node Display
- **Green circle**: Active node
- **Blue circle**: Current leader (also has crown icon)
- **Red circle**: Crashed node
- **Yellow circle**: Partitioned node
- **Crown icon**: Current view leader
- **V:N**: Current view number (never resets, monotonically increasing)
- **H:N**: Committed block height (0, 1, 2, 3...)

### Network Lines
- Faint lines show potential message paths between nodes
- In partition scenarios, some paths are blocked

### Blockchain View
- Green blocks at the bottom show committed blocks
- Chain links show parent-child relationships

### Event Log
- Real-time log of consensus events
- Color-coded by event type (commits, drops, crashes, etc.)

## HotStuff-2 Protocol Overview

HotStuff-2 achieves consensus in two phases:

1. **Propose**: Leader broadcasts block with justification QC
2. **Vote**: Replicas vote if SafeNode rule is satisfied
3. **QC Formation**: Leader aggregates 2f+1 votes
4. **Commit**: Two consecutive QCs commit the first block (two-chain rule)

Key properties demonstrated:
- **Safety**: No two honest nodes commit different blocks at the same height
- **Liveness**: Progress continues despite f < n/3 Byzantine faults
- **Linear view-change**: O(n) messages for leader rotation

## Development

### Running Tests
```bash
go test ./demo/... -v
```

### Building
```bash
go build -o demo demo/main.go
./demo
```

The demo runs on port 8080 by default.

## References

- [HotStuff-2 Paper](https://eprint.iacr.org/2023/397.pdf)
- [TigerBeetle Simulator](https://sim.tigerbeetle.com/)
- [TLA+ Specification](../formal-models/tla/)
