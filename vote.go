package hotstuff2

import (
	"encoding/binary"
	"fmt"
	"time"
)

// Vote represents a replica's vote for a block proposal.
//
// Votes are the fundamental building blocks of QCs (Quorum Certificates).
// A QC is formed when 2f+1 replicas vote for the same block in the same view.
//
// CRITICAL SAFETY: Votes include replay protection via timestamp validation.
// Replicas must reject votes with timestamps outside acceptable window (60s).
type Vote[H Hash] struct {
	view           uint32
	nodeHash       H
	validatorIndex uint16
	signature      []byte
	timestamp      uint64 // Unix timestamp in milliseconds (replay protection)
}

const (
	// VoteTimestampWindow is the acceptable time window for vote timestamps.
	// Votes with timestamps outside this window are rejected (replay protection).
	VoteTimestampWindow = 60 * 1000 // 60 seconds in milliseconds
)

// NewVote creates a new vote for a block at a given view (Ed25519).
// For BLS votes, use NewBLSVote instead.
func NewVote[H Hash](
	view uint32,
	nodeHash H,
	validatorIndex uint16,
	privateKey PrivateKey,
) (*Vote[H], error) {
	vote := &Vote[H]{
		view:           view,
		nodeHash:       nodeHash,
		validatorIndex: validatorIndex,
		timestamp:      uint64(time.Now().UnixMilli()),
	}

	// Sign the vote digest (Ed25519 format: includes validatorIndex + timestamp)
	digest := vote.Digest()
	sig, err := privateKey.Sign(digest)
	if err != nil {
		return nil, fmt.Errorf("failed to sign vote: %w", err)
	}

	vote.signature = sig
	return vote, nil
}

// BLSSigner is the interface for BLS signature creation.
// This is separate from PrivateKey to support BLS-specific signing.
type BLSSigner interface {
	// SignBLS signs a message using BLS12-381.
	SignBLS(message []byte) ([]byte, error)
}

// NewBLSVote creates a new vote using BLS signature scheme.
// BLS votes sign the common message (view + nodeHash) without validatorIndex or timestamp.
// This enables O(1) signature aggregation in QC formation.
func NewBLSVote[H Hash](
	view uint32,
	nodeHash H,
	validatorIndex uint16,
	signer BLSSigner,
) (*Vote[H], error) {
	vote := &Vote[H]{
		view:           view,
		nodeHash:       nodeHash,
		validatorIndex: validatorIndex,
		timestamp:      0, // Not used for BLS - all validators sign same message
	}

	// Sign the BLS common message (view + nodeHash only)
	digest := vote.BLSDigest()
	sig, err := signer.SignBLS(digest)
	if err != nil {
		return nil, fmt.Errorf("failed to create BLS signature: %w", err)
	}

	vote.signature = sig
	return vote, nil
}

// View returns the view number of this vote.
func (v *Vote[H]) View() uint32 {
	return v.view
}

// NodeHash returns the hash of the block being voted for.
func (v *Vote[H]) NodeHash() H {
	return v.nodeHash
}

// ValidatorIndex returns the index of the validator who created this vote.
func (v *Vote[H]) ValidatorIndex() uint16 {
	return v.validatorIndex
}

// Signature returns the signature of this vote.
func (v *Vote[H]) Signature() []byte {
	return v.signature
}

// Timestamp returns the Unix timestamp (milliseconds) when this vote was created.
func (v *Vote[H]) Timestamp() uint64 {
	return v.timestamp
}

// Digest returns the digest of the vote for signing/verification (Ed25519).
// Digest includes: view + nodeHash + validatorIndex + timestamp
// This is used for Ed25519 signatures which include per-validator data.
func (v *Vote[H]) Digest() []byte {
	// Format: [view:4][nodeHash][validatorIndex:2][timestamp:8]
	nodeHashBytes := v.nodeHash.Bytes()
	digest := make([]byte, 4+len(nodeHashBytes)+2+8)

	binary.BigEndian.PutUint32(digest[0:4], v.view)
	copy(digest[4:], nodeHashBytes)
	binary.BigEndian.PutUint16(digest[4+len(nodeHashBytes):], v.validatorIndex)
	binary.BigEndian.PutUint64(digest[4+len(nodeHashBytes)+2:], v.timestamp)

	return digest
}

