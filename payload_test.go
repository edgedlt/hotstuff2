package hotstuff2

import (
	"testing"

	"github.com/edgedlt/hotstuff2/internal/crypto"
)

// TestProposeMessageCreation tests PROPOSE message creation.
func TestProposeMessageCreation(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)
	block := NewTestBlock(1, NewTestHash("genesis"), 0)

	// Create justification QC
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		votes[i], _ = NewVote(0, NewTestHash("genesis"), uint16(i), keys[i])
	}
	justifyQC, _ := NewQC(0, NewTestHash("genesis"), votes, validators, CryptoSchemeEd25519)

	msg := NewProposeMessage(1, 0, block, justifyQC)

	if msg.Type() != MessageProposal {
		t.Errorf("Message type should be PROPOSAL, got %s", msg.Type())
	}

	if msg.View() != 1 {
		t.Errorf("View should be 1, got %d", msg.View())
	}

	if msg.ValidatorIndex() != 0 {
		t.Errorf("ValidatorIndex should be 0, got %d", msg.ValidatorIndex())
	}

	if msg.Block() == nil {
		t.Fatal("Block should not be nil")
	}

	if msg.JustifyQC() == nil {
		t.Fatal("JustifyQC should not be nil")
	}
}

// TestVoteMessageCreation tests VOTE message creation.
func TestVoteMessageCreation(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()
	vote, _ := NewVote(1, NewTestHash("block-1"), 2, key)

	msg := NewVoteMessage(1, 2, vote)

	if msg.Type() != MessageVote {
		t.Errorf("Message type should be VOTE, got %s", msg.Type())
	}

	if msg.View() != 1 {
		t.Errorf("View should be 1, got %d", msg.View())
	}

	if msg.ValidatorIndex() != 2 {
		t.Errorf("ValidatorIndex should be 2, got %d", msg.ValidatorIndex())
	}

	if msg.Vote() == nil {
		t.Fatal("Vote should not be nil")
	}
}

// TestNewViewMessageCreation tests NEWVIEW message creation.
func TestNewViewMessageCreation(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	// Create highQC
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		votes[i], _ = NewVote(2, NewTestHash("block-2"), uint16(i), keys[i])
	}
	highQC, _ := NewQC(2, NewTestHash("block-2"), votes, validators, CryptoSchemeEd25519)

	msg := NewNewViewMessage(3, 1, highQC)

	if msg.Type() != MessageNewView {
		t.Errorf("Message type should be NEWVIEW, got %s", msg.Type())
	}

	if msg.View() != 3 {
		t.Errorf("View should be 3, got %d", msg.View())
	}

	if msg.ValidatorIndex() != 1 {
		t.Errorf("ValidatorIndex should be 1, got %d", msg.ValidatorIndex())
	}

	if msg.HighQC() == nil {
		t.Fatal("HighQC should not be nil")
	}
}

// TestMessageAccessors tests message accessor methods.
func TestMessageAccessors(t *testing.T) {
	block := NewTestBlock(5, NewTestHash("prev"), 0)
	msg := NewProposeMessage[TestHash](10, 3, block, nil)

	if msg.Type() != MessageProposal {
		t.Error("Type() failed")
	}

	if msg.View() != 10 {
		t.Error("View() failed")
	}

	if msg.ValidatorIndex() != 3 {
		t.Error("ValidatorIndex() failed")
	}

	if msg.Block().Height() != 5 {
		t.Error("Block() failed")
	}

	// JustifyQC can be nil for genesis
	if msg.JustifyQC() != nil {
		t.Error("JustifyQC should be nil")
	}
}

