package hotstuff2

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

// HotStuff2 implements the HotStuff-2 consensus protocol.
//
// This implementation follows the TLA+ specification in formal-models/tla/hotstuff2.tla
// Key protocol actions:
// - LeaderPropose: Leader broadcasts proposal with justification QC
// - ReplicaVote: Replica votes if proposal extends locked QC (SafeNode rule)
// - LeaderFormQC: Leader forms QC from 2f+1 votes
// - ReplicaUpdateOnQC: Replica updates state on seeing QC
// - ReplicaTimeout: Replica advances view on timeout
//
// Safety: Lock mechanism prevents voting for conflicting blocks
// Liveness: Adaptive timeouts with exponential backoff
type HotStuff2[H Hash] struct {
	mu  sync.RWMutex
	cfg *Config[H]
	ctx *Context[H]
	pm  *Pacemaker

	// Channels
	msgChan    <-chan ConsensusPayload[H]
	stopChan   chan struct{}
	doneChan   chan struct{}
	cancelFunc context.CancelFunc

	// Callbacks
	onCommit func(Block[H])

	logger *zap.Logger
}

// NewHotStuff2 creates a new HotStuff2 consensus instance.
func NewHotStuff2[H Hash](cfg *Config[H], onCommit func(Block[H])) (*HotStuff2[H], error) {
	// Initialize context from storage
	genesisBlock, err := cfg.Storage.GetLastBlock()
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis block: %w", err)
	}

	// Initialize with genesis QC
	var genesisQC *QC[H]
	if genesisBlock != nil {
		if qc, err := cfg.Storage.GetQC(genesisBlock.Hash()); err == nil {
			if qcConcrete, ok := qc.(*QC[H]); ok {
				genesisQC = qcConcrete
			}
		}
	}

	ctx := NewContext(genesisBlock, genesisQC)

	// Load persisted state
	view, err := cfg.Storage.GetView()
	if err == nil {
		ctx.SetView(view)
	}

	lockedQC, err := cfg.Storage.GetHighestLockedQC()
	if err == nil && lockedQC != nil {
		if qcConcrete, ok := lockedQC.(*QC[H]); ok {
			ctx.UpdateLockedQC(qcConcrete)
		}
	}

	hs := &HotStuff2[H]{
		cfg:      cfg,
		ctx:      ctx,
		msgChan:  cfg.Network.Receive(),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
		onCommit: onCommit,
		logger:   cfg.Logger,
	}

	// Create pacemaker with timeout callback
	pacemakerConfig := DefaultPacemakerConfig()
	if cfg.Pacemaker != nil {
		pacemakerConfig = *cfg.Pacemaker
	}
	hs.pm = NewPacemakerWithConfig(cfg.Timer, cfg.Logger, hs.onViewTimeout, pacemakerConfig)

	return hs, nil
}

// Start starts the consensus protocol.
func (hs *HotStuff2[H]) Start() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	// Reinitialize channels for restart support
	// This allows a stopped consensus instance to be started again
	hs.stopChan = make(chan struct{})
	hs.doneChan = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	hs.cancelFunc = cancel

	hs.logger.Info("starting HotStuff2",
		zap.Uint32("view", hs.ctx.View()),
		zap.Uint16("validator_index", hs.cfg.MyIndex))

	// Start pacemaker
	hs.pm.Start(hs.ctx.View())

	// Start message processing loop
	go hs.run(ctx)

	// If I'm the leader of the current view, propose
	// (This handles both initial start and restart after crash)
	currentView := hs.ctx.View()
	if hs.cfg.IsLeader(currentView) {
		go hs.propose()
	}

	return nil
}

// Stop stops the consensus protocol.
func (hs *HotStuff2[H]) Stop() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	hs.logger.Info("stopping HotStuff2")

	// Close stopChan to signal run goroutine to exit
	// Use a select to avoid panic if already closed
	select {
	case <-hs.stopChan:
		// Already closed (double Stop call or never started)
		return
	default:
		close(hs.stopChan)
	}

	if hs.cancelFunc != nil {
		hs.cancelFunc()
	}
	hs.pm.Stop()

	// Only wait for doneChan if run goroutine was started
	// (cancelFunc is set in Start, so we use it as indicator)
	if hs.cancelFunc != nil {
		<-hs.doneChan
	}
}

