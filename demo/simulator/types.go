package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	hotstuff2 "github.com/edgedlt/hotstuff2"
	"github.com/edgedlt/hotstuff2/internal/crypto"
)

// SimHash implements hotstuff2.Hash for simulation.
type SimHash [32]byte

// NewSimHash creates a SimHash from a string.
func NewSimHash(data string) SimHash {
	return sha256.Sum256([]byte(data))
}

func (h SimHash) Bytes() []byte {
	return h[:]
}

func (h SimHash) Equals(other hotstuff2.Hash) bool {
	if otherSim, ok := other.(SimHash); ok {
		return h == otherSim
	}
	return false
}

func (h SimHash) String() string {
	return hex.EncodeToString(h[:8])
}

// SimBlock implements hotstuff2.Block for simulation.
type SimBlock struct {
	hash      SimHash
	height    uint32
	prevHash  SimHash
	payload   []byte
	proposer  uint16
	timestamp uint64
}

// NewSimBlock creates a new simulation block.
func NewSimBlock(height uint32, prevHash SimHash, proposer uint16) *SimBlock {
	data := fmt.Sprintf("block-%d-%s-%d-%d", height, prevHash.String(), proposer, height*1000)
	return &SimBlock{
		hash:      NewSimHash(data),
		height:    height,
		prevHash:  prevHash,
		payload:   []byte(data),
		proposer:  proposer,
		timestamp: uint64(height) * 1000,
	}
}

func (b *SimBlock) Hash() SimHash         { return b.hash }
func (b *SimBlock) Height() uint32        { return b.height }
func (b *SimBlock) PrevHash() SimHash     { return b.prevHash }
func (b *SimBlock) Payload() []byte       { return b.payload }
func (b *SimBlock) ProposerIndex() uint16 { return b.proposer }
func (b *SimBlock) Timestamp() uint64     { return b.timestamp }
func (b *SimBlock) Bytes() []byte         { return []byte(fmt.Sprintf("block-%d", b.height)) }

// SimStorage implements hotstuff2.Storage for simulation.
type SimStorage struct {
	mu            sync.RWMutex
	blocks        map[string]*SimBlock
	qcs           map[string]*hotstuff2.QC[SimHash]
	lastBlock     *SimBlock
	highestLocked *hotstuff2.QC[SimHash]
	view          uint32
}

// NewSimStorage creates a new simulation storage.
func NewSimStorage() *SimStorage {
	genesis := NewSimBlock(0, SimHash{}, 0)
	return &SimStorage{
		blocks:    map[string]*SimBlock{genesis.Hash().String(): genesis},
		qcs:       make(map[string]*hotstuff2.QC[SimHash]),
		lastBlock: genesis,
		view:      0,
	}
}

func (s *SimStorage) GetBlock(hash SimHash) (hotstuff2.Block[SimHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if block, ok := s.blocks[hash.String()]; ok {
		return block, nil
	}
	return nil, fmt.Errorf("block not found")
}

func (s *SimStorage) PutBlock(block hotstuff2.Block[SimHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sb, ok := block.(*SimBlock); ok {
		s.blocks[block.Hash().String()] = sb
		if sb.Height() > s.lastBlock.Height() {
			s.lastBlock = sb
		}
	}
	return nil
}

func (s *SimStorage) GetLastBlock() (hotstuff2.Block[SimHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastBlock, nil
}

func (s *SimStorage) GetQC(nodeHash SimHash) (hotstuff2.QuorumCertificate[SimHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if qc, ok := s.qcs[nodeHash.String()]; ok {
		return qc, nil
	}
	return nil, fmt.Errorf("QC not found")
}

func (s *SimStorage) PutQC(qc hotstuff2.QuorumCertificate[SimHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*hotstuff2.QC[SimHash]); ok {
		s.qcs[qc.Node().String()] = qcConcrete
	}
	return nil
}

func (s *SimStorage) GetHighestLockedQC() (hotstuff2.QuorumCertificate[SimHash], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highestLocked, nil
}

func (s *SimStorage) PutHighestLockedQC(qc hotstuff2.QuorumCertificate[SimHash]) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if qcConcrete, ok := qc.(*hotstuff2.QC[SimHash]); ok {
		s.highestLocked = qcConcrete
	}
	return nil
}

func (s *SimStorage) GetView() (uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.view, nil
}

func (s *SimStorage) PutView(view uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.view = view
	return nil
}

func (s *SimStorage) Close() error {
	return nil
}

// SimExecutor implements hotstuff2.Executor for simulation.
type SimExecutor struct {
	mu     sync.Mutex
	blocks map[string]*SimBlock
}

// NewSimExecutor creates a new simulation executor.
func NewSimExecutor() *SimExecutor {
	return &SimExecutor{
		blocks: make(map[string]*SimBlock),
	}
}

func (e *SimExecutor) Execute(block hotstuff2.Block[SimHash]) (SimHash, error) {
	return block.Hash(), nil
}

func (e *SimExecutor) Verify(block hotstuff2.Block[SimHash]) error {
	return nil
}

func (e *SimExecutor) GetStateHash() SimHash {
	return SimHash{}
}

func (e *SimExecutor) CreateBlock(height uint32, prevHash SimHash, proposerIndex uint16) (hotstuff2.Block[SimHash], error) {
	// Block time enforcement is handled by the pacemaker's MinBlockTime setting.
	// The DemoPacemakerConfig sets MinBlockTime = 1s which ensures:
	// - Leaders wait until last_commit_time + 1s before proposing
	// - Validators reject proposals that arrive too early
	e.mu.Lock()
	defer e.mu.Unlock()

	block := NewSimBlock(height, prevHash, proposerIndex)
	e.blocks[block.Hash().String()] = block
	return block, nil
}

// SimValidatorSet implements hotstuff2.ValidatorSet for simulation.
type SimValidatorSet struct {
	keys map[uint16]*crypto.Ed25519PublicKey
	n    int
}

// NewSimValidatorSetWithKeys creates a validator set and returns the private keys.
func NewSimValidatorSetWithKeys(n int) (*SimValidatorSet, []*crypto.Ed25519PrivateKey) {
	vs := &SimValidatorSet{
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

func (vs *SimValidatorSet) Count() int {
	return vs.n
}

func (vs *SimValidatorSet) GetByIndex(index uint16) (hotstuff2.PublicKey, error) {
	if key, ok := vs.keys[index]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("validator %d not found", index)
}

func (vs *SimValidatorSet) Contains(index uint16) bool {
	_, ok := vs.keys[index]
	return ok
}

func (vs *SimValidatorSet) GetPublicKeys(indices []uint16) ([]hotstuff2.PublicKey, error) {
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

func (vs *SimValidatorSet) GetLeader(view uint32) uint16 {
	return uint16(view % uint32(vs.n))
}

func (vs *SimValidatorSet) F() int {
	return (vs.n - 1) / 3
}
