package hotstuff2

import (
	"encoding/binary"
	"fmt"
)

// ConsensusMessage represents a message in the HotStuff2 protocol.
//
// Maps to TLA+ Message types:
// - PROPOSE: Leader broadcasts block with justification QC
// - VOTE: Replica votes for a proposal
// - NEWVIEW: Replica sends view change message with highQC
type ConsensusMessage[H Hash] struct {
	msgType        MessageType
	view           uint32
	validatorIndex uint16

	// PROPOSE fields
	block     Block[H]
	justifyQC *QC[H]

	// VOTE fields
	vote *Vote[H]

	// NEWVIEW fields
	highQC *QC[H]
}

// NewProposeMessage creates a PROPOSE message.
func NewProposeMessage[H Hash](
	view uint32,
	validatorIndex uint16,
	block Block[H],
	justifyQC *QC[H],
) *ConsensusMessage[H] {
	return &ConsensusMessage[H]{
		msgType:        MessageProposal,
		view:           view,
		validatorIndex: validatorIndex,
		block:          block,
		justifyQC:      justifyQC,
	}
}

// NewVoteMessage creates a VOTE message.
func NewVoteMessage[H Hash](
	view uint32,
	validatorIndex uint16,
	vote *Vote[H],
) *ConsensusMessage[H] {
	return &ConsensusMessage[H]{
		msgType:        MessageVote,
		view:           view,
		validatorIndex: validatorIndex,
		vote:           vote,
	}
}

// NewNewViewMessage creates a NEWVIEW message.
func NewNewViewMessage[H Hash](
	view uint32,
	validatorIndex uint16,
	highQC *QC[H],
) *ConsensusMessage[H] {
	return &ConsensusMessage[H]{
		msgType:        MessageNewView,
		view:           view,
		validatorIndex: validatorIndex,
		highQC:         highQC,
	}
}

// Type returns the message type.
func (m *ConsensusMessage[H]) Type() MessageType {
	return m.msgType
}

// View returns the view number.
func (m *ConsensusMessage[H]) View() uint32 {
	return m.view
}

// ValidatorIndex returns the sender's validator index.
func (m *ConsensusMessage[H]) ValidatorIndex() uint16 {
	return m.validatorIndex
}

// Block returns the proposed block (PROPOSE only).
func (m *ConsensusMessage[H]) Block() Block[H] {
	return m.block
}

// JustifyQC returns the justification QC (PROPOSE only).
func (m *ConsensusMessage[H]) JustifyQC() *QC[H] {
	return m.justifyQC
}

// Vote returns the vote (VOTE only).
func (m *ConsensusMessage[H]) Vote() *Vote[H] {
	return m.vote
}

// HighQC returns the highest QC (NEWVIEW only).
func (m *ConsensusMessage[H]) HighQC() *QC[H] {
	return m.highQC
}

// Bytes serializes the message to bytes.
// Format: [type:1][view:4][validatorIndex:2][payload...]
func (m *ConsensusMessage[H]) Bytes() []byte {
	// Header: type (1) + view (4) + validatorIndex (2) = 7 bytes
	header := make([]byte, 7)
	header[0] = byte(m.msgType)
	binary.BigEndian.PutUint32(header[1:5], m.view)
	binary.BigEndian.PutUint16(header[5:7], m.validatorIndex)

	var payload []byte

	switch m.msgType {
	case MessageProposal:
		payload = m.serializePropose()
	case MessageVote:
		payload = m.serializeVote()
	case MessageNewView:
		payload = m.serializeNewView()
	}

	return append(header, payload...)
}

func (m *ConsensusMessage[H]) serializePropose() []byte {
	// Format: [blockLen:4][block][hasQC:1][qc?]
	blockBytes := m.block.Bytes()
	blockLen := len(blockBytes)

	var qcBytes []byte
	hasQC := byte(0)
	if m.justifyQC != nil {
		hasQC = 1
		qcBytes = m.justifyQC.Bytes()
	}

	result := make([]byte, 4+blockLen+1+len(qcBytes))
	binary.BigEndian.PutUint32(result[0:4], uint32(blockLen))
	copy(result[4:], blockBytes)
	result[4+blockLen] = hasQC
	if hasQC == 1 {
		copy(result[4+blockLen+1:], qcBytes)
	}

	return result
}

func (m *ConsensusMessage[H]) serializeVote() []byte {
	return m.vote.Bytes()
}

