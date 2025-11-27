package simulator

import (
	"fmt"
	"sync"

	hotstuff2 "github.com/edgedlt/hotstuff2"
	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/edgedlt/hotstuff2/timer"
	"go.uber.org/zap"
)

// Level represents a simulation difficulty level.
type Level int

const (
	// LevelHappyPath - Perfect network, no faults
	LevelHappyPath Level = iota

	// LevelByzantine - Network partitions, crashes, delays
	LevelByzantine

	// LevelChaos - High fault rate, Byzantine behavior
	LevelChaos
)

func (l Level) String() string {
	switch l {
	case LevelHappyPath:
		return "Happy Path"
	case LevelByzantine:
		return "Byzantine Weather"
	case LevelChaos:
		return "Chaos Mode"
	default:
		return "Unknown"
	}
}

// SimNode represents a single consensus node in the simulation.
type SimNode struct {
	ID        int
	consensus *hotstuff2.HotStuff2[SimHash]
	storage   *SimStorage
	executor  *SimExecutor
	realTimer *timer.RealTimer

	// Observable state
	mu            sync.RWMutex
	committed     []hotstuff2.Block[SimHash]
	status        NodeStatus
	lastEventTime uint64
}

// NodeStatus represents the status of a node.
type NodeStatus string

const (
	NodeStatusActive      NodeStatus = "active"
	NodeStatusCrashed     NodeStatus = "crashed"
	NodeStatusPartitioned NodeStatus = "partitioned"
)

// NodeState represents the observable state of a node for the UI.
type NodeState struct {
	ID           int        `json:"id"`
	View         uint32     `json:"view"`
	CommitHeight uint32     `json:"commitHeight"`
	IsLeader     bool       `json:"isLeader"`
	Status       NodeStatus `json:"status"`
	BlockCount   int        `json:"blockCount"`
}

// Simulator is the main simulation engine.
type Simulator struct {
	mu sync.RWMutex

	// Configuration
	level          Level
	faultTolerance int
	blockTimeMode  BlockTimeMode
	nodeCount      int
	seed           int64

	// Components
	clock      *Clock
	network    *NetworkSimulator
	validators *SimValidatorSet
	privKeys   []*crypto.Ed25519PrivateKey
	nodes      []*SimNode

	// State
	running   bool
	events    []Event
	maxEvents int

	// Callbacks
	onEvent func(Event)

	logger *zap.Logger
}

// Event represents a simulation event for visualization.
type Event struct {
	Time        uint64    `json:"time"`
	Type        EventType `json:"type"`
	NodeID      int       `json:"nodeId"`
	TargetID    int       `json:"targetId,omitempty"`
	View        uint32    `json:"view,omitempty"`
	Height      uint32    `json:"height,omitempty"`
	BlockHash   string    `json:"blockHash,omitempty"`
	MessageType string    `json:"messageType,omitempty"`
	Description string    `json:"description"`
}

// EventType categorizes events.
type EventType string

const (
	EventPropose     EventType = "propose"
	EventVote        EventType = "vote"
	EventQCFormed    EventType = "qc_formed"
	EventCommit      EventType = "commit"
	EventViewChange  EventType = "view_change"
	EventTimeout     EventType = "timeout"
	EventMessageSent EventType = "message_sent"
	EventMessageDrop EventType = "message_drop"
	EventNodeCrash   EventType = "node_crash"
	EventNodeRecover EventType = "node_recover"
	EventPartition   EventType = "partition"
)

// BlockTimeMode represents the block time configuration.
type BlockTimeMode int

const (
	// BlockTimeOptimistic - No minimum block time, optimistically responsive
	BlockTimeOptimistic BlockTimeMode = iota
	// BlockTime1Second - Target 1 block per second
	BlockTime1Second
)

func (m BlockTimeMode) String() string {
	switch m {
	case BlockTimeOptimistic:
		return "Optimistic (immediate)"
	case BlockTime1Second:
		return "1 second target"
	default:
		return "Unknown"
	}
}

// Config holds simulator configuration.
type Config struct {
	Level          Level
	FaultTolerance int // f - number of Byzantine faults tolerated
	BlockTimeMode  BlockTimeMode
	Seed           int64
}

