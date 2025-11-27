package twins

import (
	"math/rand"
)

// GeneratorConfig configures scenario generation.
type GeneratorConfig struct {
	// MinReplicas is the minimum number of honest replicas
	MinReplicas int

	// MaxReplicas is the maximum number of honest replicas
	MaxReplicas int

	// MinTwins is the minimum number of twin pairs
	MinTwins int

	// MaxTwins is the maximum number of twin pairs
	MaxTwins int

	// MinViews is the minimum number of views to run
	MinViews int

	// MaxViews is the maximum number of views to run
	MaxViews int

	// IncludePartitions enables network partition scenarios
	IncludePartitions bool

	// Seed for reproducible generation (0 = random)
	Seed int64
}

// DefaultGeneratorConfig returns the default generator configuration.
func DefaultGeneratorConfig() GeneratorConfig {
	return GeneratorConfig{
		MinReplicas:       3,
		MaxReplicas:       7,
		MinTwins:          0,
		MaxTwins:          2,
		MinViews:          3,
		MaxViews:          10,
		IncludePartitions: true,
		Seed:              0,
	}
}

// Generator generates random test scenarios.
type Generator struct {
	config GeneratorConfig
	rng    *rand.Rand
}

// NewGenerator creates a new scenario generator.
func NewGenerator(config GeneratorConfig) *Generator {
	seed := config.Seed
	if seed == 0 {
		seed = rand.Int63()
	}

	return &Generator{
		config: config,
		rng:    rand.New(rand.NewSource(seed)),
	}
}

// Generate generates a single random scenario.
func (g *Generator) Generate() Scenario {
	// Random number of replicas
	replicas := g.config.MinReplicas + g.rng.Intn(g.config.MaxReplicas-g.config.MinReplicas+1)

	// Random number of twins (respecting BFT constraints)
	maxTwins := g.config.MaxTwins
	totalNodes := replicas + (maxTwins * 2)
	f := (totalNodes - 1) / 3

	// Ensure twins <= f (BFT requirement)
	if maxTwins > f {
		maxTwins = f
	}

	twins := 0
	if maxTwins > 0 {
		twins = g.config.MinTwins + g.rng.Intn(maxTwins-g.config.MinTwins+1)
	}

	// Random number of views
	views := g.config.MinViews + g.rng.Intn(g.config.MaxViews-g.config.MinViews+1)

	// Random Byzantine behavior
	behaviors := []ByzantineBehavior{
		BehaviorHonest,
		BehaviorDoubleSign,
		BehaviorSilent,
		BehaviorRandom,
	}
	behavior := behaviors[g.rng.Intn(len(behaviors))]

	// If no twins, force honest behavior
	if twins == 0 {
		behavior = BehaviorHonest
	}

	scenario := Scenario{
		Replicas: replicas,
		Twins:    twins,
		Views:    views,
		Behavior: behavior,
	}

	// Add random partitions (20% chance)
	if g.config.IncludePartitions && g.rng.Float64() < 0.2 {
		scenario.Partitions = g.generatePartitions(replicas + twins*2)
	}

	return scenario
}

// GenerateN generates n random scenarios.
func (g *Generator) GenerateN(n int) []Scenario {
	scenarios := make([]Scenario, n)
	for i := range n {
		scenarios[i] = g.Generate()
	}
	return scenarios
}

// generatePartitions generates random network partitions.
func (g *Generator) generatePartitions(totalNodes int) []Partition {
	if totalNodes < 2 {
		return nil
	}

	// Simple partition: split nodes into two groups
	splitPoint := 1 + g.rng.Intn(totalNodes-1)

	partition1 := make([]int, splitPoint)
	partition2 := make([]int, totalNodes-splitPoint)

	for i := range splitPoint {
		partition1[i] = i
	}

	for i := range totalNodes - splitPoint {
		partition2[i] = splitPoint + i
	}

	return []Partition{
		{Nodes: partition1},
		{Nodes: partition2},
	}
}

// GenerateComprehensive generates a comprehensive test suite.
// Returns a mix of edge cases and random scenarios.
func GenerateComprehensive(randomCount int) []Scenario {
	scenarios := []Scenario{}

	// Add basic scenarios
	scenarios = append(scenarios, GenerateBasicScenarios()...)

	// Add edge cases
	scenarios = append(scenarios, []Scenario{
		// Minimum viable: 4 nodes
		{
			Replicas: 4,
			Twins:    0,
			Views:    3,
			Behavior: BehaviorHonest,
		},

		// Maximum twins for 4 nodes (f=1)
		{
			Replicas: 2,
			Twins:    1,
			Views:    5,
			Behavior: BehaviorDoubleSign,
		},

		// Larger network: 7 nodes
		{
			Replicas: 7,
			Twins:    0,
			Views:    5,
			Behavior: BehaviorHonest,
		},

		// 7 nodes with maximum twins (f=2)
		{
			Replicas: 5,
			Twins:    2,
			Views:    5,
			Behavior: BehaviorDoubleSign,
		},

		// All silent (crash faults)
		{
			Replicas: 3,
			Twins:    1,
			Views:    5,
			Behavior: BehaviorSilent,
		},
	}...)

	// Add random scenarios
	gen := NewGenerator(DefaultGeneratorConfig())
	scenarios = append(scenarios, gen.GenerateN(randomCount)...)

	return scenarios
}
