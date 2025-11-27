package twins

import (
	"math/rand"
	"sync"

	"github.com/edgedlt/hotstuff2"
)

// VoteAccessor provides access to vote data from a consensus message.
// This is needed because ConsensusPayload interface doesn't expose Vote().
type VoteAccessor[H hotstuff2.Hash] interface {
	Vote() *hotstuff2.Vote[H]
}

// getVoteFromMessage attempts to extract vote from a consensus message.
func getVoteFromMessage[H hotstuff2.Hash](msg hotstuff2.ConsensusPayload[H]) *hotstuff2.Vote[H] {
	if accessor, ok := msg.(VoteAccessor[H]); ok {
		return accessor.Vote()
	}
	return nil
}

// DoubleSignInterceptor creates conflicting votes to test double-sign detection.
// When a twin sends a VOTE message, this interceptor records a conflicting vote
// to simulate the twin signing different blocks in the same view.
type DoubleSignInterceptor[H hotstuff2.Hash] struct {
	nodeID   int
	replicas int
	detector *ViolationDetector[H]
	mu       sync.Mutex
	// Track which views we've already double-signed to avoid duplicates
	doubleSignedViews map[uint32]bool
}

// NewDoubleSignInterceptor creates a new double-sign interceptor.
func NewDoubleSignInterceptor[H hotstuff2.Hash](nodeID, replicas int, detector *ViolationDetector[H]) *DoubleSignInterceptor[H] {
	return &DoubleSignInterceptor[H]{
		nodeID:            nodeID,
		replicas:          replicas,
		detector:          detector,
		doubleSignedViews: make(map[uint32]bool),
	}
}

// InterceptOutgoing intercepts outgoing messages and potentially creates conflicting votes.
func (dsi *DoubleSignInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	// Only intercept VOTE messages
	if msg.Type() != hotstuff2.MessageVote {
		return []OutgoingMessage[H]{{To: -1, Message: msg}}
	}

	vote := getVoteFromMessage(msg)
	if vote == nil {
		return []OutgoingMessage[H]{{To: -1, Message: msg}}
	}

	dsi.mu.Lock()
	defer dsi.mu.Unlock()

	// Record the original vote for detection
	if dsi.detector != nil {
		dsi.detector.RecordVote(from, vote)
	}

	// Check if we've already double-signed this view
	if dsi.doubleSignedViews[vote.View()] {
		return []OutgoingMessage[H]{{To: -1, Message: msg}}
	}
	dsi.doubleSignedViews[vote.View()] = true

	// Simulate double-signing by recording a conflicting vote in the detector.
	// In a real Byzantine node, this would be a second valid signature for a
	// different block. Here we simulate detection by recording the conflict.
	if dsi.detector != nil {
		dsi.detector.RecordConflictingVote(from, vote.ValidatorIndex(), vote.View(), vote.NodeHash())
	}

	// Send the original vote normally
	return []OutgoingMessage[H]{{To: -1, Message: msg}}
}

// InterceptIncoming passes through incoming messages unchanged.
func (dsi *DoubleSignInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	return msg
}

// EquivocationInterceptor sends different messages to different partitions.
type EquivocationInterceptor[H hotstuff2.Hash] struct {
	nodeID     int
	partitions []Partition
}

// NewEquivocationInterceptor creates a new equivocation interceptor.
func NewEquivocationInterceptor[H hotstuff2.Hash](nodeID int, partitions []Partition) *EquivocationInterceptor[H] {
	return &EquivocationInterceptor[H]{
		nodeID:     nodeID,
		partitions: partitions,
	}
}

// InterceptOutgoing may send different messages to different partitions.
func (ei *EquivocationInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	// If no partitions defined, behave normally
	if len(ei.partitions) < 2 {
		return []OutgoingMessage[H]{{To: -1, Message: msg}}
	}

	// For VOTE and PROPOSE messages, we could theoretically send different
	// content to different partitions. However, without access to create
	// new valid signatures, we can only demonstrate the routing mechanism.
	//
	// For now, send the original message to first partition and drop for others.
	// This simulates a form of equivocation where some nodes see the message
	// and others don't.
	if msg.Type() == hotstuff2.MessageVote || msg.Type() == hotstuff2.MessageProposal {
		// Only send to nodes in the first partition
		if len(ei.partitions) > 0 && len(ei.partitions[0].Nodes) > 0 {
			result := make([]OutgoingMessage[H], 0, len(ei.partitions[0].Nodes))
			for _, nodeID := range ei.partitions[0].Nodes {
				result = append(result, OutgoingMessage[H]{To: nodeID, Message: msg})
			}
			return result
		}
	}

	return []OutgoingMessage[H]{{To: -1, Message: msg}}
}