// run is the main message processing loop.
func (hs *HotStuff2[H]) run(ctx context.Context) {
	defer close(hs.doneChan)

	for {
		// Priority 1: Check for shutdown signals
		select {
		case <-ctx.Done():
			return
		case <-hs.stopChan:
			return
		default:
		}

		// Priority 2: Drain all pending messages before checking timer.
		// This prevents race conditions where timer fires before votes are processed.
		drained := false
		for !drained {
			select {
			case msg := <-hs.msgChan:
				hs.onReceive(msg)
			default:
				drained = true
			}
		}

		// Priority 3: Check timer and messages together (blocking)
		select {
		case <-ctx.Done():
			return
		case <-hs.stopChan:
			return
		case msg := <-hs.msgChan:
			hs.onReceive(msg)
		case <-hs.pm.timer.C():
			view := hs.ctx.View()
			hs.pm.OnTimeout(view)
		}
	}
}

// onReceive routes incoming messages.
func (hs *HotStuff2[H]) onReceive(payload ConsensusPayload[H]) {
	switch payload.Type() {
	case MessageProposal:
		hs.onProposal(payload)
	case MessageVote:
		hs.onVote(payload)
	case MessageNewView:
		hs.onNewView(payload)
	default:
		hs.logger.Warn("unknown message type", zap.String("type", payload.Type().String()))
	}
}

// onProposal handles PROPOSE messages (TLA+ ReplicaVote action).
//
// TLA+ ReplicaVote(r):
//
//	/\ msg.type = "PROPOSE"
//	/\ v = view[r]                    // CRITICAL: Current view only
//	/\ SafeNodeRule(r, block, qc)     // Safety check
//	/\ Send VOTE message
//	/\ Update lockedQC if qc.view > lockedQC.view
func (hs *HotStuff2[H]) onProposal(payload ConsensusPayload[H]) {
	msg, ok := payload.(*ConsensusMessage[H])
	if !ok {
		hs.logger.Error("invalid proposal message type")
		return
	}

	block := msg.Block()
	justifyQC := msg.JustifyQC()
	view := msg.View()

	hs.logger.Info("received proposal",
		zap.Uint32("view", view),
		zap.Uint32("block_height", block.Height()),
		zap.String("block_hash", block.Hash().String()))

	// View synchronization: allow catching up to proposals with valid QCs
	// This is critical for liveness after crashes or network partitions
	currentView := hs.ctx.View()
	if justifyQC != nil && view > currentView {
		// A valid proposal for a higher view can help us catch up
		// The justifyQC proves the proposer has a valid chain
		// We sync to the proposal's view if:
		// 1. The proposal view is higher than ours
		// 2. The justifyQC view is at least as high as our highest known QC
		highQC := hs.ctx.HighQC()
		highQCView := uint32(0)
		if highQC != nil {
			highQCView = highQC.View()
		}
		if justifyQC.View() >= highQCView {
			hs.logger.Info("syncing view based on proposal QC",
				zap.Uint32("old_view", currentView),
				zap.Uint32("new_view", view),
				zap.Uint32("qc_view", justifyQC.View()))
			hs.ctx.SetView(view)
			_ = hs.cfg.Storage.PutView(view)
			// Also update our highQC if the proposal's is higher
			if justifyQC.View() > highQCView {
				hs.ctx.UpdateHighQC(justifyQC)
				// CRITICAL: Store the justifyQC so we can use it for two-chain commit checks
				// Without this, checkCommit() fails with "cannot determine block view - no QC found"
				hs.ctx.AddQC(justifyQC)
				_ = hs.cfg.Storage.PutQC(justifyQC)
			}
			hs.pm.Start(view)
			currentView = view
		}
	}

	// TLA+ guard: v = view[r] (CRITICAL: exact equality, not >=)
	if view != currentView {
		hs.logger.Debug("proposal for wrong view",
			zap.Uint32("proposal_view", view),
			zap.Uint32("current_view", currentView))
		return
	}

	// Check minimum block time enforcement
	// Validators refuse to vote for proposals that arrive too early
	// This prevents malicious leaders from speeding up the chain
	if hs.pm.MinBlockTimeEnabled() {
		lastCommitTime := hs.ctx.LastCommitTime()
		if !hs.pm.CanAcceptProposal(lastCommitTime) {
			remaining := hs.pm.TimeUntilCanPropose(lastCommitTime)
			hs.logger.Debug("proposal arrived too early, rejecting",
				zap.Uint32("view", view),
				zap.Duration("time_until_allowed", remaining))
			return
		}
	}

	// Only non-leaders vote
	if hs.cfg.IsLeader(view) {
		return
	}

	// Check if already voted in this view
	if hs.ctx.HasVoted(view, hs.cfg.MyIndex) {
		hs.logger.Debug("already voted in this view", zap.Uint32("view", view))
		return
	}

	// Verify block
	if err := hs.cfg.Executor.Verify(block); err != nil {
		hs.logger.Error("block verification failed", zap.Error(err))
		return
	}

	// Store block
	hs.ctx.AddBlock(block)
	if err := hs.cfg.Storage.PutBlock(block); err != nil {
		hs.logger.Error("failed to store block", zap.Error(err))
		return
	}

	// TLA+ SafeNodeRule: Can vote if justifyQC supersedes lock OR block extends lock
	if !hs.ctx.SafeToVote(block, justifyQC) {
		hs.logger.Warn("not safe to vote (SafeNode rule violated)",
			zap.Uint32("view", view))
		return
	}

	// TLA+ UpdateLockCond: Update locked QC if justify QC is higher
	if justifyQC != nil {
		// CRITICAL: Store received QCs for two-chain commit verification.
		//
		// TLA+ spec: blockView[b] is a global map storing each block's proposal view.
		// Go implementation: Blocks don't have a View() field, so we use QCs to track views.
		//   - checkCommit() needs to find the QC for a block to determine its view
		//   - GetQC(blockHash) returns the QC that certified that block
		//   - Therefore, QCs received in proposals/NEWVIEWs must be stored
		//
		// Without this, checkCommit fails with "cannot determine block view - no QC found"
		// even though the QC exists in the protocol (abstraction gap between TLA+ and Go).
		hs.ctx.AddQC(justifyQC)
		_ = hs.cfg.Storage.PutQC(justifyQC)

		if hs.ctx.UpdateLockedQC(justifyQC) {
			// Persist locked QC (safety-critical)
			if err := hs.cfg.Storage.PutHighestLockedQC(justifyQC); err != nil {
				hs.logger.Error("failed to persist locked QC", zap.Error(err))
				return
			}
			hs.logger.Info("updated locked QC",
				zap.Uint32("qc_view", justifyQC.View()))
		}
	}

	// Create and send vote
	vote, err := NewVote(view, block.Hash(), hs.cfg.MyIndex, hs.cfg.PrivateKey)
	if err != nil {
		hs.logger.Error("failed to create vote", zap.Error(err))
		return
	}

	// TLA+ network' = network \cup {voteMsg}
	voteMsg := NewVoteMessage(view, hs.cfg.MyIndex, vote)
	hs.cfg.Network.Broadcast(voteMsg)

	hs.logger.Info("voted for proposal",
		zap.Uint32("view", view),
		zap.String("block_hash", block.Hash().String()))

	// Track our own vote and check if we can form QC
	// This is important because we may have received other votes before the proposal
	voteCount := hs.ctx.AddVote(vote)
	hs.tryFormQCIfQuorum(view, block.Hash(), voteCount)

	// Reset pacemaker (made progress)
	hs.pm.OnProgress(view)
}

