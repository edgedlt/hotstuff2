package hotstuff2

import (
	"sync"
	"time"
)

// Context maintains the consensus state for a HotStuff2 replica.
//
// This maps to the TLA+ spec state variables:
// - view[r]      -> view
// - lockedQC[r]  -> lockedQC
// - highQC[r]    -> highQC
// - committed[r] -> committed
// - votes tracking -> votes map
// - NEWVIEW tracking -> newviews map (for leader view change)
//
// Thread-safe: All methods use mutex for concurrent access.
//
// Note: Maps use string keys (hash.String()) instead of Hash type directly
// to avoid comparable constraint issues with generic interfaces.
type Context[H Hash] struct {
	mu sync.RWMutex

	// Current view number
	view uint32

	// Locked QC (safety mechanism - from TLA+ spec)
	// Updated only when new QC view > current lockedQC view
	lockedQC *QC[H]

	// Highest QC seen (liveness mechanism)
	highQC *QC[H]

	// Committed blocks (indexed by height)
	committed map[uint32]Block[H]

	// Vote tracking: view -> nodeHash (string) -> validatorIndex -> Vote
	votes map[uint32]map[string]map[uint16]*Vote[H]

	// NEWVIEW tracking: view -> validatorIndex -> QC (from NEWVIEW message)
	// Used by leader to collect 2f+1 NEWVIEWs and select highest QC
	// TLA+ spec: NewViewReplicasFromNetwork(v), HighestQCFromNetwork(v)
	newviews map[uint32]map[uint16]*QC[H]

	// Block storage: hash (string) -> Block
	blocksByHash map[string]Block[H]

	// QC storage: nodeHash (string) -> QC
	qcsByNode map[string]*QC[H]

	// Parent relationships for ancestry checking: child (string) -> parent hash
	parents map[string]H

	// Last commit time for minimum block time enforcement
	lastCommitTime time.Time
}

// NewContext creates a new consensus context.
func NewContext[H Hash](genesisBlock Block[H], genesisQC *QC[H]) *Context[H] {
	ctx := &Context[H]{
		view:         0,
		lockedQC:     genesisQC,
		highQC:       genesisQC,
		committed:    make(map[uint32]Block[H]),
		votes:        make(map[uint32]map[string]map[uint16]*Vote[H]),
		newviews:     make(map[uint32]map[uint16]*QC[H]),
		blocksByHash: make(map[string]Block[H]),
		qcsByNode:    make(map[string]*QC[H]),
		parents:      make(map[string]H),
	}

	// Store genesis
	if genesisBlock != nil {
		hashStr := genesisBlock.Hash().String()
		ctx.blocksByHash[hashStr] = genesisBlock
		ctx.committed[genesisBlock.Height()] = genesisBlock
	}

	if genesisQC != nil {
		nodeStr := genesisQC.Node().String()
		ctx.qcsByNode[nodeStr] = genesisQC
	}

	return ctx
}

// View returns the current view number.
func (c *Context[H]) View() uint32 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.view
}

// SetView updates the current view.
func (c *Context[H]) SetView(view uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.view = view
}

// LockedQC returns the current locked QC.
func (c *Context[H]) LockedQC() *QC[H] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lockedQC
}

// UpdateLockedQC updates the locked QC if the new QC has higher view.
func (c *Context[H]) UpdateLockedQC(qc *QC[H]) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if qc == nil {
		return false
	}

	if c.lockedQC == nil || qc.View() > c.lockedQC.View() {
		c.lockedQC = qc
		return true
	}

	return false
}

// HighQC returns the highest QC seen.
func (c *Context[H]) HighQC() *QC[H] {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.highQC
}

// UpdateHighQC updates the highest QC if the new QC has higher view.
func (c *Context[H]) UpdateHighQC(qc *QC[H]) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if qc == nil {
		return false
	}

	if c.highQC == nil || qc.View() > c.highQC.View() {
		c.highQC = qc
		return true
	}

	return false
}

// AddVote adds a vote to the vote tracking.
func (c *Context[H]) AddVote(vote *Vote[H]) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	view := vote.View()
	nodeHashStr := vote.NodeHash().String()
	validatorIndex := vote.ValidatorIndex()

	if c.votes[view] == nil {
		c.votes[view] = make(map[string]map[uint16]*Vote[H])
	}
	if c.votes[view][nodeHashStr] == nil {
		c.votes[view][nodeHashStr] = make(map[uint16]*Vote[H])
	}

	c.votes[view][nodeHashStr][validatorIndex] = vote
	return len(c.votes[view][nodeHashStr])
}

