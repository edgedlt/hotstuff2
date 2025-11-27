package hotstuff2

import (
	"testing"
)

// TestContextCreation tests Context initialization.
func TestContextCreation(t *testing.T) {
	genesisBlock := NewTestBlock(0, TestHash{}, 0)

	validators, keys := NewTestValidatorSetWithKeys(4)
	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes[i], _ = NewVote(0, genesisBlock.Hash(), uint16(i), keys[i])
	}
	genesisQC, _ := NewQC(0, genesisBlock.Hash(), votes, validators, CryptoSchemeEd25519)

	ctx := NewContext(genesisBlock, genesisQC)

	if ctx.View() != 0 {
		t.Errorf("Initial view should be 0, got %d", ctx.View())
	}

	if ctx.LockedQC() == nil {
		t.Error("LockedQC should be initialized with genesisQC")
	}

	if ctx.HighQC() == nil {
		t.Error("HighQC should be initialized with genesisQC")
	}

	// Genesis should be committed
	if !ctx.IsCommitted(0) {
		t.Error("Genesis block should be committed")
	}
}

// TestContextViewManagement tests view number management.
func TestContextViewManagement(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	if ctx.View() != 0 {
		t.Errorf("Initial view should be 0, got %d", ctx.View())
	}

	ctx.SetView(5)
	if ctx.View() != 5 {
		t.Errorf("View should be 5 after SetView, got %d", ctx.View())
	}

	ctx.SetView(6)
	if ctx.View() != 6 {
		t.Errorf("View should be 6 after SetView(6), got %d", ctx.View())
	}
}

// TestContextQCManagement tests QC storage and retrieval.
func TestContextQCManagement(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	// Create QC at view 1
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	// Add QC
	ctx.AddQC(qc1)

	// Retrieve QC
	retrieved, ok := ctx.GetQC(block1.Hash())
	if !ok {
		t.Fatal("QC should be retrievable")
	}

	if retrieved.View() != 1 {
		t.Errorf("Retrieved QC view should be 1, got %d", retrieved.View())
	}

	// GetQC for non-existent block
	_, ok = ctx.GetQC(NewTestHash("nonexistent"))
	if ok {
		t.Error("GetQC should return false for non-existent block")
	}
}

// TestContextLockedQCUpdate tests locked QC updates following TLA+ spec.
//
// TLA+ UpdateLockCond: lockedQC' = IF qc.view > lockedQC.view THEN qc ELSE lockedQC
func TestContextLockedQCUpdate(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	// Initial locked QC at view 1
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	ctx := NewContext[TestHash](nil, qc1)

	// Try to update with lower view QC (should fail)
	block0 := NewTestBlock(0, NewTestHash("genesis"), 0)
	votes0 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes0[i], _ = NewVote(0, block0.Hash(), uint16(i), keys[i])
	}
	qc0, _ := NewQC(0, block0.Hash(), votes0, validators, CryptoSchemeEd25519)

	if ctx.UpdateLockedQC(qc0) {
		t.Error("UpdateLockedQC should return false for lower view QC")
	}

	if ctx.LockedQC().View() != 1 {
		t.Error("LockedQC should not change for lower view")
	}

	// Update with higher view QC (should succeed)
	block2 := NewTestBlock(2, block1.Hash(), 0)
	votes2 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes2[i], _ = NewVote(2, block2.Hash(), uint16(i), keys[i])
	}
	qc2, _ := NewQC(2, block2.Hash(), votes2, validators, CryptoSchemeEd25519)

	if !ctx.UpdateLockedQC(qc2) {
		t.Error("UpdateLockedQC should return true for higher view QC")
	}

	if ctx.LockedQC().View() != 2 {
		t.Errorf("LockedQC view should be 2, got %d", ctx.LockedQC().View())
	}
}