// NodeCount returns the number of nodes for the given fault tolerance.
// BFT requires n = 3f + 1 nodes to tolerate f Byzantine faults.
func (c Config) NodeCount() int {
	return 3*c.FaultTolerance + 1
}

// DefaultConfig returns the default simulator configuration.
func DefaultConfig() Config {
	return Config{
		Level:          LevelHappyPath,
		FaultTolerance: 1, // f=1 means 4 nodes (3*1+1)
		BlockTimeMode:  BlockTime1Second,
		Seed:           42,
	}
}

// MaxFaultTolerance is the maximum f value allowed (f=5 means 16 nodes).
const MaxFaultTolerance = 5

// New creates a new simulator.
func New(cfg Config) (*Simulator, error) {
	// Validate and clamp fault tolerance
	if cfg.FaultTolerance < 1 {
		cfg.FaultTolerance = 1
	}
	if cfg.FaultTolerance > MaxFaultTolerance {
		cfg.FaultTolerance = MaxFaultTolerance
	}

	nodeCount := cfg.NodeCount()

	// Create logger (production would use nop)
	logger, _ := zap.NewDevelopment()

	// Create clock (just tracks elapsed time)
	clock := NewClock()

	// Create validators and keys
	validators, privKeys := NewSimValidatorSetWithKeys(nodeCount)

	// Create network with level-appropriate configuration (scaled by f)
	netConfig := networkConfigForLevel(cfg.Level, cfg.FaultTolerance)
	network := NewNetworkSimulator(nodeCount, netConfig, cfg.Seed)

	sim := &Simulator{
		level:          cfg.Level,
		faultTolerance: cfg.FaultTolerance,
		blockTimeMode:  cfg.BlockTimeMode,
		nodeCount:      nodeCount,
		seed:           cfg.Seed,
		clock:          clock,
		network:        network,
		validators:     validators,
		privKeys:       privKeys,
		nodes:          make([]*SimNode, nodeCount),
		events:         make([]Event, 0, 1000),
		maxEvents:      1000,
		logger:         logger,
	}

	// Set up network event hooks
	network.SetOnMessageSent(func(from, to int, msgType hotstuff2.MessageType, delayed bool) {
		sim.addEvent(Event{
			Time:        clock.Now(),
			Type:        EventMessageSent,
			NodeID:      from,
			TargetID:    to,
			MessageType: msgType.String(),
			Description: fmt.Sprintf("Node %d sent %s to Node %d", from, msgType, to),
		})
	})

	network.SetOnMessageDrop(func(from, to int, msgType hotstuff2.MessageType) {
		sim.addEvent(Event{
			Time:        clock.Now(),
			Type:        EventMessageDrop,
			NodeID:      from,
			TargetID:    to,
			MessageType: msgType.String(),
			Description: fmt.Sprintf("Message %s from Node %d to Node %d dropped", msgType, from, to),
		})
	})

	// Create nodes
	for i := range nodeCount {
		node, err := sim.createNode(i)
		if err != nil {
			return nil, fmt.Errorf("failed to create node %d: %w", i, err)
		}
		sim.nodes[i] = node
	}

	return sim, nil
}

// createNode creates a single simulation node.
func (s *Simulator) createNode(id int) (*SimNode, error) {
	storage := NewSimStorage()
	executor := NewSimExecutor()
	realTimer := timer.NewRealTimer()

	node := &SimNode{
		ID:        id,
		storage:   storage,
		executor:  executor,
		realTimer: realTimer,
		status:    NodeStatusActive,
	}

	// Create HotStuff2 config with appropriate pacemaker settings
	var pacemakerCfg hotstuff2.PacemakerConfig
	switch s.level {
	case LevelHappyPath:
		// For happy path, use configured block time mode
		pacemakerCfg = s.pacemakerConfigForMode()
	case LevelByzantine, LevelChaos:
		// For fault scenarios, use 1-second blocks for visibility
		pacemakerCfg = hotstuff2.DemoPacemakerConfig()
	default:
		pacemakerCfg = s.pacemakerConfigForMode()
	}

	cfg := &hotstuff2.Config[SimHash]{
		Logger:       s.logger.Named(fmt.Sprintf("node-%d", id)),
		Timer:        realTimer,
		Validators:   s.validators,
		MyIndex:      uint16(id),
		PrivateKey:   s.privKeys[id],
		CryptoScheme: "ed25519",
		Storage:      storage,
		Network:      s.network.NodeNetwork(id),
		Executor:     executor,
		Pacemaker:    &pacemakerCfg,
	}

	// Create consensus instance with commit callback
	consensus, err := hotstuff2.NewHotStuff2(cfg, func(block hotstuff2.Block[SimHash]) {
		node.mu.Lock()
		node.committed = append(node.committed, block)
		node.lastEventTime = s.clock.Now()
		node.mu.Unlock()

		s.addEvent(Event{
			Time:        s.clock.Now(),
			Type:        EventCommit,
			NodeID:      id,
			Height:      block.Height(),
			BlockHash:   block.Hash().String(),
			Description: fmt.Sprintf("Node %d committed block %d", id, block.Height()),
		})
	})
	if err != nil {
		return nil, err
	}

	node.consensus = consensus
	return node, nil
}