// onVote handles VOTE messages (TLA+ LeaderFormQC action).
//
// TLA+ LeaderFormQC(r):
//
//	/\ HasQuorumInNetwork(v, b)       // 2f+1 votes
//	/\ highQC' = b
//	/\ Update lockedQC if b.view > lockedQC.view
//	/\ Check CanCommit (two-chain rule)
func (hs *HotStuff2[H]) onVote(payload ConsensusPayload[H]) {
	msg, ok := payload.(*ConsensusMessage[H])
	if !ok {
		hs.logger.Error("invalid vote message type")
		return
	}

	vote := msg.Vote()
	view := vote.View()
	nodeHash := vote.NodeHash()

	hs.logger.Debug("received vote",
		zap.Uint32("view", view),
		zap.Uint16("validator", vote.ValidatorIndex()),
		zap.String("block_hash", nodeHash.String()))

	// Verify vote signature
	publicKey, err := hs.cfg.Validators.GetByIndex(vote.ValidatorIndex())
	if err != nil {
		hs.logger.Error("invalid validator index", zap.Uint16("index", vote.ValidatorIndex()))
		return
	}

	if err := vote.Verify(publicKey); err != nil {
		hs.logger.Error("vote signature verification failed", zap.Error(err))
		return
	}

	// Add vote to context
	voteCount := hs.ctx.AddVote(vote)

	hs.logger.Debug("vote added",
		zap.Uint32("view", view),
		zap.String("block_hash", nodeHash.String()),
		zap.Int("vote_count", voteCount))

	// Try to form QC if we have quorum
	hs.tryFormQCIfQuorum(view, nodeHash, voteCount)
}

