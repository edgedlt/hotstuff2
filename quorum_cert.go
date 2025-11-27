package hotstuff2

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/edgedlt/hotstuff2/internal/crypto"
)

// QC represents a Quorum Certificate - aggregated proof that 2f+1 validators
// voted for a block in a specific view.
//
// CRITICAL SAFETY: QC validation is the cornerstone of consensus safety.
// A forged QC can cause Byzantine faults and break consensus.
//
// Safety rules (from TLA+ spec):
// - MUST have exactly 2f+1 distinct signers (deduplicated)
// - MUST verify aggregate signature
// - NEVER accept QC without full validation
//
// Attack scenario: Without deduplication, Byzantine nodes could create
// valid-looking QCs with only f+1 real votes by duplicating signatures.
type QC[H Hash] struct {
	view               uint32
	node               H
	signers            []uint16 // Deduplicated, sorted validator indices
	timestamps         []uint64 // Corresponding timestamps for each signer (for Ed25519)
	aggregateSignature []byte
	cryptoScheme       string // "ed25519" or "bls"
}

const (
	// CryptoSchemeEd25519 indicates Ed25519 multi-signature (O(n) size).
	CryptoSchemeEd25519 = "ed25519"

	// CryptoSchemeBLS indicates BLS12-381 aggregate signature (O(1) size).
	CryptoSchemeBLS = "bls"
)

// NewQC creates a new QC from a set of votes.
//
// CRITICAL: This function deduplicates signers before forming the QC.
// Duplicate votes from the same validator MUST be rejected to prevent
// Byzantine attacks where <2f+1 real votes appear as a valid QC.
//
// The cryptoScheme parameter determines how signatures are aggregated:
// - "ed25519": Concatenate signatures (O(n) size)
// - "bls": Aggregate signatures using BLS12-381 (O(1) size)
func NewQC[H Hash](
	view uint32,
	nodeHash H,
	votes []*Vote[H],
	validators ValidatorSet,
	cryptoScheme string,
) (*QC[H], error) {
	if len(votes) == 0 {
		return nil, fmt.Errorf("no votes provided")
	}

	// Validate all votes are for the same view and block
	for _, vote := range votes {
		if vote.View() != view {
			return nil, fmt.Errorf("vote view %d does not match QC view %d", vote.View(), view)
		}
		if !vote.NodeHash().Equals(nodeHash) {
			return nil, fmt.Errorf("vote nodeHash does not match QC nodeHash")
		}
	}

	// CRITICAL: Deduplicate signers
	// Without this, Byzantine nodes can forge QCs with <2f+1 real votes
	signerSet := make(map[uint16]struct{})
	uniqueVotes := make([]*Vote[H], 0, len(votes))

	for _, vote := range votes {
		idx := vote.ValidatorIndex()
		if _, exists := signerSet[idx]; !exists {
			signerSet[idx] = struct{}{}
			uniqueVotes = append(uniqueVotes, vote)
		}
	}

	// Check quorum size (2f+1)
	quorum := 2*validators.F() + 1
	if len(uniqueVotes) < quorum {
		return nil, fmt.Errorf("insufficient votes: got %d, need %d", len(uniqueVotes), quorum)
	}

	// Extract signers (sorted for determinism)
	signers := make([]uint16, 0, len(uniqueVotes))
	for signer := range signerSet {
		signers = append(signers, signer)
	}
	sort.Slice(signers, func(i, j int) bool {
		return signers[i] < signers[j]
	})

	// Aggregate signatures based on crypto scheme
	var aggregateSig []byte
	var timestamps []uint64
	var err error

	switch cryptoScheme {
	case CryptoSchemeEd25519:
		aggregateSig, timestamps, err = aggregateEd25519Signatures(uniqueVotes, signers)
	case CryptoSchemeBLS:
		aggregateSig, err = aggregateBLSSignatures(uniqueVotes)
	default:
		return nil, fmt.Errorf("unsupported crypto scheme: %s", cryptoScheme)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to aggregate signatures: %w", err)
	}

	return &QC[H]{
		view:               view,
		node:               nodeHash,
		signers:            signers,
		timestamps:         timestamps,
		aggregateSignature: aggregateSig,
		cryptoScheme:       cryptoScheme,
	}, nil
}