// TestContextHighQCUpdate tests highest QC tracking.
func TestContextHighQCUpdate(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	// Add QC at view 1
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	if !ctx.UpdateHighQC(qc1) {
		t.Error("UpdateHighQC should return true for first QC")
	}

	// Try lower view QC
	block0 := NewTestBlock(0, NewTestHash("genesis"), 0)
	votes0 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes0[i], _ = NewVote(0, block0.Hash(), uint16(i), keys[i])
	}
	qc0, _ := NewQC(0, block0.Hash(), votes0, validators, CryptoSchemeEd25519)

	if ctx.UpdateHighQC(qc0) {
		t.Error("UpdateHighQC should return false for lower view QC")
	}

	// Higher view QC
	block3 := NewTestBlock(3, block1.Hash(), 0)
	votes3 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes3[i], _ = NewVote(3, block3.Hash(), uint16(i), keys[i])
	}
	qc3, _ := NewQC(3, block3.Hash(), votes3, validators, CryptoSchemeEd25519)

	if !ctx.UpdateHighQC(qc3) {
		t.Error("UpdateHighQC should return true for higher view QC")
	}

	if ctx.HighQC().View() != 3 {
		t.Errorf("HighQC view should be 3, got %d", ctx.HighQC().View())
	}
}

// TestContextBlockManagement tests block storage and retrieval.
func TestContextBlockManagement(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	block2 := NewTestBlock(2, block1.Hash(), 0)

	// Add blocks
	ctx.AddBlock(block1)
	ctx.AddBlock(block2)

	// Retrieve blocks
	retrieved1, ok1 := ctx.GetBlock(block1.Hash())
	if !ok1 {
		t.Fatal("Block1 should be retrievable")
	}

	if retrieved1.Height() != 1 {
		t.Errorf("Block1 height should be 1, got %d", retrieved1.Height())
	}

	_, ok2 := ctx.GetBlock(block2.Hash())
	if !ok2 {
		t.Fatal("Block2 should be retrievable")
	}

	// Non-existent block
	_, ok := ctx.GetBlock(NewTestHash("nonexistent"))
	if ok {
		t.Error("GetBlock should return false for non-existent block")
	}
}

// TestContextVoteTracking tests vote collection and counting.
func TestContextVoteTracking(t *testing.T) {
	_, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(1)
	block := NewTestBlock(1, NewTestHash("genesis"), 0)

	// Add votes from 3 validators
	for i := range 3 {
		vote, _ := NewVote(view, block.Hash(), uint16(i), keys[i])
		ctx.AddVote(vote)
	}

	// Count votes
	count := ctx.VoteCount(view, block.Hash())
	if count != 3 {
		t.Errorf("Vote count should be 3, got %d", count)
	}

	// Get votes
	votes := ctx.GetVotes(view, block.Hash())
	if len(votes) != 3 {
		t.Errorf("Should have 3 votes, got %d", len(votes))
	}

	// Check if has voted
	if !ctx.HasVoted(view, 0) {
		t.Error("Validator 0 should have voted")
	}

	if ctx.HasVoted(view, 3) {
		t.Error("Validator 3 should not have voted")
	}

	// Different view
	if ctx.VoteCount(2, block.Hash()) != 0 {
		t.Error("Should have 0 votes in view 2")
	}
}

// TestContextCommitTracking tests block commit tracking.
func TestContextCommitTracking(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	block2 := NewTestBlock(2, block1.Hash(), 0)

	// Initially not committed
	if ctx.IsCommitted(1) {
		t.Error("Block at height 1 should not be committed initially")
	}

	// Commit block
	ctx.Commit(block1)

	if !ctx.IsCommitted(1) {
		t.Error("Block at height 1 should be committed")
	}

	// Get committed block
	committed, ok := ctx.GetCommitted(1)
	if !ok {
		t.Fatal("Should retrieve committed block at height 1")
	}

	if !committed.Hash().Equals(block1.Hash()) {
		t.Error("Retrieved committed block hash mismatch")
	}

	// Commit another block
	ctx.Commit(block2)

	if !ctx.IsCommitted(2) {
		t.Error("Block at height 2 should be committed")
	}

	// Get committed blocks
	allCommitted := ctx.CommittedBlocks()
	if len(allCommitted) != 2 {
		t.Errorf("Should have 2 committed blocks, got %d", len(allCommitted))
	}
}

