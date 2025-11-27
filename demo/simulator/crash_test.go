package simulator

import (
	"testing"
	"time"
)

// TestCrashSurvivability verifies that the system can survive one node crash with f=1.
func TestCrashSurvivability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := Config{
		Level:          LevelHappyPath,
		FaultTolerance: 1, // f=1 means 4 nodes, tolerate 1 crash
		Seed:           42,
	}
	sim, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create simulator: %v", err)
	}

	err = sim.Start()
	if err != nil {
		t.Fatalf("failed to start simulator: %v", err)
	}
	defer sim.Stop()

	// Wait for initial progress
	time.Sleep(2 * time.Second)

	state := sim.GetState()
	initialCommits := uint32(0)
	for _, node := range state.Nodes {
		if node.CommitHeight > initialCommits {
			initialCommits = node.CommitHeight
		}
	}
	t.Logf("Initial commits before crash: %d", initialCommits)

	// Crash node 0
	t.Logf("Crashing node 0...")
	err = sim.CrashNode(0)
	if err != nil {
		t.Fatalf("failed to crash node 0: %v", err)
	}

	// Wait for system to make progress with 3 nodes
	timeout := time.After(15 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	targetCommits := initialCommits + 3 // Want 3 more blocks after crash

	for {
		select {
		case <-timeout:
			state := sim.GetState()
			t.Logf("Final state after crash:")
			for _, node := range state.Nodes {
				t.Logf("  Node %d: status=%s, view=%d, commits=%d",
					node.ID, node.Status, node.View, node.CommitHeight)
			}
			t.Fatalf("timeout: system did not make progress after crashing node 0 (initial: %d, target: %d)",
				initialCommits, targetCommits)

		case <-ticker.C:
			state := sim.GetState()
			maxCommits := uint32(0)
			for _, node := range state.Nodes {
				if node.CommitHeight > maxCommits {
					maxCommits = node.CommitHeight
				}
			}
			t.Logf("Current max commits: %d (target: %d)", maxCommits, targetCommits)
			if maxCommits >= targetCommits {
				t.Logf("SUCCESS: System made progress after crash")
				return
			}
		}
	}
}

// TestHealAllRecovery verifies that crashed nodes can recover via HealAll and resume consensus.
func TestHealAllRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cfg := Config{
		Level:          LevelHappyPath,
		FaultTolerance: 1, // f=1 means 4 nodes
		Seed:           42,
	}
	sim, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create simulator: %v", err)
	}

	err = sim.Start()
	if err != nil {
		t.Fatalf("failed to start simulator: %v", err)
	}
	defer sim.Stop()

	// Wait for initial progress
	time.Sleep(2 * time.Second)

	state := sim.GetState()
	initialCommits := uint32(0)
	for _, node := range state.Nodes {
		if node.CommitHeight > initialCommits {
			initialCommits = node.CommitHeight
		}
	}
	t.Logf("Initial commits: %d", initialCommits)

	// Crash node 2
	t.Logf("Crashing node 2...")
	err = sim.CrashNode(2)
	if err != nil {
		t.Fatalf("failed to crash node 2: %v", err)
	}

	// Wait a bit for the system to operate with crashed node
	time.Sleep(3 * time.Second)

	state = sim.GetState()
	commitsAfterCrash := uint32(0)
	for _, node := range state.Nodes {
		if node.CommitHeight > commitsAfterCrash {
			commitsAfterCrash = node.CommitHeight
		}
	}
	t.Logf("Commits after crash (before heal): %d", commitsAfterCrash)

	// Verify node 2 is crashed
	if state.Nodes[2].Status != NodeStatusCrashed {
		t.Fatalf("expected node 2 to be crashed, got %s", state.Nodes[2].Status)
	}

	// Heal all nodes
	t.Logf("Healing all nodes...")
	sim.HealAll()

	// Wait for healed node to rejoin and make progress
	timeout := time.After(15 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	targetCommits := commitsAfterCrash + 3 // Want 3 more blocks after heal

	for {
		select {
		case <-timeout:
			state := sim.GetState()
			t.Logf("Final state after heal:")
			for _, node := range state.Nodes {
				t.Logf("  Node %d: status=%s, view=%d, commits=%d",
					node.ID, node.Status, node.View, node.CommitHeight)
			}
			t.Fatalf("timeout: system did not make progress after healing (target: %d)", targetCommits)

		case <-ticker.C:
			state := sim.GetState()

			// Check all nodes are active
			for _, node := range state.Nodes {
				if node.Status != NodeStatusActive {
					t.Logf("Node %d still not active: %s", node.ID, node.Status)
				}
			}

			maxCommits := uint32(0)
			for _, node := range state.Nodes {
				if node.CommitHeight > maxCommits {
					maxCommits = node.CommitHeight
				}
			}
			t.Logf("Current max commits: %d (target: %d)", maxCommits, targetCommits)
			if maxCommits >= targetCommits {
				t.Logf("SUCCESS: System made progress after HealAll")
				return
			}
		}
	}
}
