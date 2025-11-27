package hotstuff2

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/timer"
	"go.uber.org/zap"
)

// NetworkBenchmark represents a network simulation benchmark configuration.
type NetworkBenchmark struct {
	Name             string
	Validators       int
	TargetBlocks     int
	NetworkLatencyMs int
	CryptoScheme     string
	Description      string
}

// NetworkSimulator simulates realistic network conditions for benchmarking.
type NetworkSimulator[H Hash] struct {
	nodes           []*HotStuff2[H]
	network         *LatencyNetwork[H]
	committed       map[int][]Block[H]
	commitMu        sync.Mutex
	startTime       time.Time
	firstCommitTime time.Time
	lastCommitTime  time.Time
}

// LatencyNetwork adds realistic network latency to message delivery.
type LatencyNetwork[H Hash] struct {
	baseNetwork  *SharedNetwork[H]
	latencyMs    int
	jitterMs     int
	messageCount int
	mu           sync.Mutex
}

// NewLatencyNetwork creates a network with simulated latency.
func NewLatencyNetwork[H Hash](n int, latencyMs, jitterMs int) *LatencyNetwork[H] {
	return &LatencyNetwork[H]{
		baseNetwork: NewSharedNetwork[H](n),
		latencyMs:   latencyMs,
		jitterMs:    jitterMs,
	}
}

// NodeNetwork returns a network adapter with latency for a specific node.
func (ln *LatencyNetwork[H]) NodeNetwork(nodeID int) Network[H] {
	return &LatencyAdapter[H]{
		baseAdapter: ln.baseNetwork.NodeNetwork(nodeID),
		latencyMs:   ln.latencyMs,
		jitterMs:    ln.jitterMs,
		ln:          ln,
	}
}

// LatencyAdapter adds latency to network operations.
type LatencyAdapter[H Hash] struct {
	baseAdapter Network[H]
	latencyMs   int
	jitterMs    int
	ln          *LatencyNetwork[H]
}

// Broadcast broadcasts with latency.
func (la *LatencyAdapter[H]) Broadcast(msg ConsensusPayload[H]) {
	la.ln.mu.Lock()
	la.ln.messageCount++
	la.ln.mu.Unlock()

	// Simulate network latency
	if la.latencyMs > 0 {
		delay := time.Duration(la.latencyMs) * time.Millisecond
		if la.jitterMs > 0 {
			// Add random jitter (±jitterMs)
			jitter := time.Duration(la.jitterMs/2) * time.Millisecond
			delay = delay + jitter // Simplified: no randomness for determinism
		}
		time.Sleep(delay)
	}

	la.baseAdapter.Broadcast(msg)
}

// SendTo sends to specific validator with latency.
func (la *LatencyAdapter[H]) SendTo(validatorIndex uint16, msg ConsensusPayload[H]) {
	la.ln.mu.Lock()
	la.ln.messageCount++
	la.ln.mu.Unlock()

	if la.latencyMs > 0 {
		time.Sleep(time.Duration(la.latencyMs) * time.Millisecond)
	}
	la.baseAdapter.SendTo(validatorIndex, msg)
}

// Receive returns the receive channel.
func (la *LatencyAdapter[H]) Receive() <-chan ConsensusPayload[H] {
	return la.baseAdapter.Receive()
}

// Close closes the adapter.
func (la *LatencyAdapter[H]) Close() error {
	return la.baseAdapter.Close()
}

// MessageCount returns total messages sent.
func (ln *LatencyNetwork[H]) MessageCount() int {
	ln.mu.Lock()
	defer ln.mu.Unlock()
	return ln.messageCount
}

// Close closes the network.
func (ln *LatencyNetwork[H]) Close() {
	ln.baseNetwork.Close()
}

