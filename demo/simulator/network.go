package simulator

import (
	"math/rand"
	"sync"
	"time"

	hotstuff2 "github.com/edgedlt/hotstuff2"
)

// NetworkSimulator provides a simulated network with fault injection capabilities.
type NetworkSimulator struct {
	mu sync.RWMutex

	// Node channels
	nodeChannels map[int]chan hotstuff2.ConsensusPayload[SimHash]

	// Configuration
	totalNodes int
	config     NetworkConfig

	// Fault state
	partitions   [][]int // Groups of nodes that can communicate
	crashedNodes map[int]bool
	rng          *rand.Rand

	// Statistics
	messagesSent    int
	messagesDropped int
	messagesDelayed int

	// Event hooks for visualization
	onMessageSent func(from, to int, msgType hotstuff2.MessageType, delayed bool)
	onMessageDrop func(from, to int, msgType hotstuff2.MessageType)
}

// NetworkConfig configures network fault injection.
type NetworkConfig struct {
	// PacketLoss is the probability of dropping a message (0.0 - 1.0)
	PacketLoss float64

	// MinLatency is the minimum message delay in milliseconds
	MinLatency uint64

	// MaxLatency is the maximum message delay in milliseconds
	MaxLatency uint64

	// ReplayProbability is the chance of replaying a message (0.0 - 1.0)
	ReplayProbability float64

	// EnablePartitions allows network partitions
	EnablePartitions bool

	// EnableCrashes allows node crashes
	EnableCrashes bool
}

// DefaultNetworkConfig returns a default (no faults) network configuration.
func DefaultNetworkConfig() NetworkConfig {
	return NetworkConfig{
		PacketLoss:        0.0,
		MinLatency:        1,
		MaxLatency:        10,
		ReplayProbability: 0.0,
		EnablePartitions:  false,
		EnableCrashes:     false,
	}
}

// NewNetworkSimulator creates a new network simulator.
func NewNetworkSimulator(totalNodes int, config NetworkConfig, seed int64) *NetworkSimulator {
	channels := make(map[int]chan hotstuff2.ConsensusPayload[SimHash])
	for i := range totalNodes {
		channels[i] = make(chan hotstuff2.ConsensusPayload[SimHash], 100)
	}

	return &NetworkSimulator{
		nodeChannels: channels,
		totalNodes:   totalNodes,
		config:       config,
		crashedNodes: make(map[int]bool),
		rng:          rand.New(rand.NewSource(seed)),
	}
}

// SetOnMessageSent sets the callback for when a message is sent.
func (n *NetworkSimulator) SetOnMessageSent(fn func(from, to int, msgType hotstuff2.MessageType, delayed bool)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onMessageSent = fn
}

// SetOnMessageDrop sets the callback for when a message is dropped.
func (n *NetworkSimulator) SetOnMessageDrop(fn func(from, to int, msgType hotstuff2.MessageType)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onMessageDrop = fn
}

// NodeNetwork returns a network adapter for a specific node.
func (n *NetworkSimulator) NodeNetwork(nodeID int) hotstuff2.Network[SimHash] {
	return &SimNodeNetwork{
		nodeID:  nodeID,
		network: n,
	}
}

// SetPartitions configures network partitions.
// Each partition is a slice of node IDs that can communicate with each other.
func (n *NetworkSimulator) SetPartitions(partitions [][]int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.partitions = partitions
}

// ClearPartitions removes all network partitions.
func (n *NetworkSimulator) ClearPartitions() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.partitions = nil
}

// GetPartitions returns the current network partitions.
func (n *NetworkSimulator) GetPartitions() [][]int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.partitions == nil {
		return nil
	}
	// Return a copy
	result := make([][]int, len(n.partitions))
	for i, p := range n.partitions {
		result[i] = append([]int{}, p...)
	}
	return result
}

// CanCommunicate returns true if two nodes can communicate (not partitioned).
func (n *NetworkSimulator) CanCommunicate(from, to int) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return !n.isPartitioned(from, to)
}

// CrashNode simulates a node crash.
func (n *NetworkSimulator) CrashNode(nodeID int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.crashedNodes[nodeID] = true
}

// RecoverNode recovers a crashed node.
func (n *NetworkSimulator) RecoverNode(nodeID int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.crashedNodes, nodeID)
}

// IsNodeCrashed returns true if the node is crashed.
func (n *NetworkSimulator) IsNodeCrashed(nodeID int) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.crashedNodes[nodeID]
}

// broadcast sends a message from one node to all others.
func (n *NetworkSimulator) broadcast(from int, msg hotstuff2.ConsensusPayload[SimHash]) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Check if sender is crashed
	if n.crashedNodes[from] {
		return
	}

	for to := range n.totalNodes {
		if to == from {
			continue
		}

		n.sendMessageLocked(from, to, msg)
	}
}

