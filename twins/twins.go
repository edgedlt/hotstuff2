// Package twins implements the Twins framework for Byzantine fault testing.
//
// The Twins framework creates "twin" replicas that share the same cryptographic
// keys but can behave independently. This allows testing:
// - Equivocation (signing conflicting blocks)
// - Network partitions with Byzantine nodes
// - Safety violation detection
//
// Based on "Twins: BFT Systems Made Robust" by Bano et al.
package twins

import (
	"fmt"

	"github.com/edgedlt/hotstuff2"
)

// Scenario defines a Byzantine testing scenario.
type Scenario struct {
	// Replicas is the number of honest replicas (non-twins)
	Replicas int

	// Twins is the number of twin pairs
	// Each twin pair shares keys but can behave independently
	Twins int

	// Partitions defines network partitions
	// Each partition is a list of node IDs that can communicate
	Partitions []Partition

	// Views is the number of consensus views to run
	Views int

	// Byzantine behavior to test
	Behavior ByzantineBehavior
}

// Partition represents a network partition.
type Partition struct {
	// Nodes is the list of node IDs in this partition
	Nodes []int
}

// ByzantineBehavior defines the type of Byzantine behavior to test.
type ByzantineBehavior int

const (
	// BehaviorHonest - all nodes behave honestly (baseline)
	BehaviorHonest ByzantineBehavior = iota

	// BehaviorDoubleSign - twins sign conflicting blocks
	BehaviorDoubleSign

	// BehaviorEquivocation - twins send different messages to different nodes
	BehaviorEquivocation

	// BehaviorSilent - twins are silent (crash fault)
	BehaviorSilent

	// BehaviorRandom - twins exhibit random Byzantine behavior
	BehaviorRandom

	// BehaviorDelay - twins delay and reorder messages
	BehaviorDelay
)

func (b ByzantineBehavior) String() string {
	switch b {
	case BehaviorHonest:
		return "Honest"
	case BehaviorDoubleSign:
		return "DoubleSign"
	case BehaviorEquivocation:
		return "Equivocation"
	case BehaviorSilent:
		return "Silent"
	case BehaviorRandom:
		return "Random"
	case BehaviorDelay:
		return "Delay"
	default:
		return "Unknown"
	}
}

// Result represents the result of executing a scenario.
type Result struct {
	// Scenario is the scenario that was executed
	Scenario Scenario

	// Success indicates if the scenario passed (no safety violations)
	Success bool

	// Violations contains any detected safety violations
	Violations []Violation

	// BlocksCommitted is the number of blocks committed across all nodes
	BlocksCommitted int

	// MessagesExchanged is the total number of messages sent
	MessagesExchanged int
}

// Violation represents a detected safety violation.
type Violation struct {
	// Type of violation
	Type ViolationType

	// Description of what went wrong
	Description string

	// NodeID of the node that detected/caused the violation
	NodeID int

	// View where the violation occurred
	View uint32

	// Additional context (block hashes, etc.)
	Context map[string]any
}

// ViolationType categorizes safety violations.
type ViolationType int

const (
	// ViolationNone - no violation (shouldn't happen in Violation slice)
	ViolationNone ViolationType = iota

	// ViolationFork - two blocks committed at same height
	ViolationFork

	// ViolationDoubleSign - same validator signed conflicting blocks
	ViolationDoubleSign

	// ViolationInvalidQC - QC with invalid signatures
	ViolationInvalidQC

	// ViolationSafetyViolation - voted for conflicting blocks (SafeNode rule violated)
	ViolationSafetyViolation

	// ViolationConflictingProposal - leader proposed different blocks in same view
	ViolationConflictingProposal

	// ViolationConflictingNewView - validator sent different new-view messages
	ViolationConflictingNewView
)

func (v ViolationType) String() string {
	switch v {
	case ViolationNone:
		return "None"
	case ViolationFork:
		return "Fork"
	case ViolationDoubleSign:
		return "DoubleSign"
	case ViolationInvalidQC:
		return "InvalidQC"
	case ViolationSafetyViolation:
		return "SafetyViolation"
	case ViolationConflictingProposal:
		return "ConflictingProposal"
	case ViolationConflictingNewView:
		return "ConflictingNewView"
	default:
		return "Unknown"
	}
}

