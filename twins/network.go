package twins

import (
	"sync"

	"github.com/edgedlt/hotstuff2"
)

// MessageInterceptor allows Byzantine nodes to intercept and modify messages.
// This is the core mechanism for injecting Byzantine behavior.
type MessageInterceptor[H hotstuff2.Hash] interface {
	// InterceptOutgoing intercepts messages before they are sent.
	// Returns a list of messages to actually send (may be modified, duplicated, or empty).
	// The destination -1 means broadcast to all.
	InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H]

	// InterceptIncoming intercepts messages before delivery to a node.
	// Returns nil to drop the message, or the (possibly modified) message to deliver.
	InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H]
}

// OutgoingMessage represents a message to be sent after interception.
type OutgoingMessage[H hotstuff2.Hash] struct {
	To      int // -1 for broadcast, otherwise specific node ID
	Message hotstuff2.ConsensusPayload[H]
}

// TwinsNetwork implements a network with partition support for twins testing.
type TwinsNetwork[H hotstuff2.Hash] struct {
	mu sync.RWMutex

	// Node channels
	nodeChannels map[int]chan hotstuff2.ConsensusPayload[H]

	// Network configuration
	partitions []Partition
	totalNodes int

	// Message interception
	interceptors map[int]MessageInterceptor[H]
	silentNodes  map[int]bool

	// Message tracking
	messageCount int
	messages     []MessageRecord[H]
}

// MessageRecord records a message for analysis.
type MessageRecord[H hotstuff2.Hash] struct {
	From    int
	To      int
	Message hotstuff2.ConsensusPayload[H]
	View    uint32
	Type    hotstuff2.MessageType
}

// NewTwinsNetwork creates a new twins network.
func NewTwinsNetwork[H hotstuff2.Hash](totalNodes int, partitions []Partition) *TwinsNetwork[H] {
	channels := make(map[int]chan hotstuff2.ConsensusPayload[H])
	for i := range totalNodes {
		channels[i] = make(chan hotstuff2.ConsensusPayload[H], 100)
	}

	return &TwinsNetwork[H]{
		nodeChannels: channels,
		partitions:   partitions,
		totalNodes:   totalNodes,
		interceptors: make(map[int]MessageInterceptor[H]),
		silentNodes:  make(map[int]bool),
		messages:     make([]MessageRecord[H], 0),
	}
}

// SetInterceptor sets the message interceptor for a specific node.
// Pass nil to remove the interceptor.
func (tn *TwinsNetwork[H]) SetInterceptor(nodeID int, interceptor MessageInterceptor[H]) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	if interceptor == nil {
		delete(tn.interceptors, nodeID)
	} else {
		tn.interceptors[nodeID] = interceptor
	}
}

// SetSilent marks a node as silent (crash fault simulation).
// Silent nodes do not send or receive any messages.
func (tn *TwinsNetwork[H]) SetSilent(nodeID int, silent bool) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	if silent {
		tn.silentNodes[nodeID] = true
	} else {
		delete(tn.silentNodes, nodeID)
	}
}

// IsSilent returns whether a node is marked as silent.
func (tn *TwinsNetwork[H]) IsSilent(nodeID int) bool {
	tn.mu.RLock()
	defer tn.mu.RUnlock()
	return tn.silentNodes[nodeID]
}

// NodeNetwork returns a network adapter for a specific node.
func (tn *TwinsNetwork[H]) NodeNetwork(nodeID int) hotstuff2.Network[H] {
	return &TwinsNodeAdapter[H]{
		nodeID:  nodeID,
		network: tn,
	}
}

// broadcast sends a message from one node to all others (respecting partitions).
func (tn *TwinsNetwork[H]) broadcast(from int, msg hotstuff2.ConsensusPayload[H]) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	// Silent nodes don't send messages
	if tn.silentNodes[from] {
		return
	}

	// Apply outgoing interceptor if present
	outgoing := []OutgoingMessage[H]{{To: -1, Message: msg}}
	if interceptor, ok := tn.interceptors[from]; ok {
		outgoing = interceptor.InterceptOutgoing(from, msg)
	}

	// Process each outgoing message
	for _, out := range outgoing {
		if out.To == -1 {
			// Broadcast to all
			tn.broadcastInternal(from, out.Message)
		} else {
			// Send to specific node
			tn.sendToInternal(from, out.To, out.Message)
		}
	}
}