// tryFormQCIfQuorum checks if we have quorum votes and forms a QC if so.
// This is called both when receiving votes (onVote) and after voting ourselves (onProposal).
// The latter case handles the scenario where we receive votes before seeing the proposal.
func (hs *HotStuff2[H]) tryFormQCIfQuorum(view uint32, nodeHash H, voteCount int) {
	// Check if we have quorum (2f+1 votes)
	quorum := hs.cfg.Quorum()
	if voteCount < quorum {
		return
	}

	hs.logger.Info("quorum reached",
		zap.Uint32("view", view),
		zap.String("block_hash", nodeHash.String()),
		zap.Int("votes", voteCount),
		zap.Int("quorum", quorum))

	// Form QC from votes
	votes := hs.ctx.GetVotes(view, nodeHash)
	qc, err := NewQC(view, nodeHash, votes, hs.cfg.Validators, hs.cfg.CryptoScheme)
	if err != nil {
		hs.logger.Error("failed to form QC", zap.Error(err))
		return
	}

	// Validate QC (defense in depth)
	if err := qc.Validate(hs.cfg.Validators); err != nil {
		hs.logger.Error("QC validation failed", zap.Error(err))
		return
	}

	// Store QC
	hs.ctx.AddQC(qc)
	if err := hs.cfg.Storage.PutQC(qc); err != nil {
		hs.logger.Error("failed to store QC", zap.Error(err))
		return
	}

	hs.logger.Info("QC formed",
		zap.Uint32("view", view),
		zap.String("block_hash", nodeHash.String()))

	// TLA+ highQC' = b
	hs.ctx.UpdateHighQC(qc)

	// TLA+ UpdateLockCond
	if hs.ctx.UpdateLockedQC(qc) {
		if err := hs.cfg.Storage.PutHighestLockedQC(qc); err != nil {
			hs.logger.Error("failed to persist locked QC", zap.Error(err))
		}
	}

	// TLA+ CanCommit: Check two-chain commit rule
	// When we form QC for block B, we check if B's parent can commit
	block, exists := hs.ctx.GetBlock(nodeHash)
	if exists {
		// Get parent block
		parentHash := block.PrevHash()
		if parent, parentExists := hs.ctx.GetBlock(parentHash); parentExists {
			hs.checkCommit(parent, qc)
		}
	}

	// If I'm the leader, propose next block using the QC we just formed
	// This is the happy path (TLA+ LeaderPropose) - we have a QC, so we can propose immediately
	// No need to wait for NEWVIEWs since we formed the QC from votes
	nextView := view + 1
	if hs.cfg.IsLeader(nextView) {
		go hs.advanceViewWithQC(nextView, qc)
	}
}

// checkCommit implements the two-chain commit rule.
//
// TLA+ CanCommit(block, qc):
//
//	/\ blockParent[qc] = block
//	/\ blockView[qc] = blockView[block] + 1
//
// Two consecutive QCs commit the first block.
// Returns true if a block was committed (for commit delay tracking).
func (hs *HotStuff2[H]) checkCommit(block Block[H], qc *QC[H]) bool {
	// Already committed check (early exit for efficiency)
	if hs.ctx.IsCommitted(block.Height()) {
		return false // Already committed
	}

	qcBlock, exists := hs.ctx.GetBlock(qc.Node())
	if !exists {
		return false
	}

	// Check if qcBlock is child of block (TLA+: blockParent[qc] = block)
	if !qcBlock.PrevHash().Equals(block.Hash()) {
		return false
	}

	// TLA+: blockView[qc] = blockView[block] + 1 (consecutive views)
	// The QC's view is the view in which qcBlock was proposed.
	// We need the view of 'block' (the parent), which is stored in the QC that certifies 'block'.
	blockQC, blockQCExists := hs.ctx.GetQC(block.Hash())
	if !blockQCExists {
		// No QC for block - this can happen for genesis block (height 0)
		// Genesis is implicitly at view 0, so we check if qc.View() == 1
		if block.Height() == 0 {
			if qc.View() != 1 {
				hs.logger.Debug("non-consecutive views for genesis commit",
					zap.Uint32("qc_view", qc.View()),
					zap.Uint32("expected", 1))
				return false
			}
		} else {
			// Non-genesis block without QC - shouldn't happen in normal operation
			hs.logger.Debug("cannot determine block view - no QC found",
				zap.Uint32("height", block.Height()))
			return false
		}
	} else {
		// Normal case: check consecutive views
		// blockQC.View() is the view in which 'block' was proposed
		// qc.View() is the view in which qcBlock was proposed
		// For two-chain commit: qc.View() must equal blockQC.View() + 1
		if qc.View() != blockQC.View()+1 {
			hs.logger.Debug("non-consecutive views, cannot commit",
				zap.Uint32("block_view", blockQC.View()),
				zap.Uint32("qc_view", qc.View()),
				zap.Uint32("block_height", block.Height()))
			return false
		}
	}

	hs.logger.Info("committing block",
		zap.Uint32("height", block.Height()),
		zap.String("hash", block.Hash().String()))

	hs.ctx.Commit(block)

	// Execute block
	if _, err := hs.cfg.Executor.Execute(block); err != nil {
		hs.logger.Error("block execution failed", zap.Error(err))
	}

	// Notify pacemaker of commit (resets backoff, starts commit delay)
	hs.pm.OnCommit(qc.View())

	// Callback
	if hs.onCommit != nil {
		hs.onCommit(block)
	}

	return true
}