// aggregateEd25519Signatures creates an O(n) multi-signature by concatenating.
// Format: [count:2][signer1:2]...[signerN:2][sig1:64]...[sigN:64]
// Returns the aggregate signature bytes and corresponding timestamps for each signer.
func aggregateEd25519Signatures[H Hash](votes []*Vote[H], _ []uint16) ([]byte, []uint64, error) {
	if len(votes) == 0 {
		return nil, nil, fmt.Errorf("no votes to aggregate")
	}

	// Sort votes by validator index for determinism
	sorted := make([]*Vote[H], len(votes))
	copy(sorted, votes)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ValidatorIndex() < sorted[j].ValidatorIndex()
	})

	// Calculate size: 2 bytes count + (2 bytes per signer) + (64 bytes per signature)
	sigSize := 64 // Ed25519 signature size
	size := 2 + len(sorted)*2 + len(sorted)*sigSize
	result := make([]byte, size)

	// Write count
	binary.BigEndian.PutUint16(result[0:2], uint16(len(sorted)))

	// Write signers
	offset := 2
	for _, vote := range sorted {
		binary.BigEndian.PutUint16(result[offset:], vote.ValidatorIndex())
		offset += 2
	}

	// Write signatures and collect timestamps
	timestamps := make([]uint64, len(sorted))
	for i, vote := range sorted {
		sig := vote.Signature()
		if len(sig) != sigSize {
			return nil, nil, fmt.Errorf("invalid Ed25519 signature size: expected %d, got %d", sigSize, len(sig))
		}
		copy(result[offset:], sig)
		offset += sigSize
		timestamps[i] = vote.Timestamp()
	}

	return result, timestamps, nil
}

// aggregateBLSSignatures creates an O(1) aggregate signature using BLS12-381.
// Returns the aggregate signature bytes (48 bytes).
//
// For BLS, all validators sign the SAME common message: view + nodeHash.
// This allows efficient aggregation using BLS signature addition.
//
// The vote.Signature() for BLS votes should already be a BLS signature over
// the common message (view + nodeHash), not the Ed25519 digest which includes timestamp.
func aggregateBLSSignatures[H Hash](votes []*Vote[H]) ([]byte, error) {
	if len(votes) == 0 {
		return nil, fmt.Errorf("no votes to aggregate")
	}

	// Convert vote signatures to BLS signatures
	signatures := make([]*crypto.BLSSignature, 0, len(votes))
	for _, vote := range votes {
		sig, err := crypto.BLSSignatureFromBytes(vote.Signature())
		if err != nil {
			return nil, fmt.Errorf("failed to parse BLS signature from vote %d: %w", vote.ValidatorIndex(), err)
		}
		signatures = append(signatures, sig)
	}

	// Aggregate signatures
	aggSig, err := crypto.AggregateSignatures(signatures)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate BLS signatures: %w", err)
	}

	return aggSig.Bytes(), nil
}

// View returns the view number of this QC.
func (qc *QC[H]) View() uint32 {
	return qc.view
}

// Node returns the hash of the block this QC certifies.
func (qc *QC[H]) Node() H {
	return qc.node
}

// Signers returns the list of validator indices who signed.
// Guaranteed to be deduplicated and sorted.
func (qc *QC[H]) Signers() []uint16 {
	return qc.signers
}

// AggregateSignature returns the aggregated signature bytes.
func (qc *QC[H]) AggregateSignature() []byte {
	return qc.aggregateSignature
}

// CryptoScheme returns the cryptographic scheme used ("ed25519" or "bls").
func (qc *QC[H]) CryptoScheme() string {
	return qc.cryptoScheme
}

// Validate verifies the QC is well-formed and signatures are valid.
//
// CRITICAL SAFETY: This implements the complete QC validation logic.
// Failures here can cause Byzantine faults.
//
// Validation steps:
// 1. Check quorum size (2f+1)
// 2. Verify all signers are valid validators
// 3. Check signers are deduplicated
// 4. Verify aggregate signature
//
// Returns error if any validation fails.
func (qc *QC[H]) Validate(validators ValidatorSet) error {
	// Check quorum size
	quorum := 2*validators.F() + 1
	if len(qc.signers) < quorum {
		return fmt.Errorf("insufficient signers: got %d, need %d", len(qc.signers), quorum)
	}

	// Verify all signers are valid validators
	for _, signerIdx := range qc.signers {
		if !validators.Contains(signerIdx) {
			return fmt.Errorf("invalid signer index: %d", signerIdx)
		}
	}

	// Check signers are deduplicated (should be guaranteed by NewQC, but verify)
	signerSet := make(map[uint16]struct{})
	for _, signerIdx := range qc.signers {
		if _, exists := signerSet[signerIdx]; exists {
			return fmt.Errorf("duplicate signer index: %d", signerIdx)
		}
		signerSet[signerIdx] = struct{}{}
	}

	// Verify aggregate signature based on crypto scheme
	switch qc.cryptoScheme {
	case CryptoSchemeEd25519:
		return qc.validateEd25519(validators)
	case CryptoSchemeBLS:
		return qc.validateBLS(validators)
	default:
		return fmt.Errorf("unsupported crypto scheme: %s", qc.cryptoScheme)
	}
}

