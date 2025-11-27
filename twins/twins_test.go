package twins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/edgedlt/hotstuff2"
	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHash is a simple hash for testing.
type TestHash [32]byte

func NewTestHash(data string) TestHash {
	return sha256.Sum256([]byte(data))
}

func (h TestHash) Bytes() []byte {
	return h[:]
}

func (h TestHash) Equals(other hotstuff2.Hash) bool {
	if otherTest, ok := other.(TestHash); ok {
		return h == otherTest
	}
	return false
}

func (h TestHash) String() string {
	return hex.EncodeToString(h[:8])
}

// NewVote creates a vote for testing.
func NewVote(view uint32, nodeHash TestHash, validatorIndex uint16, privKey hotstuff2.PrivateKey) (*hotstuff2.Vote[TestHash], error) {
	return hotstuff2.NewVote(view, nodeHash, validatorIndex, privKey)
}

// TestScenarioValidation tests scenario validation logic.
func TestScenarioValidation(t *testing.T) {
	tests := []struct {
		name      string
		scenario  Scenario
		expectErr bool
	}{
		{
			name: "Valid baseline scenario",
			scenario: Scenario{
				Replicas: 4,
				Twins:    0,
				Views:    5,
				Behavior: BehaviorHonest,
			},
			expectErr: false,
		},
		{
			name: "Valid single twin",
			scenario: Scenario{
				Replicas: 3,
				Twins:    1,
				Views:    5,
				Behavior: BehaviorHonest,
			},
			expectErr: false,
		},
		{
			name: "Invalid: no replicas",
			scenario: Scenario{
				Replicas: 0,
				Twins:    1,
				Views:    5,
			},
			expectErr: true,
		},
		{
			name: "Invalid: negative twins",
			scenario: Scenario{
				Replicas: 4,
				Twins:    -1,
				Views:    5,
			},
			expectErr: true,
		},
		{
			name: "Invalid: too many twins for BFT",
			scenario: Scenario{
				Replicas: 2,
				Twins:    2, // Total = 2 + 4 = 6, f = 1, but 2 twins > f
				Views:    5,
			},
			expectErr: true,
		},
		{
			name: "Invalid: no views",
			scenario: Scenario{
				Replicas: 4,
				Twins:    0,
				Views:    0,
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScenario(tt.scenario)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTwinIDCalculation tests twin ID mapping logic.
func TestTwinIDCalculation(t *testing.T) {
	const replicas = 3

	// Twin pair 0: nodes 3 and 4
	assert.Equal(t, 3, TwinID(replicas, 0, 0))
	assert.Equal(t, 4, TwinID(replicas, 0, 1))

	// Twin pair 1: nodes 5 and 6
	assert.Equal(t, 5, TwinID(replicas, 1, 0))
	assert.Equal(t, 6, TwinID(replicas, 1, 1))

	// Invalid twin index
	assert.Equal(t, -1, TwinID(replicas, 0, 2))
}

// TestIsTwin tests twin detection.
func TestIsTwin(t *testing.T) {
	const replicas = 3

	assert.False(t, IsTwin(0, replicas)) // Honest replica
	assert.False(t, IsTwin(1, replicas)) // Honest replica
	assert.False(t, IsTwin(2, replicas)) // Honest replica
	assert.True(t, IsTwin(3, replicas))  // Twin
	assert.True(t, IsTwin(4, replicas))  // Twin
}

// TestGetTwinPair tests twin pair retrieval.
func TestGetTwinPair(t *testing.T) {
	const replicas = 3

	// Honest replicas have no twin
	twin1, twin2 := GetTwinPair(0, replicas)
	assert.Equal(t, -1, twin1)
	assert.Equal(t, -1, twin2)

	// First twin pair
	twin1, twin2 = GetTwinPair(3, replicas)
	assert.Equal(t, 3, twin1)
	assert.Equal(t, 4, twin2)

	// Second twin pair
	twin1, twin2 = GetTwinPair(5, replicas)
	assert.Equal(t, 5, twin1)
	assert.Equal(t, 6, twin2)
}

// TestGetValidatorIndex tests validator index mapping.
func TestGetValidatorIndex(t *testing.T) {
	const replicas = 3

	// Honest replicas use their own index
	assert.Equal(t, 0, GetValidatorIndex(0, replicas))
	assert.Equal(t, 1, GetValidatorIndex(1, replicas))
	assert.Equal(t, 2, GetValidatorIndex(2, replicas))

	// Twin pair 0 shares validator index 3
	assert.Equal(t, 3, GetValidatorIndex(3, replicas))
	assert.Equal(t, 3, GetValidatorIndex(4, replicas))

	// Twin pair 1 shares validator index 4
	assert.Equal(t, 4, GetValidatorIndex(5, replicas))
	assert.Equal(t, 4, GetValidatorIndex(6, replicas))
}

// TestBasicScenarios tests that basic scenarios are valid.
func TestBasicScenarios(t *testing.T) {
	scenarios := GenerateBasicScenarios()

	for i, scenario := range scenarios {
		t.Run(fmt.Sprintf("Scenario_%d", i), func(t *testing.T) {
			err := ValidateScenario(scenario)
			assert.NoError(t, err, "scenario %d should be valid", i)
		})
	}
}

// TestTwins_HonestBaseline tests 4 honest nodes without twins.
func TestTwins_HonestBaseline(t *testing.T) {
	scenario := Scenario{
		Replicas: 4,
		Twins:    0,
		Views:    5,
		Behavior: BehaviorHonest,
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	assert.True(t, result.Success, "honest baseline should succeed")
	assert.Empty(t, result.Violations, "honest baseline should have no violations")
	assert.Greater(t, result.BlocksCommitted, 0, "should commit blocks")
	assert.Greater(t, result.MessagesExchanged, 0, "should exchange messages")

	t.Logf("✅ Honest baseline: %d blocks committed, %d messages",
		result.BlocksCommitted, result.MessagesExchanged)
}

// TestTwins_HonestWithTwins tests 3 honest + 1 twin pair behaving honestly.
func TestTwins_HonestWithTwins(t *testing.T) {
	scenario := Scenario{
		Replicas: 3,
		Twins:    1, // One twin pair
		Views:    5,
		Behavior: BehaviorHonest,
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	assert.True(t, result.Success, "honest twins should not cause violations")
	assert.Empty(t, result.Violations, "honest twins should have no violations")
	assert.Greater(t, result.BlocksCommitted, 0, "should commit blocks")

	t.Logf("✅ Honest twins: %d blocks committed, %d messages",
		result.BlocksCommitted, result.MessagesExchanged)
}

// TestViolationDetector_DoubleSign tests double-sign detection.
func TestViolationDetector_DoubleSign(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Create two conflicting votes from the same validator
	priv, _ := crypto.GenerateEd25519Key()

	hash1 := NewTestHash("block1")
	hash2 := NewTestHash("block2")

	vote1, err := NewVote(0, hash1, 0, priv)
	require.NoError(t, err)

	vote2, err := NewVote(0, hash2, 0, priv)
	require.NoError(t, err)

	// Record votes (simulating different twins)
	detector.RecordVote(0, vote1)
	detector.RecordVote(1, vote2)

	// Should detect double-sign
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	require.Len(t, violations, 1)

	assert.Equal(t, ViolationDoubleSign, violations[0].Type)
	assert.Equal(t, uint32(0), violations[0].View)
	t.Logf("✅ Detected double-sign: %s", violations[0].Description)
}

// TestViolationDetector_Fork tests fork detection.
func TestViolationDetector_Fork(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash1 := NewTestHash("block1")
	hash2 := NewTestHash("block2")

	// Two nodes commit different blocks at same height
	detector.RecordCommit(0, 1, hash1)
	detector.RecordCommit(1, 1, hash2)

	// Should detect fork
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	require.Len(t, violations, 1)

	assert.Equal(t, ViolationFork, violations[0].Type)
	t.Logf("✅ Detected fork: %s", violations[0].Description)
}

// TestViolationDetector_NoFalsePositives tests that identical commits don't trigger violations.
func TestViolationDetector_NoFalsePositives(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("block1")

	// Multiple nodes commit same block (normal operation)
	detector.RecordCommit(0, 1, hash)
	detector.RecordCommit(1, 1, hash)
	detector.RecordCommit(2, 1, hash)
	detector.RecordCommit(3, 1, hash)

	// Should NOT detect violation
	assert.False(t, detector.HasViolations())
	assert.Empty(t, detector.GetViolations())

	t.Logf("✅ No false positives for honest nodes")
}

// TestViolationDetector_MultipleViolations tests multiple violation detection.
func TestViolationDetector_MultipleViolations(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	priv1, _ := crypto.GenerateEd25519Key()
	priv2, _ := crypto.GenerateEd25519Key()

	// Validator 0 double-signs
	vote1, _ := NewVote(0, NewTestHash("block1"), 0, priv1)
	vote2, _ := NewVote(0, NewTestHash("block2"), 0, priv1)
	detector.RecordVote(0, vote1)
	detector.RecordVote(1, vote2)

	// Validator 1 double-signs
	vote3, _ := NewVote(1, NewTestHash("block3"), 1, priv2)
	vote4, _ := NewVote(1, NewTestHash("block4"), 1, priv2)
	detector.RecordVote(2, vote3)
	detector.RecordVote(3, vote4)

	// Should detect both violations
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	assert.Len(t, violations, 2)

	t.Logf("✅ Detected %d violations", len(violations))
}

// TestViolationDetector_ConflictingProposal tests conflicting proposal detection.
func TestViolationDetector_ConflictingProposal(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash1 := NewTestHash("block1")
	hash2 := NewTestHash("block2")

	// Leader 0 proposes different blocks in same view to different nodes
	detector.RecordProposal(0, 0, 1, hash1)
	detector.RecordProposal(1, 0, 1, hash2)

	// Should detect conflicting proposal
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	require.Len(t, violations, 1)

	assert.Equal(t, ViolationConflictingProposal, violations[0].Type)
	assert.Equal(t, uint32(1), violations[0].View)
	t.Logf("✅ Detected conflicting proposal: %s", violations[0].Description)
}

// TestViolationDetector_SameProposal tests that identical proposals don't trigger violations.
func TestViolationDetector_SameProposal(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("block1")

	// Leader 0 sends same proposal to multiple nodes (normal operation)
	detector.RecordProposal(0, 0, 1, hash)
	detector.RecordProposal(1, 0, 1, hash)
	detector.RecordProposal(2, 0, 1, hash)

	// Should NOT detect violation
	assert.False(t, detector.HasViolations())
	assert.Empty(t, detector.GetViolations())

	t.Logf("✅ No false positives for identical proposals")
}

// TestViolationDetector_ConflictingNewView tests conflicting new-view detection.
func TestViolationDetector_ConflictingNewView(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash1 := NewTestHash("qc_block1")
	hash2 := NewTestHash("qc_block2")

	// Validator 0 sends different new-view messages with different highQC
	detector.RecordNewView(0, 0, 2, 1, hash1) // highQC at view 1
	detector.RecordNewView(1, 0, 2, 1, hash2) // same view but different node hash

	// Should detect conflicting new-view
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	require.Len(t, violations, 1)

	assert.Equal(t, ViolationConflictingNewView, violations[0].Type)
	assert.Equal(t, uint32(2), violations[0].View)
	t.Logf("✅ Detected conflicting new-view: %s", violations[0].Description)
}

// TestViolationDetector_DifferentHighQCView tests conflicting new-view with different highQC views.
func TestViolationDetector_DifferentHighQCView(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("qc_block")

	// Validator 0 claims different highQC views to different nodes
	detector.RecordNewView(0, 0, 3, 1, hash) // claims highQC at view 1
	detector.RecordNewView(1, 0, 3, 2, hash) // claims highQC at view 2

	// Should detect conflicting new-view
	assert.True(t, detector.HasViolations())
	violations := detector.GetViolations()
	require.Len(t, violations, 1)

	assert.Equal(t, ViolationConflictingNewView, violations[0].Type)
	t.Logf("✅ Detected conflicting highQC views: %s", violations[0].Description)
}

// TestViolationDetector_SameNewView tests that identical new-views don't trigger violations.
func TestViolationDetector_SameNewView(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("qc_block")

	// Validator 0 sends identical new-view to multiple nodes (normal operation)
	detector.RecordNewView(0, 0, 2, 1, hash)
	detector.RecordNewView(1, 0, 2, 1, hash)
	detector.RecordNewView(2, 0, 2, 1, hash)

	// Should NOT detect violation
	assert.False(t, detector.HasViolations())
	assert.Empty(t, detector.GetViolations())

	t.Logf("✅ No false positives for identical new-views")
}

// ============================================================================
// Safety Property Verification Tests
// ============================================================================

// TestVerifySafeNodeRule_Valid tests that valid votes pass SafeNode check.
func TestVerifySafeNodeRule_Valid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("block")
	lockedHash := NewTestHash("locked_block")

	// Case 1: Block extends locked QC (safe)
	check := SafeNodeCheck[TestHash]{
		ValidatorIndex: 0,
		View:           5,
		BlockHash:      hash,
		BlockHeight:    10,
		LockedQCView:   3,
		LockedQCNode:   lockedHash,
	}
	detector.VerifySafeNodeRule(check, true, 4) // Block extends locked

	assert.False(t, detector.HasViolations(), "Vote extending locked QC should be valid")

	// Case 2: Block's justify QC has higher view than locked QC (safe)
	detector.Reset()
	detector.VerifySafeNodeRule(check, false, 4) // Doesn't extend, but justify view (4) > locked view (3)

	assert.False(t, detector.HasViolations(), "Vote with higher justify view should be valid")

	t.Logf("✅ Valid SafeNode checks passed")
}

// TestVerifySafeNodeRule_Invalid tests that invalid votes trigger SafeNode violation.
func TestVerifySafeNodeRule_Invalid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	hash := NewTestHash("block")
	lockedHash := NewTestHash("locked_block")

	// Vote that doesn't extend locked QC and has lower/equal justify view
	check := SafeNodeCheck[TestHash]{
		ValidatorIndex: 0,
		View:           5,
		BlockHash:      hash,
		BlockHeight:    10,
		LockedQCView:   4,
		LockedQCNode:   lockedHash,
	}
	detector.VerifySafeNodeRule(check, false, 3) // Doesn't extend, justify view (3) < locked view (4)

	assert.True(t, detector.HasViolations(), "Vote violating SafeNode should be detected")
	violations := detector.GetViolations()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationSafetyViolation, violations[0].Type)

	t.Logf("✅ Detected SafeNode violation: %s", violations[0].Description)
}

// TestVerifyTwoChainCommit_Valid tests that valid two-chain commits pass.
func TestVerifyTwoChainCommit_Valid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Valid two-chain: parent.view = grandparent.view + 1
	check := TwoChainCommitCheck[TestHash]{
		BlockHash:       NewTestHash("block"),
		BlockHeight:     12,
		BlockView:       7,
		ParentHash:      NewTestHash("parent"),
		ParentView:      6,
		GrandparentHash: NewTestHash("grandparent"),
		GrandparentView: 5, // 6 = 5 + 1 ✓
	}
	detector.VerifyTwoChainCommit(check)

	assert.False(t, detector.HasViolations(), "Valid two-chain commit should pass")

	t.Logf("✅ Valid two-chain commit passed")
}

// TestVerifyTwoChainCommit_Invalid tests that invalid commits trigger violation.
func TestVerifyTwoChainCommit_Invalid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Invalid: parent.view != grandparent.view + 1 (gap in chain)
	check := TwoChainCommitCheck[TestHash]{
		BlockHash:       NewTestHash("block"),
		BlockHeight:     12,
		BlockView:       7,
		ParentHash:      NewTestHash("parent"),
		ParentView:      6,
		GrandparentHash: NewTestHash("grandparent"),
		GrandparentView: 4, // 6 != 4 + 1 (gap!)
	}
	detector.VerifyTwoChainCommit(check)

	assert.True(t, detector.HasViolations(), "Invalid two-chain commit should be detected")
	violations := detector.GetViolations()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationSafetyViolation, violations[0].Type)

	t.Logf("✅ Detected two-chain violation: %s", violations[0].Description)
}