// onNewView handles NEWVIEW messages (TLA+ LeaderProcessNewView).
//
// TLA+ LeaderProcessNewView(r):
//
//	/\ r = Leader(v)
//	/\ IsQuorum(NewViewReplicasFromNetwork(v))  // 2f+1 NEWVIEWs
//	/\ highestQC = HighestQCFromNetwork(v)      // Select highest QC
//	/\ Propose with highestQC as justification
//
// Implementation note: The TLA+ spec assumes nondeterministic timing where
// timeouts can fire at any point. In practice, nodes have similar timeout
// durations, so after a crash fault, all nodes may timeout roughly together.
// This can lead to view desynchronization where nodes are at different views.
//
// To handle this, we implement view synchronization on NEWVIEWs:
//  1. Sync forward: If we receive a NEWVIEW for a higher view with a valid QC,
//     we can advance to that view (helps lagging nodes catch up)
//  2. Sync backward (leader only): If we're the leader of view V but have
//     advanced past it, we can sync back if we have quorum NEWVIEWs and
//     haven't voted (preserving safety)
func (hs *HotStuff2[H]) onNewView(payload ConsensusPayload[H]) {
	msg, ok := payload.(*ConsensusMessage[H])
	if !ok {
		return
	}

	view := msg.View()
	highQC := msg.HighQC()
	validatorIndex := msg.ValidatorIndex()

	hs.logger.Debug("received newview",
		zap.Uint32("view", view),
		zap.Uint16("from", validatorIndex))

	// Track NEWVIEW message (regardless of whether we're leader)
	// This allows us to have the data ready when we become leader
	count := hs.ctx.AddNewView(view, validatorIndex, highQC)

	hs.logger.Debug("newview tracked",
		zap.Uint32("view", view),
		zap.Int("count", count),
		zap.Int("quorum", hs.cfg.Quorum()))

	// Update our highQC if this one is higher (always useful)
	if highQC != nil {
		hs.ctx.UpdateHighQC(highQC)
		// Store the QC for two-chain commit verification
		hs.ctx.AddQC(highQC)
		_ = hs.cfg.Storage.PutQC(highQC)
	}

	currentView := hs.ctx.View()
	quorum := hs.cfg.Quorum()

	// View synchronization: Sync forward if receiving NEWVIEWs for a higher view
	// This helps nodes that are behind catch up to where consensus is happening.
	// The NEWVIEW's QC proves the sender has a valid chain at that point.
	if view > currentView && highQC != nil {
		ourHighQC := hs.ctx.HighQC()
		ourHighQCView := uint32(0)
		if ourHighQC != nil {
			ourHighQCView = ourHighQC.View()
		}
		// Sync forward if the NEWVIEW's QC is at least as good as ours
		if highQC.View() >= ourHighQCView {
			hs.logger.Info("syncing view forward based on NEWVIEW",
				zap.Uint32("old_view", currentView),
				zap.Uint32("new_view", view),
				zap.Uint32("newview_qc_view", highQC.View()))
			hs.ctx.SetView(view)
			_ = hs.cfg.Storage.PutView(view)
			hs.pm.Start(view)
			currentView = view

			// CRITICAL: When syncing forward, we must also send our own NEWVIEW
			// for this view so the leader can collect quorum. Without this,
			// the leader would only see NEWVIEWs from nodes that timed out,
			// not from nodes that synced forward.
			myHighQC := hs.ctx.HighQC()
			newViewMsg := NewNewViewMessage(view, hs.cfg.MyIndex, myHighQC)
			hs.cfg.Network.Broadcast(newViewMsg)
			hs.ctx.AddNewView(view, hs.cfg.MyIndex, myHighQC)
		}
	}

	// TLA+ guard: r = Leader(v)
	// Only the leader processes NEWVIEWs for proposing
	if !hs.cfg.IsLeader(view) {
		return
	}

	// Leader view synchronization for crash fault tolerance:
	// If we're the leader of view V but have advanced past it (due to our own
	// timeout firing before collecting enough NEWVIEWs), we can safely sync
	// back to V if:
	// 1. We have a quorum of NEWVIEWs for V (proves consensus should happen at V)
	// 2. We haven't voted in V (no equivocation risk)
	//
	// This handles the common crash recovery scenario where:
	// - Previous leader crashed
	// - All nodes timeout and send NEWVIEW for next view
	// - New leader's timeout also fires before collecting NEWVIEWs
	// - NEWVIEWs arrive after leader advanced
	if view < currentView && hs.ctx.HasNewViewQuorum(view, quorum) {
		// Safety check: only sync back if we haven't voted in that view
		if !hs.ctx.HasVoted(view, hs.cfg.MyIndex) {
			hs.logger.Info("leader syncing back to view with NEWVIEW quorum",
				zap.Uint32("from_view", currentView),
				zap.Uint32("to_view", view),
				zap.Int("newview_count", count))
			hs.ctx.SetView(view)
			_ = hs.cfg.Storage.PutView(view)
			hs.pm.Start(view)
			currentView = view
		} else {
			hs.logger.Debug("cannot sync back - already voted in view",
				zap.Uint32("view", view))
			return
		}
	}

	// TLA+ guard: view[r] = v
	if view != currentView {
		hs.logger.Debug("newview for different view",
			zap.Uint32("newview_view", view),
			zap.Uint32("current_view", currentView))
		return
	}

	// TLA+ guard: IsQuorum(NewViewReplicasFromNetwork(v))
	// Wait for 2f+1 NEWVIEW messages before proposing
	if !hs.ctx.HasNewViewQuorum(view, quorum) {
		hs.logger.Debug("waiting for more newviews",
			zap.Uint32("view", view),
			zap.Int("count", count),
			zap.Int("quorum", quorum))
		return
	}

	hs.logger.Info("newview quorum reached",
		zap.Uint32("view", view),
		zap.Int("count", count),
		zap.Int("quorum", quorum))

	// TLA+ highestQC = HighestQCFromNetwork(v)
	// Propose with the highest QC from all collected NEWVIEWs
	// Note: This must be synchronous to avoid race with view changes
	hs.proposeWithNewViewQuorum(view)
}