// BLSDigest returns the common message for BLS signing.
// Format: [view:4][nodeHash]
// This is the message ALL validators sign when using BLS, enabling aggregation.
// Unlike Digest(), this does NOT include validatorIndex or timestamp.
func (v *Vote[H]) BLSDigest() []byte {
	nodeHashBytes := v.nodeHash.Bytes()
	digest := make([]byte, 4+len(nodeHashBytes))

	binary.BigEndian.PutUint32(digest[0:4], v.view)
	copy(digest[4:], nodeHashBytes)

	return digest
}

// Verify verifies the vote signature with the given public key.
// Also validates timestamp is within acceptable window.
func (v *Vote[H]) Verify(publicKey PublicKey) error {
	// Check timestamp is within acceptable window (replay protection)
	now := uint64(time.Now().UnixMilli())
	timeDiff := int64(now) - int64(v.timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}

	if uint64(timeDiff) > VoteTimestampWindow {
		return fmt.Errorf("vote timestamp outside acceptable window: %d ms", timeDiff)
	}

	// Verify signature
	digest := v.Digest()
	if !publicKey.Verify(digest, v.signature) {
		return fmt.Errorf("invalid vote signature")
	}

	return nil
}

// Bytes serializes the vote to bytes.
// Format: [view:4][nodeHashLen:2][nodeHash][validatorIndex:2][timestamp:8][sigLen:2][signature]
func (v *Vote[H]) Bytes() []byte {
	nodeHashBytes := v.nodeHash.Bytes()
	nodeHashLen := len(nodeHashBytes)
	sigLen := len(v.signature)

	// Calculate total size
	size := 4 + 2 + nodeHashLen + 2 + 8 + 2 + sigLen
	result := make([]byte, size)

	offset := 0

	// Write view
	binary.BigEndian.PutUint32(result[offset:], v.view)
	offset += 4

	// Write nodeHash
	binary.BigEndian.PutUint16(result[offset:], uint16(nodeHashLen))
	offset += 2
	copy(result[offset:], nodeHashBytes)
	offset += nodeHashLen

	// Write validatorIndex
	binary.BigEndian.PutUint16(result[offset:], v.validatorIndex)
	offset += 2

	// Write timestamp
	binary.BigEndian.PutUint64(result[offset:], v.timestamp)
	offset += 8

	// Write signature
	binary.BigEndian.PutUint16(result[offset:], uint16(sigLen))
	offset += 2
	copy(result[offset:], v.signature)

	return result
}

// VoteFromBytes reconstructs a Vote from serialized bytes.
func VoteFromBytes[H Hash](data []byte, hashFromBytes func([]byte) (H, error)) (*Vote[H], error) {
	if len(data) < 4+2+2+8+2 {
		return nil, fmt.Errorf("data too short for vote: %d bytes", len(data))
	}

	offset := 0

	// Read view
	view := binary.BigEndian.Uint32(data[offset:])
	offset += 4

	// Read nodeHash
	nodeHashLen := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2

	if len(data) < offset+nodeHashLen {
		return nil, fmt.Errorf("data too short for nodeHash: expected %d, got %d", offset+nodeHashLen, len(data))
	}

	nodeHashBytes := data[offset : offset+nodeHashLen]
	nodeHash, err := hashFromBytes(nodeHashBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse nodeHash: %w", err)
	}
	offset += nodeHashLen

	// Read validatorIndex
	if len(data) < offset+2 {
		return nil, fmt.Errorf("data too short for validatorIndex")
	}
	validatorIndex := binary.BigEndian.Uint16(data[offset:])
	offset += 2

	// Read timestamp
	if len(data) < offset+8 {
		return nil, fmt.Errorf("data too short for timestamp")
	}
	timestamp := binary.BigEndian.Uint64(data[offset:])
	offset += 8

	// Read signature
	if len(data) < offset+2 {
		return nil, fmt.Errorf("data too short for signature length")
	}
	sigLen := int(binary.BigEndian.Uint16(data[offset:]))
	offset += 2

	if len(data) < offset+sigLen {
		return nil, fmt.Errorf("data too short for signature: expected %d, got %d", offset+sigLen, len(data))
	}

	signature := make([]byte, sigLen)
	copy(signature, data[offset:offset+sigLen])

	return &Vote[H]{
		view:           view,
		nodeHash:       nodeHash,
		validatorIndex: validatorIndex,
		signature:      signature,
		timestamp:      timestamp,
	}, nil
}
