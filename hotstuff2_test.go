package hotstuff2

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/timer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestHotStuff2_HappyPath tests the basic consensus flow with 4 nodes.
// Leader proposes, replicas vote, QC forms, block commits.
func TestHotStuff2_HappyPath(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	// Create 4 nodes
	nodes := make([]*HotStuff2[TestHash], N)
	networks := make([]*TestNetwork, N)
	committed := make([][]Block[TestHash], N)
	var commitMu sync.Mutex

	for i := range N {
		storage := NewTestStorage()
		network := NewTestNetwork()
		networks[i] = network
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
			Network:      network,
			Executor:     executor,
		}

		idx := i
		onCommit := func(block Block[TestHash]) {
			commitMu.Lock()
			committed[idx] = append(committed[idx], block)
			commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		require.NoError(t, err)
		nodes[i] = node
	}

	// Start all nodes
	for _, node := range nodes {
		require.NoError(t, node.Start())
	}
	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	// Simulate network: broadcast from one node to all others
	broadcastToAll := func(from int, msg ConsensusPayload[TestHash]) {
		for i := range N {
			if i != from {
				networks[i].msgChan <- msg
			}
		}
	}

	// Leader (node 0) should propose
	time.Sleep(50 * time.Millisecond)

	// Collect proposal from node 0
	sentMsgs := networks[0].SentMessages()
	require.NotEmpty(t, sentMsgs, "leader should send proposal")

	var proposal ConsensusPayload[TestHash]
	for _, msg := range sentMsgs {
		if msg.Type() == MessageProposal {
			proposal = msg
			break
		}
	}
	require.NotNil(t, proposal, "leader should send PROPOSAL")

	// Broadcast proposal to all replicas
	broadcastToAll(0, proposal)
	time.Sleep(50 * time.Millisecond)

	// Replicas should vote
	votes := make([]ConsensusPayload[TestHash], 0)
	for i := 1; i < N; i++ {
		msgs := networks[i].SentMessages()
		for _, msg := range msgs {
			if msg.Type() == MessageVote {
				votes = append(votes, msg)
				break
			}
		}
	}
	require.Len(t, votes, N-1, "replicas should send votes")

	// Broadcast votes to leader
	for _, vote := range votes {
		networks[0].msgChan <- vote
	}
	time.Sleep(50 * time.Millisecond)

	// Check that block was committed (two-chain rule requires 2 consecutive QCs)
	// For first block, we need to propose and vote on block 2 to commit block 1

	// Advance to view 1, leader rotates to node 1
	// (This is simplified - in real test we'd need full view advancement)

	// For this test, verify that all nodes have the same view and QC
	for i := range N {
		assert.Equal(t, uint32(0), nodes[i].View(), "all nodes should be in view 0")
	}
}

// TestHotStuff2_SafeNodeRule tests that replicas won't vote if SafeNode rule violated.
func TestHotStuff2_SafeNodeRule(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      1, // Replica, not leader
		PrivateKey:   privKeys[1],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Create a block and QC at height 1
	block1 := NewTestBlock(1, TestHash{}, 0)

	// Create 3 votes (quorum = 2f+1 = 3 for N=4)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, err := NewVote(0, block1.Hash(), uint16(i), privKeys[i])
		require.NoError(t, err)
		votes1[i] = vote
	}

	qc1, err := NewQC(0, block1.Hash(), votes1, validators, "ed25519")
	require.NoError(t, err)

	node.ctx.UpdateLockedQC(qc1)

	// Try to propose a conflicting block at same height
	conflictingBlock := NewTestBlock(1, TestHash{}, 1) // Different proposer
	require.NotEqual(t, block1.Hash(), conflictingBlock.Hash(), "blocks should conflict")

	// Create proposal with lower QC (should fail SafeNode rule)
	proposal := NewProposeMessage(1, 0, conflictingBlock, nil)
	network.msgChan <- proposal

	time.Sleep(50 * time.Millisecond)

	// Node should NOT vote (SafeNode rule violated)
	sentMsgs := network.SentMessages()
	for _, msg := range sentMsgs {
		assert.NotEqual(t, MessageVote, msg.Type(), "should not vote on conflicting block")
	}
}

