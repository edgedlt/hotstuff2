// Package hotstuff2 implements the HotStuff-2 consensus algorithm.
//
// HotStuff-2 is a two-phase Byzantine Fault Tolerant (BFT) consensus protocol
// that achieves optimistic responsiveness and linear view-change complexity.
//
// This implementation follows the architecture patterns from nspcc-dev/dbft
// for compatibility with NeoGo and uses generic types for flexibility.
package hotstuff2

// Hash represents a cryptographic hash function output.
// Implementations must be comparable and suitable for use as map keys.
type Hash interface {
	// Bytes returns the raw byte representation of the hash.
	Bytes() []byte

	// Equals returns true if this hash equals the other hash.
	// Must be consistent with Bytes() comparison.
	Equals(other Hash) bool

	// String returns a human-readable representation (typically hex-encoded).
	String() string
}

// PublicKey represents a validator's public key for signature verification.
type PublicKey interface {
	// Bytes returns the raw byte representation of the public key.
	Bytes() []byte

	// Verify verifies a signature over the given message.
	// Returns true if the signature is valid for this public key.
	Verify(message []byte, signature []byte) bool

	// Equals returns true if this public key equals the other.
	// Accepts interface{} for flexibility with different implementations.
	Equals(other interface{ Bytes() []byte }) bool

	// String returns a human-readable representation of the public key.
	String() string
}

// PrivateKey represents a validator's private key for signing messages.
type PrivateKey interface {
	// PublicKey returns the corresponding public key.
	// Returns a type that implements PublicKey interface methods.
	PublicKey() interface {
		Bytes() []byte
		Verify(message []byte, signature []byte) bool
		Equals(other interface{ Bytes() []byte }) bool
		String() string
	}

	// Sign signs the given message and returns the signature.
	// Returns an error if signing fails.
	Sign(message []byte) ([]byte, error)

	// Bytes returns the raw byte representation of the private key.
	// WARNING: Handle with care - this exposes sensitive key material.
	Bytes() []byte
}

// Block represents a proposed block in the consensus protocol.
// Blocks form a tree structure via parent links.
//
// The Block interface is intentionally minimal and payload-agnostic.
// Consensus treats block content as opaque bytes, allowing integration
// with various execution models:
//   - Traditional transactions (serialize tx list into Payload)
//   - DAG-based mempools like Narwhal (payload contains vertex references)
//   - Rollup batches (payload contains batch commitments)
//   - Any other application-specific data
type Block[H Hash] interface {
	// Hash returns the unique identifier for this block.
	// Also referred to as "nodeHash" in the HotStuff-2 paper.
	Hash() H

	// Height returns the block height (0 for genesis).
	// Also referred to as block number or sequence number.
	Height() uint32

	// PrevHash returns the hash of the parent block.
	// Must reference a valid block in the node tree.
	PrevHash() H

	// Payload returns the application-specific block content.
	// Consensus treats this as opaque bytes - interpretation is left
	// to the Executor implementation.
	//
	// Examples:
	//   - Serialized transaction list
	//   - DAG vertex references (Narwhal-style)
	//   - Rollup batch commitment
	//   - Empty (for empty blocks)
	Payload() []byte

	// ProposerIndex returns the validator index of the block proposer.
	ProposerIndex() uint16

	// Timestamp returns the block timestamp (milliseconds since epoch).
	Timestamp() uint64

	// Bytes returns the serialized form of the block.
	Bytes() []byte
}

// QuorumCertificate represents an aggregated certificate from 2f+1 validators.
//
// CRITICAL SAFETY RULES:
// QC validation is the most critical part of consensus safety. A forged QC can
// cause replicas to commit conflicting blocks, violating Byzantine fault tolerance.
//
// Implementations MUST:
//   - Validate signer set has exactly 2f+1 distinct validators (not more, not less)
//   - Deduplicate signers before accepting QC (prevents forged QCs via duplicate votes)
//   - Verify aggregate signature matches node digest + view number
//   - NEVER accept QC without full validation - even from trusted sources
//
// Attack scenario: If duplicate signers are allowed, a Byzantine node could create
// a valid-looking QC with only f+1 real votes by duplicating each signature.
type QuorumCertificate[H Hash] interface {
	// Node returns the hash of the block this QC certifies.
	Node() H

	// View returns the view number in which this QC was formed.
	View() uint32

	// Signers returns the list of validator indices who signed.
	// MUST be deduplicated and sorted.
	Signers() []uint16

	// AggregateSignature returns the aggregated signature.
	AggregateSignature() []byte

	// Bytes returns the serialized form of the QC.
	Bytes() []byte

	// Validate verifies the QC is well-formed and signatures are valid.
	// Returns error if:
	//   - Signer count < 2f+1
	//   - Duplicate signers found
	//   - Invalid signer indices
	//   - Aggregate signature verification fails
	Validate(validators ValidatorSet) error
}

// ValidatorSet represents the current set of consensus validators.
type ValidatorSet interface {
	// Count returns the total number of validators.
	Count() int

	// GetByIndex returns the public key for a validator by index.
	// Returns error if index is out of bounds.
	GetByIndex(index uint16) (PublicKey, error)

	// Contains returns true if the validator index is valid.
	Contains(index uint16) bool

	// GetPublicKeys returns public keys for the given validator indices.
	// Used for aggregate signature verification.
	GetPublicKeys(indices []uint16) ([]PublicKey, error)

	// GetLeader returns the validator index of the leader for a given view.
	// Typically implements round-robin: view % count.
	GetLeader(view uint32) uint16

	// F returns the maximum number of Byzantine validators tolerated.
	// Must return (n-1)/3 where n is the total validator count.
	F() int
}