// TestContextSafeToVote tests the SafeNode rule from TLA+ spec.
//
// TLA+ SafeNodeRule:
//
//	\/ lockedQC[r] = Nil
//	\/ (qc /= Nil /\ blockView[qc] > blockView[lockedQC[r]])
//	\/ IsAncestor(lockedQC[r], block)
func TestContextSafeToVote(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	t.Run("NoLock", func(t *testing.T) {
		// No locked QC - should always be safe
		ctx := NewContext[TestHash](nil, nil)
		block := NewTestBlock(1, NewTestHash("genesis"), 0)

		if !ctx.SafeToVote(block, nil) {
			t.Error("Should be safe to vote when no lock")
		}
	})

	t.Run("HigherQC", func(t *testing.T) {
		// Justify QC view > locked QC view - should be safe
		block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
		votes1 := make([]*Vote[TestHash], 3)
		for i := range 3 {
			votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
		}
		lockedQC, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

		ctx := NewContext[TestHash](nil, lockedQC)

		block2 := NewTestBlock(2, block1.Hash(), 0)
		votes2 := make([]*Vote[TestHash], 3)
		for i := range 3 {
			votes2[i], _ = NewVote(2, block2.Hash(), uint16(i), keys[i])
		}
		justifyQC, _ := NewQC(2, block2.Hash(), votes2, validators, CryptoSchemeEd25519)

		block3 := NewTestBlock(3, block2.Hash(), 0)

		if !ctx.SafeToVote(block3, justifyQC) {
			t.Error("Should be safe to vote when justify QC view > locked QC view")
		}
	})

	t.Run("ExtendsLock", func(t *testing.T) {
		// Block extends locked chain - should be safe
		genesis := NewTestBlock(0, TestHash{}, 0)
		block1 := NewTestBlock(1, genesis.Hash(), 0)
		votes1 := make([]*Vote[TestHash], 3)
		for i := range 3 {
			votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
		}
		lockedQC, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

		ctx := NewContext(genesis, lockedQC)
		// IMPORTANT: Must add block1 to establish parent chain
		ctx.AddBlock(block1)

		// Block2 extends block1 which is locked
		block2 := NewTestBlock(2, block1.Hash(), 0)
		// Also add block2 so its parent relationship is known
		ctx.AddBlock(block2)

		if !ctx.SafeToVote(block2, nil) {
			t.Error("Should be safe to vote when block extends locked block")
		}
	})

	t.Run("Conflicting", func(t *testing.T) {
		// Block conflicts with lock and justify QC is not higher - should NOT be safe
		genesis := NewTestBlock(0, TestHash{}, 0)

		// Lock on block1
		block1 := NewTestBlock(1, genesis.Hash(), 0)
		votes1 := make([]*Vote[TestHash], 3)
		for i := range 3 {
			votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
		}
		lockedQC, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

		ctx := NewContext(genesis, lockedQC)
		ctx.AddBlock(block1)

		// Block1b conflicts with block1 (same height, different hash)
		block1b := NewTestBlock(1, genesis.Hash(), 1) // Different proposer

		if ctx.SafeToVote(block1b, nil) {
			t.Error("Should NOT be safe to vote for conflicting block with no higher justify QC")
		}
	})
}

// TestContextAncestryChecking tests the IsAncestor function.
func TestContextAncestryChecking(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	// Build chain: genesis -> b1 -> b2 -> b3
	genesis := NewTestBlock(0, TestHash{}, 0)
	b1 := NewTestBlock(1, genesis.Hash(), 0)
	b2 := NewTestBlock(2, b1.Hash(), 0)
	b3 := NewTestBlock(3, b2.Hash(), 0)

	ctx.AddBlock(genesis)
	ctx.AddBlock(b1)
	ctx.AddBlock(b2)
	ctx.AddBlock(b3)

	// Direct parent
	if !ctx.IsAncestor(b2.Hash(), b3.Hash()) {
		t.Error("b2 should be ancestor of b3 (direct parent)")
	}

	// Grandparent
	if !ctx.IsAncestor(b1.Hash(), b3.Hash()) {
		t.Error("b1 should be ancestor of b3 (grandparent)")
	}

	// Genesis ancestor
	if !ctx.IsAncestor(genesis.Hash(), b3.Hash()) {
		t.Error("genesis should be ancestor of b3")
	}

	// Self
	if !ctx.IsAncestor(b2.Hash(), b2.Hash()) {
		t.Error("Block should be ancestor of itself")
	}

	// Not ancestor
	if ctx.IsAncestor(b3.Hash(), b2.Hash()) {
		t.Error("b3 should NOT be ancestor of b2 (reversed)")
	}

	// Unrelated blocks
	b1alt := NewTestBlock(1, genesis.Hash(), 1) // Fork
	ctx.AddBlock(b1alt)

	if ctx.IsAncestor(b1alt.Hash(), b3.Hash()) {
		t.Error("b1alt should NOT be ancestor of b3 (different chain)")
	}
}