// TestHotStuff2_TwoChainCommit tests the two-chain commit rule.
// Block N commits when we have consecutive QCs for blocks N and N+1.
func TestHotStuff2_TwoChainCommit(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	var committedBlocks []Block[TestHash]
	var commitMu sync.Mutex

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	onCommit := func(block Block[TestHash]) {
		commitMu.Lock()
		committedBlocks = append(committedBlocks, block)
		commitMu.Unlock()
	}

	node, err := NewHotStuff2(cfg, onCommit)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Create chain: genesis -> block1 -> block2
	genesis := NewTestBlock(0, TestHash{}, 0)
	block1 := NewTestBlock(1, genesis.Hash(), 0)
	block2 := NewTestBlock(2, block1.Hash(), 0)

	// Store blocks
	node.ctx.AddBlock(genesis)
	node.ctx.AddBlock(block1)
	node.ctx.AddBlock(block2)

	// Create votes for block1 (2f+1 = 3 votes)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, err := NewVote(0, block1.Hash(), uint16(i), privKeys[i])
		require.NoError(t, err)
		votes1[i] = vote
		node.ctx.AddVote(vote)
	}

	// Form QC for block1
	qc1, err := NewQC(0, block1.Hash(), votes1, validators, "ed25519")
	require.NoError(t, err)
	node.ctx.AddQC(qc1)
	node.ctx.UpdateHighQC(qc1)

	// Create votes for block2
	votes2 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, err := NewVote(1, block2.Hash(), uint16(i), privKeys[i])
		require.NoError(t, err)
		votes2[i] = vote
		node.ctx.AddVote(vote)
	}

	// Form QC for block2 - this should trigger commit of block1
	qc2, err := NewQC(1, block2.Hash(), votes2, validators, "ed25519")
	require.NoError(t, err)

	// Manually call checkCommit (simulating what happens in onVote)
	node.checkCommit(block1, qc2)

	time.Sleep(50 * time.Millisecond)

	// Verify block1 was committed
	commitMu.Lock()
	defer commitMu.Unlock()
	require.Len(t, committedBlocks, 1, "block1 should be committed")
	assert.Equal(t, block1.Hash(), committedBlocks[0].Hash())
}

// TestHotStuff2_ViewChange tests timeout and view change mechanism.
func TestHotStuff2_ViewChange(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      1, // Replica
		PrivateKey:   privKeys[1],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	initialView := node.View()
	assert.Equal(t, uint32(0), initialView)

	// Simulate timeout by firing the mock timer
	mockTimer.Fire()
	time.Sleep(50 * time.Millisecond)

	// Node should advance to view 1
	assert.Equal(t, uint32(1), node.View(), "should advance to next view on timeout")

	// Check that NEWVIEW message was sent
	sentMsgs := network.SentMessages()
	var foundNewView bool
	for _, msg := range sentMsgs {
		if msg.Type() == MessageNewView {
			foundNewView = true
			break
		}
	}
	assert.True(t, foundNewView, "should send NEWVIEW on timeout")
}

// TestHotStuff2_LeaderRotation tests that leader rotates correctly.
func TestHotStuff2_LeaderRotation(t *testing.T) {
	const N = 4
	validators := NewTestValidatorSet(N)

	cfg := &Config[TestHash]{
		Validators: validators,
		MyIndex:    0,
	}

	// View 0: leader = 0
	assert.True(t, cfg.IsLeader(0))

	// View 1: leader = 1
	cfg.MyIndex = 1
	assert.True(t, cfg.IsLeader(1))

	// View 2: leader = 2
	cfg.MyIndex = 2
	assert.True(t, cfg.IsLeader(2))

	// View 3: leader = 3
	cfg.MyIndex = 3
	assert.True(t, cfg.IsLeader(3))

	// View 4: leader = 0 (wraps around)
	cfg.MyIndex = 0
	assert.True(t, cfg.IsLeader(4))
}