// Storage provides persistent storage for consensus state.
//
// CRITICAL SAFETY: All Put operations must be durable before returning.
// The locked QC and view number must be persisted atomically to prevent
// safety violations after crash recovery.
type Storage[H Hash] interface {
	// GetBlock retrieves a block by its hash.
	GetBlock(hash H) (Block[H], error)

	// PutBlock persists a block.
	// Must complete durably before returning.
	PutBlock(block Block[H]) error

	// GetLastBlock returns the most recently committed block.
	GetLastBlock() (Block[H], error)

	// GetQC retrieves the QC that certifies the given block.
	GetQC(nodeHash H) (QuorumCertificate[H], error)

	// PutQC persists a QC.
	// Must complete durably before returning.
	PutQC(qc QuorumCertificate[H]) error

	// GetHighestLockedQC returns the highest locked QC.
	// This is safety-critical for crash recovery.
	GetHighestLockedQC() (QuorumCertificate[H], error)

	// PutHighestLockedQC persists the highest locked QC.
	// Must complete durably before returning.
	PutHighestLockedQC(qc QuorumCertificate[H]) error

	// GetView returns the current view number.
	GetView() (uint32, error)

	// PutView persists the current view number.
	// Must complete durably before returning.
	PutView(view uint32) error

	// Close releases any resources held by the storage.
	Close() error
}

// Network provides message broadcasting and delivery.
type Network[H Hash] interface {
	// Broadcast sends a message to all validators.
	Broadcast(payload ConsensusPayload[H])

	// SendTo sends a message to a specific validator.
	SendTo(validatorIndex uint16, payload ConsensusPayload[H])

	// Receive returns a channel for receiving consensus messages.
	// The channel should be buffered to prevent message loss.
	Receive() <-chan ConsensusPayload[H]

	// Close releases any resources held by the network.
	Close() error
}

// Executor handles block execution and validation.
type Executor[H Hash] interface {
	// Execute applies a block's payload and returns the resulting state hash.
	// Must be deterministic - same input always produces same output.
	Execute(block Block[H]) (stateHash H, err error)

	// Verify checks if a block is valid before voting.
	// Should validate:
	//   - Parent block exists
	//   - Payload is well-formed (application-specific)
	//   - Block height is correct
	//   - Any application-specific rules
	Verify(block Block[H]) error

	// GetStateHash returns the current state hash after all executed blocks.
	GetStateHash() H

	// CreateBlock creates a new block proposal.
	// Called when this validator is the leader.
	// The implementation decides what payload to include (e.g., transactions
	// from mempool, DAG references, rollup batch, etc.).
	CreateBlock(height uint32, prevHash H, proposerIndex uint16) (Block[H], error)
}

// Timer provides timeout management for the pacemaker.
type Timer interface {
	// Start starts the timer with the given duration in milliseconds.
	Start(duration uint64)

	// Stop stops the timer.
	Stop()

	// Reset resets the timer with a new duration.
	Reset(duration uint64)

	// C returns a channel that receives when the timer expires.
	C() <-chan struct{}
}

// ConsensusPayload represents a message in the consensus protocol.
type ConsensusPayload[H Hash] interface {
	// Type returns the message type (PROPOSE, VOTE, NEWVIEW).
	Type() MessageType

	// View returns the view number for this message.
	View() uint32

	// ValidatorIndex returns the index of the validator who created this message.
	ValidatorIndex() uint16

	// Bytes returns the serialized form of the payload.
	Bytes() []byte

	// Hash returns the hash of the payload for signing.
	Hash() H
}

// MessageType represents the type of consensus message.
type MessageType uint8

const (
	// MessageProposal represents a block proposal from the leader.
	MessageProposal MessageType = iota

	// MessageVote represents a vote for a proposal.
	MessageVote

	// MessageNewView represents a new view message during view change.
	MessageNewView
)

// String returns the string representation of the message type.
func (mt MessageType) String() string {
	switch mt {
	case MessageProposal:
		return "PROPOSAL"
	case MessageVote:
		return "VOTE"
	case MessageNewView:
		return "NEWVIEW"
	default:
		return "UNKNOWN"
	}
}

// ConsensusState provides read-only access to consensus state.
// Use HotStuff2.State() to obtain an instance.
type ConsensusState interface {
	// View returns the current view number.
	View() uint32

	// Height returns the height of the last committed block.
	Height() uint32

	// LockedQCView returns the view of the locked QC, or 0 if none.
	LockedQCView() uint32

	// HighQCView returns the view of the highest QC seen, or 0 if none.
	HighQCView() uint32

	// CommittedCount returns the number of committed blocks.
	CommittedCount() int
}

// Hooks provides callbacks for consensus events.
// All callbacks are optional - nil callbacks are safely ignored.
// Callbacks are invoked synchronously, so implementations should be fast
// or dispatch to a goroutine to avoid blocking consensus.
type Hooks[H Hash] struct {
	// OnPropose is called when this node proposes a block (leader only).
	OnPropose func(view uint32, block Block[H])

	// OnVote is called when this node votes for a proposal.
	OnVote func(view uint32, blockHash H)

	// OnQCFormed is called when a quorum certificate is formed.
	OnQCFormed func(view uint32, qc QuorumCertificate[H])

	// OnCommit is called when a block is committed (finalized).
	OnCommit func(block Block[H])

	// OnViewChange is called when the view changes.
	OnViewChange func(oldView, newView uint32)

	// OnTimeout is called when a view times out.
	OnTimeout func(view uint32)
}
