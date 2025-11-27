package hotstuff2

import (
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/internal/crypto"
)

func TestVoteCreation(t *testing.T) {
	key, err := crypto.GenerateEd25519Key()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	view := uint32(5)
	nodeHash := NewTestHash("block-1")
	validatorIndex := uint16(2)

	vote, err := NewVote(view, nodeHash, validatorIndex, key)
	if err != nil {
		t.Fatalf("Failed to create vote: %v", err)
	}

	if vote.View() != view {
		t.Errorf("View mismatch: expected %d, got %d", view, vote.View())
	}

	if !vote.NodeHash().Equals(nodeHash) {
		t.Error("NodeHash mismatch")
	}

	if vote.ValidatorIndex() != validatorIndex {
		t.Errorf("ValidatorIndex mismatch: expected %d, got %d", validatorIndex, vote.ValidatorIndex())
	}

	if len(vote.Signature()) == 0 {
		t.Error("Signature is empty")
	}

	if vote.Timestamp() == 0 {
		t.Error("Timestamp is zero")
	}
}

func TestVoteVerification(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()
	pubKey := key.PublicKey()

	vote, err := NewVote(1, NewTestHash("block-1"), 0, key)
	if err != nil {
		t.Fatalf("Failed to create vote: %v", err)
	}

	// Valid verification
	if err := vote.Verify(pubKey); err != nil {
		t.Errorf("Valid vote failed verification: %v", err)
	}

	// Wrong public key
	otherKey, _ := crypto.GenerateEd25519Key()
	otherPubKey := otherKey.PublicKey()
	if err := vote.Verify(otherPubKey); err == nil {
		t.Error("Vote verified with wrong public key")
	}
}

func TestVoteTimestampValidation(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()
	pubKey := key.PublicKey()

	// Create vote with current timestamp
	vote, _ := NewVote(1, NewTestHash("block-1"), 0, key)

	// Should verify immediately
	if err := vote.Verify(pubKey); err != nil {
		t.Errorf("Fresh vote failed verification: %v", err)
	}

	// Manually create vote with old timestamp (simulate replay attack)
	oldVote := &Vote[TestHash]{
		view:           1,
		nodeHash:       NewTestHash("block-1"),
		validatorIndex: 0,
		timestamp:      uint64(time.Now().UnixMilli()) - (VoteTimestampWindow + 1000),
	}
	digest := oldVote.Digest()
	sig, _ := key.Sign(digest)
	oldVote.signature = sig

	// Should fail due to timestamp
	if err := oldVote.Verify(pubKey); err == nil {
		t.Error("Old vote should fail timestamp validation")
	}

	// Future timestamp (clock skew attack)
	futureVote := &Vote[TestHash]{
		view:           1,
		nodeHash:       NewTestHash("block-1"),
		validatorIndex: 0,
		timestamp:      uint64(time.Now().UnixMilli()) + (VoteTimestampWindow + 1000),
	}
	digest = futureVote.Digest()
	sig, _ = key.Sign(digest)
	futureVote.signature = sig

	if err := futureVote.Verify(pubKey); err == nil {
		t.Error("Future vote should fail timestamp validation")
	}
}

func TestVoteSerialization(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()

	originalVote, _ := NewVote(5, NewTestHash("block-1"), 2, key)

	// Serialize
	voteBytes := originalVote.Bytes()

	// Deserialize
	restoredVote, err := VoteFromBytes(voteBytes, func(b []byte) (TestHash, error) {
		var hash TestHash
		copy(hash[:], b)
		return hash, nil
	})

	if err != nil {
		t.Fatalf("Failed to deserialize vote: %v", err)
	}

	// Compare fields
	if restoredVote.View() != originalVote.View() {
		t.Errorf("View mismatch after deserialization")
	}

	if !restoredVote.NodeHash().Equals(originalVote.NodeHash()) {
		t.Error("NodeHash mismatch after deserialization")
	}

	if restoredVote.ValidatorIndex() != originalVote.ValidatorIndex() {
		t.Error("ValidatorIndex mismatch after deserialization")
	}

	if restoredVote.Timestamp() != originalVote.Timestamp() {
		t.Error("Timestamp mismatch after deserialization")
	}

	// Verify signature still works
	pubKey := key.PublicKey()
	if err := restoredVote.Verify(pubKey); err != nil {
		t.Errorf("Deserialized vote verification failed: %v", err)
	}
}

func TestVoteDigest(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()

	vote1, _ := NewVote(1, NewTestHash("block-1"), 0, key)
	vote2, _ := NewVote(1, NewTestHash("block-1"), 0, key)

	// Same parameters should produce same digest
	digest1 := vote1.Digest()
	digest2 := vote2.Digest()

	if len(digest1) != len(digest2) {
		t.Error("Digest lengths differ")
	}

	// Different view should produce different digest
	vote3, _ := NewVote(2, NewTestHash("block-1"), 0, key)
	digest3 := vote3.Digest()

	digestsEqual := true
	for i := range digest1 {
		if digest1[i] != digest3[i] {
			digestsEqual = false
			break
		}
	}

	if digestsEqual {
		t.Error("Different views should produce different digests")
	}
}

func TestVoteInvalidInputs(t *testing.T) {
	// Test deserialization with invalid data
	invalidData := []byte{1, 2, 3} // Too short

	_, err := VoteFromBytes[TestHash](invalidData, func(b []byte) (TestHash, error) {
		var hash TestHash
		copy(hash[:], b)
		return hash, nil
	})

	if err == nil {
		t.Error("Expected error for invalid vote data")
	}
}

func BenchmarkVoteCreation(b *testing.B) {
	key, _ := crypto.GenerateEd25519Key()
	nodeHash := NewTestHash("block-1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = NewVote(1, nodeHash, 0, key)
	}
}

func BenchmarkVoteVerification(b *testing.B) {
	key, _ := crypto.GenerateEd25519Key()
	pubKey := key.PublicKey()
	vote, _ := NewVote(1, NewTestHash("block-1"), 0, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vote.Verify(pubKey)
	}
}

func BenchmarkVoteSerialization(b *testing.B) {
	key, _ := crypto.GenerateEd25519Key()
	vote, _ := NewVote(1, NewTestHash("block-1"), 0, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = vote.Bytes()
	}
}
