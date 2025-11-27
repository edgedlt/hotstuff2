package twins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/edgedlt/hotstuff2"
	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/edgedlt/hotstuff2/timer"
	"go.uber.org/zap"
)

// Execute runs the twins scenario and returns the result.
func (e *Executor[H]) Execute() Result {
	// Create validator set with shared keys for twins
	validators, privKeys := e.createValidatorSet()

	// Create nodes
	totalNodes := e.scenario.Replicas + (e.scenario.Twins * 2)
	committed := make(map[int][]hotstuff2.Block[H])
	var commitMu sync.Mutex

	for i := range totalNodes {
		storage := e.createStorage()
		if storage == nil {
			return Result{
				Scenario: e.scenario,
				Success:  false,
				Violations: []Violation{
					{
						Type:        ViolationNone,
						Description: fmt.Sprintf("failed to create storage for node %d", i),
						NodeID:      i,
					},
				},
			}
		}

		executor := e.createExecutor()
		if executor == nil {
			return Result{
				Scenario: e.scenario,
				Success:  false,
				Violations: []Violation{
					{
						Type:        ViolationNone,
						Description: fmt.Sprintf("failed to create executor for node %d", i),
						NodeID:      i,
					},
				},
			}
		}

		mockTimer := timer.NewMockTimer()

		// Get validator index (twins share the same index)
		validatorIndex := GetValidatorIndex(i, e.scenario.Replicas)

		cfg := &hotstuff2.Config[H]{
			Logger:       zap.NewNop(),
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(validatorIndex),
			PrivateKey:   privKeys[validatorIndex],
			CryptoScheme: "ed25519",
			Storage:      storage,
			Network:      e.network.NodeNetwork(i),
			Executor:     executor,
		}

		nodeIdx := i
		onCommit := func(block hotstuff2.Block[H]) {
			commitMu.Lock()
			committed[nodeIdx] = append(committed[nodeIdx], block)
			commitMu.Unlock()

			// Record commit for fork detection
			e.detector.RecordCommit(nodeIdx, block.Height(), block.Hash())
		}

		node, err := hotstuff2.NewHotStuff2(cfg, onCommit)
		if err != nil {
			return Result{
				Scenario: e.scenario,
				Success:  false,
				Violations: []Violation{
					{
						Type:        ViolationNone,
						Description: fmt.Sprintf("failed to create node %d: %v", i, err),
						NodeID:      i,
					},
				},
			}
		}

		e.nodes[i] = node
	}

	// Start all nodes
	for i, node := range e.nodes {
		if err := node.Start(); err != nil {
			return Result{
				Scenario: e.scenario,
				Success:  false,
				Violations: []Violation{
					{
						Type:        ViolationNone,
						Description: fmt.Sprintf("failed to start node %d: %v", i, err),
						NodeID:      i,
					},
				},
			}
		}
	}

	// Apply Byzantine behavior if configured
	if e.scenario.Behavior != BehaviorHonest {
		e.applyByzantineBehavior()
	}

	// Run for target number of views or until timeout
	// Use shorter timeout for faster tests
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	targetBlocks := e.scenario.Views // One block per view (approximately)

runLoop:
	for {
		select {
		case <-timeout:
			break runLoop

		case <-ticker.C:
			// Check if we've reached target
			commitMu.Lock()
			anyProgress := false
			for i := range totalNodes {
				if len(committed[i]) >= targetBlocks {
					anyProgress = true
					break
				}
			}
			commitMu.Unlock()

			if anyProgress {
				break runLoop
			}
		}
	}

	// Stop all nodes
	for _, node := range e.nodes {
		node.Stop()
	}

	// Small delay to let goroutines finish
	time.Sleep(50 * time.Millisecond)

	// Close network
	e.network.Close()

	// Count total blocks committed
	commitMu.Lock()
	totalBlocks := 0
	for _, blocks := range committed {
		totalBlocks += len(blocks)
	}
	commitMu.Unlock()

	// Get violations
	violations := e.detector.GetViolations()

	return Result{
		Scenario:          e.scenario,
		Success:           len(violations) == 0,
		Violations:        violations,
		BlocksCommitted:   totalBlocks,
		MessagesExchanged: e.network.MessageCount(),
	}
}

// createValidatorSet creates a validator set where twins share keys.
func (e *Executor[H]) createValidatorSet() (hotstuff2.ValidatorSet, []hotstuff2.PrivateKey) {
	// Total validators = replicas + twin pairs
	numValidators := e.scenario.Replicas + e.scenario.Twins

	keys := make(map[uint16]*crypto.Ed25519PublicKey)
	privKeys := make([]hotstuff2.PrivateKey, numValidators)

	// Create keys for honest replicas
	for i := range e.scenario.Replicas {
		priv, _ := crypto.GenerateEd25519Key()
		privKeys[i] = priv
		keys[uint16(i)] = priv.PublicKey().(*crypto.Ed25519PublicKey)
	}

	// Create shared keys for twin pairs
	for i := range e.scenario.Twins {
		priv, _ := crypto.GenerateEd25519Key()
		validatorIdx := e.scenario.Replicas + i
		privKeys[validatorIdx] = priv
		keys[uint16(validatorIdx)] = priv.PublicKey().(*crypto.Ed25519PublicKey)
	}

	vs := &twinsValidatorSet{
		keys: keys,
		n:    numValidators,
	}

	return vs, privKeys
}

