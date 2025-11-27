# HotStuff-2 Benchmark Suite

Production-quality benchmarking infrastructure for the HotStuff-2 consensus implementation.

## Quick Start

```bash
# Run standard benchmarks and generate HTML report
./bench.sh

# Run quick benchmarks (crypto + QC only)
./bench.sh quick

# Run crypto benchmarks only
./bench.sh crypto

# Run full benchmark suite (includes timer benchmarks)
./bench.sh full
```

Output: `benchmark-YYYYMMDD-HHMMSS.html`

## Benchmark Coverage

### Cryptographic Operations (`internal/crypto/`)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkEd25519Sign` | Ed25519 signature generation |
| `BenchmarkEd25519Verify` | Ed25519 signature verification |
| `BenchmarkEd25519KeyGeneration` | Ed25519 key pair generation |
| `BenchmarkBLSSign` | BLS12-381 signature generation |
| `BenchmarkBLSVerify` | BLS12-381 signature verification |
| `BenchmarkBLSKeyGeneration` | BLS12-381 key pair generation |
| `BenchmarkBLSAggregate` | BLS signature aggregation (7 sigs) |
| `BenchmarkBLSAggregateVerify` | BLS aggregate signature verification |
| `BenchmarkBLSBatchVerify` | BLS batch verification (7 sigs) |

### QC Operations (`quorum_cert_test.go`)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkQCFormation_4Nodes` | QC formation with 4 validators |
| `BenchmarkQCFormation_7Nodes` | QC formation with 7 validators |
| `BenchmarkQCFormation_10Nodes` | QC formation with 10 validators |
| `BenchmarkQCFormation_22Nodes` | QC formation with 22 validators |
| `BenchmarkQCValidation_4Nodes` | QC validation with 4 validators |
| `BenchmarkQCValidation_7Nodes` | QC validation with 7 validators |
| `BenchmarkQCValidation_10Nodes` | QC validation with 10 validators |
| `BenchmarkQCValidation_22Nodes` | QC validation with 22 validators |
| `BenchmarkQCSerialization` | QC serialization performance |

### Vote Operations (`vote_test.go`)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkVoteCreation` | Vote creation with signature |
| `BenchmarkVoteVerification` | Vote signature verification |
| `BenchmarkVoteSerialization` | Vote serialization |

### Consensus Operations (`hotstuff2_bench_test.go`)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkVoteAggregation` | Collecting and aggregating votes |
| `BenchmarkQCCreationAndValidation_4Nodes` | Combined QC ops (4 nodes) |
| `BenchmarkQCCreationAndValidation_7Nodes` | Combined QC ops (7 nodes) |

### Timer Operations (`timer/timer_test.go`)

| Benchmark | Description |
|-----------|-------------|
| `BenchmarkRealTimerStart` | Real timer start performance |
| `BenchmarkRealTimerStartStop` | Timer start/stop cycle |
| `BenchmarkMockTimerFire` | Mock timer firing |
| `BenchmarkAdaptiveTimerOnTimeout` | Adaptive timeout handling |

## Report Features

### Executive Summary
- Key performance metrics at a glance
- Crypto scheme recommendation based on network size
- QC formation latency for different network sizes

### Crypto Comparison
- Side-by-side Ed25519 vs BLS12-381 metrics
- Performance ratios and speedup factors
- Bandwidth savings analysis

### QC Operations
- Formation and validation times by network size
- Memory usage and allocation counts
- Operations per second

### Detailed Benchmarks
- Full benchmark data organized by category
- Color-coded performance indicators:
  - Green: Fast (< 100μs)
  - Yellow: Medium (100μs - 1ms)
  - Red: Slow (> 1ms)

## Expected Performance

### Ed25519 (Recommended for 4-22 validators)
| Operation | Expected | Notes |
|-----------|----------|-------|
| Sign | ~50K ops/sec | ~20μs per op |
| Verify | ~20K ops/sec | ~50μs per op |
| Key Gen | ~50K ops/sec | ~20μs per op |

### BLS12-381 (Recommended for 25+ validators)
| Operation | Expected | Notes |
|-----------|----------|-------|
| Sign | ~7K ops/sec | ~140μs per op |
| Verify | ~900 ops/sec | ~1.1ms per op |
| Aggregate | ~190K ops/sec | ~5μs per op |

### QC Operations
| Network Size | Formation | Validation |
|--------------|-----------|------------|
| 4 nodes | ~0.5μs | ~50μs |
| 7 nodes | ~0.7μs | ~50μs |
| 10 nodes | ~1μs | ~75μs |
| 22 nodes | ~2μs | ~150μs |

## Architecture

```
bench/
├── README.md          # This file
├── bench.go           # Parsing and formatting utilities
├── generate.go        # HTML report generation
└── template.html      # HTML template with CSS

cmd/benchgen/
└── main.go            # CLI tool for report generation

bench.sh               # Main benchmark runner script
```

## Running Individual Benchmarks

```bash
# All crypto benchmarks
go test -bench="^Benchmark(Ed25519|BLS)" -benchmem ./internal/crypto

# Specific benchmark
go test -bench="BenchmarkQCFormation_7Nodes" -benchmem .

# With longer duration for accuracy
go test -bench="." -benchtime=10s -benchmem ./internal/crypto

# With CPU profiling
go test -bench="BenchmarkBLSVerify" -cpuprofile=cpu.prof ./internal/crypto
go tool pprof cpu.prof

# With memory profiling
go test -bench="BenchmarkQCFormation" -memprofile=mem.prof .
go tool pprof mem.prof
```

## CI/CD Integration

```yaml
# .github/workflows/benchmark.yml
name: Benchmarks
on:
  push:
    branches: [master]
  pull_request:

jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      
      - name: Run benchmarks
        run: ./bench.sh quick
      
      - name: Upload report
        uses: actions/upload-artifact@v4
        with:
          name: benchmark-report
          path: benchmark-*.html
```

## Interpreting Results

### Crypto Scheme Selection Guide

| Validators | Recommendation | Rationale |
|------------|----------------|-----------|
| 4-7 | Ed25519 | Fastest, stdlib-only |
| 7-22 | Ed25519 | Good performance, simpler |
| 22-50 | Either | Consider bandwidth needs |
| 50-100+ | BLS12-381 | ~75% bandwidth savings |

### Performance Factors

**Hardware:**
- CPU architecture (x86_64 vs ARM)
- CPU frequency and cache size
- Memory speed

**Software:**
- Go version (1.22+ recommended)
- GOMAXPROCS setting
- Background processes

### Troubleshooting

**Benchmarks running slow?**
- Close other applications
- Run on AC power (not battery)
- Use `-benchtime=10s` for stability
- Check CPU frequency scaling

**Report not generating?**
- Verify `bench/template.html` exists
- Check Go module is initialized
- Ensure write permissions

**Inconsistent results?**
- Run multiple times
- Use `-count=5` flag
- Check for thermal throttling

## Contributing

To add a new benchmark:

1. Add benchmark function:
```go
func BenchmarkMyFeature(b *testing.B) {
    // Setup (not timed)
    data := setup()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        myFeature(data)
    }
}
```

2. Add memory tracking:
```go
func BenchmarkMyFeature(b *testing.B) {
    b.ReportAllocs()
    // ...
}
```

3. Run `./bench.sh` - new benchmarks are automatically included

4. Optionally update `generate.go` to highlight in summary