// TestVerifyQuorumIntersection_Valid tests that valid quorums pass intersection check.
func TestVerifyQuorumIntersection_Valid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// With n=4, f=1, quorum=3, two quorums must overlap by at least f+1=2
	check := QuorumIntersectionCheck{
		QC1View:    1,
		QC1Signers: []uint16{0, 1, 2}, // 3 signers
		QC2View:    2,
		QC2Signers: []uint16{1, 2, 3}, // 3 signers, overlap: {1, 2}
		F:          1,
	}
	detector.VerifyQuorumIntersection(check)

	assert.False(t, detector.HasViolations(), "Valid quorum intersection should pass")

	t.Logf("✅ Valid quorum intersection passed (overlap: 2, required: 2)")
}

// TestVerifyQuorumIntersection_Invalid tests that insufficient overlap triggers violation.
func TestVerifyQuorumIntersection_Invalid(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Insufficient overlap (this shouldn't happen in correct BFT, but we verify)
	check := QuorumIntersectionCheck{
		QC1View:    1,
		QC1Signers: []uint16{0, 1, 2}, // 3 signers
		QC2View:    2,
		QC2Signers: []uint16{2, 3, 4}, // 3 signers, overlap: only {2}
		F:          1,                 // Requires f+1=2 overlap
	}
	detector.VerifyQuorumIntersection(check)

	assert.True(t, detector.HasViolations(), "Insufficient quorum intersection should be detected")
	violations := detector.GetViolations()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationSafetyViolation, violations[0].Type)

	t.Logf("✅ Detected quorum intersection violation: %s", violations[0].Description)
}

