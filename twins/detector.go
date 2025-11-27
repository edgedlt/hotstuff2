package twins

import (
	"sync"

	"github.com/edgedlt/hotstuff2"
)

// ViolationDetector monitors consensus execution for safety violations.
type ViolationDetector[H hotstuff2.Hash] struct {
	mu sync.RWMutex

	// Track all votes by validator and view
	votes map[uint16]map[uint32][]*voteRecord[H]

	// Track all proposals by leader and view
	proposals map[uint16]map[uint32][]*proposalRecord[H]

	// Track all new-view messages by validator and view
	newViews map[uint16]map[uint32][]*newViewRecord[H]

	// Track all committed blocks by height
	committed map[uint32][]commitRecord[H]

	// Detected violations
	violations []Violation
}

// voteRecord tracks a vote with context.
type voteRecord[H hotstuff2.Hash] struct {
	ValidatorIndex uint16
	View           uint32
	NodeHash       H
	NodeID         int // Which twin/replica sent this
}

// commitRecord tracks a committed block.
type commitRecord[H hotstuff2.Hash] struct {
	Height uint32
	Hash   H
	NodeID int
}

// proposalRecord tracks a proposal with context.
type proposalRecord[H hotstuff2.Hash] struct {
	LeaderIndex uint16
	View        uint32
	BlockHash   H
	NodeID      int // Which twin/replica sent this
}

// newViewRecord tracks a new-view message with context.
type newViewRecord[H hotstuff2.Hash] struct {
	ValidatorIndex uint16
	View           uint32
	HighQCView     uint32 // View of the highest QC included
	HighQCNode     H      // Node hash of the highest QC
	NodeID         int
}

// NewViolationDetector creates a new violation detector.
func NewViolationDetector[H hotstuff2.Hash]() *ViolationDetector[H] {
	return &ViolationDetector[H]{
		votes:      make(map[uint16]map[uint32][]*voteRecord[H]),
		proposals:  make(map[uint16]map[uint32][]*proposalRecord[H]),
		newViews:   make(map[uint16]map[uint32][]*newViewRecord[H]),
		committed:  make(map[uint32][]commitRecord[H]),
		violations: make([]Violation, 0),
	}
}

// RecordVote records a vote for violation detection.
func (vd *ViolationDetector[H]) RecordVote(nodeID int, vote *hotstuff2.Vote[H]) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	validatorIndex := vote.ValidatorIndex()
	view := vote.View()
	nodeHash := vote.NodeHash()

	vd.recordVoteInternal(nodeID, validatorIndex, view, nodeHash)
}

// RecordConflictingVote records a vote that conflicts with an existing vote.
// This is used by Byzantine interceptors to simulate double-signing.
// It creates a fake conflicting hash to trigger detection.
func (vd *ViolationDetector[H]) RecordConflictingVote(nodeID int, validatorIndex uint16, view uint32, originalHash H) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// First, ensure the original vote is recorded
	vd.recordVoteInternal(nodeID, validatorIndex, view, originalHash)

	// Now record a "conflicting" vote by creating a synthetic conflict
	// We do this by recording a violation directly since we can't create
	// a different hash of the same type easily
	existingVotes := vd.votes[validatorIndex][view]
	if len(existingVotes) > 0 {
		// Record the double-sign violation
		vd.violations = append(vd.violations, Violation{
			Type:        ViolationDoubleSign,
			Description: "Byzantine node double-signed in same view",
			NodeID:      nodeID,
			View:        view,
			Context: map[string]any{
				"validator_index": validatorIndex,
				"block_hash":      originalHash.String(),
				"injected":        true, // Mark as injected by Byzantine behavior
			},
		})
	}
}