// networkConfigForLevel returns network configuration for a level.
// The fault tolerance (f) affects how aggressive the faults are:
// - Higher f = more nodes = more resilient = we can be more aggressive with faults
// - Level 2 (Byzantine): With higher f, system should rarely halt
// - Level 3 (Chaos): With higher f, system should rarely make progress
func networkConfigForLevel(level Level, f int) NetworkConfig {
	switch level {
	case LevelHappyPath:
		// Perfect network - no faults
		// Block time is controlled by the pacemaker's TimeoutCommit (1s)
		// Use minimal latency for fast message delivery
		return NetworkConfig{
			PacketLoss:        0.0,
			MinLatency:        10,
			MaxLatency:        50,
			ReplayProbability: 0.0,
			EnablePartitions:  false,
			EnableCrashes:     false,
		}
	case LevelByzantine:
		// Byzantine Weather: Moderate faults that scale with f
		// Add some network latency to make it more realistic
		return NetworkConfig{
			PacketLoss:        0.15,
			MinLatency:        50,
			MaxLatency:        200,
			ReplayProbability: 0.03,
			EnablePartitions:  true,
			EnableCrashes:     true,
		}
	case LevelChaos:
		// Chaos Mode: Aggressive faults scaled by f
		// Packet loss scales inversely with f (more nodes = can handle more loss)
		packetLoss := 0.4 - float64(f-1)*0.05 // f=1: 40%, f=5: 20%
		if packetLoss < 0.15 {
			packetLoss = 0.15
		}
		return NetworkConfig{
			PacketLoss:        packetLoss,
			MinLatency:        100,
			MaxLatency:        400,
			ReplayProbability: 0.08,
			EnablePartitions:  true,
			EnableCrashes:     true,
		}
	default:
		return DefaultNetworkConfig()
	}
}

// Start starts the simulation.
func (s *Simulator) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("simulation already running")
	}
	s.running = true
	s.mu.Unlock()

	// Start the clock (tracks elapsed time)
	s.clock.Start()

	// Start all nodes
	for _, node := range s.nodes {
		if err := node.consensus.Start(); err != nil {
			return fmt.Errorf("failed to start node %d: %w", node.ID, err)
		}
	}

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        "start",
		Description: fmt.Sprintf("Simulation started with %d nodes at level %s", s.nodeCount, s.level),
	})

	return nil
}

// Stop stops the simulation.
func (s *Simulator) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	// Stop all nodes (only if they're not crashed - crashed nodes are already stopped)
	for _, node := range s.nodes {
		node.mu.RLock()
		isCrashed := node.status == NodeStatusCrashed
		node.mu.RUnlock()

		if !isCrashed {
			node.consensus.Stop()
		}
	}

	// Stop clock
	s.clock.Stop()

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        "stop",
		Description: "Simulation stopped",
	})
}