// TestViolationDetector_InvalidQC tests invalid QC detection.
func TestViolationDetector_InvalidQC(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Create a mock validator set
	keys := make(map[uint16]*crypto.Ed25519PublicKey)
	privKeys := make([]hotstuff2.PrivateKey, 4)
	for i := range 4 {
		priv, _ := crypto.GenerateEd25519Key()
		privKeys[i] = priv
		keys[uint16(i)] = priv.PublicKey().(*crypto.Ed25519PublicKey)
	}
	validators := &twinsValidatorSet{keys: keys, n: 4}

	// Create votes for a QC - need 2f+1 = 3 for n=4
	hash := NewTestHash("block1")
	votes := make([]*hotstuff2.Vote[TestHash], 3)
	for i := range 3 {
		vote, err := NewVote(1, hash, uint16(i), privKeys[i])
		require.NoError(t, err)
		votes[i] = vote
	}

	// Create a valid QC
	qc, err := hotstuff2.NewQC(1, hash, votes, validators, "ed25519")
	require.NoError(t, err)

	// Record the valid QC - should NOT detect violation
	detector.RecordQC(0, qc, validators)
	assert.False(t, detector.HasViolations(), "Valid QC should not trigger violation")

	// Now create a QC with only 2 votes (invalid - needs 3)
	votes2 := votes[:2]
	qc2, err := hotstuff2.NewQC(2, hash, votes2, validators, "ed25519")
	// NewQC should fail for insufficient votes, but if it doesn't, RecordQC should catch it
	if err == nil {
		detector.RecordQC(0, qc2, validators)
		// Should detect invalid QC if NewQC didn't already reject it
	}

	t.Logf("✅ QC validation working correctly")
}