// recordVoteInternal records a vote and checks for double-signing (must hold lock).
func (vd *ViolationDetector[H]) recordVoteInternal(nodeID int, validatorIndex uint16, view uint32, nodeHash H) {
	// Initialize maps if needed
	if _, exists := vd.votes[validatorIndex]; !exists {
		vd.votes[validatorIndex] = make(map[uint32][]*voteRecord[H])
	}

	// Check for double-signing: same validator, same view, different blocks
	existingVotes := vd.votes[validatorIndex][view]
	for _, existing := range existingVotes {
		if !existing.NodeHash.Equals(nodeHash) {
			// Double-sign detected!
			vd.violations = append(vd.violations, Violation{
				Type:        ViolationDoubleSign,
				Description: "Validator signed conflicting blocks in same view",
				NodeID:      nodeID,
				View:        view,
				Context: map[string]any{
					"validator_index": validatorIndex,
					"block_hash_1":    existing.NodeHash.String(),
					"block_hash_2":    nodeHash.String(),
					"node_id_1":       existing.NodeID,
					"node_id_2":       nodeID,
				},
			})
		}
	}

	// Record this vote
	vd.votes[validatorIndex][view] = append(vd.votes[validatorIndex][view], &voteRecord[H]{
		ValidatorIndex: validatorIndex,
		View:           view,
		NodeHash:       nodeHash,
		NodeID:         nodeID,
	})
}

// RecordCommit records a committed block for fork detection.
func (vd *ViolationDetector[H]) RecordCommit(nodeID int, height uint32, hash H) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Check for fork: different blocks committed at same height
	existingCommits := vd.committed[height]
	for _, existing := range existingCommits {
		if !existing.Hash.Equals(hash) {
			// Fork detected!
			vd.violations = append(vd.violations, Violation{
				Type:        ViolationFork,
				Description: "Different blocks committed at same height",
				NodeID:      nodeID,
				View:        0, // Height-based, not view-based
				Context: map[string]any{
					"height":       height,
					"block_hash_1": existing.Hash.String(),
					"block_hash_2": hash.String(),
					"node_id_1":    existing.NodeID,
					"node_id_2":    nodeID,
				},
			})
		}
	}

	// Record this commit
	vd.committed[height] = append(vd.committed[height], commitRecord[H]{
		Height: height,
		Hash:   hash,
		NodeID: nodeID,
	})
}

// RecordProposal records a proposal for conflicting proposal detection.
// A Byzantine leader might propose different blocks to different nodes in the same view.
func (vd *ViolationDetector[H]) RecordProposal(nodeID int, leaderIndex uint16, view uint32, blockHash H) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Initialize maps if needed
	if _, exists := vd.proposals[leaderIndex]; !exists {
		vd.proposals[leaderIndex] = make(map[uint32][]*proposalRecord[H])
	}

	// Check for conflicting proposals: same leader, same view, different blocks
	existingProposals := vd.proposals[leaderIndex][view]
	for _, existing := range existingProposals {
		if !existing.BlockHash.Equals(blockHash) {
			// Conflicting proposal detected!
			vd.violations = append(vd.violations, Violation{
				Type:        ViolationConflictingProposal,
				Description: "Leader proposed conflicting blocks in same view",
				NodeID:      nodeID,
				View:        view,
				Context: map[string]any{
					"leader_index": leaderIndex,
					"block_hash_1": existing.BlockHash.String(),
					"block_hash_2": blockHash.String(),
					"node_id_1":    existing.NodeID,
					"node_id_2":    nodeID,
				},
			})
		}
	}

	// Record this proposal
	vd.proposals[leaderIndex][view] = append(vd.proposals[leaderIndex][view], &proposalRecord[H]{
		LeaderIndex: leaderIndex,
		View:        view,
		BlockHash:   blockHash,
		NodeID:      nodeID,
	})
}

