package hotstuff2

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/edgedlt/hotstuff2/internal/crypto"
)

func TestQCFormation(t *testing.T) {
	// Create 4 validators (f=1, quorum=3)
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 4)
	for i := range 4 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	// Create votes from 3 validators (quorum)
	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		vote, err := NewVote(view, nodeHash, uint16(i), keys[i])
		if err != nil {
			t.Fatalf("Failed to create vote %d: %v", i, err)
		}
		votes[i] = vote
	}

	// Form QC
	qc, err := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	if err != nil {
		t.Fatalf("Failed to form QC: %v", err)
	}

	if qc.View() != view {
		t.Errorf("QC view mismatch: expected %d, got %d", view, qc.View())
	}

	if !qc.Node().Equals(nodeHash) {
		t.Error("QC node hash mismatch")
	}

	signers := qc.Signers()
	if len(signers) != 3 {
		t.Errorf("Expected 3 signers, got %d", len(signers))
	}

	// Verify signers are sorted and deduplicated
	for i := 1; i < len(signers); i++ {
		if signers[i] <= signers[i-1] {
			t.Error("Signers not sorted")
		}
	}
}

func TestQCFormationInsufficientVotes(t *testing.T) {
	validators := NewTestValidatorSet(4) // f=1, quorum=3
	keys := make([]*crypto.Ed25519PrivateKey, 2)
	for i := range 2 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	// Only 2 votes (not quorum)
	votes := make([]*Vote[TestHash], 2)
	for i := range 2 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	// Should fail - insufficient votes
	_, err := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	if err == nil {
		t.Error("Expected error for insufficient votes")
	}
}

func TestQCDeduplication(t *testing.T) {
	// Test that duplicate votes from same validator are deduplicated
	validators := NewTestValidatorSet(4)
	key, _ := crypto.GenerateEd25519Key()

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	// Create 3 votes, but 2 from validator 0 (duplicate)
	votes := make([]*Vote[TestHash], 3)
	votes[0], _ = NewVote(view, nodeHash, 0, key)
	votes[1], _ = NewVote(view, nodeHash, 0, key) // Duplicate

	key2, _ := crypto.GenerateEd25519Key()
	votes[2], _ = NewVote(view, nodeHash, 1, key2)

	// Should fail - only 2 unique validators (not quorum of 3)
	_, err := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	if err == nil {
		t.Error("Expected error due to insufficient unique votes after deduplication")
	}
}

func TestQCValidation(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	// Create valid QC
	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	qc, _ := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)

	// Validate
	if err := qc.Validate(validators); err != nil {
		t.Errorf("Valid QC failed validation: %v", err)
	}
}

func TestQCSerialization(t *testing.T) {
	validators, keys := NewTestValidatorSetWithKeys(4)

	view := uint32(5)
	nodeHash := NewTestHash("block-1")

	votes := make([]*Vote[TestHash], 3)
	for i := range 3 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	originalQC, _ := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)

	// Serialize
	qcBytes := originalQC.Bytes()

	// Deserialize
	restoredQC, err := QCFromBytes(qcBytes, func(b []byte) (TestHash, error) {
		var hash TestHash
		copy(hash[:], b)
		return hash, nil
	})

	if err != nil {
		t.Fatalf("Failed to deserialize QC: %v", err)
	}

	// Compare
	if restoredQC.View() != originalQC.View() {
		t.Error("View mismatch after deserialization")
	}

	if !restoredQC.Node().Equals(originalQC.Node()) {
		t.Error("Node hash mismatch after deserialization")
	}

	if len(restoredQC.Signers()) != len(originalQC.Signers()) {
		t.Error("Signer count mismatch after deserialization")
	}

	// Validate restored QC
	if err := restoredQC.Validate(validators); err != nil {
		t.Errorf("Restored QC validation failed: %v", err)
	}
}

func TestQCMismatchedVotes(t *testing.T) {
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3)
	for i := range 3 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)

	// Votes for different blocks
	votes := make([]*Vote[TestHash], 3)
	votes[0], _ = NewVote(view, NewTestHash("block-1"), 0, keys[0])
	votes[1], _ = NewVote(view, NewTestHash("block-2"), 1, keys[1]) // Different block
	votes[2], _ = NewVote(view, NewTestHash("block-1"), 2, keys[2])

	// Should fail - votes for different blocks
	_, err := NewQC(view, NewTestHash("block-1"), votes, validators, CryptoSchemeEd25519)
	if err == nil {
		t.Error("Expected error for votes on different blocks")
	}
}

func TestQCDifferentViews(t *testing.T) {
	validators := NewTestValidatorSet(4)
	keys := make([]*crypto.Ed25519PrivateKey, 3)
	for i := range 3 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	nodeHash := NewTestHash("block-1")

	// Votes for different views
	votes := make([]*Vote[TestHash], 3)
	votes[0], _ = NewVote(1, nodeHash, 0, keys[0])
	votes[1], _ = NewVote(2, nodeHash, 1, keys[1]) // Different view
	votes[2], _ = NewVote(1, nodeHash, 2, keys[2])

	// Should fail - votes from different views
	_, err := NewQC(1, nodeHash, votes, validators, CryptoSchemeEd25519)
	if err == nil {
		t.Error("Expected error for votes from different views")
	}
}