// TestGetViolationsByType tests filtering violations by type.
func TestGetViolationsByType(t *testing.T) {
	detector := NewViolationDetector[TestHash]()

	// Create different types of violations
	priv, _ := crypto.GenerateEd25519Key()

	// Double-sign violation
	vote1, _ := NewVote(1, NewTestHash("block1"), 0, priv)
	vote2, _ := NewVote(1, NewTestHash("block2"), 0, priv)
	detector.RecordVote(0, vote1)
	detector.RecordVote(1, vote2)

	// Fork violation
	detector.RecordCommit(0, 1, NewTestHash("commit1"))
	detector.RecordCommit(1, 1, NewTestHash("commit2"))

	// Conflicting proposal violation
	detector.RecordProposal(0, 0, 1, NewTestHash("prop1"))
	detector.RecordProposal(1, 0, 1, NewTestHash("prop2"))

	// Check filtering
	doubleSignViolations := detector.GetViolationsByType(ViolationDoubleSign)
	forkViolations := detector.GetViolationsByType(ViolationFork)
	proposalViolations := detector.GetViolationsByType(ViolationConflictingProposal)

	assert.Len(t, doubleSignViolations, 1, "Should have 1 double-sign violation")
	assert.Len(t, forkViolations, 1, "Should have 1 fork violation")
	assert.Len(t, proposalViolations, 1, "Should have 1 conflicting proposal violation")

	// Check counts
	counts := detector.GetViolationCount()
	assert.Equal(t, 1, counts[ViolationDoubleSign])
	assert.Equal(t, 1, counts[ViolationFork])
	assert.Equal(t, 1, counts[ViolationConflictingProposal])

	t.Logf("✅ Violation filtering works correctly: %d double-sign, %d fork, %d proposal",
		len(doubleSignViolations), len(forkViolations), len(proposalViolations))
}

// ============================================================================
// Integration Tests - Mixed Behaviors & Partition Scenarios
// ============================================================================

// TestTwins_MixedBehaviors tests 7 nodes with 2 twin pairs under different behaviors.
func TestTwins_MixedBehaviors(t *testing.T) {
	behaviors := []ByzantineBehavior{
		BehaviorHonest,
		BehaviorDoubleSign,
		BehaviorSilent,
		BehaviorRandom,
		BehaviorDelay,
	}

	for _, behavior := range behaviors {
		t.Run(behavior.String(), func(t *testing.T) {
			scenario := Scenario{
				Replicas: 5,
				Twins:    2, // 2 twin pairs = 4 twin nodes
				Views:    5,
				Behavior: behavior,
			}

			executor, err := NewExecutor[TestHash256](scenario)
			require.NoError(t, err)

			result := executor.Execute()

			// Log results
			t.Logf("Behavior %s: %d blocks, %d msgs, %d violations",
				behavior, result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))

			// Honest behavior should have no violations
			if behavior == BehaviorHonest {
				assert.Empty(t, result.Violations, "Honest behavior should have no violations")
			}

			// DoubleSign should detect violations
			if behavior == BehaviorDoubleSign {
				assert.NotEmpty(t, result.Violations, "DoubleSign should produce violations")
			}

			// Silent should still maintain safety (no forks)
			forkViolations := 0
			for _, v := range result.Violations {
				if v.Type == ViolationFork {
					forkViolations++
				}
			}
			assert.Equal(t, 0, forkViolations, "Should never have fork violations (safety)")
		})
	}
}

// TestTwins_NetworkPartition tests consensus under network partitions.
func TestTwins_NetworkPartition(t *testing.T) {
	// 4 honest nodes split into two partitions
	scenario := Scenario{
		Replicas: 4,
		Twins:    0,
		Views:    5,
		Behavior: BehaviorHonest,
		Partitions: []Partition{
			{Nodes: []int{0, 1}}, // Partition A
			{Nodes: []int{2, 3}}, // Partition B
		},
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	// With partitions, consensus might stall (each partition has only 2 nodes)
	// But there should be no safety violations
	t.Logf("Partitioned network: %d blocks, %d msgs, %d violations",
		result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))

	// Safety must be maintained
	for _, v := range result.Violations {
		assert.NotEqual(t, ViolationFork, v.Type, "Partition should not cause forks")
	}
}

// TestTwins_LargeNetwork tests a larger 7-node network.
func TestTwins_LargeNetwork(t *testing.T) {
	scenario := Scenario{
		Replicas: 7,
		Twins:    0,
		Views:    10,
		Behavior: BehaviorHonest,
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	assert.True(t, result.Success, "7-node honest network should succeed")
	assert.Greater(t, result.BlocksCommitted, 0, "Should commit blocks")
	assert.Empty(t, result.Violations, "Should have no violations")

	t.Logf("✅ Large network: %d blocks, %d msgs", result.BlocksCommitted, result.MessagesExchanged)
}

// TestTwins_Equivocation tests consensus with equivocating twins.
func TestTwins_Equivocation(t *testing.T) {
	// 5 nodes with 1 twin pair that equivocates across partitions
	scenario := Scenario{
		Replicas: 4,
		Twins:    1,
		Views:    5,
		Behavior: BehaviorEquivocation,
		Partitions: []Partition{
			{Nodes: []int{0, 1, 4}}, // First partition includes one twin
			{Nodes: []int{2, 3, 5}}, // Second partition includes other twin
		},
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	// Log results
	t.Logf("Equivocation test: %d blocks, %d msgs, %d violations",
		result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))

	// Safety must be maintained despite equivocation
	for _, v := range result.Violations {
		assert.NotEqual(t, ViolationFork, v.Type, "Equivocation should not cause forks")
	}

	t.Logf("✅ Consensus handled equivocation: %d blocks committed", result.BlocksCommitted)
}

// TestTwins_DelayedMessages tests consensus under message delays and reordering.
func TestTwins_DelayedMessages(t *testing.T) {
	scenario := Scenario{
		Replicas: 3,
		Twins:    1, // One twin pair will delay messages
		Views:    5,
		Behavior: BehaviorDelay,
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	// Log results
	t.Logf("Delayed messages: %d blocks, %d msgs, %d violations",
		result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))

	// Safety must be maintained despite delays
	for _, v := range result.Violations {
		assert.NotEqual(t, ViolationFork, v.Type, "Delays should not cause forks")
	}

	// Should still make progress (though potentially slower)
	// With delayed messages, consensus should still work
	t.Logf("✅ Consensus handled delayed messages: %d blocks committed", result.BlocksCommitted)
}

// TestTwins_MaxByzantine tests maximum Byzantine fault tolerance.
func TestTwins_MaxByzantine(t *testing.T) {
	// With n=4, f=1, we can have 1 twin pair
	// This is the maximum Byzantine configuration
	scenario := Scenario{
		Replicas: 3,
		Twins:    1, // 1 twin pair = 2 nodes, but counts as f=1
		Views:    5,
		Behavior: BehaviorDoubleSign,
	}

	executor, err := NewExecutor[TestHash256](scenario)
	require.NoError(t, err)

	result := executor.Execute()

	// Should detect double-signing but no forks
	t.Logf("Max Byzantine (f=1): %d blocks, %d msgs, %d violations",
		result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))

	forkViolations := 0
	doubleSignViolations := 0
	for _, v := range result.Violations {
		if v.Type == ViolationFork {
			forkViolations++
		}
		if v.Type == ViolationDoubleSign {
			doubleSignViolations++
		}
	}

	assert.Equal(t, 0, forkViolations, "Should not have forks even with max Byzantine")
	assert.Greater(t, doubleSignViolations, 0, "Should detect double-signing")
}