// InterceptIncoming passes through incoming messages unchanged.
func (ei *EquivocationInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	return msg
}

// RandomBehaviorInterceptor randomly drops, delays, or passes messages.
type RandomBehaviorInterceptor[H hotstuff2.Hash] struct {
	nodeID   int
	detector *ViolationDetector[H]
	rng      *rand.Rand
	mu       sync.Mutex
}

// NewRandomBehaviorInterceptor creates a new random behavior interceptor.
func NewRandomBehaviorInterceptor[H hotstuff2.Hash](nodeID int, detector *ViolationDetector[H]) *RandomBehaviorInterceptor[H] {
	return &RandomBehaviorInterceptor[H]{
		nodeID:   nodeID,
		detector: detector,
		rng:      rand.New(rand.NewSource(int64(nodeID))),
	}
}

// InterceptOutgoing randomly decides whether to send, drop, or modify messages.
func (rbi *RandomBehaviorInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	rbi.mu.Lock()
	defer rbi.mu.Unlock()

	// Record votes for detection
	if msg.Type() == hotstuff2.MessageVote && rbi.detector != nil {
		if vote := getVoteFromMessage(msg); vote != nil {
			rbi.detector.RecordVote(from, vote)
		}
	}

	// 20% chance to drop the message entirely
	if rbi.rng.Float64() < 0.2 {
		return []OutgoingMessage[H]{}
	}

	// 80% chance to send normally
	return []OutgoingMessage[H]{{To: -1, Message: msg}}
}

// InterceptIncoming randomly drops incoming messages.
func (rbi *RandomBehaviorInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	rbi.mu.Lock()
	defer rbi.mu.Unlock()

	// 10% chance to drop incoming message
	if rbi.rng.Float64() < 0.1 {
		return nil
	}

	return msg
}

// SilentInterceptor drops all messages (equivalent to crash fault).
type SilentInterceptor[H hotstuff2.Hash] struct{}

// NewSilentInterceptor creates a new silent interceptor.
func NewSilentInterceptor[H hotstuff2.Hash]() *SilentInterceptor[H] {
	return &SilentInterceptor[H]{}
}

// InterceptOutgoing drops all outgoing messages.
func (si *SilentInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	return []OutgoingMessage[H]{} // Drop all
}

// InterceptIncoming drops all incoming messages.
func (si *SilentInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	return nil // Drop all
}

// DelayInterceptor queues messages and releases them after a configurable delay.
// This simulates network latency and message reordering.
type DelayInterceptor[H hotstuff2.Hash] struct {
	nodeID       int
	detector     *ViolationDetector[H]
	delayedQueue []delayedMessage[H]
	mu           sync.Mutex
	rng          *rand.Rand

	// Configuration
	minDelay     int     // Minimum delay in message count (0 = immediate)
	maxDelay     int     // Maximum delay in message count
	reorderProb  float64 // Probability of reordering messages (0-1)
	messageCount int     // Counter for release timing
}

// delayedMessage represents a message that has been delayed.
type delayedMessage[H hotstuff2.Hash] struct {
	message     hotstuff2.ConsensusPayload[H]
	releaseAt   int // Release after this many messages have been processed
	destination int // -1 for broadcast, otherwise specific node
}

// DelayInterceptorConfig configures the delay interceptor.
type DelayInterceptorConfig struct {
	MinDelay    int     // Minimum delay in message count
	MaxDelay    int     // Maximum delay in message count
	ReorderProb float64 // Probability of reordering (0-1)
}

// DefaultDelayConfig returns a default delay configuration.
func DefaultDelayConfig() DelayInterceptorConfig {
	return DelayInterceptorConfig{
		MinDelay:    1,
		MaxDelay:    5,
		ReorderProb: 0.3,
	}
}

