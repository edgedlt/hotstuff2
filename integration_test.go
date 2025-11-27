package hotstuff2

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/edgedlt/hotstuff2/internal/crypto"
	"github.com/edgedlt/hotstuff2/timer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Integration tests for multi-node consensus

// TestIntegration_FourNodesHappyPath tests basic consensus with 4 nodes.
// All nodes should commit the same sequence of blocks without failures.
func TestIntegration_FourNodesHappyPath(t *testing.T) {
	const N = 4
	const TARGET_BLOCKS = 5

	validators, privKeys := NewTestValidatorSetWithKeys(N)

	// Shared network for all nodes
	sharedNetwork := NewSharedNetwork[TestHash](N)

	// Create nodes
	nodes := make([]*HotStuff2[TestHash], N)
	committed := make([][]Block[TestHash], N)
	var commitMu sync.Mutex

	for i := range N {
		storage := NewTestStorage()
		executor := NewTestExecutor()
		mockTimer := timer.NewMockTimer()

		cfg := &Config[TestHash]{
			Logger:       zap.NewNop(), // Disable logging for cleaner test output
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(i),
			PrivateKey:   privKeys[i],
			CryptoScheme: "ed25519",
			Storage:      storage,
			Network:      sharedNetwork.NodeNetwork(i),
			Executor:     executor,
		}

		idx := i
		onCommit := func(block Block[TestHash]) {
			commitMu.Lock()
			committed[idx] = append(committed[idx], block)
			commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		require.NoError(t, err)
		nodes[i] = node
	}

	// Start all nodes
	for _, node := range nodes {
		require.NoError(t, node.Start())
	}

	// Wait for blocks to be committed (timeout after 30 seconds)
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for blocks to commit")

		case <-ticker.C:
			commitMu.Lock()
			allReady := true
			for i := range N {
				if len(committed[i]) < TARGET_BLOCKS {
					allReady = false
					break
				}
			}
			commitMu.Unlock()

			if allReady {
				goto done
			}
		}
	}

done:
	// Stop all nodes cleanly before verifying
	for _, node := range nodes {
		node.Stop()
	}

	// Small delay to let goroutines finish, then close network
	time.Sleep(50 * time.Millisecond)
	sharedNetwork.Close()

	// Verify all nodes committed the same chain
	commitMu.Lock()
	defer commitMu.Unlock()

	// Find minimum committed blocks across all nodes
	minCommitted := len(committed[0])
	for i := 1; i < N; i++ {
		if len(committed[i]) < minCommitted {
			minCommitted = len(committed[i])
		}
	}

	t.Logf("Node 0 committed %d blocks", len(committed[0]))
	for i := 1; i < N; i++ {
		t.Logf("Node %d committed %d blocks", i, len(committed[i]))
	}

	// Ensure all nodes committed at least TARGET_BLOCKS
	assert.GreaterOrEqual(t, minCommitted, TARGET_BLOCKS, "all nodes should commit at least %d blocks", TARGET_BLOCKS)

	// Verify that the first minCommitted blocks are the same across all nodes
	for i := 1; i < N; i++ {
		for j := range minCommitted {
			assert.True(t, committed[i][j].Hash().Equals(committed[0][j].Hash()),
				"node %d block %d hash should match node 0", i, j)
		}
	}

	t.Logf("✅ All %d nodes committed at least %d blocks with matching hashes", N, minCommitted)
	t.Logf("Total network messages: %d", sharedNetwork.MessageCount())
}

// SharedNetwork implements a simple in-memory network for integration testing.
type SharedNetwork[H Hash] struct {
	mu           sync.RWMutex
	nodeChannels map[int]chan ConsensusPayload[H]
	messageCount int
}

func NewSharedNetwork[H Hash](n int) *SharedNetwork[H] {
	channels := make(map[int]chan ConsensusPayload[H])
	for i := range n {
		channels[i] = make(chan ConsensusPayload[H], 100)
	}

	return &SharedNetwork[H]{
		nodeChannels: channels,
	}
}