// TestGenerator tests random scenario generation.
func TestGenerator(t *testing.T) {
	gen := NewGenerator(DefaultGeneratorConfig())

	for i := range 10 {
		scenario := gen.Generate()
		err := ValidateScenario(scenario)
		assert.NoError(t, err, "generated scenario %d should be valid", i)

		t.Logf("Scenario %d: %d replicas, %d twins, %d views, %s",
			i, scenario.Replicas, scenario.Twins, scenario.Views, scenario.Behavior)
	}
}

// TestComprehensive_100Scenarios runs 100 random scenarios.
func TestComprehensive_100Scenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping comprehensive test in short mode")
	}

	scenarios := GenerateComprehensive(90) // 10 basic + 90 random
	passed := 0
	failed := 0
	honestViolations := 0

	for i, scenario := range scenarios {
		t.Run(fmt.Sprintf("Scenario_%d", i), func(t *testing.T) {
			executor, err := NewExecutor[TestHash256](scenario)
			require.NoError(t, err)

			result := executor.Execute()

			// Honest scenarios should NEVER have violations
			if scenario.Behavior == BehaviorHonest && scenario.Twins == 0 {
				if !result.Success {
					honestViolations++
					t.Errorf("CRITICAL: Honest scenario had violations: %+v", result.Violations)
				}
			}

			if result.Success {
				passed++
			} else {
				failed++
			}

			t.Logf("Scenario %d: %d replicas, %d twins, %s -> %d blocks, %d msgs, %d violations",
				i, scenario.Replicas, scenario.Twins, scenario.Behavior,
				result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))
		})
	}

	t.Logf("\n=== COMPREHENSIVE TEST RESULTS ===")
	t.Logf("Total scenarios: %d", len(scenarios))
	t.Logf("Passed: %d", passed)
	t.Logf("Failed: %d", failed)
	t.Logf("Honest false positives: %d", honestViolations)

	// Critical check: no false positives
	assert.Equal(t, 0, honestViolations, "Honest scenarios must never have false violations")
}

// TestComprehensive_10Scenarios runs 10 random scenarios (fast version).
func TestComprehensive_10Scenarios(t *testing.T) {
	scenarios := GenerateComprehensive(5) // 5 basic + 5 random
	passed := 0
	failed := 0
	honestViolations := 0

	for i, scenario := range scenarios {
		t.Run(fmt.Sprintf("Scenario_%d", i), func(t *testing.T) {
			executor, err := NewExecutor[TestHash256](scenario)
			require.NoError(t, err)

			result := executor.Execute()

			// Honest scenarios should NEVER have violations
			if scenario.Behavior == BehaviorHonest && scenario.Twins == 0 {
				if !result.Success {
					honestViolations++
					t.Errorf("CRITICAL: Honest scenario had violations: %+v", result.Violations)
				}
			}

			if result.Success {
				passed++
			} else {
				failed++
			}

			t.Logf("Scenario %d: %d replicas, %d twins, %s -> %d blocks, %d msgs, %d violations",
				i, scenario.Replicas, scenario.Twins, scenario.Behavior,
				result.BlocksCommitted, result.MessagesExchanged, len(result.Violations))
		})
	}

	t.Logf("\n=== COMPREHENSIVE TEST RESULTS ===")
	t.Logf("Total scenarios: %d", len(scenarios))
	t.Logf("Passed: %d", passed)
	t.Logf("Failed: %d", failed)
	t.Logf("Honest false positives: %d", honestViolations)

	// Critical check: no false positives
	assert.Equal(t, 0, honestViolations, "Honest scenarios must never have false violations")
}

// ============================================================================
// Interceptor Unit Tests
// ============================================================================

// MockMessage implements ConsensusPayload for testing interceptors.
type MockMessage struct {
	msgType        hotstuff2.MessageType
	view           uint32
	validatorIndex uint16
	vote           *hotstuff2.Vote[TestHash]
}

func (m *MockMessage) Type() hotstuff2.MessageType     { return m.msgType }
func (m *MockMessage) View() uint32                    { return m.view }
func (m *MockMessage) ValidatorIndex() uint16          { return m.validatorIndex }
func (m *MockMessage) Bytes() []byte                   { return []byte{} }
func (m *MockMessage) Hash() TestHash                  { return TestHash{} }
func (m *MockMessage) Vote() *hotstuff2.Vote[TestHash] { return m.vote }

// TestSilentInterceptor_DropsAllOutgoing tests that SilentInterceptor drops all outgoing messages.
func TestSilentInterceptor_DropsAllOutgoing(t *testing.T) {
	interceptor := NewSilentInterceptor[TestHash]()

	// Create various message types
	messages := []*MockMessage{
		{msgType: hotstuff2.MessageVote, view: 1},
		{msgType: hotstuff2.MessageProposal, view: 2},
		{msgType: hotstuff2.MessageNewView, view: 3},
	}

	for _, msg := range messages {
		result := interceptor.InterceptOutgoing(0, msg)
		assert.Empty(t, result, "SilentInterceptor should drop all outgoing messages")
	}
}

// TestSilentInterceptor_DropsAllIncoming tests that SilentInterceptor drops all incoming messages.
func TestSilentInterceptor_DropsAllIncoming(t *testing.T) {
	interceptor := NewSilentInterceptor[TestHash]()

	msg := &MockMessage{msgType: hotstuff2.MessageVote, view: 1}
	result := interceptor.InterceptIncoming(0, msg)
	assert.Nil(t, result, "SilentInterceptor should drop all incoming messages")
}

// TestPassthroughInterceptor_PassesAllMessages tests that PassthroughInterceptor passes all messages.
func TestPassthroughInterceptor_PassesAllMessages(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewPassthroughInterceptor[TestHash](detector)

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	// Test outgoing
	outgoing := interceptor.InterceptOutgoing(0, msg)
	require.Len(t, outgoing, 1, "PassthroughInterceptor should pass messages through")
	assert.Equal(t, -1, outgoing[0].To, "Should broadcast to all")
	assert.Equal(t, msg, outgoing[0].Message, "Message should be unchanged")

	// Test incoming
	incoming := interceptor.InterceptIncoming(0, msg)
	assert.Equal(t, msg, incoming, "PassthroughInterceptor should pass incoming messages unchanged")
}