// sendTo sends a message to a specific node.
func (n *NetworkSimulator) sendTo(from, to int, msg hotstuff2.ConsensusPayload[SimHash]) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Check if sender is crashed
	if n.crashedNodes[from] {
		return
	}

	n.sendMessageLocked(from, to, msg)
}

// sendMessageLocked sends a message (must hold lock).
func (n *NetworkSimulator) sendMessageLocked(from, to int, msg hotstuff2.ConsensusPayload[SimHash]) {
	// Check if receiver is crashed
	if n.crashedNodes[to] {
		return
	}

	// Check partition
	if n.isPartitioned(from, to) {
		n.messagesDropped++
		if n.onMessageDrop != nil {
			go n.onMessageDrop(from, to, msg.Type())
		}
		return
	}

	// Check packet loss
	if n.config.PacketLoss > 0 && n.rng.Float64() < n.config.PacketLoss {
		n.messagesDropped++
		if n.onMessageDrop != nil {
			go n.onMessageDrop(from, to, msg.Type())
		}
		return
	}

	n.messagesSent++

	// Calculate delay based on config
	var delay time.Duration
	if n.config.MaxLatency > n.config.MinLatency {
		latencyRange := n.config.MaxLatency - n.config.MinLatency
		latency := n.config.MinLatency + uint64(n.rng.Int63n(int64(latencyRange)))
		delay = time.Duration(latency) * time.Millisecond
		n.messagesDelayed++
	} else if n.config.MinLatency > 0 {
		delay = time.Duration(n.config.MinLatency) * time.Millisecond
	}

	// Send message (with delay if configured)
	if delay > 0 {
		// Async delayed delivery
		ch := n.nodeChannels[to]
		onSent := n.onMessageSent
		go func() {
			time.Sleep(delay)
			select {
			case ch <- msg:
				if onSent != nil {
					onSent(from, to, msg.Type(), true)
				}
			default:
				// Channel full, drop silently
			}
		}()
	} else {
		// Immediate delivery
		select {
		case n.nodeChannels[to] <- msg:
			if n.onMessageSent != nil {
				go n.onMessageSent(from, to, msg.Type(), false)
			}
		default:
			// Channel full, drop
			n.messagesDropped++
		}
	}

	// Check for replay
	if n.config.ReplayProbability > 0 && n.rng.Float64() < n.config.ReplayProbability {
		select {
		case n.nodeChannels[to] <- msg:
			n.messagesSent++
		default:
		}
	}
}

// isPartitioned checks if two nodes are in different partitions.
func (n *NetworkSimulator) isPartitioned(from, to int) bool {
	if len(n.partitions) == 0 {
		return false
	}

	fromPartition := -1
	toPartition := -1

	for i, partition := range n.partitions {
		for _, node := range partition {
			if node == from {
				fromPartition = i
			}
			if node == to {
				toPartition = i
			}
		}
	}

	// If either node is not in any partition, they CAN communicate freely
	if fromPartition == -1 || toPartition == -1 {
		return false
	}

	// Different partitions cannot communicate
	return fromPartition != toPartition
}

// Stats returns network statistics.
func (n *NetworkSimulator) Stats() NetworkStats {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return NetworkStats{
		MessagesSent:    n.messagesSent,
		MessagesDropped: n.messagesDropped,
		MessagesDelayed: n.messagesDelayed,
	}
}

// NetworkStats contains network statistics.
type NetworkStats struct {
	MessagesSent    int
	MessagesDropped int
	MessagesDelayed int
}

// Close closes all node channels.
func (n *NetworkSimulator) Close() {
	n.mu.Lock()
	defer n.mu.Unlock()

	for _, ch := range n.nodeChannels {
		close(ch)
	}
}

// SimNodeNetwork adapts the network simulator for a specific node.
type SimNodeNetwork struct {
	nodeID  int
	network *NetworkSimulator
}

// Broadcast broadcasts a message to all nodes.
func (snn *SimNodeNetwork) Broadcast(msg hotstuff2.ConsensusPayload[SimHash]) {
	snn.network.broadcast(snn.nodeID, msg)
}

// SendTo sends a message to a specific validator.
func (snn *SimNodeNetwork) SendTo(validatorIndex uint16, msg hotstuff2.ConsensusPayload[SimHash]) {
	snn.network.sendTo(snn.nodeID, int(validatorIndex), msg)
}

// Receive returns the channel for receiving messages.
func (snn *SimNodeNetwork) Receive() <-chan hotstuff2.ConsensusPayload[SimHash] {
	snn.network.mu.RLock()
	defer snn.network.mu.RUnlock()
	return snn.network.nodeChannels[snn.nodeID]
}

// Close is a no-op (network is closed centrally).
func (snn *SimNodeNetwork) Close() error {
	return nil
}