// GetVotes returns all votes for a block in a view.
func (c *Context[H]) GetVotes(view uint32, nodeHash H) []*Vote[H] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeHashStr := nodeHash.String()
	if c.votes[view] == nil || c.votes[view][nodeHashStr] == nil {
		return nil
	}

	votes := make([]*Vote[H], 0, len(c.votes[view][nodeHashStr]))
	for _, vote := range c.votes[view][nodeHashStr] {
		votes = append(votes, vote)
	}

	return votes
}

// VoteCount returns the number of votes for a block in a view.
func (c *Context[H]) VoteCount(view uint32, nodeHash H) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeHashStr := nodeHash.String()
	if c.votes[view] == nil || c.votes[view][nodeHashStr] == nil {
		return 0
	}

	return len(c.votes[view][nodeHashStr])
}

// HasVoted returns true if the validator has voted in this view.
func (c *Context[H]) HasVoted(view uint32, validatorIndex uint16) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.votes[view] == nil {
		return false
	}

	for _, blockVotes := range c.votes[view] {
		if _, exists := blockVotes[validatorIndex]; exists {
			return true
		}
	}

	return false
}

// AddBlock stores a block in the context.
func (c *Context[H]) AddBlock(block Block[H]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hashStr := block.Hash().String()
	c.blocksByHash[hashStr] = block
	c.parents[hashStr] = block.PrevHash()
}

// GetBlock retrieves a block by hash.
func (c *Context[H]) GetBlock(hash H) (Block[H], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hashStr := hash.String()
	block, exists := c.blocksByHash[hashStr]
	return block, exists
}

// AddQC stores a QC in the context.
func (c *Context[H]) AddQC(qc *QC[H]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	nodeStr := qc.Node().String()
	c.qcsByNode[nodeStr] = qc
}

// GetQC retrieves a QC by node hash.
func (c *Context[H]) GetQC(nodeHash H) (*QC[H], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	nodeStr := nodeHash.String()
	qc, exists := c.qcsByNode[nodeStr]
	return qc, exists
}

// IsCommitted returns true if a block at the given height is committed.
func (c *Context[H]) IsCommitted(height uint32) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	_, exists := c.committed[height]
	return exists
}

// GetCommitted returns the committed block at the given height.
func (c *Context[H]) GetCommitted(height uint32) (Block[H], bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	block, exists := c.committed[height]
	return block, exists
}

// Commit marks a block as committed and records the commit time.
func (c *Context[H]) Commit(block Block[H]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.committed[block.Height()] = block
	c.lastCommitTime = time.Now()
}

// LastCommitTime returns the time of the last commit.
func (c *Context[H]) LastCommitTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastCommitTime
}

// TimeSinceLastCommit returns the duration since the last commit.
func (c *Context[H]) TimeSinceLastCommit() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.lastCommitTime.IsZero() {
		return time.Duration(0) // No commits yet, no need to wait
	}
	return time.Since(c.lastCommitTime)
}

// CommittedBlocks returns all committed blocks.
func (c *Context[H]) CommittedBlocks() []Block[H] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	blocks := make([]Block[H], 0, len(c.committed))
	for _, block := range c.committed {
		blocks = append(blocks, block)
	}

	return blocks
}

// IsAncestor checks if ancestor is an ancestor of descendant in the block tree.
func (c *Context[H]) IsAncestor(ancestor H, descendant H) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ancestor.Equals(descendant) {
		return true
	}

	currentStr := descendant.String()
	maxDepth := 1000

	for range maxDepth {
		parent, exists := c.parents[currentStr]
		if !exists {
			return false
		}

		if ancestor.Equals(parent) {
			return true
		}

		currentStr = parent.String()
	}

	return false
}

// SafeToVote implements the TLA+ SafeNodeRule predicate.
func (c *Context[H]) SafeToVote(block Block[H], justifyQC *QC[H]) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.lockedQC == nil {
		return true
	}

	if justifyQC != nil && justifyQC.View() > c.lockedQC.View() {
		return true
	}

	if c.isAncestorLocked(c.lockedQC.Node(), block.Hash()) {
		return true
	}

	return false
}

// isAncestorLocked is the internal version of IsAncestor without locking.
func (c *Context[H]) isAncestorLocked(ancestor H, descendant H) bool {
	if ancestor.Equals(descendant) {
		return true
	}

	currentStr := descendant.String()
	maxDepth := 1000

	for range maxDepth {
		parent, exists := c.parents[currentStr]
		if !exists {
			return false
		}

		if ancestor.Equals(parent) {
			return true
		}

		currentStr = parent.String()
	}

	return false
}