// onViewTimeout handles timeout (TLA+ ReplicaTimeout).
//
// TLA+ ReplicaTimeout(r):
//
//	/\ view' = view + 1
//	/\ Send NEWVIEW with highQC
//	/\ Exponential backoff
func (hs *HotStuff2[H]) onViewTimeout(view uint32) {
	currentView := hs.ctx.View()
	if view != currentView {
		return // Stale timeout
	}

	hs.logger.Warn("view timeout, advancing",
		zap.Uint32("old_view", currentView),
		zap.Uint32("new_view", currentView+1))

	// Advance view
	nextView := currentView + 1
	hs.advanceView(nextView)

	// Increase timeout (exponential backoff)
	hs.pm.IncreaseTimeout()
}

// advanceViewWithQC advances to the next view after forming a QC (happy path).
//
// TLA+ LeaderPropose: Leader has QC from normal voting, can propose immediately.
// This is different from advanceView (timeout path) which waits for NEWVIEWs.
func (hs *HotStuff2[H]) advanceViewWithQC(nextView uint32, qc *QC[H]) {
	hs.ctx.SetView(nextView)
	if err := hs.cfg.Storage.PutView(nextView); err != nil {
		hs.logger.Error("failed to persist view", zap.Error(err))
	}

	// Prune old votes and NEWVIEWs to prevent unbounded memory growth.
	const keepViews = 10
	hs.ctx.PruneVotes(nextView, keepViews)

	hs.logger.Info("advanced to new view with QC",
		zap.Uint32("view", nextView),
		zap.Uint32("qc_view", qc.View()))

	// Start timer for new view
	hs.pm.Start(nextView)

	// Leader proposes immediately using the QC we just formed
	if hs.cfg.IsLeader(nextView) {
		go hs.propose()
	}
}