// createStorage creates storage for a node.
// This is a generic wrapper that works with any Hash type.
func (e *Executor[H]) createStorage() hotstuff2.Storage[H] {
	// Type assertion to handle TestHash256
	// This is a limitation of Go generics - we can't instantiate generic types
	var zero H
	switch any(zero).(type) {
	case TestHash256:
		return any(NewTestStorage256()).(hotstuff2.Storage[H])
	default:
		// For other hash types, would need similar implementations
		return nil
	}
}

// createExecutor creates an executor for a node.
func (e *Executor[H]) createExecutor() hotstuff2.Executor[H] {
	var zero H
	switch any(zero).(type) {
	case TestHash256:
		return any(NewTestExecutor256()).(hotstuff2.Executor[H])
	default:
		return nil
	}
}

// applyByzantineBehavior applies Byzantine behavior to twin nodes.
func (e *Executor[H]) applyByzantineBehavior() {
	// Identify twin node IDs
	twinNodeIDs := make([]int, 0)
	for i := range e.scenario.Twins * 2 {
		twinNodeIDs = append(twinNodeIDs, e.scenario.Replicas+i)
	}

	switch e.scenario.Behavior {
	case BehaviorSilent:
		// Mark all twin nodes as silent (crash fault)
		for _, nodeID := range twinNodeIDs {
			e.network.SetSilent(nodeID, true)
		}

	case BehaviorDoubleSign:
		// Install double-sign interceptor on twin nodes
		for _, nodeID := range twinNodeIDs {
			interceptor := NewDoubleSignInterceptor[H](nodeID, e.scenario.Replicas, e.detector)
			e.network.SetInterceptor(nodeID, interceptor)
		}

	case BehaviorEquivocation:
		// Install equivocation interceptor on twin nodes
		for _, nodeID := range twinNodeIDs {
			interceptor := NewEquivocationInterceptor[H](nodeID, e.scenario.Partitions)
			e.network.SetInterceptor(nodeID, interceptor)
		}

	case BehaviorRandom:
		// Randomly assign behaviors to twin nodes
		for _, nodeID := range twinNodeIDs {
			interceptor := NewRandomBehaviorInterceptor[H](nodeID, e.detector)
			e.network.SetInterceptor(nodeID, interceptor)
		}

	case BehaviorDelay:
		// Delay and potentially reorder messages from twin nodes
		config := DefaultDelayConfig()
		for _, nodeID := range twinNodeIDs {
			interceptor := NewDelayInterceptor[H](nodeID, e.detector, config)
			e.network.SetInterceptor(nodeID, interceptor)
		}

	case BehaviorHonest:
		// No special behavior needed
	}
}

// twinsValidatorSet implements ValidatorSet for twins testing.
type twinsValidatorSet struct {
	keys map[uint16]*crypto.Ed25519PublicKey
	n    int
}

func (vs *twinsValidatorSet) Count() int {
	return vs.n
}

func (vs *twinsValidatorSet) GetByIndex(index uint16) (hotstuff2.PublicKey, error) {
	if key, ok := vs.keys[index]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("validator %d not found", index)
}

func (vs *twinsValidatorSet) Contains(index uint16) bool {
	_, ok := vs.keys[index]
	return ok
}

func (vs *twinsValidatorSet) GetPublicKeys(indices []uint16) ([]hotstuff2.PublicKey, error) {
	keys := make([]hotstuff2.PublicKey, len(indices))
	for i, idx := range indices {
		key, err := vs.GetByIndex(idx)
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}
	return keys, nil
}

func (vs *twinsValidatorSet) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(vs.n))
}

func (vs *twinsValidatorSet) F() int {
	return (vs.n - 1) / 3
}

// Test types for twins package.
// These are intentionally duplicated from integration_test.go because:
// 1. The twins package is a separate Go package
// 2. Test types in the main package are not exported for external use
// 3. This allows twins/ to work independently without circular dependencies

// TestHash256 implements Hash for twins testing.
type TestHash256 [32]byte

func NewTestHash256(data string) TestHash256 {
	return sha256.Sum256([]byte(data))
}

func (h TestHash256) Bytes() []byte {
	return h[:]
}