// TestMessageTypeString tests message type string representation.
func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		msgType MessageType
		want    string
	}{
		{MessageProposal, "PROPOSAL"},
		{MessageVote, "VOTE"},
		{MessageNewView, "NEWVIEW"},
		{MessageType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.msgType.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestProposeMessageNilJustifyQC tests PROPOSE with nil justify QC (genesis).
func TestProposeMessageNilJustifyQC(t *testing.T) {
	genesisBlock := NewTestBlock(0, TestHash{}, 0)
	msg := NewProposeMessage[TestHash](0, 0, genesisBlock, nil)

	if msg.JustifyQC() != nil {
		t.Error("Genesis proposal should have nil justifyQC")
	}

	if msg.Block().Height() != 0 {
		t.Error("Genesis block height should be 0")
	}
}

// TestVoteMessageAccessors tests Vote message-specific accessors.
func TestVoteMessageAccessors(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()
	vote, _ := NewVote(5, NewTestHash("test-block"), 1, key)

	msg := NewVoteMessage(5, 1, vote)

	retrievedVote := msg.Vote()
	if retrievedVote == nil {
		t.Fatal("Vote should not be nil")
	}

	if retrievedVote.View() != 5 {
		t.Errorf("Vote view should be 5, got %d", retrievedVote.View())
	}

	if retrievedVote.ValidatorIndex() != 1 {
		t.Errorf("Vote validator index should be 1, got %d", retrievedVote.ValidatorIndex())
	}
}

// TestNewViewMessageAccessors tests NEWVIEW message-specific accessors.
func TestNewViewMessageAccessors(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	// Create QC
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		votes[i], _ = NewVote(10, NewTestHash("block"), uint16(i), keys[i])
	}
	qc, _ := NewQC(10, NewTestHash("block"), votes, validators, CryptoSchemeEd25519)

	msg := NewNewViewMessage(11, 2, qc)

	retrievedQC := msg.HighQC()
	if retrievedQC == nil {
		t.Fatal("HighQC should not be nil")
	}

	if retrievedQC.View() != 10 {
		t.Errorf("HighQC view should be 10, got %d", retrievedQC.View())
	}
}

// TestMessageBytes tests message serialization.
func TestMessageBytes(t *testing.T) {
	block := NewTestBlock(1, NewTestHash("genesis"), 0)
	msg := NewProposeMessage[TestHash](1, 0, block, nil)

	bytes := msg.Bytes()
	if len(bytes) == 0 {
		t.Error("Message bytes should not be empty")
	}

	// Test Hash()
	hash := msg.Hash()
	if hash == (TestHash{}) {
		t.Error("Message hash should not be empty")
	}
}

// TestMessageEquality tests that identical messages produce same hash.
func TestMessageEquality(t *testing.T) {
	block := NewTestBlock(1, NewTestHash("genesis"), 0)
	msg1 := NewProposeMessage[TestHash](1, 0, block, nil)
	msg2 := NewProposeMessage[TestHash](1, 0, block, nil)

	// Same content should produce same hash
	if !msg1.Hash().Equals(msg2.Hash()) {
		t.Error("Identical messages should have identical hashes")
	}
}

// TestMessageDifference tests that different messages produce different hashes.
func TestMessageDifference(t *testing.T) {
	block1 := NewTestBlock(1, NewTestHash("genesis"), 0)
	block2 := NewTestBlock(2, NewTestHash("genesis"), 0)

	msg1 := NewProposeMessage[TestHash](1, 0, block1, nil)
	msg2 := NewProposeMessage[TestHash](1, 0, block2, nil)

	// Different blocks should produce different hashes
	if msg1.Hash().Equals(msg2.Hash()) {
		t.Error("Different messages should have different hashes")
	}
}

// TestProposeMessageSerialization tests PROPOSE message round-trip serialization.
func TestProposeMessageSerialization(t *testing.T) {
	// Create a PROPOSE message with QC
	block := NewTestBlock(1, NewTestHash("genesis"), 0)

	// Create QC for justify
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3)
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		keys[i], _ = crypto.GenerateEd25519Key()
		votes[i], _ = NewVote(0, NewTestHash("genesis"), uint16(i), keys[i])
	}
	justifyQC, _ := NewQC(0, NewTestHash("genesis"), votes, validators, CryptoSchemeEd25519)

	msg := NewProposeMessage[TestHash](1, 0, block, justifyQC)

	// Serialize
	bytes := msg.Bytes()
	if len(bytes) == 0 {
		t.Fatal("Serialization produced empty bytes")
	}

	// Deserialize
	// Helper to parse TestHash
	parseHash := func(b []byte) (TestHash, error) {
		var h TestHash
		copy(h[:], b)
		return h, nil
	}

	// Helper to parse Block
	parseBlock := func(b []byte) (Block[TestHash], error) {
		// For testing, reconstruct the block
		// In production, this would properly deserialize
		return block, nil
	}

	msg2, err := MessageFromBytes[TestHash](bytes, parseHash, parseBlock)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Verify type
	if msg2.Type() != MessageProposal {
		t.Errorf("Expected PROPOSE, got %v", msg2.Type())
	}

	// Verify fields
	if msg2.View() != msg.View() {
		t.Error("View mismatch after deserialization")
	}

	if msg2.ValidatorIndex() != msg.ValidatorIndex() {
		t.Error("Validator index mismatch after deserialization")
	}
}

// TestVoteMessageSerialization tests VOTE message round-trip serialization.
func TestVoteMessageSerialization(t *testing.T) {
	key, _ := crypto.GenerateEd25519Key()
	vote, _ := NewVote(1, NewTestHash("block-1"), 0, key)

	msg := NewVoteMessage[TestHash](1, 0, vote)

	// Serialize
	bytes := msg.Bytes()
	if len(bytes) == 0 {
		t.Fatal("Serialization produced empty bytes")
	}

	// Deserialize
	parseHash := func(b []byte) (TestHash, error) {
		var h TestHash
		copy(h[:], b)
		return h, nil
	}

	parseBlock := func(b []byte) (Block[TestHash], error) {
		return nil, nil // Not used for VOTE messages
	}

	msg2, err := MessageFromBytes[TestHash](bytes, parseHash, parseBlock)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Verify type
	if msg2.Type() != MessageVote {
		t.Errorf("Expected VOTE, got %v", msg2.Type())
	}

	// Verify fields
	if msg2.View() != msg.View() {
		t.Error("View mismatch after deserialization")
	}

	if msg2.ValidatorIndex() != msg.ValidatorIndex() {
		t.Error("Validator index mismatch after deserialization")
	}

	// Verify vote
	vote2 := msg2.Vote()
	if vote2 == nil {
		t.Fatal("Deserialized vote is nil")
	}

	if vote2.View() != vote.View() {
		t.Error("Vote view mismatch")
	}
}