// advanceView advances to the next view (TLA+ ReplicaTimeout continuation).
//
// When advancing due to timeout, the replica:
// 1. Updates its view
// 2. Sends NEWVIEW message with its highQC
// 3. If leader, waits for 2f+1 NEWVIEWs before proposing
func (hs *HotStuff2[H]) advanceView(nextView uint32) {
	hs.ctx.SetView(nextView)
	if err := hs.cfg.Storage.PutView(nextView); err != nil {
		hs.logger.Error("failed to persist view", zap.Error(err))
	}

	// Prune old votes and NEWVIEWs to prevent unbounded memory growth.
	// Keep votes from recent views in case of reordering or delayed messages.
	const keepViews = 10
	hs.ctx.PruneVotes(nextView, keepViews)

	// TLA+ network' = network \cup { [type |-> "NEWVIEW", view |-> nextView, ...] }
	// Send NEWVIEW message with our highQC
	highQC := hs.ctx.HighQC()
	newViewMsg := NewNewViewMessage(nextView, hs.cfg.MyIndex, highQC)
	hs.cfg.Network.Broadcast(newViewMsg)

	// Track our own NEWVIEW (we count toward quorum too)
	hs.ctx.AddNewView(nextView, hs.cfg.MyIndex, highQC)

	hs.logger.Info("advanced to new view",
		zap.Uint32("view", nextView))

	// Start timer for new view
	hs.pm.Start(nextView)

	// If I'm the new leader, check if we already have enough NEWVIEWs
	// (This can happen if we're slow and others already sent NEWVIEWs)
	if hs.cfg.IsLeader(nextView) {
		quorum := hs.cfg.Quorum()
		if hs.ctx.HasNewViewQuorum(nextView, quorum) {
			hs.logger.Info("already have newview quorum, proposing immediately",
				zap.Uint32("view", nextView))
			hs.proposeWithNewViewQuorum(nextView)
		} else {
			hs.logger.Info("waiting for newview quorum before proposing",
				zap.Uint32("view", nextView),
				zap.Int("current_count", hs.ctx.NewViewCount(nextView)),
				zap.Int("quorum", quorum))
		}
	}
}

// propose creates and broadcasts a new proposal (TLA+ LeaderPropose).
//
// TLA+ LeaderPropose(r):
//
//	/\ r = Leader(v)
//	/\ Create new block with parent = highQC
//	/\ Broadcast PROPOSE(block, highQC)
//
// This is used for initial view (view 0) or when leader has QC from previous view.
// For view changes triggered by timeout, use proposeWithNewViewQuorum instead.
func (hs *HotStuff2[H]) propose() {
	view := hs.ctx.View()

	if !hs.cfg.IsLeader(view) {
		return
	}

	// Wait for minimum block time if configured
	// This ensures leaders don't propose faster than the target block rate
	if hs.pm.MinBlockTimeEnabled() {
		lastCommitTime := hs.ctx.LastCommitTime()
		hs.pm.WaitForMinBlockTime(lastCommitTime)
	}

	hs.logger.Info("proposing as leader", zap.Uint32("view", view))

	// Get parent from highQC
	highQC := hs.ctx.HighQC()
	var parentHash H
	var height uint32 = 1

	if highQC != nil {
		parentHash = highQC.Node()
		if parent, exists := hs.ctx.GetBlock(parentHash); exists {
			height = parent.Height() + 1
		}
	}

	// Create block
	block, err := hs.cfg.Executor.CreateBlock(height, parentHash, hs.cfg.MyIndex)
	if err != nil {
		hs.logger.Error("failed to create block", zap.Error(err))
		return
	}

	// Store block
	hs.ctx.AddBlock(block)
	if err := hs.cfg.Storage.PutBlock(block); err != nil {
		hs.logger.Error("failed to store block", zap.Error(err))
		return
	}

	// Broadcast proposal
	proposal := NewProposeMessage(view, hs.cfg.MyIndex, block, highQC)
	hs.cfg.Network.Broadcast(proposal)

	hs.logger.Info("proposed block",
		zap.Uint32("view", view),
		zap.Uint32("height", block.Height()),
		zap.String("hash", block.Hash().String()),
		zap.Uint32("justify_qc_view", qcViewOrZero(highQC)))

	// Leader implicitly votes for its own proposal by creating and tracking a vote.
	// This is necessary for liveness: with n=3f+1, if f nodes are faulty and
	// the leader doesn't vote, we'd need 2f+1 votes from 2f honest non-leaders,
	// which is impossible.
	leaderVote, err := NewVote(view, block.Hash(), hs.cfg.MyIndex, hs.cfg.PrivateKey)
	if err != nil {
		hs.logger.Error("failed to create leader vote", zap.Error(err))
		return
	}
	voteCount := hs.ctx.AddVote(leaderVote)

	// Check if leader's vote alone forms quorum (e.g., n=1 single-node setup).
	// This also handles the case where votes arrived before the proposal.
	hs.tryFormQCIfQuorum(view, block.Hash(), voteCount)
}