// TestContextConcurrency tests thread-safety of Context.
func TestContextConcurrency(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	done := make(chan struct{})

	// Concurrent view updates
	go func() {
		for range 100 {
			v := ctx.View()
			ctx.SetView(v + 1)
		}
		done <- struct{}{}
	}()

	go func() {
		for range 100 {
			_ = ctx.View()
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	// View should be >= 100 (one goroutine incremented, but concurrency may cause some overlaps)
	if ctx.View() < 100 {
		t.Errorf("Expected view to be >= 100, got %d", ctx.View())
	}
}

// TestContextNewViewTracking tests NEWVIEW message tracking for view changes.
//
// TLA+ spec (ReplicaTimeout, LeaderProcessNewView):
//   - Replicas send NEWVIEW with their highQC on timeout
//   - Leader collects 2f+1 NEWVIEWs before proposing
//   - Leader selects the highest QC from all NEWVIEWs
func TestContextNewViewTracking(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(5)

	// Create some QCs at different views for testing
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	block2 := NewTestBlock(2, block1.Hash(), 0)
	votes2 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes2[i], _ = NewVote(2, block2.Hash(), uint16(i), keys[i])
	}
	qc2, _ := NewQC(2, block2.Hash(), votes2, validators, CryptoSchemeEd25519)

	block3 := NewTestBlock(3, block2.Hash(), 0)
	votes3 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes3[i], _ = NewVote(3, block3.Hash(), uint16(i), keys[i])
	}
	qc3, _ := NewQC(3, block3.Hash(), votes3, validators, CryptoSchemeEd25519)

	// Initially no NEWVIEWs
	if ctx.NewViewCount(view) != 0 {
		t.Error("Should have 0 NEWVIEWs initially")
	}

	if ctx.HasNewViewQuorum(view, 3) {
		t.Error("Should not have quorum initially")
	}

	// Add NEWVIEW from validator 0 with qc1
	count := ctx.AddNewView(view, 0, qc1)
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Add NEWVIEW from validator 1 with qc3 (highest)
	count = ctx.AddNewView(view, 1, qc3)
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	// Add NEWVIEW from validator 2 with qc2
	count = ctx.AddNewView(view, 2, qc2)
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}

	// Should now have quorum (2f+1 = 3 for n=4)
	if !ctx.HasNewViewQuorum(view, 3) {
		t.Error("Should have quorum after 3 NEWVIEWs")
	}

	// Highest QC should be qc3 (view 3)
	highestQC := ctx.HighestQCFromNewViews(view)
	if highestQC == nil {
		t.Fatal("HighestQC should not be nil")
	}
	if highestQC.View() != 3 {
		t.Errorf("Highest QC view should be 3, got %d", highestQC.View())
	}
}

// TestContextNewViewWithNilQC tests NEWVIEW tracking when some validators have nil QC.
func TestContextNewViewWithNilQC(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(2)

	// Create a QC
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	// Add NEWVIEW with nil QC (e.g., fresh node at genesis)
	ctx.AddNewView(view, 0, nil)

	// Add NEWVIEW with qc1
	ctx.AddNewView(view, 1, qc1)

	// Add another NEWVIEW with nil QC
	ctx.AddNewView(view, 2, nil)

	// Should have 3 NEWVIEWs
	if ctx.NewViewCount(view) != 3 {
		t.Errorf("Expected 3 NEWVIEWs, got %d", ctx.NewViewCount(view))
	}

	// Highest QC should be qc1 (the only non-nil one)
	highestQC := ctx.HighestQCFromNewViews(view)
	if highestQC == nil {
		t.Fatal("HighestQC should not be nil when at least one validator has QC")
	}
	if highestQC.View() != 1 {
		t.Errorf("Highest QC view should be 1, got %d", highestQC.View())
	}
}