func (h TestHash256) Equals(other hotstuff2.Hash) bool {
	if otherTest, ok := other.(TestHash256); ok {
		return h == otherTest
	}
	return false
}

func (h TestHash256) String() string {
	return hex.EncodeToString(h[:8])
}

// TestBlock256 for twins testing.
type TestBlock256 struct {
	hash      TestHash256
	height    uint32
	prevHash  TestHash256
	payload   []byte
	proposer  uint16
	timestamp uint64
}

func NewTestBlock256(height uint32, prevHash TestHash256, proposer uint16) *TestBlock256 {
	data := fmt.Sprintf("block-%d-%s-%d", height, prevHash.String(), proposer)
	return &TestBlock256{
		hash:      NewTestHash256(data),
		height:    height,
		prevHash:  prevHash,
		payload:   []byte(data),
		proposer:  proposer,
		timestamp: uint64(height) * 1000,
	}
}

func (b *TestBlock256) Hash() TestHash256     { return b.hash }
func (b *TestBlock256) Height() uint32        { return b.height }
func (b *TestBlock256) PrevHash() TestHash256 { return b.prevHash }
func (b *TestBlock256) Payload() []byte       { return b.payload }
func (b *TestBlock256) ProposerIndex() uint16 { return b.proposer }
func (b *TestBlock256) Timestamp() uint64     { return b.timestamp }
func (b *TestBlock256) Bytes() []byte {
	return []byte(fmt.Sprintf("block-%d", b.height))
}

// TestStorage256 for twins testing.
type TestStorage256 struct {
	mu            sync.RWMutex
	blocks        map[string]*TestBlock256
	qcs           map[string]*hotstuff2.QC[TestHash256]
	lastBlock     *TestBlock256
	highestLocked *hotstuff2.QC[TestHash256]
	view          uint32
}

func NewTestStorage256() *TestStorage256 {
	genesis := NewTestBlock256(0, TestHash256{}, 0)
	return &TestStorage256{
		blocks:    map[string]*TestBlock256{genesis.Hash().String(): genesis},
		qcs:       make(map[string]*hotstuff2.QC[TestHash256]),
		lastBlock: genesis,
		view:      0,
	}
}

func (s *TestStorage256) GetBlock(hash TestHash256) (hotstuff2.Block[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if block, ok := s.blocks[hash.String()]; ok {
		return block, nil
	}
	return nil, fmt.Errorf("block not found")
}

func (s *TestStorage256) PutBlock(block hotstuff2.Block[TestHash256]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tb, ok := block.(*TestBlock256); ok {
		s.blocks[block.Hash().String()] = tb
		if tb.Height() > s.lastBlock.Height() {
			s.lastBlock = tb
		}
	}
	return nil
}

func (s *TestStorage256) GetLastBlock() (hotstuff2.Block[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBlock, nil
}

func (s *TestStorage256) GetQC(nodeHash TestHash256) (hotstuff2.QuorumCertificate[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if qc, ok := s.qcs[nodeHash.String()]; ok {
		return qc, nil
	}
	return nil, fmt.Errorf("QC not found")
}

func (s *TestStorage256) PutQC(qc hotstuff2.QuorumCertificate[TestHash256]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*hotstuff2.QC[TestHash256]); ok {
		s.qcs[qc.Node().String()] = qcConcrete
	}
	return nil
}

func (s *TestStorage256) GetHighestLockedQC() (hotstuff2.QuorumCertificate[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highestLocked, nil
}

func (s *TestStorage256) PutHighestLockedQC(qc hotstuff2.QuorumCertificate[TestHash256]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*hotstuff2.QC[TestHash256]); ok {
		s.highestLocked = qcConcrete
	}
	return nil
}

func (s *TestStorage256) GetView() (uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view, nil
}

func (s *TestStorage256) PutView(view uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = view
	return nil
}

func (s *TestStorage256) Close() error {
	return nil
}

// TestExecutor256 for twins testing.
type TestExecutor256 struct {
	mu     sync.Mutex
	blocks map[string]*TestBlock256
}

func NewTestExecutor256() *TestExecutor256 {
	return &TestExecutor256{
		blocks: make(map[string]*TestBlock256),
	}
}

func (e *TestExecutor256) Execute(block hotstuff2.Block[TestHash256]) (TestHash256, error) {
	return block.Hash(), nil
}

func (e *TestExecutor256) Verify(block hotstuff2.Block[TestHash256]) error {
	return nil
}

func (e *TestExecutor256) GetStateHash() TestHash256 {
	return TestHash256{}
}

func (e *TestExecutor256) CreateBlock(height uint32, prevHash TestHash256, proposerIndex uint16) (hotstuff2.Block[TestHash256], error) {
	block := NewTestBlock256(height, prevHash, proposerIndex)
	e.mu.Lock()
	e.blocks[block.Hash().String()] = block
	e.mu.Unlock()
	return block, nil
}