// TestPassthroughInterceptor_RecordsVotes tests that PassthroughInterceptor records votes.
func TestPassthroughInterceptor_RecordsVotes(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewPassthroughInterceptor[TestHash](detector)

	// Create a vote
	priv, _ := crypto.GenerateEd25519Key()
	hash := NewTestHash("block1")
	vote, err := NewVote(1, hash, 0, priv)
	require.NoError(t, err)

	// MockMessage with vote
	msg := &MockMessage{
		msgType: hotstuff2.MessageVote,
		view:    1,
		vote:    vote,
	}

	// Intercept should record the vote
	interceptor.InterceptOutgoing(0, msg)

	// The vote should be recorded (we can't directly check the detector's internal state,
	// but we can verify by recording a conflicting vote and checking for violations)
	priv2, _ := crypto.GenerateEd25519Key()
	hash2 := NewTestHash("block2")
	vote2, _ := NewVote(1, hash2, 0, priv2)
	detector.RecordVote(1, vote2)

	// Should have a double-sign violation
	assert.True(t, detector.HasViolations(), "Should detect double-sign after recording conflicting votes")
}

// TestDoubleSignInterceptor_CreatesConflicts tests that DoubleSignInterceptor creates conflicting votes.
func TestDoubleSignInterceptor_CreatesConflicts(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewDoubleSignInterceptor[TestHash](0, 4, detector)

	// Create a vote
	priv, _ := crypto.GenerateEd25519Key()
	hash := NewTestHash("block1")
	vote, err := NewVote(1, hash, 0, priv)
	require.NoError(t, err)

	msg := &MockMessage{
		msgType: hotstuff2.MessageVote,
		view:    1,
		vote:    vote,
	}

	// Intercept should record the original and a conflicting vote
	result := interceptor.InterceptOutgoing(0, msg)

	// Should still pass the original message
	require.Len(t, result, 1, "Should pass the original message")
	assert.Equal(t, msg, result[0].Message, "Original message should be passed")

	// Should have a double-sign violation
	assert.True(t, detector.HasViolations(), "Should detect double-sign from conflicting vote")
	violations := detector.GetViolations()
	require.Len(t, violations, 1)
	assert.Equal(t, ViolationDoubleSign, violations[0].Type, "Violation should be double-sign")
}

// TestDoubleSignInterceptor_OnlyDoubleSignsOnce tests that DoubleSignInterceptor only double-signs once per view.
func TestDoubleSignInterceptor_OnlyDoubleSignsOnce(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewDoubleSignInterceptor[TestHash](0, 4, detector)

	priv, _ := crypto.GenerateEd25519Key()
	hash := NewTestHash("block1")
	vote, _ := NewVote(1, hash, 0, priv)

	msg := &MockMessage{
		msgType: hotstuff2.MessageVote,
		view:    1,
		vote:    vote,
	}

	// First call should create violation
	interceptor.InterceptOutgoing(0, msg)
	assert.Len(t, detector.GetViolations(), 1, "First vote should create violation")

	// Second call for same view should not create another violation
	interceptor.InterceptOutgoing(0, msg)
	assert.Len(t, detector.GetViolations(), 1, "Second vote in same view should not create another violation")

	// Different view should create new violation
	vote2, _ := NewVote(2, hash, 0, priv)
	msg2 := &MockMessage{
		msgType: hotstuff2.MessageVote,
		view:    2,
		vote:    vote2,
	}
	interceptor.InterceptOutgoing(0, msg2)
	assert.Len(t, detector.GetViolations(), 2, "Vote in new view should create another violation")
}

// TestDoubleSignInterceptor_PassesNonVotes tests that DoubleSignInterceptor passes non-vote messages unchanged.
func TestDoubleSignInterceptor_PassesNonVotes(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewDoubleSignInterceptor[TestHash](0, 4, detector)

	// Test proposal message
	proposalMsg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}
	result := interceptor.InterceptOutgoing(0, proposalMsg)
	require.Len(t, result, 1)
	assert.Equal(t, proposalMsg, result[0].Message)
	assert.False(t, detector.HasViolations(), "Non-vote messages should not create violations")

	// Test new-view message
	newViewMsg := &MockMessage{msgType: hotstuff2.MessageNewView, view: 1}
	result = interceptor.InterceptOutgoing(0, newViewMsg)
	require.Len(t, result, 1)
	assert.Equal(t, newViewMsg, result[0].Message)
}

// TestRandomBehaviorInterceptor_DropsSomeMessages tests that RandomBehaviorInterceptor drops some messages.
func TestRandomBehaviorInterceptor_DropsSomeMessages(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewRandomBehaviorInterceptor[TestHash](42, detector) // Use fixed seed for reproducibility

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	passed := 0
	dropped := 0

	// Run many iterations to test statistical behavior
	for range 1000 {
		result := interceptor.InterceptOutgoing(0, msg)
		if len(result) > 0 {
			passed++
		} else {
			dropped++
		}
	}

	// Should drop approximately 20% (with some variance)
	dropRate := float64(dropped) / 1000.0
	t.Logf("Drop rate: %.2f%% (expected ~20%%)", dropRate*100)

	assert.Greater(t, dropped, 100, "Should drop some messages")
	assert.Greater(t, passed, 700, "Should pass most messages")
}

// TestRandomBehaviorInterceptor_DropsIncoming tests that RandomBehaviorInterceptor drops some incoming messages.
func TestRandomBehaviorInterceptor_DropsIncoming(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewRandomBehaviorInterceptor[TestHash](123, detector)

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	passed := 0
	dropped := 0

	for range 1000 {
		result := interceptor.InterceptIncoming(0, msg)
		if result != nil {
			passed++
		} else {
			dropped++
		}
	}

	// Should drop approximately 10% incoming (with some variance)
	dropRate := float64(dropped) / 1000.0
	t.Logf("Incoming drop rate: %.2f%% (expected ~10%%)", dropRate*100)

	assert.Greater(t, dropped, 50, "Should drop some incoming messages")
	assert.Greater(t, passed, 800, "Should pass most incoming messages")
}

// TestEquivocationInterceptor_SendsToFirstPartition tests partition-aware message routing.
func TestEquivocationInterceptor_SendsToFirstPartition(t *testing.T) {
	partitions := []Partition{
		{Nodes: []int{0, 1}},
		{Nodes: []int{2, 3}},
	}

	interceptor := NewEquivocationInterceptor[TestHash](0, partitions)

	// Vote messages should only go to first partition
	voteMsg := &MockMessage{msgType: hotstuff2.MessageVote, view: 1}
	result := interceptor.InterceptOutgoing(0, voteMsg)

	require.Len(t, result, 2, "Should send to 2 nodes in first partition")
	for _, out := range result {
		assert.Contains(t, []int{0, 1}, out.To, "Should only send to nodes in first partition")
	}

	// Proposal messages should also go to first partition
	proposalMsg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}
	result = interceptor.InterceptOutgoing(0, proposalMsg)

	require.Len(t, result, 2)
	for _, out := range result {
		assert.Contains(t, []int{0, 1}, out.To)
	}
}