// ValidateScenario checks if a scenario is valid.
func ValidateScenario(s Scenario) error {
	if s.Replicas < 1 {
		return fmt.Errorf("replicas must be >= 1, got %d", s.Replicas)
	}

	if s.Twins < 0 {
		return fmt.Errorf("twins must be >= 0, got %d", s.Twins)
	}

	totalNodes := s.Replicas + (s.Twins * 2)
	f := (totalNodes - 1) / 3

	// BFT requires 3f+1 nodes to tolerate f faults
	// Twins count as 2 nodes but 1 fault (they share keys)
	totalFaults := s.Twins
	if totalFaults > f {
		return fmt.Errorf("scenario violates BFT assumptions: %d twins exceeds f=%d tolerance (n=%d)",
			s.Twins, f, totalNodes)
	}

	if s.Views < 1 {
		return fmt.Errorf("views must be >= 1, got %d", s.Views)
	}

	// Validate partitions reference valid node IDs
	for i, partition := range s.Partitions {
		for _, nodeID := range partition.Nodes {
			if nodeID < 0 || nodeID >= totalNodes {
				return fmt.Errorf("partition %d references invalid node ID %d (total nodes: %d)",
					i, nodeID, totalNodes)
			}
		}
	}

	return nil
}

// GenerateBasicScenarios generates a set of basic test scenarios.
func GenerateBasicScenarios() []Scenario {
	return []Scenario{
		// Baseline: 4 honest nodes, no twins
		{
			Replicas: 4,
			Twins:    0,
			Views:    5,
			Behavior: BehaviorHonest,
		},

		// Single twin pair, honest behavior
		{
			Replicas: 3,
			Twins:    1,
			Views:    5,
			Behavior: BehaviorHonest,
		},

		// Single twin pair, double-sign attack
		{
			Replicas: 3,
			Twins:    1,
			Views:    5,
			Behavior: BehaviorDoubleSign,
		},

		// Network partition: [0,1] and [2,3]
		{
			Replicas: 4,
			Twins:    0,
			Views:    5,
			Behavior: BehaviorHonest,
			Partitions: []Partition{
				{Nodes: []int{0, 1}},
				{Nodes: []int{2, 3}},
			},
		},

		// 7 nodes with 2 twin pairs
		{
			Replicas: 5,
			Twins:    2,
			Views:    5,
			Behavior: BehaviorDoubleSign,
		},
	}
}

// TwinID returns the twin ID for a node in a twin pair.
// Twin pairs are numbered starting after all honest replicas.
// For each twin pair i:
//   - First twin: replicas + i*2
//   - Second twin: replicas + i*2 + 1
func TwinID(replicas, twinPairIndex, twinIndex int) int {
	if twinIndex != 0 && twinIndex != 1 {
		return -1 // Invalid twin index
	}
	return replicas + twinPairIndex*2 + twinIndex
}

// IsTwin returns true if the given node ID is part of a twin pair.
func IsTwin(nodeID, replicas int) bool {
	return nodeID >= replicas
}

// GetTwinPair returns the IDs of both twins in a pair.
// Returns (-1, -1) if nodeID is not a twin.
func GetTwinPair(nodeID, replicas int) (int, int) {
	if !IsTwin(nodeID, replicas) {
		return -1, -1
	}

	offset := nodeID - replicas
	pairIndex := offset / 2
	twin1 := TwinID(replicas, pairIndex, 0)
	twin2 := TwinID(replicas, pairIndex, 1)
	return twin1, twin2
}

// GetValidatorIndex returns the validator index for a node.
// For twins, both members of the pair share the same validator index.
func GetValidatorIndex(nodeID, replicas int) int {
	if !IsTwin(nodeID, replicas) {
		return nodeID // Honest replica uses its own index
	}

	// Twin uses its pair's validator index
	offset := nodeID - replicas
	pairIndex := offset / 2
	return replicas + pairIndex
}

// Executor executes a twins scenario and detects safety violations.
type Executor[H hotstuff2.Hash] struct {
	scenario Scenario

	// Nodes includes both honest replicas and twins
	nodes map[int]*hotstuff2.HotStuff2[H]

	// Network for message routing
	network *TwinsNetwork[H]

	// Violation detector
	detector *ViolationDetector[H]
}

// NewExecutor creates a new twins scenario executor.
func NewExecutor[H hotstuff2.Hash](scenario Scenario) (*Executor[H], error) {
	if err := ValidateScenario(scenario); err != nil {
		return nil, fmt.Errorf("invalid scenario: %w", err)
	}

	totalNodes := scenario.Replicas + (scenario.Twins * 2)
	network := NewTwinsNetwork[H](totalNodes, scenario.Partitions)
	detector := NewViolationDetector[H]()

	return &Executor[H]{
		scenario: scenario,
		nodes:    make(map[int]*hotstuff2.HotStuff2[H]),
		network:  network,
		detector: detector,
	}, nil
}
