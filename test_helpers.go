package hotstuff2

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/edgedlt/hotstuff2/internal/crypto"
)

// Compile-time interface verification (per dBFT pattern).
// These ensure test types correctly implement production interfaces.
var (
	_ Hash                        = TestHash{}
	_ Block[TestHash]             = (*TestBlock)(nil)
	_ ValidatorSet                = (*TestValidatorSet)(nil)
	_ ValidatorSet                = (*GenericValidatorSet)(nil)
	_ Storage[TestHash]           = (*TestStorage)(nil)
	_ Network[TestHash]           = (*TestNetwork)(nil)
	_ Executor[TestHash]          = (*TestExecutor)(nil)
	_ QuorumCertificate[TestHash] = (*QC[TestHash])(nil)
)

// TestHash implements Hash for testing using SHA256.
type TestHash [32]byte

// NewTestHash creates a test hash from a string.
func NewTestHash(data string) TestHash {
	return sha256.Sum256([]byte(data))
}

func (h TestHash) Bytes() []byte {
	return h[:]
}

func (h TestHash) Equals(other Hash) bool {
	if otherTest, ok := other.(TestHash); ok {
		return h == otherTest
	}
	return false
}

func (h TestHash) String() string {
	return hex.EncodeToString(h[:8])
}

// TestBlock implements Block for testing.
type TestBlock struct {
	hash      TestHash
	height    uint32
	prevHash  TestHash
	payload   []byte
	proposer  uint16
	timestamp uint64
}

func NewTestBlock(height uint32, prevHash TestHash, proposer uint16) *TestBlock {
	data := fmt.Sprintf("block-%d-%s-%d", height, prevHash.String(), proposer)
	return &TestBlock{
		hash:      NewTestHash(data),
		height:    height,
		prevHash:  prevHash,
		payload:   []byte(data), // Use block data as payload
		proposer:  proposer,
		timestamp: uint64(height) * 1000,
	}
}

func (b *TestBlock) Hash() TestHash        { return b.hash }
func (b *TestBlock) Height() uint32        { return b.height }
func (b *TestBlock) PrevHash() TestHash    { return b.prevHash }
func (b *TestBlock) Payload() []byte       { return b.payload }
func (b *TestBlock) ProposerIndex() uint16 { return b.proposer }
func (b *TestBlock) Timestamp() uint64     { return b.timestamp }
func (b *TestBlock) Bytes() []byte         { return []byte(fmt.Sprintf("block-%d", b.height)) }

// TestValidatorSet implements ValidatorSet for testing.
type TestValidatorSet struct {
	keys map[uint16]*crypto.Ed25519PublicKey
	n    int
}

// GenericValidatorSet implements ValidatorSet for testing with any key type.
type GenericValidatorSet struct {
	keys map[uint16]PublicKey
	n    int
}

func NewTestValidatorSet(n int) *TestValidatorSet {
	vs, _ := NewTestValidatorSetWithKeys(n)
	return vs
}

// NewTestValidatorSetWithKeys creates a validator set and returns the private keys.
func NewTestValidatorSetWithKeys(n int) (*TestValidatorSet, []*crypto.Ed25519PrivateKey) {
	vs := &TestValidatorSet{
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

func (vs *TestValidatorSet) Count() int {
	return vs.n
}

func (vs *TestValidatorSet) GetByIndex(index uint16) (PublicKey, error) {
	if key, ok := vs.keys[index]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("validator %d not found", index)
}

func (vs *TestValidatorSet) Contains(index uint16) bool {
	_, ok := vs.keys[index]
	return ok
}

func (vs *TestValidatorSet) GetPublicKeys(indices []uint16) ([]PublicKey, error) {
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

func (vs *TestValidatorSet) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(vs.n))
}

func (vs *TestValidatorSet) F() int {
	return (vs.n - 1) / 3
}

// NewGenericValidatorSet creates a generic validator set with any key type.
func NewGenericValidatorSet(n int, keys map[uint16]PublicKey) *GenericValidatorSet {
	return &GenericValidatorSet{
		keys: keys,
		n:    n,
	}
}

func (vs *GenericValidatorSet) Count() int {
	return vs.n
}

func (vs *GenericValidatorSet) GetByIndex(index uint16) (PublicKey, error) {
	if key, ok := vs.keys[index]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("validator %d not found", index)
}

func (vs *GenericValidatorSet) Contains(index uint16) bool {
	_, ok := vs.keys[index]
	return ok
}

func (vs *GenericValidatorSet) GetPublicKeys(indices []uint16) ([]PublicKey, error) {
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

func (vs *GenericValidatorSet) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(vs.n))
}

func (vs *GenericValidatorSet) F() int {
	return (vs.n - 1) / 3
}

// TestStorage implements Storage for testing.
type TestStorage struct {
	mu            sync.RWMutex
	blocks        map[string]*TestBlock
	qcs           map[string]*QC[TestHash]
	lastBlock     *TestBlock
	highestLocked *QC[TestHash]
	view          uint32
}