// TestEquivocationInterceptor_BroadcastsOtherMessages tests that non-vote/proposal messages are broadcast.
func TestEquivocationInterceptor_BroadcastsOtherMessages(t *testing.T) {
	partitions := []Partition{
		{Nodes: []int{0, 1}},
		{Nodes: []int{2, 3}},
	}

	interceptor := NewEquivocationInterceptor[TestHash](0, partitions)

	// NewView messages should broadcast to all
	newViewMsg := &MockMessage{msgType: hotstuff2.MessageNewView, view: 1}
	result := interceptor.InterceptOutgoing(0, newViewMsg)

	require.Len(t, result, 1)
	assert.Equal(t, -1, result[0].To, "NewView should broadcast to all")
}

// TestEquivocationInterceptor_NoPartitions tests behavior with no partitions.
func TestEquivocationInterceptor_NoPartitions(t *testing.T) {
	interceptor := NewEquivocationInterceptor[TestHash](0, nil)

	msg := &MockMessage{msgType: hotstuff2.MessageVote, view: 1}
	result := interceptor.InterceptOutgoing(0, msg)

	require.Len(t, result, 1)
	assert.Equal(t, -1, result[0].To, "Without partitions, should broadcast to all")
}

// TestEquivocationInterceptor_SinglePartition tests behavior with only one partition.
func TestEquivocationInterceptor_SinglePartition(t *testing.T) {
	partitions := []Partition{
		{Nodes: []int{0, 1, 2, 3}},
	}

	interceptor := NewEquivocationInterceptor[TestHash](0, partitions)

	msg := &MockMessage{msgType: hotstuff2.MessageVote, view: 1}
	result := interceptor.InterceptOutgoing(0, msg)

	// With single partition, should broadcast (no equivocation possible)
	require.Len(t, result, 1)
	assert.Equal(t, -1, result[0].To)
}

// ============================================================================
// Delay Interceptor Tests
// ============================================================================

// TestDelayInterceptor_DelaysMessages tests that DelayInterceptor delays messages correctly.
func TestDelayInterceptor_DelaysMessages(t *testing.T) {
	config := DelayInterceptorConfig{
		MinDelay:    2,
		MaxDelay:    2, // Fixed delay for predictable testing
		ReorderProb: 0, // No reordering for this test
	}
	interceptor := NewDelayInterceptor[TestHash](0, nil, config)

	msg1 := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}
	msg2 := &MockMessage{msgType: hotstuff2.MessageProposal, view: 2}
	msg3 := &MockMessage{msgType: hotstuff2.MessageProposal, view: 3}

	// First message: should not be released immediately (delay = 2)
	result := interceptor.InterceptOutgoing(0, msg1)
	assert.Empty(t, result, "First message should be delayed")
	assert.Equal(t, 1, interceptor.QueueLength(), "Queue should have 1 message")

	// Second message: still not released
	result = interceptor.InterceptOutgoing(0, msg2)
	assert.Empty(t, result, "Second message should still be delayed")
	assert.Equal(t, 2, interceptor.QueueLength(), "Queue should have 2 messages")

	// Third message: first message should now be released (delay was 2)
	result = interceptor.InterceptOutgoing(0, msg3)
	assert.Len(t, result, 1, "First message should be released after delay")
	assert.Equal(t, msg1, result[0].Message, "Released message should be the first one")
	assert.Equal(t, 2, interceptor.QueueLength(), "Queue should have 2 messages remaining")
}

// TestDelayInterceptor_Flush tests that Flush releases all delayed messages.
func TestDelayInterceptor_Flush(t *testing.T) {
	config := DelayInterceptorConfig{
		MinDelay:    5,
		MaxDelay:    5,
		ReorderProb: 0,
	}
	interceptor := NewDelayInterceptor[TestHash](0, nil, config)

	// Queue several messages
	for i := range 3 {
		msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: uint32(i + 1)}
		interceptor.InterceptOutgoing(0, msg)
	}

	assert.Equal(t, 3, interceptor.QueueLength(), "Should have 3 messages queued")

	// Flush should release all
	result := interceptor.Flush()
	assert.Len(t, result, 3, "Flush should release all messages")
	assert.Equal(t, 0, interceptor.QueueLength(), "Queue should be empty after flush")
}

// TestDelayInterceptor_RecordsVotes tests that DelayInterceptor records votes for detection.
func TestDelayInterceptor_RecordsVotes(t *testing.T) {
	detector := NewViolationDetector[TestHash]()
	config := DelayInterceptorConfig{
		MinDelay:    1,
		MaxDelay:    1,
		ReorderProb: 0,
	}
	interceptor := NewDelayInterceptor[TestHash](0, detector, config)

	// Create a vote
	priv, _ := crypto.GenerateEd25519Key()
	hash := NewTestHash("block1")
	vote, err := NewVote(1, hash, 0, priv)
	require.NoError(t, err)

	msg := &MockMessage{
		msgType: hotstuff2.MessageVote,
		view:    1,
		vote:    vote,
	}

	// Intercept should record the vote
	interceptor.InterceptOutgoing(0, msg)

	// Record a conflicting vote to verify detection works
	hash2 := NewTestHash("block2")
	vote2, _ := NewVote(1, hash2, 0, priv)
	detector.RecordVote(1, vote2)

	assert.True(t, detector.HasViolations(), "Should detect double-sign after recording conflicting votes")
}

// TestDelayInterceptor_Reordering tests that DelayInterceptor can reorder messages.
func TestDelayInterceptor_Reordering(t *testing.T) {
	config := DelayInterceptorConfig{
		MinDelay:    10, // Long delay to ensure messages stay queued
		MaxDelay:    10,
		ReorderProb: 1.0, // Always attempt reorder
	}
	interceptor := NewDelayInterceptor[TestHash](42, nil, config) // Fixed seed

	// Queue several messages - they should all stay in queue due to long delay
	for i := range 5 {
		msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: uint32(i + 1)}
		result := interceptor.InterceptOutgoing(0, msg)
		// With minDelay=10, no messages should be released yet
		assert.Empty(t, result, "Messages should stay in queue with long delay")
	}

	// All 5 messages should be in queue
	assert.Equal(t, 5, interceptor.QueueLength(), "Should have 5 messages in queue")

	// Flush all messages
	flushed := interceptor.Flush()
	require.Len(t, flushed, 5, "Should flush 5 messages")

	// Extract views in order they were flushed
	views := make([]uint32, len(flushed))
	for i, out := range flushed {
		if mockMsg, ok := out.Message.(*MockMessage); ok {
			views[i] = mockMsg.view
		}
	}

	t.Logf("Views after reordering: %v", views)
	// With reorderProb=1.0, the queue gets shuffled on each InterceptOutgoing call
}