// proposeWithNewViewQuorum creates a proposal after collecting 2f+1 NEWVIEWs.
//
// TLA+ LeaderProcessNewView(r):
//
//	/\ IsQuorum(NewViewReplicasFromNetwork(v))
//	/\ highestQC = HighestQCFromNetwork(v)
//	/\ Propose with highestQC as justification
//
// This ensures the leader uses the highest QC seen by any correct replica,
// which is critical for liveness after a view change.
func (hs *HotStuff2[H]) proposeWithNewViewQuorum(view uint32) {
	if !hs.cfg.IsLeader(view) {
		return
	}

	// Verify we're still in the expected view
	if hs.ctx.View() != view {
		hs.logger.Debug("view changed before proposal",
			zap.Uint32("expected_view", view),
			zap.Uint32("current_view", hs.ctx.View()))
		return
	}

	// Wait for minimum block time if configured
	// This ensures leaders don't propose faster than the target block rate
	if hs.pm.MinBlockTimeEnabled() {
		lastCommitTime := hs.ctx.LastCommitTime()
		hs.pm.WaitForMinBlockTime(lastCommitTime)
	}

	hs.logger.Info("proposing after newview quorum", zap.Uint32("view", view))

	// TLA+ highestQC = HighestQCFromNetwork(v)
	// Get the highest QC from all collected NEWVIEW messages
	justifyQC := hs.ctx.HighestQCFromNewViews(view)

	// Fall back to our own highQC if no QC in NEWVIEWs (e.g., genesis)
	if justifyQC == nil {
		justifyQC = hs.ctx.HighQC()
	}

	var parentHash H
	var height uint32 = 1

	if justifyQC != nil {
		parentHash = justifyQC.Node()
		if parent, exists := hs.ctx.GetBlock(parentHash); exists {
			height = parent.Height() + 1
		}
	}

	// Create block
	block, err := hs.cfg.Executor.CreateBlock(height, parentHash, hs.cfg.MyIndex)
	if err != nil {
		hs.logger.Error("failed to create block", zap.Error(err))
		return
	}

	// Store block
	hs.ctx.AddBlock(block)
	if err := hs.cfg.Storage.PutBlock(block); err != nil {
		hs.logger.Error("failed to store block", zap.Error(err))
		return
	}

	// Broadcast proposal with the highest QC from NEWVIEWs
	proposal := NewProposeMessage(view, hs.cfg.MyIndex, block, justifyQC)
	hs.cfg.Network.Broadcast(proposal)

	hs.logger.Info("proposed block after newview quorum",
		zap.Uint32("view", view),
		zap.Uint32("height", height),
		zap.String("hash", block.Hash().String()),
		zap.Uint32("justify_qc_view", qcViewOrZero(justifyQC)))

	// Leader implicitly votes for its own proposal
	leaderVote, err := NewVote(view, block.Hash(), hs.cfg.MyIndex, hs.cfg.PrivateKey)
	if err != nil {
		hs.logger.Error("failed to create leader vote", zap.Error(err))
		return
	}
	voteCount := hs.ctx.AddVote(leaderVote)

	// Check if leader's vote alone forms quorum (e.g., n=1 single-node setup).
	// This also handles the case where votes arrived before the proposal.
	hs.tryFormQCIfQuorum(view, block.Hash(), voteCount)
}

// qcViewOrZero returns the QC view or 0 if nil (helper for logging).
func qcViewOrZero[H Hash](qc *QC[H]) uint32 {
	if qc == nil {
		return 0
	}
	return qc.View()
}

// View returns the current view.
func (hs *HotStuff2[H]) View() uint32 {
	return hs.ctx.View()
}

// Height returns the height of the latest committed block.
func (hs *HotStuff2[H]) Height() uint32 {
	blocks := hs.ctx.CommittedBlocks()
	if len(blocks) == 0 {
		return 0
	}

	maxHeight := uint32(0)
	for _, block := range blocks {
		if block.Height() > maxHeight {
			maxHeight = block.Height()
		}
	}
	return maxHeight
}