// NewDelayInterceptor creates a new delay interceptor with the given configuration.
func NewDelayInterceptor[H hotstuff2.Hash](nodeID int, detector *ViolationDetector[H], config DelayInterceptorConfig) *DelayInterceptor[H] {
	return &DelayInterceptor[H]{
		nodeID:       nodeID,
		detector:     detector,
		delayedQueue: make([]delayedMessage[H], 0),
		rng:          rand.New(rand.NewSource(int64(nodeID))),
		minDelay:     config.MinDelay,
		maxDelay:     config.MaxDelay,
		reorderProb:  config.ReorderProb,
		messageCount: 0,
	}
}

// InterceptOutgoing delays outgoing messages and optionally releases previously delayed ones.
func (di *DelayInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	di.mu.Lock()
	defer di.mu.Unlock()

	di.messageCount++

	// Record votes for detection
	if msg.Type() == hotstuff2.MessageVote && di.detector != nil {
		if vote := getVoteFromMessage(msg); vote != nil {
			di.detector.RecordVote(from, vote)
		}
	}

	// Calculate delay for this message
	delay := di.minDelay
	if di.maxDelay > di.minDelay {
		delay += di.rng.Intn(di.maxDelay - di.minDelay + 1)
	}

	// Queue the new message
	di.delayedQueue = append(di.delayedQueue, delayedMessage[H]{
		message:     msg,
		releaseAt:   di.messageCount + delay,
		destination: -1, // broadcast
	})

	// Optionally reorder the queue
	if di.reorderProb > 0 && len(di.delayedQueue) > 1 && di.rng.Float64() < di.reorderProb {
		// Swap two random messages in the queue
		i := di.rng.Intn(len(di.delayedQueue))
		j := di.rng.Intn(len(di.delayedQueue))
		di.delayedQueue[i], di.delayedQueue[j] = di.delayedQueue[j], di.delayedQueue[i]
	}

	// Release messages that have reached their delay
	result := make([]OutgoingMessage[H], 0)
	remaining := make([]delayedMessage[H], 0, len(di.delayedQueue))

	for _, dm := range di.delayedQueue {
		if di.messageCount >= dm.releaseAt {
			result = append(result, OutgoingMessage[H]{
				To:      dm.destination,
				Message: dm.message,
			})
		} else {
			remaining = append(remaining, dm)
		}
	}

	di.delayedQueue = remaining
	return result
}

// InterceptIncoming may delay incoming messages.
func (di *DelayInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	// For simplicity, incoming messages are not delayed
	// (delaying outgoing is sufficient to test the protocol)
	return msg
}

// Flush releases all remaining delayed messages.
// This should be called at the end of a test to ensure all messages are delivered.
func (di *DelayInterceptor[H]) Flush() []OutgoingMessage[H] {
	di.mu.Lock()
	defer di.mu.Unlock()

	result := make([]OutgoingMessage[H], 0, len(di.delayedQueue))
	for _, dm := range di.delayedQueue {
		result = append(result, OutgoingMessage[H]{
			To:      dm.destination,
			Message: dm.message,
		})
	}

	di.delayedQueue = nil
	return result
}

// QueueLength returns the number of messages currently delayed.
func (di *DelayInterceptor[H]) QueueLength() int {
	di.mu.Lock()
	defer di.mu.Unlock()
	return len(di.delayedQueue)
}

// PassthroughInterceptor records messages but doesn't modify them.
// Useful for vote recording without Byzantine behavior.
type PassthroughInterceptor[H hotstuff2.Hash] struct {
	detector *ViolationDetector[H]
}

// NewPassthroughInterceptor creates a new passthrough interceptor.
func NewPassthroughInterceptor[H hotstuff2.Hash](detector *ViolationDetector[H]) *PassthroughInterceptor[H] {
	return &PassthroughInterceptor[H]{detector: detector}
}

// InterceptOutgoing records votes and passes messages through.
func (pi *PassthroughInterceptor[H]) InterceptOutgoing(from int, msg hotstuff2.ConsensusPayload[H]) []OutgoingMessage[H] {
	// Record votes for detection
	if msg.Type() == hotstuff2.MessageVote && pi.detector != nil {
		if vote := getVoteFromMessage(msg); vote != nil {
			pi.detector.RecordVote(from, vote)
		}
	}
	return []OutgoingMessage[H]{{To: -1, Message: msg}}
}

// InterceptIncoming passes messages through unchanged.
func (pi *PassthroughInterceptor[H]) InterceptIncoming(to int, msg hotstuff2.ConsensusPayload[H]) hotstuff2.ConsensusPayload[H] {
	return msg
}