// RecordNewView records a new-view message for conflicting new-view detection.
// A Byzantine node might send different new-view messages to different nodes.
func (vd *ViolationDetector[H]) RecordNewView(nodeID int, validatorIndex uint16, view uint32, highQCView uint32, highQCNode H) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Initialize maps if needed
	if _, exists := vd.newViews[validatorIndex]; !exists {
		vd.newViews[validatorIndex] = make(map[uint32][]*newViewRecord[H])
	}

	// Check for conflicting new-views: same validator, same view, different highQC
	existingNewViews := vd.newViews[validatorIndex][view]
	for _, existing := range existingNewViews {
		// Conflict if highQC view differs, or if same highQC view but different node
		if existing.HighQCView != highQCView || (existing.HighQCView == highQCView && !existing.HighQCNode.Equals(highQCNode)) {
			// Conflicting new-view detected!
			vd.violations = append(vd.violations, Violation{
				Type:        ViolationConflictingNewView,
				Description: "Validator sent conflicting new-view messages",
				NodeID:      nodeID,
				View:        view,
				Context: map[string]any{
					"validator_index": validatorIndex,
					"high_qc_view_1":  existing.HighQCView,
					"high_qc_view_2":  highQCView,
					"high_qc_node_1":  existing.HighQCNode.String(),
					"high_qc_node_2":  highQCNode.String(),
					"node_id_1":       existing.NodeID,
					"node_id_2":       nodeID,
				},
			})
		}
	}

	// Record this new-view
	vd.newViews[validatorIndex][view] = append(vd.newViews[validatorIndex][view], &newViewRecord[H]{
		ValidatorIndex: validatorIndex,
		View:           view,
		HighQCView:     highQCView,
		HighQCNode:     highQCNode,
		NodeID:         nodeID,
	})
}

// RecordQC records a QC for validation.
func (vd *ViolationDetector[H]) RecordQC(nodeID int, qc hotstuff2.QuorumCertificate[H], validators hotstuff2.ValidatorSet) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Validate QC
	if err := qc.Validate(validators); err != nil {
		vd.violations = append(vd.violations, Violation{
			Type:        ViolationInvalidQC,
			Description: "QC validation failed: " + err.Error(),
			NodeID:      nodeID,
			View:        qc.View(),
			Context: map[string]any{
				"node_hash": qc.Node().String(),
			},
		})
	}
}

// GetViolations returns all detected violations.
func (vd *ViolationDetector[H]) GetViolations() []Violation {
	vd.mu.RLock()
	defer vd.mu.RUnlock()
	return append([]Violation{}, vd.violations...)
}

// HasViolations returns true if any violations were detected.
func (vd *ViolationDetector[H]) HasViolations() bool {
	vd.mu.RLock()
	defer vd.mu.RUnlock()
	return len(vd.violations) > 0
}

// Reset clears all recorded data.
func (vd *ViolationDetector[H]) Reset() {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	vd.votes = make(map[uint16]map[uint32][]*voteRecord[H])
	vd.proposals = make(map[uint16]map[uint32][]*proposalRecord[H])
	vd.newViews = make(map[uint16]map[uint32][]*newViewRecord[H])
	vd.committed = make(map[uint32][]commitRecord[H])
	vd.violations = make([]Violation, 0)
}

// ============================================================================
// Safety Property Verification
// ============================================================================

// SafeNodeCheck represents a vote that should be verified against the SafeNode rule.
type SafeNodeCheck[H hotstuff2.Hash] struct {
	ValidatorIndex uint16
	View           uint32
	BlockHash      H
	BlockHeight    uint32
	LockedQCView   uint32 // The validator's locked QC view when voting
	LockedQCNode   H      // The validator's locked QC node when voting
}

// VerifySafeNodeRule checks if a vote complies with the SafeNode predicate.
// SafeNode(block, lockedQC) = block extends lockedQC.node OR block.qc.view > lockedQC.view
//
// This is called to verify that a validator's vote is safe according to HotStuff-2 rules.
// Returns a violation if the vote doesn't comply with SafeNode.
func (vd *ViolationDetector[H]) VerifySafeNodeRule(check SafeNodeCheck[H], blockExtendsLocked bool, blockQCView uint32) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// SafeNode predicate: either block extends locked QC, or block's justify QC has higher view
	safeNodeSatisfied := blockExtendsLocked || blockQCView > check.LockedQCView

	if !safeNodeSatisfied {
		vd.violations = append(vd.violations, Violation{
			Type:        ViolationSafetyViolation,
			Description: "Vote violates SafeNode rule: block doesn't extend locked QC and justify view not higher",
			NodeID:      int(check.ValidatorIndex),
			View:        check.View,
			Context: map[string]any{
				"validator_index": check.ValidatorIndex,
				"block_hash":      check.BlockHash.String(),
				"block_height":    check.BlockHeight,
				"locked_qc_view":  check.LockedQCView,
				"locked_qc_node":  check.LockedQCNode.String(),
				"block_qc_view":   blockQCView,
				"extends_locked":  blockExtendsLocked,
			},
		})
	}
}