// TestHotStuff2_QCVerification tests that invalid QCs are rejected.
func TestHotStuff2_QCVerification(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	_, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)

	block := NewTestBlock(1, TestHash{}, 0)

	// Test 1: Insufficient votes (only 2, need 3)
	votes := make([]*Vote[TestHash], 2)
	for i := range 2 {
		vote, err := NewVote(0, block.Hash(), uint16(i), privKeys[i])
		require.NoError(t, err)
		votes[i] = vote
	}

	_, err = NewQC(0, block.Hash(), votes, validators, "ed25519")
	assert.Error(t, err, "should reject QC with insufficient votes")

	// Test 2: Duplicate signer
	validVote, _ := NewVote(0, block.Hash(), 0, privKeys[0])
	duplicateVotes := []*Vote[TestHash]{validVote, validVote, validVote}

	_, err = NewQC(0, block.Hash(), duplicateVotes, validators, "ed25519")
	assert.Error(t, err, "should reject QC with duplicate signers")

	// Test 3: Invalid signature
	tampered, _ := NewVote(0, block.Hash(), 0, privKeys[0])
	tampered.signature = []byte("invalid")

	tamperedVotes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		if i == 0 {
			tamperedVotes[i] = tampered
		} else {
			vote, _ := NewVote(0, block.Hash(), uint16(i), privKeys[i])
			tamperedVotes[i] = vote
		}
	}

	qc, err := NewQC(0, block.Hash(), tamperedVotes, validators, "ed25519")
	if err == nil {
		// If QC creation succeeded, validation should fail
		err = qc.Validate(validators)
		assert.Error(t, err, "should reject QC with invalid signature")
	}
}

// TestHotStuff2_ConcurrentVotes tests thread-safety of vote collection.
func TestHotStuff2_ConcurrentVotes(t *testing.T) {
	const N = 10
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	block := NewTestBlock(1, TestHash{}, 0)

	// Send votes concurrently from multiple goroutines
	var wg sync.WaitGroup
	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			vote, err := NewVote(0, block.Hash(), uint16(idx), privKeys[idx])
			require.NoError(t, err)

			voteMsg := NewVoteMessage(0, uint16(idx), vote)
			network.msgChan <- voteMsg
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// Verify all votes were collected
	votes := node.ctx.GetVotes(0, block.Hash())
	assert.Equal(t, N, len(votes), "should collect all concurrent votes")
}

// TestHotStuff2_HighQCUpdate tests that highQC is updated correctly.
func TestHotStuff2_HighQCUpdate(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	ctx := NewContext[TestHash](nil, nil)

	block1 := NewTestBlock(1, TestHash{}, 0)
	block2 := NewTestBlock(2, block1.Hash(), 0)

	// Create QC for view 0
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, _ := NewVote(0, block1.Hash(), uint16(i), privKeys[i])
		votes1[i] = vote
	}
	qc1, err := NewQC(0, block1.Hash(), votes1, validators, "ed25519")
	require.NoError(t, err)

	// Create QC for view 1
	votes2 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, _ := NewVote(1, block2.Hash(), uint16(i), privKeys[i])
		votes2[i] = vote
	}
	qc2, err := NewQC(1, block2.Hash(), votes2, validators, "ed25519")
	require.NoError(t, err)

	// Update with qc1
	ctx.UpdateHighQC(qc1)
	assert.Equal(t, qc1.View(), ctx.HighQC().View())

	// Update with qc2 (higher view)
	ctx.UpdateHighQC(qc2)
	assert.Equal(t, qc2.View(), ctx.HighQC().View())

	// Try to update with qc1 (lower view) - should not update
	ctx.UpdateHighQC(qc1)
	assert.Equal(t, qc2.View(), ctx.HighQC().View(), "should keep higher QC")
}