// validateEd25519 verifies an Ed25519 multi-signature.
func (qc *QC[H]) validateEd25519(validators ValidatorSet) error {
	// Parse multi-signature format: [count:2][signer1:2]...[sig1:64]...
	data := qc.aggregateSignature
	if len(data) < 2 {
		return fmt.Errorf("aggregate signature too short")
	}

	sigCount := int(binary.BigEndian.Uint16(data[0:2]))
	if sigCount != len(qc.signers) {
		return fmt.Errorf("signer count mismatch: QC has %d signers, signature has %d", len(qc.signers), sigCount)
	}

	sigSize := 64
	expectedSize := 2 + sigCount*2 + sigCount*sigSize
	if len(data) != expectedSize {
		return fmt.Errorf("invalid aggregate signature size: expected %d, got %d", expectedSize, len(data))
	}

	// Read signers from signature
	offset := 2
	sigSigners := make([]uint16, sigCount)
	for i := 0; i < sigCount; i++ {
		sigSigners[i] = binary.BigEndian.Uint16(data[offset:])
		offset += 2
	}

	// Verify signers match QC signers
	for i, signerIdx := range sigSigners {
		if i >= len(qc.signers) || signerIdx != qc.signers[i] {
			return fmt.Errorf("signer mismatch at index %d", i)
		}
	}

	// Verify each signature with its corresponding timestamp
	if len(qc.timestamps) != len(qc.signers) {
		return fmt.Errorf("timestamp count mismatch: got %d, expected %d", len(qc.timestamps), len(qc.signers))
	}

	for i := 0; i < sigCount; i++ {
		signerIdx := sigSigners[i]
		publicKey, err := validators.GetByIndex(signerIdx)
		if err != nil {
			return fmt.Errorf("failed to get public key for signer %d: %w", signerIdx, err)
		}

		// Recreate the vote digest that was signed, including timestamp
		digest := qc.voteDigestForSigner(signerIdx, qc.timestamps[i])

		sig := data[offset : offset+sigSize]
		if !publicKey.Verify(digest, sig) {
			return fmt.Errorf("invalid signature from validator %d", signerIdx)
		}
		offset += sigSize
	}

	return nil
}

// validateBLS verifies a BLS aggregate signature.
//
// For BLS, the aggregate signature is verified against the common message (view + nodeHash)
// using the aggregated public keys of all signers.
func (qc *QC[H]) validateBLS(validators ValidatorSet) error {
	// Parse aggregate signature
	aggSig, err := crypto.BLSSignatureFromBytes(qc.aggregateSignature)
	if err != nil {
		return fmt.Errorf("failed to parse aggregate BLS signature: %w", err)
	}

	// Collect BLS public keys for all signers
	publicKeys := make([]*crypto.BLSPublicKey, 0, len(qc.signers))
	for _, signerIdx := range qc.signers {
		pk, err := validators.GetByIndex(signerIdx)
		if err != nil {
			return fmt.Errorf("failed to get public key for signer %d: %w", signerIdx, err)
		}

		// Convert to BLS public key
		blsPK, err := crypto.BLSPublicKeyFromBytes(pk.Bytes())
		if err != nil {
			return fmt.Errorf("failed to parse BLS public key for signer %d: %w", signerIdx, err)
		}
		publicKeys = append(publicKeys, blsPK)
	}

	// Create the common message that all validators signed: view + nodeHash
	commonMessage := qc.blsCommonMessage()

	// Verify aggregate signature
	if err := crypto.VerifyAggregated(commonMessage, aggSig, publicKeys); err != nil {
		return fmt.Errorf("BLS aggregate signature verification failed: %w", err)
	}

	return nil
}

// blsCommonMessage creates the common message that all BLS signers sign.
// Format: [view:4][nodeHash]
// This is different from Ed25519 which includes validatorIndex and timestamp.
func (qc *QC[H]) blsCommonMessage() []byte {
	nodeHashBytes := qc.node.Bytes()
	message := make([]byte, 4+len(nodeHashBytes))

	binary.BigEndian.PutUint32(message[0:4], qc.view)
	copy(message[4:], nodeHashBytes)

	return message
}