// broadcastInternal sends a message to all nodes (internal, no interception).
func (tn *TwinsNetwork[H]) broadcastInternal(from int, msg hotstuff2.ConsensusPayload[H]) {
	for to := range tn.totalNodes {
		if to == from {
			continue
		}
		tn.sendToInternal(from, to, msg)
	}
}

// sendToInternal sends a message to a specific node (internal, handles partitions and incoming interception).
func (tn *TwinsNetwork[H]) sendToInternal(from, to int, msg hotstuff2.ConsensusPayload[H]) {
	// Check partition
	if tn.isPartitioned(from, to) {
		return
	}

	// Silent nodes don't receive messages
	if tn.silentNodes[to] {
		return
	}

	// Apply incoming interceptor if present
	finalMsg := msg
	if interceptor, ok := tn.interceptors[to]; ok {
		finalMsg = interceptor.InterceptIncoming(to, msg)
		if finalMsg == nil {
			return // Message dropped by interceptor
		}
	}

	tn.messageCount++

	// Record message
	tn.messages = append(tn.messages, MessageRecord[H]{
		From:    from,
		To:      to,
		Message: finalMsg,
		View:    finalMsg.View(),
		Type:    finalMsg.Type(),
	})

	// Send message
	select {
	case tn.nodeChannels[to] <- finalMsg:
	default:
		// Channel full, drop
	}
}

// sendTo sends a message to a specific node.
func (tn *TwinsNetwork[H]) sendTo(from, to int, msg hotstuff2.ConsensusPayload[H]) {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	// Silent nodes don't send messages
	if tn.silentNodes[from] {
		return
	}

	// Apply outgoing interceptor if present
	outgoing := []OutgoingMessage[H]{{To: to, Message: msg}}
	if interceptor, ok := tn.interceptors[from]; ok {
		outgoing = interceptor.InterceptOutgoing(from, msg)
	}

	// Process each outgoing message
	for _, out := range outgoing {
		targetTo := out.To
		if targetTo == -1 {
			targetTo = to // Default to original target if broadcast
		}
		tn.sendToInternal(from, targetTo, out.Message)
	}
}

// isPartitioned checks if two nodes are in different partitions.
func (tn *TwinsNetwork[H]) isPartitioned(from, to int) bool {
	if len(tn.partitions) == 0 {
		return false
	}

	fromPartition := -1
	toPartition := -1

	for i, partition := range tn.partitions {
		for _, node := range partition.Nodes {
			if node == from {
				fromPartition = i
			}
			if node == to {
				toPartition = i
			}
		}
	}

	// If either node is not in any partition, they can communicate
	if fromPartition == -1 || toPartition == -1 {
		return false
	}

	// Different partitions cannot communicate
	return fromPartition != toPartition
}

// MessageCount returns the total number of messages sent.
func (tn *TwinsNetwork[H]) MessageCount() int {
	tn.mu.RLock()
	defer tn.mu.RUnlock()
	return tn.messageCount
}

// GetMessages returns all messages sent (for analysis).
func (tn *TwinsNetwork[H]) GetMessages() []MessageRecord[H] {
	tn.mu.RLock()
	defer tn.mu.RUnlock()
	return append([]MessageRecord[H]{}, tn.messages...)
}

// Close closes all node channels.
func (tn *TwinsNetwork[H]) Close() {
	tn.mu.Lock()
	defer tn.mu.Unlock()

	for _, ch := range tn.nodeChannels {
		close(ch)
	}
}

// TwinsNodeAdapter adapts the twins network for a specific node.
type TwinsNodeAdapter[H hotstuff2.Hash] struct {
	nodeID  int
	network *TwinsNetwork[H]
}

// Broadcast broadcasts a message to all nodes.
func (tna *TwinsNodeAdapter[H]) Broadcast(msg hotstuff2.ConsensusPayload[H]) {
	tna.network.broadcast(tna.nodeID, msg)
}

// SendTo sends a message to a specific validator.
func (tna *TwinsNodeAdapter[H]) SendTo(validatorIndex uint16, msg hotstuff2.ConsensusPayload[H]) {
	tna.network.sendTo(tna.nodeID, int(validatorIndex), msg)
}

// Receive returns the channel for receiving messages.
func (tna *TwinsNodeAdapter[H]) Receive() <-chan hotstuff2.ConsensusPayload[H] {
	tna.network.mu.RLock()
	defer tna.network.mu.RUnlock()
	return tna.network.nodeChannels[tna.nodeID]
}

// Close is a no-op (network is closed centrally).
func (tna *TwinsNodeAdapter[H]) Close() error {
	return nil
}