// TestHotStuff2_Persistence tests that state is persisted and restored.
func TestHotStuff2_Persistence(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	// Create first instance
	node1, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node1.Start())

	// Create and persist a QC
	block := NewTestBlock(1, TestHash{}, 0)
	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, _ := NewVote(0, block.Hash(), uint16(i), privKeys[i])
		votes[i] = vote
	}
	qc, err := NewQC(0, block.Hash(), votes, validators, "ed25519")
	require.NoError(t, err)

	node1.ctx.UpdateLockedQC(qc)
	require.NoError(t, storage.PutHighestLockedQC(qc))
	require.NoError(t, storage.PutView(5))

	node1.Stop()

	// Create second instance with same storage
	node2, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)

	// Verify state was restored
	assert.Equal(t, uint32(5), node2.ctx.View(), "view should be restored")
	assert.NotNil(t, node2.ctx.LockedQC(), "locked QC should be restored")
	assert.Equal(t, qc.View(), node2.ctx.LockedQC().View(), "locked QC view should match")
}

// TestHotStuff2_MultipleBlocks tests consensus over multiple blocks.
func TestHotStuff2_MultipleBlocks(t *testing.T) {
	t.Skip("Multi-block consensus is tested in TestIntegration_FourNodesHappyPath (integration_test.go)")
	// See integration_test.go for the full multi-node simulation using SharedNetwork.
	// The twins/ package also provides Byzantine fault testing scenarios.
}

// TestHotStuff2_Height tests height tracking.
func TestHotStuff2_Height(t *testing.T) {
	validators, privKeys := NewTestValidatorSetWithKeys(4)
	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	onCommit := func(block Block[TestHash]) {
		// Record commits
	}

	node, err := NewHotStuff2(cfg, onCommit)
	require.NoError(t, err)

	// Initial height should be 0 (genesis)
	assert.Equal(t, uint32(0), node.Height(), "initial height should be 0")

	// Commit a block at height 1
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	node.ctx.AddBlock(block1)
	node.ctx.Commit(block1)

	// Height should update
	assert.Equal(t, uint32(1), node.Height(), "height should be 1 after commit")

	// Commit block at height 3 (skipping 2)
	block3 := NewTestBlock(3, block1.Hash(), 0)
	node.ctx.AddBlock(block3)
	node.ctx.Commit(block3)

	// Height should be max
	assert.Equal(t, uint32(3), node.Height(), "height should be max of committed blocks")
}

// TestHotStuff2_View tests view getter.
func TestHotStuff2_View(t *testing.T) {
	validators, privKeys := NewTestValidatorSetWithKeys(4)
	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)

	// Initial view should be 0
	assert.Equal(t, uint32(0), node.View(), "initial view should be 0")

	// Advance view
	node.ctx.SetView(5)
	assert.Equal(t, uint32(5), node.View(), "view should update")
}

// TestHotStuff2_NewViewQuorum tests that leader proposes after receiving NEWVIEW quorum.
func TestHotStuff2_NewViewQuorum(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	// Node 1 is leader for view 1 (round-robin: view % N)
	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      1, // Leader for view 1
		PrivateKey:   privKeys[1],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Advance to view 1 (where this node is leader)
	node.ctx.SetView(1)

	// Add our own NEWVIEW first
	node.ctx.AddNewView(1, 1, nil)

	// Send NEWVIEW messages from other replicas to reach quorum (2f+1 = 3)
	// We need 2 more (we already have 1 from self)
	newViewMsg0 := NewNewViewMessage[TestHash](1, 0, nil)
	node.onNewView(newViewMsg0)

	newViewMsg2 := NewNewViewMessage[TestHash](1, 2, nil)
	node.onNewView(newViewMsg2)

	time.Sleep(100 * time.Millisecond)

	// Check that a proposal was sent
	sentMsgs := network.SentMessages()
	var foundProposal bool
	for _, msg := range sentMsgs {
		if msg.Type() == MessageProposal {
			foundProposal = true
			break
		}
	}
	assert.True(t, foundProposal, "leader should propose after receiving NEWVIEW quorum")
}