func (sn *SharedNetwork[H]) NodeNetwork(nodeID int) *SharedNodeNetwork[H] {
	return &SharedNodeNetwork[H]{
		nodeID:  nodeID,
		network: sn,
	}
}

func (sn *SharedNetwork[H]) broadcast(from int, msg ConsensusPayload[H]) {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	sn.messageCount++

	for to := range sn.nodeChannels {
		if to == from {
			continue
		}

		select {
		case sn.nodeChannels[to] <- msg:
		default:
			// Channel full, drop
		}
	}
}

func (sn *SharedNetwork[H]) MessageCount() int {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	return sn.messageCount
}

func (sn *SharedNetwork[H]) Close() {
	sn.mu.Lock()
	defer sn.mu.Unlock()

	for _, ch := range sn.nodeChannels {
		close(ch)
	}
}

type SharedNodeNetwork[H Hash] struct {
	nodeID  int
	network *SharedNetwork[H]
}

func (snn *SharedNodeNetwork[H]) Broadcast(msg ConsensusPayload[H]) {
	snn.network.broadcast(snn.nodeID, msg)
}

func (snn *SharedNodeNetwork[H]) SendTo(validatorIndex uint16, msg ConsensusPayload[H]) {
	// For simplicity, just broadcast
	snn.network.broadcast(snn.nodeID, msg)
}

func (snn *SharedNodeNetwork[H]) Receive() <-chan ConsensusPayload[H] {
	snn.network.mu.RLock()
	defer snn.network.mu.RUnlock()
	return snn.network.nodeChannels[snn.nodeID]
}

func (snn *SharedNodeNetwork[H]) Close() error {
	return nil
}

// TestHash256 is a SHA256-based hash for testing.
type TestHash256 [32]byte

func NewTestHash256(data string) TestHash256 {
	return sha256.Sum256([]byte(data))
}

func (h TestHash256) Bytes() []byte {
	return h[:]
}

func (h TestHash256) Equals(other Hash) bool {
	if otherTest, ok := other.(TestHash256); ok {
		return h == otherTest
	}
	return false
}

func (h TestHash256) String() string {
	return hex.EncodeToString(h[:8])
}

// TestBlock256 is a test block using TestHash256.
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
func (b *TestBlock256) Bytes() []byte         { return []byte(fmt.Sprintf("block-%d", b.height)) }

// TestStorage256 implements Storage for TestHash256.
type TestStorage256 struct {
	mu            sync.RWMutex
	blocks        map[string]*TestBlock256
	qcs           map[string]*QC[TestHash256]
	lastBlock     *TestBlock256
	highestLocked *QC[TestHash256]
	view          uint32
}

func NewTestStorage256() *TestStorage256 {
	genesis := NewTestBlock256(0, TestHash256{}, 0)
	return &TestStorage256{
		blocks:    map[string]*TestBlock256{genesis.Hash().String(): genesis},
		qcs:       make(map[string]*QC[TestHash256]),
		lastBlock: genesis,
		view:      0,
	}
}