func (m *ConsensusMessage[H]) serializeNewView() []byte {
	// Format: [hasQC:1][qc?]
	var qcBytes []byte
	hasQC := byte(0)
	if m.highQC != nil {
		hasQC = 1
		qcBytes = m.highQC.Bytes()
	}

	result := make([]byte, 1+len(qcBytes))
	result[0] = hasQC
	if hasQC == 1 {
		copy(result[1:], qcBytes)
	}

	return result
}

// Hash returns the hash of this message (implementation depends on block hash).
func (m *ConsensusMessage[H]) Hash() H {
	// For now, return the block hash for PROPOSE, vote nodeHash for VOTE
	// This is a simplified implementation
	var zero H
	switch m.msgType {
	case MessageProposal:
		if m.block != nil {
			return m.block.Hash()
		}
	case MessageVote:
		if m.vote != nil {
			return m.vote.NodeHash()
		}
	}
	return zero
}

// MessageFromBytes deserializes a ConsensusMessage from bytes.
func MessageFromBytes[H Hash](
	data []byte,
	hashFromBytes func([]byte) (H, error),
	blockFromBytes func([]byte) (Block[H], error),
) (*ConsensusMessage[H], error) {
	if len(data) < 7 {
		return nil, fmt.Errorf("data too short for message header")
	}

	msgType := MessageType(data[0])
	view := binary.BigEndian.Uint32(data[1:5])
	validatorIndex := binary.BigEndian.Uint16(data[5:7])
	payload := data[7:]

	switch msgType {
	case MessageProposal:
		return deserializePropose(view, validatorIndex, payload, hashFromBytes, blockFromBytes)
	case MessageVote:
		return deserializeVote(view, validatorIndex, payload, hashFromBytes)
	case MessageNewView:
		return deserializeNewView(view, validatorIndex, payload, hashFromBytes)
	default:
		return nil, fmt.Errorf("unknown message type: %d", msgType)
	}
}

func deserializePropose[H Hash](
	view uint32,
	validatorIndex uint16,
	payload []byte,
	hashFromBytes func([]byte) (H, error),
	blockFromBytes func([]byte) (Block[H], error),
) (*ConsensusMessage[H], error) {
	if len(payload) < 5 {
		return nil, fmt.Errorf("payload too short for PROPOSE")
	}

	blockLen := binary.BigEndian.Uint32(payload[0:4])
	if len(payload) < int(4+blockLen+1) {
		return nil, fmt.Errorf("payload too short for block")
	}

	blockBytes := payload[4 : 4+blockLen]
	block, err := blockFromBytes(blockBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize block: %w", err)
	}

	hasQC := payload[4+blockLen]
	var justifyQC *QC[H]

	if hasQC == 1 {
		qcBytes := payload[4+blockLen+1:]
		qc, err := QCFromBytes(qcBytes, hashFromBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize QC: %w", err)
		}
		justifyQC = qc
	}

	return &ConsensusMessage[H]{
		msgType:        MessageProposal,
		view:           view,
		validatorIndex: validatorIndex,
		block:          block,
		justifyQC:      justifyQC,
	}, nil
}

func deserializeVote[H Hash](
	view uint32,
	validatorIndex uint16,
	payload []byte,
	hashFromBytes func([]byte) (H, error),
) (*ConsensusMessage[H], error) {
	vote, err := VoteFromBytes(payload, hashFromBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize vote: %w", err)
	}

	return &ConsensusMessage[H]{
		msgType:        MessageVote,
		view:           view,
		validatorIndex: validatorIndex,
		vote:           vote,
	}, nil
}

func deserializeNewView[H Hash](
	view uint32,
	validatorIndex uint16,
	payload []byte,
	hashFromBytes func([]byte) (H, error),
) (*ConsensusMessage[H], error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("payload too short for NEWVIEW")
	}

	hasQC := payload[0]
	var highQC *QC[H]

	if hasQC == 1 {
		if len(payload) < 2 {
			return nil, fmt.Errorf("payload too short for QC")
		}
		qcBytes := payload[1:]
		qc, err := QCFromBytes(qcBytes, hashFromBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize QC: %w", err)
		}
		highQC = qc
	}

	return &ConsensusMessage[H]{
		msgType:        MessageNewView,
		view:           view,
		validatorIndex: validatorIndex,
		highQC:         highQC,
	}, nil
}