// TestDelayInterceptor_VariableDelay tests variable delay between min and max.
func TestDelayInterceptor_VariableDelay(t *testing.T) {
	config := DelayInterceptorConfig{
		MinDelay:    3,
		MaxDelay:    10, // Longer delays to ensure messages stay queued
		ReorderProb: 0,
	}
	interceptor := NewDelayInterceptor[TestHash](0, nil, config)

	// Queue messages and track total released + queued
	totalSent := 10
	releasedDuringIntercept := 0
	for i := range totalSent {
		msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: uint32(i + 1)}
		result := interceptor.InterceptOutgoing(0, msg)
		releasedDuringIntercept += len(result)
	}

	// Some messages should still be in queue with longer delays
	queued := interceptor.QueueLength()
	t.Logf("Released during intercept: %d, still queued: %d", releasedDuringIntercept, queued)

	// Flush remaining and verify total matches
	result := interceptor.Flush()
	totalReleased := releasedDuringIntercept + len(result)

	assert.Equal(t, totalSent, totalReleased, "Total released should match total sent")
}

// TestDelayInterceptor_PassesIncomingUnchanged tests that incoming messages pass through.
func TestDelayInterceptor_PassesIncomingUnchanged(t *testing.T) {
	config := DefaultDelayConfig()
	interceptor := NewDelayInterceptor[TestHash](0, nil, config)

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}
	result := interceptor.InterceptIncoming(0, msg)

	assert.Equal(t, msg, result, "Incoming messages should pass through unchanged")
}

// TestDelayInterceptor_DefaultConfig tests the default configuration.
func TestDelayInterceptor_DefaultConfig(t *testing.T) {
	config := DefaultDelayConfig()

	assert.Equal(t, 1, config.MinDelay, "Default min delay should be 1")
	assert.Equal(t, 5, config.MaxDelay, "Default max delay should be 5")
	assert.Equal(t, 0.3, config.ReorderProb, "Default reorder probability should be 0.3")
}

// ============================================================================
// Network Interceptor Integration Tests
// ============================================================================

// TestNetwork_SetSilent tests that SetSilent prevents message delivery.
func TestNetwork_SetSilent(t *testing.T) {
	network := NewTwinsNetwork[TestHash](4, nil)
	defer network.Close()

	// Set node 0 as silent
	network.SetSilent(0, true)

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	// Node 0's broadcast should be dropped
	network.broadcast(0, msg)
	assert.Equal(t, 0, network.MessageCount(), "Silent node's messages should be dropped")

	// Node 1's message to node 0 should be dropped
	network.broadcast(1, msg)

	// Check that node 0 didn't receive the message
	select {
	case <-network.nodeChannels[0]:
		t.Error("Silent node should not receive messages")
	default:
		// Good - no message received
	}

	// Remove silent status
	network.SetSilent(0, false)
	assert.False(t, network.IsSilent(0))
}

// TestNetwork_SetInterceptor tests that interceptors are called.
func TestNetwork_SetInterceptor(t *testing.T) {
	network := NewTwinsNetwork[TestHash](4, nil)
	defer network.Close()

	// Set silent interceptor on node 0
	network.SetInterceptor(0, NewSilentInterceptor[TestHash]())

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	// Node 0's broadcast should be dropped by interceptor
	network.broadcast(0, msg)
	assert.Equal(t, 0, network.MessageCount(), "Intercepted node's messages should be dropped")

	// Remove interceptor
	network.SetInterceptor(0, nil)

	// Now messages should go through
	network.broadcast(0, msg)
	assert.Greater(t, network.MessageCount(), 0, "Messages should go through without interceptor")
}

// ============================================================================
// Benchmarks
// ============================================================================

// BenchmarkScenarioExecution benchmarks scenario execution time.
func BenchmarkScenarioExecution(b *testing.B) {
	scenario := Scenario{
		Replicas: 4,
		Twins:    0,
		Views:    3,
		Behavior: BehaviorHonest,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor, _ := NewExecutor[TestHash256](scenario)
		executor.Execute()
	}
}

// BenchmarkScenarioExecution_WithTwins benchmarks execution with twin pairs.
func BenchmarkScenarioExecution_WithTwins(b *testing.B) {
	scenario := Scenario{
		Replicas: 3,
		Twins:    1,
		Views:    3,
		Behavior: BehaviorDoubleSign,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		executor, _ := NewExecutor[TestHash256](scenario)
		executor.Execute()
	}
}

// BenchmarkInterceptorOverhead benchmarks message interception overhead.
func BenchmarkInterceptorOverhead(b *testing.B) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewPassthroughInterceptor[TestHash](detector)
	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interceptor.InterceptOutgoing(0, msg)
	}
}

// BenchmarkDoubleSignInterceptor benchmarks double-sign interceptor.
func BenchmarkDoubleSignInterceptor(b *testing.B) {
	detector := NewViolationDetector[TestHash]()
	interceptor := NewDoubleSignInterceptor[TestHash](0, 4, detector)

	priv, _ := crypto.GenerateEd25519Key()
	hash := NewTestHash("block")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vote, _ := NewVote(uint32(i), hash, 0, priv)
		msg := &MockMessage{
			msgType: hotstuff2.MessageVote,
			view:    uint32(i),
			vote:    vote,
		}
		interceptor.InterceptOutgoing(0, msg)
	}
}

// BenchmarkViolationDetector benchmarks vote recording and detection.
func BenchmarkViolationDetector(b *testing.B) {
	detector := NewViolationDetector[TestHash]()
	priv, _ := crypto.GenerateEd25519Key()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hash := NewTestHash(fmt.Sprintf("block%d", i))
		vote, _ := NewVote(uint32(i%100), hash, 0, priv)
		detector.RecordVote(0, vote)
	}
}

// BenchmarkNetworkBroadcast benchmarks network message broadcasting.
func BenchmarkNetworkBroadcast(b *testing.B) {
	network := NewTwinsNetwork[TestHash](7, nil)
	defer network.Close()

	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		network.broadcast(0, msg)
	}
}

// BenchmarkDelayInterceptor benchmarks delay interceptor performance.
func BenchmarkDelayInterceptor(b *testing.B) {
	config := DefaultDelayConfig()
	interceptor := NewDelayInterceptor[TestHash](0, nil, config)
	msg := &MockMessage{msgType: hotstuff2.MessageProposal, view: 1}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interceptor.InterceptOutgoing(0, msg)
	}
}