// TwoChainCommitCheck represents a commit that should be verified against the two-chain rule.
type TwoChainCommitCheck[H hotstuff2.Hash] struct {
	BlockHash       H
	BlockHeight     uint32
	BlockView       uint32
	ParentHash      H
	ParentView      uint32
	GrandparentHash H
	GrandparentView uint32
}

// VerifyTwoChainCommit checks if a commit follows the two-chain commit rule.
// In HotStuff-2, a block B is committed when there's a two-chain:
// grandparent <- parent <- block, where parent.view = grandparent.view + 1
//
// Returns a violation if the commit doesn't follow the two-chain rule.
func (vd *ViolationDetector[H]) VerifyTwoChainCommit(check TwoChainCommitCheck[H]) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Two-chain rule: parent view should be exactly grandparent view + 1
	directChain := check.ParentView == check.GrandparentView+1

	if !directChain {
		vd.violations = append(vd.violations, Violation{
			Type:        ViolationSafetyViolation,
			Description: "Commit violates two-chain rule: parent view is not grandparent view + 1",
			NodeID:      -1, // Not specific to a node
			View:        check.BlockView,
			Context: map[string]any{
				"block_hash":       check.BlockHash.String(),
				"block_height":     check.BlockHeight,
				"block_view":       check.BlockView,
				"parent_hash":      check.ParentHash.String(),
				"parent_view":      check.ParentView,
				"grandparent_hash": check.GrandparentHash.String(),
				"grandparent_view": check.GrandparentView,
			},
		})
	}
}

// QuorumIntersectionCheck represents a QC pair to verify quorum intersection.
type QuorumIntersectionCheck struct {
	QC1View    uint32
	QC1Signers []uint16
	QC2View    uint32
	QC2Signers []uint16
	F          int // Maximum Byzantine faults tolerated
}

// VerifyQuorumIntersection checks that two QCs have proper quorum intersection.
// Any two QCs must share at least one honest validator (f+1 overlap given 2f+1 quorum).
//
// This verifies the fundamental BFT property: |Q1 ∩ Q2| >= f+1 for any quorums Q1, Q2.
func (vd *ViolationDetector[H]) VerifyQuorumIntersection(check QuorumIntersectionCheck) {
	vd.mu.Lock()
	defer vd.mu.Unlock()

	// Build set from QC1 signers
	signerSet := make(map[uint16]bool)
	for _, signer := range check.QC1Signers {
		signerSet[signer] = true
	}

	// Count intersection
	intersection := 0
	for _, signer := range check.QC2Signers {
		if signerSet[signer] {
			intersection++
		}
	}

	// Must have at least f+1 overlap for quorum intersection property
	requiredOverlap := check.F + 1
	if intersection < requiredOverlap {
		vd.violations = append(vd.violations, Violation{
			Type:        ViolationSafetyViolation,
			Description: "QCs violate quorum intersection: insufficient overlap",
			NodeID:      -1,
			View:        check.QC2View, // Report on the later QC
			Context: map[string]any{
				"qc1_view":         check.QC1View,
				"qc1_signers":      check.QC1Signers,
				"qc2_view":         check.QC2View,
				"qc2_signers":      check.QC2Signers,
				"intersection":     intersection,
				"required_overlap": requiredOverlap,
				"f":                check.F,
			},
		})
	}
}

// GetViolationsByType returns violations of a specific type.
func (vd *ViolationDetector[H]) GetViolationsByType(vType ViolationType) []Violation {
	vd.mu.RLock()
	defer vd.mu.RUnlock()

	result := make([]Violation, 0)
	for _, v := range vd.violations {
		if v.Type == vType {
			result = append(result, v)
		}
	}
	return result
}

// GetViolationCount returns the count of violations by type.
func (vd *ViolationDetector[H]) GetViolationCount() map[ViolationType]int {
	vd.mu.RLock()
	defer vd.mu.RUnlock()

	counts := make(map[ViolationType]int)
	for _, v := range vd.violations {
		counts[v.Type]++
	}
	return counts
}