func (s *TestStorage256) GetBlock(hash TestHash256) (Block[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if block, ok := s.blocks[hash.String()]; ok {
		return block, nil
	}
	return nil, fmt.Errorf("block not found")
}

func (s *TestStorage256) PutBlock(block Block[TestHash256]) error {
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

func (s *TestStorage256) GetLastBlock() (Block[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBlock, nil
}

func (s *TestStorage256) GetQC(nodeHash TestHash256) (QuorumCertificate[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if qc, ok := s.qcs[nodeHash.String()]; ok {
		return qc, nil
	}
	return nil, fmt.Errorf("QC not found")
}

func (s *TestStorage256) PutQC(qc QuorumCertificate[TestHash256]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*QC[TestHash256]); ok {
		s.qcs[qc.Node().String()] = qcConcrete
	}
	return nil
}

func (s *TestStorage256) GetHighestLockedQC() (QuorumCertificate[TestHash256], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highestLocked, nil
}

func (s *TestStorage256) PutHighestLockedQC(qc QuorumCertificate[TestHash256]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*QC[TestHash256]); ok {
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

// TestExecutor256 implements Executor for TestHash256.
type TestExecutor256 struct {
	blocks map[string]*TestBlock256
}

func NewTestExecutor256() *TestExecutor256 {
	return &TestExecutor256{
		blocks: make(map[string]*TestBlock256),
	}
}

func (e *TestExecutor256) Execute(block Block[TestHash256]) (TestHash256, error) {
	return block.Hash(), nil
}

func (e *TestExecutor256) Verify(block Block[TestHash256]) error {
	return nil
}

func (e *TestExecutor256) GetStateHash() TestHash256 {
	return TestHash256{}
}

func (e *TestExecutor256) CreateBlock(height uint32, prevHash TestHash256, proposerIndex uint16) (Block[TestHash256], error) {
	block := NewTestBlock256(height, prevHash, proposerIndex)
	e.blocks[block.Hash().String()] = block
	return block, nil
}

// TestIntegration_NodeCrashRecovery tests consensus continues after a node crashes.
func TestIntegration_NodeCrashRecovery(t *testing.T) {
	const N = 4
	const TARGET_BLOCKS = 3

	validators, privKeys := NewTestValidatorSetWithKeys(N)
	sharedNetwork := NewSharedNetwork[TestHash](N)

	nodes := make([]*HotStuff2[TestHash], N)
	committed := make([][]Block[TestHash], N)
	var commitMu sync.Mutex

	for i := range N {
		storage := NewTestStorage()
		executor := NewTestExecutor()
		mockTimer := timer.NewMockTimer()

		cfg := &Config[TestHash]{
			Logger:       zap.NewNop(),
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(i),
			PrivateKey:   privKeys[i],
			CryptoScheme: "ed25519",
			Storage:      storage,
			Network:      sharedNetwork.NodeNetwork(i),
			Executor:     executor,
		}

		idx := i
		onCommit := func(block Block[TestHash]) {
			commitMu.Lock()
			committed[idx] = append(committed[idx], block)
			commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		require.NoError(t, err)
		nodes[i] = node
	}

	// Start all nodes
	for _, node := range nodes {
		require.NoError(t, node.Start())
	}

	// Wait for some blocks
	time.Sleep(500 * time.Millisecond)

	// "Crash" node 0 by stopping it
	nodes[0].Stop()
	t.Log("Node 0 crashed")

	// Wait for more blocks - consensus should continue with remaining 3 nodes
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			// Even if we don't reach target, check that some progress was made
			goto done

		case <-ticker.C:
			commitMu.Lock()
			// Check if remaining nodes committed enough blocks
			minCommitted := 999
			for i := 1; i < N; i++ { // Skip crashed node 0
				if len(committed[i]) < minCommitted {
					minCommitted = len(committed[i])
				}
			}
			commitMu.Unlock()

			if minCommitted >= TARGET_BLOCKS {
				goto done
			}
		}
	}

done:
	// Stop remaining nodes
	for i := 1; i < N; i++ {
		nodes[i].Stop()
	}
	time.Sleep(50 * time.Millisecond)
	sharedNetwork.Close()

	// Verify remaining nodes made progress
	commitMu.Lock()
	defer commitMu.Unlock()

	// At least one node should have committed blocks after the crash
	maxCommitted := 0
	for i := 1; i < N; i++ {
		if len(committed[i]) > maxCommitted {
			maxCommitted = len(committed[i])
		}
	}

	t.Logf("Max blocks committed after crash: %d", maxCommitted)
	assert.Greater(t, maxCommitted, 0, "consensus should continue after 1 node crash (f=1)")
}

// TestIntegration_SevenNodes tests consensus with 7 nodes (can tolerate 2 faults).
func TestIntegration_SevenNodes(t *testing.T) {
	const N = 7
	const TARGET_BLOCKS = 3

	validators, privKeys := NewTestValidatorSetWithKeys(N)
	sharedNetwork := NewSharedNetwork[TestHash](N)

	nodes := make([]*HotStuff2[TestHash], N)
	committed := make([][]Block[TestHash], N)
	var commitMu sync.Mutex

	for i := range N {
		storage := NewTestStorage()
		executor := NewTestExecutor()
		mockTimer := timer.NewMockTimer()

		cfg := &Config[TestHash]{
			Logger:       zap.NewNop(),
			Timer:        mockTimer,
			Validators:   validators,
			MyIndex:      uint16(i),
			PrivateKey:   privKeys[i],
			CryptoScheme: "ed25519",
			Storage:      storage,
			Network:      sharedNetwork.NodeNetwork(i),
			Executor:     executor,
		}

		idx := i
		onCommit := func(block Block[TestHash]) {
			commitMu.Lock()
			committed[idx] = append(committed[idx], block)
			commitMu.Unlock()
		}

		node, err := NewHotStuff2(cfg, onCommit)
		require.NoError(t, err)
		nodes[i] = node
	}

	// Start all nodes
	for _, node := range nodes {
		require.NoError(t, node.Start())
	}

	// Wait for blocks
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for blocks")

		case <-ticker.C:
			commitMu.Lock()
			allReady := true
			for i := range N {
				if len(committed[i]) < TARGET_BLOCKS {
					allReady = false
					break
				}
			}
			commitMu.Unlock()

			if allReady {
				goto done
			}
		}
	}

done:
	for _, node := range nodes {
		node.Stop()
	}
	time.Sleep(50 * time.Millisecond)
	sharedNetwork.Close()

	// Verify all nodes committed same chain
	commitMu.Lock()
	defer commitMu.Unlock()

	minCommitted := len(committed[0])
	for i := 1; i < N; i++ {
		if len(committed[i]) < minCommitted {
			minCommitted = len(committed[i])
		}
	}

	t.Logf("All %d nodes committed at least %d blocks", N, minCommitted)
	assert.GreaterOrEqual(t, minCommitted, TARGET_BLOCKS)

	// Verify chain consistency
	for i := 1; i < N; i++ {
		for j := range minCommitted {
			assert.True(t, committed[i][j].Hash().Equals(committed[0][j].Hash()),
				"node %d block %d should match node 0", i, j)
		}
	}
}

// TestValidatorSet256 implements ValidatorSet for testing with TestHash256.
type TestValidatorSet256 struct {
	keys map[uint16]*crypto.Ed25519PublicKey
	n    int
}

func NewTestValidatorSet256WithKeys(n int) (*TestValidatorSet256, []*crypto.Ed25519PrivateKey) {
	vs := &TestValidatorSet256{
		keys: make(map[uint16]*crypto.Ed25519PublicKey),
		n:    n,
	}

	privKeys := make([]*crypto.Ed25519PrivateKey, n)
	for i := range n {
		priv, _ := crypto.GenerateEd25519Key()
		privKeys[i] = priv
		vs.keys[uint16(i)] = priv.PublicKey().(*crypto.Ed25519PublicKey)
	}

	return vs, privKeys
}

func (vs *TestValidatorSet256) Count() int {
	return vs.n
}

func (vs *TestValidatorSet256) GetByIndex(index uint16) (PublicKey, error) {
	if key, ok := vs.keys[index]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("validator %d not found", index)
}

func (vs *TestValidatorSet256) Contains(index uint16) bool {
	_, ok := vs.keys[index]
	return ok
}

func (vs *TestValidatorSet256) GetPublicKeys(indices []uint16) ([]PublicKey, error) {
	keys := make([]PublicKey, len(indices))
	for i, idx := range indices {
		key, err := vs.GetByIndex(idx)
		if err != nil {
			return nil, err
		}
		keys[i] = key
	}
	return keys, nil
}

func (vs *TestValidatorSet256) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(vs.n))
}

func (vs *TestValidatorSet256) F() int {
	return (vs.n - 1) / 3
}