// NewNetworkSimulator creates a network simulator with latency.
func NewNetworkSimulator[H Hash](config NetworkBenchmark) (*NetworkSimulator[H], error) {
	validators, privKeys := NewTestValidatorSetWithKeys(config.Validators)
	network := NewLatencyNetwork[H](config.Validators, config.NetworkLatencyMs, config.NetworkLatencyMs/10)

	sim := &NetworkSimulator[H]{
		nodes:     make([]*HotStuff2[H], config.Validators),
		network:   network,
		committed: make(map[int][]Block[H]),
	}

	// Create nodes
	for i := 0; i < config.Validators; i++ {
		storage := NewMockStorage[H]()
		executor := NewMockExecutor[H]()
		mockTimer := timer.NewMockTimer()

		cfg := &Config[H]{
			Logger:       zap.NewNop(),
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(i),
			PrivateKey:   privKeys[i],
			CryptoScheme: config.CryptoScheme,
			Storage:      storage,
			Network:      network.NodeNetwork(i),
			Executor:     executor,
		}

		nodeIdx := i
		onCommit := func(block Block[H]) {
			sim.commitMu.Lock()
			if len(sim.committed[nodeIdx]) == 0 {
				// First commit
				if sim.firstCommitTime.IsZero() {
					sim.firstCommitTime = time.Now()
				}
			}
			sim.committed[nodeIdx] = append(sim.committed[nodeIdx], block)
			sim.lastCommitTime = time.Now()
			sim.commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		if err != nil {
			return nil, fmt.Errorf("failed to create node %d: %w", i, err)
		}

		sim.nodes[i] = node
	}

	return sim, nil
}

// Run runs the simulation until target blocks are committed.
func (ns *NetworkSimulator[H]) Run(targetBlocks int, timeoutSeconds int) error {
	// Start all nodes
	for i, node := range ns.nodes {
		if err := node.Start(); err != nil {
			return fmt.Errorf("failed to start node %d: %w", i, err)
		}
	}

	ns.startTime = time.Now()

	// Wait for target blocks
	timeout := time.After(time.Duration(timeoutSeconds) * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			ns.Stop()
			return fmt.Errorf("timeout after %d seconds, committed blocks: %v",
				timeoutSeconds, ns.getCommittedCounts())

		case <-ticker.C:
			ns.commitMu.Lock()
			allReady := true
			for i := 0; i < len(ns.nodes); i++ {
				if len(ns.committed[i]) < targetBlocks {
					allReady = false
					break
				}
			}
			ns.commitMu.Unlock()

			if allReady {
				ns.Stop()
				return nil
			}
		}
	}
}

// Stop stops all nodes.
func (ns *NetworkSimulator[H]) Stop() {
	for _, node := range ns.nodes {
		node.Stop()
	}
	// Allow goroutines spawned by nodes to finish before closing network
	time.Sleep(10 * time.Millisecond)
	ns.network.Close()
}

// GetStats returns benchmark statistics.
func (ns *NetworkSimulator[H]) GetStats() BenchmarkStats {
	ns.commitMu.Lock()
	defer ns.commitMu.Unlock()

	totalBlocks := 0
	for i := 0; i < len(ns.nodes); i++ {
		totalBlocks += len(ns.committed[i])
	}

	avgBlocksPerNode := float64(totalBlocks) / float64(len(ns.nodes))
	totalDuration := ns.lastCommitTime.Sub(ns.startTime)
	timeToFirstBlock := ns.firstCommitTime.Sub(ns.startTime)

	var tps float64
	if totalDuration.Seconds() > 0 {
		tps = avgBlocksPerNode / totalDuration.Seconds()
	}

	return BenchmarkStats{
		TotalDuration:    totalDuration,
		TimeToFirstBlock: timeToFirstBlock,
		TotalBlocks:      int(avgBlocksPerNode),
		TPS:              tps,
		MessageCount:     ns.network.MessageCount(),
	}
}

// getCommittedCounts returns the number of committed blocks per node.
func (ns *NetworkSimulator[H]) getCommittedCounts() []int {
	counts := make([]int, len(ns.nodes))
	for i := 0; i < len(ns.nodes); i++ {
		counts[i] = len(ns.committed[i])
	}
	return counts
}

// BenchmarkStats contains benchmark statistics.
type BenchmarkStats struct {
	TotalDuration    time.Duration
	TimeToFirstBlock time.Duration
	TotalBlocks      int
	TPS              float64
	MessageCount     int
}