// voteDigestForSigner creates the vote digest that a specific validator signed.
// This reconstructs what the Vote.Digest() method would have produced.
// Format: [view:4][nodeHash][validatorIndex:2][timestamp:8]
func (qc *QC[H]) voteDigestForSigner(validatorIndex uint16, timestamp uint64) []byte {
	nodeHashBytes := qc.node.Bytes()
	digest := make([]byte, 4+len(nodeHashBytes)+2+8)

	binary.BigEndian.PutUint32(digest[0:4], qc.view)
	copy(digest[4:], nodeHashBytes)
	binary.BigEndian.PutUint16(digest[4+len(nodeHashBytes):], validatorIndex)
	binary.BigEndian.PutUint64(digest[4+len(nodeHashBytes)+2:], timestamp)

	return digest
}

// Bytes serializes the QC to bytes.
// Format: [view:4][nodeHashLen:2][nodeHash][signerCount:2][signer1:2]...[timestamp1:8]...[schemeLen:1][scheme][aggSigLen:2][aggSig]
func (qc *QC[H]) Bytes() []byte {
	nodeHashBytes := qc.node.Bytes()
	schemeBytes := []byte(qc.cryptoScheme)

	// Calculate size
	size := 4 + 2 + len(nodeHashBytes) + 2 + len(qc.signers)*2 + len(qc.timestamps)*8 + 1 + len(schemeBytes) + 2 + len(qc.aggregateSignature)
	result := make([]byte, size)

	offset := 0

	// Write view
	binary.BigEndian.PutUint32(result[offset:], qc.view)
	offset += 4

	// Write nodeHash
	binary.BigEndian.PutUint16(result[offset:], uint16(len(nodeHashBytes)))
	offset += 2
	copy(result[offset:], nodeHashBytes)
	offset += len(nodeHashBytes)

	// Write signers
	binary.BigEndian.PutUint16(result[offset:], uint16(len(qc.signers)))
	offset += 2
	for _, signer := range qc.signers {
		binary.BigEndian.PutUint16(result[offset:], signer)
		offset += 2
	}

	// Write timestamps
	for _, timestamp := range qc.timestamps {
		binary.BigEndian.PutUint64(result[offset:], timestamp)
		offset += 8
	}

	// Write crypto scheme
	result[offset] = byte(len(schemeBytes))
	offset++
	copy(result[offset:], schemeBytes)
	offset += len(schemeBytes)

	// Write aggregate signature
	binary.BigEndian.PutUint16(result[offset:], uint16(len(qc.aggregateSignature)))
	offset += 2
	copy(result[offset:], qc.aggregateSignature)

	return result
}

// QCFromBytes reconstructs a QC from serialized bytes.
func QCFromBytes[H Hash](data []byte, hashFromBytes func([]byte) (H, error)) (*QC[H], error) {
	if len(data) < 4+2+2+1+2 {
		return nil, fmt.Errorf("data too short for QC")
	}

	offset := 0

	// Read view
	view := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Read nodeHash
	nodeHashLen := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2
	if len(data) < offset+nodeHashLen {
		return nil, fmt.Errorf("data too short for nodeHash")
	}
	nodeHashBytes := data[offset : offset+nodeHashLen]
	nodeHash, err := hashFromBytes(nodeHashBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nodeHash: %w", err)
	}
	offset += nodeHashLen

	// Read signers
	signerCount := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2
	if len(data) < offset+signerCount*2 {
		return nil, fmt.Errorf("data too short for signers")
	}
	signers := make([]uint16, signerCount)
	for i := 0; i < signerCount; i++ {
		signers[i] = binary.BigEndian.Uint16(data[offset:])
		offset += 2
	}

	// Read timestamps
	if len(data) < offset+signerCount*8 {
		return nil, fmt.Errorf("data too short for timestamps")
	}
	timestamps := make([]uint64, signerCount)
	for i := range signerCount {
		timestamps[i] = binary.BigEndian.Uint64(data[offset:])
		offset += 8
	}

	// Read crypto scheme
	if len(data) < offset+1 {
		return nil, fmt.Errorf("data too short for crypto scheme")
	}
	schemeLen := int(data[offset])
	offset++
	if len(data) < offset+schemeLen {
		return nil, fmt.Errorf("data too short for crypto scheme string")
	}
	cryptoScheme := string(data[offset : offset+schemeLen])
	offset += schemeLen

	// Read aggregate signature
	if len(data) < offset+2 {
		return nil, fmt.Errorf("data too short for aggregate signature length")
	}
	aggSigLen := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2
	if len(data) < offset+aggSigLen {
		return nil, fmt.Errorf("data too short for aggregate signature")
	}
	aggregateSignature := make([]byte, aggSigLen)
	copy(aggregateSignature, data[offset:offset+aggSigLen])

	return &QC[H]{
		view:               view,
		node:               nodeHash,
		signers:            signers,
		timestamps:         timestamps,
		aggregateSignature: aggregateSignature,
		cryptoScheme:       cryptoScheme,
	}, nil
}
