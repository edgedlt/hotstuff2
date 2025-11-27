# Twins Framework for Byzantine Fault Testing

This package implements the **Twins** framework for systematic Byzantine fault testing of the HotStuff-2 consensus protocol.

## Overview

The Twins framework creates "twin" replicas that share the same cryptographic keys but can behave independently. This allows comprehensive testing of:

- **Equivocation**: Signing conflicting blocks in the same view
- **Double-signing**: Creating multiple valid signatures for different messages
- **Network partitions**: Testing consensus under network splits
- **Byzantine behavior**: Silent nodes, random behavior, malicious actions
- **Safety violations**: Fork detection, invalid QCs

Based on *"Twins: BFT Systems Made Robust"* by Bano et al.

## Key Concepts

### Twin Pairs

A **twin pair** consists of two nodes that:
- Share the same validator index
- Share the same private key (can both sign as the same validator)
- Can behave independently (Byzantine behavior)
- Receive messages independently based on network partitions

Example with 3 honest replicas + 1 twin pair:
```
Node 0: Validator 0 (honest)
Node 1: Validator 1 (honest)
Node 2: Validator 2 (honest)
Node 3: Validator 3 (twin A) ← share keys
Node 4: Validator 3 (twin B) ← share keys
```

### BFT Constraints

For n total validators with f Byzantine:
- Requires: `n ≥ 3f + 1`
- Twin pairs count as 1 validator but can exhibit 1 Byzantine fault
- Example: 4 validators (3 honest + 1 twin pair) can tolerate f=1 Byzantine faults

## Architecture

### Core Components

1. **Scenario** (`twins.go`): Defines a test configuration
   - Number of replicas and twin pairs
   - Network partitions
   - Byzantine behavior type
   - Number of views to run

2. **Executor** (`executor.go`): Runs scenarios
   - Creates consensus nodes (honest + twins)
   - Configures shared keys for twins
   - Applies Byzantine behaviors
   - Tracks violations

3. **ViolationDetector** (`detector.go`): Monitors for safety violations
   - Double-sign detection
   - Fork detection (conflicting commits)
   - Invalid QC detection

4. **TwinsNetwork** (`network.go`): Network simulation
   - Message routing with partition support
   - Message recording for analysis
   - Deterministic message delivery
   - **Message interception layer** for Byzantine injection

5. **Interceptors** (`interceptors.go`): Byzantine behavior injection
   - `SilentInterceptor`: Drops all messages (crash fault)
   - `DoubleSignInterceptor`: Creates conflicting votes
   - `EquivocationInterceptor`: Routes messages to specific partitions
   - `RandomBehaviorInterceptor`: Randomly drops messages
   - `DelayInterceptor`: Delays and reorders messages
   - `PassthroughInterceptor`: Records votes without modification

6. **Generator** (`generator.go`): Random scenario generation
   - Configurable complexity bounds
   - Automatic BFT constraint validation
   - Edge case generation

## Usage

### Basic Testing

```go
// Create a scenario
scenario := Scenario{
    Replicas: 4,
    Twins:    0,
    Views:    5,
    Behavior: BehaviorHonest,
}

// Execute scenario
executor, _ := NewExecutor[TestHash256](scenario)
result := executor.Execute()

// Check results
if !result.Success {
    fmt.Printf("Violations detected: %+v\n", result.Violations)
}
```

### Random Scenario Generation

```go
// Generate 100 random valid scenarios
gen := NewGenerator(DefaultGeneratorConfig())
scenarios := gen.GenerateN(100)

// Execute all scenarios
for _, scenario := range scenarios {
    executor, _ := NewExecutor[TestHash256](scenario)
    result := executor.Execute()
    // ... check results
}
```

### Comprehensive Testing

```go
// Generate mix of edge cases + random scenarios
scenarios := GenerateComprehensive(100)

for _, scenario := range scenarios {
    executor, _ := NewExecutor[TestHash256](scenario)
    result := executor.Execute()
    
    // Honest scenarios should never have violations
    if scenario.Behavior == BehaviorHonest && scenario.Twins == 0 {
        assert.True(t, result.Success, "No false positives allowed")
    }
}
```

## Byzantine Behaviors

### Implemented Behaviors

All Byzantine behaviors are fully implemented:

1. **BehaviorHonest**: All nodes follow the protocol faithfully
2. **BehaviorDoubleSign**: Twins create conflicting votes in the same view
   - Records original vote and generates a conflicting vote
   - Detected by ViolationDetector with 100% accuracy
   - Typical scenarios detect 24-89 violations
3. **BehaviorSilent**: Twins completely stop communicating (crash fault simulation)
   - All outgoing messages dropped
   - All incoming messages dropped
   - Tests consensus resilience under f crash faults
4. **BehaviorEquivocation**: Twins send messages only to first network partition
   - Simulates partition-aware equivocation
   - Votes/proposals only reach subset of nodes
5. **BehaviorRandom**: Twins randomly drop messages
   - 20% of outgoing messages dropped
   - 10% of incoming messages dropped
   - Tests consensus under unreliable conditions

6. **BehaviorDelay**: Twins delay and reorder messages
   - Configurable minimum and maximum delay
   - Optional message reordering within delay window
   - Tests consensus under network latency conditions

### Future Behaviors

- **BehaviorCorrupted**: Send malformed messages

## Violation Detection

### Detected Violations

1. **ViolationFork**: Different blocks committed at same height
   - Indicates safety violation
   - Should NEVER happen with honest nodes

2. **ViolationDoubleSign**: Same validator signed conflicting blocks in same view
   - Indicates Byzantine behavior
   - Detects equivocation attempts

3. **ViolationInvalidQC**: QC with invalid signatures
   - Indicates crypto attack or implementation bug

4. **ViolationSafetyViolation**: SafeNode rule violated
   - Voted for conflicting blocks

### False Positive Prevention

**Critical requirement**: Honest nodes must NEVER trigger violations.

The comprehensive test suite verifies:
```go
// For every scenario with honest-only behavior
if scenario.Behavior == BehaviorHonest && scenario.Twins == 0 {
    assert.Equal(t, 0, len(violations), "No false positives")
}
```

## Test Results

### Test Suite

- 55+ tests passing
- 100+ comprehensive scenarios tested
- 0 false positives detected
- Byzantine violations correctly detected

### Scenario Coverage

- 3-7 node configurations
- 0-2 twin pairs
- All Byzantine behaviors tested
- Network partitions
- Silent/crash faults
- Random Byzantine behavior

### Performance

- Fast scenarios: 50-150ms (most honest scenarios)
- Slow scenarios: 2+ seconds (network partitions)
- Throughput: 50-900+ blocks committed per scenario

### Violation Detection Verified

| Behavior | Detection Rate | Typical Violations |
|----------|---------------|-------------------|
| DoubleSign | 100% | 24-89 per scenario |
| Silent | N/A | 0 (crash fault) |
| Equivocation | N/A | Messages routed to partition |
| Random | Variable | 0-5 per scenario |
| Delay | N/A | Safety maintained under delays |

**Double-sign detection**: Working  
**Fork detection**: Working  
**No false positives**: 0 violations on honest scenarios  
**Multi-violation tracking**: Multiple violations detected correctly  

## File Structure

```
twins/
├── twins.go          # Core types: Scenario, Violation, Executor
├── executor.go       # Scenario execution engine
├── detector.go       # Safety violation detection
├── network.go        # Network simulator with partitions + interceptor layer
├── interceptors.go   # Byzantine behavior interceptors
├── generator.go      # Random scenario generation
├── twins_test.go     # Comprehensive test suite (55+ tests)
└── README.md         # This file
```

## Integration with HotStuff-2

The twins framework tests the HotStuff-2 implementation by:

1. **Creating multiple consensus instances** with shared/independent keys
2. **Simulating network conditions** (partitions, delays, message loss)
3. **Injecting Byzantine behaviors** (double-signing, silence, random)
4. **Monitoring for safety violations** (forks, invalid QCs)
5. **Verifying liveness properties** (progress under f faults)

All testing is done with the actual consensus implementation - no mocks or simplified logic.

## Possible Future Work

- Test leader election attacks
- Test view change manipulation
- Performance profiling of scenarios
- Visualization of violation patterns
- Message timing analysis for delay scenarios

## References

- *"Twins: BFT Systems Made Robust"* - Bano et al.
- *"HotStuff: BFT Consensus with Linearity and Responsiveness"* - Yin et al.
- *"HotStuff-2: Optimal Two-Phase Responsive BFT"* - Yin et al.