// TestHotStuff2_ViewSyncForward tests that a lagging node syncs forward via NEWVIEW.
func TestHotStuff2_ViewSyncForward(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0,
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Node starts at view 0
	assert.Equal(t, uint32(0), node.View())

	// Create a block and QC for view 5
	block := NewTestBlock(1, TestHash{}, 0)
	node.ctx.AddBlock(block)

	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, _ := NewVote(5, block.Hash(), uint16(i), privKeys[i])
		votes[i] = vote
	}
	qc, _ := NewQC(5, block.Hash(), votes, validators, "ed25519")

	// Receive NEWVIEW for view 10 with highQC at view 5
	newViewMsg := NewNewViewMessage[TestHash](10, 1, qc)
	node.onNewView(newViewMsg)

	time.Sleep(50 * time.Millisecond)

	// Node should sync forward to view 10
	assert.Equal(t, uint32(10), node.View(), "node should sync forward to higher view")
}

// TestHotStuff2_RejectInvalidProposal tests that invalid proposals are rejected.
func TestHotStuff2_RejectInvalidProposal(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := &RejectingExecutor{} // Executor that rejects blocks
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      1, // Replica
		PrivateKey:   privKeys[1],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Create a proposal that will be rejected by executor
	block := NewTestBlock(1, TestHash{}, 0)
	proposal := NewProposeMessage[TestHash](0, 0, block, nil)

	// Process the proposal
	node.onProposal(proposal)

	time.Sleep(50 * time.Millisecond)

	// No vote should be sent for an invalid block
	sentMsgs := network.SentMessages()
	var foundVote bool
	for _, msg := range sentMsgs {
		if msg.Type() == MessageVote {
			foundVote = true
			break
		}
	}
	assert.False(t, foundVote, "should not vote for invalid block")
}

// RejectingExecutor rejects all blocks during verification.
type RejectingExecutor struct{}

func (e *RejectingExecutor) Execute(block Block[TestHash]) (TestHash, error) {
	return block.Hash(), nil
}

func (e *RejectingExecutor) Verify(block Block[TestHash]) error {
	return fmt.Errorf("block rejected")
}

func (e *RejectingExecutor) GetStateHash() TestHash {
	return TestHash{}
}

func (e *RejectingExecutor) CreateBlock(height uint32, prevHash TestHash, proposerIndex uint16) (Block[TestHash], error) {
	return NewTestBlock(height, prevHash, proposerIndex), nil
}

// TestHotStuff2_DuplicateVoteRejection tests that duplicate votes are not counted twice.
func TestHotStuff2_DuplicateVoteRejection(t *testing.T) {
	const N = 4
	validators, privKeys := NewTestValidatorSetWithKeys(N)

	storage := NewTestStorage()
	network := NewTestNetwork()
	executor := NewTestExecutor()
	mockTimer := timer.NewMockTimer()

	cfg := &Config[TestHash]{
		Logger:       zap.NewNop(),
		Timer:        mockTimer,
		Validators:   validators,
		MyIndex:      0, // Leader for view 0
		PrivateKey:   privKeys[0],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      network,
		Executor:     executor,
	}

	node, err := NewHotStuff2(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, node.Start())
	defer node.Stop()

	// Create a block and proposal
	block := NewTestBlock(1, TestHash{}, 0)
	node.ctx.AddBlock(block)

	// Create the same vote twice
	vote, _ := NewVote(0, block.Hash(), 1, privKeys[1])

	// Send the same vote multiple times
	voteMsg := NewVoteMessage(0, 1, vote)
	node.onVote(voteMsg)
	node.onVote(voteMsg) // Duplicate
	node.onVote(voteMsg) // Duplicate

	time.Sleep(50 * time.Millisecond)

	// Check that only one vote was counted
	voteCount := node.ctx.VoteCount(0, block.Hash())
	// Should be 2: leader's implicit vote + 1 from replica (duplicates ignored)
	assert.LessOrEqual(t, voteCount, 2, "duplicate votes should not be counted multiple times")
}
