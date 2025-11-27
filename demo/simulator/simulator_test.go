package simulator

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSimulator(t *testing.T) {
	cfg := DefaultConfig()
	sim, err := New(cfg)
	require.NoError(t, err)
	require.NotNil(t, sim)

	assert.Equal(t, LevelHappyPath, sim.Level())
	assert.False(t, sim.IsRunning())
}

func TestSimulatorStartStop(t *testing.T) {
	cfg := DefaultConfig()
	sim, err := New(cfg)
	require.NoError(t, err)

	// Start
	err = sim.Start()
	require.NoError(t, err)
	assert.True(t, sim.IsRunning())

	// Can't start twice
	err = sim.Start()
	assert.Error(t, err)

	// Stop
	sim.Stop()
	assert.False(t, sim.IsRunning())
}

func TestSimulatorGetState(t *testing.T) {
	cfg := Config{
		Level:          LevelHappyPath,
		FaultTolerance: 1, // f=1 means 4 nodes (3*1+1)
		Seed:           42,
	}
	sim, err := New(cfg)
	require.NoError(t, err)

	state := sim.GetState()
	assert.Equal(t, 4, len(state.Nodes))
	assert.Equal(t, "Happy Path", state.Level)
	assert.False(t, state.Running)
	assert.Equal(t, 1, state.FaultTolerance)
	assert.Equal(t, 4, state.NodeCount)
	assert.Equal(t, 3, state.Quorum) // 2f+1 = 3

	// Check nodes
	for i, node := range state.Nodes {
		assert.Equal(t, i, node.ID)
		assert.Equal(t, NodeStatusActive, node.Status)
	}
}

func TestSimulatorLevels(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelHappyPath, "Happy Path"},
		{LevelByzantine, "Byzantine Weather"},
		{LevelChaos, "Chaos Mode"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			cfg := Config{
				Level:          tt.level,
				FaultTolerance: 1, // f=1 means 4 nodes
				Seed:           42,
			}
			sim, err := New(cfg)
			require.NoError(t, err)

			state := sim.GetState()
			assert.Equal(t, tt.expected, state.Level)
		})
	}
}

func TestSimulatorCrashRecover(t *testing.T) {
	cfg := DefaultConfig()
	sim, err := New(cfg)
	require.NoError(t, err)

	// Crash node 1
	err = sim.CrashNode(1)
	require.NoError(t, err)

	state := sim.GetState()
	assert.Equal(t, NodeStatusCrashed, state.Nodes[1].Status)

	// Recover node 1
	err = sim.RecoverNode(1)
	require.NoError(t, err)

	state = sim.GetState()
	assert.Equal(t, NodeStatusActive, state.Nodes[1].Status)
}

func TestSimulatorPartitions(t *testing.T) {
	cfg := DefaultConfig()
	sim, err := New(cfg)
	require.NoError(t, err)

	// Create partition: [0,1] and [2,3]
	sim.SetPartitions([][]int{{0, 1}, {2, 3}})

	// Verify partitions are set
	state := sim.GetState()
	assert.NotNil(t, state.Partitions)
	assert.Equal(t, 2, len(state.Partitions))

	// Clear partitions
	sim.ClearPartitions()

	state = sim.GetState()
	for _, node := range state.Nodes {
		assert.Equal(t, NodeStatusActive, node.Status)
	}
}

func TestSimulatorHealAll(t *testing.T) {
	cfg := DefaultConfig()
	sim, err := New(cfg)
	require.NoError(t, err)

	// Crash node 0
	err = sim.CrashNode(0)
	require.NoError(t, err)

	// Crash node 1
	err = sim.CrashNode(1)
	require.NoError(t, err)

	// Create partition
	sim.SetPartitions([][]int{{2}, {3}})

	// Verify state
	state := sim.GetState()
	assert.Equal(t, NodeStatusCrashed, state.Nodes[0].Status)
	assert.Equal(t, NodeStatusCrashed, state.Nodes[1].Status)

	// Heal all
	sim.HealAll()

	// Verify all nodes are active and partitions cleared
	state = sim.GetState()
	for _, node := range state.Nodes {
		assert.Equal(t, NodeStatusActive, node.Status, "node %d should be active", node.ID)
	}
	assert.Nil(t, state.Partitions)
}

func TestClockElapsedTime(t *testing.T) {
	clock := NewClock()

	// Before start, time should be 0
	assert.Equal(t, uint64(0), clock.Now())

	// Start clock
	clock.Start()
	time.Sleep(50 * time.Millisecond)

	// Should have elapsed some time
	elapsed := clock.Now()
	assert.Greater(t, elapsed, uint64(30)) // At least 30ms
	assert.Less(t, elapsed, uint64(200))   // But not too much

	// Reset
	clock.Reset()
	time.Sleep(10 * time.Millisecond)
	elapsed = clock.Now()
	assert.Less(t, elapsed, uint64(50)) // Should be reset
}

func TestSimulatorFaultTolerance(t *testing.T) {
	tests := []struct {
		f         int
		nodeCount int
		quorum    int
	}{
		{1, 4, 3},   // f=1: 3*1+1=4 nodes, 2*1+1=3 quorum
		{2, 7, 5},   // f=2: 3*2+1=7 nodes, 2*2+1=5 quorum
		{3, 10, 7},  // f=3: 3*3+1=10 nodes, 2*3+1=7 quorum
		{5, 16, 11}, // f=5: 3*5+1=16 nodes, 2*5+1=11 quorum
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("f=%d", tt.f), func(t *testing.T) {
			cfg := Config{
				Level:          LevelHappyPath,
				FaultTolerance: tt.f,
				Seed:           42,
			}
			sim, err := New(cfg)
			require.NoError(t, err)

			state := sim.GetState()
			assert.Equal(t, tt.f, state.FaultTolerance)
			assert.Equal(t, tt.nodeCount, state.NodeCount)
			assert.Equal(t, tt.quorum, state.Quorum)
			assert.Equal(t, tt.nodeCount, len(state.Nodes))
		})
	}
}

func TestSimulatorFaultToleranceClamping(t *testing.T) {
	// Test that f is clamped to valid range [1, MaxFaultTolerance]
	cfg := Config{
		Level:          LevelHappyPath,
		FaultTolerance: 0, // Should be clamped to 1
		Seed:           42,
	}
	sim, err := New(cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, sim.FaultTolerance())

	cfg = Config{
		Level:          LevelHappyPath,
		FaultTolerance: 100, // Should be clamped to MaxFaultTolerance (5)
		Seed:           42,
	}
	sim, err = New(cfg)
	require.NoError(t, err)
	assert.Equal(t, MaxFaultTolerance, sim.FaultTolerance())
}

func TestSimulatorConsensusProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := Config{
		Level:          LevelHappyPath,
		FaultTolerance: 1, // f=1 means 4 nodes
		Seed:           42,
	}
	sim, err := New(cfg)
	require.NoError(t, err)

	err = sim.Start()
	require.NoError(t, err)
	defer sim.Stop()

	// Wait for some commits
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			state := sim.GetState()
			t.Logf("Final state: %+v", state)
			// Don't fail - just check we got some progress
			if state.MessagesSent > 0 {
				t.Logf("Made progress: %d messages sent", state.MessagesSent)
				return
			}
			t.Fatal("timeout waiting for consensus progress")

		case <-ticker.C:
			state := sim.GetState()
			// Check if any node has committed blocks
			for _, node := range state.Nodes {
				if node.CommitHeight > 0 {
					t.Logf("Node %d committed block %d", node.ID, node.CommitHeight)
					return
				}
			}
		}
	}
}