// BenchmarkNetwork_LAN tests consensus with LAN latency (1ms).
func BenchmarkNetwork_LAN_4Nodes_Ed25519(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "LAN-4Nodes-Ed25519",
		Validators:       4,
		TargetBlocks:     10,
		NetworkLatencyMs: 1,
		CryptoScheme:     "ed25519",
		Description:      "4 validators, 1ms latency (LAN)",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 30); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// BenchmarkNetwork_WAN tests consensus with WAN latency (50ms).
func BenchmarkNetwork_WAN_4Nodes_Ed25519(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "WAN-4Nodes-Ed25519",
		Validators:       4,
		TargetBlocks:     10,
		NetworkLatencyMs: 50,
		CryptoScheme:     "ed25519",
		Description:      "4 validators, 50ms latency (WAN)",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 60); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// BenchmarkNetwork_HighLatency tests consensus with high latency (100ms).
func BenchmarkNetwork_HighLatency_4Nodes_Ed25519(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "HighLatency-4Nodes-Ed25519",
		Validators:       4,
		TargetBlocks:     10,
		NetworkLatencyMs: 100,
		CryptoScheme:     "ed25519",
		Description:      "4 validators, 100ms latency (Intercontinental)",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 90); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// BenchmarkNetwork_LAN_7Nodes tests with 7 validators.
func BenchmarkNetwork_LAN_7Nodes_Ed25519(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "LAN-7Nodes-Ed25519",
		Validators:       7,
		TargetBlocks:     10,
		NetworkLatencyMs: 1,
		CryptoScheme:     "ed25519",
		Description:      "7 validators, 1ms latency (LAN)",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 30); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// BenchmarkNetwork_WAN_7Nodes tests with 7 validators and WAN latency.
func BenchmarkNetwork_WAN_7Nodes_Ed25519(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "WAN-7Nodes-Ed25519",
		Validators:       7,
		TargetBlocks:     10,
		NetworkLatencyMs: 50,
		CryptoScheme:     "ed25519",
		Description:      "7 validators, 50ms latency (WAN)",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 60); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// BenchmarkNetwork_LAN_BLS tests BLS with LAN latency.
func BenchmarkNetwork_LAN_4Nodes_BLS(b *testing.B) {
	config := NetworkBenchmark{
		Name:             "LAN-4Nodes-BLS",
		Validators:       4,
		TargetBlocks:     10,
		NetworkLatencyMs: 1,
		CryptoScheme:     "bls",
		Description:      "4 validators, 1ms latency (LAN), BLS signatures",
	}

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sim, err := NewNetworkSimulator[TestHash](config)
		if err != nil {
			b.Fatal(err)
		}

		b.StartTimer()
		if err := sim.Run(config.TargetBlocks, 30); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()

		stats := sim.GetStats()
		b.ReportMetric(stats.TPS, "blocks/sec")
		b.ReportMetric(float64(stats.MessageCount), "messages")
		b.ReportMetric(stats.TimeToFirstBlock.Seconds()*1000, "ttfb_ms")
	}
}

// TestNetworkSimulation is a regular test to verify network simulation works.
func TestNetworkSimulation(t *testing.T) {
	config := NetworkBenchmark{
		Name:             "Test-4Nodes",
		Validators:       4,
		TargetBlocks:     3,
		NetworkLatencyMs: 1,
		CryptoScheme:     "ed25519",
		Description:      "Smoke test for network simulation",
	}

	sim, err := NewNetworkSimulator[TestHash](config)
	if err != nil {
		t.Fatal(err)
	}

	if err := sim.Run(config.TargetBlocks, 10); err != nil {
		t.Fatal(err)
	}

	stats := sim.GetStats()
	t.Logf("Stats: TPS=%.2f, Messages=%d, Duration=%v, TTFB=%v",
		stats.TPS, stats.MessageCount, stats.TotalDuration, stats.TimeToFirstBlock)

	// Check we committed at least the target blocks (may commit more before stopping)
	if stats.TotalBlocks < config.TargetBlocks {
		t.Errorf("Expected at least %d blocks, got %d", config.TargetBlocks, stats.TotalBlocks)
	}
}
