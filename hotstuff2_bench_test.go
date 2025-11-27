package hotstuff2

import (
	"sync"
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/edgedlt/hotstuff2/timer"
	"go.uber.org/zap"
)

// BenchmarkEndToEndConsensus_4Nodes benchmarks full consensus with 4 nodes
// Target: 1000 blocks/sec = 1ms per block
func BenchmarkEndToEndConsensus_4Nodes(b *testing.B) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	// Shared network for all nodes
	sharedNetwork := NewSharedNetwork[TestHash](N)
	defer func() {
		sharedNetwork.Close()
	}()

	// Create nodes
	nodes := make([]*HotStuff2[TestHash], N)
	committed := make([][]Block[TestHash], N)
	var commitMu sync.Mutex

	for i := range N {
		storage := NewTestStorage()
		executor := NewTestExecutor()
		mockTimer := timer.NewMockTimer()

		cfg := &Config[TestHash]{
			Logger:       zap.NewNop(),
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(i),
			PrivateKey:   privKeys[i],
			CryptoScheme: "ed25519",
			Storage:      storage,
			Network:      sharedNetwork.NodeNetwork(i),
			Executor:     executor,
		}

		idx := i
		onCommit := func(block Block[TestHash]) {
			commitMu.Lock()
			committed[idx] = append(committed[idx], block)
			commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		if err != nil {
			b.Fatal(err)
		}
		nodes[i] = node
	}

	// Start all nodes
	for _, node := range nodes {
		if err := node.Start(); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	// Wait for N blocks to be committed
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	targetBlocks := b.N
	for {
		select {
		case <-timeout:
			for _, node := range nodes {
				node.Stop()
			}
			b.Fatalf("timeout: only committed %d/%d blocks", len(committed[0]), targetBlocks)

		case <-ticker.C:
			commitMu.Lock()
			blockCount := len(committed[0])
			commitMu.Unlock()

			if blockCount >= targetBlocks {
				b.StopTimer()
				for _, node := range nodes {
					node.Stop()
				}
				return
			}
		}
	}
}

// BenchmarkConsensusRound_4Nodes measures single consensus round latency
func BenchmarkConsensusRound_4Nodes(b *testing.B) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		sharedNetwork := NewSharedNetwork[TestHash](N)
		nodes := make([]*HotStuff2[TestHash], N)
		committed := make([]int, N)
		var commitMu sync.Mutex

		for j := range N {
			storage := NewTestStorage()
			executor := NewTestExecutor()
			mockTimer := timer.NewMockTimer()

			cfg := &Config[TestHash]{
				Logger:       zap.NewNop(),
				Timer:        mockTimer,
				Validators:   validators,
				MyIndex:      uint16(j),
				PrivateKey:   privKeys[j],
				CryptoScheme: "ed25519",
				Storage:      storage,
				Network:      sharedNetwork.NodeNetwork(j),
				Executor:     executor,
			}

			idx := j
			onCommit := func(block Block[TestHash]) {
				commitMu.Lock()
				committed[idx]++
				commitMu.Unlock()
			}

			node, err := NewHotStuff2(cfg, onCommit)
			if err != nil {
				b.Fatal(err)
			}
			nodes[j] = node
		}

		for _, node := range nodes {
			if err := node.Start(); err != nil {
				b.Fatal(err)
			}
		}

		b.StartTimer()

		// Wait for first block to commit on all nodes
		timeout := time.After(5 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				b.Fatal("timeout waiting for consensus round")
			case <-ticker.C:
				commitMu.Lock()
				allCommitted := true
				for j := 0; j < N; j++ {
					if committed[j] < 1 {
						allCommitted = false
						break
					}
				}
				commitMu.Unlock()

				if allCommitted {
					b.StopTimer()
					for _, node := range nodes {
						node.Stop()
					}
					sharedNetwork.Close()
					goto nextIteration
				}
			}
		}
	nextIteration:
	}
}

// BenchmarkQCCreationAndValidation_4Nodes measures QC formation and verification
// Target: QC creation <1ms, QC validation <5ms
func BenchmarkQCCreationAndValidation_4Nodes(b *testing.B) {
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3) // 2f+1 for n=4
	for i := range 3 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	b.Run("QCFormation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
		}
	})

	qc, _ := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)

	b.Run("QCValidation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = qc.Validate(validators)
		}
	})
}

// BenchmarkQCCreationAndValidation_7Nodes measures QC ops with 7 validators
func BenchmarkQCCreationAndValidation_7Nodes(b *testing.B) {
	validators := NewTestValidatorSet(7)
	keys := make([]*crypto.Ed25519PrivateKey, 5) // 2f+1 for n=7
	for i := range 5 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	votes := make([]*Vote[TestHash], 5)
	for i := range 5 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	b.Run("QCFormation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
		}
	})

	qc, _ := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)

	b.Run("QCValidation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = qc.Validate(validators)
		}
	})
}

// BenchmarkVoteAggregation measures how quickly we can aggregate votes
func BenchmarkVoteAggregation(b *testing.B) {
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 4)
	for i := range 4 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		votes := make([]*Vote[TestHash], 3) // 2f+1 for n=4
		for j := range 3 {
			votes[j], _ = NewVote(view, nodeHash, uint16(j), keys[j])
		}
		b.StartTimer()

		_, _ = NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	}
}

// BenchmarkThroughput_100Blocks measures sustained throughput over 100 blocks
func BenchmarkThroughput_100Blocks(b *testing.B) {
	const N = 4
	const TARGET_BLOCKS = 100

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		validators, privKeys := NewTestValidatorSetWithKeys(N)
		sharedNetwork := NewSharedNetwork[TestHash](N)

		nodes := make([]*HotStuff2[TestHash], N)
		committed := make([][]Block[TestHash], N)
		var commitMu sync.Mutex

		for j := range N {
			storage := NewTestStorage()
			executor := NewTestExecutor()
			mockTimer := timer.NewMockTimer()

			cfg := &Config[TestHash]{
				Logger:       zap.NewNop(),
				Timer:        mockTimer,
				Validators:   validators,
				MyIndex:      uint16(j),
				PrivateKey:   privKeys[j],
				CryptoScheme: "ed25519",
				Storage:      storage,
				Network:      sharedNetwork.NodeNetwork(j),
				Executor:     executor,
			}

			idx := j
			onCommit := func(block Block[TestHash]) {
				commitMu.Lock()
				committed[idx] = append(committed[idx], block)
				commitMu.Unlock()
			}

			node, err := NewHotStuff2(cfg, onCommit)
			if err != nil {
				b.Fatal(err)
			}
			nodes[j] = node
		}

		for _, node := range nodes {
			if err := node.Start(); err != nil {
				b.Fatal(err)
			}
		}

		b.StartTimer()

		// Wait for TARGET_BLOCKS to be committed
		timeout := time.After(30 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)

		for {
			select {
			case <-timeout:
				b.Fatalf("timeout: only committed %d/%d blocks", len(committed[0]), TARGET_BLOCKS)

			case <-ticker.C:
				commitMu.Lock()
				blockCount := len(committed[0])
				commitMu.Unlock()

				if blockCount >= TARGET_BLOCKS {
					b.StopTimer()
					ticker.Stop()
					for _, node := range nodes {
						node.Stop()
					}
					sharedNetwork.Close()
					goto nextIteration
				}
			}
		}
	nextIteration:
	}
}