func NewTestStorage() *TestStorage {
	genesis := NewTestBlock(0, TestHash{}, 0)
	return &TestStorage{
		blocks:    map[string]*TestBlock{genesis.Hash().String(): genesis},
		qcs:       make(map[string]*QC[TestHash]),
		lastBlock: genesis,
		view:      0,
	}
}

func (s *TestStorage) GetBlock(hash TestHash) (Block[TestHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if block, ok := s.blocks[hash.String()]; ok {
		return block, nil
	}
	return nil, fmt.Errorf("block not found")
}

func (s *TestStorage) PutBlock(block Block[TestHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if tb, ok := block.(*TestBlock); ok {
		s.blocks[block.Hash().String()] = tb
		if tb.Height() > s.lastBlock.Height() {
			s.lastBlock = tb
		}
	}
	return nil
}

func (s *TestStorage) GetLastBlock() (Block[TestHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBlock, nil
}

func (s *TestStorage) GetQC(nodeHash TestHash) (QuorumCertificate[TestHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if qc, ok := s.qcs[nodeHash.String()]; ok {
		return qc, nil
	}
	return nil, fmt.Errorf("QC not found")
}

func (s *TestStorage) PutQC(qc QuorumCertificate[TestHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*QC[TestHash]); ok {
		s.qcs[qc.Node().String()] = qcConcrete
	}
	return nil
}

func (s *TestStorage) GetHighestLockedQC() (QuorumCertificate[TestHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highestLocked, nil
}

func (s *TestStorage) PutHighestLockedQC(qc QuorumCertificate[TestHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*QC[TestHash]); ok {
		s.highestLocked = qcConcrete
	}
	return nil
}

func (s *TestStorage) GetView() (uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view, nil
}

func (s *TestStorage) PutView(view uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = view
	return nil
}

func (s *TestStorage) Close() error {
	return nil
}

// TestNetwork implements Network for testing.
type TestNetwork struct {
	mu      sync.RWMutex
	msgChan chan ConsensusPayload[TestHash]
	sent    []ConsensusPayload[TestHash]
}

func NewTestNetwork() *TestNetwork {
	return &TestNetwork{
		msgChan: make(chan ConsensusPayload[TestHash], 100),
		sent:    make([]ConsensusPayload[TestHash], 0),
	}
}

func (n *TestNetwork) Broadcast(payload ConsensusPayload[TestHash]) {
	n.mu.Lock()
	n.sent = append(n.sent, payload)
	n.mu.Unlock()

	select {
	case n.msgChan <- payload:
	default:
	}
}

func (n *TestNetwork) SendTo(validatorIndex uint16, payload ConsensusPayload[TestHash]) {
	n.Broadcast(payload)
}

func (n *TestNetwork) Receive() <-chan ConsensusPayload[TestHash] {
	return n.msgChan
}

func (n *TestNetwork) Close() error {
	close(n.msgChan)
	return nil
}

func (n *TestNetwork) SentMessages() []ConsensusPayload[TestHash] {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]ConsensusPayload[TestHash]{}, n.sent...)
}

// TestExecutor implements Executor for testing.
type TestExecutor struct {
	blocks map[string]*TestBlock
}

func NewTestExecutor() *TestExecutor {
	return &TestExecutor{
		blocks: make(map[string]*TestBlock),
	}
}

func (e *TestExecutor) Execute(block Block[TestHash]) (TestHash, error) {
	return block.Hash(), nil
}

func (e *TestExecutor) Verify(block Block[TestHash]) error {
	return nil
}

func (e *TestExecutor) GetStateHash() TestHash {
	return TestHash{}
}

func (e *TestExecutor) CreateBlock(height uint32, prevHash TestHash, proposerIndex uint16) (Block[TestHash], error) {
	block := NewTestBlock(height, prevHash, proposerIndex)
	e.blocks[block.Hash().String()] = block
	return block, nil
}

// Generic mock constructors for use with any Hash type

// NewMockStorage creates a generic mock storage.
func NewMockStorage[H Hash]() Storage[H] {
	// For testing, we only support TestHash
	var h H
	switch any(h).(type) {
	case TestHash:
		return any(NewTestStorage()).(Storage[H])
	default:
		panic("NewMockStorage: only TestHash is supported for testing")
	}
}

// NewMockNetwork creates a generic mock network.
func NewMockNetwork[H Hash]() Network[H] {
	var h H
	switch any(h).(type) {
	case TestHash:
		return any(NewTestNetwork()).(Network[H])
	default:
		panic("NewMockNetwork: only TestHash is supported for testing")
	}
}

// NewMockExecutor creates a generic mock executor.
func NewMockExecutor[H Hash]() Executor[H] {
	var h H
	switch any(h).(type) {
	case TestHash:
		return any(NewTestExecutor()).(Executor[H])
	default:
		panic("NewMockExecutor: only TestHash is supported for testing")
	}
}