// IsRunning returns true if the simulation is running.
func (s *Simulator) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetState returns the current simulation state for the UI.
func (s *Simulator) GetState() State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make([]NodeState, len(s.nodes))
	for i, node := range s.nodes {
		node.mu.RLock()
		view := node.consensus.View()
		commitHeight := uint32(0)
		if len(node.committed) > 0 {
			commitHeight = node.committed[len(node.committed)-1].Height()
		}
		nodes[i] = NodeState{
			ID:           node.ID,
			View:         view,
			CommitHeight: commitHeight,
			IsLeader:     s.validators.GetLeader(view) == uint16(node.ID),
			Status:       node.status,
			BlockCount:   len(node.committed),
		}
		node.mu.RUnlock()
	}

	netStats := s.network.Stats()
	partitions := s.network.GetPartitions()

	// Calculate quorum: 2f + 1
	quorum := 2*s.faultTolerance + 1

	return State{
		SimTime:         s.clock.Now(),
		Level:           s.level.String(),
		Running:         s.running,
		FaultTolerance:  s.faultTolerance,
		NodeCount:       s.nodeCount,
		Quorum:          quorum,
		Nodes:           nodes,
		Partitions:      partitions,
		MessagesSent:    netStats.MessagesSent,
		MessagesDropped: netStats.MessagesDropped,
		Events:          s.getRecentEvents(50),
	}
}

// State represents the full simulation state for the UI.
type State struct {
	SimTime         uint64      `json:"simTime"`
	Level           string      `json:"level"`
	Running         bool        `json:"running"`
	FaultTolerance  int         `json:"faultTolerance"`
	NodeCount       int         `json:"nodeCount"`
	Quorum          int         `json:"quorum"`
	Nodes           []NodeState `json:"nodes"`
	Partitions      [][]int     `json:"partitions"`
	MessagesSent    int         `json:"messagesSent"`
	MessagesDropped int         `json:"messagesDropped"`
	Events          []Event     `json:"events"`
}

// addEvent adds an event to the event log.
func (s *Simulator) addEvent(e Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	if len(s.events) > s.maxEvents {
		s.events = s.events[len(s.events)-s.maxEvents:]
	}
	callback := s.onEvent
	s.mu.Unlock()

	if callback != nil {
		callback(e)
	}
}

// getRecentEvents returns the most recent n events.
func (s *Simulator) getRecentEvents(n int) []Event {
	if len(s.events) <= n {
		return append([]Event{}, s.events...)
	}
	return append([]Event{}, s.events[len(s.events)-n:]...)
}

// SetOnEvent sets the callback for simulation events.
func (s *Simulator) SetOnEvent(fn func(Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

// CrashNode simulates a node crash.
// This stops the node's consensus instance entirely (crash-fault model from TLA+ spec).
// A crashed node does not participate in consensus at all - no votes, proposals, or NEWVIEWs.
func (s *Simulator) CrashNode(nodeID int) error {
	if nodeID < 0 || nodeID >= len(s.nodes) {
		return fmt.Errorf("invalid node ID: %d", nodeID)
	}

	node := s.nodes[nodeID]
	node.mu.Lock()
	node.status = NodeStatusCrashed
	node.mu.Unlock()

	// Stop the consensus instance - crashed nodes don't participate
	// This matches the TLA+ fault model where faulty replicas have guard `r \in Honest`
	// which prevents them from taking any actions
	node.consensus.Stop()

	s.network.CrashNode(nodeID)

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        EventNodeCrash,
		NodeID:      nodeID,
		Description: fmt.Sprintf("Node %d crashed", nodeID),
	})

	return nil
}

// RecoverNode recovers a crashed node.
// This restarts the node's consensus instance so it can participate again.
func (s *Simulator) RecoverNode(nodeID int) error {
	if nodeID < 0 || nodeID >= len(s.nodes) {
		return fmt.Errorf("invalid node ID: %d", nodeID)
	}

	node := s.nodes[nodeID]
	node.mu.Lock()
	wasActive := node.status == NodeStatusActive
	node.status = NodeStatusActive
	node.mu.Unlock()

	s.network.RecoverNode(nodeID)

	// Restart the consensus instance if it was crashed
	// The node will resume from its persisted state
	if !wasActive {
		if err := node.consensus.Start(); err != nil {
			return fmt.Errorf("failed to restart consensus for node %d: %w", nodeID, err)
		}
	}

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        EventNodeRecover,
		NodeID:      nodeID,
		Description: fmt.Sprintf("Node %d recovered", nodeID),
	})

	return nil
}