// TestNewViewMessageSerialization tests NEWVIEW message round-trip serialization.
func TestNewViewMessageSerialization(t *testing.T) {
	// Create high QC
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3)
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		keys[i], _ = crypto.GenerateEd25519Key()
		votes[i], _ = NewVote(1, NewTestHash("block-1"), uint16(i), keys[i])
	}
	highQC, _ := NewQC(1, NewTestHash("block-1"), votes, validators, CryptoSchemeEd25519)

	msg := NewNewViewMessage[TestHash](2, 1, highQC)

	// Serialize
	bytes := msg.Bytes()
	if len(bytes) == 0 {
		t.Fatal("Serialization produced empty bytes")
	}

	// Deserialize
	parseHash := func(b []byte) (TestHash, error) {
		var h TestHash
		copy(h[:], b)
		return h, nil
	}

	parseBlock := func(b []byte) (Block[TestHash], error) {
		return nil, nil // Not used for NEWVIEW messages
	}

	msg2, err := MessageFromBytes[TestHash](bytes, parseHash, parseBlock)
	if err != nil {
		t.Fatalf("Deserialization failed: %v", err)
	}

	// Verify type
	if msg2.Type() != MessageNewView {
		t.Errorf("Expected NEWVIEW, got %v", msg2.Type())
	}

	// Verify fields
	if msg2.View() != msg.View() {
		t.Error("View mismatch after deserialization")
	}

	if msg2.ValidatorIndex() != msg.ValidatorIndex() {
		t.Error("Validator index mismatch after deserialization")
	}

	// Verify highQC
	highQC2 := msg2.HighQC()
	if highQC2 == nil {
		t.Fatal("Deserialized highQC is nil")
	}

	if highQC2.View() != highQC.View() {
		t.Error("HighQC view mismatch")
	}
}

// TestMessageSerializationRoundTrip tests that all message types can be serialized and deserialized.
func TestMessageSerializationRoundTrip(t *testing.T) {
	parseHash := func(b []byte) (TestHash, error) {
		var h TestHash
		copy(h[:], b)
		return h, nil
	}

	parseBlock := func(b []byte) (Block[TestHash], error) {
		// For testing, return a dummy block
		return NewTestBlock(1, NewTestHash("test"), 0), nil
	}

	// Test PROPOSE
	block := NewTestBlock(1, NewTestHash("genesis"), 0)
	proposeMsg := NewProposeMessage[TestHash](1, 0, block, nil)
	proposeBytes := proposeMsg.Bytes()
	proposeParsed, err := MessageFromBytes[TestHash](proposeBytes, parseHash, parseBlock)
	if err != nil {
		t.Errorf("PROPOSE round-trip failed: %v", err)
	}
	if proposeParsed.Type() != MessageProposal {
		t.Error("PROPOSE type mismatch after round-trip")
	}

	// Test VOTE
	key, _ := crypto.GenerateEd25519Key()
	vote, _ := NewVote(1, NewTestHash("block-1"), 0, key)
	voteMsg := NewVoteMessage[TestHash](1, 0, vote)
	voteBytes := voteMsg.Bytes()
	voteParsed, err := MessageFromBytes[TestHash](voteBytes, parseHash, parseBlock)
	if err != nil {
		t.Errorf("VOTE round-trip failed: %v", err)
	}
	if voteParsed.Type() != MessageVote {
		t.Error("VOTE type mismatch after round-trip")
	}

	// Test NEWVIEW
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3)
	votes := make([]*Vote[TestHash], 3)
	for i := 0; i < 3; i++ {
		keys[i], _ = crypto.GenerateEd25519Key()
		votes[i], _ = NewVote(1, NewTestHash("block-1"), uint16(i), keys[i])
	}
	highQC, _ := NewQC(1, NewTestHash("block-1"), votes, validators, CryptoSchemeEd25519)
	newViewMsg := NewNewViewMessage[TestHash](2, 1, highQC)
	newViewBytes := newViewMsg.Bytes()
	newViewParsed, err := MessageFromBytes[TestHash](newViewBytes, parseHash, parseBlock)
	if err != nil {
		t.Errorf("NEWVIEW round-trip failed: %v", err)
	}
	if newViewParsed.Type() != MessageNewView {
		t.Error("NEWVIEW type mismatch after round-trip")
	}
}