// CanCommit checks if a block can be committed using the two-chain rule.
//
// TLA+ CanCommit(block, qc):
//
//	/\ blockParent[qc] = block
//	/\ blockView[qc] = blockView[block] + 1
//
// Both conditions must hold: qcBlock must be child of block AND they must be
// in consecutive views.
func (c *Context[H]) CanCommit(block Block[H], qc *QC[H]) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if qc == nil {
		return false
	}

	// Check qcBlock exists
	qcBlock, exists := c.blocksByHash[qc.Node().String()]
	if !exists {
		return false
	}

	// TLA+: blockParent[qc] = block
	if !qcBlock.PrevHash().Equals(block.Hash()) {
		return false
	}

	// TLA+: blockView[qc] = blockView[block] + 1 (consecutive views)
	// Get the QC that certifies 'block' to determine its view
	blockQC, blockQCExists := c.qcsByNode[block.Hash().String()]
	if !blockQCExists {
		// No QC for block - genesis block case
		// Genesis is implicitly at view 0, so qc.View() must be 1
		if block.Height() == 0 {
			return qc.View() == 1
		}
		// Non-genesis block without QC - cannot verify consecutive views
		return false
	}

	// Check consecutive views
	return qc.View() == blockQC.View()+1
}

// AddNewView records a NEWVIEW message from a validator for a view.
// Returns the count of unique validators who have sent NEWVIEW for this view.
// TLA+ spec: network' = network \cup { [type |-> "NEWVIEW", view |-> v, replica |-> r, qc |-> highQC[r]] }
func (c *Context[H]) AddNewView(view uint32, validatorIndex uint16, highQC *QC[H]) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.newviews[view] == nil {
		c.newviews[view] = make(map[uint16]*QC[H])
	}

	// Store the validator's highQC (may be nil)
	c.newviews[view][validatorIndex] = highQC
	return len(c.newviews[view])
}

// NewViewCount returns the number of NEWVIEW messages received for a view.
// TLA+ spec: Cardinality(NewViewReplicasFromNetwork(v))
func (c *Context[H]) NewViewCount(view uint32) int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.newviews[view] == nil {
		return 0
	}
	return len(c.newviews[view])
}

// HasNewViewQuorum returns true if 2f+1 NEWVIEW messages have been received for a view.
// TLA+ spec: IsQuorum(NewViewReplicasFromNetwork(v))
func (c *Context[H]) HasNewViewQuorum(view uint32, quorum int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.newviews[view] == nil {
		return false
	}
	return len(c.newviews[view]) >= quorum
}

// HighestQCFromNewViews returns the highest QC from all NEWVIEW messages in a view.
// TLA+ spec: HighestQCFromNetwork(v)
func (c *Context[H]) HighestQCFromNewViews(view uint32) *QC[H] {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.newviews[view] == nil {
		return nil
	}

	var highestQC *QC[H]
	for _, qc := range c.newviews[view] {
		if qc == nil {
			continue
		}
		if highestQC == nil || qc.View() > highestQC.View() {
			highestQC = qc
		}
	}

	return highestQC
}

// HasSentNewView returns true if this validator has already sent NEWVIEW for a view.
func (c *Context[H]) HasSentNewView(view uint32, validatorIndex uint16) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.newviews[view] == nil {
		return false
	}
	_, exists := c.newviews[view][validatorIndex]
	return exists
}

// PruneVotes removes old votes and newviews to free memory.
func (c *Context[H]) PruneVotes(currentView uint32, keepViews uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if currentView < keepViews {
		return
	}

	pruneBelow := currentView - keepViews

	for view := range c.votes {
		if view < pruneBelow {
			delete(c.votes, view)
		}
	}

	for view := range c.newviews {
		if view < pruneBelow {
			delete(c.newviews, view)
		}
	}
}

// Stats returns statistics about the context state.
func (c *Context[H]) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	totalVotes := 0
	for _, viewVotes := range c.votes {
		for _, blockVotes := range viewVotes {
			totalVotes += len(blockVotes)
		}
	}

	return map[string]interface{}{
		"view":            c.view,
		"locked_qc_view":  c.lockedQCView(),
		"high_qc_view":    c.highQCView(),
		"committed_count": len(c.committed),
		"blocks_count":    len(c.blocksByHash),
		"qcs_count":       len(c.qcsByNode),
		"total_votes":     totalVotes,
	}
}

func (c *Context[H]) lockedQCView() uint32 {
	if c.lockedQC == nil {
		return 0
	}
	return c.lockedQC.View()
}

func (c *Context[H]) highQCView() uint32 {
	if c.highQC == nil {
		return 0
	}
	return c.highQC.View()
}