// SetPartitions sets network partitions.
func (s *Simulator) SetPartitions(partitions [][]int) {
	s.network.SetPartitions(partitions)

	// If clearing partitions (empty array), reset partitioned nodes to active
	if len(partitions) == 0 {
		for _, node := range s.nodes {
			node.mu.Lock()
			if node.status == NodeStatusPartitioned {
				node.status = NodeStatusActive
			}
			node.mu.Unlock()
		}
		s.addEvent(Event{
			Time:        s.clock.Now(),
			Type:        EventPartition,
			Description: "Network partitions cleared",
		})
		return
	}

	// Mark nodes not in any partition as partitioned (isolated)
	partitioned := make(map[int]bool)
	for _, partition := range partitions {
		for _, id := range partition {
			partitioned[id] = true
		}
	}

	for _, node := range s.nodes {
		node.mu.Lock()
		// Only change status if node is currently active
		if node.status == NodeStatusActive && !partitioned[node.ID] {
			node.status = NodeStatusPartitioned
		}
		node.mu.Unlock()
	}

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        EventPartition,
		Description: fmt.Sprintf("Network partitions set: %v", partitions),
	})
}

// ClearPartitions removes all network partitions.
func (s *Simulator) ClearPartitions() {
	s.network.ClearPartitions()

	// Reset all partitioned nodes to active
	for _, node := range s.nodes {
		node.mu.Lock()
		if node.status == NodeStatusPartitioned {
			node.status = NodeStatusActive
		}
		node.mu.Unlock()
	}

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        EventPartition,
		Description: "Network partitions cleared",
	})
}

// HealAll recovers all crashed nodes and clears all partitions.
func (s *Simulator) HealAll() {
	// Clear partitions first
	s.network.ClearPartitions()

	// Recover all nodes
	for _, node := range s.nodes {
		node.mu.Lock()
		wasCrashed := node.status == NodeStatusCrashed
		if node.status != NodeStatusActive {
			node.status = NodeStatusActive
			s.network.RecoverNode(node.ID)
		}
		node.mu.Unlock()

		// Restart consensus for crashed nodes
		if wasCrashed {
			if err := node.consensus.Start(); err != nil {
				s.logger.Error("failed to restart consensus on heal",
					zap.Int("node", node.ID),
					zap.Error(err))
			}
		}
	}

	s.addEvent(Event{
		Time:        s.clock.Now(),
		Type:        EventNodeRecover,
		Description: "All nodes healed and partitions cleared",
	})
}

// Reset resets the simulation to its initial state.
func (s *Simulator) Reset() error {
	s.Stop()

	// Reset clock
	s.clock.Reset()

	// Clear events
	s.mu.Lock()
	s.events = s.events[:0]
	s.mu.Unlock()

	// Recreate network
	netConfig := networkConfigForLevel(s.level, s.faultTolerance)
	s.network = NewNetworkSimulator(s.nodeCount, netConfig, s.seed)

	// Recreate nodes
	for i := range s.nodeCount {
		node, err := s.createNode(i)
		if err != nil {
			return fmt.Errorf("failed to recreate node %d: %w", i, err)
		}
		s.nodes[i] = node
	}

	return nil
}

// SetLevel changes the simulation level (requires reset).
func (s *Simulator) SetLevel(level Level) {
	s.mu.Lock()
	s.level = level
	s.mu.Unlock()
}

// Level returns the current simulation level.
func (s *Simulator) Level() Level {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.level
}

// FaultTolerance returns the current fault tolerance (f).
func (s *Simulator) FaultTolerance() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.faultTolerance
}

// BlockTimeMode returns the current block time mode.
func (s *Simulator) BlockTimeMode() BlockTimeMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blockTimeMode
}

// SetBlockTimeMode sets the block time mode.
func (s *Simulator) SetBlockTimeMode(mode BlockTimeMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blockTimeMode = mode
}

// pacemakerConfigForMode returns the pacemaker configuration for the current block time mode.
func (s *Simulator) pacemakerConfigForMode() hotstuff2.PacemakerConfig {
	switch s.blockTimeMode {
	case BlockTimeOptimistic:
		// Optimistic mode: no minimum block time, immediate responsiveness
		return hotstuff2.DefaultPacemakerConfig()
	case BlockTime1Second:
		// 1-second target block time for visible demo
		return hotstuff2.DemoPacemakerConfig()
	default:
		return hotstuff2.DemoPacemakerConfig()
	}
}