func TestQCInvalidValidatorIndex(t *testing.T) {
	validators := NewTestValidatorSet(4) // Valid indices: 0-3

	// Manually create QC with invalid validator index
	qc := &QC[TestHash]{
		view:         1,
		node:         NewTestHash("block-1"),
		signers:      []uint16{0, 1, 99}, // 99 is invalid
		cryptoScheme: CryptoSchemeEd25519,
	}

	// Should fail validation
	if err := qc.Validate(validators); err == nil {
		t.Error("Expected error for invalid validator index")
	}
}

func TestQCByzantineAttack(t *testing.T) {
	// Simulate Byzantine attack: attacker tries to create QC with <2f+1 real votes
	// by duplicating signatures

	validators := NewTestValidatorSet(4) // f=1, quorum=3
	keys := make([]*crypto.Ed25519PrivateKey, 2)
	for i := range 2 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	// Only 2 real votes
	votes := make([]*Vote[TestHash], 2)
	for i := range 2 {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	// Attacker adds duplicate vote (same validator voting twice)
	votes = append(votes, votes[0])

	// NewQC should deduplicate and detect insufficient unique votes
	_, err := NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	if err == nil {
		t.Error("Byzantine attack not detected: QC formed with <2f+1 unique votes")
	}
}

// BenchmarkQCFormation_4Nodes benchmarks QC formation for minimal BFT network.
func BenchmarkQCFormation_4Nodes(b *testing.B) {
	benchmarkQCFormation(b, 4)
}

// BenchmarkQCFormation_7Nodes benchmarks QC formation for standard network.
func BenchmarkQCFormation_7Nodes(b *testing.B) {
	benchmarkQCFormation(b, 7)
}

// BenchmarkQCFormation_10Nodes benchmarks QC formation for medium network.
func BenchmarkQCFormation_10Nodes(b *testing.B) {
	benchmarkQCFormation(b, 10)
}

// BenchmarkQCFormation_22Nodes benchmarks QC formation for larger network.
func BenchmarkQCFormation_22Nodes(b *testing.B) {
	benchmarkQCFormation(b, 22)
}

func benchmarkQCFormation(b *testing.B, n int) {
	validators := NewTestValidatorSet(n)
	quorum := (2*((n-1)/3) + 1) // 2f+1
	keys := make([]*crypto.Ed25519PrivateKey, quorum)
	for i := range quorum {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	view := uint32(1)
	nodeHash := NewTestHash("block-1")

	votes := make([]*Vote[TestHash], quorum)
	for i := range quorum {
		votes[i], _ = NewVote(view, nodeHash, uint16(i), keys[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = NewQC(view, nodeHash, votes, validators, CryptoSchemeEd25519)
	}
}

// BenchmarkQCValidation_4Nodes benchmarks QC validation for minimal BFT network.
func BenchmarkQCValidation_4Nodes(b *testing.B) {
	benchmarkQCValidation(b, 4)
}

// BenchmarkQCValidation_7Nodes benchmarks QC validation for standard network.
func BenchmarkQCValidation_7Nodes(b *testing.B) {
	benchmarkQCValidation(b, 7)
}

// BenchmarkQCValidation_10Nodes benchmarks QC validation for medium network.
func BenchmarkQCValidation_10Nodes(b *testing.B) {
	benchmarkQCValidation(b, 10)
}

// BenchmarkQCValidation_22Nodes benchmarks QC validation for larger network.
func BenchmarkQCValidation_22Nodes(b *testing.B) {
	benchmarkQCValidation(b, 22)
}

func benchmarkQCValidation(b *testing.B, n int) {
	validators := NewTestValidatorSet(n)
	quorum := (2*((n-1)/3) + 1) // 2f+1
	keys := make([]*crypto.Ed25519PrivateKey, quorum)
	for i := range quorum {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	votes := make([]*Vote[TestHash], quorum)
	for i := range quorum {
		votes[i], _ = NewVote(1, NewTestHash("block-1"), uint16(i), keys[i])
	}

	qc, _ := NewQC(1, NewTestHash("block-1"), votes, validators, CryptoSchemeEd25519)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = qc.Validate(validators)
	}
}

// BenchmarkQCSerialization benchmarks QC serialization performance.
func BenchmarkQCSerialization(b *testing.B) {
	validators := NewTestValidatorSet(7)
	keys := make([]*crypto.Ed25519PrivateKey, 5)
	for i := range 5 {
		keys[i], _ = crypto.GenerateEd25519Key()
	}

	votes := make([]*Vote[TestHash], 5)
	for i := range 5 {
		votes[i], _ = NewVote(1, NewTestHash("block-1"), uint16(i), keys[i])
	}

	qc, _ := NewQC(1, NewTestHash("block-1"), votes, validators, CryptoSchemeEd25519)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = qc.Bytes()
	}
}

// TestBLSAggregationInQC tests that BLS signature aggregation works in QC formation.
//
// This test verifies:
// 1. BLS votes can be created using NewBLSVote
// 2. QC formation with BLS aggregates signatures into O(1) size
// 3. QC validation with BLS correctly verifies the aggregate signature
func TestBLSAggregationInQC(t *testing.T) {
	// Create 4 BLS key pairs
	blsKeys := make([]*crypto.BLSPrivateKey, 4)
	blsPubKeys := make([]*crypto.BLSPublicKey, 4)
	for i := range 4 {
		var err error
		blsKeys[i], err = crypto.GenerateBLSKey()
		if err != nil {
			t.Fatalf("Failed to generate BLS key %d: %v", i, err)
		}
		blsPubKeys[i] = blsKeys[i].PublicKey()
	}

	// Create a BLS validator set
	validators := NewTestBLSValidatorSet(blsPubKeys)

	// Create BLS votes (3 out of 4 validators)
	view := uint32(1)
	nodeHash := NewTestHash("block-1")
	votes := make([]*Vote[TestHash], 3)

	for i := range 3 {
		var err error
		votes[i], err = NewBLSVote(view, nodeHash, uint16(i), &blsSignerWrapper{blsKeys[i]})
		if err != nil {
			t.Fatalf("Failed to create BLS vote %d: %v", i, err)
		}
	}

	// Create QC with BLS aggregation
	qc, err := NewQC(view, nodeHash, votes, validators, CryptoSchemeBLS)
	if err != nil {
		t.Fatalf("Failed to create BLS QC: %v", err)
	}

	// Verify QC properties
	if qc.CryptoScheme() != CryptoSchemeBLS {
		t.Errorf("Expected BLS crypto scheme, got %s", qc.CryptoScheme())
	}

	if len(qc.Signers()) != 3 {
		t.Errorf("Expected 3 signers, got %d", len(qc.Signers()))
	}

	// BLS aggregate signature should be 48 bytes (G1 point)
	if len(qc.AggregateSignature()) != 48 {
		t.Errorf("Expected 48-byte BLS aggregate signature, got %d bytes", len(qc.AggregateSignature()))
	}

	// Validate QC
	if err := qc.Validate(validators); err != nil {
		t.Errorf("QC validation failed: %v", err)
	}

	t.Logf("BLS QC created successfully: %d signers, %d-byte aggregate signature",
		len(qc.Signers()), len(qc.AggregateSignature()))
}

// blsSignerWrapper wraps a BLS private key to implement BLSSigner interface.
type blsSignerWrapper struct {
	key *crypto.BLSPrivateKey
}

func (s *blsSignerWrapper) SignBLS(message []byte) ([]byte, error) {
	sig, err := s.key.Sign(message)
	if err != nil {
		return nil, err
	}
	return sig.Bytes(), nil
}

// TestBLSValidatorSet is a test validator set for BLS testing.
type TestBLSValidatorSet struct {
	pubKeys []*crypto.BLSPublicKey
}

// NewTestBLSValidatorSet creates a validator set from BLS public keys.
func NewTestBLSValidatorSet(pubKeys []*crypto.BLSPublicKey) *TestBLSValidatorSet {
	return &TestBLSValidatorSet{pubKeys: pubKeys}
}

func (v *TestBLSValidatorSet) Count() int {
	return len(v.pubKeys)
}

func (v *TestBLSValidatorSet) GetByIndex(index uint16) (PublicKey, error) {
	if int(index) >= len(v.pubKeys) {
		return nil, fmt.Errorf("validator index %d out of range", index)
	}
	return &blsPublicKeyWrapper{v.pubKeys[index]}, nil
}

func (v *TestBLSValidatorSet) Contains(index uint16) bool {
	return int(index) < len(v.pubKeys)
}

func (v *TestBLSValidatorSet) GetPublicKeys(indices []uint16) ([]PublicKey, error) {
	keys := make([]PublicKey, len(indices))
	for i, idx := range indices {
		pk, err := v.GetByIndex(idx)
		if err != nil {
			return nil, err
		}
		keys[i] = pk
	}
	return keys, nil
}

func (v *TestBLSValidatorSet) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(len(v.pubKeys)))
}

func (v *TestBLSValidatorSet) F() int {
	return (len(v.pubKeys) - 1) / 3
}

// blsPublicKeyWrapper wraps a BLS public key to implement PublicKey interface.
type blsPublicKeyWrapper struct {
	key *crypto.BLSPublicKey
}

func (pk *blsPublicKeyWrapper) Verify(message, signature []byte) bool {
	sig, err := crypto.BLSSignatureFromBytes(signature)
	if err != nil {
		return false
	}
	return pk.key.Verify(message, sig)
}

func (pk *blsPublicKeyWrapper) Bytes() []byte {
	return pk.key.Bytes()
}

func (pk *blsPublicKeyWrapper) Equals(other interface{ Bytes() []byte }) bool {
	if other == nil {
		return false
	}
	return bytes.Equal(pk.Bytes(), other.Bytes())
}

func (pk *blsPublicKeyWrapper) String() string {
	return fmt.Sprintf("BLSPublicKey(%x...)", pk.Bytes()[:8])
}