// TestContextNewViewAllNilQC tests NEWVIEW tracking when all validators have nil QC.
func TestContextNewViewAllNilQC(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(1)

	// All NEWVIEWs with nil QC (e.g., all at genesis)
	ctx.AddNewView(view, 0, nil)
	ctx.AddNewView(view, 1, nil)
	ctx.AddNewView(view, 2, nil)

	// Should have quorum
	if !ctx.HasNewViewQuorum(view, 3) {
		t.Error("Should have quorum with 3 NEWVIEWs even if all nil QC")
	}

	// Highest QC should be nil
	highestQC := ctx.HighestQCFromNewViews(view)
	if highestQC != nil {
		t.Error("HighestQC should be nil when all validators have nil QC")
	}
}

// TestContextNewViewDeduplication tests that duplicate NEWVIEWs from same validator are handled.
func TestContextNewViewDeduplication(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(3)

	// Create QCs
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	votes1 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes1[i], _ = NewVote(1, block1.Hash(), uint16(i), keys[i])
	}
	qc1, _ := NewQC(1, block1.Hash(), votes1, validators, CryptoSchemeEd25519)

	block2 := NewTestBlock(2, block1.Hash(), 0)
	votes2 := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes2[i], _ = NewVote(2, block2.Hash(), uint16(i), keys[i])
	}
	qc2, _ := NewQC(2, block2.Hash(), votes2, validators, CryptoSchemeEd25519)

	// Validator 0 sends NEWVIEW with qc1
	count := ctx.AddNewView(view, 0, qc1)
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Validator 0 sends another NEWVIEW with qc2 (higher) - should update
	count = ctx.AddNewView(view, 0, qc2)
	if count != 1 {
		t.Errorf("Expected count still 1 (same validator), got %d", count)
	}

	// HasSentNewView should return true
	if !ctx.HasSentNewView(view, 0) {
		t.Error("Validator 0 should have sent NEWVIEW")
	}

	// The stored QC should be qc2 (the latest one)
	highestQC := ctx.HighestQCFromNewViews(view)
	if highestQC == nil || highestQC.View() != 2 {
		t.Error("Should store the latest QC from validator")
	}
}

// TestContextNewViewPruning tests that old NEWVIEWs are pruned with votes.
func TestContextNewViewPruning(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	// Add NEWVIEWs for views 1, 5, 10
	ctx.AddNewView(1, 0, nil)
	ctx.AddNewView(5, 0, nil)
	ctx.AddNewView(10, 0, nil)

	// Verify they exist
	if ctx.NewViewCount(1) != 1 {
		t.Error("Should have NEWVIEW for view 1")
	}
	if ctx.NewViewCount(5) != 1 {
		t.Error("Should have NEWVIEW for view 5")
	}
	if ctx.NewViewCount(10) != 1 {
		t.Error("Should have NEWVIEW for view 10")
	}

	// Prune old views (keep last 5 views from current view 10)
	ctx.PruneVotes(10, 5)

	// Views older than 10-5=5 should be pruned
	if ctx.NewViewCount(1) != 0 {
		t.Error("NEWVIEW for view 1 should be pruned")
	}

	// Views 5 and above should remain
	if ctx.NewViewCount(5) != 1 {
		t.Error("NEWVIEW for view 5 should remain")
	}
	if ctx.NewViewCount(10) != 1 {
		t.Error("NEWVIEW for view 10 should remain")
	}
}

// TestContextNewViewConcurrency tests thread-safety of NEWVIEW tracking.
func TestContextNewViewConcurrency(t *testing.T) {
	ctx := NewContext[TestHash](nil, nil)

	view := uint32(5)
	done := make(chan struct{})

	// Concurrent AddNewView from different validators
	for i := range 10 {
		go func(idx uint16) {
			ctx.AddNewView(view, idx, nil)
			done <- struct{}{}
		}(uint16(i))
	}

	// Concurrent reads
	go func() {
		for range 100 {
			_ = ctx.NewViewCount(view)
			_ = ctx.HasNewViewQuorum(view, 7)
			_ = ctx.HighestQCFromNewViews(view)
		}
		done <- struct{}{}
	}()

	// Wait for all goroutines
	for range 11 {
		<-done
	}

	// Should have exactly 10 NEWVIEWs (one per validator)
	if ctx.NewViewCount(view) != 10 {
		t.Errorf("Expected 10 NEWVIEWs, got %d", ctx.NewViewCount(view))
	}
}
